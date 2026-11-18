package youzanwan3

import (
	"time"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/QuantumNous/new-api/constant"
)

const ChannelName = "Youzan Wan3"

const (
	// 上游模型 ID，同时作为 new-api 的公开模型名（广场展示名与上游完全一致）。
	videoModel = "wan3.0-video"
	primeModel = "wan3.0-video-prime"

	// wan30ProModel 是有赞 2026-11-17 文档里的 wan-3.0-pro（与 wan-3.0 并列的
	// 独立上游模型，不是 wan3.0-video 的别名）。
	//
	// 契约（照 upstream 文档，2026-11-18 接入）：
	//   - 时长 4-30 整数秒（文档：4–30）；
	//   - 分辨率 720P / 1080P，尺寸只有 1280x720 / 720x1280 / 1920x1080 / 1080x1920；
	//   - 画幅仅 16:9 / 9:16；
	//   - 素材走 images / videos / audios 数组（**不是** media），数量与组合以
	//     上游接口校验为准；
	//   - 计费按条：720P ¥5.0/条、1080P ¥7.0/条，与时长无关。
	//
	// ⚠️ 为什么不复用 wan3.0-video：两者是上游的两个商品（不同模型 ID、不同
	// 时长下界、不同素材契约）。把它映射到 wan3.0-video 会同时踩两边的边界
	// （10 秒下界 vs 4 秒、仅参考图 vs 支持音视频）。
	wan30ProModel = "wan-3.0-pro"

	// tehuiModel 是对外别名：客户看到「Seedance 2.5 特惠版」，实际由上游
	// wan3.0-video-prime 执行（同一份算力，按秒分档计价）。
	// 该名字 2026-10-26 随 MegaAI 渠道停售而空置，2026-10-29 改为路由到有赞 prime。
	// 别名不写进 ModelList —— 那份清单必须与上游 /v1/models 逐字一致，
	// 否则「同步上游模型」会把别名当成真实上游模型写回渠道配置。
	//
	// ⚠️ 它是 constant.Seedance25TeHuiModel 的镜像。**真正的能力判定一律走
	// constant 的能力组**（constant.IsSeedance25Family 等），不要在这里新增
	// 族成员的判断分支 —— 那正是「母本加了能力、活动版没加」的根源。
	tehuiModel = constant.Seedance25TeHuiModel

	// smartLegacyModel / legacyPrimeModel 是 2026-10-28 前的公开名，语义等价：
	// wan3.0-smart → wan3.0-video、wan3.0-video-prime-1080p → wan3.0-video-prime。
	// 保留仅为让老客户的既定请求继续可路由（广场已不再展示）。
	smartLegacyModel = "wan3.0-smart"
	legacyPrimeModel = "wan3.0-video-prime-1080p"

	// r2vModel 已从上游 /v1/models 目录消失，调用会被 upstreamModelFor 明确拒绝；
	// 该名字仅用于给出可读错误，并兼容仍在轮询中的历史任务。
	r2vModel = "wan2.7-r2v"
)

// ModelList 与上游 /v1/models 的模型 ID 完全一致（在售的真实上游模型）。
//
// 2026-11-18 加 wan-3.0-pro：上游文档 2026-11-17 版把它列为独立模型。
// 别名（seedance2.5-特惠 / -活动）**不写进来** —— 这份清单必须与上游逐字
// 一致，否则「同步上游模型」会把别名当成真实模型写回渠道配置。
var ModelList = []string{
	videoModel,
	primeModel,
	wan30ProModel,
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

// wan30ProResolutionSizeRatio：wan-3.0-pro **按条**计费（与时长无关），
// 对外 720P ¥5.0/条、1080P ¥7.0/条（2026-11-18 老板调价；原 ¥4.5 / ¥5.5）。
//
// ModelPrice 按 720P 基准配置（5/7.3 ≈ 0.6849315），1080P 倍率 = 7 / 5。
// EstimateBilling 只返回 size 倍率、**不带 seconds** —— 带秒数会把 ¥5 乘成
// 每多一秒多收一份钱，正是「按条」与「按秒」的分水岭。
//
// ⚠️ 与 seedance2.5-活动 价格不同（活动 ¥4.5/条）：两者**不是同一个商品**，
// 活动版锁死 720P，本模型 1080P 是另一档价；不要把两者互相别名化。
var wan30ProResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 7.0 / 5.0,
}

// wan30ProRatios 是上游 wan-3.0-pro 允许的成片画幅（文档原话「画幅仅 16:9 / 9:16」）。
// 与其它模型不同：**不支持** adaptive / 1:1 / 4:3 / 3:4 / 21:9。
var wan30ProRatios = []string{"16:9", "9:16"}

// wan30ProDefaultRatio 是客户没传画幅时的缺省值。
// 取 16:9（横屏是文档示例与主流用法；不能沿用其它模型的 adaptive ——
// 上游这个模型不认 adaptive）。
const wan30ProDefaultRatio = "16:9"

// wan30ProSizeByRatio 是「分辨率档 × 画幅」到上游 size 字面量的映射。
//
// wan-3.0-pro 的上游契约推荐用 size（而不是 resolution/ratio 分开传）：
// 文档示例就是 {"size":"1280x720"}，可取值只有这四个。
var wan30ProSizeByRatio = map[string]string{
	"720P/16:9":  "1280x720",
	"720P/9:16":  "720x1280",
	"1080P/16:9": "1920x1080",
	"1080P/9:16": "1080x1920",
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

// smartRatios 是上游允许的成片比例（2026-10-28 新增 21:9 宽银幕），
// 两个在售模型共用同一套比例。
var smartRatios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}

// r2vRatios：wan2.7-r2v 仅支持固定比例，不接受 adaptive（仅历史任务轮询用）。
var r2vRatios = []string{"16:9", "9:16", "1:1"}

const (
	// reasonContentModeration 是无理由失败的统一口径：任务已运行一段时间
	// 才失败、且上游未返回任何原因时，归因为内容审核不通过。
	reasonContentModeration = "内容审核不通过"

	// 2026-11-15 上游切换后标准版只有 720P/1080P 两档（480P 下架），
	// 缺省取最低档 720P（与 prime 同口径）。
	defaultResolution = "720P"
	// primeDefaultResolution：上游 prime 只提供 720P/1080P，缺省取最低档 720P
	// （不静默升到更贵的 1080P）。2026-11-15 起 prime 仅 720P，见
	// normalizeResolutionForModel / validateExplicitDurationForModel 的直连分支。
	primeDefaultResolution = "720P"

	defaultRatio = "adaptive"

	// wan2.7-r2v 的历史限制（时长仅 5s / 10s 两档，比例固定）。
	// 上游已下架，这些常量仅用于兼容仍在轮询中的历史任务。
	r2vMinDuration  = 5
	r2vMaxDuration  = 10
	r2vDefaultRatio = "16:9"

	// 2026-11-15 上游切换后的时长口径（见 api.youzan666.vip/docs）：
	//   - 标准版 wan3.0-video：10—30 秒逐秒可选（480P 同步下架，仅 720P/1080P）；
	//   - prime：仅 30 秒、仅 720P。
	//
	// ⚠️ minDuration / maxDuration 是**有赞标准版直连**（wan3.0-video）的硬边界：
	//   上游 10 秒起才收，<10 秒一律拒单。直连客户（自己买 wan3.0-video 的）
	//   必须按这个边界 fail-loud —— 夹紧到 10 秒等于「客户要 5 秒拿到 10 秒」。
	minDuration     = 10
	maxDuration     = 30
	defaultDuration = 10

	// promoMinDuration 是**促销族**（seedance2.5-特惠 / -活动）的时长下界（秒）。
	//
	// 2026-11-17 老板口径：促销族对外支持 **4-30 秒**，低于 4 秒 fail-loud 报错。
	//
	// 为什么促销族能低于标准版的 10 秒：促销族 <10 秒的请求**不会**走到有赞 ——
	// 路由层（middleware/seedance_route.go 规则六第三分支）会把它们改派到
	// CapCut 满血档 cp-seedance-2.0（区间 4–15 秒），正好覆盖 4–9 秒。
	// 有赞标准版只接 **≥10 秒**。
	//
	// ⚠️ 所以这里的 4 秒**不是**「有赞能跑 4 秒」—— 4 秒请求必须先被路由层
	//   改派走 CapCut；任何漏过路由层、直接落到有赞标准版的 4-9 秒请求，
	//   都会被上游拒（fail-loud，不夹紧）。
	//   路由层那道门槛就是 seedanceroute_setting.ShortCapcutMinSeconds。
	promoMinDuration = 4

	// primeFixedDuration 是 2026-11-15 上游切换后 prime 唯一接受的时长。
	// 旧协议的「2-30 逐秒」已失效：非 30 秒的 prime 请求上游一律拒单。
	primeFixedDuration = 30

	// tehuiPrimeSeconds 是**特惠母本**改走 prime 执行的时长（秒）。
	//
	// 2026-11-16 老板口径：「特惠 30 秒的请求要走有赞的 prime 模型（画质增强）」。
	// 背景：上游 prime（wan3.0-video-prime）是**独立上游**，画质更好、上游单价更低
	// （720P ¥0.15/秒 vs 标准版 ¥0.18/秒），但它**仅支持 30 秒、仅 720P**。
	//
	// 所以「特惠 30 秒走 prime」是**逐秒判断**而不是模型级映射：
	// 只有恰好 30 秒才落 prime，其余秒数仍走标准版 wan3.0-video。
	tehuiPrimeSeconds = 30

	// failureRecheckDelay 是上游报 failed 后、我方退款前的**复核窗口**。
	//
	// 2026-11-16 生产实锤：上游余额耗尽时把任务标成 failed 并非真终态 ——
	// 充值后会把**同一个任务**复活继续执行并计费（当时 HTTP 402 挂起的请求
	// 在充值后被执行计费）。轮询层若立即退款，就会形成「上游扣了成本、
	// 我方没收客户钱」的纯亏损。
	//
	// 10 分钟的取值：上游充值后复活任务在几分钟内开始跑；轮询间隔约 60 秒，
	// 窗口内会问上游约 10 次，足以观察到复活；再长会明显拖慢客户退款时效。
	// 详见 service.TaskFailureRechecker 的语义约定。
	failureRecheckDelay = 10 * time.Minute

	// 上游 media 上限（2026-10-28 口径）：图片 10 + 视频 5 + 音频 5，
	// 三类合计不得超过 20 项。两个模型同一口径（旧版 prime 的 8 张限制已取消）。
	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5
	maxTotalMedia      = 20

	// minReferenceImageShortSide 是参考图短边下限（px）。
	//
	// 2026-11-11 真机实测（同一张图只改尺寸，2 秒任务）：
	//	204×360 / 230×406 → MEDIA_IMAGE_INVALID（上游文案却写「图片无法读取或格式不支持」）
	//	240×424 / 255×450 / 306×540 / 408×720 → 成功出片
	// 上游真实下限约在短边 230~240，但规则不透明、报错还会误导成格式问题；
	// 且这种尺寸放大到 720P 交付本身就是糊的，故本地直接按交付档 720 拦截。
	minReferenceImageShortSide = 720

	// referenceImageMaxSide 是参考图长边上限（px）：判断「长宽比过窄」时按它折算，
	// 1920 是 1080P 交付档的 1.78 倍，缩放后画质余量足够。
	referenceImageMaxSide = 1920

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

	// maxTotalReferenceAudioSeconds 是**多条**参考音频时长合计的上限（秒）。
	//
	// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
	// 单个 [1, 15] 秒，**总时长不大于 15 秒**。与参考视频是同一条硬约束。
	//
	// 真实失败样本（tasks id=1258~1262，2026-11-02 19:35~19:48）：
	// 4 条音频实测 8.10/3.11/5.00/2.98 秒，**单条都在 15 秒内**，
	// 累计 19.20 秒，上游 MEDIA_DURATION_INVALID 拒单。
	// 客户连续重试 5 次，每次白占一次余额、等 1~3 分钟退款，
	// 而上游那句「参考素材时长不符合要求」完全没说是哪条素材、多长、多久。
	//
	// ⚠️ 单条上限仍不拦：官方写的是「单个 [1, 15] 秒」，但实测上游只对
	// **合计**超限报错，单条超长是否也拒没有样本，不确定就不拦
	// （多拦会误伤本来能跑的请求）。
	maxTotalReferenceAudioSeconds = taskcommon.MaxReferenceAudioSeconds

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

	// perCallFixedDuration 是「按次档」的**默认时长**（秒）。
	//
	// 2026-11-02 上架 seedance2.5-活动 时定：¥4.5/次、固定 30 秒。
	// **2026-11-11 起时长放开为 2-30 秒逐秒可选**（售价仍恒 ¥4.5/条），
	// 这个常量只表达「客户没传时按 30 秒跑」，不再限制合法取值范围 ——
	// 越界（<2 / >30）由 validatePerCallFixedTier 400 拒掉。
	//
	// ⚠️ 毛利提示：30 秒是本档的成本上限（30 × ¥0.15 = ¥4.50 正好贴售价），
	// 客户传满 30 秒毛利为 0；传 10 秒成本 ¥1.50、毛利 67%。
	//
	// ⚠️ 这里是 constant.Seedance25PerCallFixedSeconds 的**派生**（不是副本）。
	// 源链路的默认值与条件路由层的归一必须用**同一个** 30 —— 两者不一致时，
	// 「客户不传时长」会被跑成两个不同长度，而且不报错。
	perCallFixedDuration = constant.Seedance25PerCallFixedSeconds

	// perCallFixedResolution 是「按次档」的固定分辨率档。
	//
	// 按次只有一档价（¥4.5/次，与分辨率无关），所以锁死 720P ——
	// 上游 prime 提供 720P/1080P，1080P 的成本是 720P 的 1.33 倍，
	// 卖同一个价就会在 1080P 上倒挂。锁 720P 才能保证「一口价 = 一个成本」。
	//
	// ⚠️ 同样是 constant.Seedance25PerCallFixedResolution 的派生。
	perCallFixedResolution = constant.Seedance25PerCallFixedResolution

	// wan30ProMinDuration / wan30ProDefaultDuration 是 wan-3.0-pro 的时长口径。
	//
	// 上游文档（2026-11-17 版）原话「4–30」，比标准版 wan3.0-video 的 10-30
	// 更宽 —— 这是**上游模型自身**的能力，不走改派，直接透传即可。
	// 没传时长按 5 秒（文档示例值，也是最短档附近，客户最容易核对的取值）。
	//
	// ⚠️ 越界一律 400 fail-loud（validateExplicitDurationForModel），不做夹紧：
	// 按条计费下把 60 秒夹成 30 秒，客户付了同价却少一半片长。
	wan30ProMinDuration     = 4
	wan30ProDefaultDuration = 5
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
