package megabyai

const ChannelName = "MegaAI"

// ModelList 是 MegaAI 渠道对外暴露的模型。
//
//   - sd-2.5：按次计费，上游固定价（与分辨率、时长无关）。
//   - wan-3.0：按次计费 + 分辨率档（上游 480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0，均按「次」计）。
//
// ⚠️ 上游已不再提供无后缀的 sd-2.5（/v1/models 里只剩 sd-2.5-720p 这类带档位后缀的型号），
// 生产渠道只挂 wan-3.0；代码里保留 sd-2.5 是不破坏既有路径。
var ModelList = []string{
	"sd-2.5",
	"wan-3.0",
}

// wan30Model 是走「统一参数 + 分辨率档按次计费」契约的模型名。
const wan30Model = "wan-3.0"

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
