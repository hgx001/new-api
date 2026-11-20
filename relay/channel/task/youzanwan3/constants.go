package youzanwan3

import (
	"strings"
	"time"

	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/QuantumNous/new-api/constant"
)

const ChannelName = "Youzan Wan3"

const (
	// 上游模型 ID，同时作为 new-api 的公开模型名（广场展示名与上游完全一致）。
	//
	// ⚠️ 2026-11-18 重要修正：wan-3.0 与 SJ-wan3-prime **不是** wan3.0-video /
	// wan3.0-video-prime 的改名，而是上游**新上架的两个独立模型**。
	// 上游文档（api.youzan666.vip/docs，2026-11-17 版）把它们与旧的两个并列列出，
	// 契约也完全不同（见下面各自注释）。旧的两个并未下架：
	//   - wan3.0-video 仍可提交（10–30 秒，720P/1080P，resolution+ratio 形态）；
	//   - wan3.0-video-prime 的上游通道**已禁用**（报 workflow_unavailable /
	//     lifecycle=disabled），提交必失败。
	//
	// 也就是说：把旧名映射到新上游 ID 是**错的**（会改变旧模型的时长与素材契约），
	// 正确做法是让新模型各自独立成一条路径，旧名保持原样。
	//
	// ⚠️ 这两个常量**不是**新模型的 ID —— 它们是「旧标准版 / 旧 prime」这条
	// 历史路径的名字，全站几十处分支都引用它们（isPrimeModel、默认值、倍率表、
	// 促销族映射……）。把它们的值换成新模型会让旧路径整体被新模型接管：
	// 促销族的默认档会从 wan3.0-video 变成 wan-3.0、prime 直连的「仅 30 秒」
	// 闸门会套到新 prime 上（它逐秒可选）。所以这里保持原值不动，
	// 新模型另立 wan30Model / sjWan3PrimeModel 两个常量。
	videoModel = "wan3.0-video"
	primeModel = "wan3.0-video-prime"

	// wan30Model 是有赞渠道 **wan-3.0-yz** 的**对外公开名**（老板 2026-11-18 定）。
	//
	// ⚠️ 公开名与上游 ID **故意不一样**：上游真实模型 ID 是 "wan-3.0"（见
	// wan30UpstreamModel），对外挂 "wan-3.0-yz" 是为了：
	//   1. 与同站的 wan3.0-video-官网 / wan-3.0-特惠 命名习惯一致（一律带渠道后缀），
	//      客户在广场上一眼能看出这是「有赞」这一路的货；
	//   2. 彻底避开 "wan-3.0" 这个裸名的历史包袱（它先后属于过 MegaAI 渠道
	//      （已停售，代码侧归档为 wan-3.0-megabyai-retired）和本站其它商品）。
	//
	// ⚠️ 改名只改**对外名**。上游 ID 不变（上游不提供 "wan-3.0-yz" 这个模型），
	// 所以下发时必须用 wan30UpstreamModel —— 见 upstreamModelFor。
	//
	// 上游契约（2026-11-18 实测 + 文档）：
	//   - 时长 **10–30 整数秒**。实测原文："Duration 4s is outside the allowed
	//     range 10-30 seconds for model wan-3.0"。文档写 4–30，但实测更严 ——
	//     按实测来（放行 5–9 秒就是「客户付了钱、建了单，上游才拒」）；
	//   - 参数形态是 **size**（1280x720 / 720x1280 / 1920x1080 / 1080x1920），
	//     **不接受** resolution+ratio 组合（实测回 "unsupported resolution or ratio"）；
	//   - 素材是**顶层 images / videos / audios 数组**（与 wan-3.0-pro 同契约）。
	//     文档原话：「wan-3.0、wan-3.0-pro、seedance-2.0 支持 images、videos、
	//     audios 数组，素材数量与组合以接口校验为准」；
	//   - 画幅仅 16:9 / 9:16。
	wan30Model = "wan-3.0-yz"

	// wan30UpstreamModel 是 wan-3.0-yz 对应的**上游真实模型 ID**。
	//
	// ⚠️ 它必须与 wan30Model 分开：对外名可以带渠道后缀、可以随时改，
	// 但上游 ID 是上游定的，改了就 404。上游请求体里 model 字段填的是它。
	//
	// ⚠️ 也**不能**把 wan30UpstreamModel 写进对外可见的字段（提交回执的
	// upstream_model_name 会带出它）—— 脱敏层只对别名模型遮模型名，
	// 而 wan-3.0-yz 不在别名名单里，所以它的名字会原样出现在对客响应里。
	// 这正是我们要的：公开名 = 上游名 = wan-3.0-yz，客户能对照文档核对。
	wan30UpstreamModel = "wan-3.0"

	// sjWan3PrimeModel 是上游 2026-11-18 新上架的独立模型 **SJ-wan3-prime**。
	//
	// 上游契约（2026-11-18 实测 + 文档）：
	//   - 时长 **2–30 整数秒**（上游原文 "duration must be an integer between 2
	//     and 30"）。对外按老板口径收口到 5–30 秒；
	//   - **仅 720P**。实测上游回 "Resolution 1080p is disabled for model
	//     SJ-wan3-prime"（480p 同理被禁）—— 文档写的「480P/720P/1080P」与
	//     实测不符，按实测来（放行 1080P 就是静默降档给客户）；
	//   - 参数形态是 **size**（不是 resolution+ratio）。实测 resolution=720P 单独
	//     下发回 "WAN_RATIO_INVALID"，而 size=720x1280 建单成功；
	//   - 画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4（实测 21:9 与 adaptive 不可用）。
	sjWan3PrimeModel = "SJ-wan3-prime"

	// mwSd25FlashModel 是上游在售的 **mw-sd2.5-flash**（妙物 Seedance 2.5 Flash）。
	//
	// 2026-11-20 老板口径：把有赞的这个模型接进平台，**促销族母本
	//（seedance2.5-特惠）的请求全部路由到它**，同时撤掉原来的
	// CapCut 出口 cp-seedance-2.5flash。活动版（seedance2.5-活动）下架。
	//
	// 上游契约（api.youzan666.vip/docs 2026-11-19 版 + /v1/models 实核）：
	//   - 时长 **4–30 整数秒**，缺省 5 秒（对外下界仍收口到 5 秒，与旧口径一致）；
	//   - 分辨率 **只有 480p / 720p**（上游 0.10 / 0.15 ⚡每秒）。
	//     对客特惠恒定交付 720P：480P 请求按最低交付档 720P 执行（升档不亏客户），
	//     1080P 显式请求 400（上游没有这一档）；
	//   - 画幅 16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9 / auto，缺省 16:9；
	//   - 素材：最多 10 图 / 5 视频 / 5 音频；首尾帧也支持
	//     （尾帧依赖首帧、首尾帧与普通参考素材互斥）；
	//   - 参数形态是 **duration（兼容 seconds）+ 小写 resolution + ratio + media**；
	//   - 计费：按（输出秒数 + 参考视频计时秒数）× 该分辨率每秒单价。
	//
	// ⚠️ 它与 SJ-wan3-prime 的边界完全不同：SJ 是 15–30 秒、仅 720P、且**不收
	// 参考视频/音频与首尾帧**。mw 把这几项全放开了，所以促销族的素材闸门
	// 也要跟着放开（见 validateMedia）—— 否则「模型支持、本地先拦」。
	mwSd25FlashModel = "mw-sd2.5-flash"

	// hkWan3Model 是上游 2026-11-19 在售的 **hk-wan3**（WAN3 官渠，满血满参，
	// 支持参考视频）。公开名与上游 ID 完全一致，不需要别名映射。
	//
	// 上游契约（api.youzan666.vip/docs 2026-11-19 版 + /api/pricing 核对）：
	//   - 时长 **4–30 整数秒**，缺省 5 秒；
	//   - 分辨率 480p / 720p / 1080p（上游成本 0.11 / 0.21 / 0.41 ⚡/秒）；
	//   - 画幅 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto，缺省 16:9；
	//   - 参数形态是 **resolution + ratio**（不是 size）：文档示例就是
	//     {"resolution":"720p","ratio":"16:9"}；
	//   - 素材可走 media（reference_image / reference_video / reference_audio），
	//     也可走顶层 images / videos / audios；上限 10 图 / 5 视频 / 5 音频、合计 20；
	//   - 首尾帧用 first_frame / last_frame（尾帧需要首帧，且与普通参考素材互斥）。
	//
	// 定价（老板 2026-11-19 口径）：**720P 钉死 ¥0.25/秒**；其余档在上游成本上
	// 加 ¥0.07 → 480P ¥0.18、1080P ¥0.48。上游 720P 成本是 0.21，所以 720P 的
	// 实际加价是 ¥0.04 —— 这是老板明确给的对外报价，不要按「+0.07」反推去改它。
	hkWan3Model = "hk-wan3"

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

// PublicModelName 把上游模型 ID 映射成**对外公开名**。
//
// 为什么需要它：调价同步是按上游目录里的 ID 算价的（上游只有 ID），
// 但写回 ModelPrice / 倍率表的键必须是**对外公开名** —— 计费链路
// （ModelPriceHelper / EstimateBilling）读的是客户请求里的那个名字。
// 两者不同的模型（wan-3.0-yz ↔ wan-3.0）不转换的话，价格就写到了
// 一个没人调用的键上，客户按 ¥0/秒 或旧价计费。
//
// 未登记的上游 ID 原样返回（绝大多数模型公开名 == 上游名）。
func PublicModelName(upstreamID string) string {
	if strings.TrimSpace(upstreamID) == wan30UpstreamModel {
		return wan30Model
	}
	return upstreamID
}

// PublicModelNames 是「上游 ID → 对外公开名」的映射表（当前只有一个非恒等项）。
//
// ⚠️ 新增「公开名 ≠ 上游名」的模型时，这里和 ModelList 必须一起改：
// ModelList 列上游真名（否则上游同步会把别名当真实模型写回渠道配置），
// 这里列对外名（否则价格写到错的键上）。
var PublicModelNames = map[string]string{
	wan30UpstreamModel: wan30Model,
}

// ModelList 与上游 /v1/models 的模型 ID 完全一致（在售的真实上游模型）。
//
// 2026-11-18 加 wan-3.0 / SJ-wan3-prime：上游新上架的**独立模型**，
// 与旧的 wan3.0-video / wan3.0-video-prime 并列存在（实测两者都在
// api.youzan666.vip/v1/models 里）。旧名也保留在清单里 —— 它们仍是可提交的
// 上游模型（只有 wan3.0-video-prime 的通道已禁用），且老客户在用。
// 别名（seedance2.5-特惠 / -活动）**不写进来** —— 这份清单必须与上游逐字
// 一致，否则「同步上游模型」会把别名当成真实模型写回渠道配置。
//
// ⚠️ wan-3.0-yz 的**上游 ID 是 wan-3.0**，所以这里列的是 wan30UpstreamModel
// 而不是对外名 wan30Model。写成对外名会让「同步上游模型」把 wan-3.0-yz
// 当成上游真实模型写进 channels.models —— 上游没有这个模型，直接 404。
// 对外名 → 上游名的映射由渠道的 model_mapping 承担。
var ModelList = []string{
	videoModel,
	primeModel,
	wan30ProModel,
	wan30UpstreamModel,
	sjWan3PrimeModel,
	hkWan3Model,
	// 2026-11-20：妙物 Seedance 2.5 Flash，促销族母本的常驻上游。
	mwSd25FlashModel,
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

// wan30ResolutionSizeRatio：上游 wan-3.0（对外名 wan-3.0-yz，2026-11-18 新上架）。
//
// 上游契约（实测 + 文档）：
//   - 时长 **10–30 整数秒**（上游原文 "Duration 4s is outside the allowed range
//     10-30 seconds for model wan-3.0"；5-9 秒一律拒单，这是硬边界）；
//   - 参数形态是 **size**（1280x720 / 720x1280 / 1920x1080 / 1080x1920），
//     **不接受** resolution+ratio 组合；
//   - 画幅仅 16:9 / 9:16（size 四个值正好覆盖这两个画幅的两档分辨率）。
//
// 定价（老板 2026-11-18 定）：**分档价** —— 720P ¥0.20/秒、1080P ¥0.25/秒。
// 基准档取 720P（ModelPrice 配 0.20/7.3 ≈ 0.0273973），1080P 倍率 = 0.25/0.20 = 1.25。
//
// ⚠️ 改价时 ModelPrice 与本表必须一起改，否则两档价会错位。
//
// ⚠️ 下界 10 秒是**上游硬边界**，不能跟别的模型统一到 5 秒：5-9 秒本地放行
// 就是「客户付了钱、建了单，上游才拒」。
var wan30ResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 1.25,
}

// wan30Ratios 是 wan-3.0 允许的画幅（size 的四个值正好覆盖这两档）。
var wan30Ratios = []string{"16:9", "9:16"}

// wan30DefaultRatio 是 wan-3.0 没传画幅时的缺省值（横屏，与 pro 一致）。
const wan30DefaultRatio = "16:9"

// wan30SizeByRatio 是「分辨率档 × 画幅」到上游 size 字面量的映射。
var wan30SizeByRatio = map[string]string{
	"720P/16:9":  "1280x720",
	"720P/9:16":  "720x1280",
	"1080P/16:9": "1920x1080",
	"1080P/9:16": "1080x1920",
}

// sjWan3PrimeResolutionSizeRatio：上游 SJ-wan3-prime（2026-11-18 新上架）。
//
// 上游契约（2026-11-18 实测优先于文档）：
//   - 时长 **2–30 整数秒**（上游原文 "duration must be an integer between 2
//     and 30"）。对外按老板口径收口到 5–30 秒；
//   - **仅 720P 可用**。实测上游回 "Resolution 1080p is disabled for model
//     SJ-wan3-prime"（480p 同样被禁）。⚠️ 文档参数表写的是「480P / 720P /
//     1080P」—— 与实测不符，按实测走：放开 1080P 就是「客户付了 1080P 的钱
//     静默拿 720P 的片」；
//   - 参数形态是 **size**。实测 resolution=720P 单独下发回 "WAN_RATIO_INVALID"
//     （缺 ratio），size=720x1280 建单成功；
//   - 画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4（实测 21:9 与 adaptive 不可用）；
//   - 独立模型；成片转存 COS 后返回签名链接。
//
// ⚠️ 与旧 prime（wan3.0-video-prime）完全不同：旧 prime 仅 30 秒、仅 720P；
//
//	新 prime 逐秒可选。两者是**两个商品**，不要互相别名化。
//
// 定价沿用旧 prime 同价（老板 2026-11-18 口径）：¥0.25/秒，基准档 720P。
// 上游 1080P 被禁 → 只有一档价 → 倍率恒为 1。
var sjWan3PrimeResolutionSizeRatio = map[string]float64{
	"720P": 1.0,
}

// sjWan3PrimeRatios 是 SJ-wan3-prime 支持的画幅白名单。
// 实测 21:9 被上游 plugin 拒（"hook buildSubmitRequest failed"），
// adaptive 被 sigrun-video 拒（WAN_RATIO_INVALID）。
var sjWan3PrimeRatios = []string{"16:9", "9:16", "1:1", "4:3", "3:4"}

// sjWan3PrimeDefaultRatio 是 SJ-wan3-prime 没传画幅时的缺省值（横屏）。
const sjWan3PrimeDefaultRatio = "16:9"

// sjWan3PrimeSizeByRatio 是「画幅 → 上游 size 字面量」的映射。
//
// SJ-wan3-prime 只提供 720P 一档，实测接受的 size 只有横竖两个方幅尺寸；
// 方幅（1:1 / 4:3 / 3:4）上游标称支持，但具体 size 字面量未在文档给出 ——
// 方幅走 720x720 这类正方形尺寸，缺省映射到 720x720（正方形即 1:1）。
var sjWan3PrimeSizeByRatio = map[string]string{
	"16:9": "1280x720",
	"9:16": "720x1280",
	"1:1":  "720x720",
	"4:3":  "960x720",
	"3:4":  "720x960",
}

// hkWan3Ratios 是 hk-wan3 允许的成片画幅。
// 上游用 "auto"（不是其它模型的 "adaptive"）；adaptive 不在白名单里，
// 由 resolveRatio 回退到缺省 16:9，老客户沿用 adaptive 写法不会 400。
var hkWan3Ratios = []string{"16:9", "9:16", "1:1", "4:3", "3:4", "auto"}

// hkWan3DefaultRatio 是 hk-wan3 没传画幅时的缺省值（与上游文档一致）。
const hkWan3DefaultRatio = "16:9"

// mwSd25FlashRatios 是 mw-sd2.5-flash 支持的成片画幅。
//
// 上游文档（2026-11-19 版）：16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9 / auto。
// 与 hk-wan3 同形态 —— 它认的也是 "auto"（不是其它模型的历史写法 "adaptive"）。
var mwSd25FlashRatios = []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", "auto"}

// mwSd25FlashDefaultRatio 是 mw-sd2.5-flash 没传画幅时的缺省值。
//
// 取 16:9：与文档示例一致，也与促销族此前的缺省口径一致。
const mwSd25FlashDefaultRatio = "16:9"

// hkWan3ResolutionSizeRatio：hk-wan3 按秒分档计价。
//
// 对外售价（老板 2026-11-19 定）：480P ¥0.18、720P ¥0.25、1080P ¥0.48 ——
// 720P 是钉死的对外价（上游成本 0.21，加价 ¥0.04），其余两档按「上游 + ¥0.07」。
//
// 基准档取**最低价档 480P**（与 ModelPrice「基准档配最低价」的口径一致），
// ModelPrice 配 0.18/7.3 ≈ 0.0246575，720P / 1080P 用倍率表达。
//
// ⚠️ 改价时 ModelPrice 与本表必须一起改，否则档位价会错位。
// ⚠️ 这里只是**动态倍率读不到时的兜底**；生产实际走 YouzanWan3Pricing 的 ratios。
var hkWan3ResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  0.25 / 0.18,
	"1080P": 0.48 / 0.18,
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

	// promoResidentModel 是**促销族母本**（seedance2.5-特惠）的常驻上游模型。
	//
	// 2026-11-20 老板口径：换成有赞的 **mw-sd2.5-flash**（原 SJ-wan3-prime 退出）。
	// 上游契约见 mwSd25FlashModel 的注释：4–30 秒、480p/720p、支持音视频与首尾帧。
	//
	// 与旧常驻模型的差别：SJ 只接 15–30 秒、仅 720P、且**拒收音视频与首尾帧**，
	// 所以历史上 5–14 秒要靠路由层改派豆包 Fast、1080P 靠改派、音视频靠规则九
	// 改派 CapCut。mw 把这些能力一次补齐 —— 这些改派**全部不再需要**，
	// 促销族的请求应在适配器内直达本模型（路由层相应开关由运营关掉）。
	promoResidentModel = mwSd25FlashModel

	// promoMinDuration 是**促销族母本**（seedance2.5-特惠）的时长下界（秒）。
	//
	// 2026-11-20 起上游换成 mw-sd2.5-flash（4–30 秒），对外仍按 **5 秒**收口 ——
	// 与改版前的对外承诺逐字节一致，老客户不会因为换上游而被改掉可用区间。
	promoMinDuration = 5

	// perCallPromoMinDuration 是**活动版**（按次档 seedance2.5-活动）的时长下界。
	//
	// 2026-11-18 调整常驻模型时只动母本特惠，活动版保持 4 秒：
	// 它按条卖 ¥4.5、时长 4-30 秒，短时长路由（short_rule_enabled）本就关闭，
	// 4-14 秒仍由原标准版执行。跟着特惠改成 5 秒会把能卖的单挡掉。
	perCallPromoMinDuration = 4

	// promoShortRouteMaxSeconds 是促销族「短请求改派」的上界（秒，**含**）。
	//
	// 2026-11-18 老板口径：15 秒以下 → seedance-2-0-fast，15 秒及以上 → SJ-wan3-prime。
	// 因此 ≤14 秒必须由路由层改派，SJ 只接 15–30 秒。
	//
	// ⚠️ 这个值必须与 seedanceroute_setting 的短时长阈值保持一致：
	//   路由层用 short_trigger_seconds 判定，这里用于本地兜底校验；
	//   两处分叉会出现「路由层认为该改派、适配器认为该拒」的错位。
	promoShortRouteMaxSeconds = 14

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

	// wan30MinDuration / wan30DefaultDuration 是 wan-3.0 的时长口径。
	//
	// 上游硬边界 10 秒（实测：4-9 秒一律回 duration_out_of_range，10 秒通过）。
	// 与 SJ-wan3-prime 的 2 秒、wan-3.0-pro 的 4 秒不同 —— 这是上游商品本身的
	// 差异，统一到 5 秒只对后两者成立，对 wan-3.0 只会造出「建单后才失败」。
	wan30MinDuration     = 10
	wan30DefaultDuration = 10

	// hkWan3MinDuration / hkWan3DefaultDuration 是 hk-wan3 的时长口径。
	//
	// 上游文档（2026-11-19 版）写 4–30 整数秒，缺省 5 秒；与 wan-3.0 的 10 秒
	// 下界不同 —— 这是上游商品本身的差异，不能统一按 10 秒拦（会把能跑的单挡掉），
	// 也不能按 5 秒夹紧（客户要 4 秒拿到 5 秒，按秒计费下等于多收一秒的钱）。
	hkWan3MinDuration     = 4
	hkWan3DefaultDuration = 5

	// sjWan3PrimeMinDuration / sjWan3PrimeDefaultDuration 是 SJ-wan3-prime 的
	// 时长口径：上游 2-30 秒。老板 2026-11-18 口径「最小时长统一 ≥5 秒」，
	// 这里取 5 秒作为对外下界（上游允许 2-4 秒，但对外统一收口到 5 秒，
	// 与 wan-3.0-pro 一致）。
	sjWan3PrimeMinDuration     = 5
	sjWan3PrimeDefaultDuration = 5

	// mwSd25FlashMinDuration / mwSd25FlashDefaultDuration 是 mw-sd2.5-flash
	// 的时长口径：上游 4–30 秒、缺省 5 秒。
	//
	// ⚠️ 上游 4 秒虽可跑，但**对外仍从 5 秒起卖**（promoMinDuration）——
	// 换上游不是改对外承诺的理由；放宽到 4 秒是独立的商业决定。
	mwSd25FlashMinDuration     = 4
	mwSd25FlashDefaultDuration = 5
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
