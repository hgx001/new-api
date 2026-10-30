package wan3

import "strings"

// ChannelName 后台展示名
const ChannelName = "Wan3"

// ModelList 对外暴露的官方模型。
// 分辨率通过请求参数控制（与 DashScope API 对齐）。
// - wan3.0-video：标准版
// - resolution / metadata.resolution："480P" | "720P" | "1080P"
// 高速版 wan3.0-video-prime 可通过频道模型映射接入，不改变现有默认模型列表。
var ModelList = []string{
	"wan3.0-video",
}

// OfficialModelName 官网渠道的独立对外模型名（上游仍是标准版 wan3.0-video）。
const OfficialModelName = "wan3.0-video-官网"

// Official720PModelName 官网渠道的「720P 成片」对外模型名。
//
// 2026-10-30 改名：「官网」→「特惠」。
//	改名理由与 seedance2.5-特惠 / wan-3.0-特惠 同一套后缀风格 ——
//	「特惠」直白表达"比官网原生 720P 便宜"，比「官网」更能让客户一眼看懂档位关系。
//	旧名 Official720PModelNameLegacy 保留为**可调用别名**（见下），但不进模型广场。
//
// 与 OfficialModelName 的区别：
//
//	OfficialModelName     上游直接生成 720P/1080P，按分辨率档位定价。
//	Official720PModelName 上游只生成 480P，成片再交给云端做 2 倍提升，
//	                       最终交付 720P。价格是「生成 + 增强」的复合价。
//
// ⚠️ 对外文案**不提**「超分 / 增强 / 腾讯云」等内部实现词 ——
//	用户只需要知道这个模型出 720P。
const Official720PModelName = "wan3.0-720p-特惠"

// Official720PModelNameLegacy 是 Official720PModelName 的**旧名**（2026-10-30 改名前）。
//
// 过渡期**保留可调用**（与 wan-3.0-无限 → wan-3.0-特惠 同一套做法，见 manwu 适配器）：
//   · 已把旧名写进客户端代码的调用方不能立刻断线；
//   · 但旧名**不进模型广场**（model/pricing.go 的 isAllowedPricingModel 不放行它），
//     避免出现两个名字卖同一份货、客户不知道该用哪个。
//
// ⚠️ 旧名必须同样配 ModelPrice（setting/ratio_setting/model_ratio.go），
//	否则旧名会走 ModelPriceHelperPerCall 的「查不到价」分支，
//	任务按次计费算成 ¥0 —— **静默免单**，比报错更难发现。
const Official720PModelNameLegacy = "wan3.0-720p-官网"

// official720PGenerationResolution 是 Official720PModelName 向上游请求的分辨率。
//
// 固定 480P：先用便宜的 480P 生成，再提升到 720P，
// 比直接生成 720P 便宜（实测 480P ¥0.20/秒 vs 720P ¥0.45/秒）。
const official720PGenerationResolution = "480P"

// official720POutputResolution 是 Official720PModelName 最终交付的分辨率档位。
const official720POutputResolution = "720P"

// upstreamModelFor 把「对外模型名」映射成上游真正认识的模型名。
//
// 为什么必须有这个兜底（2026-10-30 修）：
//	渠道的 model_mapping 是**可配的**，管理员随时可能改掉或漏配新模型名。
//	而 BuildRequestBody 原本直接用 info.UpstreamModelName —— 那个值来自
//	渠道映射，映射里没有这个键时就还是对外名，于是上游收到
//	「wan3.0-720p-官网」这种它根本不认识的模型名，直接报错。
//
//	官网这两个名字的语义是固定的（上游永远是标准版 wan3.0-video），
//	所以在代码里锁死，**不依赖渠道配置**——与 youzanwan3 的 upstreamModelFor
//	是同一套做法。
//
// ⚠️ 只认这两个名字，别的一律放行原值：标准版 wan3.0-video 本来就对，
//	prime 等变体由渠道 model_mapping 负责，代码里不越俎代庖。
func upstreamModelFor(modelName string) string {
	switch strings.TrimSpace(modelName) {
	case OfficialModelName, Official720PModelName, Official720PModelNameLegacy:
		return upstreamModel
	default:
		return strings.TrimSpace(modelName)
	}
}

// isOfficial720PModel 判断某个对外模型名是否走「480P 生成 → 云端提升 → 交付 720P」
// 这条两跳链路。
//
// 新名（wan3.0-720p-特惠）与旧名（wan3.0-720p-官网）**都算**：
//	旧名是 2026-10-30 改名前的公开名，过渡期保留可调用（见 Official720PModelNameLegacy）。
//	只认新名的话，旧名调用方会静默落到单跳链路 —— 结果是
//	**用户买了 720P 拿到 480P，而且照样扣费**（最坏情况）。
func isOfficial720PModel(modelName string) bool {
	switch strings.TrimSpace(modelName) {
	case Official720PModelName, Official720PModelNameLegacy:
		return true
	default:
		return false
	}
}

// resolutionSizeRatio 各分辨率相对于 480P 基准价的倍率，**按对外模型名分表**。
//
// 官网渠道（OfficialModelName）：2026-10-22 用户拍板调价 → 480P ¥0.20/秒、
// 720P ¥0.45/秒、1080P ¥0.80/秒，对应倍率 1 : 2.25 : 4（0.45/0.20 = 2.25，
// 不是整数倍）。其 ModelPrice 配置为 480P 基准单价（USD 等值，0.2/7.3）。
//
// 其它模型名（wan3.0-video 标准版等）价格一律不动：
// 480P ¥0.27/秒、720P ¥0.54/秒、1080P ¥1.08/秒 → 1 : 2 : 4。
//
// ⚠️ 必须分表，不能用一张全局倍率表：调价只授权了官网渠道这一个模型名。
// 若共用一张表，标准版的 720P/1080P 会被连带带偏
// （0.27 × 2.25 = ¥0.6075/秒，是一个从未存在过的价）。
var officialResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  2.25,
	"1080P": 4.0,
}

var standardResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  2.0,
	"1080P": 4.0,
}

const (
	// upstreamModel 固定使用标准版
	upstreamModel = "wan3.0-video"

	// 默认分辨率
	defaultResolution = "480P"

	defaultRatio = "adaptive"
	defaultAudio = true

	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5

	// 参考视频时长上限（官方《万相3.0-视频生成API参考》2026-10-22 核对）：
	//   · reference_video 最多 5 段，**单个 [1, 15] 秒，总时长不大于 15 秒**；
	//   · reference_audio 最多 5 段，单个 [1, 15] 秒，总时长不大于 15 秒。
	// 单个与总时长都要在本地拦住：超限上游会拒，而本站的计费秒数
	// （生成 + 参考视频总秒数）正是上游的 usage.duration，超限等于按上游
	// 根本不会生成的规格预扣了钱。
	//
	// ⚠️ 音频上限（单个/总时长 15 秒）**不在本地强制**：本站不探测音频时长
	// （WAV 头可读、MP3 需解帧），超限只能由上游拒绝。
	maxReferenceVideoTotalSeconds = 15
	maxReferenceAudioTotalSeconds = 15

	// maxTotalDurationWithVideo 是「有视频输入时」的时长上限：
	// 官方规定 **输入视频总时长 + 输出视频时长不超过 30 秒**。
	// 本站计费秒数 = 生成时长 + 参考视频总秒数 = 同一个数，所以强制这条
	// 等于把预扣额度也一并锁在 30 秒内（无视频输入时生成时长本身就是 [2,30]）。
	maxTotalDurationWithVideo = 30
)
