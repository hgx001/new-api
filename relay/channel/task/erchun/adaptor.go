package erchun

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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

// TaskAdaptor 对接二春 v1 开放 API 的异步视频合同。
//
// 与其它 task 渠道最大的不同：素材不能直接塞 URL。上游要求先把文件 multipart 上传到
// /v1/media/uploads 拿到 media_id，创建请求里传的是 [{"media_id":"..."}]。因此
// BuildRequestBody 内部会「下载下游给的 URL → 上传到上游 → 组装 media_id」。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	proxy       string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	a.proxy = info.ChannelSetting.Proxy
}

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) GetModelList() []string { return []string{PublicModel} }

// ---------------------------------------------------------------- 请求校验

// ValidateRequestAndSetAction 强制本渠道的档位与素材约束。
//
// 校验放在这里（计费预扣之前），一是 400 不会被当成 5xx 触发渠道重试，
// 二是被拒的请求不消耗上游额度。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	if _, mediaErr := buildMedia(req); mediaErr != nil {
		return service.TaskErrorWrapperLocal(mediaErr, "invalid_request", http.StatusBadRequest)
	}
	if _, resErr := resolveResolution(req); resErr != nil {
		return service.TaskErrorWrapperLocal(resErr, "invalid_resolution", http.StatusBadRequest)
	}
	if _, ratioErr := resolveAspectRatio(req); ratioErr != nil {
		return service.TaskErrorWrapperLocal(ratioErr, "invalid_ratio", http.StatusBadRequest)
	}
	return nil
}

// resolveResolution 只放行 480P。上游支持 720P/1080P，但本渠道只对外定价 480P，
// 让非 480P 走到建单会按 480P 收费却出了 720P 的片，属于静默少收。
func resolveResolution(req relaycommon.TaskSubmitReq) (string, error) {
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" {
		return onlyResolution, nil
	}
	switch strings.ToUpper(raw) {
	case "480", "480P":
		return onlyResolution, nil
	default:
		return "", errors.Errorf("%s only supports %s, got %q", PublicModel, onlyResolution, raw)
	}
}

// resolveDuration 读时长。**req.Seconds 是必须的回退路径**：下游按 OpenAI video 形状
// 传的是 {"seconds":"2"}（字符串），它落到 TaskSubmitReq.Seconds 而不是 Duration。
// 只读 Duration 会静默退回默认 5 秒 —— 2026-10-06 首次真机验证就是这么多收了
// 2.5 倍（请求 2 秒、按 5 秒计费）。与 wan3/dashscope 的同一处理保持一致。
func resolveDuration(req relaycommon.TaskSubmitReq) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(strings.TrimSpace(req.Seconds))
	}
	if duration <= 0 {
		return defaultDurationSeconds
	}
	if duration < minDurationSeconds {
		return minDurationSeconds
	}
	if duration > maxDurationSeconds {
		return maxDurationSeconds
	}
	return duration
}

// resolveAspectRatio 读比例：优先 metadata.ratio / metadata.aspect_ratio，其次从 size
// （如 1280x720）反解，都没有则用上游默认的 adaptive。
// 允许值以 /v1/catalog 的 aspect_ratios 为准，不接受目录外的值。
func resolveAspectRatio(req relaycommon.TaskSubmitReq) (string, error) {
	candidate := ""
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := req.Metadata[key].(string); ok {
				candidate = strings.TrimSpace(value)
				if candidate != "" {
					break
				}
			}
		}
	}
	if candidate == "" && strings.TrimSpace(req.Size) != "" {
		if ratio, ok := ratioFromSize(req.Size); ok {
			candidate = ratio
		}
	}
	if candidate == "" {
		return "adaptive", nil
	}
	if !allowedRatios[candidate] {
		return "", errors.Errorf("%s does not support aspect_ratio %q (allowed: %s)",
			PublicModel, candidate, strings.Join(sortedRatioKeys(), ", "))
	}
	return candidate, nil
}

// ratioFromSize 把 "1280x720" 这类尺寸反解成最简比例。
func ratioFromSize(size string) (string, bool) {
	parts := strings.SplitN(strings.ToLower(strings.TrimSpace(size)), "x", 2)
	if len(parts) != 2 {
		return "", false
	}
	width, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 {
		return "", false
	}
	divisor := greatestCommonDivisor(width, height)
	return fmt.Sprintf("%d:%d", width/divisor, height/divisor), true
}

func greatestCommonDivisor(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// buildMedia 归一化素材，沿用 youzanwan3 的取值顺序：显式 media → metadata.media →
// 旧版扁平字段（旧版扁平字段一律当参考图，与其它渠道一致）。
func buildMedia(req relaycommon.TaskSubmitReq) ([]relaycommon.TaskMedia, error) {
	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var err error
		if media, err = metadataMedia(req.Metadata); err != nil {
			return nil, err
		}
	}
	if len(media) == 0 {
		images := req.Images
		if len(images) == 0 && strings.TrimSpace(req.Image) != "" {
			images = []string{req.Image}
		}
		if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
			images = []string{req.InputReference}
		}
		for _, item := range images {
			if strings.TrimSpace(item) != "" {
				media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: strings.TrimSpace(item)})
			}
		}
	}
	return media, validateMedia(media)
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

func validateMedia(media []relaycommon.TaskMedia) error {
	counts := map[string]int{}
	for _, item := range media {
		item.Type = strings.TrimSpace(item.Type)
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			return errors.New("erchun media url is required")
		}
		switch item.Type {
		case "reference_image", "first_frame", "last_frame":
			counts["image"]++
		case "reference_video":
			counts["video"]++
		case "reference_audio":
			counts["audio"]++
		default:
			return errors.Errorf("erchun media type %q is unsupported", item.Type)
		}
	}
	if counts["image"] > maxReferenceImages {
		return errors.Errorf("erchun accepts at most %d reference images", maxReferenceImages)
	}
	if counts["video"] > maxReferenceVideos {
		return errors.Errorf("erchun accepts at most %d reference videos", maxReferenceVideos)
	}
	if counts["audio"] > maxReferenceAudios {
		return errors.Errorf("erchun accepts at most %d reference audios", maxReferenceAudios)
	}
	if total := counts["image"] + counts["video"] + counts["audio"]; total > maxMixedTotal {
		return errors.Errorf("erchun accepts at most %d media files in total", maxMixedTotal)
	}
	return nil
}

// ---------------------------------------------------------------- 计费

// EstimateBilling 按秒计费，base = ModelPrice 的每秒单价，只乘 seconds。
//
// 没有 size 倍率：本渠道只允许 480P，分辨率维度恒为 1。写成两个倍率再加一次浮点
// 乘截断只会引入不必要的不确定性（见 relay_task.go 的 applyTaskOtherRatios 注释）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{"seconds": float64(resolveDuration(req))}
}

// ---------------------------------------------------------------- 建单

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + createPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// 幂等键：同一请求重试必须复用同一个 Key，否则超时会重复下单。
	// 下游没显式带 Idempotency-Key 时用 task_id 派生，保证同一任务内稳定。
	if key := resolveIdempotencyKey(c, info); key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return nil
}

func resolveIdempotencyKey(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil && c.Request != nil {
		if key := strings.TrimSpace(c.GetHeader("Idempotency-Key")); key != "" {
			return key
		}
	}
	if info != nil && info.PublicTaskID != "" {
		return "newapi-" + info.PublicTaskID
	}
	return ""
}

type createRequest struct {
	Model          string     `json:"model"`
	Prompt         string     `json:"prompt"`
	AspectRatio    string     `json:"aspect_ratio,omitempty"`
	Resolution     string     `json:"resolution,omitempty"`
	DurationSecond int        `json:"duration_seconds,omitempty"`
	InputImages    []mediaRef `json:"input_images,omitempty"`
	InputVideos    []mediaRef `json:"input_videos,omitempty"`
	InputAudios    []mediaRef `json:"input_audios,omitempty"`
}

type mediaRef struct {
	MediaID string `json:"media_id"`
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	media, err := buildMedia(req)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(media) == 0 {
		return nil, errors.New("erchun requires prompt or reference media")
	}
	resolution, err := resolveResolution(req)
	if err != nil {
		return nil, err
	}
	aspectRatio, err := resolveAspectRatio(req)
	if err != nil {
		return nil, err
	}

	body := createRequest{
		Model:          a.upstreamModel(info),
		Prompt:         prompt,
		AspectRatio:    aspectRatio,
		Resolution:     resolution,
		DurationSecond: resolveDuration(req),
	}
	for _, item := range media {
		mediaID, err := a.uploadMedia(item)
		if err != nil {
			return nil, err
		}
		switch item.Type {
		case "reference_video":
			body.InputVideos = append(body.InputVideos, mediaRef{MediaID: mediaID})
		case "reference_audio":
			body.InputAudios = append(body.InputAudios, mediaRef{MediaID: mediaID})
		default:
			body.InputImages = append(body.InputImages, mediaRef{MediaID: mediaID})
		}
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal erchun request body failed")
	}
	return bytes.NewReader(encoded), nil
}

// upstreamModel 优先用渠道的模型映射结果；没配映射时回落到本渠道唯一的稳定 ID。
// 不能把 PublicModel 直接发给上游 —— 它是我们自己的对外名，上游只认 mdl_ 稳定 ID。
func (a *TaskAdaptor) upstreamModel(info *relaycommon.RelayInfo) string {
	if info != nil {
		if mapped := strings.TrimSpace(info.UpstreamModelName); strings.HasPrefix(mapped, "mdl_") {
			return mapped
		}
	}
	return UpstreamModelWan3
}

// ---------------------------------------------------------------- 素材上传

// uploadMedia 下载下游给的素材再上传到上游，返回 media_id。
func (a *TaskAdaptor) uploadMedia(item relaycommon.TaskMedia) (string, error) {
	kind := "image"
	allowed, limit := allowedImageMIME, int64(maxImageBytes)
	switch item.Type {
	case "reference_video":
		kind, allowed, limit = "video", allowedVideoMIME, int64(maxVideoBytes)
	case "reference_audio":
		kind, allowed, limit = "audio", allowedAudioMIME, int64(maxAudioBytes)
	}

	data, mimeType, sourceName, err := readAsset(item.URL, limit)
	if err != nil {
		return "", err
	}
	if !allowed[strings.ToLower(strings.TrimSpace(mimeType))] {
		return "", errors.Errorf("erchun %s reference does not accept %s (allowed: %s)",
			kind, mimeType, strings.Join(sortedKeys(allowed), ", "))
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	// 上游会校验文件 part 声明的 MIME 与实际内容是否一致；CreateFormFile 会写死
	// application/octet-stream 从而触发 415，所以这里手工构造 part header。
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition",
		`form-data; name="file"; filename=`+strconv.Quote(mediaFileName(kind, sourceName, mimeType)))
	partHeader.Set("Content-Type", mimeType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", err
	}
	if err = writer.Close(); err != nil {
		return "", err
	}

	client, err := a.httpClient()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, a.baseURL+uploadPath, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "erchun media upload request failed")
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", errors.Wrap(err, "read erchun media upload response failed")
	}
	// 上传成功是 201；其余状态按错误处理，不要把错误页当 media_id 用。
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", errors.Errorf("erchun media upload failed: HTTP %d: %s",
			resp.StatusCode, truncateForError(respBody))
	}
	var uploaded uploadResponse
	if err := common.Unmarshal(respBody, &uploaded); err != nil {
		return "", errors.Wrap(err, "unmarshal erchun media upload response failed")
	}
	if strings.TrimSpace(uploaded.MediaID) == "" {
		return "", errors.Errorf("erchun media upload returned empty media_id: %s", truncateForError(respBody))
	}
	return uploaded.MediaID, nil
}

type uploadResponse struct {
	Object  string `json:"object"`
	MediaID string `json:"media_id"`
}

func (a *TaskAdaptor) httpClient() (*http.Client, error) {
	client, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return http.DefaultClient, nil
	}
	return client, nil
}

// readAsset 读取材质字节。data: URL 直接解，其余必须是 http(s)，并过 SSRF 白名单。
func readAsset(source string, limit int64) ([]byte, string, string, error) {
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		data, mimeType, err := parseDataURL(source)
		return data, mimeType, "asset", err
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", "", errors.New("erchun requires http(s) URL or data URL media")
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(source, fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode,
		fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", "", errors.Wrap(err, "erchun media URL blocked")
	}
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", "", errors.Errorf("download media failed: HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, "", "", errors.Errorf("media file exceeds the %d byte limit", limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(data)) > limit {
		return nil, "", "", errors.Errorf("media file exceeds the %d byte limit", limit)
	}
	if len(data) == 0 {
		return nil, "", "", errors.New("media file is empty")
	}
	return data, http.DetectContentType(data), path.Base(parsed.Path), nil
}

func parseDataURL(value string) ([]byte, string, error) {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 || !strings.HasPrefix(strings.ToLower(parts[0]), "data:") {
		return nil, "", errors.New("invalid data URL")
	}
	meta := parts[0][5:]
	mimeType := strings.Split(meta, ";")[0]
	if strings.Contains(meta, ";base64") {
		decoded, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, "", errors.New("invalid data URL base64 payload")
		}
		return decoded, mimeType, nil
	}
	return nil, "", errors.New("erchun only supports base64 data URLs")
}

func mediaFileName(kind, sourceName, mimeType string) string {
	ext := path.Ext(strings.TrimSpace(sourceName))
	switch kind {
	case "video":
		if ext == "" {
			ext = ".mp4"
		}
	case "audio":
		if ext == "" {
			ext = ".mp3"
		}
	default:
		if ext == "" {
			switch mimeType {
			case "image/png":
				ext = ".png"
			case "image/webp":
				ext = ".webp"
			default:
				ext = ".jpg"
			}
		}
	}
	return "erchun" + kind + ext
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func truncateForError(body []byte) string {
	const limit = 300
	text := strings.TrimSpace(string(body))
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

// ---------------------------------------------------------------- 提交响应

type createResponse struct {
	ID         string `json:"id"`
	Object     string `json:"object"`
	Status     string `json:"status"`
	CreatedAt  int64  `json:"created_at"`
	StatusURL  string `json:"status_url"`
	ContentURL string `json:"content_url"`
	RequestID  string `json:"request_id"`
	Error      *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// v1 合同里创建成功是 202 Accepted，框架只把 200 当成功，否则会抛
	// fail_to_fetch_task 而不解析响应体（任务其实已提交，预扣费收不回来）。
	if resp != nil && (resp.StatusCode == http.StatusAccepted || resp.StatusCode == http.StatusCreated) {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrap(err, "read_response_body_failed"), "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		taskErr = upstreamError(resp.StatusCode, responseBody)
		return
	}
	var created createResponse
	if err := common.Unmarshal(responseBody, &created); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}
	if created.Error != nil && strings.TrimSpace(created.Error.Code) != "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("%s: %s", created.Error.Code, created.Error.Message),
			"upstream_error", http.StatusBadRequest)
		return
	}
	taskID = strings.TrimSpace(created.ID)
	if taskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("erchun returned empty task id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}
	if strings.TrimSpace(created.StatusURL) == "" {
		created.StatusURL = fmt.Sprintf(taskPath, taskID)
	}
	// 202 只代表受理，这里必须回 queued，不能当完成。
	status := convertErchunStatus(created.Status)

	modelName := PublicModel
	if info != nil && info.OriginModelName != "" {
		modelName = info.OriginModelName
	}
	publicID := ""
	if info != nil && info.TaskRelayInfo != nil {
		publicID = info.PublicTaskID
	}
	out := gin.H{
		"id":         publicID,
		"object":     "video",
		"model":      modelName,
		"status":     status,
		"progress":   0,
		"created_at": created.CreatedAt,
	}
	if publicID != "" {
		out["status_url"] = taskcommon.BuildProxyURL(publicID)
	}
	c.JSON(http.StatusOK, out)
	return taskID, responseBody, nil
}

func upstreamError(statusCode int, body []byte) *dto.TaskError {
	var wrapper struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	_ = common.Unmarshal(body, &wrapper)
	code := firstNonEmpty(wrapper.Error.Code, wrapper.Code)
	message := firstNonEmpty(wrapper.Error.Message, wrapper.Message)
	if code == "" && message == "" {
		message = string(body)
	}
	taskErr := service.TaskErrorWrapper(fmt.Errorf("%s: %s", code, message), "upstream_error", statusCode)
	return taskErr
}

// ---------------------------------------------------------------- 轮询

type taskResponse struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	BillingSt    string `json:"billing_status"`
	Model        string `json:"model"`
	UpdatedAt    int64  `json:"updated_at"`
	CompletedAt  int64  `json:"completed_at"`
	ContentURL   string `json:"content_url"`
	RequestID    string `json:"request_id"`
	ErrorCode    string `json:"-"`
	ErrorMessage string `json:"-"`
	Error        *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task id")
	}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf(baseUrl+taskPath, taskID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, errors.Wrap(err, "new erchun http client failed")
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res taskResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal erchun task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0}
	if res.Error != nil {
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(res.Error.Message, res.Error.Code, taskcommon.ReasonContentModeration)
		return result, nil
	}
	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "queued", "pending", "submitted":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "in_progress", "running", "processing":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "completed", "succeeded", "success":
		if strings.TrimSpace(res.ContentURL) == "" {
			// 终态却没给 content_url：不能当成功，否则客户端会拿到一个打不开的地址。
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		// content_url 需要 Bearer，下游拿不到。这里**故意留空 Url**：
		// service/task_polling.go 在 Url 为空时会自动落成站内 content 代理地址，
		// 再由 controller/video_proxy.go 的 ChannelTypeErchun 分支带渠道密钥回源。
	case "failed", "canceled", "cancelled":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = taskcommon.ReasonContentModeration
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

// ConvertToOpenAIVideo 把二春任务记录转成 OpenAI Video API 查询响应（GET /v1/videos/{id}）。
//
// 产物 URL 契约与 ParseTaskResult 同口径：content_url 需要渠道 Bearer 才能取，
// **不透传上游相对路径**，metadata.url 统一落成站内 /v1/videos/{task}/content 代理地址，
// 由 controller/video_proxy.go 的 ChannelTypeErchun 分支带渠道密钥回源下载。
// ArcReel 的 openai SDK 下载走 client.videos.download_content(video_id)，天然命中该代理端点。
// 缺失此方法时 relay_task.go 的 OpenAI Video 查询分支会落到 not_implemented:{platform}，
// 下游（ArcReel）轮询永远拿不到终态 —— 2026-10-08 B 机 wan3.0-480p 线上事故的根因。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var res taskResponse
	if err := common.Unmarshal(originTask.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal erchun task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = res.Model

	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "completed", "succeeded", "success":
		// content_url 需要渠道 Bearer，不能透传给客户端；落站内代理地址。
		openAIVideo.SetMetadata("url", taskcommon.BuildProxyURL(originTask.TaskID))
	case "failed", "canceled", "cancelled":
		message := taskcommon.ReasonContentModeration
		code := ""
		if res.Error != nil {
			message = firstNonEmpty(res.Error.Message, res.Error.Code, message)
			code = res.Error.Code
		}
		openAIVideo.Error = &dto.OpenAIVideoError{Message: message, Code: code}
	}

	return common.Marshal(openAIVideo)
}

func convertErchunStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "succeeded", "success":
		return dto.VideoStatusCompleted
	case "failed", "canceled", "cancelled":
		return dto.VideoStatusFailed
	case "in_progress", "running", "processing":
		return dto.VideoStatusInProgress
	case "queued", "pending", "submitted":
		return dto.VideoStatusQueued
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

func sortedRatioKeys() []string {
	out := make([]string, 0, len(allowedRatios))
	for key := range allowedRatios {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
