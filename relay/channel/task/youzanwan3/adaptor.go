package youzanwan3

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
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
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/youzanwan3_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const maxAssetBytes = 100 << 20

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	proxy       string
}

type generateResponse struct {
	ID     string `json:"id"`
	TaskID string `json:"taskId"`
	Task   struct {
		ID string `json:"id"`
	} `json:"task"`
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
	// 上游 V1 协议用 ok + error 表达失败（形如 {"ok":false,"error":{"code":...,
	// "message":...}}）。这两个字段必须单独解析：失败响应里根本没有 task id，
	// 若不先判错误就会退化成「returned empty task id」，把上游的真实原因
	// （如 DURATION_INVALID / PROMPT_INVALID / 积分不足）吞成无信息量的 502。
	OK      bool          `json:"ok"`
	Error   upstreamError `json:"error"`
	Message string        `json:"message"`
}

// upstreamError 兼容有赞两种 error 形态：裸字符串，或 {code, message} 对象。
//
// 2026-10-05 事故：对象形态让 `Error string` 整体反序列化失败，ParseTaskResult
// 每轮轮询都抛 "cannot unmarshal object into ... error of type string"，任务因此
// 永远停在 NOT_START / 0%——用户既看不到失败原因，也永远拿不到退款（上游早已
// refundedPoints）。对象形态统一拍平成 "code: message"，与上游字符串形态里已有的
// "WAN3_QUOTA_CAPACITY_INSUFFICIENT: ..." 风格一致。
type upstreamError string

func (e *upstreamError) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := common.Unmarshal(data, &text); err != nil {
			return err
		}
		*e = upstreamError(text)
		return nil
	}
	var object struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := common.Unmarshal(data, &object); err != nil {
		return err
	}
	switch {
	case object.Code != "" && object.Message != "":
		*e = upstreamError(object.Code + ": " + object.Message)
	case object.Message != "":
		*e = upstreamError(object.Message)
	default:
		*e = upstreamError(object.Code)
	}
	return nil
}

type taskResponse struct {
	OK     bool   `json:"ok"`
	Status string `json:"status"`
	Result struct {
		URL string `json:"url"`
	} `json:"result"`
	Error   upstreamError `json:"error"`
	Message string        `json:"message"`
	Task    struct {
		ID           string        `json:"id"`
		Status       string        `json:"status"`
		VideoURL     string        `json:"videoUrl"`
		ErrorCode    string        `json:"errorCode"`
		ErrorMessage string        `json:"errorMessage"`
		Error        upstreamError `json:"error"`
	} `json:"task"`
}

type assetResponse struct {
	Asset struct {
		ID   string `json:"id"`
		Kind string `json:"kind"`
	} `json:"asset"`
	URL string `json:"url"`
}

var versionSuffixPattern = regexp.MustCompile(`(?i)/v\d+(beta)?$`)

// normalizeAPIRoot mirrors the reference Youzan client's normalizeRoot:
// the /api/* endpoints live at the host root, so a pasted base URL with a
// version suffix (e.g. https://youzan666.vip/v1) must have it stripped.
func normalizeAPIRoot(baseURL string) string {
	root := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return versionSuffixPattern.ReplaceAllString(root, "")
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = normalizeAPIRoot(info.ChannelBaseUrl)
	a.apiKey = info.ApiKey
	a.proxy = info.ChannelSetting.Proxy
}

// resolveResultURL mirrors `new URL(raw, root + '/')`: upstream returns a
// host-relative path (e.g. /outputs/videos/xxx.mp4), which must be resolved
// against the API root before it can be proxied or downloaded.
func (a *TaskAdaptor) resolveResultURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if root := strings.TrimRight(a.baseURL, "/"); root != "" {
		if base, err := url.Parse(root + "/"); err == nil {
			if ref, err := url.Parse(raw); err == nil {
				return base.ResolveReference(ref).String()
			}
		}
	}
	return raw
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	// 素材组合/数量错误（首尾帧混用、r2v 缺参考图等）属于客户端参数错误：
	// 在这里以 400 本地错误返回，既给出正确状态码，也不会被当成 5xx 触发渠道重试。
	media, mediaErr := buildMedia(req)
	if mediaErr != nil {
		return service.TaskErrorWrapperLocal(mediaErr, "invalid_request", http.StatusBadRequest)
	}
	// 参考视频按秒加价（生成秒数 + 参考视频总秒数），秒数在这里探测并塞进 context，
	// 由 EstimateBilling 读出来并入计费。必须放在预扣之前：EstimateBilling 没有
	// 错误返回值，那时才发现解析不了就只能按 0 秒收钱（静默少收）。
	if refErr := a.setReferenceVideoSeconds(c, media); refErr != nil {
		return service.TaskErrorWrapperLocal(
			errors.Wrap(refErr, "probe reference video duration failed"),
			"invalid_input_reference", http.StatusBadRequest)
	}
	return nil
}

// setReferenceVideoSeconds 探测所有 reference_video 素材的时长，累加后写入 context。
// 没有参考视频时不写 key —— EstimateBilling 读到 0 与读不到要表现一致（不加价）。
func (a *TaskAdaptor) setReferenceVideoSeconds(c *gin.Context, media []relaycommon.TaskMedia) error {
	total := 0
	for _, item := range media {
		if item.Type != "reference_video" {
			continue
		}
		seconds, err := probeReferenceVideoSeconds(item.URL, a.proxy)
		if err != nil {
			return err
		}
		total += seconds
	}
	if total > 0 {
		c.Set(refVideoSecondsContextKey, total)
	}
	return nil
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

func isR2VModel(model string) bool {
	return strings.TrimSpace(model) == r2vModel
}

func isPrimeModel(model string) bool {
	switch strings.TrimSpace(model) {
	case primeModel, legacyPrimeModel, tehuiModel:
		return true
	default:
		return false
	}
}

// isTehuiModel 判断是不是「独立定价的对外别名」seedance2.5-特惠。
//
// 它虽然由 prime 执行（isPrimeModel 为真，档位校验/默认值都走 prime 那套），
// 但**价格是它自己的**：720P ¥0.20、1080P ¥0.30，不跟 prime 联动。
// 计费取值时必须先认它，否则会读到 prime 的倍率 —— 表现是不报错、静默算错价。
func isTehuiModel(model string) bool {
	return strings.TrimSpace(model) == tehuiModel
}

// upstreamModelFor maps the public aliases retained by new-api to the model
// IDs exposed by the current Youzan API.  The mapping is explicit so the
// public name remains stable while the upstream protocol can evolve.
func upstreamModelFor(model string) (string, error) {
	switch strings.TrimSpace(model) {
	case videoModel, smartLegacyModel:
		return videoModel, nil
	case primeModel, legacyPrimeModel, tehuiModel:
		return primeModel, nil
	case r2vModel:
		return "", errors.New("youzan wan2.7-r2v is not exposed by the current upstream API")
	default:
		return "", errors.Errorf("unsupported Youzan Wan3 model %q", model)
	}
}

func normalizeResolutionForModel(model, resolution string) string {
	if isPrimeModel(model) {
		// 上游 prime 只提供 720P / 1080P：480P 请求按最低档 720P 执行，
		// 既不静默升到更贵的 1080P，也让请求能够成功。
		switch resolution {
		case "720P", "1080P":
			return resolution
		case "480P":
			return primeDefaultResolution
		default:
			return ""
		}
	}
	if !isR2VModel(model) {
		return resolution
	}
	// 上游 wan2.7-r2v 只接受小写 720p / 1080p，且不支持 480P。
	switch resolution {
	case "720P", "720p":
		return "720p"
	case "1080P", "1080p":
		return "1080p"
	default:
		return ""
	}
}

func defaultResolutionForModel(model string) string {
	if isR2VModel(model) {
		return "1080p"
	}
	if isPrimeModel(model) {
		return primeDefaultResolution
	}
	return defaultResolution
}

func resolveResolutionForModel(req relaycommon.TaskSubmitReq, model string) string {
	if value := normalizeResolutionForModel(model, normalizeResolution(req.Resolution)); value != "" {
		return value
	}
	if req.Metadata != nil {
		if value, ok := req.Metadata["resolution"].(string); ok {
			if value = normalizeResolutionForModel(model, normalizeResolution(value)); value != "" {
				return value
			}
		}
	}
	if value := normalizeResolutionForModel(model, normalizeResolution(req.Size)); value != "" {
		return value
	}
	return defaultResolutionForModel(model)
}

func resolveResolution(req relaycommon.TaskSubmitReq) string {
	return resolveResolutionForModel(req, req.Model)
}

func resolveDuration(req relaycommon.TaskSubmitReq) int {
	return resolveDurationForModel(req, req.Model)
}

// resolveDurationForModel 解析时长。2026-10-28 起上游 prime 也改为按秒计费
// （supportsSmartDuration=false，但 2—30 秒逐秒可选），不再固定 30 秒；
// 两个在售模型因此共用同一套夹紧规则。
func resolveDurationForModel(req relaycommon.TaskSubmitReq, model string) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(req.Seconds)
	}
	if duration <= 0 {
		return defaultDuration
	}
	if isR2VModel(model) {
		// 上游仅接受 5 秒和 10 秒两档，就近取档。
		if duration <= r2vMinDuration {
			return r2vMinDuration
		}
		return r2vMaxDuration
	}
	if duration < minDuration {
		return minDuration
	}
	maxAllowedDuration := maxDuration
	if duration > maxAllowedDuration {
		return maxAllowedDuration
	}
	return duration
}

func resolveRatio(req relaycommon.TaskSubmitReq) string {
	allowedRatios := smartRatios
	fallback := defaultRatio
	if isR2VModel(req.Model) {
		allowedRatios = r2vRatios
		fallback = r2vDefaultRatio
	}
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := req.Metadata[key].(string); ok {
				value = strings.TrimSpace(value)
				for _, allowed := range allowedRatios {
					if value == allowed {
						return value
					}
				}
			}
		}
	}
	return fallback
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

func buildMedia(req relaycommon.TaskSubmitReq) ([]relaycommon.TaskMedia, error) {
	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var err error
		media, err = metadataMedia(req.Metadata)
		if err != nil {
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
	return media, validateMedia(media, req.Model)
}

func validateMedia(media []relaycommon.TaskMedia, model string) error {
	counts := make(map[string]int)
	for _, item := range media {
		if strings.TrimSpace(item.URL) == "" {
			return errors.New("youzan wan3 media url is required")
		}
		switch item.Type {
		case "reference_image", "reference_video", "reference_audio":
			counts[item.Type]++
		case "first_frame", "last_frame":
			// 2026-10-28 上游 /v1/tasks 取消了 firstFrameAssetId/lastFrameAssetId，
			// media 只认 reference_image / reference_video / reference_audio 三类，
			// 且 type 必须等于上传回执的 asset.kind。首尾帧已无法表达；静默降级成
			// 参考图会让客户端以为做了首尾帧控制（实际只是普通参考），故本地直接拒绝。
			return errors.New("youzan wan3 no longer supports first_frame/last_frame, use reference_image instead")
		default:
			return errors.Errorf("youzan wan3 media type %q is unsupported", item.Type)
		}
	}
	if counts["reference_image"] > maxReferenceImages {
		return errors.Errorf("youzan wan3 accepts at most %d reference images", maxReferenceImages)
	}
	if counts["reference_video"] > maxReferenceVideos {
		return errors.Errorf("youzan wan3 accepts at most %d reference videos", maxReferenceVideos)
	}
	if counts["reference_audio"] > maxReferenceAudios {
		return errors.Errorf("youzan wan3 accepts at most %d reference audios", maxReferenceAudios)
	}
	if total := counts["reference_image"] + counts["reference_video"] + counts["reference_audio"]; total > maxTotalMedia {
		return errors.Errorf("youzan wan3 accepts at most %d media items in total", maxTotalMedia)
	}
	return nil
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
		return decoded, mimeType, err
	}
	decoded, err := url.PathUnescape(parts[1])
	return []byte(decoded), mimeType, err
}

func getHTTPClient(proxy string) (*http.Client, error) {
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = http.DefaultClient
	}
	return client, nil
}

func (a *TaskAdaptor) readAsset(source string) ([]byte, string, string, error) {
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		data, mimeType, err := parseDataURL(source)
		return data, mimeType, "asset", err
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, "", "", errors.New("youzan wan3 requires http(s) URL or data URL media")
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(source, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", "", errors.Wrap(err, "youzan wan3 media URL blocked")
	}
	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return nil, "", "", err
	}
	request, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return nil, "", "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", "", err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", "", errors.Errorf("download media failed: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > maxAssetBytes {
		return nil, "", "", errors.New("media file is too large")
	}
	limited := io.LimitReader(response.Body, maxAssetBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", "", err
	}
	if len(data) > maxAssetBytes {
		return nil, "", "", errors.New("media file is too large")
	}
	mimeType := response.Header.Get("Content-Type")
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = mimeType[:index]
	}
	return data, mimeType, path.Base(parsed.Path), nil
}

func flattenImage(data []byte) ([]byte, string, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, "image/jpeg", nil
	}
	background := image.NewRGBA(img.Bounds())
	draw.Draw(background, background.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(background, background.Bounds(), img, img.Bounds().Min, draw.Over)
	var output bytes.Buffer
	if err := jpeg.Encode(&output, background, &jpeg.Options{Quality: 95}); err != nil {
		return nil, "", err
	}
	return output.Bytes(), "image/jpeg", nil
}

func fileName(kind, source, mimeType string) string {
	name := path.Base(strings.TrimSpace(source))
	if name == "." || name == "/" || name == "" {
		name = "reference-" + kind
	}
	if strings.HasPrefix(name, "data:") || strings.Contains(name, "?") {
		name = "reference-" + kind
	}
	if kind == "image" {
		return strings.TrimSuffix(name, path.Ext(name)) + ".jpg"
	}
	if path.Ext(name) == "" {
		switch mimeType {
		case "video/mp4":
			name += ".mp4"
		case "audio/mpeg":
			name += ".mp3"
		}
	}
	return name
}

func assetUploadKind(mediaType string) (string, string, error) {
	switch mediaType {
	case "reference_image":
		return "reference_image", "image", nil
	case "reference_video":
		return "reference_video", "video", nil
	case "reference_audio":
		return "reference_audio", "audio", nil
	default:
		return "", "", errors.Errorf("youzan wan3 media type %q is unsupported by the current API", mediaType)
	}
}

func (a *TaskAdaptor) uploadAsset(mediaType, source string) (string, error) {
	assetKind, fileKind, err := assetUploadKind(mediaType)
	if err != nil {
		return "", err
	}
	data, mimeType, sourceName, err := a.readAsset(source)
	if err != nil {
		return "", err
	}
	if fileKind == "image" {
		data, mimeType, err = flattenImage(data)
		if err != nil {
			return "", err
		}
	}
	contentType := strings.TrimSpace(mimeType)
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	query := url.Values{
		"kind":     {assetKind},
		"filename": {fileName(fileKind, sourceName, mimeType)},
	}
	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequest(
		http.MethodPost,
		strings.TrimRight(a.baseURL, "/")+"/v1/assets?"+query.Encode(),
		bytes.NewReader(data),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	dataResponse, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return "", err
	}
	var parsed assetResponse
	if err := common.Unmarshal(dataResponse, &parsed); err != nil || response.StatusCode < 200 || response.StatusCode >= 300 || parsed.Asset.ID == "" || strings.TrimSpace(parsed.URL) == "" {
		detail := strings.TrimSpace(string(dataResponse))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return "", errors.Errorf("youzan wan3 asset upload failed: HTTP %d: %s", response.StatusCode, detail)
	}
	if parsed.Asset.Kind != "" && parsed.Asset.Kind != assetKind {
		return "", errors.Errorf("youzan wan3 asset kind mismatch: requested %s, got %s", assetKind, parsed.Asset.Kind)
	}
	return parsed.URL, nil
}

func (a *TaskAdaptor) buildRequestBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	upstreamModel, err := upstreamModelFor(req.Model)
	if err != nil {
		return nil, err
	}
	media, err := buildMedia(req)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(media) == 0 {
		return nil, errors.New("youzan wan3 requires prompt or media")
	}
	body := map[string]interface{}{
		"model":      upstreamModel,
		"prompt":     prompt,
		"ratio":      resolveRatio(req),
		"resolution": resolveResolution(req),
		"duration":   resolveDuration(req),
	}
	if len(media) == 0 {
		return common.Marshal(body)
	}
	// 2026-10-28 协议：素材先 POST /v1/assets 上传，再把回执里的签名 url 按数组顺序
	// 放进 media，且 type 必须等于回执的 asset.kind。旧协议那套
	// conversationId + mentions + @alias 占位符已被上游整体移除。
	items := make([]map[string]string, 0, len(media))
	for _, item := range media {
		assetURL, err := a.uploadAsset(item.Type, item.URL)
		if err != nil {
			return nil, err
		}
		items = append(items, map[string]string{"type": item.Type, "url": assetURL})
	}
	body["media"] = items
	return common.Marshal(body)
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	modelName := billingModelName(info)
	if strings.TrimSpace(req.Model) == "" {
		req.Model = modelName
	}
	if strings.TrimSpace(modelName) == "" {
		modelName = req.Model
	}
	// 两个在售模型均为按秒计费：quota = ModelPrice × seconds × size。
	// prime 的 720P / 1080P 分档差价体现在 size 倍率（见 primeResolutionSizeRatio），
	// 不再像旧版那样整条模型按次计价。
	//
	// 带参考视频时把参考视频总秒数并入 seconds（口径同 wan3.0-video-官网）：
	// 2 秒生成 + 3 秒参考视频 = 按 5 秒收费。秒数由 validate 阶段探测并写入 context，
	// 这里只读不算 —— 探测失败已在 validate 阶段 400 拒掉，走到这里必然有值。
	resolution := resolveResolutionForModel(req, modelName)
	ratio := resolutionRatioForModel(modelName, resolution)
	seconds := resolveDurationForModel(req, modelName)
	if value, ok := c.Get(refVideoSecondsContextKey); ok {
		if refSeconds, ok := value.(int); ok && refSeconds > 0 {
			seconds += refSeconds
		}
	}
	return map[string]float64{"seconds": float64(seconds), "size": ratio}
}

// billingModelName 取本次请求的下游模型名（优先 OriginModelName，回退上游名）。
func billingModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if name := strings.TrimSpace(info.OriginModelName); name != "" {
		return name
	}
	if info.ChannelMeta != nil {
		return strings.TrimSpace(info.ChannelMeta.UpstreamModelName)
	}
	return ""
}

// resolutionRatioForModel 返回对应模型的分辨率倍率，未知档位回退 1。
//
// 优先读「上游动态调价」算出的倍率（setting/youzanwan3_setting），
// 没有配置时回退到下面的代码常量 —— 保证首次部署、或同步任务还没跑过时行为不变。
//
// 两个别名各有一套：wan3.0-video-prime-1080p 跟 prime 走（读 prime 的倍率），
// seedance2.5-特惠 独立定价（读它自己的倍率，见 isTehuiModel 分支）。
func resolutionRatioForModel(model, resolution string) float64 {
	if isR2VModel(model) {
		if ratio := r2vResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if isTehuiModel(model) {
		// 独立定价：先看它自己的动态倍率，读不到用自己那张常量表。
		// 注意顺序 —— 必须排在 isPrimeModel 之前，否则会读到 prime 的倍率。
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(tehuiModel, resolution); ok {
			return ratio
		}
		if ratio := tehuiResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if isPrimeModel(model) {
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(primeModel, resolution); ok {
			return ratio
		}
		if ratio := primeResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if ratio, ok := youzanwan3_setting.GetResolutionRatio(videoModel, resolution); ok {
		return ratio
	}
	if ratio := videoResolutionSizeRatio[resolution]; ratio > 0 {
		return ratio
	}
	return 1
}

// idempotencyKey 由公开 task id 派生：上游 /v1/tasks 强制要求 Idempotency-Key
// （8—100 位，仅字母/数字/下划线/连字符），同一编号不得改任务内容。
// 用公开 task id 派生可让「本地重试同一次提交」命中上游幂等窗口，避免重复扣费。
func idempotencyKey(info *relaycommon.RelayInfo) string {
	raw := ""
	if info != nil {
		raw = strings.TrimSpace(info.PublicTaskID)
	}
	var builder strings.Builder
	for _, char := range raw {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z', char >= '0' && char <= '9', char == '_', char == '-':
			builder.WriteRune(char)
		default:
			builder.WriteByte('-')
		}
	}
	key := builder.String()
	if len(key) < 8 {
		key = "wan3task-" + key
	}
	if len(key) > 100 {
		key = key[:100]
	}
	if len(key) < 8 {
		key = "wan3task-fallback"
	}
	return key
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(a.baseURL, "/") + "/v1/tasks", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey(info))
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	if info != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		req.Model = info.UpstreamModelName
	}
	body, err := a.buildRequestBody(req)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// 2xx 统一归一成 200：框架 relay_task.go 仅认 200，而上游提交可能返回 202，
	// 不归一会被误判为 fail_to_fetch_task（任务其实已受理并计费）。
	if resp != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 && resp.StatusCode != http.StatusOK {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()
	var parsed generateResponse
	if err := common.Unmarshal(data, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_body_failed", http.StatusBadGateway)
	}
	taskID := parsed.ID
	if taskID == "" {
		taskID = parsed.TaskID
	}
	if taskID == "" {
		// V1 协议把 task id 放在 task 对象里（{"ok":true,"task":{"id":"wz_..."}}），
		// 这一支必须查，否则所有正常建单都会退化成「empty task id」。
		taskID = parsed.Task.ID
	}
	if taskID == "" {
		taskID = parsed.Data.ID
	}
	if taskID == "" {
		// 先把上游的真实错误透出去：ok=false 或带 error 字段时上游没建任务，
		// 直接报「空 task id」会掩盖真实原因（入参非法 / 积分不足 / 上游故障）。
		if upstreamMsg := string(parsed.Error); upstreamMsg != "" {
			return "", nil, service.TaskErrorWrapper(
				errors.New("youzan wan3 rejected the request: "+upstreamMsg),
				"upstream_error",
				http.StatusBadRequest,
			)
		}
		if parsed.Message != "" {
			return "", nil, service.TaskErrorWrapper(
				errors.New("youzan wan3 rejected the request: "+parsed.Message),
				"upstream_error",
				http.StatusBadRequest,
			)
		}
		return "", nil, service.TaskErrorWrapper(errors.New("youzan wan3 returned empty task id"), "invalid_response", http.StatusBadGateway)
	}
	publicID := ""
	modelName := ""
	if info != nil {
		publicID = info.PublicTaskID
		modelName = info.PublicModelName()
	}
	c.JSON(http.StatusOK, gin.H{
		"id":         publicID,
		"object":     "video",
		"model":      modelName,
		"status":     dto.VideoStatusQueued,
		"progress":   0,
		"created_at": time.Now().Unix(),
	})
	return taskID, data, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		taskID, ok = body["task_id"].(string)
	}
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid youzan wan3 task id")
	}
	client, err := getHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodGet, normalizeAPIRoot(baseURL)+"/v1/tasks/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	return client.Do(request)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response taskResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}
	// 2026-10-28 起 /v1/tasks 把状态放在 task 对象里（task.status / task.videoUrl /
	// task.errorCode / task.errorMessage）。旧的扁平字段（status / result.url）保留
	// 兼容读取，因为库里按老协议落库的历史任务还要在轮询里正常收敛。
	status := strings.ToLower(strings.TrimSpace(firstNonEmpty(response.Task.Status, response.Status)))
	resultURL := firstNonEmpty(response.Task.VideoURL, response.Result.URL)
	reason := firstNonEmpty(
		string(response.Task.ErrorMessage),
		// 只有 errorCode 时也要把它带出来：比兜底的「内容审核不通过」更接近真相，
		// 也能避免把参数类失败误报成审核问题。
		response.Task.ErrorCode,
		string(response.Task.Error),
		string(response.Error),
		response.Message,
	)
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending", "submitted", "submitting":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "succeeded", "success", "completed", "done":
		if resolved := a.resolveResultURL(resultURL); resolved == "" {
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
		} else {
			result.Status = model.TaskStatusSuccess
			result.Progress = taskcommon.ProgressComplete
			result.Url = resolved
		}
	case "failed", "failure", "canceled", "cancelled", "error", "refunded":
		// refunded 是有赞的终态：上游已把点数退回（refundedPoints > 0）且 result 为空。
		// 不映射成失败的话会落进 default 继续报 IN_PROGRESS，用户的钱就卡在预扣里。
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(reason, reasonContentModeration)
	default:
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (a *TaskAdaptor) GetModelList() []string { return ModelList }
func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	result := &dto.OpenAIVideo{
		ID:          task.TaskID,
		Object:      "video",
		Model:       task.Properties.OriginModelName,
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.UpdatedAt,
	}
	result.Status = dto.VideoStatusUnknown
	switch task.Status {
	case model.TaskStatusQueued:
		result.Status = dto.VideoStatusQueued
	case model.TaskStatusInProgress:
		result.Status = dto.VideoStatusInProgress
	case model.TaskStatusSuccess:
		result.Status = dto.VideoStatusCompleted
	case model.TaskStatusFailure:
		result.Status = dto.VideoStatusFailed
	}
	result.SetProgressStr(task.Progress)
	if task.GetResultURL() != "" {
		result.SetMetadata("url", task.GetResultURL())
	}
	if task.Status == model.TaskStatusFailure {
		result.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(task.FailReason, reasonContentModeration),
		}
	}
	return common.Marshal(result)
}
