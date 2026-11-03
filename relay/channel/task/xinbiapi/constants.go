package xinbiapi

import (
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

// ChannelName 后台展示名。
const ChannelName = "心笔 Model Router"

// upstreamModel 是本渠道固定使用的上游模型别名。
//
// ⚠️ 在代码里锁死、**不依赖渠道 model_mapping**：这个渠道目前只接 wan3.0-video
// 一个上游模型。映射是管理员可改的，漏配或改错会让上游收到它不认识的模型名；
// 而本渠道的语义是固定的，所以与 wan3 / youzanwan3 的 upstreamModelFor
// 同一套做法，直接在代码里兜底。
const upstreamModel = "wan3.0-video"

// ModelNamePayPerCall 是对外售卖的按次模型名：**原生 720P，固定 30 秒**。
//
// 与 wan3.0-720p-特惠 的区别只在计费口径：特惠按秒（¥0.20/秒），
// 本模型按次（¥4.5/次，与时长无关）。
//
// ⚠️ 商业口径（2026-11-01 定）：对外一律表述为「原生 720P」。
// 交付链路里含有画质增强步骤，那是内部实现细节，不得出现在
// 模型描述、渠道备注、错误信息、接口响应等任何对客/对管理员的文案里。
// 改动本文件或 adaptor 时请保持这个口径。
const ModelNamePayPerCall = "wan3.0-720p-按次"

// ModelList 本渠道对外的模型列表。
//
// ⚠️ seedance2.5-活动（2026-11-03 起）是本渠道的**常驻**对外模型：
// 活动版不再是「有赞常驻 + 30 秒改派心笔」，而是**反过来** ——
// 心笔是活动版的常驻链路，豆包（seedance-2-5）是它的兜底路由。
// 活动版与 ModelNamePayPerCall 是**同一条链路**（同一上游模型、同一档位、
// 同一成本），区别只在对外名称与计费口径（活动版按次 ¥4.50 固定 30 秒）。
var ModelList = []string{
	ModelNamePayPerCall,
	constant.Seedance25HuoDongModel,
}

// generationResolution 是向上游请求的生成档位。
//
// 这是内部实现细节（成本优化），与对外的「原生 720P」表述无关，勿外露。
const generationResolution = "480P"

// deliveredResolution 是对外承诺的交付档位，也是校验客户端传参的唯一合法值。
const deliveredResolution = "720P"

// fixedDuration 是本模型的固定时长（秒）。
//
// 2026-11-01 定稿：**固定 30 秒**，不开放其它档位。
// 理由：按次定价（¥4.5/次）只有在时长固定时才不会出现「长片亏本」——
// 30 秒已经是这一档的成本上限。
//
// 客户端显式传别的时长一律 400（不能静默改成 30 秒：那是「用户以为买的是 5 秒」）。
// 完全不传时按 30 秒处理，并在上游请求里**显式下发 30**，保证实际生成与计费口径一致。
const fixedDuration = 30

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

// needsSuperResolution 判断该对外模型名是否走「交付前画质增强」这一步。
//
// 函数名沿用内部实现术语，对外文案不得使用该说法（见 ModelNamePayPerCall 注释）。
//
// ⚠️ 2026-11-03：seedance2.5-活动 成为本渠道的常驻对外模型后，
// 这个白名单**不能再只认 ModelNamePayPerCall 一个名字** ——
//
//	活动版走心笔时 properties.origin_model_name 记的是「seedance2.5-活动」
//	（对外模型名，不是上游模型名）。白名单漏了它 → 画质增强不触发 →
//	客户拿到 480P 原始片，而对外承诺是 720P。**这是静默降质**：
//	任务照样成功、账单照收 ¥4.50，客户拿到的是低一档的成片。
//
//	所以判据改成「是不是本渠道的对外模型」而不是「是不是某一个名字」：
//	新增对外模型时只要加进 ModelList，增强自动跟上。
func needsSuperResolution(originModelName string) bool {
	name := strings.TrimSpace(originModelName)
	if name == "" {
		return false
	}
	for _, publicModel := range ModelList {
		if name == publicModel {
			return true
		}
	}
	return false
}
