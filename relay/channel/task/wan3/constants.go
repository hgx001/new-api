package wan3

// ChannelName 后台展示名
const ChannelName = "Wan3"

// ModelList 对外暴露的模型。
// 统一使用 wan3.0-video，分辨率通过请求参数控制（与 DashScope 渠道对齐）。
// - resolution / metadata.resolution: "480P" | "720P"
// - 默认 480P（与 DashScope 渠道保持一致）
var ModelList = []string{
	"wan3.0-video",
}

// resolutionSizeRatio 各分辨率相对于 480P 基准价的倍率。
// 480P/720P 对外单价分别为 ¥2.66/秒和 ¥5.20/秒。
var resolutionSizeRatio = map[string]float64{
	"480P": 1.0,
	"720P": 5.2 / 2.66,
}

const (
	// upstreamModel 固定使用标准版
	upstreamModel = "wan3.0-video"

	// 默认分辨率
	defaultResolution = "480P"

	defaultRatio = "16:9"
	defaultAudio = false

	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5
)
