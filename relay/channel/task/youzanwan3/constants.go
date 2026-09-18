package youzanwan3

const ChannelName = "Youzan Wan3"

// 有赞 Wan3 官方工作台支持的模型（2026-09-18 起仅剩智能调度版，
// 旧 wan3.0-video / wan3.0-video-prime 已下线）。
var ModelList = []string{
	"wan3.0-video-smart",
}

// smartResolutionSizeRatio 智能调度版各分辨率相对于 480P 基准价的倍率：
// 480P=¥0.28/秒、720P=¥0.45/秒、1080P=¥0.65/秒（上游成本 ¥0.18/秒）。
// smart 模型的 ModelPrice 应配置为 480P 基准单价（USD 计价，0.28/7.3）。
var smartResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  0.45 / 0.28,
	"1080P": 0.65 / 0.28,
}

const (
	// reasonContentModeration 是无理由失败的统一口径：任务已运行一段时间
	// 才失败、且上游未返回任何原因时，归因为内容审核不通过。
	reasonContentModeration = "内容审核不通过"

	defaultResolution = "480P"
	defaultRatio      = "adaptive"
	defaultAudio      = true

	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5
)
