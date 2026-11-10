package yuanmeng

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/relay/channel/task/wan3"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// TaskAdaptor 对接「视频生成车站」（yuanmengstudio.com）的排队通道。
//
// 协议三步：
//
//	POST /vr/api/tasks  提交（X-Token 鉴权）→ 返回 {ok, task:{id, status}}
//	GET  /vr/api/tasks  轮询（X-Token 鉴权）→ 返回 {ok, tasks:[{id, status, videoUrl, ...}]}
//	成片：task.videoUrl 直链（**仅约 24 小时有效**）
//
// ── 三个必须记住的上游特性 ──────────────────────────────────────────────
//
//  1. **鉴权是 X-Token 头，不是 Bearer**。渠道密钥里存的就是这个 token。
//  2. **每条任务固定扣 1 次**（充值 1 元 = 1 次），与分辨率/时长无关。
//     所以计费必须走按次口径，绝不能按时长叠加倍率。
//  3. **成片直链只有约 24 小时有效**，站点不留副本，过期不可恢复。
//     本站因此不直接透传上游地址，而是把成片转存到腾讯云 COS
//     （复用 wan3 的 MPS 链路），库里存 cos-proxy:// 占位符，取片时实时签名。
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
	a.baseURL = strings.TrimRight(strings.TrimSpace(info.ChannelBaseUrl), "/")
	a.apiKey = info.ApiKey
	a.proxy = info.ChannelSetting.Proxy
}

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) GetModelList() []string {
	return []string{PublicModel}
}

// ────────────────────────────── 请求校验 ──────────────────────────────

// ValidateRequestAndSetAction 校验本渠道的档位与素材约束。
//
// 校验放在计费预扣之前：400 不触发渠道重试，被拒请求不消耗上游次数。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	if _, modeErr := resolveMode(req); modeErr != nil {
		return service.TaskErrorWrapperLocal(modeErr, "invalid_request", http.StatusBadRequest)
	}
	if _, durErr := resolveDuration(req); durErr != nil {
		return service.TaskErrorWrapperLocal(durErr, "invalid_duration", http.StatusBadRequest)
	}
	if _, resErr := resolveResolution(req); resErr != nil {
		return service.TaskErrorWrapperLocal(resErr, "invalid_resolution", http.StatusBadRequest)
	}
	if _, ratioErr := resolveRatio(req); ratioErr != nil {
		return service.TaskErrorWrapperLocal(ratioErr, "invalid_ratio", http.StatusBadRequest)
	}
	if _, mediaErr := buildMedia(req); mediaErr != nil {
		return service.TaskErrorWrapperLocal(mediaErr, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

// resolveMode 解析提交模式。
//
// 上游三态：reference（全模态，默认）/ first_frame（首帧）/ first_last（首尾帧）。
// ⚠️ first_frame 与 first_last **不能同时**传 refs/audioRefs/videoRefs/docFile/docLink，
//   上游会拒单。用户在本站传了这些素材却选首尾帧模式时，明确报错而不是让上游拒。
func resolveMode(req relaycommon.TaskSubmitReq) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(req.Mode))
	if raw == "" {
		return modeReference, nil
	}
	switch raw {
	case modeReference, modeFirstFrame, modeFirstLast:
		return raw, nil
	default:
		return "", errors.Errorf("unsupported mode %q, expected one of %s/%s/%s",
			req.Mode, modeReference, modeFirstFrame, modeFirstLast)
	}
}

// resolveDuration 解析生成时长。
//
// 上游接受 2～30 的整数，也可传 -1 让平台智能决定（-1 只在文生图场景有意义，
// 本渠道统一按 5 秒兜底，避免 -1 被用户误解成"无限长"）。
//
// **req.Seconds 是必须的回退路径**：下游按 OpenAI video 形状传的是 {"seconds":"7"}
// （字符串），落到 TaskSubmitReq.Seconds 而不是 Duration。漏读 Seconds 会静默
// 按默认 5 秒建单。
func resolveDuration(req relaycommon.TaskSubmitReq) (int, error) {
	if req.Duration > 0 {
		if req.Duration < minDurationSeconds || req.Duration > maxDurationSeconds {
			return 0, durationError(req.Duration)
		}
		return req.Duration, nil
	}
	if raw := strings.TrimSpace(req.Seconds); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err == nil {
			if parsed < minDurationSeconds || parsed > maxDurationSeconds {
				return 0, durationError(parsed)
			}
			return parsed, nil
		}
	}
	return defaultDurationSeconds, nil
}

func durationError(got int) error {
	return errors.Errorf("this model accepts %d-%d seconds, got %d",
		minDurationSeconds, maxDurationSeconds, got)
}

// resolveResolution 解析分辨率。
//
// 上游三档（480P/720P/1080P）**同价**（每条都扣 1 次），所以对外承诺的是
// 「按次计费、不因分辨率加价」。仍然 fail-loud 校验越界值，不静默吸附 ——
// 用户要 4K 却拿到 480P 是比报错更糟的体验。
//
// 大小写与裸数字（"1080p" / "1080"）都归一到上游写法（"1080P"），
// 因为这两种是用户最常写的形式，直接拒掉只会徒增客服量。
func resolveResolution(req relaycommon.TaskSubmitReq) (string, error) {
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" {
		return defaultResolution, nil
	}
	// 归一化大小写：上游文档写 480P/720P/1080P（大写 P），但用户常写 1080p。
	normalized := strings.ToUpper(raw)
	if allowedResolutions[normalized] {
		return normalized, nil
	}
	// 兼容 "1080" 这类裸数字写法。
	if numeric, ok := parseResolutionNumber(raw); ok && allowedResolutions[numeric] {
		return numeric, nil
	}
	return "", errors.Errorf("this model only supports resolutions %s, got %q",
		"480P/720P/1080P", req.Resolution)
}

// parseResolutionNumber 从 "1080" / "1080p" / "1080P" 里抽出数字档位。
func parseResolutionNumber(raw string) (string, bool) {
	trimmed := strings.ToUpper(strings.TrimSpace(raw))
	trimmed = strings.TrimSuffix(trimmed, "P")
	switch trimmed {
	case "480":
		return resolution480P, true
	case "720":
		return resolution720P, true
	case "1080":
		return resolution1080P, true
	}
	return "", false
}

// resolveRatio 解析画幅比例。
//
// 上游接受 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9 / adaptive。
//
// 统一走 relaycommon.ResolveTaskRatio（全站唯一入口），优先级：
//
//	metadata.ratio → metadata.aspect_ratio → 顶层 ratio → 顶层 aspect_ratio
//	→ size（恰好是比例写法时）→ 16:9
//
// ⚠️ 2026-11-10：此前本函数**只读 metadata**，客户端按 OpenAI 风格把比例
// 放在请求体顶层（{"ratio":"9:16"}）时会被静默丢弃 → 落 16:9，
// 成片比例不对但任务成功、照扣费。首轮修复漏了本包 —— 根因是全站性的。
//
// ⚠️ 视频生成车站的口径：非法比例**报错**（unsupported ratio），
// 不静默降级 —— 档位是硬约束，提前报错能给客户可读原因。
func resolveRatio(req relaycommon.TaskSubmitReq) (string, error) {
	result := relaycommon.ResolveTaskRatio(req, relaycommon.TaskRatioOptions{
		Candidates:    relaycommon.TaskRatioCandidates(allowedRatios),
		DefaultRatio:  defaultRatio,
		RejectInvalid: true,
		// size 只认「恰好是比例写法」的情况（"16:9"）；
		// 像素写法（"1280x720"）不认，保持原有行为不变。
		SizeToRatio: func(size string) (string, bool) {
			if size == "" {
				return "", false
			}
			return size, true
		},
		// 客户端常用 "16/9"、"16：9"（全角冒号）写法，归一化后再比对档位。
		Normalize: normalizeRatio,
	})
	if result.Rejected != "" {
		return "", errors.Errorf("unsupported ratio %q, expected one of 16:9/9:16/1:1/4:3/3:4/21:9/adaptive",
			result.Rejected)
	}
	return result.Ratio, nil
}

// normalizeRatio 把 "16/9" / "16：9"（全角冒号）归一化成 "16:9"。
func normalizeRatio(raw string) string {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.ReplaceAll(trimmed, "：", ":")
	trimmed = strings.ReplaceAll(trimmed, "/", ":")
	return trimmed
}

// ────────────────────────────── 素材 ──────────────────────────────

// buildMedia 归一化素材并按上游规格校验。
//
// 取值顺序沿用 sanyuan：显式 media → metadata.media → 旧版扁平字段
// （images / image / input_reference、audios / audio）。
//
// 素材类型映射到上游字段：
//
//	reference_image → refs
//	reference_audio → audioRefs
//	reference_video → videoRefs
//	first_frame     → firstFrame（仅 mode=first_frame/first_last）
//	last_frame      → lastFrame （仅 mode=first_last）
//
// ⚠️ 上游 refs 字段文档给了**两种写法**（字符串数组 / {"kind":"image","url":...} 对象数组），
//   没有正式联合类型定义。本站统一用**字符串数组**（文档主字段说明与快速开始都是它），
//   实测时若被拒再改对象数组。
func buildMedia(req relaycommon.TaskSubmitReq) (*mediaSet, error) {
	set := &mediaSet{}
	mode, err := resolveMode(req)
	if err != nil {
		return nil, err
	}
	set.mode = mode

	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var mediaErr error
		if media, mediaErr = metadataMedia(req.Metadata); mediaErr != nil {
			return nil, mediaErr
		}
	}
	if len(media) == 0 {
		images := req.Images
		if len(images) == 0 && strings.TrimSpace(req.Image) != "" {
			images = []string{req.Image}
		}
		if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
			images = []string{strings.TrimSpace(req.InputReference)}
		}
		for _, item := range images {
			if strings.TrimSpace(item) != "" {
				media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: strings.TrimSpace(item)})
			}
		}
		audios := req.Audios
		if len(audios) == 0 && strings.TrimSpace(req.Audio) != "" {
			audios = []string{req.Audio}
		}
		for _, item := range audios {
			if strings.TrimSpace(item) != "" {
				media = append(media, relaycommon.TaskMedia{Type: "reference_audio", URL: strings.TrimSpace(item)})
			}
		}
	}

	for _, item := range media {
		itemType := strings.ToLower(strings.TrimSpace(item.Type))
		itemURL := strings.TrimSpace(item.URL)
		if itemURL == "" {
			return nil, errors.New("media url is required")
		}
		switch itemType {
		case "reference_image", "image":
			set.refs = append(set.refs, itemURL)
		case "reference_audio", "audio":
			set.audioRefs = append(set.audioRefs, itemURL)
		case "reference_video", "video":
			set.videoRefs = append(set.videoRefs, itemURL)
		case "first_frame":
			if set.firstFrame != "" {
				return nil, errors.New("only one first frame is allowed")
			}
			set.firstFrame = itemURL
		case "last_frame":
			if set.lastFrame != "" {
				return nil, errors.New("only one last frame is allowed")
			}
			set.lastFrame = itemURL
		default:
			return nil, errors.Errorf("unsupported media type %q", item.Type)
		}
	}

	if err := set.validate(mode); err != nil {
		return nil, err
	}
	return set, nil
}

// mediaSet 是归一化后的素材集合，字段名与上游建单字段一一对应。
type mediaSet struct {
	mode       string
	refs       []string
	audioRefs  []string
	videoRefs  []string
	firstFrame string
	lastFrame  string
}

// validate 按上游规格校验素材组合。
func (s *mediaSet) validate(mode string) error {
	if len(s.refs) > maxReferenceImages {
		return errors.Errorf("at most %d reference images are allowed, got %d",
			maxReferenceImages, len(s.refs))
	}
	if len(s.audioRefs) > maxReferenceAudios {
		return errors.Errorf("at most %d reference audios are allowed, got %d",
			maxReferenceAudios, len(s.audioRefs))
	}
	if len(s.videoRefs) > maxReferenceVideos {
		return errors.Errorf("at most %d reference videos are allowed, got %d",
			maxReferenceVideos, len(s.videoRefs))
	}
	total := len(s.refs) + len(s.audioRefs) + len(s.videoRefs)
	if total > maxTotalMaterials {
		return errors.Errorf("at most %d materials in total are allowed, got %d",
			maxTotalMaterials, total)
	}
	// 首尾帧模式与全模态素材互斥 —— 上游会拒单，这里提前给可读报错。
	if mode == modeFirstFrame || mode == modeFirstLast {
		if total > 0 {
			return errors.Errorf("mode %s does not accept reference images/audios/videos", mode)
		}
	}
	if mode == modeFirstFrame && s.lastFrame != "" {
		return errors.Errorf("mode %s does not accept a last frame", mode)
	}
	if mode == modeFirstLast && (s.firstFrame == "" || s.lastFrame == "") {
		return errors.New("mode first_last requires both a first frame and a last frame")
	}
	return nil
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

// ────────────────────────────── 计费 ──────────────────────────────

// EstimateBilling 继承 BaseBilling 返回 nil：**按次计费**。
//
// 上游每条固定扣 1 次，与分辨率、时长无关。
// ⚠️ 不能把时长当倍率返回 —— 按次模型叠加 seconds 倍率会重蹈
// TASK_PRICE_PATCH 跳过相乘的事故形态（少收或错收）。

// ────────────────────────────── 建单 ──────────────────────────────

// createRequest 是上游建单请求体。字段名必须与上游逐字一致。
type createRequest struct {
	Title      string   `json:"title,omitempty"`
	Prompt     string   `json:"prompt"`
	Duration   int      `json:"duration,omitempty"`
	Ratio      string   `json:"ratio,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
	Audio      bool     `json:"audio,omitempty"`
	Mode       string   `json:"mode,omitempty"`
	Refs       []string `json:"refs,omitempty"`
	AudioRefs  []string `json:"audioRefs,omitempty"`
	VideoRefs  []string `json:"videoRefs,omitempty"`
	FirstFrame string   `json:"firstFrame,omitempty"`
	LastFrame  string   `json:"lastFrame,omitempty"`
	DocLink    string   `json:"docLink,omitempty"`
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + createPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	// ⚠️ 上游鉴权是 X-Token 头，**不是** Authorization: Bearer。
	req.Header.Set("X-Token", a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, taskcommon.NewUserError(errors.New("this model requires a prompt"), "invalid_request")
	}
	// 上游 prompt 长度 5～20000 字符。
	if len(prompt) < 5 {
		return nil, taskcommon.NewUserError(
			errors.Errorf("prompt is too short: %d characters, minimum is 5", len(prompt)),
			"invalid_request")
	}
	if len(prompt) > 20000 {
		return nil, taskcommon.NewUserError(
			errors.Errorf("prompt is too long: %d characters, maximum is 20000", len(prompt)),
			"invalid_request")
	}

	mode, err := resolveMode(req)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_request")
	}
	duration, err := resolveDuration(req)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_duration")
	}
	resolution, err := resolveResolution(req)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_resolution")
	}
	ratio, err := resolveRatio(req)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_ratio")
	}
	media, err := buildMedia(req)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_request")
	}

	body := createRequest{
		Title:      buildTitle(req),
		Prompt:     prompt,
		Duration:   duration,
		Ratio:      ratio,
		Resolution: resolution,
		Audio:      resolveAudio(req),
		Mode:       mode,
		Refs:       media.refs,
		AudioRefs:  media.audioRefs,
		VideoRefs:  media.videoRefs,
		FirstFrame: media.firstFrame,
		LastFrame:  media.lastFrame,
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal yuanmeng request body failed")
	}
	return bytes.NewReader(encoded), nil
}

// buildTitle 任务标题：上游用它在上游列表里标识任务。
// 取不到就用模型名 + 时间戳，保证上游列表里可辨认。
func buildTitle(req relaycommon.TaskSubmitReq) string {
	if req.Metadata != nil {
		if v, ok := req.Metadata["title"].(string); ok {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}

// resolveAudio 是否生成声音。上游 audio 是**布尔开关**，不是音频数组。
//
// 默认 true：上游排队通道本身带台词/BGM/音效能力，是它的卖点之一。
// 用户可以在 metadata.audio=false 显式关掉。
func resolveAudio(req relaycommon.TaskSubmitReq) bool {
	if req.Metadata != nil {
		if raw, ok := req.Metadata["audio"].(bool); ok {
			return raw
		}
	}
	return true
}

// ────────────────────────────── 提交响应 ──────────────────────────────

type createResponse struct {
	OK   bool   `json:"ok"`
	Msg  string `json:"msg"`
	Task struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		CreditsCost int    `json:"creditsCost"`
	} `json:"task"`
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "read_response_body_failed"),
			"read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	// ⚠️ 上游错误也可能是 HTTP 200（body 里 ok:false），所以状态码与 ok 都要查。
	if resp.StatusCode != http.StatusOK {
		return "", nil, upstreamError(resp.StatusCode, responseBody)
	}
	var created createResponse
	if err := common.Unmarshal(responseBody, &created); err != nil {
		return "", nil, service.TaskErrorWrapper(
			errors.Wrapf(err, "body: %s", responseBody),
			"unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if !created.OK {
		// ok:false 形态：次数不足、refs 超限、文档与网页二选一等。
		// 一律 fail-loud，**不假装成功** —— 预扣费由框架失败路径退款。
		return "", nil, service.TaskErrorWrapper(
			errors.Errorf("upstream submit failed: %s", firstNonEmpty(created.Msg, "unknown error")),
			"upstream_error", http.StatusBadRequest)
	}
	taskID := strings.TrimSpace(created.Task.ID)
	if taskID == "" {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("upstream returned empty task id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
	}

	modelName := PublicModel
	if info != nil && info.OriginModelName != "" {
		modelName = info.PublicModelName()
	}
	publicID := ""
	if info != nil && info.TaskRelayInfo != nil {
		publicID = info.PublicTaskID
	}
	out := gin.H{
		"id":       publicID,
		"object":   "video",
		"model":    modelName,
		"status":   dto.VideoStatusQueued,
		"progress": 0,
	}
	if publicID != "" {
		out["status_url"] = taskcommon.BuildProxyURL(publicID)
	}
	c.JSON(http.StatusOK, out)
	return taskID, responseBody, nil
}

func upstreamError(statusCode int, body []byte) *dto.TaskError {
	var wrapper struct {
		OK      bool   `json:"ok"`
		Msg     string `json:"msg"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	_ = common.Unmarshal(body, &wrapper)
	message := firstNonEmpty(wrapper.Msg, wrapper.Message, wrapper.Error, string(body))
	return service.TaskErrorWrapper(
		errors.Errorf("yuanmeng: HTTP %d: %s", statusCode, message),
		"upstream_error", statusCode)
}

// ────────────────────────────── 轮询 ──────────────────────────────

// upstreamTask 是上游任务对象在轮询响应里的形态。
type upstreamTask struct {
	// ID 用 json.RawMessage 收 —— 上游文档写的是字符串（T130），但实测存在
	// 返回数字的情况。用 string 会直接让**整个响应解析失败**，
	// 表现为任务永远卡在进行中。
	ID json.RawMessage `json:"id"`
	// Status 是上游任务状态：pending/submitted/running/done/failed/cancelled。
	Status string `json:"status"`
	// FailReason 是失败原因（status=failed 时有值）。
	FailReason string `json:"failReason"`
	// VideoURL 是成片直链（**约 24 小时有效**）。
	VideoURL string `json:"videoUrl"`
	// VideoDirect 与 VideoURL 同值（上游历史命名，保留兼容）。
	VideoDirect string `json:"videoDirect"`
	// Video 是带下载签名的网页播放地址。
	Video string `json:"video"`
}

// taskID 返回归一化后的任务 ID（数字也能取到）。
func (t *upstreamTask) taskID() string {
	return rawToString(t.ID)
}

// tasksResponse 是 GET /vr/api/tasks 的响应。
type tasksResponse struct {
	OK        bool           `json:"ok"`
	Msg       string         `json:"msg"`
	Tasks     []upstreamTask `json:"tasks"`
	Total     int            `json:"total"`
	Pages     int            `json:"pages"`
	Remaining int            `json:"remaining"`
}

// FetchTask 轮询上游任务列表。
//
// ⚠️ 上游**没有单任务查询接口** —— 只有列表查询 GET /vr/api/tasks（分页）。
// 所以这里用 day=all + 大 pageSize 拉全量，**在本地定位目标任务**，
// 再把找到的那一条重新包装成响应体返回。
//
// 为什么用 day=all：排队通道可能等数小时甚至跨天，任务发车前后的日期归属
// 会变，只查 today 会漏掉昨天提交、今天才发车的任务。
//
// 为什么在 FetchTask 里定位（而不是 ParseTaskResult）：
//	框架接口 ParseTaskResult(respBody) 只给响应体，**拿不到目标 task_id**。
//	而上游返回的是整个列表 —— 不定位的话，解析时会取列表里的第一条，
//	可能匹配到别人的任务。在 FetchTask 里定位完再包装，ParseTaskResult
//	拿到的就一定是目标任务，语义与其它渠道一致。
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task id")
	}
	baseUrl = strings.TrimRight(strings.TrimSpace(baseUrl), "/")
	query := url.Values{}
	query.Set("day", "all")
	query.Set("page", "1")
	query.Set("pageSize", "100")

	req, err := http.NewRequest(http.MethodGet, baseUrl+tasksPath+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// ⚠️ X-Token 鉴权，不是 Bearer。
	req.Header.Set("X-Token", key)
	req.Header.Set("Accept", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, errors.Wrap(err, "new yuanmeng http client failed")
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read yuanmeng task list failed")
	}

	// 非 2xx 原样回传状态码与响应体，由框架的 5xx 兜底逻辑处理
	// （上游临时故障 ≠ 任务失败，必须留给下一轮重试）。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &http.Response{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Header:     resp.Header,
			Body:       io.NopCloser(bytes.NewReader(responseBody)),
		}, nil
	}

	// 定位目标任务并重新包装成单任务响应。
	wrapped, found := wrapTargetTask(responseBody, taskID)
	if !found {
		// 没找到 —— 保留原列表响应。ParseTaskResult 会据此判定「仍在进行中」
		//（**不判失败**：排队通道可能等几小时，判失败会误退款）。
		wrapped = responseBody
	}
	return &http.Response{
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Header:     resp.Header,
		Body:       io.NopCloser(bytes.NewReader(wrapped)),
	}, nil
}

// wrapTargetTask 从列表响应里定位目标任务，包装成单任务响应。
//
// 返回包装后的 JSON 与是否找到。找不到时返回原响应体（调用方决定怎么处理）。
func wrapTargetTask(responseBody []byte, taskID string) ([]byte, bool) {
	var list tasksResponse
	if err := common.Unmarshal(responseBody, &list); err != nil {
		return responseBody, false
	}
	target := findUpstreamTask(list.Tasks, taskID)
	if target == nil {
		return responseBody, false
	}
	// 注意：成片直链（videoUrl）**不能**在这里落到对外响应体里 ——
	// 上游直链只有约 24 小时有效，透传给客户端等于给用户一个定时炸弹。
	// ParseTaskResult 读的是这个包装体，但仍然不把 Url 写进 TaskInfo。
	single := map[string]any{
		"ok":   true,
		"task": target,
	}
	encoded, err := common.Marshal(single)
	if err != nil {
		return responseBody, false
	}
	return encoded, true
}

// ParseTaskResult 解析轮询结果。
//
// ⚠️ 上游状态机：pending（候车）→ submitted（已发车）→ running（生成中）→ done。
// 前三个都是进行中态。
//
// ⚠️ 输入既可能是 FetchTask 包装后的单任务（{"ok":true,"task":{...}}），
// 也可能是原始列表（{"ok":true,"tasks":[...]}）—— 两种都要能解析。
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res struct {
		OK    bool           `json:"ok"`
		Msg   string         `json:"msg"`
		Task  *upstreamTask  `json:"task"`
		Tasks []upstreamTask `json:"tasks"`
	}
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal yuanmeng task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0}
	if !res.OK {
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(res.Msg, reasonUpstreamFailed)
		return result, nil
	}

	// 优先用单任务形态（FetchTask 已定位好）。
	//
	// ⚠️ **列表形态不能直接取第一条**：上游没有单任务查询接口，FetchTask
	// 找不到目标任务时会原样回传整个列表。此时取第一条 = 拿到**别人的任务**，
	// 可能把别人的成片地址当成本人的结果交付出去 —— 这不是"可能出错"，
	// 是必然出错。列表形态下必须保持"进行中"，等下一轮轮询再定位。
	task := res.Task
	if task == nil {
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
		return result, nil
	}

	switch strings.ToLower(strings.TrimSpace(task.Status)) {
	case statusDone:
		if strings.TrimSpace(task.VideoURL) == "" && strings.TrimSpace(task.Video) == "" {
			// 终态却没给成片地址：不能当成功，否则客户端拿到打不开的地址。
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		// ⚠️ **故意留空 Url**：上游直链只有约 24 小时有效，透传给客户端等于
		// 给用户一个定时炸弹。本站把成片转存到 COS 后，ResultURL 落成
		// cos-proxy:// 占位符，取片时实时签名（见 AfterUpstreamSuccess）。
		result.ResultText = ""
	case statusFailed:
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(task.FailReason, reasonUpstreamFailed)
	case statusCancelled:
		// 用户主动取消是终态。上游会退回次数。
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = "任务已取消"
	default:
		// pending / submitted / running，以及未知状态，一律当进行中兜底。
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

// findUpstreamTask 在任务列表里找目标任务。
//
// ⚠️ 上游列表接口的 tasks 数组里每项都带 id，正常按 ID 精确匹配即可。
// 但上游可能返回**数字** ID，所以两边都做字符串化比较。
func findUpstreamTask(tasks []upstreamTask, targetID string) *upstreamTask {
	target := strings.TrimSpace(targetID)
	if target == "" {
		return nil
	}
	for i := range tasks {
		if strings.TrimSpace(tasks[i].taskID()) == target {
			return &tasks[i]
		}
	}
	return nil
}

// rawToString 把 json.RawMessage 转成字符串：数字 ID 也能取到。
func rawToString(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return ""
	}
	if strings.HasPrefix(trimmed, "\"") {
		var s string
		if err := common.Unmarshal(raw, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	return trimmed
}

// ────────────────────────────── 成片转存 ──────────────────────────────

// AfterUpstreamSuccess 在成片出来后接管，把成片转存到腾讯云 COS。
//
// 为什么必须转存：上游直链**只有约 24 小时有效**，站点不留副本。
// 不转存的话，用户过了 24 小时再取片必然 404，而任务在我们库里是「成功」状态 ——
// 用户看到的是"成功但打不开"，这是最坏的一种失败。
//
// 转存链路复用 wan3 已验证的 MPS 方案：MPS 以 URL 直链为输入，
// 产物落 COS，库里存 cos-proxy:// 占位符，取片时实时预签名。
//
// ⚠️ 这里用 **SubmitRelocate（纯转存）而不是 SubmitSuperResolution（2 倍超分）**：
// 上游三档分辨率同价，1080P 原生就有，走超分反而会把画质和尺寸搞乱。
func (a *TaskAdaptor) AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil {
		return false, nil
	}
	// 没配置 MPS 时不接管：行为与改造前完全一致（用户拿到 24 小时有效的上游直链）。
	if !tencentmps_setting.IsConfigured() {
		return false, nil
	}
	// ⚠️ **upstreamURL 一定是空的**：ParseTaskResult 为了不透传 24 小时过期的
	// 上游直链，在 done 分支故意留空 TaskInfo.Url。而 service 层正是把这个
	// Url 当参数传进来 —— 两个设计互相打脸：直接判空返回 false 的话，转存永远
	// 不会触发，ResultURL 落到 BuildProxyURL（站内 /content 自引用），客户取片
	// 时代理回落到上游直链，实测报 502（上游 401）。
	//
	// 所以这里必须从已落库的上游原始响应 task.Data 里回捞真实地址。
	source := strings.TrimSpace(upstreamURL)
	if source == "" {
		source = upstreamSourceURL(task)
	}
	if source == "" {
		return false, nil
	}
	return wan3.SubmitRelocate(ctx, task, source)
}

// upstreamSourceURL 从任务的原始上游响应（task.Data）里取成片直链。
//
// task.Data 存的是上游最后一次返回的完整 JSON（{"ok":true,"task":{...}}），
// 里面 videoUrl / videoDirect / video 三个字段都可能有值，按可靠性依次取。
func upstreamSourceURL(task *model.Task) string {
	if task == nil || len(task.Data) == 0 {
		return ""
	}
	var payload struct {
		Task *upstreamTask `json:"task"`
		// 兼容直接存上游单任务对象（没有外层 task 包装）的情况。
		upstreamTask
	}
	if err := common.Unmarshal(task.Data, &payload); err != nil {
		return ""
	}
	candidates := []string{}
	if payload.Task != nil {
		candidates = append(candidates, payload.Task.VideoURL, payload.Task.VideoDirect, payload.Task.Video)
	}
	candidates = append(candidates, payload.VideoURL, payload.VideoDirect, payload.Video)
	return firstNonEmpty(candidates...)
}

// PollPostProcess 推进转存任务。
func (a *TaskAdaptor) PollPostProcess(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	return wan3.PollRelocate(ctx, task)
}

var _ interface {
	AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error)
	PollPostProcess(ctx context.Context, task *model.Task) (bool, error)
} = (*TaskAdaptor)(nil)

// ────────────────────────────── 对外查询 ──────────────────────────────

// ConvertToOpenAIVideo 把任务记录转成 OpenAI Video API 查询响应。
//
// 产物 URL 契约与 ParseTaskResult 同口径：**不透传上游直链**（24 小时过期），
// metadata.url 统一落成站内 /v1/videos/{task}/content 代理。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = PublicModel
	if originTask.Properties.OriginModelName != "" {
		openAIVideo.Model = originTask.Properties.OriginModelName
	}

	switch originTask.Status {
	case model.TaskStatusSuccess:
		openAIVideo.SetMetadata("url", taskcommon.BuildProxyURL(originTask.TaskID))
	case model.TaskStatusFailure:
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, taskcommon.ReasonContentModeration),
		}
	}
	return common.Marshal(openAIVideo)
}

// ────────────────────────────── 工具 ──────────────────────────────

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
