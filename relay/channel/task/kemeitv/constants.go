package kemeitv

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// ChannelName 后台展示名。
const ChannelName = "Kemeitv"

// ModelList 对外暴露的模型 —— **只接这两个**（2026-11-19 用户口径）。
//
// 上游（ai.kemeitv.cn）的万相 wan3.0-video 是 OpenAI 兼容协议，
// 这里只挂 tj（特精）与 prime-tj（特精满血）两个变体，
// 其余模型一律不接 —— 挂了就会在模型广场露出上游没授权的商品名。
var ModelList = []string{
	ModelNameTJ,
	ModelNamePrimeTJ,
}

const (
	// ModelNameTJ 特精档：万相 wan3.0-video 的 tj 变体。
	ModelNameTJ = "wan3.0-video-tj"

	// ModelNamePrimeTJ 特精满血档：wan3.0-video 的 prime tj 变体。
	ModelNamePrimeTJ = "wan3.0-video-prime-tj"
)

// upstreamModelFor 把「对外模型名」映射成上游真正认识的模型名。
//
// 为什么在代码里锁死而不是靠渠道 model_mapping：
//
//	渠道的 model_mapping 是**可配的**，管理员随时可能改掉或漏配；
//	而本适配器下游的计费/规格判定都以「对外模型名」为准，
//	一旦映射漂移就会出现「按 A 的规格计费、按 B 的规格执行」。
//	这是与 wan3 / youzanwan3 一致的做法：语义固定的名字在代码里锁死。
//
// ⚠️ 只认这两个名字，别的一律放行原值：万一将来上游再上新变体，
//
//	让它走「原样透传」比静默改名安全 —— 改名会连带算错价。
func upstreamModelFor(modelName string) string {
	switch strings.TrimSpace(modelName) {
	case ModelNameTJ:
		return upstreamModelTJ
	case ModelNamePrimeTJ:
		return upstreamModelPrimeTJ
	default:
		return strings.TrimSpace(modelName)
	}
}

const (
	// upstreamModelTJ / upstreamModelPrimeTJ 上游模型 ID。
	//
	// ⚠️ 与对外模型名**恰好同名** —— 上游的万相模型就叫 wan3.0-video-tj /
	// wan3.0-video-prime-tj。但仍然要在代码里写死，而不是直接用对外名：
	// 一旦将来渠道 model_mapping 配错（把 tj 映到 wan3.0-video），
	// 显式映射能挡住，避免「买 tj 拿到标准版」的降级。
	upstreamModelTJ      = "wan3.0-video-tj"
	upstreamModelPrimeTJ = "wan3.0-video-prime-tj"
)

// isPrimeModel 判断是不是 prime 档。
//
// 用途：prime 档与标准档的**规格上限不同**（时长上限、参考素材上限），
// 计费基准价也不同，必须分开处理。
func isPrimeModel(modelName string) bool {
	return strings.TrimSpace(modelName) == ModelNamePrimeTJ
}

// 规格常量 —— 取自上游《WAN视频生成》文档（2026-11-19 核对）。
//
// ⚠️ 上游文档对「wan3.0-video」写的时长是 `duration` 默认 5 秒，
//
//	未明确写上限；这里取阿里百炼万相视频的通行档位 [2, 30]，
//	超出本地 400 拒单（上游也会拒，但本地拒单不建单不扣费）。
const (
	// minDuration 最小时长（秒）。
	minDuration = 2

	// maxDuration 最大时长（秒）。
	maxDuration = 30

	// defaultDuration 默认时长（秒）—— 与上游文档的缺省值一致。
	defaultDuration = 5
)

// 参考素材上限 —— 取自上游《WAN视频生成》文档的 OpenAI 兼容参数字典：
//
//   - images：数量决定默认 role（1 张=首帧、2 张=首尾帧、3+ 张=全部参考图）
//   - videos / audios：默认 role 分别是 reference_video / reference_audio
//   - files / links：默认 role 分别是 file / link
//
// ⚠️ 上游文档明确「场景互斥」：首尾帧（first_frame / last_frame）与
// 参考类素材（reference_* / file / link）**不可混用**。混用上游报 400，
// 这里在本地就拦掉 —— 客户端拿到的是明确的本地错误，而不是上游原文。
const (
	// maxReferenceImages 参考图上限（含首帧/尾帧）。
	maxReferenceImages = 10

	// maxReferenceVideos 参考视频段数上限。
	maxReferenceVideos = 5

	// maxReferenceAudios 参考音频段数上限。
	maxReferenceAudios = 5

	// maxReferenceFiles 参考文件数上限。
	maxReferenceFiles = 5

	// maxReferenceLinks 参考网页数上限。
	maxReferenceLinks = 5
)

// 素材允许的文件后缀 —— 取自上游《WAN视频生成》的素材规格表。
//
// ⚠️ 上游是**按后缀**判格式的（2026-11-19 真机实测：传 .m4a 会被拒，
//
//	原文 "format m4a is not supported. Supported formats: ['wav', 'mp3']"）。
//	所以在**本地校验阶段**就要拦下来 ——
//	上游拒单发生在建单之后，钱已经预扣、客户还要等退款。
//
// ⚠️ 音频只收 wav / mp3：m4a / aac / flac 虽是常见音频容器，上游不收。
var (
	// allowedImageExts 参考图允许的后缀。
	allowedImageExts = []string{".jpg", ".jpeg", ".png", ".bmp", ".webp"}

	// allowedVideoExts 参考视频允许的后缀。
	allowedVideoExts = []string{".mp4", ".mov"}

	// allowedAudioExts 参考音频允许的后缀。
	allowedAudioExts = []string{".wav", ".mp3"}
)

// 分辨率档位。
//
// ⚠️ 上游文档写的是 `resolution`（如 "720P"、"1080P"，`size` 会转大写），
//
//	且 Wan3 支持 480P / 720P / 1080P 三档。基准档取 480P（最低档），
//	720P / 1080P 走分辨率倍率（与站内 wan3 系口径一致：1 / 2 / 4）。
const (
	resolution480P  = "480P"
	resolution720P  = "720P"
	resolution1080P = "1080P"

	// defaultResolution 缺省分辨率。
	defaultResolution = resolution720P
)

// resolutionSizeRatio 各分辨率相对基准档（480P）的倍率。
//
// 1 : 2 : 4 —— 与站内 wan3 / youzanwan3 的标准口径一致。
var resolutionSizeRatio = map[string]float64{
	resolution480P:  1.0,
	resolution720P:  2.0,
	resolution1080P: 4.0,
}

// modelResolutionPriceCNY 按「模型 + 分辨率」声明**绝对对外单价（CNY/秒）**。
//
// 背景（2026-11-20 客户调价）：wan3.0-video-tj 的 480P 基准价 ¥0.138→¥0.15、
// wan3.0-video-prime-tj ¥0.24→¥0.25，但 720P / 1080P 的**绝对价必须保持不变**
// （0.25 / 0.552 与 0.48 / 0.96）。基准价存在库里的 ModelPrice（USD 等值），
// 而两台生产服务器的基准价并不相同（新服务器要求维持原价），只要把倍率写死，
// 改任一侧的基准价都会把高档位一起带偏：
//
//	基准 0.138 时 720P 倍率 0.25/0.138 ≈ 1.8116
//	若基准改 0.15 而倍率不动 → 720P 变成 ¥0.2717（客户没让涨）
//
// 所以这里存绝对目标价，运行时按**当前**基准价反推倍率：
//
//	size = 目标价(CNY) / (ModelPrice(USD) × USD2RMB)
//
// 同一份代码下：480P 各取各库里的基准价；720P/1080P 锁死在目标价，
// 与基准价怎么调无关。
//
// 只对显式列出的「模型 + 分辨率」生效；未列出的组合（含 480P）回落到
// resolutionSizeRatio 的标准倍率。
var modelResolutionPriceCNY = map[string]map[string]float64{
	ModelNameTJ: {
		resolution720P:  0.25,
		resolution1080P: 0.552,
	},
	ModelNamePrimeTJ: {
		resolution720P:  0.48,
		resolution1080P: 0.96,
	},
}

// resolutionRatioForModel 返回「模型 + 分辨率」相对基准档（480P）的倍率。
//
// 有绝对价声明且能读到基准价时按绝对价反推；读不到 ModelPrice（未配置 /
// 单测环境）必须回落标准倍率，绝不能返回 0 —— 倍率为 0 等于整单免费。
func resolutionRatioForModel(modelName, resolution string) float64 {
	if targets, ok := modelResolutionPriceCNY[modelName]; ok {
		if targetCNY, ok := targets[resolution]; ok && targetCNY > 0 {
			if baseUSD, ok := ratio_setting.GetModelPrice(modelName, false); ok && baseUSD > 0 {
				baseCNY := baseUSD * ratio_setting.USD2RMB
				if baseCNY > 0 {
					return targetCNY / baseCNY
				}
			}
		}
	}
	return resolutionSizeRatio[resolution]
}

// defaultRatio 缺省画幅。
//
// ⚠️ 不用 adaptive：上游文档的 ratio 取值是「16:9、9:16、1:1 等」，
//
//	没有列 adaptive。缺省取 16:9（横屏），与上游「不传 ratio」的默认一致。
const defaultRatio = "16:9"

// 素材 role 取值 —— 与上游文档的 role 表完全一致。
const (
	roleFirstFrame     = "first_frame"
	roleLastFrame      = "last_frame"
	roleReferenceImage = "reference_image"
	roleReferenceVideo = "reference_video"
	roleReferenceAudio = "reference_audio"
	roleFile           = "file"
	roleLink           = "link"
)

// 协议路径 —— 上游《开放端点一览表》与《WAN视频生成》两处一致。
const (
	// submitPath 异步视频任务提交（OpenAI 兼容）。
	submitPath = "/v1/video/generations"

	// fetchPath 异步视频任务状态查询（OpenAI 兼容）。%s 为任务 ID。
	fetchPath = "/v1/video/generations/%s"
)
