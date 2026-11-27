package erchun

import "strings"

// 二春（Erchun）v1 开放 API 的渠道常量。
//
// 依据 2026-11-27 实时读取的线上目录：
//   GET /v1/catalog  模型 mdl_97ab8f57415ef0eddf1f0734e36f3a9a（阿里云 Wan 3.0 新线路）
//
// 本渠道对外只暴露一个模型，但开放 480P/720P/1080P 三档：
// 上游目录里 resolutions 是 480P/720P/1080P（默认 1080P），三档都必须显式下发、
// 显式定价 —— 不传 resolution 会被上游按默认 1080P 建单而按 480P 收费。

const (
	ChannelName = "二春 Erchun"

	// PublicModel 是下游唯一能请求、能在模型广场看到的名字。
	PublicModel = "wan3-新大陆"

	// UpstreamModelWan3 是 /v1/catalog 的稳定 ID，创建请求的 model 必须用它。
	// display_name（阿里云 Wan 3.0（新线路））只用于界面展示，不能当请求值。
	UpstreamModelWan3 = "mdl_97ab8f57415ef0eddf1f0734e36f3a9a"

	// createPath / taskPath / contentPath / uploadPath 均取自 catalog 条目的
	// endpoint / task_path / content_path，以及文档的素材上传端点。
	createPath  = "/v1/videos"
	taskPath    = "/v1/tasks/%s"
	contentPath = "/v1/tasks/%s/content"
	uploadPath  = "/v1/media/uploads"
)

// 时长：catalog 的 durations 是 enum，min=2 max=30 default=5。
const (
	minDurationSeconds     = 2
	maxDurationSeconds     = 30
	defaultDurationSeconds = 5
	// defaultResolution 是没有声明档位时的计费/下发档位。上游默认是 1080P，
	// 我们必须显式下发，否则会按上游默认值建出 1080P 的单而按 480P 收费。
	defaultResolution = "480P"
)

// resolutionPriceCNY 是本模型的**对外售价**（元/秒），按分辨率档位定价。
//
// 2026-11-27 用户定稿：480P ¥0.138/秒、720P ¥0.24/秒、1080P ¥0.5/秒。
// 计费时按「档位价 / 480P 基准价」反推倍率，基准价本身由 ModelPrice option
// 承载（设为 0.138/7.3 美元/秒），改价必须两边同时改。
var resolutionPriceCNY = map[string]float64{
	"480P":  0.138,
	"720P":  0.24,
	"1080P": 0.5,
}

// allowedResolutions 是本渠道对外定价并下发的档位集合。
var allowedResolutions = map[string]bool{
	"480P":  true,
	"720P":  true,
	"1080P": true,
}

// resolutionRatio 返回某档位相对 480P 基准价的计费倍率。
//
// 例：720P = 0.24/0.138 ≈ 1.7391，1080P = 0.5/0.138 ≈ 3.6232。
func resolutionRatio(resolution string) float64 {
	base, ok := resolutionPriceCNY[defaultResolution]
	if !ok || base <= 0 {
		return 1
	}
	price, ok := resolutionPriceCNY[strings.ToUpper(strings.TrimSpace(resolution))]
	if !ok || price <= 0 {
		return 1
	}
	return price / base
}

// 素材上限取自 catalog 的 capabilities（逐模型），不是文档里的全局 upload 限制 ——
// 全局更宽松（视频 256MB），但该模型只接受 100MB，按模型能力收紧才对。
const (
	maxImageBytes = 20 << 20  // 20971520
	maxVideoBytes = 100 << 20 // 104857600
	maxAudioBytes = 15 << 20  // 15728640

	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5
	maxMixedTotal      = 20
)

// 逐模型 MIME 白名单，同样取自 catalog capabilities。
var (
	allowedImageMIME = map[string]bool{"image/jpeg": true, "image/png": true, "image/webp": true}
	allowedVideoMIME = map[string]bool{"video/mp4": true, "video/quicktime": true}
	allowedAudioMIME = map[string]bool{"audio/mpeg": true, "audio/wav": true}
)

// allowedRatios 取自 /v1/catalog 该模型的 aspect_ratios，default 为 adaptive。
var allowedRatios = map[string]bool{
	"adaptive": true,
	"16:9":     true,
	"4:3":      true,
	"1:1":      true,
	"3:4":      true,
	"9:16":     true,
}

// 失败原因文案。
//
// 不能一律兜底成「内容审核不通过」：2026-10-08 manwu 豆包首单实证（commit 332ecfb5）
// 正是这个坑 —— 上游报 url_unavailable（产物 URL 校验未通过），本质是基础设施问题，
// 用户看到「内容审核不通过」会误以为自己提示词违规，白白排查半天。
// 故按失败模式给如实文案，与 manwu / youzanwan3 的处理保持一致。
const (
	reasonUpstreamFailed = "上游任务失败"
	reasonCancelled      = "任务已取消"
)
