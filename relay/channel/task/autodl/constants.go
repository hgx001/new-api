package autodl

// ChannelName 后台展示名
const ChannelName = "AutoDL"

// ModelList 对外暴露的模型（AutoDL ComfyUI 视频生成）。
// 模型名以 autodl: 前缀命名空间，便于模型广场白名单统一放行。
// workflow_id 与入参 schema 来自 autodl.art 控制台「ComfyUI工作流」页
// （/large-model/comfyui/<id> 右侧抽屉 API 标签），2026-09-11 对照官方工作流元数据。
var ModelList = []string{
	"autodl:minimax-h3-text-to-video",
	"autodl:minimax-h3-lightx2v-v5",
	"autodl:minimax-h3-lightx2v-v5-15s",
	"autodl:minimax-h3-b99-12s",
	"autodl:minimax-h3-u24",
	"autodl:minimax-h3-u08",
	"autodl:minimax-h3-image-audio-10s",
	"autodl:minimax-h3-image-audio-15s",
	"autodl:minimax-h3-lipsync",
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
	// PromptSupported 表示工作流是否接受 prompt 字段。
	PromptSupported bool
	// PromptRequired 表示工作流是否要求 prompt 非空。
	PromptRequired bool
	// ResolutionRatios 是相对于该工作流基础分辨率价格的计费倍率。
	ResolutionRatios map[string]float64
	// MinSeed 是随机种子的最小值；仅在 MaxSeed > 0 时生效。
	MinSeed int64
	// MaxSeed 是随机种子的最大值；<=0 表示该工作流不支持 seed。
	MaxSeed int64
	// SupportsImages 表示工作流是否接受参考图片。
	SupportsImages bool
	// RequiresImages 是否必须传参考图（ref_image_0 为必填）。
	RequiresImages bool
	// MaxImages 最多参考图数量（当前多图工作流为 9：ref_image_0..8）。
	MaxImages int
	// SupportsAudios 表示工作流是否接受参考音频。
	SupportsAudios bool
	// RequiresAudios 是否必须传参考音频（ref_audio_0 为必填）。
	RequiresAudios bool
	// MaxAudios 最多参考音频数量（当前多音频工作流为 3：ref_audio_0..2）。
	MaxAudios int
	// UsesAudioDuration 表示时长要以 AutoDL 的 audio_duration 字段提交。
	UsesAudioDuration bool
}

// workflowByModel 模型名 -> 工作流配置。
//
// 多参考图/多音频工作流的媒体入参均为带下标的独立字段，由适配器把客户端
// 的 images/audios 数组按下标展开注入，见 adaptor.go BuildRequestBody。
// h3ResolutionRatios 是 H3 系工作流的分辨率计费倍率（2026-09-03 起按官网调价）：
// 480p/736p 保持基准 ¥0.10/秒，768p（720p 档）调至 ¥0.12/秒（ratio 1.2），
// 计费公式：ModelPrice × seconds × size。
var h3ResolutionRatios = map[string]float64{
	"480p竖":      1.0,
	"768p竖":      1.2,
	"1080p竖":     4.5,
	"480p横":      1.0,
	"768p横":      1.2,
	"1080p横":     4.5,
	"480p(1:1)":  1.0,
	"768p(1:1)":  1.2,
	"1080p(1:1)": 4.5,
}

var h3V2ResolutionRatios = map[string]float64{
	"480p竖":  1.0,
	"768p竖":  1.2,
	"1080p竖": 5.0,
	"480p横":  1.0,
	"768p横":  1.2,
	"1080p横": 5.0,
}

var workflowByModel = map[string]workflowConfig{
	// H3 文生视频（无需参考图）：duration 1-15s；480p/768p 竖横(1:1)。
	"autodl:minimax-h3-text-to-video": {
		WorkflowID:       "minimax_h3_lightx2v_no_pic",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  200000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
	},
	// H3 多图参考生视频：duration 1-10s；480p/768p/1080p 竖横(1:1)；ref_image_0 必填。
	"autodl:minimax-h3-lightx2v-v5": {
		WorkflowID:       "minimax_h3_lightx2v_v5",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "1080p竖", "480p横", "768p横", "1080p横", "480p(1:1)", "768p(1:1)", "1080p(1:1)"},
		MaxDuration:      10,
		MaxPromptLength:  500000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          1,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		RequiresImages:   true,
		MaxImages:        9,
	},
	// H3 多图生视频15秒：duration 1-15s；480p/768p 竖横(1:1)；ref_image_0 必填。
	"autodl:minimax-h3-lightx2v-v5-15s": {
		WorkflowID:       "minimax_h3_lightx2v_v5_15s",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  500000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          1,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		RequiresImages:   true,
		MaxImages:        9,
	},
	// H3 多图生视频12秒：duration 1-12s；仅 736p 竖/横/(1:1)；ref_image_0 必填。
	"autodl:minimax-h3-b99-12s": {
		WorkflowID:       "minimax_h3_b99_003_12s",
		Resolution:       "736p竖",
		Resolutions:      []string{"736p竖", "736p横", "736p(1:1)"},
		MaxDuration:      12,
		MaxPromptLength:  10000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          1,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		RequiresImages:   true,
		MaxImages:        9,
	},
	// H3 多图多音频生视频（画质优先）：duration 1-15s；最多 9 张图片、3 条音频；
	// 支持 480p/768p 竖横/(1:1)，seed 最小值为 0。
	"autodl:minimax-h3-u24": {
		WorkflowID:       "minimax_h3_zm_u24",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  10000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          0,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		RequiresImages:   true,
		MaxImages:        9,
		SupportsAudios:   true,
		MaxAudios:        3,
	},
	// H3 多图多音频生视频（高速版）：schema 与画质优先版一致，工作流不同。
	"autodl:minimax-h3-u08": {
		WorkflowID:       "minimax_h3_zm_u08",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"},
		MaxDuration:      15,
		MaxPromptLength:  10000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          0,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		RequiresImages:   true,
		MaxImages:        9,
		SupportsAudios:   true,
		MaxAudios:        3,
	},
	// H3 图像/音频生视频 10 秒版：图片和音频均可选，最多 9 张图片、3 条音频。
	// 480p/768p/1080p 仅支持竖屏和横屏，seed 最小值为 1。
	"autodl:minimax-h3-image-audio-10s": {
		WorkflowID:       "minimax_h3_image_audio_to_video_v2",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "1080p竖", "480p横", "768p横", "1080p横"},
		MaxDuration:      10,
		MaxPromptLength:  10000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3V2ResolutionRatios,
		MinSeed:          1,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		MaxImages:        9,
		SupportsAudios:   true,
		MaxAudios:        3,
	},
	// H3 图像/音频生视频 15 秒版：与 10 秒版的区别是最大时长为 15 秒，且不支持 1080p。
	"autodl:minimax-h3-image-audio-15s": {
		WorkflowID:       "minimax_h3_image_audio_to_video_v2_15s",
		Resolution:       "768p竖",
		Resolutions:      []string{"480p竖", "768p竖", "480p横", "768p横"},
		MaxDuration:      15,
		MaxPromptLength:  10000,
		PromptSupported:  true,
		PromptRequired:   true,
		ResolutionRatios: h3ResolutionRatios,
		MinSeed:          1,
		MaxSeed:          999999999999999,
		SupportsImages:   true,
		MaxImages:        9,
		SupportsAudios:   true,
		MaxAudios:        3,
	},
	// H3 单图音频同步生视频：必须 1 张图片和 1 条音频，使用 audio_duration，自动对口型。
	"autodl:minimax-h3-lipsync": {
		WorkflowID:        "minimax_h3_image_audio_to_video",
		Resolution:        "768p竖",
		Resolutions:       []string{"480p竖", "768p竖", "1080p竖", "480p横", "768p横", "1080p横"},
		MaxDuration:       15,
		PromptSupported:   false,
		PromptRequired:    false,
		SupportsImages:    true,
		RequiresImages:    true,
		MaxImages:         1,
		SupportsAudios:    true,
		RequiresAudios:    true,
		MaxAudios:         1,
		UsesAudioDuration: true,
		ResolutionRatios:  h3ResolutionRatios,
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
