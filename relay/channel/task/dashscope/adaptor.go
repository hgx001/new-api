package dashscope

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// submitRequest 提交生成任务的请求体（DashScope 原生视频合成协议，非 OpenAI 格式）。
// model 固定为上游 wan3.0-video；分辨率经 resolution（480P/720P）+ ratio（16:9/9:16...）区分。
type submitRequest struct {
	Model      string       `json:"model"`
	Input      submitInput  `json:"input"`
	Parameters submitParams `json:"parameters"`
}

type submitInput struct {
	Prompt string `json:"prompt"`
}

type submitParams struct {
	Resolution string `json:"resolution"`
	Ratio      string `json:"ratio"`
	Duration   int    `json:"duration"`
}

// submitResponse 提交响应：{"output":{"task_id":...,"task_status":"PENDING"}} 或错误 {"code":"InvalidParameter","message":"..."}
type submitResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
	} `json:"output"`
}

// pollResponse 轮询响应：{"output":{"task_id":...,"task_status":"SUCCEEDED","video_url":"...","results":[...]}}
type pollResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Output  struct {
		TaskID     string `json:"task_id"`
		TaskStatus string `json:"task_status"`
		SubmitTime string `json:"submit_time"`
		// 成功时优先取 video_url（DashScope 视频合成直链）；
		// 部分接口也返回 results 数组，做兼容兜底。
		VideoURL string `json:"video_url"`
		Results  []struct {
			URL  string `json:"url"`
			Type string `json:"type"`
		} `json:"results"`
	} `json:"output"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	resolution  string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	if a.baseURL == "" {
		a.baseURL = baseURLDefault
	}
	a.apiKey = info.ApiKey
	// 兼容旧模型名映射（wan3.0-video-480p / wan3.0-video-720p）
	if r, ok := resolutionByModel[info.OriginModelName]; ok && r != "" {
		a.resolution = r
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

// resolveResolution 从请求参数解析分辨率，优先级：
// 1. 顶层 resolution（"480P" / "720P"）
// 2. metadata.resolution（"480P" / "720P"）
// 3. size 映射（"960x540" → 480P, "1280x720" → 720P）
// 4. Init 阶段通过模型名设置的值（兼容旧调用方式）
// 5. 默认 480P
func (a *TaskAdaptor) resolveResolution(req relaycommon.TaskSubmitReq) string {
	// 1. 顶层 resolution（AutoDL 等任务模型使用）
	if r := strings.ToUpper(strings.TrimSpace(req.Resolution)); r != "" {
		if _, valid := resolutionSizeRatio[r]; valid {
			return r
		}
	}

	// 2. metadata.resolution 优先
	if req.Metadata != nil {
		if r, ok := req.Metadata["resolution"].(string); ok {
			r = strings.ToUpper(strings.TrimSpace(r))
			if _, valid := resolutionSizeRatio[r]; valid {
				return r
			}
		}
	}

	// 3. size 映射
	if req.Size != "" {
		if r, ok := sizeToResolution[req.Size]; ok {
			return r
		}
	}

	// 4. 模型名映射（兼容旧调用方式）
	if a.resolution != "" {
		return a.resolution
	}

	// 5. 默认
	return defaultResolution
}

// resolveDuration 解析时长并钳制到 2 秒 ~ 30 秒（wan3.0-video 官方范围）
func (a *TaskAdaptor) resolveDuration(req relaycommon.TaskSubmitReq) int {
	d := req.Duration
	if d <= 0 {
		if s, err := strconv.Atoi(req.Seconds); err == nil {
			d = s
		}
	}
	if d <= 0 {
		return defaultDuration
	}
	if d < minDuration {
		return minDuration
	}
	if d > maxDuration {
		return maxDuration
	}
	return d
}

// resolveRatio 解析客户端要求的画面比例：优先 metadata.ratio，其次 req.Size（"1280x720"），最后默认 16:9。
func resolveRatio(req relaycommon.TaskSubmitReq) string {
	if req.Metadata != nil {
		if r, ok := req.Metadata["ratio"].(string); ok && isValidRatio(r) {
			return r
		}
	}
	if r, ok := ratioFromSize(req.Size); ok {
		return r
	}
	return defaultRatio
}

func isValidRatio(r string) bool {
	switch r {
	case "adaptive", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	}
	return false
}

// ratioFromSize 把 "1280x720" 这类尺寸映射到最接近的 ratio 档位
func ratioFromSize(size string) (string, bool) {
	if size == "" {
		return "", false
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	candidates := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
		"4:3":  4.0 / 3.0,
		"3:4":  3.0 / 4.0,
		"1:1":  1.0,
	}
	ratio := float64(w) / float64(h)
	best := ""
	bestDiff := math.MaxFloat64
	for name, v := range candidates {
		if diff := math.Abs(ratio - v); diff < bestDiff {
			bestDiff = diff
			best = name
		}
	}
	if bestDiff > 0.25 {
		return "adaptive", true
	}
	return best, true
}

// EstimateBilling 返回计费倍率：seconds（时长）+ size（分辨率倍率）。
// 框架用 ModelPrice × seconds × size 计算配额。
// 基准价为 480P（size=1.0），720P 时 size≈1.955。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolution := a.resolveResolution(req)
	sizeRatio := resolutionSizeRatio[resolution]
	if sizeRatio == 0 {
		sizeRatio = 1.0
	}
	return map[string]float64{
		"seconds": float64(a.resolveDuration(req)),
		"size":    sizeRatio,
	}
}

// AdjustBillingOnSubmit 提交后无需调整计费倍率
func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete 任务终态时无需追加/退还
func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	// DashScope 使用标准 Bearer 鉴权 + 异步标志头
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("X-DashScope-Async", "enable")
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("field prompt is required")
	}

	// 从请求参数解析分辨率（不再依赖模型名硬编码）
	resolution := a.resolveResolution(req)

	body := submitRequest{
		Model: upstreamModel,
		Input: submitInput{Prompt: req.Prompt},
		Parameters: submitParams{
			Resolution: resolution,
			Ratio:      resolveRatio(req),
			Duration:   a.resolveDuration(req),
		},
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(encoded), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// 2xx 统一归一成 200，避免框架 relay_task.go 仅认 200 而把已提交任务误判为 fail_to_fetch_task。
	if resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.StatusCode != http.StatusOK {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var dResp submitResponse
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	// 提交失败（含鉴权/参数错误）：DashScope 返回 {"code":"InvalidParameter","message":"..."}
	if dResp.Code != "" {
		msg := dResp.Message
		if msg == "" {
			msg = "DashScope submit failed"
		}
		taskErr = service.TaskErrorWrapper(fmt.Errorf("dashscope error: %s", msg), "upstream_error", resp.StatusCode)
		return
	}

	upstreamID := dResp.Output.TaskID
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("dashscope returned empty task_id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	// 解析实际使用的分辨率（用于响应和日志）
	resolution := a.resolution
	if resolution == "" {
		resolution = defaultResolution
	}

	// 以 OpenAI video 风格返回，保持与其他视频通道一致的客户端体验
	now := time.Now().Unix()
	out := gin.H{
		"id":         info.PublicTaskID,
		"object":     "video",
		"model":      info.OriginModelName,
		"status":     "queued",
		"progress":   0,
		"created_at": now,
		"resolution": resolution,
		"seconds":    strconv.Itoa(a.resolveDurationForResponse(c)),
	}
	taskData, _ = common.Marshal(out)
	c.JSON(http.StatusOK, out)
	return upstreamID, taskData, nil
}

func (a *TaskAdaptor) resolveDurationForResponse(c *gin.Context) int {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return defaultDuration
	}
	return a.resolveDuration(req)
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/api/v1/tasks/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res pollResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{Code: 0}

	// 上游错误（鉴权/配额等）：DashScope 返回 {"code":...,"message":...}
	if res.Code != "" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Message
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
		return taskResult, nil
	}

	switch strings.ToUpper(res.Output.TaskStatus) {
	case "PENDING", "QUEUED", "SUBMITTING":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		// 优先取 video_url 直链，缺省时回退到 results[0].url
		url := res.Output.VideoURL
		if url == "" && len(res.Output.Results) > 0 {
			url = res.Output.Results[0].URL
		}
		if url != "" {
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Progress = taskcommon.ProgressComplete
			// 存进 ti.Url，框架自动写入 task.PrivateData.ResultURL，
			// 由 controller/video_proxy.go 的 default 分支转发给客户端。
			taskResult.Url = url
		} else {
			// 已成功但成片尚未转存，继续轮询
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = taskcommon.ProgressInProgress
		}
	case "FAILED", "FAILURE", "CANCELED", "CANCELLED":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Message
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
		return taskResult, nil
	default:
		// 未知状态按进行中处理，避免误判为失败
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	}

	return taskResult, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ConvertToOpenAIVideo 基于任务状态重建 OpenAI video 对象返回给客户端。
// 注意：task.Data 在轮询成功后会被框架覆盖为上游原始响应（DashScope 格式），
// 不能直接拿来当 OpenAI video 用；URL/时长等从上游 Data 里解析，状态/进度用任务自身字段。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var res pollResponse
	if err := common.Unmarshal(originTask.Data, &res); err != nil {
		// Data 结构异常时兜底：至少给出任务级状态
		openAIVideo := dto.NewOpenAIVideo()
		openAIVideo.ID = originTask.TaskID
		openAIVideo.Model = originTask.Properties.OriginModelName
		openAIVideo.Status = originTask.Status.ToVideoStatus()
		openAIVideo.SetProgressStr(originTask.Progress)
		openAIVideo.CreatedAt = originTask.CreatedAt
		return common.Marshal(openAIVideo)
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Model = originTask.Properties.OriginModelName
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	if originTask.FinishTime > 0 {
		openAIVideo.CompletedAt = originTask.FinishTime
	}

	// 成功成片 URL 放 metadata.url（与 kling 一致），video_proxy 仍以 PrivateData.ResultURL 为准
	if url := res.Output.VideoURL; url != "" {
		openAIVideo.SetMetadata("url", url)
	} else if len(res.Output.Results) > 0 {
		openAIVideo.SetMetadata("url", res.Output.Results[0].URL)
	}

	if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{Message: originTask.FailReason}
	}

	return common.Marshal(openAIVideo)
}
