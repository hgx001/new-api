package dashscope

// ChannelName 后台展示名
const ChannelName = "DashScope"

// ModelList 对外暴露的模型（阿里云百炼 wan3.0-video）。
// 底层同一个 upstream 模型 wan3.0-video，分辨率通过请求参数控制：
//   - resolution / metadata.resolution: "480P" | "720P"（推荐）
//   - size: "960x540" → 480P, "1280x720" → 720P（兼容）
//   - 默认 480P
var ModelList = []string{
	"wan3.0-video",
}

// resolutionByModel 兼容旧模型名映射（迁移过渡期使用）。
var resolutionByModel = map[string]string{
	"wan3.0-video-480p": "480P",
	"wan3.0-video-720p": "720P",
	"wan3.0-video":      "", // 由请求参数决定
}

// sizeToResolution 将 size 字符串（"1280x720"）映射到 DashScope resolution 参数。
var sizeToResolution = map[string]string{
	"960x540":  "480P",
	"1280x720": "720P",
}

// resolutionPriceCNY 各分辨率的每秒对外售价（人民币）。
// new-api 内部 ModelPrice 以 USD 计，基准价取 480P（¥2.66/7.3≈0.36438 USD/s）。
// 720P 通过 EstimateBilling 返回 size ratio（5.2/2.66≈1.955）乘以基准价。
var resolutionPriceCNY = map[string]float64{
	"480P": 2.66,
	"720P": 5.2,
}

// resolutionSizeRatio 各分辨率相对于 480P 基准价的倍率。
var resolutionSizeRatio = map[string]float64{
	"480P": 1.0,
	"720P": 5.2 / 2.66, // ≈1.9549
}

const (
	// baseURLDefault DashScope API 固定域名；渠道 base_url 为空时兜底。
	baseURLDefault = "https://dashscope.aliyuncs.com"

	// 上游模型名（DashScope 固定）。
	upstreamModel = "wan3.0-video"

	// 默认分辨率（客户端未指定时）。
	defaultResolution = "480P"

	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	// 客户端未指定比例时的默认档位
	defaultRatio = "16:9"
)
