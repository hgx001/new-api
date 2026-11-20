package youzanwan3

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/youzanwan3_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

// 2026-11-15 上游切到 New API 协议：建单 POST /v1/videos、查询 GET /v1/videos/{id}，
// 素材直接在请求体的 media 里给公网 URL（上游自己抓取），不再有 /v1/assets 上传、
// 本地下载/缩放/重编码这一步 —— 提交是纯本地的，不再需要素材处理预算与并发闸门。

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	proxy       string
}

type generateResponse struct {
	ID string `json:"id"`
	// 上游失败时返回 {"error":{...}} 或 {"error":"..."}（New API 的 OpenAI 风格错误）。
	// 这些字段必须单独解析：失败响应里根本没有 task id，若不先判错误就会退化成
	// 「returned empty task id」，把上游的真实原因（积分不足 / 参数非法）吞成 502。
	Error   upstreamError `json:"error"`
	Message string        `json:"message"`
}

// upstreamError 兼容上游三种 error 形态：{code,message} 对象、裸字符串、
// 以及只有 message 的对象（New API 的 OpenAI 风格错误对象）。
//
// 对客文案优先取 message：新版上游把机器码放 code（invalid_request_error 之类）、
// 把可读原因放 message，拼在一起既鼓噪又帮不上客户（2026-11-15 切协议）。
// code 仍保留用于排查 —— 拿不到 message 时兜底回显 code。
type upstreamError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

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
		e.Message = text
		return nil
	}
	// 用别名类型解码，避免再次进入本方法（无限递归）。
	type plain upstreamError
	var object plain
	if err := common.Unmarshal(data, &object); err != nil {
		return err
	}
	*e = upstreamError(object)
	return nil
}

// text 返回给客户/日志看的一行文案。
func (e upstreamError) text() string {
	if message := strings.TrimSpace(e.Message); message != "" {
		return message
	}
	return strings.TrimSpace(e.Code)
}

// videoTaskResponse 是 GET /v1/videos/{id} 的响应（2026-11-15 上游切到 New API 协议）。
// 状态取 queued / in_progress / completed / failed，进度是 0—100 的整数，
// 完成态的成片地址在 metadata.url，失败原因在 error.message。
type videoTaskResponse struct {
	ID       string        `json:"id"`
	Status   string        `json:"status"`
	Progress int           `json:"progress"`
	Error    upstreamError `json:"error"`
	Message  string        `json:"message"`
	URL      string        `json:"url"`
	Metadata struct {
		URL string `json:"url"`
	} `json:"metadata"`
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
	// 参考图尺寸校验（短边 <720 本地 400）：上游的 MEDIA_IMAGE_INVALID 文案是
	// 「图片无法读取或格式不支持」，客户看不懂只会反复重试同一张图。
	// 新版协议下素材由上游抓取，这里只读图片头部拿尺寸，不下载整图。
	imageCtx, cancelImages := context.WithTimeout(c.Request.Context(), referenceImageProbeTimeout)
	defer cancelImages()
	if imageErr := a.validateReferenceImages(imageCtx, media); imageErr != nil {
		return service.TaskErrorWrapperLocal(imageErr, "invalid_image", http.StatusBadRequest)
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
	// 2026-11-15 上游切换后的显式时长/分辨率硬边界（prime 仅 30 秒仅 720P、
	// 标准版最低 10 秒）必须在**校验阶段**拒掉 —— 夹紧会造成按秒计费的静默改档。
	if taskErr := validateExplicitDurationForModel(req); taskErr != nil {
		return taskErr
	}
	// wan-3.0-pro 的画幅/尺寸白名单（上游仅 16:9 / 9:16、四个固定尺寸）。
	// 与 prime 的显式分辨率校验同一理由：静默改画幅 =「要方图给横图」。
	if taskErr := validateWan30ProExplicitRatio(req); taskErr != nil {
		return taskErr
	}
	// 两个新模型（wan-3.0 / SJ-wan3-prime）的显式参数白名单：
	//   - wan-3.0：画幅仅 16:9 / 9:16；
	//   - SJ-wan3-prime：分辨率仅 720P（上游实测 1080p/480p 被禁用）。
	// 都必须 fail-loud —— 静默降档等于「付了 1080P 的钱拿 720P 的片」。
	if taskErr := validateNewModelExplicitRatio(req); taskErr != nil {
		return taskErr
	}
	if taskErr := validateSJWan3PrimeExplicitResolution(req); taskErr != nil {
		return taskErr
	}
	// 促销族 1080P 兜底：常驻上游 SJ-wan3-prime 仅 720P。
	// 正常流量会在路由层被改派到 seedance-2-0-fast，走到这里说明没改派成功。
	if taskErr := validatePromoResolutionForModel(req); taskErr != nil {
		return taskErr
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
// 按次档（seedance2.5-活动）：**时长 4-30 秒逐秒可选、售价恒 ¥4.5/条**
// （2026-11-11 起放开时长；2026-11-17 老板口径下界统一收为 4 秒 ——
// 4-9 秒由路由层改派 CapCut 满血档执行，≥10 秒留标准版 wan3.0-video），
// 分辨率固定 720P。
//
// 两条硬约束，都必须 fail-loud：
//   - **时长**：4-30 秒都合法；<4 或 >30 → 400。**不做就近夹紧** ——
//     客户要 60 秒却只出 30 秒、或要 3 秒却按 4 秒跑，都是「付了钱拿不到
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
	// 活动版下界保持 4 秒（2026-11-18 只调整母本特惠，见 perCallPromoMinDuration）。
	if duration != 0 && (duration < perCallPromoMinDuration || duration > maxDuration) {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("视频时长必须在 %d 到 %d 秒之间，当前传入 %d 秒",
				perCallPromoMinDuration, maxDuration, duration),
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
// 与 resolveDurationForModel 的区别：那个函数会**夹紧**（<10 提到 10、>30 压到 30），
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

// validateExplicitDurationForModel 校验客户端**显式传入**的时长与分辨率
// （2026-11-15 上游切换后的新硬边界）。
//
// 两条 fail-loud 边界，夹紧替代不了：
//   - prime 直连：只接受 30 秒、只接受 720P。旧协议的「2-30 逐秒 / 1080P」
//     已失效，非 30 秒或 1080P 的请求上游一律拒 —— 与其让上游回一句
//     看不懂的参数错误，不如本地 400 把能接受的值直接写清楚。
//   - 标准版系（促销族 / wan3.0-video 直连 / smart）：<10 秒 400。
//     客户要 8 秒却被夹到 10 秒，按秒计费下等于「付 10 秒的钱」的静默改档 ——
//     与按次档 2026-11-11 定下的「越界不吸附」口径一致。
//
// >30 秒维持既有口径（resolveDurationForModel 夹紧到 30），不在这里拒；
// 按次档自己的 >30 拒单在 validatePerCallFixedTier。
//
// ⚠️ 报错文案**刻意不带模型名**：促销族在脱敏名单里（model/upstream_redact.go）。
func validateExplicitDurationForModel(req relaycommon.TaskSubmitReq) *dto.TaskError {
	duration, err := parseRequestedDuration(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	if duration == 0 {
		// prime 直连的显式分辨率检查与时长无关，没传时长也要查。
		if taskErr := validatePrimeExplicitResolution(req); taskErr != nil {
			return taskErr
		}
		return nil
	}
	if isPrimeOnlyModel(req.Model) {
		if taskErr := validatePrimeExplicitResolution(req); taskErr != nil {
			return taskErr
		}
		if duration != primeFixedDuration {
			return service.TaskErrorWrapperLocal(
				errors.Errorf("duration must be exactly %d seconds for this model, got %d",
					primeFixedDuration, duration),
				"invalid_duration", http.StatusBadRequest)
		}
		return nil
	}
	// 促销族下界 4 秒（2026-11-17 老板口径），标准版直连仍是 10 秒；
	// wan-3.0-pro 按上游文档 4 秒起；wan-3.0 实测上游硬边界 10 秒；
	// SJ-wan3-prime 上游允许 2 秒、对外按老板口径收口到 5 秒。
	//
	// ⚠️ 促销族 <10 秒的请求正常会被路由层改派到 CapCut 满血档；这里放行 4 秒
	//   是兜底（路由层那档开关关掉时不应该直接 400 —— 那是「本来能跑的请求
	//   在源链路被拒」，不是「上游不支持」）。
	lowerBound := minDuration
	switch {
	case isWan30ProModel(req.Model):
		lowerBound = wan30ProMinDuration
	case isWan30Model(req.Model):
		// wan-3.0 实测硬边界 10 秒：上游原文 "Duration 4s is outside the
		// allowed range 10-30 seconds for model wan-3.0"。
		// ⚠️ 不能为了「统一到 5 秒」而放开 5-9 秒 —— 那是「客户付了钱、
		// 建了单，上游才拒」。
		lowerBound = wan30MinDuration
	case isSJWan3PrimeModel(req.Model):
		// SJ-wan3-prime 上游允许 2 秒起；对外统一收口到 5 秒（老板口径）。
		lowerBound = sjWan3PrimeMinDuration
	case isHkWan3Model(req.Model):
		// hk-wan3 上游 4 秒起（文档 2026-11-19），不夹紧：夹到 5 秒等于
		// 「客户要 4 秒多付一秒的钱」。
		lowerBound = hkWan3MinDuration
	case isPromoFamilyModel(req.Model):
		// 母本特惠 5 秒起（常驻 SJ-wan3-prime 只接 15-30 秒，5-14 秒由
		// 路由层改派豆包 Fast）；活动版按次档保持 4 秒下界。
		if isPerCallModel(req.Model) {
			lowerBound = perCallPromoMinDuration
		} else {
			lowerBound = promoMinDuration
		}
	}
	if duration < lowerBound {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("视频时长必须在 %d 到 %d 秒之间，当前传入 %d 秒",
				lowerBound, maxDuration, duration),
			"invalid_duration", http.StatusBadRequest)
	}
	if duration > maxDuration {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("视频时长必须在 %d 到 %d 秒之间，当前传入 %d 秒",
				lowerBound, maxDuration, duration),
			"invalid_duration", http.StatusBadRequest)
	}
	return nil
}

// validatePrimeExplicitResolution 校验 prime 直连请求显式要的分辨率。
//
// 2026-11-15 上游切换后 prime 仅 720P（尺寸只有 1280x720 / 720x1280），
// 旧协议支持的 1080P 已下架。显式要 1080P 必须 400，不能走
// normalizeResolutionForModel 的静默回落（那等于「要 1080P 给 720P」）。
func validatePrimeExplicitResolution(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if !isPrimeOnlyModel(req.Model) {
		return nil
	}
	if isPrimeExplicit1080P(req.Resolution) {
		return primeResolutionError(req.Resolution)
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["resolution"].(string); ok && isPrimeExplicit1080P(raw) {
			return primeResolutionError(raw)
		}
	}
	return nil
}

func isPrimeExplicit1080P(raw string) bool {
	return strings.TrimSpace(raw) != "" && normalizeResolution(raw) == "1080P"
}

// validateSJWan3PrimeExplicitResolution 校验 SJ-wan3-prime 显式要的分辨率。
//
// 2026-11-18 实测上游对该模型回
// "Resolution 1080p is disabled for model SJ-wan3-prime"（480p 同理被禁）——
// **只有 720P 可用**。
//
// 客户显式要 1080P / 480P 时**必须 400**，不能走 normalizeResolutionForModel
// 的静默回落：那等于「收 1080P 的钱给 720P 的片」，而且按秒计费下客户还会
// 以为多付了钱买到了更清晰的片。
//
// ⚠️ 文档参数表写的是「480P / 720P / 1080P」，与实测不符。这里按实测收紧 ——
// 真被拒时是上游侧临时状态，届时再放开即可（放开的方向是安全的：宁可拒，
// 不可静默降档）。
func validateSJWan3PrimeExplicitResolution(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if !isSJWan3PrimeModel(req.Model) {
		return nil
	}
	rejected := func(raw string) *dto.TaskError {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("分辨率 %q 不受支持：该模型目前仅支持 720P", strings.TrimSpace(raw)),
			"invalid_resolution", http.StatusBadRequest)
	}
	if raw := strings.TrimSpace(req.Resolution); raw != "" {
		if normalized := normalizeResolution(raw); normalized != primeDefaultResolution {
			return rejected(raw)
		}
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["resolution"].(string); ok && strings.TrimSpace(raw) != "" {
			if normalized := normalizeResolution(raw); normalized != primeDefaultResolution {
				return rejected(raw)
			}
		}
	}
	return nil
}

// validatePromoResolutionForModel 是促销族母本 1080P 的**硬闸门**。
//
// 2026-11-20 起常驻上游换成 mw-sd2.5-flash，而上游**只有 480p / 720p 两档**。
// 对客特惠恒定交付 720P：480P 请求按 720P 执行（升档），1080P 一律 400 ——
// 没有任何一条出口能交付 1080P，静默降 720P 就是「收 1080P 的钱给 720P 的片」。
//
// ⚠️ 与改版前的差别：此前 1080P 还能靠路由层改派豆包 Fast 执行；2026-11-20
// 起改派链全部撤掉（老板口径「特惠就路由到 mw-sd2.5-flash」），所以这里必须
// fail-loud，文案也不再提「检查改派开关」—— 已经没有那个开关了。
//
// ⚠️ 按次档（活动）不在这里判：它的 1080P 由 validatePerCallFixedTier 拒。
func validatePromoResolutionForModel(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if !isPromoFamilyModel(req.Model) || isPerCallModel(req.Model) {
		return nil
	}
	if normalizeResolution(req.Resolution) != "1080P" {
		return nil
	}
	return service.TaskErrorWrapperLocal(
		errors.New("该模型目前仅支持 720P 交付（不支持 1080P），请把分辨率改为 720P 后重试"),
		"invalid_resolution", http.StatusBadRequest)
}

// validateNewModelExplicitRatio 校验两个新模型（wan-3.0 / SJ-wan3-prime）
// 显式传入的画幅。
//
// wan-3.0：上游仅 16:9 / 9:16（文档原话「画幅仅 16:9 / 9:16」），
//
//	客户显式要 1:1 / 4:3 / 21:9 时必须 400 —— 静默改成横图等于「要方图给横图」。
//
// SJ-wan3-prime：支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4（实测 21:9 与 adaptive
//
//	不可用）。⚠️ 这里**不拦** adaptive：adaptive 是历史默认写法，拦掉会让
//	老客户的请求直接 400。adaptive 不在白名单 → resolveRatio 回退到 16:9。
func validateNewModelExplicitRatio(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if isWan30Model(req.Model) {
		ratioErr := func(raw string) *dto.TaskError {
			return service.TaskErrorWrapperLocal(
				errors.Errorf("画幅 %q 不受支持：该模型仅支持 16:9 / 9:16", strings.TrimSpace(raw)),
				"invalid_ratio", http.StatusBadRequest)
		}
		if !isWan30Ratio(req.Ratio) && strings.TrimSpace(req.Ratio) != "" {
			return ratioErr(req.Ratio)
		}
		if !isWan30Ratio(req.AspectRatio) && strings.TrimSpace(req.AspectRatio) != "" {
			return ratioErr(req.AspectRatio)
		}
		if req.Metadata != nil {
			for _, key := range []string{"ratio", "aspect_ratio"} {
				if raw, ok := req.Metadata[key].(string); ok && strings.TrimSpace(raw) != "" {
					if !isWan30Ratio(raw) {
						return ratioErr(raw)
					}
				}
			}
		}
		return nil
	}
	return nil
}

// isWan30Ratio 判断画幅是否在 wan-3.0 的白名单内（16:9 / 9:16）。
func isWan30Ratio(raw string) bool {
	normalized := strings.TrimSpace(raw)
	for _, allowed := range wan30Ratios {
		if normalized == allowed {
			return true
		}
	}
	return false
}

// validateWan30ProExplicitRatio 校验 wan-3.0-pro 显式传入的画幅与尺寸。
//
// 上游文档（2026-11-17 版）原话「画幅仅 16:9 / 9:16」，尺寸只列了四个值。
// 客户显式要 1:1 / 4:3 / 21:9 时**必须 400**，不能走 resolveRatio 的静默回落 ——
// 那等于「要方图给横图」，且按条计费已经扣了钱。
//
// size 的口径（两条都合法）：
//   - 分辨率写法（720P / 1080P / 720 / 1080）—— 平台通用习惯，照常接受；
//   - 文档列出的四个尺寸（1280x720 / 720x1280 / 1920x1080 / 1080x1920）；
//   - 其它 WxH（例如 1600x900、1080x1080）上游不认，本地 400 并列出合法值。
func validateWan30ProExplicitRatio(req relaycommon.TaskSubmitReq) *dto.TaskError {
	if !isWan30ProModel(req.Model) {
		return nil
	}
	ratioErr := func(raw string) *dto.TaskError {
		return service.TaskErrorWrapperLocal(
			errors.Errorf("画幅 %q 不受支持：该模型仅支持 16:9 / 9:16", strings.TrimSpace(raw)),
			"invalid_ratio", http.StatusBadRequest)
	}
	if !isWan30ProRatio(req.Ratio) && strings.TrimSpace(req.Ratio) != "" {
		return ratioErr(req.Ratio)
	}
	if !isWan30ProRatio(req.AspectRatio) && strings.TrimSpace(req.AspectRatio) != "" {
		return ratioErr(req.AspectRatio)
	}
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if raw, ok := req.Metadata[key].(string); ok && strings.TrimSpace(raw) != "" {
				if !isWan30ProRatio(raw) {
					return ratioErr(raw)
				}
			}
		}
	}
	sizeCandidates := []string{req.Size}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["size"].(string); ok {
			sizeCandidates = append(sizeCandidates, raw)
		}
	}
	for _, raw := range sizeCandidates {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		if normalizeResolution(raw) != "" {
			continue
		}
		if resolutionFromWan30ProSize(raw) != "" {
			continue
		}
		return service.TaskErrorWrapperLocal(
			errors.Errorf("尺寸 %q 不受支持：该模型仅支持 1280x720 / 720x1280 / 1920x1080 / 1080x1920（也可改用 resolution=720P/1080P）", strings.TrimSpace(raw)),
			"invalid_size", http.StatusBadRequest)
	}
	return nil
}

// isWan30ProRatio 判断画幅是否在 wan-3.0-pro 的白名单内（16:9 / 9:16）。
func isWan30ProRatio(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	for _, ratio := range wan30ProRatios {
		if trimmed == ratio {
			return true
		}
	}
	return false
}

func primeResolutionError(raw string) *dto.TaskError {
	return service.TaskErrorWrapperLocal(
		errors.Errorf("resolution %q is not supported, only 720P is available", strings.TrimSpace(raw)),
		"invalid_resolution", http.StatusBadRequest)
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

// isWan30ProModel 判断是不是 wan-3.0-pro（独立上游模型，不是别名）。
//
// 它与标准版/prime 的边界都不同（见 constants.go 的 wan30ProModel 注释）：
// 时长 4-30、画幅仅 16:9/9:16、素材走 images/videos/audios 数组、按条计价。
// 所有分模型判断都必须先认它，否则会落进标准版分支（10 秒下界 + 禁止音视频 +
// 按秒计费），表现是「客户按文档传 5 秒被拒」或「1080P 按 720P 价格收」。
func isWan30ProModel(model string) bool {
	return strings.TrimSpace(model) == wan30ProModel
}

// isWan30Model 判断是不是 wan-3.0（上游 2026-11-18 新上架的独立模型）。
//
// 它与旧标准版 wan3.0-video 的边界**完全不同**（见 constants.go 的注释）：
//   - 时长 10-30 秒（上游原文 "Duration 4s is outside the allowed range
//     10-30 seconds for model wan-3.0"）；
//   - 参数形态是 size 四个固定值，**不接受** resolution+ratio
//     （实测回 "unsupported resolution or ratio"）；
//   - 画幅仅 16:9 / 9:16。
//
// 之所以必须单独判：所有分模型判断都要先认它，否则会落进旧标准版分支，
// 发出 resolution+ratio 组合 → 上游回 "unsupported resolution or ratio"。
func isWan30Model(model string) bool {
	return strings.TrimSpace(model) == wan30Model
}

// isSJWan3PrimeModel 判断是不是 SJ-wan3-prime（上游 2026-11-18 新上架）。
//
// 边界（2026-11-18 实测优先于文档）：时长 2-30 秒（对外收口 5-30）、
// **仅 720P**（上游回 "Resolution 1080p is disabled for model SJ-wan3-prime"）、
// size 形态（resolution+ratio 单独下发回 WAN_RATIO_INVALID）。
//
// ⚠️ 它与旧 prime（wan3.0-video-prime）的区别最关键：旧 prime 仅 30 秒、
//
//	仅 720P；新 prime 逐秒可选。两者是两个商品，不能互相别名化。
func isSJWan3PrimeModel(model string) bool {
	return strings.TrimSpace(model) == sjWan3PrimeModel
}

// isHkWan3Model 判断是不是 hk-wan3（上游 2026-11-19 在售，WAN3 官渠）。
//
// 它与 wan-3.0 的区别是**参数形态**：hk-wan3 认 resolution + ratio
// （文档示例 {"resolution":"720p","ratio":"16:9"}），wan-3.0 只认 size。
// 落错分支的表现是「上游回 unsupported resolution or ratio / unsupported size」。
//
// 能力边界（文档 2026-11-19）：时长 4–30 秒、480P/720P/1080P、
// 画幅 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto、支持参考图/视频/音频与首尾帧。
func isHkWan3Model(model string) bool {
	return strings.TrimSpace(model) == hkWan3Model
}

func isPrimeModel(model string) bool {
	// seedance2.5 促销族（母本 + 活动版）2026-11-15 起**默认**由标准版执行：
	// 分辨率档位与默认值仍与 prime 旧口径一致（720P/1080P、缺省 720P），
	// 所以 here 保留在 isPrimeModel 里；时长/素材的 prime 新边界只对
	// **直连** prime 生效，见 isPrimeOnlyModel。
	//
	// ⚠️ 促销族的 **30 秒**是例外：走 prime（见 upstreamModelForRequest），
	// 但那只是**上游模型**的选择，对客的档位口径不变，所以这里仍判 true。
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

// isPrimeOnlyModel 判断是不是「prime 直连」请求（**不含**促销族）。
//
// 2026-11-15 上游切换后 prime 收窄为「仅 30 秒、仅 720P」，这套新边界只该
// 卡直连 prime 的请求；促销族（特惠/活动）默认由标准版 wan3.0-video 执行、
// 只有 30 秒逐条映射到 prime，适用的是标准版的 10-30 秒区间 —— 不能被
// prime 的新边界误伤。
func isPrimeOnlyModel(model string) bool {
	if constant.IsSeedance25Family(model) {
		return false
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

// isPromoFamilyModel 判断是不是 seedance2.5 促销族（母本 + 活动版）。
//
// 判定走 constant 的**能力组** —— 新增族成员时这里自动跟着生效，
// 不用逐个列模型名（逐个列名正是「母本加了、活动版没同步」的根源）。
func isPromoFamilyModel(model string) bool {
	return constant.IsSeedance25Family(model)
}

// isPromoResidentMw 判断「这个模型是否由 mw-sd2.5-flash 执行」。
//
// 判定口径是「促销族 **且不是按次档**」：促销族里按次的那个（活动版）
// 走的是标准版 wan3.0-video，与 mw 的契约不同（时长下界、素材、计费都不一套），
// 不能一起套 mw 的分支。
//
// 为什么按能力组判而不是写模型名：促销族新增成员时这里自动跟进 ——
// 逐个列名正是「母本加了能力、另一个没加」的根源（见 constant 包头注释）。
func isPromoResidentMw(model string) bool {
	return isPromoFamilyModel(model) && !isPerCallModel(model)
}

// upstreamModelFor maps the public aliases retained by new-api to the model
// IDs exposed by the current Youzan API.  The mapping is explicit so the
// public name remains stable while the upstream protocol can evolve.
//
// ⚠️ 这个函数**只看模型名**，不看时长/分辨率 —— 它回答的是「这个对外名对应
// 哪个上游模型」。促销族 30 秒落 prime 的逐秒判断在 upstreamModelForRequest 里
// （那条路径还要同时看时长与分辨率档位）。
func upstreamModelFor(model string) (string, error) {
	// seedance2.5 促销族（母本 + 活动版）2026-11-18 起由 **SJ-wan3-prime** 常驻执行。
	//
	// 历史：2026-11-15 上游切换后曾默认落标准版 wan3.0-video，只有恰好 30 秒
	// 例外走旧 prime；2026-11-18 老板口径改为「特惠常驻换 SJ-wan3-prime」，
	// 旧 prime 通道同时被上游禁用，逐秒例外一并取消。
	//
	// ⚠️ SJ 只接 15–30 秒、仅 720P：5–14 秒与 1080P 由路由层分流，
	//   见 constants.go 的 promoResidentModel 注释。
	//   判定走能力组 —— 新增族成员时这里自动跟着生效。
	if constant.IsSeedance25Family(model) {
		// ⚠️ 本次只调整**母本特惠**（按秒档）；活动版（按次档）保持原标准版映射。
		// 两者虽然同族，但商品与计费口径不同：活动按条卖 ¥4.5、时长 4-30 秒，
		// 改成 SJ（15-30 秒）会把 4-14 秒的按次单打坏，而活动的短时长路由
		//（short_rule_enabled）本来就是关闭的。
		if isPerCallModel(model) {
			return videoModel, nil
		}
		return promoResidentModel, nil
	}
	switch strings.TrimSpace(model) {
	case wan30Model:
		// 对外名 wan-3.0-yz → 上游真实 ID wan-3.0。
		//
		// ⚠️ 必须返回 wan30UpstreamModel（上游真名），不能返回 wan30Model
		// （对外名）—— 上游没有 "wan-3.0-yz" 这个模型，填对外名直接 404。
		return wan30UpstreamModel, nil
	case sjWan3PrimeModel:
		// 同上。新 prime 逐秒可选，**不能**复用旧 prime 的「仅 30 秒」闸门。
		return sjWan3PrimeModel, nil
	case videoModel, smartLegacyModel:
		// 旧标准版：wan3.0-smart 是它的历史别名。
		return videoModel, nil
	case primeModel, legacyPrimeModel:
		// 旧 prime：wan3.0-video-prime-1080p 是它的历史别名。
		// ⚠️ 上游这条通道已禁用（workflow_unavailable / lifecycle=disabled），
		// 调用会失败 —— 保留映射是为了让报错指向真实原因，而不是「模型不存在」。
		return primeModel, nil
	case wan30ProModel:
		// 公开名与上游 ID 完全一致（文档要求「请求使用精确模型 ID」）。
		return wan30ProModel, nil
	case hkWan3Model:
		// 公开名 == 上游 ID，恒等映射（与 /v1/models 逐字一致）。
		return hkWan3Model, nil
	case r2vModel:
		return "", errors.New("youzan wan2.7-r2v is not exposed by the current upstream API")
	default:
		return "", errors.Errorf("unsupported Youzan Wan3 model %q", model)
	}
}

// upstreamModelForRequest 返回「这条请求实际该用哪个上游模型」。
//
// 与 upstreamModelFor（只看模型名）的差别：这里同时看**时长与分辨率档位**，
// 因为上游 prime 的能力边界是「仅 30 秒、仅 720P」—— 只有两者都落在
// prime 的边界内，才能把请求交给它，否则上游一律拒单。
//
// 2026-11-16 老板口径：**促销族（特惠母本 + 活动版）的 30 秒请求走 prime**
// （画质增强、上游更便宜），其余秒数走标准版 wan3.0-video。
//
// 活动版为什么也在内（2026-11-16 晚追加）：活动按次卖 ¥4.5/条，测算基准就是
// prime ¥0.15/秒 × 30 秒 = ¥4.50；落到标准版（¥0.18/秒 → ¥5.40）每单倒挂
// ¥0.90。它固定 30 秒 / 固定 720P，正好落在 prime 边界内，不存在降档风险。
//
// ⚠️ 判定用 isPromoFamilyModel（能力组）而不是 isTehuiModel：族里再成员化
// （比如加「活动 Pro」）时这里自动跟着生效，不会重演「母本改了、活动版没改」。
// 但**秒数边界仍只认 primeFixedDuration（30）**，放宽范围不等于放宽边界。
//
// ⚠️ **1080P 不能走 prime**：prime 只提供 720P（上游文档原话「Prime 只接受
// 以下两种尺寸。其他时长、480P、1080P 或其他尺寸均不接受」）。客户显式要了
// 1080P 时静默降到 720P = 「收 1080P 的钱给 720P 的片」——
// 所以 1080P 留在标准版（它两档齐全），而不是降档进 prime。
func upstreamModelForRequest(req relaycommon.TaskSubmitReq) (string, error) {
	// 活动版（按次档）的 **30 秒**仍走旧 prime：按条 ¥4.5 的成本基准就是
	// prime 的 ¥0.15/秒 × 30 = ¥4.5；改走标准版 ¥0.18/秒会亏 ¥0.9/单。
	//
	// ⚠️ 母本特惠不再有此例外：2026-11-18 起常驻 SJ-wan3-prime 本身就覆盖
	// 15–30 秒，而旧 prime 的画质增强档只对按次商品保留。
	if isPerCallModel(req.Model) {
		duration := resolveDurationForModel(req, req.Model)
		resolution := resolveResolutionForModel(req, req.Model)
		if duration == tehuiPrimeSeconds && resolution != "1080P" {
			return primeModel, nil
		}
	}
	return upstreamModelFor(req.Model)
}

func normalizeResolutionForModel(model, resolution string) string {
	// wan-3.0-pro：只有 720P / 1080P 两档（与标准版同档位），480P 按最低档执行。
	// 未知写法返回空串，由调用方继续找下一种来源、最后落到默认 720P。
	if isWan30ProModel(model) {
		switch resolution {
		case "720P", "1080P":
			return resolution
		case "480P":
			return defaultResolution
		default:
			return ""
		}
	}
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
	if isPromoResidentMw(model) {
		// mw-sd2.5-flash：上游只有 480p / 720p 两档，对客恒定交付 720P。
		//
		// 任何写法都归一到 720P：
		//   - 480P → 720P 是**升档**，客户不会拿到比承诺更糊的片；
		//   - 1080P 在校验阶段就被 validatePromoResolutionForModel 400 拦掉，
		//     走不到这里（静默降档 = 「收 1080P 的钱给 720P 的片」）。
		return defaultResolution
	}
	if isWan30Model(model) {
		// wan-3.0：720P / 1080P 两档（size 四个值覆盖）。480P 按最低档 720P 执行。
		switch resolution {
		case "720P", "1080P":
			return resolution
		case "480P":
			return defaultResolution
		default:
			return ""
		}
	}
	if isSJWan3PrimeModel(model) {
		// SJ-wan3-prime：**仅 720P**。
		//
		// ⚠️ 实测（2026-11-18）上游对该模型回
		// "Resolution 1080p is disabled for model SJ-wan3-prime"（480p 同理被禁）。
		// 文档参数表写的「480P / 720P / 1080P」与实测不符 —— 按实测走。
		//
		// 这里**只认 720P**，其余档位返回空串由调用方继续找下一种来源、
		// 最后落到默认 720P；显式要 1080P/480P 的请求由
		// validateSJWan3PrimeExplicitResolution 在校验阶段 400 ——
		// 静默降档等于「付了 1080P 的钱拿 720P 的片」。
		if resolution == primeDefaultResolution {
			return primeDefaultResolution
		}
		return ""
	}
	if isHkWan3Model(model) {
		// hk-wan3：480P / 720P / 1080P 三档都在售（上游分档计价 0.11/0.21/0.41）。
		// 三档全部放行 —— 480P 是三档里最便宜的一档，不能像旧标准版那样
		// 静默升到 720P（那是「客户要便宜档、我们按贵档收」）。
		switch resolution {
		case "480P", "720P", "1080P":
			return resolution
		default:
			return ""
		}
	}
	if isPrimeModel(model) {
		// 上游 prime 只提供 720P / 1080P：480P 请求按最低档 720P 执行，
		// 既不静默升到更贵的 1080P，也让请求能够成功。
		// 2026-11-15 起 prime 收窄为仅 720P，显式 1080P 的直连请求在
		// validatePrimeExplicitResolution 阶段 400（促销族仍支持 1080P ——
		// 它们的上游已换成 720P/1080P 两档齐全的标准版）。
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
		// 2026-11-15 上游切换后标准版（wan3.0-video / smart）也只有
		// 720P/1080P 两档（480P 下架）：480P 请求按最低档 720P 执行，
		// 与 prime 同口径；其余写法原样透传（含小写等历史形态）。
		if resolution == "480P" {
			return defaultResolution
		}
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
	if isSJWan3PrimeModel(model) {
		// SJ-wan3-prime 仅 720P。
		return primeDefaultResolution
	}
	if isHkWan3Model(model) {
		// hk-wan3 文档缺省 720p（不是最低档 480P，也不是更贵的 1080P）。
		return "720P"
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
	// wan-3.0-pro 的上游契约推荐用 size 指定输出（文档示例 {"size":"1280x720"}）。
	// 客户照抄文档只传 size 时，normalizeResolution("1280x720") 解析不出档位，
	// 会一路落到默认 720P —— 1080P 尺寸就会被静默降成 720P（收 720P 的钱、
	// 给 720P 的片，客户要的 1080P 丢失）。这里按文档的四个合法尺寸反推档位。
	if isWan30ProModel(model) {
		if value := resolutionFromWan30ProSize(req.Size); value != "" {
			return value
		}
		if req.Metadata != nil {
			if raw, ok := req.Metadata["size"].(string); ok {
				if value := resolutionFromWan30ProSize(raw); value != "" {
					return value
				}
			}
		}
	}
	return defaultResolutionForModel(model)
}

// resolutionFromWan30ProSize 按上游 wan-3.0-pro 文档的四个合法尺寸反推分辨率档。
//
// 只认文档列出的值（大小写不敏感、容忍 "×" 与首尾空白）：
//
//	1280x720 / 720x1280   → 720P
//	1920x1080 / 1080x1920 → 1080P
//
// 其它写法返回空串（不猜）：例如 "1080x1080" 是 1:1，上游这个模型根本不支持，
// 由 validateWan30ProExplicitRatio 统一 400，而不是在这里悄悄认成 1080P。
func resolutionFromWan30ProSize(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	normalized = strings.ReplaceAll(normalized, "×", "x")
	normalized = strings.ReplaceAll(normalized, " ", "")
	switch normalized {
	case "1280x720", "720x1280":
		return "720P"
	case "1920x1080", "1080x1920":
		return "1080P"
	default:
		return ""
	}
}

func resolveResolution(req relaycommon.TaskSubmitReq) string {
	return resolveResolutionForModel(req, req.Model)
}

func resolveDuration(req relaycommon.TaskSubmitReq) int {
	return resolveDurationForModel(req, req.Model)
}

// resolveDurationForModel 解析时长。2026-11-15 上游切换后的口径：
//   - 标准版 wan3.0-video（促销族/直连/smart 都落它）：10—30 秒逐秒可选；
//   - prime（直连）：仅 30 秒。
//
// 夹紧规则只对「标准版系」生效；prime 的非 30 秒请求在
// validateExplicitDurationForModel 阶段就已 400，走不到这里。
//
// ⚠️ 按次档（seedance2.5-活动）**不在这里固定**：2026-11-11 起时长放开为
// 逐秒可选（售价恒 ¥4.5/条，与时长无关）；2026-11-15 起随上游收窄为 10-30。
// 只有「没传」才补默认 30 秒 —— 保持旧客户不传时长时拿到的片长不变。
// 越界已在 validateExplicitDurationForModel / validatePerCallFixedTier 400 拒掉。
func resolveDurationForModel(req relaycommon.TaskSubmitReq, model string) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(req.Seconds)
	}
	if duration <= 0 {
		// 按次档没传 → 30 秒（保持旧客户拿到的片长）；prime 直连没传 →
		// 30 秒（它只接 30 秒）；wan-3.0-pro 没传 → 5 秒（文档示例值）；
		// wan-3.0 没传 → 10 秒（上游下界）；SJ-wan3-prime 没传 → 5 秒
		// （对外统一口径的下界）；
		// 其余模型默认标准版最低档 10 秒。
		switch {
		case isWan30ProModel(model):
			return wan30ProDefaultDuration
		case isWan30Model(model):
			return wan30DefaultDuration
		case isSJWan3PrimeModel(model):
			return sjWan3PrimeDefaultDuration
		case isHkWan3Model(model):
			// hk-wan3 文档缺省 5 秒（上游 4 秒起，5 秒是文档示例值）。
			return hkWan3DefaultDuration
		case isPromoResidentMw(model):
			// mw-sd2.5-flash 文档缺省 5 秒；与对外下界同值，
			// 客户不传时长时拿到的片长不会低于承诺。
			return mwSd25FlashDefaultDuration
		}
		if isPerCallModel(model) || isPrimeOnlyModel(model) {
			return perCallFixedDuration
		}
		return defaultDuration
	}
	// wan-3.0-pro：4-30 秒（上游文档口径），直接按区间夹紧兜底 ——
	// 显式越界已在 validateExplicitDurationForModel 400 拒掉。
	if isWan30ProModel(model) {
		if duration < wan30ProMinDuration {
			return wan30ProMinDuration
		}
		if duration > maxDuration {
			return maxDuration
		}
		return duration
	}
	// wan-3.0：实测硬边界 10-30 秒。显式越界已在 validate 阶段 400，
	// 这里夹紧只是兜底（比如路由层改写进来的秒数）。
	if isWan30Model(model) {
		if duration < wan30MinDuration {
			return wan30MinDuration
		}
		if duration > maxDuration {
			return maxDuration
		}
		return duration
	}
	// SJ-wan3-prime：上游 2-30 秒，对外收口 5-30 秒。逐秒可选 ——
	// 与旧 prime 的「仅 30 秒」完全不同，不能套 primeFixedDuration。
	if isSJWan3PrimeModel(model) {
		if duration < sjWan3PrimeMinDuration {
			return sjWan3PrimeMinDuration
		}
		if duration > maxDuration {
			return maxDuration
		}
		return duration
	}
	if isHkWan3Model(model) {
		// hk-wan3：上游 4–30 秒。显式越界已在 validate 阶段 400，
		// 这里夹紧只是兜底（例如路由层改写进来的秒数）。
		if duration < hkWan3MinDuration {
			return hkWan3MinDuration
		}
		if duration > maxDuration {
			return maxDuration
		}
		return duration
	}
	if isR2VModel(model) {
		// 上游仅接受 5 秒和 10 秒两档，就近取档。
		if duration <= r2vMinDuration {
			return r2vMinDuration
		}
		return r2vMaxDuration
	}
	if isPrimeOnlyModel(model) {
		// 2026-11-15 起 prime 只接 30 秒：显式时长一律落到 30
		//（非 30 的显式请求已在 validateExplicitDurationForModel 400，
		// 这里只是兜底，比如路由层改写进来的秒数）。
		return primeFixedDuration
	}
	// 促销族（特惠/活动）下界是 4 秒（2026-11-17 老板口径），其余标准版直连
	// 下界仍是 10 秒。
	//
	// ⚠️ 促销族的 <10 秒请求正常会被路由层改派到 CapCut 满血档；这里允许 4 秒
	//   是**兜底**（路由层那档开关关掉、或者路由层没生效时），
	//   避免把「本来能跑的 4 秒单」在源链路就拒掉。
	if isPromoFamilyModel(model) {
		// 母本特惠 5 秒起（2026-11-18，常驻 SJ 只接 15-30 秒，5-14 秒改派豆包 Fast）；
		// 活动版按次档保持 4 秒下界。
		lower := promoMinDuration
		if isPerCallModel(model) {
			lower = perCallPromoMinDuration
		}
		if duration < lower {
			return lower
		}
		if duration > maxDuration {
			return maxDuration
		}
		return duration
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
	if isWan30ProModel(req.Model) {
		// wan-3.0-pro 上游只认 16:9 / 9:16（文档原话「画幅仅 16:9 / 9:16」）。
		// 缺省 16:9 —— 不能落 adaptive，上游不认。
		allowedRatios = wan30ProRatios
		fallback = wan30ProDefaultRatio
	}
	// wan-3.0：画幅仅 16:9 / 9:16（size 四个值正好覆盖）。缺省 16:9。
	if isWan30Model(req.Model) {
		allowedRatios = wan30Ratios
		fallback = wan30DefaultRatio
	}
	// SJ-wan3-prime：支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4，
	// **不支持** adaptive 与 21:9（实测上游拒）。缺省 16:9。
	if isSJWan3PrimeModel(req.Model) {
		allowedRatios = sjWan3PrimeRatios
		fallback = sjWan3PrimeDefaultRatio
	}
	if isR2VModel(req.Model) {
		allowedRatios = r2vRatios
		fallback = r2vDefaultRatio
	}
	// hk-wan3：16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto，缺省 16:9。
	// ⚠️ 它认的是 "auto"，不是其它模型的 "adaptive" —— adaptive 写法不在
	// 白名单，会回退到 16:9（不 400，老客户照旧能跑）。
	if isHkWan3Model(req.Model) {
		allowedRatios = hkWan3Ratios
		fallback = hkWan3DefaultRatio
	}
	// mw-sd2.5-flash：7 档画幅（含 21:9 与 auto），缺省 16:9。
	// ⚠️ 它认 "auto" 而不是历史写法 "adaptive"；adaptive 不在白名单 →
	// 回退 16:9（不 400，老客户照旧能跑）。
	if isPromoResidentMw(req.Model) {
		allowedRatios = mwSd25FlashRatios
		fallback = mwSd25FlashDefaultRatio
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
	// wan-3.0 / wan-3.0-pro 的上游契约把视频/音频做成**顶层数组**（videos / audios），
	// 不像其它模型那样只走 media。客户端照抄上游文档时这些素材必须并入
	// 校验与下发，否则会被静默丢弃（客户付了钱、成片里没有参考素材）。
	//
	// ⚠️ 2026-11-18：原先只有 wan-3.0-pro 走这条分支，但上游文档明确
	// 「wan-3.0、wan-3.0-pro、seedance-2.0 支持 images / videos / audios 数组」
	// —— wan-3.0 同样吃顶层音视频数组。只放 pro 会让 wan-3.0 的
	// 顶层 videos/audios 静默丢失（任务成功、成片无参考素材）。
	if isWan30ProModel(req.Model) || isWan30Model(req.Model) || isHkWan3Model(req.Model) || isPromoResidentMw(req.Model) {
		media = append(media, legacyVideoAudioMedia(req)...)
	}
	return media, validateMedia(media, req.Model)
}

// ValidateIncomingMedia 用本渠道在 model 上的素材契约预检一份「还没改派」的请求。
//
// 为什么导出：条件路由（middleware/wan3_ref_video_hk_route.go 等）必须在改派
// 之前判断「目标链路会不会直接 400」。源链路的宽容度常常更高（官网适配器
// 根本不读素材：data URI、超限素材都被静默丢弃、请求照常出片），不预检就会把
// 「以前能跑」变成「直接报错」。
//
// 复用 buildMedia（含按 model 决定的顶层 videos/audios 合并）+ validateMedia
// 这两份实现：数量上限、首尾帧互斥、只收公网 http(s) 都在里面。
// 不另写一套判据 —— 两套判据迟早漂。
//
// ⚠️ 只做**纯解析**校验：不碰网络（参考图尺寸探测、参考视频时长探测都不在这里，
// 那是 validate 阶段的事）。
func ValidateIncomingMedia(req relaycommon.TaskSubmitReq, model string) error {
	probe := req
	probe.Model = model
	_, err := buildMedia(probe)
	return err
}

// legacyVideoAudioMedia 把 wan-3.0-pro 的顶层 videos / audios（含单数写法）
// 转成 reference_video / reference_audio。
//
// 只给 wan-3.0-pro 用：其它模型（2026-11-15 上游切换后）不接受参考视频/音频，
// 把字段并进来只会让它们撞上「仅接受 reference_image」的闸门，报错更难懂。
func legacyVideoAudioMedia(req relaycommon.TaskSubmitReq) []relaycommon.TaskMedia {
	media := make([]relaycommon.TaskMedia, 0, len(req.Videos)+len(req.Audios)+2)
	appendList := func(kind, single string, list []string) {
		if len(list) == 0 && strings.TrimSpace(single) != "" {
			list = []string{single}
		}
		for _, item := range list {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				media = append(media, relaycommon.TaskMedia{Type: kind, URL: trimmed})
			}
		}
	}
	appendList("reference_video", req.Video, req.Videos)
	appendList("reference_audio", req.Audio, req.Audios)
	return media
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
		if err := validateMediaURL(item.URL); err != nil {
			return err
		}
		switch item.Type {
		case "reference_image", "reference_video", "reference_audio":
			counts[item.Type]++
		case "first_frame", "last_frame":
			// 2026-10-28 上游取消首尾帧（firstFrameAssetId/lastFrameAssetId 已不存在），
			// media 只认 reference_image / reference_video / reference_audio 三类；
			// 2026-11-15 切到 New API 协议后素材契约不变。
			//
			// ⚠️ hk-wan3 例外（2026-11-19）：上游文档明确它支持首帧 / 首尾帧
			// （first_frame / last_frame，兼容 startImage / endImage）。拦掉等于
			// 把这个模型的一半能力禁掉；但仍要走组合校验（见
			// validateHkWan3FrameMedia），避免把上游必拒的组合放进去。
			if !isHkWan3Model(model) && !isPromoResidentMw(model) {
				return errors.New("youzan wan3 no longer supports first_frame/last_frame, use reference_image instead")
			}
			counts[item.Type]++
		default:
			return errors.Errorf("youzan wan3 media type %q is unsupported", item.Type)
		}
	}
	if isHkWan3Model(model) {
		if err := validateFrameMedia(counts, hkWan3Model); err != nil {
			return err
		}
	}
	// mw-sd2.5-flash 的首尾帧组合约束与 hk-wan3 同一条上游规则
	//（尾帧依赖首帧、首尾帧与普通参考素材互斥）。
	//
	// ⚠️ 文案用「该模型」而不是上游 ID：促销族的对外名是 seedance2.5-特惠，
	// 报错里出现 mw-sd2.5-flash 等于告诉客户「你调 A 实际跑 B」。
	if isPromoResidentMw(model) {
		if err := validateFrameMedia(counts, "该模型"); err != nil {
			return err
		}
	}
	if counts["reference_image"] > maxReferenceImages {
		return errors.Errorf("youzan wan3 accepts at most %d reference images", maxReferenceImages)
	}
	// 2026-11-15 上游切换：标准版 wan3.0-video 的 media **仅接受 reference_image**
	// （上游文档原话「wan3.0-video 当前路由的 media 素材仅接受 reference_image 类型」）。
	// 促销族 2026-11-15 起也落标准版执行，所以除 prime 直连外的所有请求都按这条闸拦。
	// 不拦的后果：请求透传给上游 → 上游按它自己的文案拒单 → 客户对着
	// 「参数或参考素材不符合要求」反复重试。参考视频/音频的诉求应改走
	// CapCut 链路（cp-seedance-2.5 支持参考音视频直链）。
	//
	// ⚠️ wan-3.0-pro 例外：它的上游文档明确「支持 images、videos、audios 数组」
	// （2026-11-17 版），与 wan3.0-video 不是同一份素材契约。拦掉它等于
	// 把这个模型的一半能力禁掉。
	//
	// ⚠️ wan-3.0 同样例外（2026-11-18）：上游文档「wan-3.0、wan-3.0-pro、
	// seedance-2.0 支持 images、videos、audios 数组」把这两个模型并列，
	// 素材契约一致。不放行的话，客户照文档传顶层 videos/audios 会被本地
	// 400 拦下（文案还在说「已不再支持」，与事实相反）。
	//
	// ⚠️ hk-wan3 同样例外（2026-11-19）：文档写明它「可参考视频」，media 收
	// reference_image / reference_video / reference_audio，顶层三数组也收。
	// ⚠️ mw-sd2.5-flash 同样例外（2026-11-20）：促销族母本改由它执行，
	// 上游文档明确它收 reference_image / reference_video / reference_audio
	// （10 图 / 5 视频 / 5 音频）与首尾帧。不放行等于把「换更宽的上游」
	// 变成「比原来更窄的能力」—— 原来音视频靠规则九改派 CapCut，现在直达。
	if !isPrimeOnlyModel(model) && !isWan30ProModel(model) && !isWan30Model(model) && !isHkWan3Model(model) &&
		!isPromoResidentMw(model) &&
		(counts["reference_video"] > 0 || counts["reference_audio"] > 0) {
		return errors.New("参考视频/参考音频已不再支持：上游 2026-11-15 切换后该链路仅接受 reference_image 参考图，请去掉视频/音频素材后重试（需要参考音视频请改用支持它们的模型）")
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

// validateFrameMedia 校验支持首尾帧的模型（hk-wan3 / mw-sd2.5-flash）的帧组合。
//
// 上游文档：「尾帧需要首帧，首尾帧模式与普通参考素材互斥」。
// 这三条都是**上游必然拒绝**的组合，本地 400 比让客户走完
// 「建单 → 预扣 → 上游拒 → 退款」便宜得多。
//
// name 是报错里显示的模型称呼：公开名（hk-wan3）可以直接写，
// 上游内部名（mw-sd2.5-flash）必须用「该模型」代指，避免泄露改派关系。
func validateFrameMedia(counts map[string]int, name string) error {
	if counts["first_frame"]+counts["last_frame"] == 0 {
		return nil
	}
	if counts["first_frame"] > 1 || counts["last_frame"] > 1 {
		return errors.Errorf("%s 的首帧 / 尾帧各只能传一张", name)
	}
	if counts["last_frame"] > 0 && counts["first_frame"] == 0 {
		return errors.Errorf("%s 传尾帧时必须同时传首帧（last_frame 依赖 first_frame）", name)
	}
	if counts["reference_image"]+counts["reference_video"]+counts["reference_audio"] > 0 {
		return errors.Errorf("%s 的首尾帧模式与普通参考素材互斥：请只用首尾帧，或只用 reference_* 参考素材", name)
	}
	return nil
}

// validateMediaURL 参考素材必须是上游能自己抓取的公网 http(s) 地址。
//
// 2026-11-15 协议切换后素材不再经我们上传（旧 /v1/assets 已失效），上游按 URL
// 自己抓取：data URL / 相对路径 / 内网地址它都拿不到。这种请求提交上去会变成
// 「客户付了钱、任务跑完才报素材取不到」，是最差的一种失败，所以本地 400 提前拒。
// 与 manwu / megabyai 通道同一口径。
func validateMediaURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		// 脱敏后再回显：raw 可能是整张图片的 base64 data URL，
		// 原样回显等于把字节流写进 400 响应与网关日志。
		return errors.Errorf("参考素材必须是公网 http(s) 地址（当前：%s）：新版上游自己按 URL 抓取，不再接受内联 data URL。请先上传素材（图片 /v1/uploads，视频/音频 /v1/files）后重试",
			taskcommon.RedactMaterialURL(raw))
	}
	return nil
}

const (
	// referenceImageProbeBytes 只读参考图头部：DecodeConfig 只需要文件头，
	// 不再像旧协议那样整张下载 + 缩放重编码（素材改由上游按 URL 自己抓取）。
	referenceImageProbeBytes = 2 << 20

	// referenceImageProbeTimeout 是「一次提交里所有参考图校验」的总预算，
	// 不能让客户为了本地校验等到自己的提交超时。
	referenceImageProbeTimeout = 20 * time.Second

	// referenceImageProbeConcurrency 限制同时探测的参考图数量：
	// 一次提交常见 6~7 张，串行会把 RTT 叠加成十秒级。
	referenceImageProbeConcurrency = 4
)

// validateReferenceImages 在提交前校验参考图尺寸（只读头部，不下载整图）。
//
// 2026-11-11 真机实测（同一张图只改尺寸）：短边 204/230 上游回 MEDIA_IMAGE_INVALID，
// 文案却是「图片无法读取或格式不支持」，客户只会反复重试同一张图；短边 <720 的图
// 放大到 720P 交付本身也是糊的。规则沿用（短边 <720 本地 400，禁止透传给上游），
// 只是素材改由上游抓取后，我们不再需要整张下载/重编码，读头部拿尺寸即可。
func (a *TaskAdaptor) validateReferenceImages(ctx context.Context, media []relaycommon.TaskMedia) error {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(referenceImageProbeConcurrency)
	for _, item := range media {
		if item.Type != "reference_image" {
			continue
		}
		group.Go(func() error {
			return a.probeReferenceImage(groupCtx, item.URL)
		})
	}
	return group.Wait()
}

func (a *TaskAdaptor) probeReferenceImage(ctx context.Context, source string) error {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(source, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return errors.Wrap(err, "参考图地址不被允许（内网或受限地址）")
	}
	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.Wrap(err, "参考图无法访问（源站下载失败）")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return errors.Errorf("参考图无法访问：源站返回 HTTP %d（地址可能已失效）", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, referenceImageProbeBytes))
	if err != nil {
		return errors.Wrap(err, "参考图读取失败")
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return errors.Errorf("参考图无法解析（文件可能已损坏，或为暂不支持的格式 %s），请转成 JPG/PNG 后重试",
			http.DetectContentType(data))
	}
	return checkReferenceImageSize(config.Width, config.Height)
}

// checkReferenceImageSize 按「长边不超过 referenceImageMaxSide、短边不低于
// minReferenceImageShortSide」判定参考图是否可用。
func checkReferenceImageSize(width, height int) error {
	target := referenceImageTargetSize(image.Rect(0, 0, width, height))
	if target.Dx() >= minReferenceImageShortSide && target.Dy() >= minReferenceImageShortSide {
		return nil
	}
	if target.Dx() != width || target.Dy() != height {
		return errors.Errorf(
			"参考图长宽比过窄：%dx%d 压缩到长边 %dpx 后为 %dx%d，短边至少需要 %dpx，请裁剪后重试",
			width, height, referenceImageMaxSide,
			target.Dx(), target.Dy(), minReferenceImageShortSide)
	}
	return errors.Errorf("参考图分辨率过低：%dx%d，短边至少需要 %dpx，请更换清晰图片后重试",
		width, height, minReferenceImageShortSide)
}

// referenceImageTargetSize 计算缩放后的目标尺寸：长边不超过 referenceImageMaxSide，
// 保持长宽比，且从不放大。
func referenceImageTargetSize(bounds image.Rectangle) image.Rectangle {
	width, height := bounds.Dx(), bounds.Dy()
	if width <= referenceImageMaxSide && height <= referenceImageMaxSide {
		return image.Rect(0, 0, width, height)
	}
	longest := width
	if height > width {
		longest = height
	}
	scale := float64(referenceImageMaxSide) / float64(longest)
	target := image.Rect(0, 0,
		int(math.Round(float64(width)*scale)),
		int(math.Round(float64(height)*scale)))
	// 极端长宽比可能把短边算成 0，留 1px 让「短边不足」判定给出可读报错。
	if target.Dx() < 1 {
		target.Max.X = 1
	}
	if target.Dy() < 1 {
		target.Max.Y = 1
	}
	return target
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

// buildRequestBody 组装 POST /v1/videos 的请求体（2026-11-15 上游切到 New API 协议）。
//
// 与旧协议（POST /v1/tasks + POST /v1/assets 上传换签名地址）的区别：素材 URL
// **原样透传**，由上游自己抓取；时长改用上游 TaskSubmitReq 认的 seconds 字符串
// （2026-11-15 探针实测：body 只带 seconds:"2"，查询响应回显 seconds=2）。
// 构建过程纯本地、无网络调用 —— 客户 60 秒的提交超时不再受素材体积影响。
// buildRequestBody 保留「纯本地构造」的旧签名：不做参考图优化。
// 生产提交走 buildRequestBodyWithOptions。
func (a *TaskAdaptor) buildRequestBody(req relaycommon.TaskSubmitReq) ([]byte, error) {
	return a.buildRequestBodyWithOptions(req, false)
}

func (a *TaskAdaptor) buildRequestBodyWithOptions(req relaycommon.TaskSubmitReq, optimizeImages bool) ([]byte, error) {
	upstreamModel, err := upstreamModelForRequest(req)
	if err != nil {
		return nil, err
	}
	media, err := buildMedia(req)
	if err != nil {
		return nil, err
	}
	// 参考图体积优化（2026-11-20）：只对促销族母本生效，给自有 CDN 地址追加
	// CI 质量参数，缩小上游并发回源时的字节数。见 optimizeReferenceMedia。
	if optimizeImages {
		media = optimizeReferenceMedia(media)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(media) == 0 {
		return nil, errors.New("youzan wan3 requires prompt or media")
	}
	// wan-3.0-pro 走上游文档的原生契约：seconds（整数）/ size（四个固定尺寸）/
	// images / videos / audios 数组。**不能**复用下面那套 ratio+resolution+media ——
	// 那是 wan3.0-video 的契约，字段名与素材形态都不同。
	if isWan30ProModel(req.Model) {
		return buildWan30ProRequestBody(req, upstreamModel, prompt, media)
	}
	// wan-3.0 与 SJ-wan3-prime 都走 **size 契约**。
	//
	// 实测 2026-11-18：
	//   - wan-3.0：只认 size 的四个固定值（1280x720 / 720x1280 / 1920x1080 /
	//     1080x1920）。发 resolution+ratio 组合会回 "unsupported resolution or
	//     ratio"；单发 resolution 会回 "unsupported size"（说明它把 resolution
	//     也按 size 解析）。
	//   - SJ-wan3-prime：单发 resolution=720P 会回 "WAN_RATIO_INVALID"
	//     （缺 ratio），而 size=720x1280 建单成功 —— 同样是 size 契约。
	//
	// size 与 resolution+ratio 可以**同时**下发（上游以 size 为准），但我们
	// 只发 size —— 多发一份就多一份「上游认哪个」的不确定性。
	switch {
	case isWan30Model(req.Model):
		return buildWan30RequestBody(req, upstreamModel, prompt, media)
	case isSJWan3PrimeModel(req.Model):
		return buildSJWan3PrimeRequestBody(req, upstreamModel, prompt, media)
	case isHkWan3Model(req.Model):
		// hk-wan3 走回 resolution + ratio 契约（与旧标准版同形态），
		// 但分辨率要还原成文档示例的小写写法（480p / 720p / 1080p）。
		return buildHkWan3RequestBody(req, upstreamModel, prompt, media)
	case isPromoResidentMw(req.Model):
		// mw-sd2.5-flash 是 resolution + ratio 契约，但**参数名与取值不同**：
		// 时长用 duration（整数）、分辨率小写、素材统一走 media。
		return buildMwSd25FlashRequestBody(req, upstreamModel, prompt, media)
	}
	body := map[string]interface{}{
		"model":      upstreamModel,
		"prompt":     prompt,
		"ratio":      resolveRatio(req),
		"resolution": resolveResolution(req),
		"seconds":    strconv.Itoa(resolveDuration(req)),
	}
	if len(media) > 0 {
		body["media"] = media
	}
	return common.Marshal(body)
}

// buildWan30RequestBody 按上游 wan-3.0 的契约构造请求体。
//
// 实测口径（2026-11-18）：
//
//	{"model":"wan-3.0","prompt":"...","seconds":10,"size":"1280x720",
//	 "images":[...],"videos":[...],"audios":[...]}
//
// 要点：
//   - size 是唯一被接受的尺寸字段（四个固定值，见 wan30SizeByRatio）；
//   - seconds 是**字符串**（与旧标准版一致，pro 才是整数）；
//   - 素材是**顶层 images / videos / audios 数组**（不是 media）。
//     文档原话：「wan-3.0、wan-3.0-pro、seedance-2.0 支持 images、videos、
//     audios 数组」—— 与 wan-3.0-pro 同一份契约，所以两者共用同一个构造函数。
func buildWan30RequestBody(
	req relaycommon.TaskSubmitReq,
	upstreamModel, prompt string,
	media []relaycommon.TaskMedia,
) ([]byte, error) {
	resolution := resolveResolutionForModel(req, req.Model)
	ratio := resolveRatio(req)
	size := wan30SizeByRatio[resolution+"/"+ratio]
	if size == "" {
		// 理论上到不了：分辨率已归一为 720P/1080P、画幅已归一到白名单。
		// 真到了说明调用顺序被人改坏了 —— 明确报错，不要静默发一个上游不认的 size。
		return nil, errors.Errorf(
			"wan-3.0 无法推导合法 size：resolution=%q ratio=%q", resolution, ratio)
	}
	body := map[string]interface{}{
		"model":   upstreamModel,
		"prompt":  prompt,
		"seconds": strconv.Itoa(resolveDurationForModel(req, req.Model)),
		"size":    size,
	}
	appendMediaArrays(body, media)
	return common.Marshal(body)
}

// appendMediaArrays 把素材拆成顶层 images / videos / audios 三个数组。
//
// wan-3.0 与 wan-3.0-pro 共用：上游文档明确这两个模型用这三个顶层数组，
// 不是 media 数组。少发一个字段 = 那类素材**静默丢失**（客户付了钱、
// 成片里没有参考素材），这是最差的失败方式。
func appendMediaArrays(body map[string]interface{}, media []relaycommon.TaskMedia) {
	images := make([]string, 0, len(media))
	videos := make([]string, 0)
	audios := make([]string, 0)
	for _, item := range media {
		switch item.Type {
		case "reference_image":
			images = append(images, item.URL)
		case "reference_video":
			videos = append(videos, item.URL)
		case "reference_audio":
			audios = append(audios, item.URL)
		}
	}
	if len(images) > 0 {
		body["images"] = images
	}
	if len(videos) > 0 {
		body["videos"] = videos
	}
	if len(audios) > 0 {
		body["audios"] = audios
	}
}

// buildSJWan3PrimeRequestBody 按上游 SJ-wan3-prime 的契约构造请求体。
//
// 实测口径（2026-11-18）：
//
//	{"model":"SJ-wan3-prime","prompt":"...","seconds":5,"size":"1280x720"}
//
// 要点：
//   - size 是被接受的尺寸字段（不是 resolution+ratio）；
//   - **仅 720P 一档**（上游 1080p/480p 被禁用）；
//   - seconds 是字符串（与旧标准版一致）；
//   - 素材走 media 数组。
func buildSJWan3PrimeRequestBody(
	req relaycommon.TaskSubmitReq,
	upstreamModel, prompt string,
	media []relaycommon.TaskMedia,
) ([]byte, error) {
	ratio := resolveRatio(req)
	size := sjWan3PrimeSizeByRatio[ratio]
	if size == "" {
		// 理论上到不了：画幅已归一到白名单。
		// 真到了说明调用顺序被人改坏了 —— 明确报错，不要静默发一个上游不认的 size。
		return nil, errors.Errorf(
			"SJ-wan3-prime 无法推导合法 size：ratio=%q", ratio)
	}
	body := map[string]interface{}{
		"model":   upstreamModel,
		"prompt":  prompt,
		"seconds": strconv.Itoa(resolveDurationForModel(req, req.Model)),
		"size":    size,
	}
	if len(media) > 0 {
		body["media"] = media
	}
	return common.Marshal(body)
}

// buildHkWan3RequestBody 按上游 hk-wan3 的契约构造请求体。
//
// 上游文档（2026-11-19 版）示例：
//
//	{"model":"hk-wan3","prompt":"...","seconds":5,"resolution":"720p",
//	 "ratio":"16:9","media":[{"type":"first_frame","url":"..."}]}
//
// 要点：
//   - 参数形态是 **resolution + ratio**（不是 size）—— 发 size 会被上游当
//     非法参数拒；这与 wan-3.0 / SJ-wan3-prime 的 size 契约相反；
//   - resolution 按文档示例下发**小写**（480p / 720p / 1080p）；
//   - seconds 下发整数（文档示例是数字）；
//   - 素材统一走 media 数组：reference_* 与首尾帧都能放在里面，
//     比顶层三个数组少一层「到底认哪个字段」的不确定性。
func buildHkWan3RequestBody(
	req relaycommon.TaskSubmitReq,
	upstreamModel, prompt string,
	media []relaycommon.TaskMedia,
) ([]byte, error) {
	resolution := strings.ToLower(resolveResolutionForModel(req, req.Model))
	if resolution == "" {
		// 理论上到不了：分辨率已归一为 480P/720P/1080P。
		// 真到了说明调用顺序被人改坏了 —— 明确报错，不要发一个上游不认的档位。
		return nil, errors.Errorf("hk-wan3 无法推导合法分辨率：resolution=%q", req.Resolution)
	}
	body := map[string]interface{}{
		"model":      upstreamModel,
		"prompt":     prompt,
		"seconds":    resolveDurationForModel(req, req.Model),
		"resolution": resolution,
		"ratio":      resolveRatio(req),
	}
	// audio 是 hk-wan3 的「成片是否带音轨」开关，上游**默认 false**（无声）。
	// 业务口径是必须有声：不显式传 true，客户传了参考音频也只影响风格、
	// 拿到的是无声成片（2026-11-19 实测：同参数不加 audio 出 1.73MB 纯视频，
	// 加 audio:true 出 1.80MB、AAC 44.1kHz 双声道）。
	//
	// 因此这里**无条件下发 true**。关闭只能走显式口径：metadata 里
	// with_audio=false / audio_output=false，或顶层 {"audio": false}
	// （布尔值在解码层收进 TaskSubmitReq.AudioEnabled，不会混进参考音频 URL）。
	// 见 hkWan3AudioEnabled。
	if hkWan3AudioEnabled(req) {
		body["audio"] = true
	}
	if len(media) > 0 {
		body["media"] = media
	}
	return common.Marshal(body)
}

// buildMwSd25FlashRequestBody 按上游 mw-sd2.5-flash 的契约构造请求体。
//
// 上游文档（2026-11-19 版）示例：
//
//	{"model":"mw-sd2.5-flash","prompt":"...","duration":5,
//	 "resolution":"480p","ratio":"16:9","media":[...]}
//
// 要点：
//   - 时长字段是 **duration**（整数）。文档说兼容 seconds，但我们只发一个字段 ——
//     两个都发时上游认哪个没有明确约定，多一份就是多一份不确定性；
//   - resolution 按文档示例下发**小写**（480p / 720p）。对客恒定 720p：
//     480P 请求在归一阶段已升档，1080P 在校验阶段 400；
//   - ratio 走七档白名单（含 auto），缺省 16:9；
//   - 素材统一走 media：reference_* 与首尾帧都能放进去，
//     比顶层 images/videos/audios 少一层「到底认哪个字段」的歧义。
func buildMwSd25FlashRequestBody(
	req relaycommon.TaskSubmitReq,
	upstreamModel, prompt string,
	media []relaycommon.TaskMedia,
) ([]byte, error) {
	resolution := strings.ToLower(resolveResolutionForModel(req, req.Model))
	if resolution == "" {
		// 理论上到不了：分辨率已归一为 720P。
		// 真到了说明调用顺序被人改坏了 —— 明确报错，不要发一个上游不认的档位。
		return nil, errors.Errorf(
			"mw-sd2.5-flash 无法推导合法分辨率：resolution=%q", req.Resolution)
	}
	body := map[string]interface{}{
		"model":      upstreamModel,
		"prompt":     prompt,
		"duration":   resolveDurationForModel(req, req.Model),
		"resolution": resolution,
		"ratio":      resolveRatio(req),
	}
	if len(media) > 0 {
		body["media"] = media
	}
	return common.Marshal(body)
}

// hkWan3AudioEnabled 返回本次 hk-wan3 请求是否要成片音轨。
//
// 优先级（高 → 低）：
//  1. metadata.with_audio / metadata.audio_output —— 本站文档口径，布尔。
//  2. 顶层 {"audio": true|false} —— 上游文档口径，解码时收进 AudioEnabled。
//  3. 默认 true：业务要求成片必须有声，取不到就按有声道处理 ——
//     少一段音轨比多一段更容易被当成故障。
func hkWan3AudioEnabled(req relaycommon.TaskSubmitReq) bool {
	for _, key := range []string{"with_audio", "audio_output"} {
		value, ok := req.Metadata[key]
		if !ok {
			continue
		}
		if wantAudio, ok := value.(bool); ok {
			return wantAudio
		}
	}
	if req.AudioEnabled != nil {
		return *req.AudioEnabled
	}
	return true
}

// buildWan30ProRequestBody 按上游 wan-3.0-pro 文档构造请求体。
//
// 文档原文（2026-11-17 版）：
//
//	{"model":"wan-3.0-pro","prompt":"...","seconds":5,"size":"1280x720",
//	 "images":[...],"videos":[...],"audios":[...]}
//
// 要点：
//   - seconds 是**整数**（其它模型下发的是字符串，那是旧协议的习惯）；
//   - size 必须是四个固定值之一（由分辨率档 × 画幅推导，见 wan30ProSizeByRatio）；
//   - 素材是三个**顶层数组**，不是 media。
func buildWan30ProRequestBody(
	req relaycommon.TaskSubmitReq,
	upstreamModel, prompt string,
	media []relaycommon.TaskMedia,
) ([]byte, error) {
	resolution := resolveResolutionForModel(req, req.Model)
	ratio := resolveRatio(req)
	size := wan30ProSizeByRatio[resolution+"/"+ratio]
	if size == "" {
		// 理论上到不了：分辨率已归一为 720P/1080P、画幅已归一到白名单。
		// 真到了说明调用顺序被人改坏了 —— 明确报错，不要静默发一个上游不认的 size。
		return nil, errors.Errorf(
			"wan-3.0-pro 无法推导合法 size：resolution=%q ratio=%q", resolution, ratio)
	}
	body := map[string]interface{}{
		"model":   upstreamModel,
		"prompt":  prompt,
		"seconds": resolveDurationForModel(req, req.Model),
		"size":    size,
	}
	appendMediaArrays(body, media)
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
	// wan-3.0-pro 是「按条 + 分辨率档」：¥5.0/条（720P）、¥7.0/条（1080P），
	// **与时长无关**、参考视频秒数也不并入。
	//
	// 只返回 size 倍率 —— 绝不能带 seconds：applyTaskOtherRatios 会把秒数乘进
	// ¥5，30 秒的请求就变成 ¥150（按次档注释里记着同一个坑）。
	if isWan30ProModel(modelName) {
		resolution := resolveResolutionForModel(req, modelName)
		return map[string]float64{"size": resolutionRatioForModel(modelName, resolution)}
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
	// hk-wan3 的参考视频**不额外收费**（2026-11-20 老板口径）：seconds 只取
	// 生成时长，不并入 refVideoSeconds。参考视频照样在 validate 阶段探测并
	// 校验时长上限（那是上游的能力约束，与售价无关）。
	if isHkWan3Model(modelName) {
		return map[string]float64{"seconds": float64(seconds), "size": ratio}
	}
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

// RouteBillingSizeRatio 返回按「源模型口径」计的 size 倍率，供条件路由
// （middleware/seedance_route.go 的规则六）在改派前记进 context。
//
// 为什么需要从外面问：规则六把 seedance2.5-特惠 改派到同渠道的标准版
// wan3.0-video，而标准版三档同价 —— 它的 adaptor 给的 size 恒为 1.0，
// 给不出「源模型 1080P 是 1.5 倍」这件事。计费按源模型算，就必须由源链路
// 把倍率报出来，否则 1080P 会按 ¥0.20/秒 收（应收 ¥0.30），少收 33%。
//
// 解析口径与 EstimateBilling 完全一致（同一个 resolutionRatioForModel），
// 所以「动态调价改了」与「这里报的倍率」永远同步，不会出现两套价格。
func RouteBillingSizeRatio(model, resolution string) float64 {
	return resolutionRatioForModel(model, resolution)
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
	if isWan30ProModel(model) {
		// 按条 + 分辨率档（720P 1.0 / 1080P 7.0/5.0）。
		// 同样先读动态倍率：价格同步任务若把它纳入，改动无需发版即可生效。
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(wan30ProModel, resolution); ok {
			return ratio
		}
		if ratio := wan30ProResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if isWan30Model(model) {
		// wan-3.0：两档同价 → 倍率全为 1（¥0.23/秒）。
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(wan30Model, resolution); ok {
			return ratio
		}
		if ratio := wan30ResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if isSJWan3PrimeModel(model) {
		// SJ-wan3-prime：仅 720P 一档 → 倍率恒为 1（¥0.25/秒）。
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(sjWan3PrimeModel, resolution); ok {
			return ratio
		}
		if ratio := sjWan3PrimeResolutionSizeRatio[resolution]; ratio > 0 {
			return ratio
		}
		return 1
	}
	if isHkWan3Model(model) {
		// hk-wan3：基准档 480P（¥0.18），720P ¥0.25（≈1.389×）、1080P ¥0.48（≈2.667×）。
		// 与其它模型一样先读动态倍率，读不到再用常量表。
		if ratio, ok := youzanwan3_setting.GetResolutionRatio(hkWan3Model, resolution); ok {
			return ratio
		}
		if ratio := hkWan3ResolutionSizeRatio[resolution]; ratio > 0 {
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

// idempotencyKey 由公开 task id 派生（8—100 位，仅字母/数字/下划线/连字符）。
//
// 旧上游 /v1/tasks **强制**要求 Idempotency-Key；2026-11-15 切到 New API 协议后
// 它不再是必填项，但保留发送：同一公开 task id 的本地重试会复用同一把钥匙，
// 万一上游（或它后面的通道）实现了幂等窗口，就能挡住一次「超时重试 = 重复建单 + 重复扣费」。
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
	return strings.TrimRight(a.baseURL, "/") + "/v1/videos", nil
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
	// 压缩判断必须在 model 被换成上游名之前做：换成 mw-sd2.5-flash 后
	// isPromoResidentMw 不再成立，参考图就压不到了。
	compressImages := isPromoResidentMw(req.Model)
	if info != nil && strings.TrimSpace(info.UpstreamModelName) != "" {
		req.Model = info.UpstreamModelName
	}
	body, err := a.buildRequestBodyWithOptions(req, compressImages)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(body), nil
}

// assetDownloadFailedRetryMax / assetDownloadFailedRetryDelay 是「上游抓素材
// 超时」的重试预算（2026-11-20）。
//
// 现场（logs id=18830，客户 arcreel，5 张参考图）：图片 19:25:10 才写进 CDN，
// 19:26:39 就提交，上游并发回源超时，回 400：
//
//	{"code":"asset_download_failed","message":"连接素材服务器超时。尚未提交上游生成。","retryable":false}
//
// 复核同一批图：国内 0.3 秒、全球 12 节点全 200 —— 素材本身没问题，是
// 「刚上传 + 冷缓存 + 多张并发回源」撞在一起。
//
// 只对这一个错误码、只重试一次：
//   - 其它 4xx 重试只会把客户余额和上游额度再烧一遍；
//   - 上游明确「尚未提交上游生成」，重试没有生成成本，也不会重复计费；
//   - 第一次失败的上游请求已经把 CDN 填热，原样重发几乎必成。
//
// 客户断开（ctx 取消）时立即放弃重试：ArcReel 侧提交预算是 60 秒，而这次
// 上游 88 秒才回失败，那时客户端早已放弃，重试只是白等。
const (
	assetDownloadFailedRetryMax   = 1
	assetDownloadFailedRetryDelay = 3 * time.Second
)

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	// 请求体必须可重放：重试要用同一份字节。上游 400 的响应体很小，
	// 把它读出来还原给 DoResponse 的代价可以忽略。
	bodyBytes, err := io.ReadAll(requestBody)
	if err != nil {
		return nil, err
	}
	resp, err := doWithAssetDownloadRetry(
		c.Request.Context(),
		func() (*http.Response, error) {
			return channel.DoTaskApiRequest(a, c, info, bytes.NewReader(bodyBytes))
		},
		assetDownloadFailedRetryMax,
		assetDownloadFailedRetryDelay,
	)
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

// doWithAssetDownloadRetry 在「上游抓素材失败」时原样重发，最多 maxRetries 次。
//
// 响应体读完会原样塞回去：不论重试还是直接返回，调用方（DoResponse）都要
// 继续读它，不能出现空 body。
func doWithAssetDownloadRetry(
	ctx context.Context,
	attempt func() (*http.Response, error),
	maxRetries int,
	delay time.Duration,
) (*http.Response, error) {
	for tried := 0; ; tried++ {
		resp, err := attempt()
		if err != nil {
			return nil, err
		}
		if resp == nil || tried >= maxRetries {
			return resp, nil
		}
		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(body))
		if readErr != nil || !isAssetDownloadFailed(resp.StatusCode, body) {
			return resp, nil
		}
		select {
		case <-ctx.Done():
			// 客户端已经走了：把上游这次的真实失败原样交回去。
			return resp, nil
		case <-time.After(delay):
		}
	}
}

// isAssetDownloadFailed 判定响应是否为「上游抓不到我们给的素材」。
//
// 只认 400 + 该错误码：上游还有 media_invalid / asset_too_large 等同样带
// 400 的素材类错误，那些重试多少次都一样。
func isAssetDownloadFailed(statusCode int, body []byte) bool {
	return statusCode == http.StatusBadRequest && bytes.Contains(body, []byte("asset_download_failed"))
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
		// 先把上游的真实错误透出去：失败响应里没有 task id，
		// 直接报「空 task id」会掩盖真实原因（入参非法 / 积分不足 / 上游故障）。
		if upstreamMsg := parsed.Error.text(); upstreamMsg != "" {
			// 上游账户余额耗尽 ≠ 普通失败：实测（2026-11-16）上游此时会把请求
			// **挂起**而非丢弃，充值后复活执行并计费。我方不建单、不收费，
			// 上游每复活一笔就是一笔纯亏 —— 必须尽快充值或临时停用渠道止损。
			if service.IsUpstreamAccountBalanceError(resp.StatusCode, upstreamMsg) {
				logger.LogWarn(c, fmt.Sprintf(
					"[SYS] youzan wan3 上游账户余额不足（%s），请求已被上游挂起：充值后上游会复活执行并计费。"+
						"请立即给上游充值，或临时停用该渠道止损，期间该渠道新请求会持续产生挂起负债",
					upstreamMsg))
			}
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
	request, err := http.NewRequest(http.MethodGet, normalizeAPIRoot(baseURL)+"/v1/videos/"+url.PathEscape(taskID), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+key)
	return client.Do(request)
}

// progressString 把上游的百分比进度转成 TaskInfo 的进度文案。
//
// 上游在 in_progress 时会带 progress（0—100），透传它客户才能看到「在动」；
// 拿不到时退回通用文案 —— 回显 0% 会被当成卡死。
func progressString(progress int) string {
	if progress <= 0 {
		return taskcommon.ProgressInProgress
	}
	if progress > 100 {
		progress = 100
	}
	return strconv.Itoa(progress) + "%"
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var response videoTaskResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, err
	}
	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(response.Status)) {
	case "queued", "pending", "submitted", "submitting":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress":
		result.Status = model.TaskStatusInProgress
		result.Progress = progressString(response.Progress)
	case "succeeded", "success", "completed", "done":
		// 完成态的成片地址有两种形态：
		//
		// 1. 旧协议：地址在 metadata.url（New API 惯例）或顶层 url，
		//    相对路径按 API 根解析成绝对地址 —— 直接透传给客户。
		// 2. 2026-11-15 上游切换后的新协议：completed 响应**不再携带任何地址**
		//    （实测 {"status":"completed","progress":100} 就是不带 url/metadata），
		//    成片改由 /v1/videos/{upstream_task_id}/content 端点提供，且必须带
		//    渠道密钥才能取。这时返回 Success 且 Url 留空，由轮询层落成
		//    站内代理地址（service/task_polling.go：Success + 空 URL →
		//    BuildProxyURL），取片时由 controller/video_proxy.go 带渠道密钥回源。
		//
		// ⚠️ 这里不能再「completed 但无地址就保持 IN_PROGRESS」：新协议下
		// completed 永远不会补发地址，等下去就是任务永远卡在处理中、
		// 客户的钱卡在预扣里（2026-11-15 生产实测 task 2997 卡死 15 分钟）。
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = a.resolveResultURL(firstNonEmpty(response.Metadata.URL, response.URL))
	case "failed", "failure", "canceled", "cancelled", "error", "refunded":
		// refunded 等终态必须收敛为失败，否则任务会落进 default 一直 IN_PROGRESS，
		// 客户的钱卡在预扣里拿不回来。
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(response.Error.text(), response.Message, reasonContentModeration)
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

// 编译期断言：本渠道参与轮询层的「上游失败复核」（service.TaskFailureRechecker）。
var _ service.TaskFailureRechecker = (*TaskAdaptor)(nil)

// FailureRecheckDelay 实现可选接口 service.TaskFailureRechecker。
//
// 上游余额耗尽时把任务标成 failed 并非真终态：充值后会把**同一个任务**复活
// 继续执行并计费（2026-11-16 生产实锤，见 failureRecheckDelay 注释）。轮询层
// 依据本窗口延迟退款：窗口内复活 → 正常交付结算；窗口过后仍是 failed 才退款。
func (a *TaskAdaptor) FailureRecheckDelay() time.Duration { return failureRecheckDelay }

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
