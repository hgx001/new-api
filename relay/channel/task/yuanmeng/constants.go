package yuanmeng

// 视频生成车站（yuanmengstudio.com）排队通道的渠道常量与模型档位表。
//
// 依据 2026-11-04 实时读取的上游合同（自建 Python/Flask 站点，非 OpenAI 兼容）：
//
//	POST /vr/api/student/login  → {ok, token, name, credits}
//	POST /vr/api/tasks          → {ok, task:{id, status, creditsCost}}
//	GET  /vr/api/tasks?pageSize=20 → {ok, tasks:[...], total, pages, counts, remaining}
//	GET  /vr/api/balance        → {ok, name, credits, unit, note, tasks:{...}}
//
// 鉴权是 **X-Token 请求头**（不是 Bearer）。登录一次拿到的 token 长期有效，
// 换卡密后旧 token 立即失效（已提交任务不受影响）。
//
// ⚠️ 上游把「次数」叫 credits，充值 1 元 = 1 次，**每条任务固定扣 1 次**，
//    与分辨率（480P/720P/1080P 同价）、时长档都无关。失败或取消会按实际扣减退回。
//
// ⚠️ 上游成片直链**只有约 24 小时有效**，站点不保存副本，过期无法恢复。
//    本站因此不直接透传上游地址：见 adaptor.go 的转存说明。
//
// ⚠️ 上游排队是 FIFO **攒批发车**，可能等待数小时。任务一旦发车就不能再改或取消。

const (
	// ChannelName 是渠道显示名。
	ChannelName = "视频生成车站"

	// PublicModel 是排队通道对外唯一的模型名。
	//
	// 上游是万相 wan3 全模态，但上游没有「模型」这个概念（只有通道），
	// 对外要起一个能表达「排队慢速」语义的名字。
	PublicModel = "wan3.0-排队"

	// UpstreamModel 是上游的模型标识。上游建单接口不带 model 字段，
	// 但保留常量以便日后上游加字段时对齐（与上游文档「排队通道 = wan3」对应）。
	UpstreamModel = "wan3"

	// createPath / tasksPath / balancePath 取自上游 API 文档。
	//
	// ⚠️ 上游站点挂在 /vr 下，所有接口路径都以 /vr/api 开头。
	createPath  = "/vr/api/tasks"
	tasksPath   = "/vr/api/tasks"
	balancePath = "/vr/api/balance"
)

// 素材规格上限（上游文档「素材规格限制」表）。超限上游会拒单。
//
// ⚠️ 单次任务所有类型素材合计最多 20 个，比各类单项上限更严。
const (
	maxReferenceImages = 10
	maxReferenceAudios = 5
	maxReferenceVideos = 5
	maxTotalMaterials  = 20

	// 单段音频/视频时长合计上限 15 秒；参考视频时长 + 生成时长 ≤ 30 秒。
	maxReferenceAudioSeconds = 15
	maxReferenceVideoSeconds = 15
	maxTotalDurationSeconds  = 30
)

// 时长：上游 duration 接受 2～30 的整数，也可传 -1 让平台智能决定。
// ⚠️ 传参考视频时，「参考视频时长 + 生成时长」必须 ≤ 30 秒。
const (
	minDurationSeconds     = 2
	maxDurationSeconds     = 30
	defaultDurationSeconds = 5
)

// 分辨率：上游三档同价（每条都扣 1 次），所以对外不按分辨率加价。
const (
	resolution480P  = "480P"
	resolution720P  = "720P"
	resolution1080P = "1080P"
)

// allowedResolutions 是上游接受的分辨率档位精确集合。
var allowedResolutions = map[string]bool{
	resolution480P:  true,
	resolution720P:  true,
	resolution1080P: true,
}

// defaultResolution 是未指定分辨率时的取值。
//
// 选 720P 的理由：上游三档同价，但对用户而言 720P 是「够用又不糊」的主流档；
// 480P 在大屏上明显偏糊，1080P 对排队通道（数小时）来说性价比不划算。
const defaultResolution = resolution720P

// 画幅比例：上游接受 7 种取值，adaptive 表示让平台自适应。
var allowedRatios = map[string]bool{
	"16:9":     true,
	"9:16":     true,
	"1:1":      true,
	"4:3":      true,
	"3:4":      true,
	"21:9":     true,
	"adaptive": true,
}

// defaultRatio 是未指定比例时的取值。上游参考图场景下 16:9 最常用。
const defaultRatio = "16:9"

// 提交模式：reference（全模态参考，默认）/ first_frame（首帧）/ first_last（首尾帧）。
const (
	modeReference = "reference"
	modeFirstFrame = "first_frame"
	modeFirstLast  = "first_last"
)

// 上游任务状态枚举（文档「任务状态枚举」）。
const (
	statusPending   = "pending"   // 候车中（已提交，等发车）
	statusSubmitted = "submitted" // 已发往平台，排队生成
	statusRunning   = "running"   // 生成中
	statusDone      = "done"      // 完成
	statusFailed    = "failed"    // 失败（次数自动退回）
	statusCancelled = "cancelled" // 已取消（终态）
)

// reasonUpstreamFailed 是上游没给失败原因时的兜底文案。
const reasonUpstreamFailed = "上游任务失败"
