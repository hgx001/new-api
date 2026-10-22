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

// resolutionSizeRatio 各分辨率相对于 480P 基准价的倍率（比例 1:2:4）。
// 480P=¥0.27/秒、720P=¥0.54/秒、1080P=¥1.08/秒。
// wan3 模型的 ModelPrice 应配置为 480P 基准单价（USD 计价，默认 0.0369863），乘以 seconds × size 即得应收价格。
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

	maxReferenceImages = 10
	maxReferenceVideos = 5
	maxReferenceAudios = 5

	// 参考视频时长上限（官方《万相3.0-视频生成API参考》2026-10-22 核对）：
	//   · reference_video 最多 5 段，**单个 [1, 15] 秒，总时长不大于 15 秒**；
	//   · reference_audio 最多 5 段，单个 [1, 15] 秒，总时长不大于 15 秒。
	// 单个与总时长都要在本地拦住：超限上游会拒，而本站的计费秒数
	// （生成 + 参考视频总秒数）正是上游的 usage.duration，超限等于按上游
	// 根本不会生成的规格预扣了钱。
	//
	// ⚠️ 音频上限（单个/总时长 15 秒）**不在本地强制**：本站不探测音频时长
	// （WAV 头可读、MP3 需解帧），超限只能由上游拒绝。
	maxReferenceVideoTotalSeconds = 15
	maxReferenceAudioTotalSeconds = 15

	// maxTotalDurationWithVideo 是「有视频输入时」的时长上限：
	// 官方规定 **输入视频总时长 + 输出视频时长不超过 30 秒**。
	// 本站计费秒数 = 生成时长 + 参考视频总秒数 = 同一个数，所以强制这条
	// 等于把预扣额度也一并锁在 30 秒内（无视频输入时生成时长本身就是 [2,30]）。
	maxTotalDurationWithVideo = 30
)
