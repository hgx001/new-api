package megabyai

const ChannelName = "MegaAI"

// ModelList 是 MegaAI 渠道对外暴露的模型。
//
//   - sd-2.5：按次计费，上游固定价（与分辨率、时长无关）。
//   - wan-3.0-megabyai-retired：原名 "wan-3.0"，按次计费 + 分辨率档
//     （上游 480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0，均按「次」计）。
//     ⚠️ 2026-11-18 改名归档：MegaAI 已停售，"wan-3.0" 让给有赞新模型。
//   - seedance2.5-特惠：wan-3.0 的对外别名，参数传递完全一致，但**固定 ¥3.0/条**
//     （不分分辨率档、不乘时长；上游锁 720p 结算，成本 ¥1.8/条）。
//
// ⚠️ 上游已不再提供无后缀的 sd-2.5（/v1/models 里只剩 sd-2.5-720p 这类带档位后缀的型号），
// 生产渠道只挂 wan-3.0 与 seedance2.5-特惠；代码里保留 sd-2.5 是不破坏既有路径。
// T3Seedance25Model 是 2026-11-23 重新接入 MegaAI 时上架的第二个对外模型。
//
// 上游目录（/api/pricing）描述：「30图10视频10音频，480p/720p，4-30秒，不卡脸，支持全比例」，
// 按次计费、分辨率分档（上游 480p ¥3 / 720p ¥4；本站对外 480p ¥4 / 720p ¥5）。
//
// ⚠️ **2026-11-24 下架**（用户口径「下架同渠道 T3-seedance-2.5」）：
// 不再出现在 ModelList（渠道 models / 广场都看不到），生产库的能力行与
// ModelPrice 键同步清除。代码里的能力表与映射**保留** —— 已建单的在途任务
// 仍走同一条链路轮询，删掉映射会让在途单改派到 passthrough。
const T3Seedance25Model = "T3-seedance-2.5"

// Seedance20FastM5Model / Seedance20M5Model 是 2026-11-24 新接入的两档（同一上游）。
//
// 上游是两个独立商品（同族但不同价，不能互为别名）：
//
//	seedance-2.0-fast-m5：480p ¥0.16/秒、720p ¥0.35/秒（**按秒**）
//	seedance-2.0-m5     ：480p ¥0.21/秒、720p ¥0.44/秒（**按秒**）
//
// 本站定价 = 上游各档 + ¥0.04/秒（老板口径「各个分辨率加 4 分钱」）：
//
//	seedance-2.0-fast-m5：480p ¥0.20/秒、720p ¥0.39/秒
//	seedance-2.0-m5     ：480p ¥0.25/秒、720p ¥0.48/秒
//
// 能力（两档相同）：9 图 / 3 视频 / 3 音频、4–15 秒、比例含 21:9、参考视频不计费。
const (
	Seedance20FastM5Model = "seedance-2.0-fast-m5"
	Seedance20M5Model     = "seedance-2.0-m5"
)

// ⚠️ 公开名带 `-m5` 后缀是**上游商品名**（老板指定沿用），不是渠道名：
// 面对客户的模型卡片描述里**不得**出现上游平台/渠道名（脱敏名单里有 megabyai/megaai）。

var ModelList = []string{
	"sd-2.5",
	wan30Model,
	Seedance25TeHuiModel,
	Seedance20FastM5Model,
	Seedance20M5Model,
}

// wan30Model 是走「统一参数 + 分辨率档按次计费」契约的模型名。
//
// ⚠️ 2026-11-18：这里**曾经**是 "wan-3.0"，但 MegaAI 渠道（type=64）已于
// 2026-11-10 停售，生产库里该渠道、其模型卡片、其 ModelPrice 键全部清空。
// 而 "wan-3.0" 这个名字 2026-11-18 被**有赞渠道的新独立模型 wan-3.0** 接管
// （按秒计费，¥0.23/秒起）。
//
// 两个商品同名会让「ModelPrice 基准价键」直接冲突：残留的按次价 ¥3.0/次
// 会覆盖新模型的按秒价，前端广场也会显示错的价格。所以这里把 MegaAI 的
// 对外名改成带后缀的**归档名**，让出 "wan-3.0" 给有赞。
//
// 改名后 MegaAI 这条链路彻底不可路由（渠道本身已不存在），属于预期结果；
// 保留本包是因为 youkou（type=59）还要复用 TaskAdaptor 的按次计费能力
// （见 relay/megabyai_channel_retired_test.go）。
const wan30Model = "wan-3.0-megabyai-retired"

// Seedance25TeHuiModel 是 wan-3.0 的对外别名（2026-10-22 新增）。
//
// 它复用 wan-3.0 的**全部**参数传递（duration / ratio / resolution / referenceImages ...），
// 只是对外价格口径不同：固定 ¥3.0/条，不按分辨率分档、不乘时长。
// 上游仍然是 wan-3.0 —— 渠道 model_mapping 配同样的映射，代码里另有兜底表，
// 渠道忘了配 mapping 时也不会把这个中文别名发给上游。
//
// ⚠️ 2026-11-18：上游名随 wan30Model 一起改为归档名 "wan-3.0-megabyai-retired"
// （原为 "wan-3.0"，让给有赞新模型）。MegaAI 渠道已停售，该链路已不可路由。
const Seedance25TeHuiModel = "seedance2.5-特惠"

// megabyaiUnifiedContractModels 是走上游「统一参数」契约的对外模型名集合。
//
// ⚠️ 必须用**精确集合**判断，不能用前缀：站内还有 seedance-2-5 / seedance-2.5 /
// seedance2.5特惠版 等一堆相近的 seedance 模型名，前缀匹配会误命中别家渠道的枚举表。
var megabyaiUnifiedContractModels = map[string]bool{
	wan30Model:            true,
	Seedance25TeHuiModel:  true,
	T3Seedance25Model:     true, // 2026-11-24 已下架，仅保在途单兼容
	Seedance20FastM5Model: true,
	Seedance20M5Model:     true,
}

// megabyaiResolutionTierModels 是**按分辨率档计价**的模型（其余统一契约模型是固定按次价）。
var megabyaiResolutionTierModels = map[string]bool{
	wan30Model:            true,
	T3Seedance25Model:     true, // 已下架，仅保在途单兼容
	Seedance20FastM5Model: true,
	Seedance20M5Model:     true,
}

// megabyaiPerSecondModels 是**按秒 + 分辨率档**双重计价的模型（2026-11-24）。
//
// ⚠️ 与 megabyaiResolutionTierModels 的分工：那个集合里的 wan-3.0 / T3 是
// 「按次 + 分辨率档」（EstimateBilling 只给 size，**绝不能给 seconds**）；
// 这里的 m5 两档是「按秒 + 分辨率档」，必须同时给 seconds 和 size。
// 两边弄反的后果都是静默错价：少给 seconds 时 4 秒片只收 1 秒；
// 多给 seconds 时按次价被乘秒数（¥4/条 → ¥120）。
var megabyaiPerSecondModels = map[string]bool{
	Seedance20FastM5Model: true,
	Seedance20M5Model:     true,
}

// megabyaiUpstreamModels 是「对外模型名 → 上游模型名」的兜底表。
//
// 渠道的 model_mapping 是权威配置，但这里再兜一层：别名模型的语义是固定的
// （seedance2.5-特惠 永远是 wan-3.0），不能因为渠道漏配 mapping 就把别名原样发给上游。
var megabyaiUpstreamModels = map[string]string{
	wan30Model:            wan30Model,
	Seedance25TeHuiModel:  wan30Model,
	T3Seedance25Model:     T3Seedance25Model,
	Seedance20FastM5Model: Seedance20FastM5Model,
	Seedance20M5Model:     Seedance20M5Model,
}

// wan30LockedTier 是「固定分辨率 + 固定时长」的档位。
type wan30LockedTier struct {
	Resolution string
	Duration   int
}

// megabyaiLockedTierModels 是能力被锁死档位的模型。
//
//   - seedance2.5-特惠：只出 720p、只出 30 秒一条。
//
// ⚠️ 这类模型的能力门控必须 **fail-loud**：客户端要别的档位时明确 400，
// 不能静默改档 —— 静默把 1080p 降成 720p，用户付了钱却拿不到想要的清晰度；
// 静默把 5 秒拉成 30 秒，客户端会以为省了钱，结果账单按次一样。
// 与 MiniMax 通道「能力门控一律 fail-loud」的口径一致。
var megabyaiLockedTierModels = map[string]wan30LockedTier{
	Seedance25TeHuiModel: {Resolution: "720p", Duration: 30},
}

// wan30ResolutionSizeRatio 是 wan-3.0 各分辨率档相对 480p 基准价的倍率。
//
// 对外价（每次）：480p ¥3.0 / 720p ¥3.5 / 1080p ¥4.0 → 1 : 3.5/3.0 : 4.0/3.0。
// 基准价（480p）落在 defaultModelPrice["wan-3.0"]，这里只放档位之间的相对倍率。
// 上游成本是 ¥1.6 / ¥1.8 / ¥2.0（每次）。
// 2026-10-22 由 ¥2.4/3.0/4.0 上调为 ¥3.0/3.5/4.0，倍率同步改为按新基准价算。
var wan30ResolutionSizeRatio = map[string]float64{
	"480p":  1.0,
	"720p":  3.5 / 3.0,
	"1080p": 4.0 / 3.0,
}

// wan30Resolutions 是上游 wan-3.0 的分辨率档（上游价目表只有这三档）。
var wan30Resolutions = []string{"480p", "720p", "1080p"}

const (
	// wan30DefaultResolution 是客户端未指定分辨率时**显式下发**给上游的档位。
	// 取上游最低档：显式下发才能保证「我们收的价」与「上游收的价」是同一档，
	// 否则会出现按 480p 计价、上游却按 1080p 收钱的倒挂。
	wan30DefaultResolution = "480p"

	// wan30ResolutionContextKey 存放本次请求解析出的分辨率档，
	// 供 EstimateBilling 复用（保证计费档位与下发档位是同一个值）。
	wan30ResolutionContextKey = "megabyai_wan30_resolution"

	// wan30DurationContextKey 存放本次请求解析出的时长（秒）。
	//
	// 按秒计费的 m5 两档靠它算钱：EstimateBilling 只在 ValidateRequestAndSetAction
	// 之后被调，把解析结果放 context 才能保证「计费秒数」与「实际下发秒数」
	// 永远是同一个值（重新解析一遍会在默认值/字段优先级上漂）。
	wan30DurationContextKey = "megabyai_wan30_duration"

	// 上游 wan-3.0 的素材数量上限（上游价目说明：参考图 ≤10、参考视频 ≤5、参考音频 ≤5）。
	wan30MaxReferenceImages = 10
	wan30MaxReferenceVideos = 5
	wan30MaxReferenceAudios = 5
)

// megabyaiCapability 描述一个对外模型的能力边界。
//
// 上游同一张价目表下不同模型的档位/素材数差别很大（wan-3.0 有 1080p、
// T3 没有；T3 能收 30 张图、wan-3.0 只收 10 张）。用一张表把它们分开描述，
// 比在适配器里到处写 `if model == ...` 更好查、更好测。
//
// 零值字段的含义：
//   - Resolutions 为空 → 用 wan30Resolutions（三档）；
//   - SizeRatios 为空 → 按次固定价（不带 size 倍率）；
//   - MinSeconds/MaxSeconds 都为 0 → 不检查时长区间（上游自己决定）。
//   - MaxImages/MaxVideos/MaxAudios 为 0 → 用 wan-3.0 的 10/5/5。
type megabyaiCapability struct {
	Resolutions []string
	SizeRatios  map[string]float64

	MaxImages int
	MaxVideos int
	MaxAudios int

	MinSeconds int
	MaxSeconds int
}

// T3 的对外价：480p ¥4.0 / 720p ¥5.0（按次）→ 1 : 5.0/4.0。
// 基准价（480p）落在 defaultModelPrice["T3-seedance-2.5"]，这里只放档位间相对倍率。
var t3ResolutionSizeRatio = map[string]float64{
	"480p": 1.0,
	"720p": 5.0 / 4.0,
}

// t3Resolutions 是上游 T3-seedance-2.5 的分辨率档（**没有 1080p**）。
// 客户传 1080p 必须本地 400：静默改成 720p 等于收了 720p 的钱却
// 没告诉客户清晰度降了，静默按 1080p 计费则是多收钱。
var t3Resolutions = []string{"480p", "720p"}

// m5Resolutions 是上游 m5 两档的分辨率档（同样**没有 1080p**）。
// 上游价目表只有 480p / 720p 两个 resolution_prices 键，多传档位只会让上游自己拒。
var m5Resolutions = []string{"480p", "720p"}

// m5 两档的对外价（按秒）：上游各档 + ¥0.04/秒（2026-11-24 老板口径）。
//
//	seedance-2.0-fast-m5：上游 0.16 / 0.35 → 对外 0.20 / 0.39 → 倍率 1 : 1.95
//	seedance-2.0-m5     ：上游 0.21 / 0.44 → 对外 0.25 / 0.48 → 倍率 1 : 1.92
//
// 基准价（480p 元/秒）落在 defaultModelPrice，这里只放档位之间的相对倍率。
// ⚠️ 改价必须两边成对改（基准价 × 倍率 = 720p 单价），否则两档差额会漂。
var (
	m5FastResolutionSizeRatio = map[string]float64{"480p": 1.0, "720p": 0.39 / 0.20}
	m5ResolutionSizeRatio     = map[string]float64{"480p": 1.0, "720p": 0.48 / 0.25}
)

// m5 两档的素材/时长口径（上游目录原文：「9图3视频3音频，4-15秒…视频最大支持总共15s、
// 音频总共15s」）。与 T3（30/10/10、4–30 秒）**不同**，不要共用同一份上限。
const (
	m5MaxReferenceImages = 9
	m5MaxReferenceVideos = 3
	m5MaxReferenceAudios = 3
	m5MinSeconds         = 4
	m5MaxSeconds         = 15
)

// megabyaiCapabilities 是「对外模型名 → 能力边界」。
var megabyaiCapabilities = map[string]megabyaiCapability{
	wan30Model: {
		Resolutions: wan30Resolutions,
		SizeRatios:  wan30ResolutionSizeRatio,
		MaxImages:   wan30MaxReferenceImages,
		MaxVideos:   wan30MaxReferenceVideos,
		MaxAudios:   wan30MaxReferenceAudios,
	},
	T3Seedance25Model: {
		Resolutions: t3Resolutions,
		SizeRatios:  t3ResolutionSizeRatio,
		MaxImages:   30,
		MaxVideos:   10,
		MaxAudios:   10,
		MinSeconds:  4,
		MaxSeconds:  30,
	},
	Seedance20FastM5Model: {
		Resolutions: m5Resolutions,
		SizeRatios:  m5FastResolutionSizeRatio,
		MaxImages:   m5MaxReferenceImages,
		MaxVideos:   m5MaxReferenceVideos,
		MaxAudios:   m5MaxReferenceAudios,
		MinSeconds:  m5MinSeconds,
		MaxSeconds:  m5MaxSeconds,
	},
	Seedance20M5Model: {
		Resolutions: m5Resolutions,
		SizeRatios:  m5ResolutionSizeRatio,
		MaxImages:   m5MaxReferenceImages,
		MaxVideos:   m5MaxReferenceVideos,
		MaxAudios:   m5MaxReferenceAudios,
		MinSeconds:  m5MinSeconds,
		MaxSeconds:  m5MaxSeconds,
	},
}

// capabilityFor 取模型的能力表；未知模型（含 sd-2.5 这类透传模型、
// 以及锁定档位模型）回落到 wan-3.0 的默认能力表。
//
// 锁定档位模型（seedance2.5-特惠）在解析分辨率/时长时走的是另一条分支，
// 不会用到这里的 Resolutions/SizeRatios，但素材上限仍是 10/5/5，与默认一致。
func capabilityFor(model string) megabyaiCapability {
	if c, ok := megabyaiCapabilities[model]; ok {
		if len(c.Resolutions) == 0 {
			c.Resolutions = wan30Resolutions
		}
		if c.MaxImages == 0 {
			c.MaxImages = wan30MaxReferenceImages
		}
		if c.MaxVideos == 0 {
			c.MaxVideos = wan30MaxReferenceVideos
		}
		if c.MaxAudios == 0 {
			c.MaxAudios = wan30MaxReferenceAudios
		}
		return c
	}
	return megabyaiCapability{
		Resolutions: wan30Resolutions,
		SizeRatios:  wan30ResolutionSizeRatio,
		MaxImages:   wan30MaxReferenceImages,
		MaxVideos:   wan30MaxReferenceVideos,
		MaxAudios:   wan30MaxReferenceAudios,
	}
}
