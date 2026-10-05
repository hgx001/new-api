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
	"mime/multipart"
	"net/http"
	"net/textproto"
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

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

const maxAssetBytes = 64 << 20

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
	Data   struct {
		ID string `json:"id"`
	} `json:"data"`
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
	Status string `json:"status"`
	Result struct {
		URL string `json:"url"`
	} `json:"result"`
	Error   upstreamError `json:"error"`
	Message string        `json:"message"`
}

type conversationResponse struct {
	ID string `json:"id"`
}

type assetResponse struct {
	Asset struct {
		ID           string `json:"id"`
		DisplayAlias string `json:"displayAlias"`
	} `json:"asset"`
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
	if _, mediaErr := buildMedia(req); mediaErr != nil {
		return service.TaskErrorWrapperLocal(mediaErr, "invalid_request", http.StatusBadRequest)
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
	return strings.TrimSpace(model) == primeModel
}

func normalizeResolutionForModel(model, resolution string) string {
	if isPrimeModel(model) {
		// 上游该档位仅支持 1080P，请求里的其它分辨率一律按 1080P 执行。
		return primeResolution
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
	if isPrimeModel(req.Model) {
		// 上游该档位固定 30 秒，忽略请求里的其它时长。
		return primeDuration
	}
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(req.Seconds)
	}
	if duration <= 0 {
		return defaultDuration
	}
	if isR2VModel(req.Model) {
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
		case "first_frame", "last_frame", "reference_image", "reference_video", "reference_audio":
			counts[item.Type]++
		default:
			return errors.Errorf("youzan wan3 media type %q is unsupported", item.Type)
		}
	}
	if counts["first_frame"] > 1 || counts["last_frame"] > 1 {
		return errors.New("youzan wan3 accepts at most one first_frame and one last_frame")
	}
	// 上游文档：首尾帧单独使用，不能和普通参考素材混用；
	// 混用会被上游拒绝（WAN3_API_REFERENCE_MODE / WAN3_REFERENCE_MODE_CONFLICT）。
	if counts["first_frame"]+counts["last_frame"] > 0 &&
		counts["reference_image"]+counts["reference_video"]+counts["reference_audio"] > 0 {
		return errors.New("youzan wan3 does not accept first_frame/last_frame mixed with reference media")
	}
	if isR2VModel(model) && counts["first_frame"]+counts["last_frame"] > 0 {
		// wan2.7-r2v 上游只支持参考图，首尾帧会被静默丢弃，这里直接拒绝。
		return errors.New("youzan wan2.7-r2v does not accept first_frame/last_frame")
	}
	if isR2VModel(model) && counts["reference_image"] == 0 {
		// 上游要求至少 1 张参考图，本地先拦住，不消耗上游调用。
		return errors.New("wan2.7-r2v requires at least one reference image")
	}
	if isR2VModel(model) && counts["reference_video"]+counts["reference_audio"] > 0 {
		// r2v 只发参考图，参考视频/音频会被静默丢弃，这里直接拒绝。
		return errors.New("youzan wan2.7-r2v does not accept reference video or audio")
	}
	imageLimit := maxReferenceImages
	switch {
	case isR2VModel(model):
		imageLimit = r2vMaxReferenceImages
	case isPrimeModel(model):
		imageLimit = primeMaxReferenceImages
	}
	if counts["reference_image"] > imageLimit {
		return errors.Errorf("youzan wan3 accepts at most %d reference images", imageLimit)
	}
	if counts["reference_video"] > maxReferenceVideos {
		return errors.Errorf("youzan wan3 accepts at most %d reference videos", maxReferenceVideos)
	}
	if counts["reference_audio"] > maxReferenceAudios {
		return errors.Errorf("youzan wan3 accepts at most %d reference audios", maxReferenceAudios)
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

func (a *TaskAdaptor) requestJSON(method, endpoint string, body []byte) ([]byte, int, error) {
	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return nil, 0, err
	}
	request, err := http.NewRequest(method, strings.TrimRight(a.baseURL, "/")+endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, err
	}
	defer response.Body.Close()
	data, readErr := io.ReadAll(io.LimitReader(response.Body, maxAssetBytes))
	return data, response.StatusCode, readErr
}

func (a *TaskAdaptor) createConversation() (string, error) {
	body, err := common.Marshal(map[string]string{"title": "Wan3 工作台任务"})
	if err != nil {
		return "", err
	}
	data, status, err := a.requestJSON(http.MethodPost, "/api/conversations", body)
	if err != nil {
		return "", err
	}
	var response conversationResponse
	if err := common.Unmarshal(data, &response); err != nil || status < 200 || status >= 300 || response.ID == "" {
		return "", errors.Errorf("youzan wan3 conversation failed: HTTP %d", status)
	}
	return response.ID, nil
}

func (a *TaskAdaptor) uploadAsset(conversationID, kind, source string) (string, string, error) {
	data, mimeType, sourceName, err := a.readAsset(source)
	if err != nil {
		return "", "", err
	}
	if kind == "image" {
		data, mimeType, err = flattenImage(data)
		if err != nil {
			return "", "", err
		}
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("conversationId", conversationID); err != nil {
		return "", "", err
	}
	if err := writer.WriteField("kind", kind); err != nil {
		return "", "", err
	}
	// Upstream validates the file part's declared MIME against the actual
	// content (MEDIA_MIME_MISMATCH otherwise); CreateFormFile would send
	// application/octet-stream, so build the part header explicitly.
	contentType := strings.TrimSpace(mimeType)
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", `form-data; name="file"; filename=`+strconv.Quote(fileName(kind, sourceName, mimeType)))
	partHeader.Set("Content-Type", contentType)
	part, err := writer.CreatePart(partHeader)
	if err != nil {
		return "", "", err
	}
	if _, err = part.Write(data); err != nil {
		return "", "", err
	}
	if err := writer.Close(); err != nil {
		return "", "", err
	}
	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return "", "", err
	}
	request, err := http.NewRequest(http.MethodPost, strings.TrimRight(a.baseURL, "/")+"/api/multimodal-assets", &body)
	if err != nil {
		return "", "", err
	}
	request.Header.Set("Authorization", "Bearer "+a.apiKey)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		return "", "", err
	}
	defer response.Body.Close()
	dataResponse, err := io.ReadAll(io.LimitReader(response.Body, maxAssetBytes))
	if err != nil {
		return "", "", err
	}
	var parsed assetResponse
	if err := common.Unmarshal(dataResponse, &parsed); err != nil || response.StatusCode < 200 || response.StatusCode >= 300 || parsed.Asset.ID == "" {
		detail := strings.TrimSpace(string(dataResponse))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return "", "", errors.Errorf("youzan wan3 asset upload failed: HTTP %d: %s", response.StatusCode, detail)
	}
	alias := parsed.Asset.DisplayAlias
	if alias == "" {
		alias = kind + "1"
	}
	return parsed.Asset.ID, alias, nil
}

func validatePromptReferences(prompt string, media []relaycommon.TaskMedia) error {
	counts := map[string]int{}
	for _, item := range media {
		counts[item.Type]++
	}
	checks := []struct {
		pattern string
		count   int
		label   string
	}{
		{`@(?:image|图片)\d+`, counts["reference_image"], "图片"},
		{`@(?:video|视频)\d+`, counts["reference_video"], "视频"},
		{`@(?:audio|音频)\d+`, counts["reference_audio"], "音频"},
	}
	for _, check := range checks {
		matches := len(regexp.MustCompile(check.pattern).FindAllString(prompt, -1))
		if matches > check.count {
			return errors.Errorf("提示词引用的%s素材超过已提供数量", check.label)
		}
	}
	return nil
}

func replaceLegacyToken(prompt, kind, token string, index int) string {
	patterns := map[string][]string{
		"image": {"@image" + strconv.Itoa(index), "@图片" + strconv.Itoa(index)},
		"video": {"@video" + strconv.Itoa(index), "@视频" + strconv.Itoa(index)},
		"audio": {"@audio" + strconv.Itoa(index), "@音频" + strconv.Itoa(index)},
	}
	for _, candidate := range patterns[kind] {
		if strings.Contains(prompt, candidate) {
			return strings.Replace(prompt, candidate, token, 1)
		}
	}
	return prompt
}

func (a *TaskAdaptor) buildRequestBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	media, err := buildMedia(req)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(media) == 0 {
		return nil, errors.New("youzan wan3 requires prompt or media")
	}
	if err := validatePromptReferences(prompt, media); err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"model":      req.Model,
		"prompt":     prompt,
		"ratio":      resolveRatio(req),
		"resolution": resolveResolution(req),
		"duration":   resolveDuration(req),
	}
	if isR2VModel(req.Model) {
		// r2v 走上游扁平参考图字段（referenceImages）；assets + mentions 是
		// all_in_one 智能调度的约定，r2v 不识别，会导致上游报缺少参考图。
		referenceImages := make([]string, 0, len(media))
		for _, item := range media {
			if item.Type == "reference_image" {
				referenceImages = append(referenceImages, item.URL)
			}
		}
		if len(referenceImages) == 0 {
			// validateMedia 已拦住缺图情况，这里只是防御性检查。
			return nil, errors.New("wan2.7-r2v requires at least one reference image")
		}
		body["referenceImages"] = referenceImages
		return common.Marshal(body)
	}
	if req.Metadata != nil {
		if audio, ok := metadataBool(req.Metadata, "audio", "generate_audio"); ok {
			body["audio"] = audio
		} else {
			body["audio"] = defaultAudio
		}
	} else {
		body["audio"] = defaultAudio
	}

	if len(media) == 0 {
		return common.Marshal(body)
	}
	conversationID, err := a.createConversation()
	if err != nil {
		return nil, err
	}
	body["conversationId"] = conversationID
	mentions := make([]map[string]string, 0)
	kindIndices := make(map[string]int)
	for _, item := range media {
		kind := "image"
		switch item.Type {
		case "reference_video":
			kind = "video"
		case "reference_audio":
			kind = "audio"
		case "first_frame", "last_frame":
			kind = "image"
		}
		assetID, alias, err := a.uploadAsset(conversationID, kind, item.URL)
		if err != nil {
			return nil, err
		}
		switch item.Type {
		case "first_frame":
			body["firstFrameAssetId"] = assetID
		case "last_frame":
			body["lastFrameAssetId"] = assetID
		default:
			kindIndices[kind]++
			token := "@" + alias
			prompt = replaceLegacyToken(prompt, kind, token, kindIndices[kind])
			if !strings.Contains(prompt, token) {
				prompt = strings.TrimSpace(prompt + " " + token)
			}
			mentions = append(mentions, map[string]string{"token": token, "assetId": assetID})
		}
	}
	body["prompt"] = prompt
	if len(mentions) > 0 {
		body["mentions"] = mentions
	}
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
	if isPrimeModel(modelName) {
		// 按次计费：ModelPrice 已是一次任务的价格，不能再乘时长/分辨率倍率。
		return nil
	}
	resolution := resolveResolutionForModel(req, modelName)
	ratio := resolutionRatioForModel(modelName, resolution)
	return map[string]float64{"seconds": float64(resolveDuration(req)), "size": ratio}
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
func resolutionRatioForModel(model, resolution string) float64 {
	if isR2VModel(model) {
		if ratio := r2vResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if ratio := smartResolutionSizeRatio[resolution]; ratio > 0 {
		return ratio
	}
	return 1
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(a.baseURL, "/") + "/api/generate-video", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
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
		taskID = parsed.Data.ID
	}
	if taskID == "" {
		return "", nil, service.TaskErrorWrapper(errors.New("youzan wan3 returned empty task id"), "invalid_response", http.StatusBadGateway)
	}
	publicID := ""
	modelName := ""
	if info != nil {
		publicID = info.PublicTaskID
		modelName = info.OriginModelName
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
	request, err := http.NewRequest(http.MethodGet, normalizeAPIRoot(baseURL)+"/api/task/"+url.PathEscape(taskID), nil)
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
	status := strings.ToLower(strings.TrimSpace(response.Status))
	result := &relaycommon.TaskInfo{Code: 0}
	switch status {
	case "queued", "pending", "submitted", "submitting":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "succeeded", "success", "completed", "done":
		if resultURL := a.resolveResultURL(response.Result.URL); resultURL == "" {
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
		} else {
			result.Status = model.TaskStatusSuccess
			result.Progress = taskcommon.ProgressComplete
			result.Url = resultURL
		}
	case "failed", "failure", "canceled", "cancelled", "error", "refunded":
		// refunded 是有赞的终态：上游已把点数退回（refundedPoints > 0）且 result 为空。
		// 不映射成失败的话会落进 default 继续报 IN_PROGRESS，用户的钱就卡在预扣里。
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(string(response.Error), response.Message, reasonContentModeration)
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
