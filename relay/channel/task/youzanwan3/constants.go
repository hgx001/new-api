package youzanwan3

const ChannelName = "Youzan Wan3"

const (
	// 上游模型 ID，同时作为 new-api 的公开模型名（广场展示名与上游完全一致）。
	videoModel = "wan3.0-video"
	primeModel = "wan3.0-video-prime"

	// smartLegacyModel / legacyPrimeModel 是 2026-10-28 前的公开名，语义等价：
	// wan3.0-smart → wan3.0-video、wan3.0-video-prime-1080p → wan3.0-video-prime。
	// 保留仅为让老客户的既定请求继续可路由（广场已不再展示）。
	smartLegacyModel = "wan3.0-smart"
	legacyPrimeModel = "wan3.0-video-prime-1080p"

	// r2vModel 已从上游 /v1/models 目录消失，调用会被 upstreamModelFor 明确拒绝；
	// 该名字仅用于给出可读错误，并兼容仍在轮询中的历史任务。
	r2vModel = "wan2.7-r2v"
)

// ModelList 与上游 /v1/models 的模型 ID 完全一致（2026-10-28 起上游只提供这两个）。
var ModelList = []string{
	videoModel,
	primeModel,
}

// videoResolutionSizeRatio：上游 wan3.0-video 三档同价（0.18 ⚡/秒 = ¥0.18/秒），
// 售价统一 ¥0.23/秒（上游 + ¥0.05），故三档倍率均为 1。
// ModelPrice 应配置为 ¥0.23 对应的 USD（0.23/7.3 ≈ 0.0315068）。
var videoResolutionSizeRatio = map[string]float64{
	"480P":  1.0,
	"720P":  1.0,
	"1080P": 1.0,
}

// primeResolutionSizeRatio：上游 wan3.0-video-prime 按分辨率分档计价
// （720P 0.15 ⚡/秒、1080P 0.20 ⚡/秒）。售价 = 上游 + ¥0.05：
// 720P ¥0.20/秒、1080P ¥0.25/秒。ModelPrice 按 720P 基准配置（0.20/7.3 ≈ 0.0273973），
// 1080P 倍率 = 0.25 / 0.20 = 1.25。
var primeResolutionSizeRatio = map[string]float64{
	"720P":  1.0,
	"1080P": 1.25,
}

// r2vResolutionSizeRatio：wan2.7-r2v 已下架，仅为轮询中的历史任务保留倍率表。
// 上游只认小写分辨率，键名与适配器输出保持一致。
var r2vResolutionSizeRatio = map[string]float64{
	"720p":  1.0,
	"1080p": 1.0,
}

// smartRatios 是上游 /v1/tasks 允许的成片比例（2026-10-28 新增 21:9 宽银幕），
// 两个在售模型共用同一套比例。
var smartRatios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}

// r2vRatios：wan2.7-r2v 仅支持固定比例，不接受 adaptive（仅历史任务轮询用）。
var r2vRatios = []string{"16:9", "9:16", "1:1"}

const (
	// reasonContentModeration 是无理由失败的统一口径：任务已运行一段时间
	// 才失败、且上游未返回任何原因时，归因为内容审核不通过。
	reasonContentModeration = "内容审核不通过"

	// videoModel 三档（480P/720P/1080P）同价，缺省取最低档 480P。
	defaultResolution = "480P"
	// primeDefaultResolution：上游 prime 只提供 720P/1080P，缺省取最低档 720P
	// （不静默升到更贵的 1080P）。
	primeDefaultResolution = "720P"

	defaultRatio = "adaptive"

	// wan2.7-r2v 的历史限制（时长仅 5s / 10s 两档，比例固定）。
	// 上游已下架，这些常量仅用于兼容仍在轮询中的历史任务。
	r2vMinDuration  = 5
	r2vMaxDuration  = 10
	r2vDefaultRatio = "16:9"

	// 两个在售模型均为按秒计费，时长 2—30 秒逐秒可选。
	minDuration     = 2
	maxDuration     = 30
	defaultDuration = 5

	// 2026-10-28 上游 /v1/tasks 的 media 上限：图片 10 + 视频 5 + 音频 5，
	// 三类合计不得超过 20 项。两个模型同一口径（旧版 prime 的 8 张限制已取消）。
	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5
	maxTotalMedia      = 20
)
