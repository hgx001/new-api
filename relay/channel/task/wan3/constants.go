package wan3

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

// resolutionSizeRatio 各分辨率相对于 480P 基准价的倍率，与官方价目精确对齐：
// 官网 480P=¥0.3/秒、720P=¥0.6/秒、1080P=¥1.2/秒（比例 1:2:4）。
// wan3 模型的 ModelPrice 应配置为 480P 基准单价（默认 0.3），乘以 seconds × size 即得官网价格。
var resolutionSizeRatio = map[string]float64{
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

	maxReferenceImages            = 10
	maxReferenceVideos            = 5
	maxReferenceAudios            = 5
	maxReferenceVideoTotalSeconds = 15
	maxReferenceAudioTotalSeconds = 15
)
