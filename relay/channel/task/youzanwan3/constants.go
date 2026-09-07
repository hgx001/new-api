package youzanwan3

const ChannelName = "Youzan Wan3"

// 有赞 Wan3 官方工作台支持的模型。
var ModelList = []string{
	"wan3.0-video",
	"wan3.0-video-prime",
}

// 价格按 480P 基准，分辨率倍率与现有 Wan3 保持一致。
var resolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  2.0,
	"1080P": 4.0,
}

const (
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
