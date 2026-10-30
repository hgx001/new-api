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
	"github.com/QuantumNous/new-api/logger"
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
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	// 参考视频按时长同价计费（生成秒数 + 参考视频总秒数）。
	// 探测必须放在 validate 阶段：EstimateBilling 没有错误返回值，解析失败时无法拒单，
	// 而计费拿不到时长就会按纯生成时长收费（少收）。
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	media, err := buildWan3Media(req)
	if err != nil {
		return service.TaskErrorWrapper(err, "invalid_input_reference", http.StatusBadRequest)
	}
	// 参考图必须在建单前确认抓得到：上游是「直传 URL、生成时才下载」，
	// 坏链接会让用户白等一两分钟才收到 InvalidParameter，甚至出现
	// 「成片成功但完全没有参考图」。详见 media_probe.go。
	if err := validateReferenceImageURLs(media); err != nil {
		return service.TaskErrorWrapper(err, "invalid_input_reference", http.StatusBadRequest)
	}
	if len(media) > 0 {
		// 留痕：事后排查「这张图到底有没有随请求发出去」时，
		// 库里的 properties 不存 media，只能靠这条日志对上任务时间。
		logger.LogInfo(c, fmt.Sprintf("wan3 media accepted: %s", describeMedia(media)))
	}
	total := 0
	totalReal := 0.0
	videoCount := 0
	for _, item := range media {
		if item.Type != "reference_video" {
			continue
		}
		probed, err := probeReferenceVideoSeconds(item.URL)
		if err != nil {
			return service.TaskErrorWrapper(errors.Wrap(err, "probe reference video duration failed"), "invalid_input_reference", http.StatusBadRequest)
		}
		total += probed.seconds
		totalReal += probed.realSeconds
		videoCount++
	}
	if videoCount > 0 {
		// 官方《万相3.0-视频生成API参考》：参考视频最多 5 段、总时长不大于 15 秒。
		if totalReal > maxReferenceVideoTotalSeconds+durationEpsilon {
			return service.TaskErrorWrapper(
				errors.Errorf("wan3 reference videos total %.2fs exceeds the %ds limit", totalReal, maxReferenceVideoTotalSeconds),
				"invalid_input_reference", http.StatusBadRequest)
		}
		// 官方同一条文档：有视频输入时，**输入视频总时长 + 输出视频时长不超过 30 秒**。
		// 本站计费秒数 = 生成时长 + 参考视频总秒数 = 上游 usage.duration（同一个数），
		// 所以这条约束顺带把预扣额度也锁在 30 秒内；不本地拦就会按上游根本不会
		// 生成的规格预扣了钱，再等上游 400 退款。
		generation := resolveDuration(req)
		if totalReal+float64(generation) > maxTotalDurationWithVideo+durationEpsilon {
			return service.TaskErrorWrapper(
				errors.Errorf("wan3 reference video total %.2fs + generation %ds exceeds the %ds limit",
					totalReal, generation, maxTotalDurationWithVideo),
				"invalid_input_reference", http.StatusBadRequest)
		}
		c.Set(refVideoSecondsContextKey, total)
	}
	return nil
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

// resolveGenerationResolution 解析**向上游请求**的分辨率。
//
// 与 resolveResolution 的区别：对外模型 wan3.0-720p-特惠 是「两跳」链路，
// 上游固定只生成 480P，成片再交给云端提升到 720P —— 否则用户传 720P 时
// 会直接生成 720P，两跳链路形同虚设（多花一倍生成费，还拿不到成片提升的效果）。
//
// ⚠️ 只有这个模型名走这条逻辑；其余模型名（官网原生 720P/1080P、标准版）
//	的分辨率照旧由用户请求决定。
func resolveGenerationResolution(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) string {
	if info != nil && isOfficial720PModel(info.OriginModelName) {
		return official720PGenerationResolution
	}
	return resolveResolution(req)
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
	// 兼容客户端把分辨率档直接放 size（如 "720P"），与 WxH 尺寸等价解析。
	if normalized := normalizeResolution(size); normalized != "" {
		return normalized, true
	}
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

// EstimateBilling 按秒和分辨率计费：框架用 ModelPrice × seconds × size 计算配额。
// 有参考视频时 seconds = 生成时长 + 参考视频总秒数（同分辨率单价），
// 例如 10 秒参考视频生成 10 秒 480P → 0.20 × 20 = ¥4.00。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolutionRatio := resolutionSizeRatioFor(resolveResolution(req), info)
	seconds := resolveDuration(req)
	if value, ok := c.Get(refVideoSecondsContextKey); ok {
		if refSeconds, ok := value.(int); ok && refSeconds > 0 {
			seconds += refSeconds
		}
	}
	return map[string]float64{
		"seconds": float64(seconds),
		"size":    resolutionRatio,
	}
}

// resolutionSizeRatioFor 按**对外模型名**选分辨率倍率表：官网渠道用调价后的
// 1/2.25/4，其余模型名保持 1/2/4 不变。调价只授权了官网渠道这一个模型名，
// 共用一张表会把标准版的 720P/1080P 连带带偏。
func resolutionSizeRatioFor(resolution string, info *relaycommon.RelayInfo) float64 {
	table := standardResolutionSizeRatio
	if info != nil {
		switch info.OriginModelName {
		case OfficialModelName:
			table = officialResolutionSizeRatio
		case Official720PModelName, Official720PModelNameLegacy:
			// wan3.0-720p-特惠 只有一个输出档位（720P），价格是
			// 「480P 生成 + 成片提升」的复合价，已经含在 ModelPrice 里。
			//
			// ⚠️ 这里必须恒为 1.0，不能落进 standardResolutionSizeRatio：
			// 用户传 720P 时那张表给 2.0，价格会翻一倍。
			return 1.0
		}
	}
	ratio := table[resolution]
	if ratio == 0 {
		ratio = 1.0
	}
	return ratio
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
	// 兜底：官网这两个对外名上游一律是标准版 wan3.0-video。
	//
	// ⚠️ 必须在渠道映射之后做：渠道 model_mapping 是可配的，管理员漏配了新模型名时
	//	UpstreamModelName 会仍是「wan3.0-720p-特惠」，上游根本不认识这个名字。
	//	不兜底就是「用户付了钱、上游直接拒」。
	model = upstreamModelFor(model)
	parameters := submitParameters{
		Resolution:   resolveGenerationResolution(req, info),
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
		taskResult.Reason = firstNonEmpty(res.Message, res.Output.Message, res.Code, res.Output.Code, taskcommon.ReasonContentModeration)
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
		taskResult.Reason = firstNonEmpty(res.Message, res.Output.Message, taskcommon.ReasonContentModeration)
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
//
// ⚠️ 成片地址的口径：
//
//   · 两跳链路（wan3.0-720p-特惠）下，库里的 ResultURL 是**内部占位符**
//     （cos-proxy://…），客户端拿到没法下载；而且上游直链是 **480P 原片**，
//     而用户买到的是 720P —— 给上游直链等于交付了错的产物。
//     → 换成本站 /content 代理地址，由代理解析占位符后 302 到临时直链。
//
//   · 单跳链路（wan3.0-video 等）下，ResultURL 是上游 OSS 签名直链，
//     客户端直连下载最快（走本站代理反而要多经一次本机 3Mbps 出口，30MB 要 87 秒）。
//     → 原样给直链。
//
// 这两条都由 taskcommon.PublicResultURL 统一裁决，不在本文件里各写一套。
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res taskResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal Wan3 task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Status = taskVideoStatus(task, res.Output.TaskStatus)
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt
	// 成片地址：只在任务成功后给。
	//
	// 未完成时不给（给的是上游 480P 直链，对两跳链路来说是错的产物；
	// 未完成的客户端应继续轮询，不该拿到一个半成品的地址）。
	stored := strings.TrimSpace(task.PrivateData.ResultURL)
	if stored == "" {
		// 兜底：库里没落地址的历史任务，用上游回执里的直链。
		stored = strings.TrimSpace(res.Output.VideoURL)
	}
	if task.Status == model.TaskStatusSuccess {
		if publicURL := taskcommon.PublicResultURL(task.TaskID, stored); publicURL != "" {
			openAIResp.SetMetadata("url", publicURL)
		}
	}
	if res.Code != "" || res.Output.Code != "" {
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    firstNonEmpty(res.Code, res.Output.Code),
			Message: firstNonEmpty(res.Message, res.Output.Message, taskcommon.ReasonContentModeration),
		}
	}
	return common.Marshal(openAIResp)
}

// taskVideoStatus 决定对外报告的任务状态。
//
// ⚠️ 两跳链路**不能**用上游回执里的状态（2026-10-30 修）：
//	上游回执（task.Data）在第一跳成功的那一刻就冻结了 —— 里面写的 TaskStatus
//	是 "SUCCEEDED"，指的是「第一跳生成完了」，不是「整条链路完了」。
//
//	两跳链路（wan3.0-720p-特惠）在第一跳成功后还要跑第二跳（成片提升），
//	这期间库里任务是 IN_PROGRESS，但回执里已经是 SUCCEEDED。
//	只按回执判定的话，客户端会在第二跳还没跑完时就看到 status=completed，
//	然后去取片 —— 拿到的是**还没提升过的 480P**，正是这套链路要消灭的东西。
//
//	所以口径是：
//	  · 任务还没成功（库内非 SUCCESS）→ 以库内状态为准（两跳链路的真实进度）；
//	  · 任务已成功 → 报 completed。
//	单跳链路（其它模型名）两者一致，行为不变。
func taskVideoStatus(task *model.Task, upstreamStatus string) string {
	if task == nil {
		return convertWan3Status(upstreamStatus)
	}
	switch task.Status {
	case model.TaskStatusSuccess:
		return dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		return dto.VideoStatusFailed
	case model.TaskStatusQueued, model.TaskStatusSubmitted, model.TaskStatusNotStart:
		return dto.VideoStatusQueued
	case model.TaskStatusInProgress:
		return dto.VideoStatusInProgress
	}
	// 库里状态不认识（历史数据/异常）→ 回落到上游回执。
	return convertWan3Status(upstreamStatus)
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
