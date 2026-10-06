package erchun

// 二春（Erchun）v1 开放 API 的渠道常量。
//
// 依据 2026-10-06 实时读取的线上合同：
//   GET /api/open-api/documentation  合同 api_724ce42b2efce7f0aeffd76ed1701a2a
//   GET /v1/catalog                  模型 mdl_ec286eb1bc618249d65b87bc79366417（阿里云 Wan 3.0）
//
// 本渠道对外只暴露一个模型，且只允许 480P —— 这是产品决定，不是上游限制：
// 上游目录里 resolutions 是 480P/720P/1080P（默认 1080P），我们在校验层把非 480P
// 直接 400，避免下游用我们没定价的档位建单。

const (
	ChannelName = "二春 Erchun"

	// PublicModel 是下游唯一能请求、能在模型广场看到的名字。
	PublicModel = "wan3.0-480p"

	// UpstreamModelWan3 是 /v1/catalog 的稳定 ID，创建请求的 model 必须用它。
	// display_name（阿里云 Wan 3.0）只用于界面展示，不能当请求值。
	UpstreamModelWan3 = "mdl_ec286eb1bc618249d65b87bc79366417"

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
	// defaultResolution 是本渠道唯一允许的档位。上游默认是 1080P，我们必须显式
	// 下发 480P，否则会按上游默认值建出 1080P 的单而按 480P 收费。
	onlyResolution = "480P"
)

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
