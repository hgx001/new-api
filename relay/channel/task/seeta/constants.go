package seeta

// ChannelName 后台展示名
const ChannelName = "SeetaCloud"

// ModelList 对外暴露的模型（SeetaCloud 自建 ComfyUI 实例，ArcReel-H3 工作流）。
//
// 模型名带中文后缀，直接面向客户可见，便于在模型广场里按用途检索；
// 上游工作流名（minimax_h3_lightx2v_no_pic / minimax_h3_lightx2v）只在
// workflowByModel 里做内部映射，不出现在任何对外出口。
var ModelList = []string{
	"minimax_h3-文生",
	"minimax_h3-图生",
}

// 上游工作流名（对外不可见）。
const (
	// workflowTextToVideo 纯文生视频工作流。
	workflowTextToVideo = "minimax_h3_lightx2v_no_pic"
	// workflowImageToVideo 首帧图生视频工作流。
	workflowImageToVideo = "minimax_h3_lightx2v"
)

// workflowConfig 每个对外模型对应的上游工作流配置。
type workflowConfig struct {
	// WorkflowID 是上游 ComfyUI 工作流名，拼进
	// POST /api/v1/comfyui/comfyui_workflow/{WorkflowID}。
	WorkflowID string
	// Resolution 是未指定分辨率时使用的默认档位。
	Resolution string
	// Resolutions 是该工作流实际允许的分辨率枚举值。
	//
	// ⚠️ 上游只认「档位+方向」写法（480p横 / 768p竖 / 480p(1:1)），
	// 裸 "480p" 虽然也返回 200，但那是服务端兜底，不是文档契约，
	// 所以白名单只放带方向后缀的完整 token。
	Resolutions []string
	// MaxDuration 该工作流允许的最大时长（秒）；上游硬上限 15 秒。
	MaxDuration int
	// MaxPromptLength 是上游允许的 prompt 最大字符数。
	MaxPromptLength int
	// RequiresImages 是否必须传参考图。
	RequiresImages bool
	// MaxImages 最多参考图数量。
	MaxImages int
	// SupportsSeed 是否接受 seed 参数。
	SupportsSeed bool
	// ResolutionRatios 是相对于 480P 基础价的计费倍率。
	ResolutionRatios map[string]float64
}

// seetaResolutionRatios 是分辨率计费倍率：
//
//	480P  → ¥0.03/秒（基准，ratio 1.0）
//	768p  → ¥0.05/秒（720P 档，ratio 1.667）
//
// 注意上游的 768p 实际交付分辨率是 832×480 起步的 720P 档，
// 对客按「720p 档」称，与价格表里的 720P 档位对齐。
var seetaResolutionRatios = map[string]float64{
	"480p横":     1.0,
	"480p竖":     1.0,
	"480p(1:1)": 1.0,
	"768p横":     1.667,
	"768p竖":     1.667,
	"768p(1:1)": 1.667,
}

var workflowByModel = map[string]workflowConfig{
	// 文生视频：纯提示词，不接受参考图；duration 1-15s。
	"minimax_h3-文生": {
		WorkflowID:       workflowTextToVideo,
		Resolution:       "480p横",
		Resolutions:      []string{"480p横", "480p竖", "480p(1:1)", "768p横", "768p竖", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  200000,
		SupportsSeed:     true,
		ResolutionRatios: seetaResolutionRatios,
	},
	// 图生视频：需要 1 张参考图（首帧），duration 1-15s。
	"minimax_h3-图生": {
		WorkflowID:       workflowImageToVideo,
		Resolution:       "480p横",
		Resolutions:      []string{"480p横", "480p竖", "480p(1:1)", "768p横", "768p竖", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  200000,
		RequiresImages:   true,
		MaxImages:        9,
		SupportsSeed:     true,
		ResolutionRatios: seetaResolutionRatios,
	},
}

const (
	// baseURLDefault SeetaCloud 实例默认地址；渠道 base_url 为空时兜底。
	baseURLDefault = "https://u1133789-a936-c07acd4f.westd.seetacloud.com:8443"

	// 对外计价：480P ¥0.03/秒，768p（720P 档）¥0.05/秒。
	// 基础价由 ratio_setting.defaultModelPrice 提供，分辨率倍率由上面的工作流配置提供。
	pricePerSecondCNY = 0.03

	minDuration     = 1
	maxDuration     = 15
	defaultDuration = 5

	// maxReferenceImageBytes 单张参考图的最大字节数。
	// 上游只收 data URI，图片要随请求体一起送过去，所以必须在本地
	// 先拦体积，否则一个 20MB 的图会把网关内存打爆。
	maxReferenceImageBytes = 10 * 1024 * 1024

	// maxReferenceImageCount 单次最多参考图数量，与上游 ref_image_N 槽位对齐。
	maxReferenceImageCount = 9

	// maxUpstreamKeyframes 上游工作流真正生效的关键帧上限：首帧 + 尾帧 = 2 张。
	//
	// 上游 /v1/models 的描述写明「最多 2 张参考图——这是 V2 蒸馏权重的关键帧
	// 槽位上限，不是配置项」。实测（2026-11-06）传 3 张上游照收照出片，
	// 但第 3 张完全没有生效 —— 上游不报错只是默默忽略。
	// 所以这个上限必须由网关自己兜住，不能等上游报错。
	maxUpstreamKeyframes = 2
)
