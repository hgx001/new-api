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
	"math"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	//
	// 超限（单条 / 合计）的文案已经是给客户看的中文提示，**原样透传**；
	// 只有探测失败才套技术前缀 —— 否则下游会看到
	// 「probe reference video duration failed: 参考视频时长 32.39 秒超过 15 秒上限」，
	// 前半截是内部实现细节，客户读不懂还会以为是我们故障。
	if refErr := a.setReferenceVideoSeconds(c, media); refErr != nil {
		if !isReferenceLimitError(refErr) {
			refErr = errors.Wrap(refErr, "probe reference video duration failed")
		}
		return service.TaskErrorWrapperLocal(
			refErr, "invalid_input_reference", http.StatusBadRequest)
	}
	// 参考音频**合计**时长上限（15 秒）—— 与参考视频同一条硬约束。
	//
	// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
	// 单个 [1, 15] 秒，总时长不大于 15 秒。
	//
	// 真实失败样本（tasks id=1258~1262，2026-11-02 19:35~19:48）：4 条音频
	// 实测 8.10/3.11/5.00/2.98 秒，**单条都在 15 秒内**，累计 19.20 秒，
	// 上游 MEDIA_DURATION_INVALID 拒单。客户连续重试 5 次，每次白占余额、
	// 等 1~3 分钟退款，而上游那句「参考素材时长不符合要求」根本没说是哪条素材。
	//
	// 放在这里（预扣之前）而不是等上游拒：本地 400 既不扣费也不建单。
	if refErr := a.setReferenceAudioSeconds(c, media); refErr != nil {
		if !isReferenceLimitError(refErr) {
			refErr = errors.Wrap(refErr, "probe reference audio duration failed")
		}
		return service.TaskErrorWrapperLocal(
			refErr, "invalid_input_reference", http.StatusBadRequest)
	}
	// 有参考视频时生成时长也要受限（老板 2026-10-31 定）。
	//
	// 为什么放在这里而不是探测函数里：生成时长与参考视频时长是两条独立的限制，
	// 但都必须在校验阶段（预扣之前）拒掉 —— 不然会走完「建单 → 预扣 → 上游拒绝
	// → 退款」，客户白占一次余额还要等退款，且失败提示是上游那句看不懂的
	// 「参数或参考素材不符合要求」。
	//
	// 只在**有参考视频**时生效：无参考视频时 30 秒生成是正常的（渠道 17 的
	// wan3.0-video 就支持），不能一刀切砍掉。
	if genErr := validateGenerationSecondsWithReferenceVideo(c, media, resolveDurationForModel(req, req.Model)); genErr != nil {
		return genErr
	}
	// 按次档（seedance2.5-活动）的档位锁定必须在**校验阶段**拒掉，
	// 不能等到计费或建单之后 —— 否则会走完「建单 → 预扣 → 上游拒绝 → 退款」，
	// 客户白占一次余额还要等退款。
	//
	// ⚠️ 顺序很关键：这条校验要排在**路由改派之后**才有效。改派会把请求体的
	// model 换成目标模型（豆包/官网链路），req.Model 随之变化 ——
	// 改派成功的请求走的是目标链路，本就不该再按「按次档」来卡时长。
	// 改派失败的请求 model 不变，这条校验照常生效。
	if taskErr := validatePerCallFixedTier(req); taskErr != nil {
		return taskErr
	}
	return nil
}

// validatePerCallFixedTier 校验「按次档」的固定时长与固定分辨率。
//
// 按次档（seedance2.5-活动）：**时长 2-30 秒逐秒可选、售价恒 ¥4.5/条**
// （2026-11-11 起放开时长；此前固定 30 秒），分辨率固定 720P。
//
// 两条硬约束，都必须 fail-loud：
//   - **时长**：2-30 秒都合法；<2 或 >30 → 400。**不做就近夹紧** ——
//     客户要 60 秒却只出 30 秒、或要 1 秒却按 2 秒跑，都是「付了钱拿不到
//     想要的片」。不传 → 由 resolveDurationForModel 补默认值 30 秒。
//   - **分辨率**：只接受 720P。1080P 的上游成本是 720P 的 1.33 倍
//     （¥0.20/秒 vs ¥0.15/秒），卖同一个价就在 1080P 上倒挂；
//     而静默降到 720P 等于「收 1080P 的钱给 720P 的片」。
//
// ⚠️ 报错文案**刻意不带模型名**：本模型在脱敏名单里（model/upstream_redact.go），
// 模型名会被替换成「上游」，出来是「上游-活动」这种碎词。
// 用户本来就知道自己调的是哪个模型，不需要在这里重复。
func validatePerCallFixedTier(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if !isPerCallModel(req.Model) {
		return nil
	}
	duration, err := parseRequestedDuration(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	if duration != 0 && (duration < minDuration || duration > maxDuration) {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("duration must be between %d and %d seconds, got %d",
				minDuration, maxDuration, duration),
			"invalid_duration", http.StatusBadRequest)
	}
	// 分辨率：只在客户端**显式**给了非 720P 时报错。
	// 完全不传时用默认值 720P（normalizeResolutionForModel 已经这么处理）。
	if raw := strings.TrimSpace(req.Resolution); raw != "" {
		if normalized := normalizeResolution(raw); normalized != perCallFixedResolution {
			return service.TaskErrorWrapperLocal(
				errors.Errorf("resolution %q is not supported, this model always delivers %s",
					raw, perCallFixedResolution),
				"invalid_resolution", http.StatusBadRequest)
		}
	}
	return nil
}

// parseRequestedDuration 解析客户端**显式传入**的时长。
//
// 第二个返回值 ok=false 表示没传（duration 会被归一到固定值，不报错）。
// 与 resolveDurationForModel 的区别：那个函数会**夹紧**（<2 提到 2、>30 压到 30），
// 这里必须拿到客户端的**原值** —— 只有原值才能判断「客户是不是要了别的档位」。
func parseRequestedDuration(req relaycommon.TaskSubmitReq) (int, error) {
	duration := req.Duration
	if duration <= 0 {
		if raw := strings.TrimSpace(req.Seconds); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				return 0, errors.Errorf("invalid seconds %q", raw)
			}
			duration = parsed
		}
	}
	return duration, nil
}

// validateGenerationSecondsWithReferenceVideo 校验「有参考视频时的生成时长上限」。
//
// 返回 *dto.TaskError（400 本地错误）而不是普通 error：本地错误不扣费、
// 不建单、也不触发渠道 AutoBan/重试 —— 客户改完素材重试即可。
func validateGenerationSecondsWithReferenceVideo(c *gin.Context, media []relaycommon.TaskMedia, generation int) *dto.TaskError {
	if !hasReferenceVideo(media) {
		return nil
	}
	if generation <= maxGenerationSecondsWithVideo {
		return nil
	}
	return service.TaskErrorWrapperLocal(
		errors.Errorf("有参考视频时生成时长不能超过 %d 秒，当前 %d 秒，请缩短生成时长或去掉参考视频",
			maxGenerationSecondsWithVideo, generation),
		"invalid_input_reference", http.StatusBadRequest)
}

// hasReferenceVideo 判断素材里有没有参考视频。
func hasReferenceVideo(media []relaycommon.TaskMedia) bool {
	for _, item := range media {
		if item.Type == "reference_video" {
			return true
		}
	}
	return false
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
	// 合计上限：上游官方约束「参考视频总时长 ≤ 15 秒」（与 wan3.0-video-官网同源）。
	// 单条各 15 秒但总共 70 秒的组合同样会被上游拒，本地提前拦。
	if total > maxTotalReferenceVideoSeconds {
		return newReferenceLimitError("参考视频总时长 %d 秒超过 %d 秒上限，请裁剪后重试",
			total, maxTotalReferenceVideoSeconds)
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
	// seedance2.5 促销族（母本 + 活动版）都由 prime 执行：档位校验、
	// 默认分辨率、素材上限全部走 prime 那一套。
	//
	// ⚠️ 判定一律走 constant 的**能力组**，不要在这里逐个列模型名 ——
	// 逐个列名正是「母本加了能力、活动版没同步」的根源。
	if constant.IsSeedance25Family(model) {
		return true
	}
	switch strings.TrimSpace(model) {
	case primeModel, legacyPrimeModel:
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
//
// ⚠️ 活动版（seedance2.5-活动）**不属于这里**：它是按次档，倍率恒为 1、
// 时长固定 30 秒、分辨率固定 720P，与母本的按秒分档完全不同的计费口径。
// 判定见 isPerCallModel。
func isTehuiModel(model string) bool {
	return strings.TrimSpace(model) == tehuiModel
}

// isPerCallModel 判断是不是「按次计费档」的模型（2026-11-02 上架活动版）。
//
// 与母本的差别是**计费口径**，不是能力：两者共用同一套素材上限、
// 同一个上游模型（wan3.0-video-prime）、同一条路由规则。
//
// 按次的三个硬性后果（改代码前先看这三条）：
//  1. 时长 2-30 秒逐秒可选，越界 400（不吸附）；没传 → 默认 30 秒；
//  2. 分辨率固定 720P —— 1080P 成本是 720P 的 1.33 倍，卖同一价会倒挂；
//  3. 不乘任何倍率 —— EstimateBilling 返回 nil，售价就是 ¥4.5/条（与时长无关）。
func isPerCallModel(model string) bool {
	return constant.IsSeedance25PerCall(model)
}

// upstreamModelFor maps the public aliases retained by new-api to the model
// IDs exposed by the current Youzan API.  The mapping is explicit so the
// public name remains stable while the upstream protocol can evolve.
func upstreamModelFor(model string) (string, error) {
	// seedance2.5 促销族（母本 + 活动版）上游恒为 wan3.0-video-prime。
	// 判定走能力组 —— 新增族成员时这里自动跟着生效。
	if constant.IsSeedance25Family(model) {
		return primeModel, nil
	}
	switch strings.TrimSpace(model) {
	case videoModel, smartLegacyModel:
		return videoModel, nil
	case primeModel, legacyPrimeModel:
		return primeModel, nil
	case r2vModel:
		return "", errors.New("youzan wan2.7-r2v is not exposed by the current upstream API")
	default:
		return "", errors.Errorf("unsupported Youzan Wan3 model %q", model)
	}
}

func normalizeResolutionForModel(model, resolution string) string {
	// 按次档固定 720P：只有一档价（¥4.5/次），上游 1080P 的成本是 720P 的
	// 1.33 倍（¥0.20/秒 vs ¥0.15/秒），卖同一个价就会在 1080P 上倒挂。
	//
	// ⚠️ 客户显式要了 1080P 时**必须报错**，不能静默降到 720P ——
	// 静默降清晰度等于「付了 1080P 的钱拿到 720P 的片」。
	// 报错由 resolveResolutionForModel 的调用方（Validate 阶段）处理。
	if isPerCallModel(model) {
		switch resolution {
		case perCallFixedResolution:
			return perCallFixedResolution
		default:
			return ""
		}
	}
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
//
// ⚠️ 按次档（seedance2.5-活动）**不在这里固定**：2026-11-11 起时长放开为
// 2-30 秒逐秒可选（售价恒 ¥4.5/条，与时长无关）。只有「没传」才补默认 30 秒 ——
// 保持旧客户不传时长时拿到的片长不变。越界已在 validatePerCallFixedTier 400 拒掉。
func resolveDurationForModel(req relaycommon.TaskSubmitReq, model string) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(req.Seconds)
	}
	if duration <= 0 {
		// 按次档没传 → 30 秒（保持旧客户拿到的片长）；其余模型默认 5 秒。
		if isPerCallModel(model) {
			return perCallFixedDuration
		}
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
	if duration > maxDuration {
		return maxDuration
	}
	return duration
}

// resolveRatio 解析成片画幅比例。
//
// 读取顺序（2026-11-10 排障后调整）：
//
//	metadata.ratio → metadata.aspect_ratio → 顶层 ratio → 顶层 aspect_ratio
//	→ size 推断 → 各自的 fallback（默认 "adaptive"）
//
// ⚠️ 为什么必须读**顶层**字段（2026-11-10）：客户端常按 OpenAI / 视频 API 的
// 习惯把比例放在请求体顶层（{"ratio":"9:16"}）。此前 TaskSubmitReq 没有这两个
// 字段，顶层比例被 Go 解码器**静默丢弃**，本函数只读 metadata，于是比例一路
// 落到 fallback="adaptive"。实测任务 task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q：
// 用户要 9:16，上游实收 ratio=adaptive，成片 1280x720（横屏），钱照扣。
//
// ⚠️ 非法比例**不报错**、只是不生效（继续往下找，最后落到 fallback）。
// 理由：比例是「锦上添花」的画幅参数，不是计费硬参数；为它拒绝整条请求会
// 打断本来能正常出片的调用。但静默降级必须留日志（见 buildRequestBody），
// 否则又是「参数被吞」型故障。
//
// ⚠️ size 推断只在顶层与 metadata 都没给比例时兜底，且**只认 "WxH"** 形式。
// "720P" / "1080P" 这类分辨率写法不是尺寸，推断不出比例（见 ratioFromSize）。
func resolveRatio(req relaycommon.TaskSubmitReq) string {
	allowedRatios := smartRatios
	fallback := defaultRatio
	if isR2VModel(req.Model) {
		allowedRatios = r2vRatios
		fallback = r2vDefaultRatio
	}
	candidates := make(relaycommon.TaskRatioCandidates, len(allowedRatios))
	for _, ratio := range allowedRatios {
		candidates[ratio] = true
	}

	result := relaycommon.ResolveTaskRatio(req, relaycommon.TaskRatioOptions{
		Candidates:   candidates,
		DefaultRatio: fallback,
		SizeToRatio:  ratioFromSize,
	})
	return result.Ratio
}

// ratioFromSize 把 "1280x720" 这类尺寸映射到最接近的比例档位。
//
// ⚠️ 只认 "宽x高" 形式："720P" / "1080P" 是分辨率写法，推不出画幅比例，
// 会被这里拒掉（返回 false），由调用方落到 adaptive。
//
// 与 dashscope / erchun / wan3 三处同名函数的判定口径保持一致：
// 取最接近的候选档，差距超过 0.25 则判定为「推不出」而返回 adaptive。
func ratioFromSize(size string) (string, bool) {
	size = strings.TrimSpace(size)
	if size == "" {
		return "", false
	}
	parts := strings.Split(strings.ToLower(size), "x")
	if len(parts) != 2 {
		return "", false
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return "", false
	}
	candidates := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
		"4:3":  4.0 / 3.0,
		"3:4":  3.0 / 4.0,
		"1:1":  1.0,
	}
	ratio := float64(width) / float64(height)
	best := ""
	bestDiff := math.MaxFloat64
	for name, value := range candidates {
		if diff := math.Abs(ratio - value); diff < bestDiff {
			bestDiff = diff
			best = name
		}
	}
	if bestDiff > 0.25 {
		return "", false
	}
	return best, true
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

// buildMedia 汇总请求里的全部素材：media（含 metadata.input.media 回退）
// 与 legacy 字段（images / image / input_reference）**合并**，不是二选一。
//
// 为什么必须合并（2026-11-02 修）：客户端普遍把图片放 images、把音视频放 media，
// 两个字段是**并行**的，不是备选关系。原实现「media 非空就整段跳过 legacy」会
// 静默丢掉图片 —— 任务照常建单、照常扣费、状态照常 SUCCESS，成片却完全没有
// 参考图，prompt 里的 @图片N 全部落空。
//
// 线上实测（渠道 17「有赞 Wan3」，本 adaptor）：客户 36 的 43 个带音视频的任务
// （13 个音频、30 个视频）**全部**丢了图，50 个纯图片任务正常。任务 1084 的 prompt
// 引用「@图片1~4 + @视频1 + @音频1~3」，上游只收到 1 视频 + 3 音频。
// 判据是 media 里有没有东西，与首尾帧无关。
//
// 注：首尾帧在本渠道已降级成 reference_image（见 validateMedia），所以合并后
// 不会产生「首帧 + 参考图」的冲突组合。
func buildMedia(req relaycommon.TaskSubmitReq) ([]relaycommon.TaskMedia, error) {
	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var err error
		media, err = metadataMedia(req.Metadata)
		if err != nil {
			return nil, err
		}
	}
	// 无条件并入：legacy 图排在 media 之后，图与图之间的相对顺序保持不变，
	// 所以 prompt 里「@图片N」的指认不会错位。
	media = append(media, legacyImages(req)...)
	return media, validateMedia(media, req.Model)
}

// legacyImages 把老字段（images / image / input_reference）转成 reference_image。
//
// 三者仍是「取第一个非空」的关系（保持既有语义，避免同一张图被重复计入），
// 但**不再因为 media 非空而被整体丢弃**。
func legacyImages(req relaycommon.TaskSubmitReq) []relaycommon.TaskMedia {
	images := req.Images
	if len(images) == 0 && strings.TrimSpace(req.Image) != "" {
		images = []string{req.Image}
	}
	if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
		images = []string{req.InputReference}
	}
	media := make([]relaycommon.TaskMedia, 0, len(images))
	for _, item := range images {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: trimmed})
		}
	}
	return media
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
	// 尺寸过小直接本地拒绝：上游会回「图片无法读取或格式不支持」（实际是分辨率不够），
	// 客户要么白等一次退款，要么反复重试同一张图。
	// 用 UserError 是为了降级成 400：换张图就能修，不能算渠道故障（500 会重试 + AutoBan）。
	if bounds := img.Bounds(); bounds.Dx() < minReferenceImageShortSide || bounds.Dy() < minReferenceImageShortSide {
		return nil, "", taskcommon.NewUserError(errors.Errorf(
			"参考图分辨率过低：%dx%d，短边至少需要 %dpx，请更换清晰图片后重试",
			bounds.Dx(), bounds.Dy(), minReferenceImageShortSide), "")
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
	// 按次档（seedance2.5-活动）：**返回 nil**，不乘任何倍率。
	//
	// 返回 nil 的含义是「这条链路按次结算，售价就是 ModelPrice 本身」
	// （与漫屋的豆包链路、megabyai 的统一契约一致，见
	// relay/helper/price.go 的 ModelPriceHelperPerCall）。
	//
	// ⚠️ 这里必须返回 nil 而不是 {"seconds": 30}：
	//   - 母本是按秒的，EstimateBilling 返 {"seconds": N, "size": R}；
	//   - 按次档若也返 seconds，relay/relay_task.go 的 applyTaskOtherRatios
	//     会把 30 秒**乘进** ¥4.5 的预扣额度 → 收 ¥135。
	//   - 而且按次档连参考视频秒数都不该并入（上游按秒计费，参考视频时长
	//     不影响我们的售价口径）。
	if isPerCallModel(modelName) {
		return nil
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
	// 按次档：倍率恒为 1。
	//
	// 按次只有一档价（¥4.5/次），与分辨率无关；而 EstimateBilling 对按次档
	// 已经返回 nil（不产生 size 倍率），这里返回 1 是**双保险** ——
	// 万一将来有人改动 EstimateBilling，倍率也不会把固定价乘歪。
	if isPerCallModel(model) {
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

// normalizeFailureReason 把上游的**兜底**错误码换成客户能行动的说法。
//
// 只动兜底码，具体错误一律原样透传：
//   - CONTENT_REJECTED / MEDIA_DURATION_INVALID / MEDIA_PARAMETER_INVALID 这些码
//     本身就是「该怎么改」的指引，替换掉反而让客户无从下手。
//   - TASK_ERROR 是上游自己都定位不到的兜底码，原话是「错误请重试」
//     「未知错误，请重新尝试」——客户读到会以为是自己参数写错了，
//     于是反复改 prompt、改分辨率、改时长，其实什么都不用改，等几分钟重投即可。
//
// 原始错误不会被丢掉：上游的完整响应一直存在 tasks.data 里（含 errorCode 与
// errorMessage），运营排查时随时能翻出来，所以这里不需要再记一条日志。
func normalizeFailureReason(errorCode, reason string) string {
	if strings.EqualFold(strings.TrimSpace(errorCode), upstreamCatchAllErrorCode) {
		return reasonUpstreamBusy
	}
	// 老协议（扁平 error 字段）会把 code 与 message 拼成「CODE: message」，
	// 没有独立的 errorCode 可读，这里按前缀再兜一次。
	if strings.HasPrefix(strings.TrimSpace(reason), upstreamCatchAllErrorCode) {
		return reasonUpstreamBusy
	}
	return reason
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
		result.Reason = normalizeFailureReason(response.Task.ErrorCode, firstNonEmpty(reason, reasonContentModeration))
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
