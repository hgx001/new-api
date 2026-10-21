package sanyuan

// 三元巡狩（SYX）视频生成平台的渠道常量与模型档位表。
//
// 依据 2026-10-14 / 2026-10-21 实时读取的上游合同（Flask 自建，非 OpenAI 兼容）：
//   GET /v1/workflows → 8 条工作流，**全部共用同一套建单字段**（2026-10-21 catalog
//   实测：8 条工作流的 params 完全一致）：
//     prompt / duration / resolution / image_data_urls / audio_data_urls /
//     seed / context_ir_enabled / workflow；
//   POST /v1/video/generate → {ok, event_id}；POST /v1/video/check → {ok, status}；
//   GET /v1/video/file/{event_id} 下载产物（需渠道 Bearer）。
//
// ⚠️ 开放 API **没有参考视频、没有首尾帧字段** —— 本站所有三元巡狩模型都不支持这两种
// 素材。wan3.0-video-prime-1080p 曾经对外宣传的「参考视频 ≤5 / 首尾帧各 1」是原
// 有赞上游的能力，2026-10-21 随上游切换一并从描述与教程里删除。
//
// 各工作流的差异只在**档位与计费**：
//
//	wanwan 1080P（拼车）        ¥1/次  次日发车（12–14 点 / 20 点）  480p–1080p  1–30 秒
//	WAN 3.0（720P/1080P·30s）  ¥4/次  实时                        720p/1080p  5–30 秒
//	WAN 3.0 PRO                ¥5.2/次 实时                        720p/1080p  5–30 秒
//	wan3.0-smart               ¥0.2/秒 实时                        480p–1080p  ≤30 秒

const (
	ChannelName = "三元巡狩"

	// SlowPublicModel 是拼车慢速档（对外 ¥2/次，上游成本 ¥1/次）。
	SlowPublicModel = "wan3.0-1080p-慢速"

	// PrimePublicModel 是满血 30 秒 1080P 档（对外 ¥8/次，上游成本 ¥4/次）。
	//
	// 2026-10-21：上游从「有赞 Wan3」切到三元巡狩的 WAN 3.0 实时工作流 —— 原 youzan
	// 侧的「wan3满血30秒」已被上游撤下（返回「模型 …不存在或未启用」）。对外模型名
	// 与 ¥8.00/次 的定价保持不变，变的只有上游与部分能力口径。
	PrimePublicModel = "wan3.0-video-prime-1080p"

	// PublicModel / UpstreamWorkflow 是历史常量名，仍指向慢速档，供旧引用使用。
	PublicModel      = SlowPublicModel
	UpstreamWorkflow = SlowWorkflow

	// SlowWorkflow 是慢速档使用的上游工作流名。
	SlowWorkflow = "wanwan 1080P（拼车）"

	// PrimeWorkflow 是满血档使用的上游工作流名。**必须与上游原文逐字一致**
	// （全角括号、括号后的空格、中间的间隔号），差一个字符上游就 400「不支持的子接口」。
	PrimeWorkflow = "WAN 3.0（ 720P/1080P · 30s）"

	// createPath / checkPath / fileFormat 取自上游 API 文档。
	// fileFormat 是产物相对路径（download_url 形如 /v1/video/file/{event_id}），
	// 需渠道 Bearer 才能取，对外一律走站内 content 代理。
	createPath = "/v1/video/generate"
	checkPath  = "/v1/video/check"
	fileFormat = "/v1/video/file/%s"
)

// 时长：慢速档 wanwan 的 duration_options=[]（不设档位），1-30 自由整数真跑。
// 区间外 fail-loud 400，不吸附 —— 静默改档会让用户按 31 秒的期待拿到 30 秒成片。
const (
	minDurationSeconds     = 1
	maxDurationSeconds     = 30
	defaultDurationSeconds = 5
)

// 满血档：上游 WAN 3.0 的 usage_note 写「时长 5–30 秒」，合同写 max_duration=30。
// 对外语义是「固定 30 秒」（与原 youzan prime 一致：传别的值被忽略）。
const (
	primeMinDurationSeconds = 5
	primeMaxDurationSeconds = 30
	primeDurationSeconds   = 30
)

// 分辨率：tier(480p/720p/1080p) × 方向(竖/横)。无提示时默认竖。
const (
	defaultResolution  = "480p"
	primeResolution    = "1080p"
	directionPortrait  = "竖"
	directionLandscape = "横"
)

// allowedResolutions 是慢速档（拼车工作流）的 resolution_options 精确档位。
var allowedResolutions = map[string]bool{
	"480p竖": true, "480p横": true,
	"720p竖": true, "720p横": true,
	"1080p竖": true, "1080p横": true,
}

// primeResolutions 是 WAN 3.0 工作流的档位（无 480p）。
var primeResolutions = map[string]bool{
	"720p竖": true, "720p横": true,
	"1080p竖": true, "1080p横": true,
}

// 素材上限取合并协议文档（image_data_urls ≤9、audio_data_urls ≤3），比上游
// capabilities（图 10 / 音 5）更严；文档注明首张图被当首帧。参考视频与尾帧开放
// API 没有入参，一律拒绝。
const (
	maxReferenceImages = 9
	maxReferenceAudios = 3
	maxMediaBytes      = 20 << 20 // 20971520，单素材下载上限
)

// modelProfile 描述一个对外模型在三元巡狩侧的档位：上游工作流 + 各自的约束。
//
// 上游所有工作流共用同一套建单字段，差异只在档位与计费，所以这里按模型各存一份
// 约束，而不是把档位写死成一个渠道。
type modelProfile struct {
	// PublicModel 是下游唯一能请求、能在模型广场看到的名字。
	PublicModel string
	// Workflow 是 /v1/workflows 的工作流名，创建请求的 workflow 必须用它。
	Workflow string

	// 时长：Min/Max 是上游接受的区间，Default 是未指定时的取值，
	// Fixed > 0 表示该档位时长固定（忽略请求里的时长）。
	MinDuration     int
	MaxDuration     int
	DefaultDuration int
	FixedDuration   int

	// 分辨率：Resolutions 是该工作流支持的完整档位集合（如 "1080p竖"），
	// DefaultTier 是未指定时的档位，FixedTier 非空表示该档位分辨率固定
	// （忽略请求里的其它 tier，只保留方向）。
	Resolutions map[string]bool
	DefaultTier string
	FixedTier   string

	// 素材上限。
	MaxReferenceImages int
	MaxReferenceAudios int
}

// slowProfile 是拼车慢速档：¥1/次、次日发车、时长 1–30 自由、6 档分辨率。
var slowProfile = &modelProfile{
	PublicModel:        SlowPublicModel,
	Workflow:           SlowWorkflow,
	MinDuration:        minDurationSeconds,
	MaxDuration:        maxDurationSeconds,
	DefaultDuration:    defaultDurationSeconds,
	Resolutions:        allowedResolutions,
	DefaultTier:        defaultResolution,
	MaxReferenceImages: maxReferenceImages,
	MaxReferenceAudios: maxReferenceAudios,
}

// primeProfile 是满血档：¥4/次、实时、固定 30 秒 1080P。
var primeProfile = &modelProfile{
	PublicModel:        PrimePublicModel,
	Workflow:           PrimeWorkflow,
	MinDuration:        primeMinDurationSeconds,
	MaxDuration:        primeMaxDurationSeconds,
	DefaultDuration:    primeDurationSeconds,
	FixedDuration:      primeDurationSeconds,
	Resolutions:        primeResolutions,
	DefaultTier:        primeResolution,
	FixedTier:          primeResolution,
	MaxReferenceImages: maxReferenceImages,
	MaxReferenceAudios: maxReferenceAudios,
}

// profilesByModel 是对外模型名 → 档位表的索引。
var profilesByModel = map[string]*modelProfile{
	SlowPublicModel:  slowProfile,
	PrimePublicModel: primeProfile,
}

// 失败原因兜底文案：上游 error 是自由文本，原样透传；缺省才用它。
const reasonUpstreamFailed = "上游任务失败"
