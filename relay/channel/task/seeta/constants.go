package seeta

// ChannelName 后台展示名
const ChannelName = "SeetaCloud"

// ModelList 对外暴露的模型（SeetaCloud 自建 ComfyUI 实例，ArcReel-H3 网关）。
//
// 2026-11-08：FastH3 V2 退役下架，Omni 完全覆盖其文生/图生/首尾帧能力，
// 并额外支持 9 图 + 3 视频 + 3 音频参考与 1080p。因此对外只留 omni。
//
// ⚠️ workflowByModel 里 **保留** v2 与两个旧中文名的映射：在途任务仍要能
// 路由/查询，紧急回滚时也只需恢复这里的列表（网关侧另有
// ARCREEL_ENABLE_V2_MODEL 开关），不必重建工作流配置。
var ModelList = []string{
	"minimax_h3_omni",
}

// 上游工作流名（对外不可见）。
const (
	// workflowV2 FastH3 V2 8 步蒸馏工作流：文生 / 图生（首帧）/ 首尾帧三合一。
	workflowV2 = "minimax_h3_v2"
	// workflowOmni FastH3-Omni ref2va 工作流：多模态参考（设定参考 9 图 +
	// 3 视频 + 3 音频，可与首帧/尾帧锚定同请求共存）。
	workflowOmni = "minimax_h3_omni"
)

// ReferenceMode 参考图的语义（同名 images 字段在不同工作流里含义不同）：
//
//	"keyframes" — 参考图是**关键帧**（第 1 张=首帧，第 2 张=尾帧），
//	              上游 V2 蒸馏权重只有这两个槽位，硬上限 2 张。
//	"setref"    — 参考图是**设定参考**（ref_blocks 通路，最多 9 张）；
//	              首帧/尾帧通过请求 metadata 的 first_frame/last_frame 单独传，
//	              可与设定参考同请求共存（Omni 契约）。
const (
	referenceModeKeyframes = "keyframes"
	referenceModeSetRef    = "setref"
)

// workflowConfig 每个对外模型对应的上游工作流配置。
type workflowConfig struct {
	// WorkflowID 是上游工作流名，拼进
	// POST /api/v1/comfyui/comfyui_workflow/{WorkflowID}。
	WorkflowID string
	// Resolution 是未指定分辨率时使用的默认档位。
	Resolution string
	// Resolutions 是该工作流实际允许的分辨率枚举值。
	//
	// ⚠️ 上游只认「档位+方向」写法（480p横 / 768p竖 / 1080p(1:1)），
	// 裸 "480p" 虽然也返回 200，但那是服务端兜底，不是文档契约，
	// 所以白名单只放带方向后缀的完整 token。
	// 1080p 档只有 Omni 链支持（V2 没有潜空间放大路径，传了必须报错）。
	Resolutions []string
	// MaxDuration 该工作流允许的最大时长（秒）；上游硬上限 15 秒。
	MaxDuration int
	// MaxPromptLength 是上游允许的 prompt 最大字符数。
	MaxPromptLength int
	// RequiresImages 是否必须传参考图。
	RequiresImages bool
	// ReferenceMode 参考图语义（keyframes / setref，见上方常量注释）。
	ReferenceMode string
	// MaxImages 参考图数量上限（keyframes 模式 = 2 关键帧槽位；
	// setref 模式 = 9 设定参考槽位，与上游 ref_image_0..8 对齐）。
	MaxImages int
	// MaxVideos 参考视频上限（Omni 3 个；V2 不支持 = 0，传了要报错）。
	MaxVideos int
	// MaxAudios 参考音频上限（Omni 3 个；V2 不支持 = 0）。
	MaxAudios int
	// SupportsSeed 是否接受 seed 参数。
	SupportsSeed bool
	// ResolutionRatios 是相对于 480P 基础价的计费倍率。
	ResolutionRatios map[string]float64
}

// v2ResolutionRatios 是 FastH3 V2 链的分辨率计费倍率（相对 480P 基准价 ¥0.03/秒）：
//
//	480P  → ¥0.03/秒（基准，ratio 1.0）
//	768p  → ¥0.05/秒（720P 档，ratio 1.667）
//
// V2 没有 1080p 通路（无潜空间放大），所以这里不设 1080p 键。
//
// 注意上游的 768p 实际交付分辨率是 832×480 起步的 720P 档，
// 对客按「720p 档」称，与价格表里的 720P 档位对齐。
var v2ResolutionRatios = map[string]float64{
	"480p横":     1.0,
	"480p竖":     1.0,
	"480p(1:1)": 1.0,
	"768p横":     1.667,
	"768p竖":     1.667,
	"768p(1:1)": 1.667,
}

// omniResolutionRatios 是 FastH3-Omni 链的分辨率计费倍率（相对 480P 基准价 ¥0.02/秒）：
//
//	480P  → ¥0.02/秒（基准，ratio 1.0）
//	768p  → ¥0.04/秒（720P 档，ratio 2.0）
//	1080p → ¥0.06/秒（ratio 3.0，上游是 768p 采样 + 潜空间放大）
//
// ⚠️ Omni 的基准价**与 V2 不同**（¥0.02 vs ¥0.03），倍率也各自一套 ——
// 2026-11-08 客户把 Omni 的 480P/768P 下调、1080P 保持原价，等价于把
// 768p 倍率从 1.667 抬到 2.0、1080p 从 2.0 抬到 3.0。两边必须同时改：
// 只改基准价会按旧倍率算出 ¥0.0333 / ¥0.04（少收），只改倍率会按旧基准
// 算出 ¥0.06 / ¥0.09（多收）。基准价在 ratio_setting.defaultModelPrice
// 与生产 ModelPrice 里，倍率只在这里。
var omniResolutionRatios = map[string]float64{
	"480p横":      1.0,
	"480p竖":      1.0,
	"480p(1:1)":  1.0,
	"768p横":      2.0,
	"768p竖":      2.0,
	"768p(1:1)":  2.0,
	"1080p横":     3.0,
	"1080p竖":     3.0,
	"1080p(1:1)": 3.0,
}

var (
	// v2Resolutions V2 链允许的分辨率档（无 1080p）。
	v2Resolutions = []string{
		"480p横", "480p竖", "480p(1:1)",
		"768p横", "768p竖", "768p(1:1)",
	}
	// omniResolutions Omni 链允许的分辨率档（含 1080p 潜空间放大档）。
	omniResolutions = append(append([]string{}, v2Resolutions...),
		"1080p横", "1080p竖", "1080p(1:1)")
)

// workflowByModel 对外模型 → 上游工作流配置。
//
// 旧名（minimax_h3-文生 / minimax_h3-图生）是合并前的对外模型：保留映射让
// 老请求继续路由——文生别名 = V2 全语义（合并后不带图即文生，带图也不再拒）；
// 图生别名保留「必须至少 1 张参考图」的旧契约。
var workflowByModel = map[string]workflowConfig{
	// FastH3 V2：不带图=纯文生视频；1 张=首帧图生；2 张=首尾帧驱动。
	"minimax_h3_v2": {
		WorkflowID:       workflowV2,
		Resolution:       "480p横",
		Resolutions:      v2Resolutions,
		MaxDuration:      15,
		MaxPromptLength:  200000,
		ReferenceMode:    referenceModeKeyframes,
		MaxImages:        2,
		MaxVideos:        0,
		MaxAudios:        0,
		SupportsSeed:     true,
		ResolutionRatios: v2ResolutionRatios,
	},
	// FastH3-Omni：文生/图生/首尾帧 + 设定参考（9 图 + 3 视频 + 3 音频）+ 1080p。
	"minimax_h3_omni": {
		WorkflowID:       workflowOmni,
		Resolution:       "480p横",
		Resolutions:      omniResolutions,
		MaxDuration:      15,
		MaxPromptLength:  200000,
		ReferenceMode:    referenceModeSetRef,
		MaxImages:        9,
		MaxVideos:        3,
		MaxAudios:        3,
		SupportsSeed:     true,
		ResolutionRatios: omniResolutionRatios,
	},
	// —— 旧对外名（不再出现在 ModelList，仅作路由别名）——
	"minimax_h3-文生": {
		WorkflowID:       workflowV2,
		Resolution:       "480p横",
		Resolutions:      v2Resolutions,
		MaxDuration:      15,
		MaxPromptLength:  200000,
		ReferenceMode:    referenceModeKeyframes,
		MaxImages:        2,
		SupportsSeed:     true,
		ResolutionRatios: v2ResolutionRatios,
	},
	"minimax_h3-图生": {
		WorkflowID:       workflowV2,
		Resolution:       "480p横",
		Resolutions:      v2Resolutions,
		MaxDuration:      15,
		MaxPromptLength:  200000,
		RequiresImages:   true,
		ReferenceMode:    referenceModeKeyframes,
		MaxImages:        2,
		SupportsSeed:     true,
		ResolutionRatios: v2ResolutionRatios,
	},
}

const (
	// baseURLDefault SeetaCloud 实例默认地址；渠道 base_url 为空时兜底。
	baseURLDefault = "https://u1133789-a936-c07acd4f.westd.seetacloud.com:8443"

	// 对外计价（2026-11-08 起）：
	//   minimax_h3_v2   480P ¥0.03/秒、768p（720P 档）¥0.05/秒
	//   minimax_h3_omni 480P ¥0.02/秒、768p ¥0.04/秒、1080p ¥0.06/秒
	// 两条链的**基准价不同**，所以「基准价」不是一个常量，而是每个模型一条：
	// ratio_setting.defaultModelPrice（代码默认）或生产 ModelPrice（优先）。
	// 这个常量只代表 V2 的 480P 基准价，给测试和旧文档核对用，不要拿它算 Omni。
	//
	// ⚠️ Omni 带参考视频时计费秒数 = 生成秒数 + 参考视频总秒数（逐条实测时长
	// 四舍五入后相加），再乘档位倍率；参考图片/音频不额外计费，V2 不收参考视频。
	// 实现见 refvideo.go / adaptor.go（ValidateRequestAndSetAction + EstimateBilling）。
	v2PricePerSecondCNY = 0.03

	minDuration     = 1
	maxDuration     = 15
	defaultDuration = 5

	// maxReferenceImageBytes 单张参考图（图片）的最大字节数。
	// 上游只收 data URI，素材要随请求体一起送过去，所以必须在本地
	// 先拦体积，否则一个 20MB 的图会把网关内存打爆。
	maxReferenceImageBytes = 10 * 1024 * 1024

	// maxReferenceVideoBytes 单个参考视频的最大字节数（Omni 链）。
	// 参考片段通常几秒到十几秒，50MB 足够；base64 后线上体积 ×4/3，
	// 再大就该让客户自己剪短。
	maxReferenceVideoBytes = 50 * 1024 * 1024

	// maxReferenceAudioBytes 单个参考音频的最大字节数（Omni 链）。
	maxReferenceAudioBytes = 30 * 1024 * 1024

	// maxReferenceImageCount 设定参考模式下单次最多参考图数量，与上游
	// ref_image_N 槽位（0..8）对齐。
	maxReferenceImageCount = 9

	// maxUpstreamKeyframes 关键帧（首尾帧）模式的上游硬上限：首帧 + 尾帧 = 2 张。
	//
	// 上游 /v1/models 的描述写明「最多 2 张参考图——这是 V2 蒸馏权重的关键帧
	// 槽位上限，不是配置项」。实测（2026-11-06）传 3 张上游照收照出片，
	// 但第 3 张完全没有生效 —— 上游不报错只是默默忽略。
	// 所以这个上限必须由网关自己兜住，不能等上游报错。
	maxUpstreamKeyframes = 2
)
