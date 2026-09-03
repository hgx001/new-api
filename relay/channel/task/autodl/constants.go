package autodl

// ChannelName 后台展示名
const ChannelName = "AutoDL"

// ModelList 对外暴露的模型（AutoDL ComfyUI 视频生成）。
// 模型名以 autodl: 前缀命名空间，便于模型广场白名单统一放行。
// workflow_id 与入参 schema 来自 autodl.art 控制台「ComfyUI工作流」页
// （/large-model/comfyui/<id> 右侧抽屉 API 标签），2026-09-01 浏览器实测抓取。
var ModelList = []string{
	"autodl:h3-video",
	"autodl:multiref-video-1",
	"autodl:multiref-video-2",
	"autodl:multiref-video-3",
}

// workflowConfig 每个模型对应的 AutoDL ComfyUI 工作流配置
type workflowConfig struct {
	// WorkflowID 是 autodl.art 的工作流 ID（控制台点开工作流 -> 右侧抽屉 -> workflow_id）。
	WorkflowID string
	// Resolution 是未指定 resolution 时使用的默认分辨率档位。
	Resolution string
	// Resolutions 是该工作流实际允许的 resolution 枚举值。
	Resolutions []string
	// MaxDuration 该工作流允许的最大时长（秒）；<=0 时用全局 maxDuration 兜底。
	MaxDuration int
	// MaxPromptLength 是上游允许的 prompt 最大字符数。
	MaxPromptLength int
	// ResolutionRatios 是相对于该工作流基础分辨率价格的计费倍率。
	ResolutionRatios map[string]float64
	// MaxSeed 是随机种子的最大值；<=0 表示该工作流不支持 seed。
	MaxSeed int64
	// RequiresImages 是否必须传参考图（ref_image_0 为必填）。
	RequiresImages bool
	// MaxImages 最多参考图数量（当前所有多图工作流均为 9：ref_image_0..8）。
	MaxImages int
}

// workflowByModel 模型名 -> 工作流配置。
//
// 多参考图类工作流入参为 ref_image_0..ref_image_8 独立字段（必填 ref_image_0），
// 由适配器把客户端的 images 数组按下标展开注入，见 adaptor.go BuildRequestBody。
// v5 的 1080p 上游单价是 480p/768p 的 4.5 倍，平台停用该档（见下方 workflowByModel）；
// 其他分辨率价格相同。
// h3ResolutionRatios 是 H3 系工作流的分辨率计费倍率（2026-09-03 起按官网调价）：
// 480p/736p 保持基准 ¥0.10/秒，768p（720p 档）调至 ¥0.12/秒（ratio 1.2），
// 计费公式：ModelPrice × seconds × size。
var h3ResolutionRatios = map[string]float64{
	"480p竖":     1.0,
	"768p竖":     1.2,
	"480p横":     1.0,
	"768p横":     1.2,
	"480p(1:1)": 1.0,
	"768p(1:1)": 1.2,
}

var workflowByModel = map[string]workflowConfig{
	// H3 文生视频（无需参考图）：duration 1-15s；480p/768p 竖横(1:1)。
	"autodl:h3-video": {
		WorkflowID:      "minimax_h3_lightx2v_no_pic",
		Resolution:      "768p竖",
		Resolutions:     []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:     15,
		MaxPromptLength: 200000,
		ResolutionRatios: h3ResolutionRatios,
	},
	// H3 多图参考生视频：duration 1-10s；480p/768p 竖横(1:1)；ref_image_0 必填。
	"autodl:multiref-video-1": {
		WorkflowID:       "minimax_h3_lightx2v_v5",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      10,
		MaxPromptLength:  500000,
		ResolutionRatios: h3ResolutionRatios,
		MaxSeed:          999999999999999,
		RequiresImages:   true,
		MaxImages:        9,
	},
	// H3 多图生视频15秒：duration 1-15s；480p/768p 竖横(1:1)；ref_image_0 必填。
	"autodl:multiref-video-2": {
		WorkflowID:       "minimax_h3_lightx2v_v5_15s",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  500000,
		ResolutionRatios: h3ResolutionRatios,
		MaxSeed:          999999999999999,
		RequiresImages:   true,
		MaxImages:        9,
	},
	// H3 多图生视频12秒：duration 1-12s；仅 736p 竖/横/(1:1)；ref_image_0 必填。
	"autodl:multiref-video-3": {
		WorkflowID:      "minimax_h3_b99_003_12s",
		Resolution:      "736p竖",
		Resolutions:     []string{"736p竖", "736p横", "736p(1:1)"},
		MaxDuration:     12,
		MaxPromptLength: 10000,
		MaxSeed:         999999999999999,
		RequiresImages:  true,
		MaxImages:       9,
	},
}

const (
	// baseURLDefault AutoDL ComfyUI API 固定域名；渠道 base_url 为空时兜底。
	baseURLDefault = "https://autodl.art"

	// 计费：¥0.1/秒。new-api 内部 ModelPrice 以 USD 计，换算 0.1/7.3；
	// 实际值写在渠道的 ModelPrice 字段，这里仅作常量说明，不硬编码进逻辑。
	// 上游参考价（2026-09-01 限时活动）：480p/768p 白天 ¥0.02/秒、夜间 ¥0.01/秒；
	// v5 工作流 1080p 白天 ¥0.09/秒、夜间 ¥0.05/秒。对外基准价仍由渠道 ModelPrice 配置。
	pricePerSecondCNY = 0.1

	minDuration     = 1
	maxDuration     = 30
	defaultDuration = 5
)
