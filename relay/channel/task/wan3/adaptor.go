package wan3

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

// submitRequest is the native DashScope request body for Wan3.
type submitRequest struct {
	Model      string           `json:"model"`
	Input      submitInput      `json:"input"`
	Parameters submitParameters `json:"parameters"`
}

type submitInput struct {
	Prompt string                  `json:"prompt,omitempty"`
	Media  []relaycommon.TaskMedia `json:"media,omitempty"`
}

type submitParameters struct {
	Resolution   string `json:"resolution,omitempty"`
	Ratio        string `json:"ratio,omitempty"`
	Duration     int    `json:"duration,omitempty"`
	PromptExtend bool   `json:"prompt_extend"`
	Audio        bool   `json:"audio"`
	Watermark    bool   `json:"watermark"`
	Seed         *int64 `json:"seed,omitempty"`
}

type wan3Output struct {
	TaskID     string `json:"task_id"`
	TaskStatus string `json:"task_status"`
	VideoURL   string `json:"video_url,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
}

type submitResponse struct {
	RequestID string     `json:"request_id"`
	Output    wan3Output `json:"output"`
	Code      string     `json:"code,omitempty"`
	Message   string     `json:"message,omitempty"`
}

// taskResponse is the native DashScope response returned by GET /api/v1/tasks/{id}.
type taskResponse = submitResponse

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
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

// resolveDuration 解析时长并钳制到 wan3 允许的 2–30 秒
func resolveDuration(req relaycommon.TaskSubmitReq) int {
	d := req.Duration
	if d <= 0 {
		d, _ = strconv.Atoi(req.Seconds)
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

// resolveResolution 从请求参数解析分辨率。兼容 OpenAI 风格的 WxH/宽高尺寸，
// 最终只产生 Wan3 原生的 480P、720P 或 1080P。
func resolveResolution(req relaycommon.TaskSubmitReq) string {
	if r := normalizeResolution(req.Resolution); r != "" {
		return r
	}
	if req.Metadata != nil {
		if r, ok := req.Metadata["resolution"].(string); ok {
			if normalized := normalizeResolution(r); normalized != "" {
				return normalized
			}
		}
	}
	if r, ok := resolutionFromSize(req.Size); ok {
		return r
	}
	return defaultResolution
}

func normalizeResolution(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "480", "480P":
		return "480P"
	case "720", "720P":
		return "720P"
	case "1080", "1080P":
		return "1080P"
	default:
		return ""
	}
}

func resolutionFromSize(size string) (string, bool) {
	size = strings.ReplaceAll(strings.TrimSpace(size), "*", "x")
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	short := w
	if h < short {
		short = h
	}
	switch {
	case short <= 600:
		return "480P", true
	case short <= 900:
		return "720P", true
	default:
		return "1080P", true
	}
}

// resolveRatio 推断宽高比：优先 metadata 显式指定，其次由 size 推断，最后使用官方 adaptive。
func resolveRatio(req relaycommon.TaskSubmitReq) string {
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if r, ok := req.Metadata[key].(string); ok {
				r = strings.TrimSpace(r)
				if isValidRatio(r) {
					return r
				}
			}
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

// ratioFromSize 把 "1280x720" 或 "1280*720" 这类尺寸映射到 Wan3 ratio。
func ratioFromSize(size string) (string, bool) {
	size = strings.ReplaceAll(strings.TrimSpace(size), "*", "x")
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

func normalizeWan3Prompt(prompt string) string {
	for _, prefix := range []string{"图", "视频", "音频"} {
		for i := 1; i <= 20; i++ {
			prompt = strings.ReplaceAll(prompt, "@"+prefix+strconv.Itoa(i), prefix+strconv.Itoa(i))
		}
	}
	return prompt
}

func metadataBool(metadata map[string]interface{}, keys ...string) (bool, bool) {
	for _, key := range keys {
		value, ok := metadata[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed, true
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(typed))
			if err == nil {
				return parsed, true
			}
		}
	}
	return false, false
}

func metadataMedia(metadata map[string]interface{}) ([]relaycommon.TaskMedia, error) {
	if metadata == nil {
		return nil, nil
	}
	raw, ok := metadata["media"]
	if !ok {
		if input, inputOK := metadata["input"].(map[string]interface{}); inputOK {
			raw, ok = input["media"]
		}
	}
	if !ok || raw == nil {
		return nil, nil
	}
	encoded, err := common.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal metadata media failed")
	}
	var media []relaycommon.TaskMedia
	if err := common.Unmarshal(encoded, &media); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata media failed")
	}
	return media, nil
}

func inferLegacyMediaType(url string) string {
	lower := strings.ToLower(strings.TrimSpace(url))
	if strings.HasPrefix(lower, "data:video/") || strings.HasSuffix(strings.Split(lower, "?")[0], ".mp4") || strings.HasSuffix(strings.Split(lower, "?")[0], ".mov") {
		return "reference_video"
	}
	if strings.HasPrefix(lower, "data:audio/") || strings.HasSuffix(strings.Split(lower, "?")[0], ".mp3") || strings.HasSuffix(strings.Split(lower, "?")[0], ".wav") {
		return "reference_audio"
	}
	return "reference_image"
}

func buildWan3Media(req relaycommon.TaskSubmitReq) ([]relaycommon.TaskMedia, error) {
	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var err error
		media, err = metadataMedia(req.Metadata)
		if err != nil {
			return nil, err
		}
	}
	if len(media) == 0 {
		legacy := req.Images
		if len(legacy) == 0 && strings.TrimSpace(req.Image) != "" {
			legacy = []string{req.Image}
		}
		if len(legacy) == 0 && strings.TrimSpace(req.InputReference) != "" {
			legacy = []string{req.InputReference}
		}
		for _, url := range legacy {
			url = strings.TrimSpace(url)
			if url != "" {
				media = append(media, relaycommon.TaskMedia{Type: inferLegacyMediaType(url), URL: url})
			}
		}
	}
	if err := validateWan3Media(media); err != nil {
		return nil, err
	}
	return media, nil
}

func validateWan3Media(media []relaycommon.TaskMedia) error {
	counts := make(map[string]int)
	for _, item := range media {
		item.Type = strings.TrimSpace(item.Type)
		if item.URL = strings.TrimSpace(item.URL); item.URL == "" {
			return errors.New("wan3 media url is required")
		}
		switch item.Type {
		case "first_frame", "last_frame", "reference_image", "reference_video", "reference_audio", "file", "link":
			counts[item.Type]++
		default:
			return errors.Errorf("wan3 media type %q is unsupported", item.Type)
		}
	}
	if counts["first_frame"] > 1 || counts["last_frame"] > 1 {
		return errors.New("wan3 accepts at most one first_frame and one last_frame")
	}
	if counts["reference_image"] > maxReferenceImages {
		return errors.Errorf("wan3 accepts at most %d reference images", maxReferenceImages)
	}
	if counts["reference_video"] > maxReferenceVideos {
		return errors.Errorf("wan3 accepts at most %d reference videos", maxReferenceVideos)
	}
	if counts["reference_audio"] > maxReferenceAudios {
		return errors.Errorf("wan3 accepts at most %d reference audios", maxReferenceAudios)
	}
	if counts["file"] > 1 || counts["link"] > 1 {
		return errors.New("wan3 accepts at most one file and one link")
	}
	if counts["file"] > 0 && counts["link"] > 0 {
		return errors.New("wan3 file and link media are mutually exclusive")
	}
	frames := counts["first_frame"] > 0 || counts["last_frame"] > 0
	references := counts["reference_image"] > 0 || counts["reference_video"] > 0 || counts["reference_audio"] > 0 || counts["file"] > 0 || counts["link"] > 0
	if frames && references {
		return errors.New("wan3 first_frame/last_frame cannot be combined with reference media")
	}
	return nil
}

// EstimateBilling 按秒和分辨率计费：框架用 ModelPrice × seconds × size 计算配额
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolutionRatio := resolutionSizeRatio[resolveResolution(req)]
	if resolutionRatio == 0 {
		resolutionRatio = 1.0
	}
	return map[string]float64{
		"seconds": float64(resolveDuration(req)),
		"size":    resolutionRatio,
	}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/api/v1/services/aigc/video-generation/video-synthesis", strings.TrimRight(a.baseURL, "/")), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-DashScope-Async", "enable")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}

	media, err := buildWan3Media(req)
	if err != nil {
		return nil, err
	}
	prompt := normalizeWan3Prompt(strings.TrimSpace(req.Prompt))
	if prompt == "" && len(media) == 0 {
		return nil, errors.New("wan3 requires prompt or media")
	}

	model := upstreamModel
	if info != nil && info.ChannelMeta != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		model = strings.TrimSpace(info.UpstreamModelName)
	}
	parameters := submitParameters{
		Resolution:   resolveResolution(req),
		Ratio:        resolveRatio(req),
		Duration:     resolveDuration(req),
		PromptExtend: true,
		Audio:        defaultAudio,
		Watermark:    false,
		Seed:         req.Seed,
	}
	if req.Metadata != nil {
		if value, ok := metadataBool(req.Metadata, "audio", "generate_audio"); ok {
			parameters.Audio = value
		}
		if value, ok := metadataBool(req.Metadata, "watermark"); ok {
			parameters.Watermark = value
		}
		if value, ok := metadataBool(req.Metadata, "prompt_extend"); ok {
			parameters.PromptExtend = value
		}
	}
	body := submitRequest{
		Model: model,
		Input: submitInput{
			Prompt: prompt,
			Media:  media,
		},
		Parameters: parameters,
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
	// wan3 提交成功返回 202 Accepted，而框架 relay_task.go 只接受 200，
	// 否则会直接抛 fail_to_fetch_task 而不解析响应体（任务其实已提交，会导致预扣费无法收回）。
	// 这里把 2xx 统一归一成 200。
	if resp != nil && resp.StatusCode == http.StatusAccepted {
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
	if dResp.Code != "" {
		message := dResp.Message
		if message == "" {
			message = "wan3 request failed"
		}
		taskErr = service.TaskErrorWrapper(fmt.Errorf("wan3 error %s: %s", dResp.Code, message), "upstream_error", resp.StatusCode)
		return
	}
	if dResp.Output.Code != "" {
		message := dResp.Output.Message
		if message == "" {
			message = "wan3 task submission failed"
		}
		taskErr = service.TaskErrorWrapper(fmt.Errorf("wan3 error %s: %s", dResp.Output.Code, message), "upstream_error", resp.StatusCode)
		return
	}

	upstreamID := strings.TrimSpace(dResp.Output.TaskID)
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("wan3 returned empty task id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	req, _ := relaycommon.GetTaskRequest(c)
	resolution := resolveResolution(req)
	publicID := ""
	modelName := ""
	if info != nil {
		modelName = info.OriginModelName
		if info.TaskRelayInfo != nil {
			publicID = info.PublicTaskID
		}
	}
	out := gin.H{
		"id":         publicID,
		"object":     "video",
		"model":      modelName,
		"status":     convertWan3Status(dResp.Output.TaskStatus),
		"progress":   0,
		"created_at": time.Now().Unix(),
		"resolution": resolution,
		"seconds":    strconv.Itoa(resolveDurationForResponse(c)),
	}
	taskData = responseBody
	c.JSON(http.StatusOK, out)
	return upstreamID, taskData, nil
}

// resolveDurationForResponse 仅用于响应回显，取不到时回退默认值
func resolveDurationForResponse(c *gin.Context) int {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return defaultDuration
	}
	return resolveDuration(req)
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task id")
	}
	uri := fmt.Sprintf("%s/api/v1/tasks/%s", strings.TrimRight(baseUrl, "/"), taskID)
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
	var res taskResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal Wan3 task result failed")
	}

	taskResult := &relaycommon.TaskInfo{Code: 0}
	if res.Code != "" || res.Output.Code != "" {
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = firstNonEmpty(res.Message, res.Output.Message, res.Code, res.Output.Code, "Wan3 task failed")
		return taskResult, nil
	}

	switch strings.ToUpper(strings.TrimSpace(res.Output.TaskStatus)) {
	case "PENDING", "QUEUED", "SUBMITTING":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		if strings.TrimSpace(res.Output.VideoURL) == "" {
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = taskcommon.ProgressInProgress
		} else {
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Progress = taskcommon.ProgressComplete
			taskResult.Url = res.Output.VideoURL
		}
	case "FAILED", "FAILURE", "CANCELED", "CANCELLED", "UNKNOWN":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = firstNonEmpty(res.Message, res.Output.Message, "Wan3 task failed")
	default:
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

// ConvertToOpenAIVideo 把官方 Wan3 响应转换成 OpenAI video 对象返回给客户端。
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res taskResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal Wan3 task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = convertWan3Status(res.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt
	if res.Output.VideoURL != "" {
		openAIResp.SetMetadata("url", res.Output.VideoURL)
	}
	if res.Code != "" || res.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    firstNonEmpty(res.Code, res.Output.Code),
			Message: firstNonEmpty(res.Message, res.Output.Message, "Wan3 task failed"),
		}
	}
	return common.Marshal(openAIResp)
}

func convertWan3Status(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PENDING", "QUEUED", "SUBMITTING":
		return dto.VideoStatusQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		return dto.VideoStatusInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		return dto.VideoStatusCompleted
	case "FAILED", "FAILURE", "CANCELED", "CANCELLED", "UNKNOWN":
		return dto.VideoStatusFailed
	default:
		return dto.VideoStatusUnknown
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
