package youzanwan3

const ChannelName = "Youzan Wan3"

const (
	// 上游模型 ID，同时作为 new-api 的公开模型名（广场展示名与上游完全一致）。
	videoModel = "wan3.0-video"
	primeModel = "wan3.0-video-prime"

	// tehuiModel 是对外别名：客户看到「Seedance 2.5 特惠版」，实际由上游
	// wan3.0-video-prime 执行（同一份算力，按秒分档计价）。
	// 该名字 2026-10-26 随 MegaAI 渠道停售而空置，2026-10-29 改为路由到有赞 prime。
	// 别名不写进 ModelList —— 那份清单必须与上游 /v1/models 逐字一致，
	// 否则「同步上游模型」会把别名当成真实上游模型写回渠道配置。
	tehuiModel = "seedance2.5-特惠"

	// smartLegacyModel / legacyPrimeModel 是 2026-10-28 前的公开名，语义等价：
	// wan3.0-smart → wan3.0-video、wan3.0-video-prime-1080p → wan3.0-video-prime。
	// 保留仅为让老客户的既定请求继续可路由（广场已不再展示）。
	smartLegacyModel = "wan3.0-smart"
	legacyPrimeModel = "wan3.0-video-prime-1080p"

	// r2vModel 已从上游 /v1/models 目录消失，调用会被 upstreamModelFor 明确拒绝；
	// 该名字仅用于给出可读错误，并兼容仍在轮询中的历史任务。
	r2vModel = "wan2.7-r2v"
)

// ModelList 与上游 /v1/models 的模型 ID 完全一致（2026-10-28 起上游只提供这两个）。
var ModelList = []string{
	videoModel,
	primeModel,
}

// videoResolutionSizeRatio：上游 wan3.0-video 三档同价（0.18 ⚡/秒 = ¥0.18/秒），
// 售价统一 ¥0.23/秒（上游 + ¥0.05），故三档倍率均为 1。
// ModelPrice 应配置为 ¥0.23 对应的 USD（0.23/7.3 ≈ 0.0315068）。
var videoResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  1.0,
	"1080P": 1.0,
}

// primeResolutionSizeRatio：上游 wan3.0-video-prime 按分辨率分档计价
// （720P 0.15 ⚡/秒、1080P 0.20 ⚡/秒）。售价 = 上游 + 每档固定加价：
// 720P ¥0.25/秒（加价 ¥0.10）、1080P ¥0.30/秒（加价 ¥0.10）。
// ModelPrice 按 720P 基准配置（0.25/7.3 ≈ 0.0342466），1080P 倍率 = 0.30 / 0.25 = 1.2。
//
// 2026-10-29 第二轮调整：720P 由 ¥0.20 提到 ¥0.25（老板要求），同时把 1080P 的
// 加价从 ¥0.10 保持不动 —— 结果是两档只差 ¥0.05、倍率从 1.5 收窄到 1.2。
// 这个常量只是**动态倍率读不到时的兜底**，生产实际走 YouzanWan3Pricing 的 ratios。
var primeResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 1.2,
}

// tehuiResolutionSizeRatio：对外别名 seedance2.5-特惠 **独立定价**，不跟随 prime。
//
// 2026-10-29 老板明确：这个模型保持它自己的价（720P ¥0.20/秒、1080P ¥0.30/秒），
// prime 调价它不动。所以它有自己的一张倍率表，ModelPrice 也是单独配的
// （0.20/7.3），并且**不在 youzanAliasTargets 里** —— 同步任务只改上游那两个
// 真实模型的价，不会碰它。
//
// 720P 是基准档（ModelPrice 按它配），1080P 倍率 = 0.30 / 0.20 = 1.5。
var tehuiResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 1.5,
}

// r2vResolutionSizeRatio：wan2.7-r2v 已下架，仅为轮询中的历史任务保留倍率表。
// 上游只认小写分辨率，键名与适配器输出保持一致。
var r2vResolutionSizeRatio = map[string]float64{
	"720p":  1.0,
	"1080p": 1.0,
}

// smartRatios 是上游 /v1/tasks 允许的成片比例（2026-10-28 新增 21:9 宽银幕），
// 两个在售模型共用同一套比例。
var smartRatios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}

// r2vRatios：wan2.7-r2v 仅支持固定比例，不接受 adaptive（仅历史任务轮询用）。
var r2vRatios = []string{"16:9", "9:16", "1:1"}

const (
	// reasonContentModeration 是无理由失败的统一口径：任务已运行一段时间
	// 才失败、且上游未返回任何原因时，归因为内容审核不通过。
	reasonContentModeration = "内容审核不通过"

	// videoModel 三档（480P/720P/1080P）同价，缺省取最低档 480P。
	defaultResolution = "480P"
	// primeDefaultResolution：上游 prime 只提供 720P/1080P，缺省取最低档 720P
	// （不静默升到更贵的 1080P）。
	primeDefaultResolution = "720P"

	defaultRatio = "adaptive"

	// wan2.7-r2v 的历史限制（时长仅 5s / 10s 两档，比例固定）。
	// 上游已下架，这些常量仅用于兼容仍在轮询中的历史任务。
	r2vMinDuration  = 5
	r2vMaxDuration  = 10
	r2vDefaultRatio = "16:9"

	// 两个在售模型均为按秒计费，时长 2—30 秒逐秒可选。
	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	// 2026-10-28 上游 /v1/tasks 的 media 上限：图片 10 + 视频 5 + 音频 5，
	// 三类合计不得超过 20 项。两个模型同一口径（旧版 prime 的 8 张限制已取消）。
	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5
	maxTotalMedia      = 20

	// maxReferenceVideoSeconds 是单条参考视频的时长上限（秒）。
	//
	// 2026-10-31 12:24 起上游开始明确拒绝超长参考素材，原文：
	//	参考素材时长过长：当前为 31.609 秒，最长允许 15 秒
	//
	// 在此之前本地**只校验 >0 秒**（见 video_duration.go），所以 31.6 秒的素材
	// 能一路走完「建单 → 预扣 ¥12.40 → 上游拒绝 → 退款」。客户在 12:24~13:07
	// 重试了 14 次，每次都白占一次余额、还要等 1~3 分钟退款。
	//
	// 为什么现在才拦：上游的 15 秒限制一直存在（与 wan3 同源同模型），
	// 只是此前没有客户传过超长素材，本地校验的空档没被触发。
	//
	// ⚠️ 只拦**单条**，不拦总量：上游文案明确说的是「当前为 31.609 秒」这种
	// 单条口径，没有任何证据表明它限制多条合计。多拦会误伤「两条各 10 秒」
	// 这类本来能跑的请求。等出现总量超限的真实错误再加。
	maxReferenceVideoSeconds = 15

	// maxTotalReferenceVideoSeconds 是**多条**参考视频时长合计的上限（秒）。
	//
	// 与 wan3.0-video-官网同源同模型，官方《万相3.0-视频生成API参考》写明
	// 「参考视频最多 5 段、总时长不大于 15 秒」。有赞 prime 是同一套万相 3.0
	// 后端，网关限制一致（上游单条错误文案里的「最长允许 15 秒」也印证了这点）。
	//
	// 为什么加：本地只拦单条的话，「5 条各 14 秒」= 70 秒能一路走完
	// 「建单 → 预扣 → 上游拒绝 → 退款」，与单条超限是同一类浪费。
	// 合计超 15 秒必然超单条限制的量级，属于确定的违规。
	maxTotalReferenceVideoSeconds = 15

	// maxGenerationSecondsWithVideo 是「有参考视频时」生成时长的上限（秒）。
	//
	// 上游的官方约束是「输入视频总时长 + 输出视频时长不超过 30 秒」，而参考视频
	// 单条就上限 15 秒 —— 所以「15 秒参考 + 15 秒生成」正好卡在 30 秒边界。
	// 老板 2026-10-31 定：「有参考视频的时候，生成的视频也要小于 15 秒」。
	//
	// 为什么用 15 而不是 30：与参考视频上限同值，口径统一；且真实失败样本
	// （tasks id=967~982）全是 30 秒生成 + 31.6 秒参考，按同值拦截最早、最省。
	// 边界取**闭区间**：15 秒生成是允许的（15+15=30 正好不超上游上限）。
	maxGenerationSecondsWithVideo = 15
)

// 导出的限制值，供条件路由中间件（middleware/seedance_route.go）在**改派前**
// 用同一套口径预检。
//
// 为什么需要导出：源链路（youzanwan3）与改派目标链路（wan3.0-720p-特惠 → wan3 包）
// 会被同一个「seedance2.5-特惠」请求命中。如果两条链路的限制不一致，同一份素材
// 会出现「16:9 被拒、9:16 能跑」或反过来的「时好时坏」现象 —— 客户看到的是
// 同一个模型同样的参数，时不时就失败一次。
//
// 口径统一为：单条参考视频 ≤15 秒、合计 ≤15 秒、生成时长（有参考视频时）≤15 秒。
const (
	// MaxReferenceVideoSeconds 是单条参考视频的时长上限（秒）。
	MaxReferenceVideoSeconds = maxReferenceVideoSeconds
	// MaxTotalReferenceVideoSeconds 是多条参考视频时长合计的上限（秒）。
	MaxTotalReferenceVideoSeconds = maxTotalReferenceVideoSeconds
	// MaxGenerationSecondsWithVideo 是有参考视频时生成时长的上限（秒）。
	MaxGenerationSecondsWithVideo = maxGenerationSecondsWithVideo
)
