package sanyuan

// 三元巡狩（SYX）视频生成平台的渠道常量。
//
// 依据 2026-10-14 实时读取的上游合同（Flask 自建，非 OpenAI 兼容）：
//   GET /v1/workflows  → wanwan 1080P（拼车）：¥1/次按次计费、时长 1-30 自由整数、
//   分辨率 480p竖/480p横/720p竖/720p横/1080p竖/1080p横（默认 480p竖）、
//   first_frame=false、max_reference_images=10、max_reference_audio=5
//   POST /v1/video/generate → {ok, event_id}；POST /v1/video/check → {ok, status}；
//   GET /v1/video/file/{event_id} 下载产物（需渠道 Bearer）。

const (
	ChannelName = "三元巡狩"

	// PublicModel 是下游唯一能请求、能在模型广场看到的名字（用户指定，
	// 按次 ¥2 定价；上游工作流成本 ¥1/次）。
	PublicModel = "wan3.0-1080p-慢速"

	// UpstreamWorkflow 是 /v1/workflows 的固定工作流名，创建请求的 workflow 必须用它。
	UpstreamWorkflow = "wanwan 1080P（拼车）"

	// createPath / checkPath / fileFormat 取自上游 API 文档。
	// fileFormat 是产物相对路径（download_url 形如 /v1/video/file/{event_id}），
	// 需渠道 Bearer 才能取，对外一律走站内 content 代理。
	createPath = "/v1/video/generate"
	checkPath  = "/v1/video/check"
	fileFormat = "/v1/video/file/%s"
)

// 时长：wanwan 的 duration_options=[]（不设档位），1-30 自由整数真跑。
// 区间外 fail-loud 400，不吸附 —— 静默改档会让用户按 31 秒的期待拿到 30 秒成片。
const (
	minDurationSeconds     = 1
	maxDurationSeconds     = 30
	defaultDurationSeconds = 5
)

// 分辨率 6 档：tier(480p/720p/1080p) × 方向(竖/横)。defaultResolution 是默认
// tier（480p），方向无提示时默认竖（directionPortrait）。
const (
	defaultResolution  = "480p"
	directionPortrait  = "竖"
	directionLandscape = "横"
)

// allowedResolutions 是上游 workflow 的 resolution_options 精确档位。
var allowedResolutions = map[string]bool{
	"480p竖": true, "480p横": true,
	"720p竖": true, "720p横": true,
	"1080p竖": true, "1080p横": true,
}

// 素材上限取合并协议文档（image_data_urls ≤9、audio_data_urls ≤3），比 wanwan
// capabilities（图 10 / 音 5）更严；文档注明首张图被当首帧。参考视频与尾帧该工作流
// 不支持（capabilities：first_frame=false / last_frame=false / max_reference_videos=0）。
const (
	maxReferenceImages = 9
	maxReferenceAudios = 3
	maxMediaBytes      = 20 << 20 // 20971520，单素材下载上限
)

// 失败原因兜底文案：上游 error 是自由文本，原样透传；缺省才用它。
const reasonUpstreamFailed = "上游任务失败"
