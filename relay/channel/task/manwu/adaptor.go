package manwu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Constants
// ============================

const (
	ModelName       = "dola-seedance-2.5"
	ChannelName     = "manwu"
	PlatformID      = "dola"
	OutputModeVideo = "video"
	InputTypeImage  = "reference_image"

	// baseURLDefault 为渠道未配置 BaseURL 时的兜底（ArcReel 生产地址）
	baseURLDefault = "https://arcreel.heibaidao.cn"

	submitPath = "/api/v1/remote-generation/jobs"

	defaultRatio = "16:9"
	// defaultDuration 与上游兜底口径对齐：ArcReel 对未指定/非法时长会回落 30 秒，
	// 这里缺省显式传 30，保证预扣计费与上游实际生成时长一致。
	defaultDuration = 30

	maxReferenceImages = 2

	idempotencyKeyPrefix = "manwu-"

	normalizedRequestKey = "manwu_request"

	reasonCancelled = "任务已取消"
)

var allowedDurations = map[int]bool{
	5:  true,
	10: true,
	15: true,
	30: true,
}

var allowedRatios = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// ============================
// Request / Response structures
// ============================

// clientRequest 是厂商侧请求体。seconds/duration/input_reference/images 用
// json.RawMessage 承接，以便宽容解析数字/字符串、单值/数组等别名形态。
type clientRequest struct {
	Model          string          `json:"model"`
	Prompt         string          `json:"prompt"`
	Seconds        json.RawMessage `json:"seconds"`
	Duration       json.RawMessage `json:"duration"`
	Size           string          `json:"size"`
	Ratio          string          `json:"ratio"`
	AspectRatio    string          `json:"aspect_ratio"`
	InputReference json.RawMessage `json:"input_reference"`
	Images         json.RawMessage `json:"images"`
	IdempotencyKey string          `json:"idempotency_key"`
}

// normalizedRequest 是校验通过后缓存在 gin.Context 中的规范化请求，
// 供 EstimateBilling / BuildRequestBody 复用（避免重复解析与二次校验）。
type normalizedRequest struct {
	Prompt         string
	Duration       int
	Ratio          string
	Images         []string
	IdempotencyKey string
}

// submitRequest 是 ArcReel 建单载荷。
type submitRequest struct {
	PlatformId     string      `json:"platformId"`
	OutputMode     string      `json:"outputMode"`
	Prompt         string      `json:"prompt"`
	VideoParams    videoParams `json:"videoParams"`
	Inputs         []jobInput  `json:"inputs,omitempty"`
	IdempotencyKey string      `json:"idempotencyKey"`
}

type videoParams struct {
	Ratio    string `json:"ratio"`
	Duration *int   `json:"duration,omitempty"`
}

type jobInput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// submitResponse 是 ArcReel 建单响应，宽容解析 jobId / id 两种字段名。
type submitResponse struct {
	JobID  string `json:"jobId"`
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// jobResponse 是 ArcReel 轮询响应，宽容解析 sourceUrl / source_url。
type jobResponse struct {
	JobID        string `json:"jobId"`
	Status       string `json:"status"`
	SourceURL    string `json:"sourceUrl"`
	SourceURLAlt string `json:"source_url"`
	Error        string `json:"error"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	if strings.TrimSpace(a.baseURL) == "" {
		a.baseURL = baseURLDefault
	}
}

// ValidateRequestAndSetAction 解析厂商请求并做白名单校验。
// 注意：ArcReel 只接受 http(s) 参考图 URL，不支持 multipart 文件上传，
// 因此不走 ValidateBasicTaskRequest 的共享解析（其 seconds 字段仅支持字符串形态）。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	var req clientRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	if strings.TrimSpace(req.Model) != ModelName {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model must be %s", ModelName), "invalid_model", http.StatusBadRequest)
	}

	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	duration, err := resolveDuration(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}

	ratio, err := resolveRatio(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_ratio", http.StatusBadRequest)
	}

	images, err := resolveReferenceImages(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}

	idempotencyKey := strings.TrimSpace(req.IdempotencyKey)
	if idempotencyKey == "" {
		idempotencyKey = idempotencyKeyPrefix + common.GetUUID()
	}

	c.Set(normalizedRequestKey, &normalizedRequest{
		Prompt:         prompt,
		Duration:       duration,
		Ratio:          ratio,
		Images:         images,
		IdempotencyKey: idempotencyKey,
	})
	info.Action = constant.TaskActionGenerate
	return nil
}

// EstimateBilling 返回计费倍率：seconds（时长）。框架用 ModelPrice × seconds 计算配额。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if c == nil {
		return nil
	}
	req, err := getNormalizedRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{
		"seconds": float64(req.Duration),
	}
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", strings.TrimRight(a.baseURL, "/"), submitPath), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// BuildRequestBody converts the normalized request into the ArcReel job payload.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := getNormalizedRequest(c)
	if err != nil {
		return nil, err
	}

	inputs := make([]jobInput, 0, len(req.Images))
	for _, image := range req.Images {
		inputs = append(inputs, jobInput{Type: InputTypeImage, URL: image})
	}

	body := submitRequest{
		PlatformId: PlatformID,
		OutputMode: OutputModeVideo,
		Prompt:     req.Prompt,
		VideoParams: videoParams{
			Ratio:    req.Ratio,
			Duration: &req.Duration,
		},
		Inputs:         inputs,
		IdempotencyKey: req.IdempotencyKey,
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(data), nil
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var submitResp submitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	taskID = firstNonEmpty(submitResp.JobID, submitResp.ID)
	if taskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("arcreel returned empty jobId, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return taskID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s%s/%s", strings.TrimRight(baseUrl, "/"), submitPath, url.PathEscape(taskID))
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{ModelName}
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult maps the ArcReel job status to the unified TaskInfo.
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res jobResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "assigned", "accepted", "submitting", "provider_running", "provider_succeeded", "url_validating":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "ready":
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Url = firstNonEmpty(res.SourceURL, res.SourceURLAlt)
	case "failed", "url_unavailable":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = res.Error
	case "cancelled":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = reasonCancelled
	default:
		// "unknown" 及无法识别的状态
		taskResult.Status = model.TaskStatusUnknown
	}
	return taskResult, nil
}

// ConvertToOpenAIVideo 把轮询同步后的 task.Data（ArcReel jobResponse 原始 JSON）
// 映射成 OpenAI video 查询响应；否则客户端永远看到 queued 无法收敛。
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res jobResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal manwu task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt

	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "ready":
		openAIResp.Status = dto.VideoStatusCompleted
		if url := firstNonEmpty(res.SourceURL, res.SourceURLAlt); url != "" {
			openAIResp.SetMetadata("url", url)
		}
	case "failed", "url_unavailable":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    strings.ToLower(strings.TrimSpace(res.Status)),
			Message: firstNonEmpty(res.Error, taskcommon.ReasonContentModeration),
		}
	case "cancelled":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{Code: "cancelled", Message: reasonCancelled}
	case "queued":
		openAIResp.Status = dto.VideoStatusQueued
	case "assigned", "accepted", "submitting", "provider_running", "provider_succeeded", "url_validating":
		openAIResp.Status = dto.VideoStatusInProgress
	default:
		openAIResp.Status = dto.VideoStatusUnknown
	}
	return common.Marshal(openAIResp)
}

// ============================
// helpers
// ============================

func getNormalizedRequest(c *gin.Context) (*normalizedRequest, error) {
	v, exists := c.Get(normalizedRequestKey)
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(*normalizedRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}
	return req, nil
}

// resolveDuration 解析时长：seconds（首选，数字或数字字符串）→ duration → 缺省 30。
// 白名单 5/10/15/30，缺省或 null 视为未指定；字段存在但非法（格式错误或不在白名单）
// 直接报错——上游 ArcReel 对非法值会静默回落 30，厂商侧必须显式拒绝，
// 避免计费时长与实际生成时长脱节。
func resolveDuration(req clientRequest) (int, error) {
	seconds, found, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	if found {
		if !allowedDurations[seconds] {
			return 0, fmt.Errorf("seconds must be one of 5/10/15/30, got %d", seconds)
		}
		return seconds, nil
	}

	duration, found, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if found {
		if !allowedDurations[duration] {
			return 0, fmt.Errorf("duration must be one of 5/10/15/30, got %d", duration)
		}
		return duration, nil
	}

	return defaultDuration, nil
}

// resolveRatio 解析画面比例：size（首选）→ ratio → aspect_ratio → 缺省 16:9。
func resolveRatio(req clientRequest) (string, error) {
	candidate := strings.TrimSpace(req.Size)
	if candidate == "" {
		candidate = strings.TrimSpace(req.Ratio)
	}
	if candidate == "" {
		candidate = strings.TrimSpace(req.AspectRatio)
	}
	if candidate == "" {
		return defaultRatio, nil
	}
	if !allowedRatios[candidate] {
		return "", fmt.Errorf("ratio must be one of 16:9/9:16/1:1/4:3/3:4/21:9, got %q", candidate)
	}
	return candidate, nil
}

// resolveReferenceImages 解析参考图：input_reference（首选）→ images。
// dola 最多 2 张参考图，且每张必须是 http(s) URL。
func resolveReferenceImages(req clientRequest) ([]string, error) {
	images := parseStringList(req.InputReference)
	if len(images) == 0 {
		images = parseStringList(req.Images)
	}
	if len(images) > maxReferenceImages {
		return nil, fmt.Errorf("dola accepts at most %d reference images, got %d", maxReferenceImages, len(images))
	}
	for _, image := range images {
		u, err := url.Parse(strings.TrimSpace(image))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("reference image must be an http(s) URL, got %q", image)
		}
	}
	return images, nil
}

// parseFlexibleInt 兼容数字与数字字符串两种 JSON 形态。
// found=false 表示字段缺省或 null（视为未指定）；
// 字段存在但格式非法（非数字、浮点、非数字字符串）时返回错误。
func parseFlexibleInt(raw json.RawMessage) (value int, found bool, err error) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false, nil
	}
	var num *int
	if uerr := common.Unmarshal(raw, &num); uerr == nil {
		if num != nil {
			return *num, true, nil
		}
		return 0, false, nil
	}
	var text string
	if uerr := common.Unmarshal(raw, &text); uerr == nil {
		if v, aerr := strconv.Atoi(strings.TrimSpace(text)); aerr == nil {
			return v, true, nil
		}
	}
	return 0, false, fmt.Errorf("is not a valid integer")
}

// parseStringList 兼容字符串与字符串数组两种 JSON 形态
// （与 relaycommon 对 input_reference/images 的宽容解析一致）。
func parseStringList(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var values []string
	if err := common.Unmarshal(raw, &values); err == nil {
		return values
	}
	var value string
	if err := common.Unmarshal(raw, &value); err == nil && value != "" {
		return []string{value}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
