package hailuo

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// v2 响应处理：建单、轮询解析、OpenAI video 转换
// ---------------------------------------------------------------------------

// looksLikeV2Query 判断一份上游报文是不是 v2 形状。轮询阶段拿不到 RelayInfo，
// 只能靠报文自身特征判断（v1 顶层必有 base_resp，v2 顶层是 task/error）。
func looksLikeV2Query(respBody []byte) bool {
	if len(bytes.TrimSpace(respBody)) == 0 {
		return false
	}
	var probe struct {
		Task     json.RawMessage `json:"task"`
		Type     string          `json:"type"`
		BaseResp *struct {
			StatusCode *int `json:"status_code"`
		} `json:"base_resp"`
	}
	if err := common.Unmarshal(respBody, &probe); err != nil {
		return false
	}
	if len(bytes.TrimSpace(probe.Task)) > 0 && probe.Task[0] == '{' {
		return true
	}
	if probe.Type == "error" && probe.BaseResp == nil {
		return true
	}
	return false
}

// doResponseV2 处理 v2 建单响应。注意要在**写客户端响应之前**把上游错误转成
// TaskError：v2 失败时 HTTP 可能是 4xx/5xx，但错误信息在 body 里。
func (a *TaskAdaptor) doResponseV2(c *gin.Context, responseBody []byte, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	var parsed V2CreateResponse
	if err := common.Unmarshal(responseBody, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("body: %s", truncateForError(responseBody)),
			"unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if errBody := parsed.Error; errBody != nil {
		return "", nil, v2ErrorToTaskError(errBody)
	}
	if parsed.BaseResp.StatusCode != StatusSuccess {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("minimax v2 api error(%d): %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg),
			strconv.Itoa(parsed.BaseResp.StatusCode), http.StatusBadRequest)
	}
	if strings.TrimSpace(parsed.TaskID) == "" {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("minimax v2 create response missing task_id: %s", truncateForError(responseBody)),
			"invalid_response", http.StatusInternalServerError)
	}

	// 落库报文里补两个标记：端点形态（供 FetchTask 判路径）+ 对外模型名。
	// 不这么做的话，轮询阶段无法区分 v1/v2，会把 v2 任务打到 v1 端点上。
	stored := map[string]any{}
	if err := common.Unmarshal(responseBody, &stored); err != nil {
		stored = map[string]any{}
	}
	stored["_minimax_endpoint"] = "v2"
	stored["model"] = info.UpstreamModelName
	storedData, err := common.Marshal(stored)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "marshal_stored_response_failed", http.StatusInternalServerError)
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	if spec, ok := lookupV2Spec(info.UpstreamModelName); ok {
		ov.Seconds = strconv.Itoa(spec.DefaultDur)
	}
	c.JSON(http.StatusOK, ov)
	return parsed.TaskID, storedData, nil
}

// v2ErrorToTaskError 把 v2 错误体映射成 TaskError。
// http_code 402/insufficient_balance 故意保持为上游错误（不是 local）：
// 它真的是上游账户没钱，应该触发换渠道与 AutoBan——这正好是我们刚修好的
// 分类器要认的那一类。
func v2ErrorToTaskError(e *V2ErrorBody) *dto.TaskError {
	status := http.StatusBadRequest
	if code, err := strconv.Atoi(e.HTTPCode); err == nil && code >= 100 && code < 600 {
		status = code
	}
	code := e.Type
	if code == "" {
		code = "minimax_api_error"
	}
	return service.TaskErrorWrapper(fmt.Errorf("minimax v2 error(%s): %s", e.Type, e.Message), code, status)
}

func truncateForError(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// parseV2TaskResult 解析 v2 轮询结果。
func parseV2TaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var resp V2QueryTaskResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal v2 task result failed: %w", err)
	}
	result := &relaycommon.TaskInfo{}
	if resp.Error != nil {
		result.Status = model.TaskStatusFailure
		result.Progress = "100%"
		result.Reason = resp.Error.Message
		return result, nil
	}
	if resp.Task == nil {
		return nil, fmt.Errorf("v2 query response missing task")
	}

	task := resp.Task
	switch strings.ToLower(task.Status) {
	case V2StatusSucceeded:
		result.Status = model.TaskStatusSuccess
		result.Progress = "100%"
		result.Url = task.Content.URL
		result.ResultText = task.Content.Prompt
		if result.Url == "" && result.ResultText == "" {
			// 成功但无产物：宁可报失败也不要报成功，否则客户端拿到空任务。
			result.Status = model.TaskStatusFailure
			result.Reason = "upstream reported success but returned neither url nor prompt"
		}
	case V2StatusFailed, V2StatusCancelled:
		result.Status = model.TaskStatusFailure
		result.Progress = "100%"
		if result.Reason == "" {
			result.Reason = "minimax " + task.Status
		}
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = "30%"
		if strings.EqualFold(task.Status, V2StatusRunning) {
			result.Progress = "50%"
		}
	}
	return result, nil
}

// convertV2ToOpenAIVideo 把 v2 任务转成 OpenAI video 响应。
// 文本产物（Context-IR 提示词）走 metadata.prompt + media_type=text，
// **不返回 url**——指向 content 代理的占位地址会诱使客户端去拉一个不存在的文件。
func convertV2ToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var resp V2QueryTaskResponse
	if err := common.Unmarshal(originTask.Data, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal minimax v2 task data failed: %w", err)
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if text := strings.TrimSpace(originTask.PrivateData.ResultText); text != "" {
		openAIVideo.Status = dto.VideoStatusCompleted
		// ToOpenAIVideo() 会预置 metadata.url（此处为空串）。文本交付物必须
		// **删掉**这个键而不是留空值：客户端常写 `if (url) fetch(url)`，空串
		// 也会被当成“有产物”而拉一个不存在的文件。
		delete(openAIVideo.Metadata, "url")
		openAIVideo.SetMetadata("prompt", text)
		openAIVideo.SetMetadata("media_type", "text")
	} else if url := strings.TrimSpace(v2ResultURL(resp.Task)); url != "" {
		openAIVideo.Status = dto.VideoStatusCompleted
		openAIVideo.SetMetadata("url", url)
		openAIVideo.SetMetadata("media_type", "video")
		if resp.Task != nil {
			openAIVideo.Seconds = strconv.Itoa(resp.Task.Duration)
			if size := v2SizeFor(resp.Task, ""); size != "" {
				openAIVideo.Size = size
			}
		}
	}

	if originTask.Status == model.TaskStatusFailure {
		reason := originTask.FailReason
		if reason == "" && resp.Task != nil {
			reason = resp.Task.Status
		}
		openAIVideo.Error = &dto.OpenAIVideoError{Message: reason}
	} else if resp.Error != nil {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: resp.Error.Message,
			Code:    resp.Error.Type,
		}
	}

	data, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, fmt.Errorf("marshal openai video failed: %w", err)
	}
	return data, nil
}

func v2ResultURL(task *V2Task) string {
	if task == nil {
		return ""
	}
	return task.Content.URL
}
