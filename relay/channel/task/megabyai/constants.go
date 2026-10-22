package megabyai

const ChannelName = "MegaAI"

// ModelList 是 MegaAI 渠道对外暴露的模型。
//
//   - sd-2.5：按次计费，上游固定价（与分辨率、时长无关）。
//   - wan-3.0：按次计费 + 分辨率档（上游 480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0，均按「次」计）。
//   - seedance2.5-特惠：wan-3.0 的对外别名，参数传递完全一致，但**固定 ¥5.0/条**
//     （不分分辨率档、不乘时长；上游仍按 wan-3.0 的实际档位结算，最高档 1080p 成本 ¥2.0）。
//
// ⚠️ 上游已不再提供无后缀的 sd-2.5（/v1/models 里只剩 sd-2.5-720p 这类带档位后缀的型号），
// 生产渠道只挂 wan-3.0 与 seedance2.5-特惠；代码里保留 sd-2.5 是不破坏既有路径。
var ModelList = []string{
	"sd-2.5",
	"wan-3.0",
	Seedance25TeHuiModel,
}

// wan30Model 是走「统一参数 + 分辨率档按次计费」契约的模型名。
const wan30Model = "wan-3.0"

// Seedance25TeHuiModel 是 wan-3.0 的对外别名（2026-10-22 新增）。
//
// 它复用 wan-3.0 的**全部**参数传递（duration / ratio / resolution / referenceImages ...），
// 只是对外价格口径不同：固定 ¥5.0/条，不按分辨率分档、不乘时长。
// 上游仍然是 wan-3.0 —— 渠道 model_mapping 配同样的映射，代码里另有兜底表，
// 渠道忘了配 mapping 时也不会把这个中文别名发给上游。
const Seedance25TeHuiModel = "seedance2.5-特惠"

// megabyaiUnifiedContractModels 是走上游「统一参数」契约的对外模型名集合。
//
// ⚠️ 必须用**精确集合**判断，不能用前缀：站内还有 seedance-2-5 / seedance-2.5 /
// seedance2.5特惠版 等一堆相近的 seedance 模型名，前缀匹配会误命中别家渠道的枚举表。
var megabyaiUnifiedContractModels = map[string]bool{
	wan30Model:           true,
	Seedance25TeHuiModel: true,
}

// megabyaiResolutionTierModels 是**按分辨率档计价**的模型（其余统一契约模型是固定按次价）。
var megabyaiResolutionTierModels = map[string]bool{
	wan30Model: true,
}

// megabyaiUpstreamModels 是「对外模型名 → 上游模型名」的兜底表。
//
// 渠道的 model_mapping 是权威配置，但这里再兜一层：别名模型的语义是固定的
// （seedance2.5-特惠 永远是 wan-3.0），不能因为渠道漏配 mapping 就把别名原样发给上游。
var megabyaiUpstreamModels = map[string]string{
	wan30Model:           wan30Model,
	Seedance25TeHuiModel: wan30Model,
}

// wan30ResolutionSizeRatio 是 wan-3.0 各分辨率档相对 480p 基准价的倍率。
//
// 对外价（每次）：480p ¥2.4 / 720p ¥3.0 / 1080p ¥4.0 → 1 : 1.25 : 1.666667。
// 基准价（480p）落在 defaultModelPrice["wan-3.0"]，这里只放档位之间的相对倍率。
// 上游成本是 ¥1.6 / ¥1.8 / ¥2.0（每次），对外价按 1.5 倍定价。
var wan30ResolutionSizeRatio = map[string]float64{
	"480p":  1.0,
	"720p":  1.25,
	"1080p": 1.666667,
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

	// 上游 wan-3.0 的素材数量上限（上游价目说明：参考图 ≤10、参考视频 ≤5、参考音频 ≤5）。
	wan30MaxReferenceImages = 10
	wan30MaxReferenceVideos = 5
	wan30MaxReferenceAudios = 5
)
