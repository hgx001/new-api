package manwu

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
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
	// ModelName 是渠道默认模型（dola），保留导出供既有测试与文档引用。
	// 多模型路由请查 modelSpecs / SpecFor。
	ModelName       = ModelDola
	ChannelName     = "manwu"
	InputTypeImage  = "reference_image"
	InputTypeVideo  = "reference_video"
	OutputModeVideo = "video"
	OutputModeImage = "image"
	OutputModeText  = "prompt"

	// ArcReel 侧 platformId 标识（建单载荷 platformId 字段）。
	PlatformID       = "dola"
	PlatformIDGemini = "gemini"
	PlatformIDJimeng = "jimeng"
	// PlatformIDDoubao 是豆包官网（www.doubao.com）平台。**与 dola 是两个独立的
	// 执行站点**——虽然模型同为 Seedance 系，跑的官网、账号池、风控完全不同
	// （2026-10-08 用户明确区分）。Worker 侧上报 `doubao.dom` 能力接单。
	PlatformIDDoubao = "doubao"
	// PlatformIDImage 是 ArcReel 的抽象图片平台（PUBLIC_REMOTE_IMAGE_PLATFORM）：
	// 它是内部建单协议值，与对外模型名 ModelImage 分离；服务端按
	// MANWU_REMOTE_IMAGE_PROVIDER 决定实际走 gemini 还是 jimeng。
	PlatformIDImage = "manwu-image"
	// PlatformIDTongyiwan 是通义万相官网平台，走 **wan2.7 免灵感值（relax_mode）
	// 慢队列**。Worker 侧 RemoteTaskAdapter 对 tongyiwan 已硬编码
	// generateWithCredits + requireFreeMode，因此这条通路**不消耗灵感值**。
	// 对外两个模型（图生 / 编辑）共用该 platformId，靠素材类型分流 DOM 路线。
	PlatformIDTongyiwan = "tongyiwan"
	// PlatformIDPicsart 是 Picsart 官网（picsart.com）平台，走**免费无限合成档
	// Wan 3.0**（账号权益，不扣积分）。Worker 侧上报 `picsart.dom` 能力接单。
	//
	// ⚠️ 与豆包/万相不同，picsart 是**一级建单平台**（2026-10-24 老板定稿），
	// 不再靠「豆包建单 + Worker 路由」那条老路。历史所有 Picsart 出片都是
	// platformId=doubao + reroutedToPlatformId=picsart；本模型起改直派，
	// 账目/白名单/重派都按 picsart 走。
	PlatformIDPicsart = "picsart"

	// baseURLDefault 为渠道未配置 BaseURL 时的兜底（ArcReel 生产地址）
	baseURLDefault = "https://arcreel.heibaidao.cn"

	submitPath = "/api/v1/remote-generation/jobs"

	defaultRatio = "16:9"
	// defaultDuration 与上游兜底口径对齐：ArcReel 对未指定/非法时长会回落 15 秒，
	// 这里缺省显式传 15，保证预扣与实际生成时长一致。
	defaultDuration = 15

	// 参考图上限按 dola 官网真机口径取 10（与 ArcReel openai 后端 manwu 分支同口径）。
	//
	// ⚠️ **豆包侧同样到 10，不要按平台分档**（2026-10-23 生产数据取证）：
	// ArcReel 生产库 `remote_generation_jobs` 里 platform_id=doubao 的任务实测带图张数
	// 分布为 0/1/2/3/4/5/6/7/9/10 张 —— 9 张有 3 条走到 `ready`（已提交官网），
	// 10 张有 2 条，其失败原因分别是「参考图上传失败：第 1/10 张 upload-error:no-accept-signal」
	// 与「豆包模型设置失败（模型核验不一致）」——**都与张数无关**；
	// 全库**没有任何一条因「参考图张数超限」失败的记录**。
	// 换言之：豆包侧放行到 10 张没有证据反对，凭猜收紧只会把合法任务拦死。
	// （另有一条不同维度的限制：豆包官网对「多参考图 + 大量角色锁定」有内容难度门槛，
	// 那由 ranjing-ai 侧 contentDifficulty.ts 识别并换号，与张数上限无关。）
	maxReferenceImages = 10
	// maxReferenceVideos 反解只认 1 个视频：ArcReel create_job 硬约束「视频反解
	// 任务必须且只能携带 1 个参考视频」，多传会被上游 400。
	maxReferenceVideos = 1
	// maxImageCount 与 ArcReel `_remote_image_count` 的 clamp 上限一致（1..10）。
	maxImageCount     = 10
	defaultImageCount = 1

	idempotencyKeyPrefix = "manwu-"

	normalizedRequestKey = "manwu_request"

	// CtxKeyManwuIdempotencyKey：按「客户端本次请求」缓存的幂等键。重试（同渠道
	// 或换渠道）都会重新走 ValidateRequestAndSetAction，若每次都用新 UUID，ArcReel
	// 的幂等重放就永远不命中 —— 504 这类「提交结果未知」的失败重试会重复建单、
	// 重复预扣。gen-78754689fdc325f5ce17510c 事故（2026-10-25）根因之一。
	CtxKeyManwuIdempotencyKey = "manwu_idempotency_key"

	// CtxKeyTaskSubmitIdempotent：标记本任务提交是幂等的（走 ArcReel 唯一索引 +
	// 幂等重放防重）。controller 据此对 408/504/524 这类「结果未知」放行重试，
	// 其他渠道保持原有的不重试语义。
	CtxKeyTaskSubmitIdempotent = "task_submit_idempotent"

	reasonCancelled = "任务已取消"
	// reasonEmptyResult：上游报 ready 却既没有产物 URL 也没有文本结果。判失败而不是
	// 成功——空交付必须触发退款，否则用户为「什么都没拿到」付了钱。
	reasonEmptyResult = "任务完成但上游未返回结果"
	// reasonOutputUnavailable：上游报 url_unavailable（产物 URL 校验未通过）。不能落
	// 「内容审核不通过」兜底——2026-10-08 豆包首单实证：ArcReel 校验器白名单缺失误判
	// 时，用户看到「内容审核不通过」会误以为自己提示词违规，实际是基础设施问题。
	reasonOutputUnavailable = "产物 URL 校验失败：生成结果暂不可用或已过期"
	// reasonUnknownFailure：上游报 failed 但没给任何原因。**不能用
	// taskcommon.ReasonContentModeration**——那是「无理由失败统一归内容审核」的旧口径，
	// 会把 Worker 失联/服务端回收/超时这类基础设施问题谎报成内容违规，客户拿着错的
	// 方向去改提示词，白折腾一轮。2026-10-18 用户拍板改成中性文案。
	reasonUnknownFailure = "上游生成失败（未提供具体原因），请稍后重试或联系客服"
	// reasonDirectUploadUnsupported：全渠道都不收 multipart 文件。参考素材必须是
	// ArcReel 能回源的公网 URL（图片 URL、dola/gemini 的参考图 URL、反解的视频 URL），
	// 因此这条拒绝对四个模型都成立，不能写成只讲反解。
	reasonDirectUploadUnsupported = "暂不支持文件直传：本渠道全部模型都需要公网可访问的 http(s) 素材 URL（视频反解请传视频 URL）"
)

// 七个公开模型共用同一条 ArcReel 建单链路（POST /api/v1/remote-generation/jobs），
// 差异只在 platformId + outputMode + 参数白名单，故用一张路由表而不是多个 adaptor。
//
// 模型名口径：
//   - seedance-2.0：dola 官网 Seedance 2.0（远端视频，时长固定 10 秒，按次计费）
//   - seedance-2.5：dola 官网 Seedance 2.5（远端视频，固定 30 秒 / 720p，按次计费）
//   - seedance-2-5：豆包官网 Seedance 2.5（远端视频，5/10/15/30 秒四档，
//     按次计费 ¥3.0/次）。公开名是短名（2026-10-08 用户定稿，2026-10-12 去掉
//     db- 前缀），转发 ArcReel 前翻译成官网档位真名 doubao-seedance-2-5-260628
//     （见 modelSpec.UpstreamModel）；与 dola 的 seedance-2.5 同名不同站，**互不通用**
//   - seedance-2-0-fast：豆包官网 Seedance 2.0 Fast（远端视频，5-15 秒逐秒可选，
//     **按秒计费 ¥0.1/秒**），上游真名 doubao-seedance-2-0-fast-260128
//   - Nano Banana Pro：ArcReel 抽象图片平台（PUBLIC_REMOTE_IMAGE_PLATFORM）。服务端
//     MANWU_REMOTE_IMAGE_PROVIDER 决定实际走 gemini(nano_banana_2) 还是
//     jimeng(dreamina_image_5_0_lite)，客户端不感知，也不该猜
//   - gemini-web-video：Gemini 官网 Veo（远端视频，按次计费）。**故意不叫 veo-***：
//     官方 Gemini 渠道已占 veo-3.1-generate-preview 等模型名，重名会被路由到错渠道
//   - jimeng-video-reverse：即梦「视频反解」技能，单视频入参、提示词文本出参
const (
	ModelDola         = "seedance-2.0"
	ModelDola25       = "seedance-2.5"
	ModelDoubao25     = "seedance-2-5"
	ModelDoubao20Fast = "seedance-2-0-fast"
	ModelImage        = "Nano Banana Pro"
	ModelVideo        = "gemini-web-video"
	ModelReverse      = "jimeng-video-reverse"
	// 万相 wan2.7 免灵感值（2026-10-16 上架，老板定稿）：后缀用中文，
	// 与 dola/doubao 的公开名风格区分，避免和上游 dashscope 的 wan 系重名。
	ModelWan27I2V  = "wan2.7-图生"
	ModelWan27Edit = "wan2.7-编辑"
	// ModelWan3TeHui 是 Picsart 免费无限档 Wan 3.0（2026-10-24 上架，2026-10-25 改名）。
	//
	// 改名理由（老板 2026-10-25）：「无限」指**账号侧的无限合成权益**（不扣积分、
	// 不限条数），但对客户是**按次计费**，叫「无限」容易被理解成「免费无限调用」，
	// 咨询成本高。改用「特惠」——与 MegaAI 渠道的 `seedance2.5-特惠`
	// （relay/channel/task/megabyai/constants.go）同一套后缀风格。
	//
	// 与 MegaAI 渠道的 `wan-3.0`（付费满参版，按分辨率档计价）仍是**两个不同的商品**，
	// 名字只差后缀，不要合并、不要互相别名化。
	ModelWan3TeHui = "wan-3.0-特惠"
	// ModelWan3TeHuiLegacy 是改名前的公开名，**只保留可调用**、不进模型广场
	// （广场白名单见 model/pricing.go 的 isAllowedPricingModel）。
	//
	// 为什么留别名而不是硬切：本商品 2026-10-24 才上架，24 小时内可能已有接入方
	// 按旧名联调；断掉是不可逆的商务损失，而多一个 map 条目的成本几乎为零。
	// 清理条件：确认 30 天内无任何调用（`logs` 表按 model_name 统计）后，
	// 删掉本行 + modelSpecs 里对应条目 + 生产库 abilities 里的旧名即可。
	ModelWan3TeHuiLegacy = "wan-3.0-无限"
)

// 豆包官网档位真名：Worker 侧 DOM 点击目标（src/platforms/doubao/params.ts），
// 只出现在发往 ArcReel 的载荷里，不对客户端暴露。
const (
	doubaoUpstreamModel25     = "doubao-seedance-2-5-260628"
	doubaoUpstreamModel20Fast = "doubao-seedance-2-0-fast-260128"
)

// picsartUpstreamModel 是 Picsart 官网 Wan 3.0 的档位 id（URL `?model=` 与内部
// payload 同名，见 cron_video_brach src/platforms/picsart/params.ts:108）。
// 公开名 `wan-3.0-特惠` 只在本表内存在，发往 ArcReel/Worker 的一律是它。
const picsartUpstreamModel = "wan-3.0-video"

// 任务形态：决定校验分支与建单载荷。
const (
	kindDolaVideo   = "dola_video"   // Seedance 2.0：时长固定 10 秒/比例/参考图白名单
	kindDola25Video = "dola25_video" // Seedance 2.5：固定 30 秒/720p/比例/参考图白名单
	kindDoubaoVideo = "doubao_video" // 豆包官网：5/10/15/30 秒四档/比例七档（含 auto）/参考图
	kindVeoVideo    = "veo_video"    // 只有宽高比：官网无模型/时长/分辨率控件
	kindImage       = "image"        // 张数 + 宽高比 + 参考图
	kindReverse     = "reverse"      // 单视频入参，文本出参
	// 万相两条 DOM 路线：上游模型同为 Wan2_7，分流全靠素材类型 ——
	// 图生带 reference_image；编辑带 reference_video（Worker 侧据
	// generationMode='edit' || !!referenceVideo 判定 isEditMode）。
	kindWanI2VVideo  = "wan_i2v_video"  // 图生：5–15 秒逐秒 / 720P-1080P / 参考图 ≤2 / 不收参考视频
	kindWanEditVideo = "wan_edit_video" // 编辑：主视频恰 1 个 / 参考图 ≤10 / 时长跟随
	// kindPicsartVideo 是 Picsart 官网 Wan 3.0（免费无限档）：固定 30 秒、
	// 分辨率 480P/720P、比例六档（含 adaptive）、参考图 ≤10、不收参考视频。
	kindPicsartVideo = "picsart_video"
)

// 万相 wan2.7 免灵感值参数口径（与 Worker 侧 TONGYIWAN_UNLIMITED_VIDEO_PROFILE 同源：
// cron_video_brach src/platforms/tongyiwan/params.ts:101-107）。
// 时长 5–15 秒逐秒真跑（官网档位），分辨率两档；缺省 5 秒 / 720P。
const (
	wanMinDuration     = 5
	wanMaxDuration     = 15
	wanDefaultDuration = 5
	// 图生路线的参考图上限 2：万相官网通道的既有约束（RemoteTaskAdapter 同口径）。
	wanMaxI2VImages = 2
	// 编辑路线的参考图上限 10：对齐 MANWU_EDIT_VIDEO_PROFILE.maxReferenceImages。
	wanMaxEditImages = 10
	// wanUpstreamModel 是发往 ArcReel 的官网模型 id。公开名只在本表内存在，
	// Worker 侧 normalizeTongyiwanTaskOverrides 还会把它收敛成 Wan2_7（双保险）。
	wanUpstreamModel = "Wan2_7"
)

// wanAllowedResolutions 只认 720P/1080P（大小写不敏感，解析时统一转大写）。
var wanAllowedResolutions = map[string]bool{"720P": true, "1080P": true}

// ── Picsart 特惠档 Wan 3.0 口径（2026-10-24 老板定稿，2026-10-25 改名 + 统一价）──
//
// 与 Worker 侧 src/platforms/picsart/params.ts 的官网档位同源，但**收窄**：
//   - 时长固定 30 秒（官网还有 auto/5/10/15，本商品只卖 30 秒）；
//   - 分辨率只开 480P/720P（1080P 官网有，但 30 秒 1080P 出片更慢且没溢价）；
//     两档不同价：480P ¥1.50、720P ¥2.00。ModelPrice 配**最低档 480P** 的 ¥1.50，
//     这样模型广场徽标显示「¥1.50 / 请求」；720P 由 EstimateBilling 的 size 倍率
//     乘回 ¥2.00（见 picsart720PSizeRatio）。
//   - 比例六档，含官网默认的 adaptive，**没有 21:9**（Wan 3.0 官网不支持）。
//
// 非法值一律 400，不静默回落 —— 客户以为买的是 10 秒、我们交付 30 秒，
// 或者以为 1080P、实际 720P，都是错交付。
const (
	picsartFixedDuration      = 30
	picsartDefaultResolution  = "720P"
	picsartMaxReferenceImages = 10
	// picsart720PSizeRatio 是 720P 相对 480P 基准价的倍率。
	//
	// 对外价（老板 2026-10-25 定稿）：480P ¥1.50/次、720P ¥2.00/次。
	// ModelPrice 只能配一个数，且模型广场徽标直接显示它 —— 所以配**最低档**
	// 480P 的 ¥1.50，720P 用本倍率乘回 ¥2.00：
	//   1.333… = 2.00 / 1.50
	// ⚠️ 改价时 ModelPrice['wan-3.0-特惠'] 与本倍率必须同时改，否则两档价会错位
	//    （测试 TestPicsartUnlimited720PRatioMatchesPriceList 锁住了这层关系）。
	picsart720PSizeRatio = 2.0 / 1.5
)

var picsartAllowedResolutions = map[string]bool{"480P": true, "720P": true}

var picsartAllowedRatios = map[string]bool{
	"adaptive": true,
	"16:9":     true,
	"9:16":     true,
	"1:1":      true,
	"4:3":      true,
	"3:4":      true,
}

type modelSpec struct {
	PlatformID string
	OutputMode string
	Kind       string
	// UpstreamModel：发往 ArcReel 的上游模型名；空 = 与公开名一致。豆包公开短名
	// ≠ 官网档位真名，翻译在这一层做（ArcReel/Worker 两侧白名单仍是真名）。
	UpstreamModel string
}

var modelSpecs = map[string]modelSpec{
	ModelDola:         {PlatformID: PlatformID, OutputMode: OutputModeVideo, Kind: kindDolaVideo},
	ModelDola25:       {PlatformID: PlatformID, OutputMode: OutputModeVideo, Kind: kindDola25Video},
	ModelDoubao25:     {PlatformID: PlatformIDDoubao, OutputMode: OutputModeVideo, Kind: kindDoubaoVideo, UpstreamModel: doubaoUpstreamModel25},
	ModelDoubao20Fast: {PlatformID: PlatformIDDoubao, OutputMode: OutputModeVideo, Kind: kindDoubaoVideo, UpstreamModel: doubaoUpstreamModel20Fast},
	ModelVideo:        {PlatformID: PlatformIDGemini, OutputMode: OutputModeVideo, Kind: kindVeoVideo},
	ModelImage:        {PlatformID: PlatformIDImage, OutputMode: OutputModeImage, Kind: kindImage},
	ModelReverse:      {PlatformID: PlatformIDJimeng, OutputMode: OutputModeText, Kind: kindReverse},
	// 万相两个模型共用 tongyiwan 平台，差异在 Kind（→ 校验分支 / DOM 路线）。
	ModelWan27I2V:  {PlatformID: PlatformIDTongyiwan, OutputMode: OutputModeVideo, Kind: kindWanI2VVideo},
	ModelWan27Edit: {PlatformID: PlatformIDTongyiwan, OutputMode: OutputModeVideo, Kind: kindWanEditVideo},
	// Picsart 免费无限档 Wan 3.0（2026-10-24 上架，2026-10-25 改名「特惠」）：
	// 公开名 → 官网档位 id 的翻译在这一层做，ArcReel/Worker 两侧白名单仍是官网真名。
	ModelWan3TeHui: {PlatformID: PlatformIDPicsart, OutputMode: OutputModeVideo, Kind: kindPicsartVideo, UpstreamModel: picsartUpstreamModel},
	// 改名前的公开名，同一条 spec：只保可调用，不进广场（见 ModelWan3TeHuiLegacy 注释）。
	ModelWan3TeHuiLegacy: {PlatformID: PlatformIDPicsart, OutputMode: OutputModeVideo, Kind: kindPicsartVideo, UpstreamModel: picsartUpstreamModel},
}

// SpecFor 返回模型路由；未知模型返回 false。调用方必须先拒绝，不能默认落到 dola
// （那会让 Veo 请求被当成 dola 提交，上游跑错平台还要用户付两次钱）。
func SpecFor(model string) (modelSpec, bool) {
	spec, ok := modelSpecs[strings.TrimSpace(model)]
	return spec, ok
}

// ModelList 返回本渠道支持的全部模型名（有序，供错误提示与 GetModelList 共用）。
func ModelList() []string {
	names := make([]string, 0, len(modelSpecs))
	for name := range modelSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Seedance 2.0 系时长口径（2026-10-10 用户定稿，两端同口径；2026-10-24 曾把 dola
// 收窄为固定 10 秒，2026-10-26 按用户要求恢复为 5–15 秒区间）：
// 对外均按 **5–15 秒区间**受理，区间内任意整数秒都放行且**原样透传、不吸附**；
// 区间外（含 30s）400。缺省 15 秒。
//
//   - dola（seedance-2.0）：**任意值真跑**——区间内原样透传。依据：Worker
//     durationHack 真机实证官网后端接受任意值（7 秒提交成片 ffprobe 实测
//     7.10 秒），且 dola 不点时长 UI、目标值全靠请求体改写下发。
//   - 豆包（seedance-2-0-fast，2026-10-12 跟进）：与 dola 同口径透传——Worker
//     侧豆包 durationHack（请求体改写）兜底，非档位值跳过 UI、提交前武装
//     钩子 + 硬闸门拒单，不会静默回落错误时长。
const (
	seedance20MinDuration = 5
	seedance20MaxDuration = 15
)

// validateSeedance20Duration 校验 Seedance 2.0 系（dola / 豆包 2.0 Fast 共用）
// 单字段的 5–15 秒区间口径：区间内任意整数秒**原样透传**（任意秒真跑，不吸附），
// 区间外报错。与 ArcReel `_doubao20_duration_or_default`/`_dola20_duration_or_default`
// 及 Worker 侧 normalizeDoubaoDuration/normalizeDolaDuration 同边界。
func validateSeedance20Duration(field string, value int, model string) (int, error) {
	if value < seedance20MinDuration || value > seedance20MaxDuration {
		return 0, fmt.Errorf("%s must be between 5 and 15 for %s, got %d", field, model, value)
	}
	return value, nil
}

var allowedRatios = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// 豆包官网参数白名单（2026-10-06 真机探针订正，与 Worker 侧
// cron_video_brach src/platforms/doubao/params.ts 同口径）：
//   - 时长分模型：2.5 四档 5/10/15/30；2.0 Fast 按 5–15 秒
//     区间**透传**（2026-10-12 任意秒真跑，见
//     validateSeedance20Duration——不再吸附），区间外直接 400——Worker 侧
//     durationHack 武装失败会硬闸门拒单，这里提前挡避免用户下单后才失败；
//     ⚠️ dola（seedance-2.0）不再是区间口径：2026-10-24 起锁定 10 秒
//     （dolaFixedDuration），与本表无关；
//   - 比例七档，含官网出厂默认档 `auto`（「自动 · 10s」）；
//   - 比例「未指定不编造」：豆包未指定时保持官网当前状态（App 侧
//     resolveDoubaoRatioLabel 空 → null 纪律），不强补 16:9。
var allowedDoubaoDurations = map[int]bool{
	5:  true,
	10: true,
	15: true,
	30: true,
}

var allowedDoubaoRatios = map[string]bool{
	"auto": true,
	"3:4":  true,
	"4:3":  true,
	"9:16": true,
	"16:9": true,
	"1:1":  true,
	"21:9": true,
}

// ============================
// Request / Response structures
// ============================

// clientRequest 是厂商侧请求体。seconds/duration/input_reference/images 等用
// json.RawMessage 承接，以便宽容解析数字/字符串、单值/数组等别名形态。
type clientRequest struct {
	Model          string          `json:"model"`
	Prompt         string          `json:"prompt"`
	Seconds        json.RawMessage `json:"seconds"`
	Duration       json.RawMessage `json:"duration"`
	Size           string          `json:"size"`
	Ratio          string          `json:"ratio"`
	AspectRatio    string          `json:"aspect_ratio"`
	Resolution     string          `json:"resolution"`
	InputReference json.RawMessage `json:"input_reference"`
	Images         json.RawMessage `json:"images"`
	// 参考图别名字段（2026-10-22 丢图事故）：第三方客户端用的字段名五花八门，
	// 白名单只认 input_reference / images 时，别名会被**静默丢弃** → 建单不带图
	// → 官网回「参考资源 图 1（老年）当前不存在」，额度照烧。
	// 这里把常见别名一并承接（见 referenceImageFields）。
	Image           json.RawMessage `json:"image"`
	ImageURL        json.RawMessage `json:"image_url"`
	ImageURLs       json.RawMessage `json:"image_urls"`
	ReferenceImage  json.RawMessage `json:"reference_image"`
	ReferenceImages json.RawMessage `json:"reference_images"`
	InputImages     json.RawMessage `json:"input_images"`
	// camelCase 变体：Go 的 encoding/json 匹配 json tag 时**忽略大小写但不忽略下划线**
	// ——「referenceImages」匹配不上 tag「reference_images」。而 JS/TS 生态的客户端
	// 默认就发 camelCase，不单列就是又一次静默丢图（2026-10-22 丢图事故的同类变种）。
	InputReferenceCamelCase  json.RawMessage `json:"inputReference"`
	ImageURLCamelCase        json.RawMessage `json:"imageUrl"`
	ImageURLsCamelCase       json.RawMessage `json:"imageUrls"`
	ReferenceImageCamelCase  json.RawMessage `json:"referenceImage"`
	ReferenceImagesCamelCase json.RawMessage `json:"referenceImages"`
	InputImagesCamelCase     json.RawMessage `json:"inputImages"`
	Videos                   json.RawMessage `json:"videos"`
	Video                    json.RawMessage `json:"video"`
	N                        json.RawMessage `json:"n"`
	Count                    json.RawMessage `json:"count"`
	IdempotencyKey           string          `json:"idempotency_key"`
}

// normalizedRequest 是校验通过后缓存在 gin.Context 中的规范化请求，
// 供 EstimateBilling / BuildRequestBody 复用（避免重复解析与二次校验）。
type normalizedRequest struct {
	Model          string
	Kind           string
	Prompt         string
	Duration       int
	Ratio          string
	Resolution     string
	Count          int
	Images         []string
	Videos         []string
	IdempotencyKey string
	// ReferenceSource 记录参考图来自哪个请求字段（可观测性）。
	//
	// 2026-10-22 丢图事故排查时最大的盲区就是「客户端到底传了什么字段名」：
	// new-api 不留存原始请求体（logs.other 只有 request_path/model_price），
	// 现场无法证明 threeFatAi 传的是 images 还是别的名字。这一行是补盲区的锚点。
	ReferenceSource string
	// ReferenceCount 是实际解析出的参考图张数（与 ReferenceSource 一起进日志）。
	ReferenceCount int
}

// submitRequest 是 ArcReel 建单载荷。视频走 videoParams、图片走 imageParams、
// 反解两者都不带（ArcReel 对 outputMode=prompt 要求 inputs 恰好 1 个参考视频）。
type submitRequest struct {
	PlatformId     string       `json:"platformId"`
	OutputMode     string       `json:"outputMode"`
	Prompt         string       `json:"prompt"`
	VideoParams    *videoParams `json:"videoParams,omitempty"`
	ImageParams    *imageParams `json:"imageParams,omitempty"`
	Inputs         []jobInput   `json:"inputs,omitempty"`
	IdempotencyKey string       `json:"idempotencyKey"`
}

type videoParams struct {
	Model      string `json:"model,omitempty"`
	Ratio      string `json:"ratio"`
	Duration   *int   `json:"duration,omitempty"`
	Resolution string `json:"resolution,omitempty"`
	// GenerationMode='edit' 让 ArcReel/Worker 明确走视频编辑链路。万相编辑模型专用；
	// 图生模型不带该字段。Worker 侧 RemoteTaskAdapter 同时按 reference_video 判定，
	// 故这是显式双保险，不是唯一开关。
	GenerationMode string `json:"generationMode,omitempty"`
}

type imageParams struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	Count       int    `json:"count"`
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
// ResultPrompt 是 outputMode=prompt（视频反解）的文本结果。
type jobResponse struct {
	JobID         string `json:"jobId"`
	Status        string `json:"status"`
	SourceURL     string `json:"sourceUrl"`
	SourceURLAlt  string `json:"source_url"`
	ResultPrompt  string `json:"resultPrompt"`
	ResultPrompt2 string `json:"result_prompt"`
	Error         string `json:"error"`
	// ErrorCode / ErrorDiagnostic：2026-10-18 ArcReel 的失败原因对内/对外拆分。
	//   Error           = 对外文案（官网拒绝类 = 官网原话原样），**给客户**；
	//   ErrorDiagnostic = Worker 上报的内部诊断原文（含 free-mode-guard / params-ok
	//                     等内部符号），**只进日志，绝不下发客户**；
	//   ErrorCode       = 稳定机器码（content_rejected / quota_exhausted / …）。
	// 老版本 ArcReel 不带后两个字段 → 空串，走既有回落，行为不变。
	ErrorCode       string `json:"errorCode"`
	ErrorDiagnostic string `json:"errorDiagnostic"`
	CreatedAt       string `json:"createdAt"`
	UpdatedAt       string `json:"updatedAt"`
}

func (r jobResponse) resultURL() string {
	return firstNonEmpty(r.SourceURL, r.SourceURLAlt)
}

func (r jobResponse) resultText() string {
	return firstNonEmpty(r.ResultPrompt, r.ResultPrompt2)
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

// ValidateRequestAndSetAction 解析厂商请求，按模型形态分派校验。
// 注意：ArcReel 只接受 http(s) 参考素材 URL，不支持 multipart 文件上传，
// 因此不走 ValidateBasicTaskRequest 的共享解析（其 seconds 字段仅支持字符串形态）。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 反解的天然入参是本地 mp4，客户端很容易直接 multipart 上传。先把这种请求挡在
	// 入口给出可读原因，而不是让 JSON 解析报一个语法错误。
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("%s", reasonDirectUploadUnsupported),
			"invalid_input_reference",
			http.StatusBadRequest,
		)
	}

	var req clientRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	spec, ok := SpecFor(req.Model)
	if !ok {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("model must be one of %s, got %q", strings.Join(ModelList(), ", "), strings.TrimSpace(req.Model)),
			"invalid_model",
			http.StatusBadRequest,
		)
	}

	// 幂等键口径：客户端显式传入则原样用（每次重试天然相同）；否则从 gin context
	// 取本请求缓存的 key，没有才生成新的并缓存 —— 保证重试复用同一个 key，让
	// ArcReel 的 (user_id, idempotency_key) 唯一索引 + 幂等重放真正生效。
	idemKey := req.IdempotencyKey
	if idemKey == "" {
		idemKey = c.GetString(CtxKeyManwuIdempotencyKey)
		if idemKey == "" {
			idemKey = idempotencyKeyPrefix + common.GetUUID()
		}
		c.Set(CtxKeyManwuIdempotencyKey, idemKey)
	}
	// 标记提交幂等：controller 的 shouldRetryTaskRelay 据此对 408/504/524 放行重试。
	c.Set(CtxKeyTaskSubmitIdempotent, true)

	norm := &normalizedRequest{
		Model:          strings.TrimSpace(req.Model),
		Kind:           spec.Kind,
		IdempotencyKey: idemKey,
	}

	if spec.Kind == kindReverse {
		// 反解建单允许空 prompt（ArcReel 对 outputMode=prompt 不要求 prompt）；
		// 带 prompt 也照传，当作给即梦助手的附加指令。
		norm.Prompt = strings.TrimSpace(req.Prompt)
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		videos, err := resolveVideos(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
		}
		norm.Videos = videos
		c.Set(normalizedRequestKey, norm)
		info.Action = constant.TaskActionGenerate
		return nil
	}

	// 其余形态（图片 / 两路视频）prompt 必填。
	norm.Prompt = strings.TrimSpace(req.Prompt)
	if norm.Prompt == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	// 豆包的比例在 kindDoubaoVideo 分支自行解析：官网含 auto 档且「未指定不编造」
	// （空 → 保持官网当前状态），与 dola「缺省补 16:9」口径不同，公共段不适用。
	// Picsart 同理：官网默认档是 adaptive（跟随输入图比例），缺省不能补成 16:9。
	if spec.Kind != kindDoubaoVideo && spec.Kind != kindPicsartVideo {
		ratio, err := resolveRatio(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_ratio", http.StatusBadRequest)
		}
		norm.Ratio = ratio
	}

	images, referenceSource, err := resolveReferenceImages(req)
	if err != nil {
		return rejectReferenceRequest(c, req.Model, err)
	}
	norm.Images = images
	norm.ReferenceSource = referenceSource
	norm.ReferenceCount = len(images)
	// 参考图来源必须进日志：new-api 不留存原始请求体，丢图事故排查时
	// 「客户端到底传了什么字段名」是唯一的证据（2026-10-22 事故的盲区）。
	logger.LogInfo(c, fmt.Sprintf("manwu 参考图解析：model=%s source=%s count=%d",
		norm.Model, firstNonEmpty(referenceSource, "<none>"), len(images)))

	// 提示词引用了参考图、实际却没带够 → 建单前拒（2026-10-22 丢图事故）。
	// 不拒的话，这一单会一路跑到官网，官网回「参考资源 图 1（老年）当前不存在」，
	// 额度照烧、用户白等 —— 入口拒是最便宜的一刀。
	if err := assertPromptReferenceCoverage(norm.Prompt, images); err != nil {
		return rejectReferenceRequest(c, norm.Model,
			newReferenceRejection(referenceReasonCoverage, err))
	}

	switch spec.Kind {
	case kindDolaVideo:
		duration, err := resolveDuration(req, norm.Model)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Duration = duration
	case kindDoubaoVideo:
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		// 豆包比例「未指定不编造」：公共段 resolveRatio 已把空值填成 16:9（dola
		// 口径），豆包要用原始值覆盖回去 —— 空值原样透传，让 Worker 保持官网
		// 当前状态（出厂「自动」档），而不是静默改掉用户没选的参数。
		rawRatio := firstNonEmpty(
			strings.TrimSpace(req.Size),
			strings.TrimSpace(req.Ratio),
			strings.TrimSpace(req.AspectRatio),
		)
		if rawRatio == "" {
			norm.Ratio = ""
		} else {
			if !allowedDoubaoRatios[rawRatio] {
				return service.TaskErrorWrapperLocal(
					fmt.Errorf("ratio must be one of auto/16:9/9:16/1:1/4:3/3:4/21:9, got %q", rawRatio),
					"invalid_ratio", http.StatusBadRequest,
				)
			}
			norm.Ratio = rawRatio
		}
		duration, err := resolveDoubaoDuration(req, norm.Model)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Duration = duration
	case kindDola25Video:
		duration, err := resolveFixedDuration(req, norm.Model, 30)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		norm.Duration = duration
	case kindVeoVideo:
		// Veo 官网只有宽高比控件（2026-09-22 真机验证）：模型/时长/分辨率没有可实现
		// 的控件，Worker 侧会 fail-fast 拒单。这里提前 400，别让用户白付一次预扣。
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
	case kindImage:
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		count, err := resolveImageCount(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_count", http.StatusBadRequest)
		}
		norm.Count = count
	case kindWanI2VVideo:
		// 万相图生/文生：prompt + 参考图（≤2）→ 视频。不收参考视频（那是编辑路线）。
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		if hasValue(req.Videos) || hasValue(req.Video) {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("%s is the image-to-video route and accepts no reference video; use %s instead", norm.Model, ModelWan27Edit),
				"invalid_input_reference", http.StatusBadRequest,
			)
		}
		// 图生路线的参考图上限比编辑路线严（2 vs 10）：官网首/尾帧 + 参考图通道的
		// 既有约束。resolveReferenceImages 只卡全局 10，这里按 kind 再收一道。
		if len(images) > wanMaxI2VImages {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("%s accepts at most %d reference images, got %d", norm.Model, wanMaxI2VImages, len(images)),
				"invalid_input_reference", http.StatusBadRequest,
			)
		}
		norm.Duration, err = resolveWanDuration(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Resolution, err = resolveWanResolution(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_resolution", http.StatusBadRequest)
		}
	case kindWanEditVideo:
		// 万相视频编辑：主视频恰 1 个（ArcReel/Worker 硬约束，多传只取首个 → 明确拒绝），
		// 参考图 ≤10，时长跟随主视频（5–15 逐秒）。
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		videos, vErr := resolveReferenceVideos(req)
		if vErr != nil {
			return service.TaskErrorWrapperLocal(vErr, "invalid_input_reference", http.StatusBadRequest)
		}
		norm.Videos = videos
		if len(images) > wanMaxEditImages {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("%s accepts at most %d reference images, got %d", norm.Model, wanMaxEditImages, len(images)),
				"invalid_input_reference", http.StatusBadRequest,
			)
		}
		norm.Duration, err = resolveWanDuration(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Resolution, err = resolveWanResolution(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_resolution", http.StatusBadRequest)
		}
	case kindPicsartVideo:
		// Picsart 免费无限档 Wan 3.0（2026-10-24 老板定稿上架）：固定 30 秒、
		// 分辨率 480P/720P、比例六档（含 adaptive）、参考图 ≤10、不收参考视频。
		// 与 Worker 侧 src/platforms/picsart/params.ts 的官网档位同源但**收窄**。
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		// Picsart 官网这条路线只吃参考图；参考视频是别的平台的素材形态，
		// 静默丢弃会让客户白付一次，入口直接拒。
		if hasValue(req.Videos) || hasValue(req.Video) {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("%s accepts reference images only, not reference video", norm.Model),
				"invalid_input_reference", http.StatusBadRequest,
			)
		}
		if len(images) > picsartMaxReferenceImages {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("%s accepts at most %d reference images, got %d", norm.Model, picsartMaxReferenceImages, len(images)),
				"invalid_input_reference", http.StatusBadRequest,
			)
		}
		// 固定 30 秒：客户端传 10 秒 → **400 拒绝**，不静默改成 30
		// （客户会以为拿到的是 10 秒，成片出来才发现，等于错交付）。
		duration, err := resolveFixedDuration(req, norm.Model, picsartFixedDuration)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Duration = duration
		// 比例「未指定不编造」：空 → 不下发，让官网用它自己的 adaptive 默认档。
		rawRatio := firstNonEmpty(
			strings.TrimSpace(req.Size),
			strings.TrimSpace(req.Ratio),
			strings.TrimSpace(req.AspectRatio),
		)
		if rawRatio == "" {
			norm.Ratio = ""
		} else {
			normalizedRatio := normalizePicsartRatio(rawRatio)
			if normalizedRatio == "" {
				return service.TaskErrorWrapperLocal(
					fmt.Errorf("ratio must be one of adaptive/16:9/9:16/1:1/4:3/3:4, got %q", rawRatio),
					"invalid_ratio", http.StatusBadRequest,
				)
			}
			norm.Ratio = normalizedRatio
		}
		resolution, resErr := resolvePicsartResolution(req)
		if resErr != nil {
			return service.TaskErrorWrapperLocal(resErr, "invalid_resolution", http.StatusBadRequest)
		}
		norm.Resolution = resolution
	}

	c.Set(normalizedRequestKey, norm)
	info.Action = constant.TaskActionGenerate
	return nil
}

// EstimateBilling 只对需要「基价 × 倍率」的模型返回倍率：
// 豆包 2.0 Fast 按秒（¥0.1/秒 × 秒数）、图片按张（张数作为 n 倍率乘到 ¥0.3/张 上）、
// Picsart 特惠档按分辨率（720P 取 480P 基价的 1.333…）。
//
// 其余按次模型（豆包 2.5、dola 两档、Gemini Veo、反解）必须返回 nil，否则
// relay_task.go 会把按次价再乘一遍（¥3/次 × 30 秒 = ¥90，账实不符）。
//
// ⚠️ picsart 是**故意**返回倍率的例外：对外价是「480P ¥1.50 / 720P ¥2.00」，
// 而 ModelPrice 只能配一个数、且模型广场的徽标直接显示它。老板 2026-10-25
// 定稿：**基价配最低档 480P 的 ¥1.50**，让徽标显示「¥1.50 / 请求」；
// 720P 用 size=2.0/1.5 乘回 ¥2.00。
//   - 反过来配 720P 基价 ¥2.00 会让徽标显示 ¥2，而客户最低能买到 ¥1.50，
//     看着像涨价（这就是本次调整的起因）。
//   - 缺省分辨率在归一阶段已落 720P，所以走倍率分支，不会漏乘。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if c == nil {
		return nil
	}
	req, err := getNormalizedRequest(c)
	if err != nil {
		return nil
	}
	if req.Kind == kindImage && req.Count > 1 {
		return map[string]float64{"n": float64(req.Count)}
	}
	if req.Kind == kindDoubaoVideo && req.Model == ModelDoubao20Fast && req.Duration > 0 {
		return map[string]float64{"seconds": float64(req.Duration)}
	}
	// Picsart 特惠档：480P 是基准价（返回 nil = 不乘，¥1.50），720P 乘 1.333… → ¥2.00。
	// 改价时 ModelPrice 与本倍率必须一起改（见 picsart720PSizeRatio 注释）。
	if req.Kind == kindPicsartVideo && req.Resolution == "720P" {
		return map[string]float64{"size": picsart720PSizeRatio}
	}
	return nil
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
	spec, ok := SpecFor(req.Model)
	if !ok {
		return nil, fmt.Errorf("unknown manwu model: %s", req.Model)
	}

	body := submitRequest{
		PlatformId:     spec.PlatformID,
		OutputMode:     spec.OutputMode,
		Prompt:         req.Prompt,
		IdempotencyKey: req.IdempotencyKey,
	}

	switch spec.Kind {
	case kindReverse:
		// inputs 恰好 1 个 reference_video；不带 videoParams/imageParams
		// （ArcReel 对 outputMode=prompt 会拒绝其它参数形态）。
		body.Inputs = []jobInput{{Type: InputTypeVideo, URL: req.Videos[0]}}
	case kindImage:
		body.ImageParams = &imageParams{AspectRatio: req.Ratio, Count: req.Count}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	case kindVeoVideo:
		// 只传宽高比：官网没有时长/分辨率控件，传了也会被 ArcReel 剥掉，写进去只会
		// 让人误以为我们执行了用户请求的时长。
		body.VideoParams = &videoParams{Ratio: req.Ratio}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	case kindDolaVideo, kindDola25Video:
		body.VideoParams = &videoParams{Model: req.Model, Ratio: req.Ratio, Duration: &req.Duration}
		if spec.Kind == kindDola25Video {
			body.VideoParams.Resolution = "720p"
		}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	case kindDoubaoVideo:
		// 豆包：model 字段传官网档位真名（公开短名在 spec.UpstreamModel 翻译，
		// ArcReel/Worker 两侧白名单再归一一次）；ratio 可为空串（未指定不编造，
		// Worker 端 resolveDoubaoRatioLabel 空 → null → 保持官网当前状态）；
		// 豆包无分辨率控件，不传 resolution。
		body.VideoParams = &videoParams{Model: firstNonEmpty(spec.UpstreamModel, req.Model), Ratio: req.Ratio, Duration: &req.Duration}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	case kindWanI2VVideo, kindWanEditVideo:
		// 万相免灵感值：model 固定官网 id Wan2_7（公开名 wan2.7-图生/编辑 只在本表内），
		// ratio/duration/resolution 透传。Worker 侧 RemoteTaskAdapter 对 tongyiwan
		// 已硬编码免灵感值 + Wan2_7 兜底，这里显式写 model 只是让 job 记录自解释。
		body.VideoParams = &videoParams{
			Model:      wanUpstreamModel,
			Ratio:      req.Ratio,
			Duration:   &req.Duration,
			Resolution: req.Resolution,
		}
		if spec.Kind == kindWanEditVideo {
			// 编辑链路双保险：Worker 侧 RemoteTaskAdapter 同时按 reference_video
			// 判定 isEditMode，这里显式带 generationMode 让 ArcReel job 记录可读。
			body.VideoParams.GenerationMode = "edit"
			// 主视频走 reference_video 素材槽，Worker 据此分流到编辑 DOM 路线。
			body.Inputs = append(body.Inputs, jobInput{Type: InputTypeVideo, URL: req.Videos[0]})
		}
		// 图生的首/尾帧与编辑的参考图都走 reference_image 槽。
		body.Inputs = append(body.Inputs, toInputs(InputTypeImage, req.Images)...)
	case kindPicsartVideo:
		// Picsart 特惠档 Wan 3.0：model 恒为官网档位 id（公开名 wan-3.0-特惠
		// 只在本表内存在，不对 ArcReel/Worker 暴露）；时长 30 秒固定、分辨率
		// 480P/720P 已在校验层归一。ratio 可为空串（未指定不编造 → Worker 端
		// 保持官网 adaptive 默认档，不硬塞 16:9）。
		body.VideoParams = &videoParams{
			Model:      firstNonEmpty(spec.UpstreamModel, picsartUpstreamModel),
			Ratio:      req.Ratio,
			Duration:   &req.Duration,
			Resolution: req.Resolution,
		}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	default:
		return nil, fmt.Errorf("unsupported manwu request kind: %s", spec.Kind)
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(data), nil
}

func toInputs(inputType string, urls []string) []jobInput {
	if len(urls) == 0 {
		return nil
	}
	inputs := make([]jobInput, 0, len(urls))
	for _, u := range urls {
		inputs = append(inputs, jobInput{Type: inputType, URL: u})
	}
	return inputs
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
	// 订单号对账（2026-10-14 用户要求）：公开单号 task_xxx 与 ArcReel 上游单号
	// gen-xxx 一起落系统日志，排查时在 newapi / ArcReel / App 三方 Ctrl-F 同一个
	// id 即可对上同一笔订单，不再靠时间+提示词猜配对。
	//
	// 参考图来源字段名与张数一并落日志（2026-10-22 丢图事故的排查盲区：
	// new-api 不留存原始请求体，这一行是「客户端到底传了什么字段名」的唯一定位点）。
	referenceSource, referenceCount := "<none>", 0
	if norm, err := getNormalizedRequest(c); err == nil && norm != nil {
		referenceSource = firstNonEmpty(norm.ReferenceSource, "<none>")
		referenceCount = norm.ReferenceCount
	}
	logger.LogInfo(c, fmt.Sprintf("manwu 任务已提交 task_id=%s upstream_job_id=%s model=%s ref_source=%s ref_count=%d",
		info.PublicTaskID, taskID, info.OriginModelName, referenceSource, referenceCount))
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
	return ModelList()
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
	status := strings.ToLower(strings.TrimSpace(res.Status))
	switch status {
	case "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "assigned", "accepted", "submitting", "provider_running", "provider_succeeded", "url_validating":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "ready":
		taskResult.Progress = taskcommon.ProgressComplete
		raw := res.resultURL()
		taskResult.ResultText = res.resultText()
		switch {
		case directResultURL(raw) != "":
			// 公网直链：直接交付。
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Url = directResultURL(raw)
		case strings.TrimSpace(raw) != "":
			// ArcReel 自有托管路径（非公网、需渠道密钥）：交付有效，但由
			// new-api 的 content 代理回源，这里不吐地址。
			taskResult.Status = model.TaskStatusSuccess
		case taskResult.ResultText != "":
			// 视频反解：文本即交付物，没有媒体 URL。
			taskResult.Status = model.TaskStatusSuccess
		default:
			// ready 但什么都没给：按失败收敛，让轮询阶段退款。
			taskResult.Status = model.TaskStatusFailure
			taskResult.Reason = reasonEmptyResult
		}
	case "failed", "url_unavailable":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		if status == "url_unavailable" {
			taskResult.Reason = firstNonEmpty(res.Error, reasonOutputUnavailable)
		} else {
			// res.Error 是 ArcReel 的**对外**文案；为空时给中性兜底，
			// 不能落到「内容审核不通过」（那会把基础设施问题谎报成内容违规）。
			taskResult.Reason = firstNonEmpty(res.Error, reasonUnknownFailure)
		}
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
	// 文本结果以落库的 PrivateData.ResultText 为准（权威副本）；task.Data 只是上游
	// 响应的快照，经脱敏/重写后可能缺字段。反解提示词很长，更不能只靠快照。
	text := strings.TrimSpace(task.PrivateData.ResultText)

	status := strings.ToLower(strings.TrimSpace(res.Status))
	switch status {
	case "ready":
		raw := strings.TrimSpace(res.resultURL())
		if text == "" {
			text = res.resultText()
		}
		switch {
		case directResultURL(raw) != "":
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("url", directResultURL(raw))
			openAIResp.SetMetadata("media_type", inferMediaType(raw))
		case raw != "":
			// 自有托管路径：回落到 content 代理地址，客户端拿到的一定能打开。
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("url", taskcommon.BuildProxyURL(task.TaskID))
			openAIResp.SetMetadata("media_type", inferMediaType(raw))
		case text != "":
			// 反解：提示词就是全部交付物。**不给 url**——写一个指向 content 代理的
			// 占位地址会诱使客户端去拉一个根本不存在的媒体文件（代理只会 502）。
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("prompt", text)
			openAIResp.SetMetadata("media_type", "text")
		default:
			openAIResp.Status = dto.VideoStatusFailed
			openAIResp.Error = &dto.OpenAIVideoError{Code: "empty_result", Message: reasonEmptyResult}
		}
	case "failed", "url_unavailable":
		// 2026-10-18 用户拍板：对外只给 res.Error（ArcReel 的 user_message，
		// 官网拒绝类就是官网原话原样），内部诊断原文只进日志。
		// 兜底**不能**再用「内容审核不通过」——上游没给原因时那是谎报归因。
		fallback := reasonUnknownFailure
		if status == "url_unavailable" {
			// url_unavailable 是基础设施侧产物校验失败，不能归因为内容审核。
			fallback = reasonOutputUnavailable
		}
		if diag := strings.TrimSpace(res.ErrorDiagnostic); diag != "" && diag != strings.TrimSpace(res.Error) {
			// 只记日志，不进响应体。errorCode 一起带上，便于按码统计。
			logger.LogDebug(context.Background(), "manwu job failed: task=%s code=%s diagnostic=%s",
				task.TaskID, res.ErrorCode, diag)
		}
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    strings.ToLower(strings.TrimSpace(res.Status)),
			Message: firstNonEmpty(res.Error, fallback),
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

// rejectFields 拒绝该任务形态不支持、且**无法诚实执行**的字段。
//
// 口径与 Worker 侧一致（RemoteTaskAdapter 对 Gemini/Veo 的 unsupported params 直接
// 拒单）：宁可入口 400，也不要「过滤后伪装成已按请求执行」——用户会为一个没跑的
// 参数付费。
func rejectFields(req clientRequest, kind, modelName string) error {
	switch kind {
	case kindDoubaoVideo:
		// 豆包官网无分辨率控件（App 侧无 resolution 处理链路），显式拒绝。
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
	case kindDola25Video:
		resolution := strings.TrimSpace(req.Resolution)
		if resolution != "" && !strings.EqualFold(resolution, "720p") {
			return fmt.Errorf("%s is fixed at 720p", modelName)
		}
	case kindVeoVideo:
		if hasValue(req.Seconds) {
			return fmt.Errorf("%s is fixed-duration web Veo: seconds/duration is not supported", modelName)
		}
		if hasValue(req.Duration) {
			return fmt.Errorf("%s is fixed-duration web Veo: seconds/duration is not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
	case kindImage:
		if hasValue(req.Seconds) || hasValue(req.Duration) {
			return fmt.Errorf("%s is an image model: seconds/duration is not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
	case kindReverse:
		if hasValue(req.Seconds) || hasValue(req.Duration) {
			return fmt.Errorf("%s duration is fixed by the skill and is not supported", modelName)
		}
		if strings.TrimSpace(req.Size) != "" || strings.TrimSpace(req.Ratio) != "" ||
			strings.TrimSpace(req.AspectRatio) != "" {
			return fmt.Errorf("%s produces text, so size/ratio/aspect_ratio are not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
		// 反解只收一个参考视频：任何形态的参考图字段都不该出现（别名也算，
		// 否则客户端用 image_urls 传图会被静默忽略，任务照跑、图全丢）。
		if hasAnyReferenceImage(req) {
			return fmt.Errorf("%s accepts exactly one reference video, not images", modelName)
		}
	case kindWanI2VVideo, kindWanEditVideo:
		// 万相两条路线都是视频模型：n/count 无意义（照旧按次计费，1 请求 = 1 条视频）。
		if hasValue(req.Count) || hasValue(req.N) {
			return fmt.Errorf("%s is a video model: n/count is not supported (billed per request)", modelName)
		}
	}
	return nil
}

// resolveDuration 解析 dola seedance-2.0 时长：seconds（首选，数字或数字字符串）
// → duration → 缺省 15。口径为 **5–15 秒区间任意值真跑**：
// 区间内任意整数秒原样透传（真机实证官网后端接受任意值）；缺省或 null 视为
// 未指定；字段存在但非法（格式错误或越出 5–15）直接报错——上游 ArcReel 对
// 非法值会静默回落 15，厂商侧必须显式拒绝，避免计费时长与实际生成时长脱节。
func resolveDuration(req clientRequest, model string) (int, error) {
	seconds, found, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	if found {
		return validateSeedance20Duration("seconds", seconds, model)
	}

	duration, found, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if found {
		return validateSeedance20Duration("duration", duration, model)
	}

	return defaultDuration, nil
}

// resolveWanDuration 解析万相（图生/编辑）时长：seconds → duration → 缺省 5。
// 口径 5–15 秒逐秒（与 Worker 侧 TONGYIWAN_UNLIMITED_VIDEO_PROFILE.durations 同源），
// 越界显式报错 —— ArcReel 侧对非法值会静默回落 5，厂商侧必须显式拒绝，
// 否则「要 30 秒」的用户会按默认价拿到 5 秒片子。
func resolveWanDuration(req clientRequest) (int, error) {
	seconds, found, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	if found {
		return validateWanDuration("seconds", seconds)
	}

	duration, found, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if found {
		return validateWanDuration("duration", duration)
	}

	return wanDefaultDuration, nil
}

func validateWanDuration(field string, value int) (int, error) {
	if value < wanMinDuration || value > wanMaxDuration {
		return 0, fmt.Errorf("%s must be between %d and %d seconds, got %d", field, wanMinDuration, wanMaxDuration, value)
	}
	return value, nil
}

// resolveWanResolution 解析万相分辨率：未指定返回空串（ArcReel 回落 720P，
// 与 Worker defaultResolution 一致）；指定了就必须是 720P/1080P（大小写不敏感）。
func resolveWanResolution(req clientRequest) (string, error) {
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" {
		return "", nil
	}
	normalized := strings.ToUpper(raw)
	if !wanAllowedResolutions[normalized] {
		return "", fmt.Errorf("resolution must be one of 720P/1080P, got %q", raw)
	}
	return normalized, nil
}

// normalizePicsartRatio 把客户端各种写法归一到 Picsart 官网的六档比例。
// 返回空串 = 不在白名单（调用方负责 400，**不回落默认值**）。
//
// 归一规则与 Worker 侧 src/platforms/picsart/params.ts 的
// normalizePicsartAspectRatio 同口径：`auto` / `自适应` 都收敛到官网的 `adaptive`，
// `16/9`、`16：9`、`16x9` 统一成 `16:9`。
func normalizePicsartRatio(raw string) string {
	text := strings.TrimSpace(raw)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(strings.ReplaceAll(text, " ", ""))
	switch lower {
	case "auto", "adaptive", "自适应", "自适应比例":
		return "adaptive"
	}
	normalized := strings.NewReplacer("：", ":", "/", ":", "x", ":", "×", ":").Replace(lower)
	if !picsartAllowedRatios[normalized] {
		return ""
	}
	return normalized
}

// resolvePicsartResolution 解析分辨率：只认 480P/720P（大小写不敏感）。
//
// 缺省给 720P 而不是空：Picsart 官网默认档是 1080P，本商品**不开放 1080P**，
// 不下发就会让官网出客户没买的档（更慢、也没溢价）。这与「不编造用户没选的值」
// 不冲突 —— 开放集合里必须有确定默认，否则默认值落在开放集合之外。
func resolvePicsartResolution(req clientRequest) (string, error) {
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" {
		return picsartDefaultResolution, nil
	}
	normalized := strings.ToUpper(strings.ReplaceAll(raw, " ", ""))
	if !picsartAllowedResolutions[normalized] {
		return "", fmt.Errorf("resolution must be one of 480P/720P, got %q", raw)
	}
	return normalized, nil
}
func resolveReferenceVideos(req clientRequest) ([]string, error) {
	videos, err := resolveVideos(req)
	if err != nil {
		return nil, err
	}
	if len(videos) != 1 {
		return nil, fmt.Errorf("video edit requires exactly one reference video, got %d", len(videos))
	}
	return videos, nil
}

// doubaoDefaultDuration 与 Worker 侧 DOUBAO_DEFAULT_DURATION（5s 档）一致。
const doubaoDefaultDuration = 5

// resolveDoubaoDuration 解析豆包时长：seconds（首选，数字或数字字符串）→ duration
// → 缺省 5。口径分模型：2.5 四档白名单 5/10/15/30；2.0 Fast 按 **5–15 秒区间**
// 透传（2026-10-12 任意秒真跑，与 dola 同口径，不再吸附）。缺省或 null 视为
// 未指定；字段存在但非法直接报错——与 resolveFixedDuration 同纪律，避免计费时长与
// 实际生成时长脱节（ArcReel 侧对非法值会静默回落 5）。
func resolveDoubaoDuration(req clientRequest, model string) (int, error) {
	seconds, found, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	if found {
		return checkDoubaoDuration("seconds", seconds, model)
	}

	duration, found, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if found {
		return checkDoubaoDuration("duration", duration, model)
	}

	return doubaoDefaultDuration, nil
}

// checkDoubaoDuration 豆包时长口径：2.0 Fast = 5–15 秒区间透传（任意秒真跑）；
// 2.5 = 5/10/15/30 四档白名单。
func checkDoubaoDuration(field string, value int, model string) (int, error) {
	if model == ModelDoubao20Fast {
		return validateSeedance20Duration(field, value, model)
	}
	if !allowedDoubaoDurations[value] {
		return 0, fmt.Errorf("%s must be one of 5/10/15/30 for %s, got %d", field, model, value)
	}
	return value, nil
}

// resolveFixedDuration 解析固定时长模型：未指定时使用固定值，显式传入其它值直接拒绝。
func resolveFixedDuration(req clientRequest, modelName string, expected int) (int, error) {
	seconds, secondsFound, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	duration, durationFound, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if secondsFound && seconds != expected {
		return 0, fmt.Errorf("%s duration is fixed at %d seconds, got %d", modelName, expected, seconds)
	}
	if durationFound && duration != expected {
		return 0, fmt.Errorf("%s duration is fixed at %d seconds, got %d", modelName, expected, duration)
	}
	return expected, nil
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

// referenceImageField 描述一个「参考图候选字段」：名字 + 取值函数。
type referenceImageField struct {
	Name string
	Raw  func(req clientRequest) json.RawMessage
}

// referenceImageFields 是参考图字段候选名，按优先级排列（第一个有值的字段胜出）。
//
// 2026-10-22 丢图事故：旧白名单只有 input_reference / images，第三方客户端用了
// 别的字段名 → 字段被**静默丢弃** → 建单载荷没有 inputs → 官网回
// 「参考资源 图 1（老年）当前不存在」，额度照烧。
//
// 为什么不「多字段合并」而是「第一个有值的胜出」：合并会把同一张图重复传两次
// （客户端同时传 images 和 image_urls 时），反而制造新的错位。
var referenceImageFields = []referenceImageField{
	{"input_reference", func(r clientRequest) json.RawMessage { return r.InputReference }},
	{"inputReference", func(r clientRequest) json.RawMessage { return r.InputReferenceCamelCase }},
	{"images", func(r clientRequest) json.RawMessage { return r.Images }},
	{"image", func(r clientRequest) json.RawMessage { return r.Image }},
	{"image_url", func(r clientRequest) json.RawMessage { return r.ImageURL }},
	{"imageUrl", func(r clientRequest) json.RawMessage { return r.ImageURLCamelCase }},
	{"image_urls", func(r clientRequest) json.RawMessage { return r.ImageURLs }},
	{"imageUrls", func(r clientRequest) json.RawMessage { return r.ImageURLsCamelCase }},
	{"reference_image", func(r clientRequest) json.RawMessage { return r.ReferenceImage }},
	{"referenceImage", func(r clientRequest) json.RawMessage { return r.ReferenceImageCamelCase }},
	{"reference_images", func(r clientRequest) json.RawMessage { return r.ReferenceImages }},
	{"referenceImages", func(r clientRequest) json.RawMessage { return r.ReferenceImagesCamelCase }},
	{"input_images", func(r clientRequest) json.RawMessage { return r.InputImages }},
	{"inputImages", func(r clientRequest) json.RawMessage { return r.InputImagesCamelCase }},
}

// ── 参考图拒绝原因（机器可读，2026-10-23）──
//
// 过去「参考图相关的失败」全叫 `invalid_input_reference`：张数超上限、非 http URL、
// 字段形态解析失败、提示词引用数不足 —— 四件完全不同的事共用一个错误码，
// 排查时只能去翻 message 文本猜「是图太多」还是「是图丢了」。
//
// 现在 message 追加稳定的 reason 标识（**错误码本身不变**：客户端可能按 code
// 分支处理，改码是破坏性变更；原文案前缀也不动，客户端按前缀匹配仍能命中）。
const (
	referenceReasonTooMany     = "reference_images_too_many"
	referenceReasonNotHTTPURL  = "reference_image_not_http_url"
	referenceReasonParseFailed = "reference_image_parse_failed"
	referenceReasonCoverage    = "prompt_reference_coverage_mismatch"
)

// referenceRejectionError 是「参考图被拒」的带原因错误。
type referenceRejectionError struct {
	Reason string
	Err    error
}

func (e *referenceRejectionError) Error() string { return e.Err.Error() }
func (e *referenceRejectionError) Unwrap() error { return e.Err }

// newReferenceRejection 给错误打上机器可读的拒绝原因。
func newReferenceRejection(reason string, err error) error {
	return &referenceRejectionError{Reason: reason, Err: err}
}

// referenceRejectionReason 从错误里取拒绝原因；不是参考图拒绝时返回空串。
func referenceRejectionReason(err error) string {
	var rejection *referenceRejectionError
	if errors.As(err, &rejection) {
		return rejection.Reason
	}
	return ""
}

// rejectReferenceRequest 是参考图相关拒绝的**统一出口**：
//   - 打一行结构化日志（reason + model + 原文案）→ 排查时不用翻 message 文本；
//   - message 末尾追加稳定的 reason 标识，客户端也能机器可读地分辨
//     「是图太多」还是「是图丢了」。
//
// ⚠️ 错误码保持 `invalid_input_reference` 不变（客户端可能按 code 分支处理）；
// 原文案前缀也不动（客户端按前缀匹配仍能命中）。
func rejectReferenceRequest(c *gin.Context, model string, err error) *dto.TaskError {
	reason := referenceRejectionReason(err)
	if reason == "" {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	logger.LogWarn(c, fmt.Sprintf("manwu 参考图拒绝：reason=%s model=%s detail=%s",
		reason, model, err.Error()))
	return service.TaskErrorWrapperLocal(
		fmt.Errorf("%w（reason=%s）", err, reason),
		"invalid_input_reference", http.StatusBadRequest,
	)
}

// resolveReferenceImages 解析参考图：按 referenceImageFields 的优先级取第一个有值的字段。
// 每张必须是 http(s) URL（ArcReel 不接受 multipart/data URI）。
//
// 解析失败**必须报错**：旧实现两种形态都解析不了就静默返回 nil，等于把客户端
// 传来的图悄悄丢掉（2026-10-22 丢图事故的放大器之一）。
//
// 返回值里的 source 是命中的字段名，用于日志与规范化请求（可观测性）。
func resolveReferenceImages(req clientRequest) ([]string, string, error) {
	for _, field := range referenceImageFields {
		raw := field.Raw(req)
		if !hasValue(raw) {
			continue
		}
		images, err := parseStringList(raw)
		if err != nil {
			return nil, field.Name, newReferenceRejection(referenceReasonParseFailed,
				fmt.Errorf("%s: %w", field.Name, err))
		}
		if len(images) == 0 {
			continue
		}
		if len(images) > maxReferenceImages {
			return nil, field.Name, newReferenceRejection(referenceReasonTooMany,
				fmt.Errorf("manwu accepts at most %d reference images, got %d", maxReferenceImages, len(images)))
		}
		for _, image := range images {
			if err := assertHTTPURL(image, "reference image"); err != nil {
				return nil, field.Name, newReferenceRejection(referenceReasonNotHTTPURL, err)
			}
		}
		return images, field.Name, nil
	}
	return nil, "", nil
}

// hasAnyReferenceImage 判断请求里是否带了任何形态的**参考图**字段（只看「有没有带」），
// 用于反解链路拒绝「图片 + 视频」混传。
//
// 注意 input_reference 不算图片：反解链路里 input_reference 装的是**参考视频**
// （resolveVideos 复用同一字段），把它算成图片会把所有反解请求都毙掉。
// camelCase 的 inputReference 同理（那是同一个字段的另一种写法）。
func hasAnyReferenceImage(req clientRequest) bool {
	for _, field := range referenceImageFields {
		if field.Name == "input_reference" || field.Name == "inputReference" {
			continue
		}
		if hasValue(field.Raw(req)) {
			return true
		}
	}
	return false
}

// ── 提示词里的参考图引用（2026-10-22 丢图事故）──
//
// 事故 prompt（真实单 gen-bb57f7f1cd19c75326cf5b78）：
// 「【人物锁定】严格使用参考图，男受访者 = 参考图1中的人物。@图2/@图1/@图3」
// —— 引用了三张图，但派单素材是空的。
//
// ⚠️ **三层各有一道同口径的闸**：网关入口（本文件）→ ArcReel 建单
// （server/routers/remote_generation.py 的 _assert_prompt_reference_coverage）
// → Worker 提交前（ranjing-ai electron/remote/remoteReferenceGuard.ts 的
// checkRemoteReferenceCoverage）。**三个仓库都是我们自己的**（同一产品的三个项目，
// 详见各仓 AGENTS.md 的「三项目关系链路」），改任一端的判据必须同步另两端 ——
// 口径不同就会出现「一层拦、一层放行」，闸门形同虚设。
//
// 判据：
// - 位置式引用只认中文形式 `@图片N` / `@图N`（豆包官网与漫屋工作流的写法）；
// - 期望张数 = **最大编号**（`@图片3` → 3 张），位置绑定语义；
// - `@imageNN` 是别的系统的**素材名 token**，不能当位置序号判（撞号会把合法任务拦死），
//   只用于「提示词引用了图、实际一张都没带」的零图兜底。

var (
	// promptPositionRefRe 匹配位置式引用：@图片N / @图N。
	promptPositionRefRe = regexp.MustCompile(`@(图片|图)(\d+)`)
	// promptImageTokenRe 匹配全部图片引用 token（含英文素材名式 @imageNN）。
	promptImageTokenRe = regexp.MustCompile(`(?i)@(图片|图|image)(\d+)`)
	// promptRefIndexRe 从标签里取编号。
	promptRefIndexRe = regexp.MustCompile(`\d+`)
)

// findPromptRefs 找出提示词里的引用标签，并按「@ 前不能是字母/下划线」的边界规则过滤：
// mail@图片1 不是引用；而 @图1@图2 这种紧挨着的两个标签都要认出来。
func findPromptRefs(prompt string, re *regexp.Regexp) []string {
	if prompt == "" {
		return nil
	}
	matches := re.FindAllStringSubmatchIndex(prompt, -1)
	tags := make([]string, 0, len(matches))
	seen := make(map[string]bool, len(matches))
	for _, match := range matches {
		start := match[0]
		if start > 0 {
			previous, size := utf8.DecodeLastRuneInString(prompt[:start])
			if size > 0 && (previous == '_' || isASCIILetter(previous)) {
				continue
			}
		}
		tag := prompt[match[0]:match[1]]
		if seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

func isASCIILetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// promptReferenceExpectation 返回提示词暗示的参考图张数（= 位置引用的最大编号）
// 与命中的标签。没有位置式引用时返回 0。
func promptReferenceExpectation(prompt string) (int, []string) {
	tags := findPromptRefs(prompt, promptPositionRefRe)
	expected := 0
	for _, tag := range tags {
		index, err := strconv.Atoi(promptRefIndexRe.FindString(tag))
		if err == nil && index > expected {
			expected = index
		}
	}
	return expected, tags
}

// assertPromptReferenceCoverage 校验「提示词引用的参考图数」与「实际带上的张数」。
//
// 两条规则：
//   - 位置绑定：引用里的最大编号是需要的张数，实际张数不足 → 拒（@图片3 指向第 3 张，
//     只有 1 张图时第 3 张根本不存在）；
//   - 零图兜底：提示词里出现任何图片引用 token，而实际一张图都没带 → 拒。
//
// 实际张数**多于**引用数不拦（多带图不算错位）。
func assertPromptReferenceCoverage(prompt string, images []string) error {
	actual := len(images)
	expected, tags := promptReferenceExpectation(prompt)
	if expected > 0 {
		if actual < expected {
			return fmt.Errorf("prompt references %d reference image(s) (%s) but only %d were provided",
				expected, strings.Join(tags, ", "), actual)
		}
		return nil
	}
	if actual == 0 {
		if tokens := findPromptRefs(prompt, promptImageTokenRe); len(tokens) > 0 {
			return fmt.Errorf("prompt references reference image(s) (%s) but no reference image was provided",
				strings.Join(tokens, ", "))
		}
	}
	return nil
}

// resolveVideos 解析反解的视频地址：input_reference（首选）→ videos → video。
// 上限恰好 1 个（ArcReel 硬约束）。
func resolveVideos(req clientRequest) ([]string, error) {
	fields := []referenceImageField{
		{"input_reference", func(r clientRequest) json.RawMessage { return r.InputReference }},
		{"inputReference", func(r clientRequest) json.RawMessage { return r.InputReferenceCamelCase }},
		{"videos", func(r clientRequest) json.RawMessage { return r.Videos }},
		{"video", func(r clientRequest) json.RawMessage { return r.Video }},
	}
	for _, field := range fields {
		raw := field.Raw(req)
		if !hasValue(raw) {
			continue
		}
		videos, err := parseStringList(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field.Name, err)
		}
		if len(videos) == 0 {
			continue
		}
		if len(videos) > maxReferenceVideos {
			return nil, fmt.Errorf("video reverse accepts exactly %d reference video, got %d", maxReferenceVideos, len(videos))
		}
		for _, video := range videos {
			if err := assertHTTPURL(video, "reference video"); err != nil {
				return nil, err
			}
		}
		return videos, nil
	}
	return nil, fmt.Errorf("video reference is required: pass a public http(s) video URL via input_reference")
}

// resolveImageCount 解析出图张数：n（首选）→ count → 缺省 1，上限 10（与 ArcReel
// `_remote_image_count` 的 clamp 一致）。
func resolveImageCount(req clientRequest) (int, error) {
	count, found, err := parseFlexibleInt(req.N)
	if err != nil {
		return 0, fmt.Errorf("n %s: %w", string(req.N), err)
	}
	if !found {
		if count, found, err = parseFlexibleInt(req.Count); err != nil {
			return 0, fmt.Errorf("count %s: %w", string(req.Count), err)
		}
	}
	if !found {
		return defaultImageCount, nil
	}
	if count < 1 || count > maxImageCount {
		return 0, fmt.Errorf("n must be between 1 and %d, got %d", maxImageCount, count)
	}
	return count, nil
}

func assertHTTPURL(raw, label string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL, got %q", label, raw)
	}
	return nil
}

// parseFlexibleInt 兼容数字与数字字符串两种 JSON 形态。
// found=false 表示字段缺省或 null（视为未指定）；
// 字段存在但格式非法（非数字、浮点、非数字字符串）时返回错误。
func parseFlexibleInt(raw json.RawMessage) (value int, found bool, err error) {
	if !hasValue(raw) {
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
//
// 2026-10-22 丢图事故：旧实现两种形态都解析不了就**静默返回 nil**，
// 于是 ["https://a.png", 123] 这种混合数组会把整组图丢掉 —— 建单变成「无图」，
// 任务照发、额度照烧。现在解析失败一律报错（fail-loud），由调用方转成 400。
//
// 数组逐元素解析：单个元素坏掉时整组报错，而不是丢掉坏元素后用剩下的部分
// 继续（那样同样会造成「张数不对」的错位）。
func parseStringList(raw json.RawMessage) ([]string, error) {
	if !hasValue(raw) {
		return nil, nil
	}
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		var elements []json.RawMessage
		if err := common.Unmarshal(raw, &elements); err != nil {
			return nil, fmt.Errorf("must be a string or an array of strings")
		}
		values := make([]string, 0, len(elements))
		for index, element := range elements {
			value, err := parseStringElement(element)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", index, err)
			}
			if value != "" {
				values = append(values, value)
			}
		}
		return values, nil
	}
	var value string
	if err := common.Unmarshal(raw, &value); err == nil {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, nil
		}
		return []string{value}, nil
	}
	// 对象形态 {"url": "https://..."}：部分客户端用这种带一层包装的写法。
	var object struct {
		URL string `json:"url"`
	}
	if err := common.Unmarshal(raw, &object); err == nil {
		if url := strings.TrimSpace(object.URL); url != "" {
			return []string{url}, nil
		}
	}
	return nil, fmt.Errorf("must be a string or an array of strings")
}

// parseStringElement 解析数组里的单个元素：字符串，或带 url 字段的对象。
func parseStringElement(element json.RawMessage) (string, error) {
	var value string
	if err := common.Unmarshal(element, &value); err == nil {
		return strings.TrimSpace(value), nil
	}
	var object struct {
		URL string `json:"url"`
	}
	if err := common.Unmarshal(element, &object); err == nil {
		if url := strings.TrimSpace(object.URL); url != "" {
			return url, nil
		}
	}
	return "", fmt.Errorf("must be a string URL")
}

// hasValue 判断 JSON 字段是否「真的带了值」：缺省 / null / 空串 / 空数组都算未指定。
//
// 注意**不**把数字 0 当作未指定：`seconds: 0` / `n: 0` 是用户明确传了非法值，应该
// 走到白名单校验并 400，而不是静默回落到缺省值（计费时长/张数会与用户意图脱节）。
func hasValue(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", `""`, "[]":
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// ownedAssetPathPrefix 是 ArcReel 自有托管产物的稳定路径契约：Worker 上传到
// ArcReel 本站托管目录的产物（Gemini 图片 blob、Veo 的 data: mp4 转存）都挂在
// 这个前缀下。它不是公网地址，**必须带渠道密钥（service token）才能下载**。
//
// ⚠️ 不能只认相对路径：实测(2026-10-08, langdu 生产) ArcReel/Worker 某些路径
// 会上报**绝对** URL（同域拼接后的 https://arcreel.../api/v1/remote-generation/...）。
// 只判 http 前缀会把这种"伪公网直链"直接交付/透传给客户端，结果 401 ——
// 拿到链接的人没有渠道密钥，链接就是个打不开的摆设。
func IsOwnedAssetPath(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Path != "" {
		trimmed = parsed.Path // 绝对地址取路径部分, 相对路径原样
	}
	return strings.Contains(trimmed, "/api/v1/remote-generation/jobs/")
}

// directResultURL 只把「公网绝对地址」当作可直交付的结果。
//
// ArcReel 对 Worker 上传到本站托管目录的产物（Gemini 图片 blob、Veo 的 data: mp4）
// 存的是**相对路径** `/api/v1/remote-generation/jobs/{id}/outputs/{file}`——它不是
// 公网地址且需要渠道 token。透传给客户端等于交付一个打不开的链接，因此这里返回空串，
// 让 new-api 落成 `/v1/videos/{task_id}/content` 代理（task_polling.go 的既有分支），
// 由 VideoProxy 带渠道密钥回源。dola/tiktok 这类公网 CDN 地址照旧直传。
//
// ⚠️ 自有托管路径即使被拼成了**绝对** URL 也不能直传（见 IsOwnedAssetPath），
// 2026-10-08 实测：langdu 生产一条 gemini-web-video 任务拿到
// metadata.url=https://arcreel.../api/v1/remote-generation/...mp4，客户端直连 401、
// content 代理也按"公网直链"透传 401 → 整条链路表现为 502，成片拿不到。
func directResultURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") &&
		!strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return ""
	}
	if IsOwnedAssetPath(trimmed) {
		return ""
	}
	return trimmed
}

// inferMediaType 按结果地址后缀推断交付物类型，供客户端区分图片 / 视频 / 文本。
func inferMediaType(rawURL string) string {
	switch strings.ToLower(path.Ext(strings.SplitN(rawURL, "?", 2)[0])) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "image"
	case ".mp4", ".mov", ".webm", ".m4v":
		return "video"
	default:
		return "video"
	}
}
