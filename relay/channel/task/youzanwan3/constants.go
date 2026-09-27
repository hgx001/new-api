package youzanwan3

const ChannelName = "Youzan Wan3"

// 有赞 Wan3 官方工作台支持的模型。
var ModelList = []string{
	"wan3.0-smart",
	"wan2.7-r2v",
}

// smartResolutionSizeRatio 智能调度版各分辨率相对于 480P 基准价的倍率：
// 480P=¥0.28/秒、720P=¥0.32/秒、1080P=¥0.40/秒（上游成本 20 点数/秒，约 ¥0.20/秒）。
// smart 模型的 ModelPrice 应配置为 480P 基准单价（USD 计价，0.28/7.3）。
var smartResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  0.32 / 0.28,
	"1080P": 0.40 / 0.28,
}

// r2vResolutionSizeRatio：wan2.7-r2v 对外全分辨率统一价，所以各档倍率均为 1。
// 基准单价由 ModelPrice 提供（¥0.10/秒）；wan2.7-r2v 不支持 480P，默认使用 1080P。
var r2vResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 1.0,
}

const r2vModel = "wan2.7-r2v"

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
