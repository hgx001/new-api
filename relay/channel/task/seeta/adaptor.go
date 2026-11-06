package seeta

import (
	"bytes"
	"context"
	"encoding/base64"
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
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response structures
// ============================

// submitResponse 提交响应。
//
// ⚠️ 上游**不包 code/msg 外层**（与 autodl.art 不同），成功时形如：
//
//	{"data":{"task_id":"f9e5b054..."}}
//
// 失败时形如：
//
//	{"error":{"message":"只接受 data URI","type":"invalid_request_error"}}
//
// 所以这里必须同时容纳 data 与 error 两个分支，不能只看 code 字段。
type submitResponse struct {
	Data struct {
		TaskID string `json:"task_id"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// pollResponse 轮询响应：
//
//	{"data":{"task_id":"...","status":"PENDING|RUNNING|SUCCESS|FAILED","message":"","results":[...]}}
type pollResponse struct {
	Data struct {
		TaskID string       `json:"task_id"`
		Status string       `json:"status"`
		Message string      `json:"message"`
		Results []resultItem `json:"results"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// resultItem results 数组中的元素（url 字段才是真实资源地址）。
type resultItem struct {
	URL      string `json:"url"`
	Filename string `json:"filename"`
	Subfolder string `json:"subfolder"`
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
	a.baseURL = strings.TrimSpace(info.ChannelBaseUrl)
	if a.baseURL == "" {
		a.baseURL = baseURLDefault
	}
	a.apiKey = info.ApiKey
	if cfg, ok := workflowByModel[info.OriginModelName]; ok {
		a.wf = cfg
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirectWithPrompt(c, info, true)
}

// resolveDuration 解析时长并钳制到 1 秒 ~ 当前工作流允许的最大时长。
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

// resolveResolution 解析分辨率档位。
//
// 客户端可以直接传上游 token（"768p横"），也可以传 OpenAI 风格的 size
// （"1280x720"）——后者按短边推档、按宽高比推方向。
func (a *TaskAdaptor) resolveResolution(req relaycommon.TaskSubmitReq) (string, error) {
	if requested := strings.TrimSpace(req.Resolution); requested != "" {
		for _, allowed := range a.wf.Resolutions {
			if requested == allowed {
				return requested, nil
			}
		}
		return "", errors.Errorf("model %s does not support resolution %q", a.originModelName(), requested)
	}
	if req.Size != "" {
		if derived, ok := deriveResolutionFromSize(req.Size, a.wf.Resolutions); ok {
			return derived, nil
		}
	}
	return a.wf.Resolution, nil
}

// originModelName 返回对外模型名，供错误信息使用。
func (a *TaskAdaptor) originModelName() string {
	for name, cfg := range workflowByModel {
		if cfg.WorkflowID == a.wf.WorkflowID {
			return name
		}
	}
	return "seeta model"
}

// EstimateBilling 按秒和分辨率计费：框架用 ModelPrice × seconds × size 计算配额。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolution, err := a.resolveResolution(req)
	if err != nil {
		resolution = a.wf.Resolution
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
		return "", errors.Errorf("model %s is not mapped to a SeetaCloud workflow", info.OriginModelName)
	}
	return fmt.Sprintf("%s/api/v1/comfyui/comfyui_workflow/%s", a.baseURL, a.wf.WorkflowID), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	// 上游 ComfyUI 协议用原始 token 鉴权（Authorization: <token>）。
	// 实测带 "Bearer " 前缀也能通过，但裸 token 是该协议的原生写法。
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
		return nil, errors.Errorf("model %s is not mapped to a SeetaCloud workflow", info.OriginModelName)
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, errors.New("field prompt is required")
	}
	if max := a.wf.MaxPromptLength; max > 0 && utf8.RuneCountInString(prompt) > max {
		return nil, errors.Errorf("model %s supports prompts up to %d characters", info.OriginModelName, max)
	}

	resolution, err := a.resolveResolution(req)
	if err != nil {
		return nil, err
	}

	if req.Seed != nil && !a.wf.SupportsSeed {
		return nil, errors.Errorf("model %s does not support seed", info.OriginModelName)
	}

	body := map[string]interface{}{
		"resolution": resolution,
		"prompt":     req.Prompt,
		"duration":   a.resolveDuration(req),
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}

	// 参考图：上游**只收 data URI**，传 URL 会 400「只接受 data URI」。
	// 所以客户给的是 http(s) 链接时，网关要先把图下载下来转成 data URI。
	images := collectReferenceImages(req)
	if len(images) > maxReferenceImageCount {
		return nil, errors.Errorf("model %s supports at most %d reference images", info.OriginModelName, maxReferenceImageCount)
	}
	if !a.wf.RequiresImages && len(images) > 0 {
		return nil, errors.Errorf("model %s does not support reference images", info.OriginModelName)
	}
	if a.wf.RequiresImages && len(images) == 0 {
		return nil, errors.Errorf("model %s requires at least one reference image (images field)", info.OriginModelName)
	}
	for i, image := range images {
		dataURI, err := toDataURI(c, image)
		if err != nil {
			return nil, errors.Wrapf(err, "reference image %d", i+1)
		}
		// 上游对图生视频/首尾帧工作流识别 first_frame 或 ref_image_0，
		// 多图时按 ref_image_N 递增下发（ref_image_0 即首帧）。
		if i == 0 {
			body["first_frame"] = dataURI
		}
		body[fmt.Sprintf("ref_image_%d", i)] = dataURI
	}

	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(encoded), nil
}

// collectReferenceImages 从请求里收集参考图，兼容 images / image /
// input_reference / media 四种入口。
func collectReferenceImages(req relaycommon.TaskSubmitReq) []string {
	images := make([]string, 0, len(req.Images))
	for _, image := range req.Images {
		if image = strings.TrimSpace(image); image != "" {
			images = append(images, image)
		}
	}
	if len(images) == 0 {
		if image := strings.TrimSpace(req.InputReference); image != "" {
			images = append(images, image)
		} else if image := strings.TrimSpace(req.Image); image != "" {
			images = append(images, image)
		}
	}
	for _, media := range req.Media {
		if strings.Contains(strings.ToLower(media.Type), "image") {
			if image := strings.TrimSpace(media.URL); image != "" {
				images = append(images, image)
			}
		}
	}
	return images
}

// toDataURI 把参考图转成 data URI。
//
// 上游只接受 data URI，所以 http(s) 链接必须先下载再内联。
// 已经内联的 data URI 直接放行（仍要过体积与格式校验）。
func toDataURI(c *gin.Context, raw string) (string, error) {
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		if err := validateDataURI(raw); err != nil {
			return "", err
		}
		return raw, nil
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "", errors.Errorf("reference image must be an http(s) URL or a data URI, got %q", truncateForError(raw))
	}
	data, mimeType, err := downloadImage(c, raw)
	if err != nil {
		return "", err
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// validateDataURI 校验内联 data URI：格式与体积都要过。
func validateDataURI(raw string) error {
	comma := strings.Index(raw, ",")
	if comma < 0 {
		return errors.New("malformed data URI")
	}
	header := strings.ToLower(raw[:comma])
	if !strings.HasPrefix(header, "data:image/") {
		return errors.New("data URI is not an image")
	}
	payload := raw[comma+1:]
	if len(payload) > maxReferenceImageBytes*4/3+4 {
		return errors.Errorf("image exceeds the %d byte limit", maxReferenceImageBytes)
	}
	return nil
}

// downloadReferenceTimeout 单图下载超时。
//
// 建单请求是同步的，不能因为客户给了一个慢图床就无限期占着连接。
const downloadReferenceTimeout = 30 * time.Second

// downloadImage 下载一张参考图，返回字节与 MIME。
func downloadImage(c *gin.Context, rawURL string) ([]byte, string, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode,
		fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", errors.Wrap(err, "URL blocked")
	}

	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		client = http.DefaultClient
	}

	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	downloadCtx, cancel := context.WithTimeout(ctx, downloadReferenceTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", errors.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxReferenceImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxReferenceImageBytes {
		return nil, "", errors.Errorf("image exceeds the %d byte limit", maxReferenceImageBytes)
	}

	mimeType := response.Header.Get("Content-Type")
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = mimeType[:index]
	}
	mimeType = strings.TrimSpace(mimeType)
	// 对象存储常返回 application/octet-stream，这时靠魔数兜底。
	if mimeType == "" || mimeType == "application/octet-stream" || mimeType == "binary/octet-stream" {
		mimeType = detectImageMime(data)
	}
	if !strings.HasPrefix(strings.ToLower(mimeType), "image/") {
		return nil, "", errors.Errorf("reference image content type %q is not an image", mimeType)
	}
	return data, mimeType, nil
}

// detectImageMime 用文件头判断图片类型。
func detectImageMime(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 6 && data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return "image/gif"
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "image/bmp"
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	}
	return "image/png"
}

// truncateForError 截断长串，避免把整段 base64 打进错误信息。
func truncateForError(s string) string {
	if len(s) <= 64 {
		return s
	}
	return s[:64] + "..."
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// 提交成功返回 200；这里把 2xx 统一归一成 200，
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

	// 上游的错误走 error 分支（HTTP 4xx），成功走 data.task_id。
	if dResp.Error != nil {
		msg := dResp.Error.Message
		if msg == "" {
			msg = "SeetaCloud submit failed"
		}
		taskErr = service.TaskErrorWrapper(fmt.Errorf("seeta error: %s", msg), "upstream_error", resp.StatusCode)
		return
	}

	upstreamID := dResp.Data.TaskID
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("seeta returned empty task_id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	// 以 OpenAI video 风格返回，保持与其他视频通道一致的客户端体验
	now := time.Now().Unix()
	out := gin.H{
		"id":         info.PublicTaskID,
		"object":     "video",
		"model":      info.PublicModelName(),
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

	if res.Error != nil {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Error.Message
		if taskResult.Reason == "" {
			taskResult.Reason = taskcommon.ReasonContentModeration
		}
		return taskResult, nil
	}

	switch strings.ToUpper(res.Data.Status) {
	case "FAILED", "FAIL", "FAILURE", "CANCELLED", "CANCELED", "ERROR":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.Data.Message
		if taskResult.Reason == "" {
			taskResult.Reason = taskcommon.ReasonContentModeration
		}
		return taskResult, nil
	case "PENDING", "SUBMITTING", "QUEUED":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS", "STORING":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
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

// deriveResolutionFromSize 从 OpenAI 格式的 size（如 "1280x720"）推导上游格式的
// resolution（如 "768p横"）。
//
// 映射规则：短边 → 分辨率档（480p/768p），宽高比 → 方向后缀（横/竖/(1:1)）。
// 推导出的档位必须在 allowedResolutions 列表中；同后缀没有精确匹配时，
// 选短边最接近的档位。
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

// extractTierValue 从 resolution token（如 "768p横"）提取分辨率数值（768）。
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
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res pollResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal SeetaCloud task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	if res.Error != nil {
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    res.Error.Type,
			Message: firstNonEmpty(res.Error.Message, taskcommon.ReasonContentModeration),
		}
		return common.Marshal(openAIResp)
	}

	switch strings.ToUpper(res.Data.Status) {
	case "FAILED", "FAIL", "FAILURE", "CANCELLED", "CANCELED", "ERROR":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    res.Data.Status,
			Message: firstNonEmpty(res.Data.Message, taskcommon.ReasonContentModeration),
		}
		return common.Marshal(openAIResp)
	case "PENDING", "SUBMITTING", "QUEUED":
		openAIResp.Status = dto.VideoStatusQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS", "STORING":
		openAIResp.Status = dto.VideoStatusInProgress
	case "SUCCESS", "SUCCEEDED", "COMPLETED":
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
