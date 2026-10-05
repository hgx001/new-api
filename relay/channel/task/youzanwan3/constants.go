package youzanwan3

const ChannelName = "Youzan Wan3"

// 有赞 Wan3 官方工作台支持的模型。
var ModelList = []string{
	"wan3.0-smart",
	"wan3.0-video-prime-1080p",
	"wan2.7-r2v",
}

// smartResolutionSizeRatio 智能调度版各分辨率相对于 480P 基准价的倍率：
// 480P=¥0.28/秒、720P=¥0.28/秒、1080P=¥0.30/秒（上游成本 20 点数/秒，约 ¥0.20/秒）。
// smart 模型的 ModelPrice 应配置为 480P 基准单价（USD 计价，0.28/7.3）。
//
// 2026-10-05 调价：720P ¥0.32→¥0.28、1080P ¥0.40→¥0.30，480P 维持 ¥0.28。
// 注意 720P 与 480P 现已同价，倍率写 1.0；调价只动这张表，不要动 ModelPrice
// 的 480P 基准（动了会连带把三档一起抬高）。
var smartResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  1.0,
	"1080P": 0.30 / 0.28,
}

// r2vResolutionSizeRatio：wan2.7-r2v 对外全分辨率统一价，所以各档倍率均为 1。
// 基准单价由 ModelPrice 提供（¥0.10/秒）；wan2.7-r2v 不支持 480P，默认使用 1080p，
// 上游只认小写分辨率，键名与适配器输出保持一致。
var r2vResolutionSizeRatio = map[string]float64{
	"720p":  1.0,
	"1080p": 1.0,
}

// smartRatios 智能调度版允许的成片比例；智能调度会自适应当前比例。
var smartRatios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4"}

// r2vRatios：上游 wan2.7-r2v 仅支持固定比例，不接受 adaptive。
var r2vRatios = []string{"16:9", "9:16", "1:1"}

const r2vModel = "wan2.7-r2v"

// primeModel 是上游的满血 30 秒档（上游名 "wan3满血30秒"）：
// 与 smart 同走 wan3_all_in_one，但只支持 1080P、时长固定 30 秒，
// 对外按次计费（¥8/次），所以不参与时长/分辨率倍率。
const primeModel = "wan3.0-video-prime-1080p"

const (
	// reasonContentModeration 是无理由失败的统一口径：任务已运行一段时间
	// 才失败、且上游未返回任何原因时，归因为内容审核不通过。
	reasonContentModeration = "内容审核不通过"

	defaultResolution = "480P"
	defaultRatio      = "adaptive"

	// wan2.7-r2v 上游限制：时长仅 5s / 10s 两档，比例固定 16:9 / 9:16 / 1:1，
	// 参考图最多 3 张（wan3 系为 10 张）。
	r2vMinDuration        = 5
	r2vMaxDuration        = 10
	r2vDefaultRatio       = "16:9"
	r2vMaxReferenceImages = 3
	defaultAudio          = true

	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5

	// wan3.0-video-prime-1080p 上游限制：分辨率仅 1080P、时长固定 30 秒，
	// 参考图上限 8 张（smart 为 10 张）。
	primeResolution         = "1080P"
	primeDuration           = 30
	primeMaxReferenceImages = 8
)
