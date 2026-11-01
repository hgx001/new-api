package xinbiapi

// ChannelName 后台展示名。
const ChannelName = "心笔 Model Router"

// upstreamModel 是本渠道固定使用的上游模型别名。
//
// ⚠️ 在代码里锁死、**不依赖渠道 model_mapping**：这个渠道目前只接 wan3.0-video
// 一个上游模型（480P 档）。映射是管理员可改的，漏配或改错会让上游收到
// 它不认识的模型名；而本渠道的语义是固定的，所以与 wan3 / youzanwan3 的
// upstreamModelFor 同一套做法，直接在代码里兜底。
const upstreamModel = "wan3.0-video"

// ModelNamePayPerCall 是对外售卖的按次模型名（480P 生成 → 提升 720P 交付）。
//
// 与 wan3.0-720p-特惠 的区别只在计费口径：特惠按秒（¥0.20/秒），
// 本模型按次（¥4.5/次，与时长无关）。链路完全相同：
// 上游只出 480P，成片交给腾讯 MPS 做 2 倍提升，最终交付 720P。
const ModelNamePayPerCall = "wan3.0-720p-按次"

// ModelList 本渠道对外的模型列表。
var ModelList = []string{
	ModelNamePayPerCall,
}

// generationResolution 是向上游请求的生成档位。
//
// 固定 480P：先用便宜的 480P 生成，再提升到 720P。
// 上游 480P ¥0.135/秒、720P ¥0.27/秒，两跳（480P + 提升 ¥0.0067/秒）明显更便宜。
const generationResolution = "480P"

// deliveredResolution 是对外承诺、也是超分的目标档位。
const deliveredResolution = "720P"

// durations 允许的时长区间（含端点）。
//
// 上游 parameterSchema 对 wan3.0-video 声明的是 2–30 秒（兼容口本身只做
// 1–120 的粗校验，真正的档位约束在模型合同里）。这里按合同在本地拦住，
// 避免把一个上游一定会拒的请求先建单再失败。
const (
	minDuration = 2
	maxDuration = 30
)

// defaultDuration 客户端没给时长时的默认值。
const defaultDuration = 5

// ratios 上游 parameterSchema 声明的画面比例集合。
// 注意自适应要传 adaptive，不是 auto。
var ratios = map[string]bool{
	"adaptive": true,
	"16:9":     true,
	"9:16":     true,
	"1:1":      true,
	"4:3":      true,
	"3:4":      true,
}

// defaultRatio 与上游 schema 一致：不传即 adaptive。
const defaultRatio = "adaptive"

// maxReferenceImages 参考图张数上限（上游 mediaConstraints：reference_image ≤ 9）。
const maxReferenceImages = 9

// needsSuperResolution 判断该对外模型名是否走「480P 生成 → 提升 720P」两跳链路。
func needsSuperResolution(originModelName string) bool {
	return originModelName == ModelNamePayPerCall
}
