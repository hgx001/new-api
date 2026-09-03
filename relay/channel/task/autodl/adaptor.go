package autodl

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// submitRequest 提交生成任务的请求体（autodl ComfyUI 自定义协议，非 OpenAI 格式）。
// Images 由 BuildRequestBody 按工作流配置展开为 ref_image_0..N 字段，不直接序列化。
type submitRequest struct {
	Prompt     string   `json:"prompt"`
	Duration   int      `json:"duration"`
	Resolution string   `json:"resolution"`
	Seed       *int64   `json:"seed,omitempty"`
	Images     []string `json:"-"`
}

// submitResponse 提交响应：{"code":"Success","data":{"task_id":...},"msg":"","request_id":"..."}
type submitResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID   string `json:"task_id"`
		Status   string `json:"status"`
		Workflow string `json:"workflow"`
		Message  string `json:"message"`
	} `json:"data"`
}

// pollResponse 轮询响应：{"code":"Success","data":{"status":...,"results":[{type,url,...}]}}
type pollResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		TaskID   string       `json:"task_id"`
		Status   string       `json:"status"`
		Duration int          `json:"duration"`
		Results  []resultItem `json:"results"`
		Message  string       `json:"message"`
	} `json:"data"`
}

// resultItem results 数组中的元素（autodl 返回的是对象，url 字段才是真实资源地址）
type resultItem struct {
	Type     string `json:"type"`
	URL      string `json:"url"`
	FileType string `json:"file_type"`
	Alias    string `json:"alias"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	wf          workflowConfig
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	if a.baseURL == "" {
		a.baseURL = baseURLDefault
	}
	a.apiKey = info.ApiKey
	if cfg, ok := workflowByModel[info.OriginModelName]; ok {
		a.wf = cfg
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

// resolveDuration 解析时长并钳制到 1 秒 ~ 当前工作流允许的最大时长
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
	max := maxDuration
	if a.wf.MaxDuration > 0 {
		max = a.wf.MaxDuration
	}
	if d > max {
		return max
	}
	return d
}

// EstimateBilling 按秒和分辨率计费：框架用 ModelPrice × seconds × size 计算配额。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolution := a.wf.Resolution
	if requestedResolution := strings.TrimSpace(req.Resolution); requestedResolution != "" {
		resolution = requestedResolution
	} else if req.Size != "" {
		if derived, ok := deriveResolutionFromSize(req.Size, a.wf.Resolutions); ok {
			resolution = derived
		}
	}
	ratio := a.wf.ResolutionRatios[resolution]
	if ratio <= 0 {
		ratio = 1.0
	}
	return map[string]float64{
		"seconds": float64(a.resolveDuration(req)),
		"size":    ratio,
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
	if a.wf.WorkflowID == "" {
		return "", errors.Errorf("model %s is not mapped to an AutoDL workflow_id", info.OriginModelName)
	}
	return fmt.Sprintf("%s/api/v1/comfyui/comfyui_workflow/%s", a.baseURL, a.wf.WorkflowID), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	// autodl ComfyUI API 使用原始 token 鉴权（Authorization: <token>），不带 Bearer 前缀
	req.Header.Set("Authorization", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	if a.wf.WorkflowID == "" {
		return nil, errors.Errorf("model %s is not mapped to an AutoDL workflow_id", info.OriginModelName)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("field prompt is required")
	}
	if max := a.wf.MaxPromptLength; max > 0 && utf8.RuneCountInString(prompt) > max {
		return nil, errors.Errorf("model %s supports prompts up to %d characters", info.OriginModelName, max)
	}

	resolution := a.wf.Resolution
	if requestedResolution := strings.TrimSpace(req.Resolution); requestedResolution != "" {
		validResolution := false
		for _, allowed := range a.wf.Resolutions {
			if requestedResolution == allowed {
				validResolution = true
				break
			}
		}
		if !validResolution {
			return nil, errors.Errorf("model %s does not support resolution %q", info.OriginModelName, requestedResolution)
		}
		resolution = requestedResolution
	} else if req.Size != "" {
		if derived, ok := deriveResolutionFromSize(req.Size, a.wf.Resolutions); ok {
			resolution = derived
		}
	}

	if req.Seed != nil {
		if a.wf.MaxSeed <= 0 {
			return nil, errors.Errorf("model %s does not support seed", info.OriginModelName)
		}
		if *req.Seed < 1 || *req.Seed > a.wf.MaxSeed {
			return nil, errors.Errorf("model %s seed must be between 1 and %d", info.OriginModelName, a.wf.MaxSeed)
		}
	}

	body := map[string]interface{}{
		"prompt":     req.Prompt,
		"duration":   a.resolveDuration(req),
		"resolution": resolution,
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	// 多参考图：autodl 多图工作流用 ref_image_0..ref_image_8 独立字段（首张必填），
	// 把客户端的 images 数组按下标展开注入。单图工作流不应静默丢弃图片，
	// 超出工作流上限时也要明确报错，避免客户端误以为所有图片都已提交。
	images := make([]string, 0, len(req.Images))
	for _, image := range req.Images {
		if image = strings.TrimSpace(image); image != "" {
			images = append(images, image)
		}
	}
	if !a.wf.RequiresImages {
		if len(images) > 0 {
			return nil, errors.Errorf("model %s does not support reference images", info.OriginModelName)
		}
	} else {
		if len(images) == 0 {
			return nil, errors.Errorf("model %s requires at least one reference image (images field)", info.OriginModelName)
		}
		if max := a.wf.MaxImages; max > 0 && len(images) > max {
			return nil, errors.Errorf("model %s supports at most %d reference images", info.OriginModelName, max)
		}
		for i, image := range images {
			body[fmt.Sprintf("ref_image_%d", i)] = image
		}
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
	// autodl 提交成功返回 200；这里把 2xx 统一归一成 200，
	// 避免框架 relay_task.go 仅认 200 而把已提交任务误判为 fail_to_fetch_task。
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

	// 提交失败（含鉴权错误）：autodl 返回 code != "Success"
	if dResp.Code != "Success" {
		msg := dResp.Msg
		if msg == "" {
			msg = dResp.Data.Message
		}
		if msg == "" {
			msg = "AutoDL submit failed"
		}
		taskErr = service.TaskErrorWrapper(fmt.Errorf("autodl error: %s", msg), "upstream_error", resp.StatusCode)
		return
	}

	upstreamID := dResp.Data.TaskID
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("autodl returned empty task_id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
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
		"size":       a.wf.Resolution,
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
	uri := fmt.Sprintf("%s/api/v1/comfyui/comfyui_workflow/result/%s", baseUrl, taskID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", key)
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

	switch strings.ToUpper(res.Code) {
	case "FAIL", "FAILED", "FAILURE", "CANCELLED", "CANCELED":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Data.Message
		if taskResult.Reason == "" {
			taskResult.Reason = res.Msg
		}
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
		return taskResult, nil
	}

	// code 非 Success 也视为上游报错（如鉴权/配额问题）
	if res.Code != "" && res.Code != "Success" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Msg
		if taskResult.Reason == "" {
			taskResult.Reason = res.Data.Message
		}
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
		return taskResult, nil
	}

	switch strings.ToUpper(res.Data.Status) {
	case "SUBMITTING", "QUEUED", "PENDING":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "STORING", "PROCESSING", "IN_PROGRESS":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		if len(res.Data.Results) > 0 {
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Progress = taskcommon.ProgressComplete
			// 把第一个结果的 url 存进 ti.Url，框架会自动写入 task.PrivateData.ResultURL，
			// 由 controller/video_proxy.go 的 default 分支转发给客户端。
			taskResult.Url = res.Data.Results[0].URL
		} else {
			// 已成功但成片尚未转存，继续轮询
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = taskcommon.ProgressInProgress
		}
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

// deriveResolutionFromSize 从 OpenAI 格式的 size（如 "768x1280"）推导 AutoDL 格式的
// resolution（如 "768p竖"）。当客户端通过 OpenAI 兼容协议 /v1/videos 调用 autodl 模型
// 但未传 resolution 字段时，用此函数从 size 推导，避免 resolution 永远落默认值。
//
// 映射规则：短边 → 分辨率档（480p/768p/1080p/736p），宽高比 → 方向后缀（竖/横/(1:1)）。
// 推导出的 resolution 必须在 allowedResolutions 列表中；如果精确组合不在列表中
// （如 720p 短边映射到 768p 档），选最近档位 + 同后缀；同后缀也没有则选最近档位任意后缀。
func deriveResolutionFromSize(size string, allowedResolutions []string) (string, bool) {
	w, h, ok := parseSize(size)
	if !ok {
		return "", false
	}

	var suffix string
	if w == h {
		suffix = "(1:1)"
	} else if h > w {
		suffix = "竖"
	} else {
		suffix = "横"
	}

	short := w
	if h < short {
		short = h
	}

	best := ""
	bestDiff := -1
	for _, allowed := range allowedResolutions {
		if !strings.HasSuffix(allowed, suffix) {
			continue
		}
		tier := extractTierValue(allowed)
		if tier == 0 {
			continue
		}
		diff := tier - short
		if diff < 0 {
			diff = -diff
		}
		if bestDiff < 0 || diff < bestDiff {
			bestDiff = diff
			best = allowed
		}
	}
	if best != "" {
		return best, true
	}

	for _, allowed := range allowedResolutions {
		tier := extractTierValue(allowed)
		if tier == 0 {
			continue
		}
		diff := tier - short
		if diff < 0 {
			diff = -diff
		}
		if bestDiff < 0 || diff < bestDiff {
			bestDiff = diff
			best = allowed
		}
	}
	return best, best != ""
}

func parseSize(size string) (int, int, bool) {
	parts := strings.SplitN(size, "x", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, errW := strconv.Atoi(parts[0])
	h, errH := strconv.Atoi(parts[1])
	if errW != nil || errH != nil {
		return 0, 0, false
	}
	return w, h, true
}

// extractTierValue 从 AutoDL resolution token（如 "768p竖"）提取分辨率数值（768）。
func extractTierValue(resolution string) int {
	s := resolution
	for _, suffix := range []string{"(1:1)", "竖", "横"} {
		s = strings.TrimSuffix(s, suffix)
	}
	s = strings.TrimSuffix(s, "p")
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ConvertToOpenAIVideo 把任务数据转换成 OpenAI video 对象返回给客户端。
// 轮询同步后 task.Data 是 redactVideoResponseBody 的轮询响应体
// （{"code":"Success","data":{"status":"SUCCEEDED","results":[{url...}]}}) ，
// 这里解析它并映射成 OpenAI video 的 status/progress/metadata.url，
// 否则客户端（如 OpenAI SDK videos.retrieve）永远看到 queued 无法收敛。
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res pollResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal AutoDL task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	switch strings.ToUpper(res.Code) {
	case "FAIL", "FAILED", "FAILURE", "CANCELLED", "CANCELED":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    res.Code,
			Message: firstNonEmpty(res.Data.Message, res.Msg, "AutoDL task failed"),
		}
		return common.Marshal(openAIResp)
	}
	if res.Code != "" && res.Code != "Success" {
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    res.Code,
			Message: firstNonEmpty(res.Msg, res.Data.Message, "AutoDL task failed"),
		}
		return common.Marshal(openAIResp)
	}

	switch strings.ToUpper(res.Data.Status) {
	case "SUBMITTING", "QUEUED", "PENDING":
		openAIResp.Status = dto.VideoStatusQueued
	case "RUNNING", "STORING", "PROCESSING", "IN_PROGRESS":
		openAIResp.Status = dto.VideoStatusInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		openAIResp.Status = dto.VideoStatusCompleted
		if len(res.Data.Results) > 0 && strings.TrimSpace(res.Data.Results[0].URL) != "" {
			openAIResp.SetMetadata("url", res.Data.Results[0].URL)
		}
	default:
		openAIResp.Status = dto.VideoStatusUnknown
	}
	return common.Marshal(openAIResp)
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
