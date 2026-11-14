package middleware

// 规则七：电商/产品推荐内容 → CapCut 快速档（cp-seedance-2.5flash）。
//
// 触发源是**提示词内容**（电商类 / 产品推荐），外加两个可选的参数入口
//（9:16 比例、时长超阈值）。详见 setting/seedanceroute_setting/ecom_route.go
// 的包头注释 —— 那里解释了为什么「内容」与「参数」是「或」而不是「且」。
//
// ── 为什么单独一个文件 ──
//
// 主文件 seedance_route.go 已经有近 2000 行，承载的是规则一~六。
// 这条规则的**契约完全不同**（跨渠道到 CapCut、触发源是内容、准入条件更严），
// 塞进主文件会让那个文件继续膨胀，而且两套准入逻辑互相干扰、难读难测。
// 独立成文件后，规则七的判定与改写可以单独测、单独review。
//
// ── 跨渠道改派的核心约束（CapCut ch20/manwu ← 有赞 ch17/youzanwan3）──
//
// 源链路（有赞 prime）与目标链路（CapCut）**不是同一个 adaptor**，
// 入参契约有实质差异，改派必须逐项对齐，否则就是错交付：
//
//	· 参考图：源链路认 media[] / metadata.media / metadata.input.media /
//	  images 等一串形态；CapCut 认顶层那 14 个别名 + media[]。
//	  **统一抬到顶层 input_reference**（CapCut 的 referenceImageFields 第一位），
//	  并把 media / metadata.media / metadata.input.media **摘掉** ——
//	  留着就是同图投两次（与 rewriteBodyForXinbiPerCall 同一个坑）。
//
//	· 参考视频：源链路收，CapCut 也收（2026-11-14 放开，限 1 个），
//	  但**老板明确要求「有视频参考的不路由」** → 直接挡。
//
//	· 参考音频：CapCut 静默丢弃 → 改派过去等于把客户的音频悄悄扔了 → 挡。
//
//	· 分辨率：源链路支持 480P/720P/1080P，CapCut **只卖 480p/720p**
//	  （1080p 官网有但未定价，显式 400）。1080P 的请求改派过去必 400 → 挡。
//
//	· 时长：源链路 2–30 秒，CapCut 2.5 Flash **4–30 秒**、2.0 Fast **4–15 秒**。
//	  2–3 秒的请求改派过去必被上游拒 → 挡。
//
//	· 比例：源链路的 adaptive 档，CapCut 不认（它的候选是 16:9/9:16/1:1/
//	  4:3/3:4/21:9）。adaptive 要映射到 auto 之外的档位 ——
//	  但 CapCut 没有 auto 档，所以 adaptive 一律挡（客户没指定画幅，
//	  替他挑一个就是替客户改需求）。
//
//	· 参考图上限：源链路 ≤10 张，CapCut ≤30 张 → 源链路上限更严，不挡。
//
// ⚠️ 这份准入逻辑同时被**规则六第三分支**（≤10 秒 → cp-seedance-2.0fast）
// 复用 —— 两条规则的目标在同一条 CapCut 链路（ch20），契约完全一致，
// 只有「时长上界」不同（2.5 Flash 30 秒 / 2.0 Fast 15 秒）。
// 同一套准入逻辑写两遍 = 迟早漂，所以抽成 prepareCapcutRoute。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// capcutMinDuration 是 CapCut 链路的时长下界（秒）。
//
// 与 relay/channel/task/manwu 的 capcutMinDuration 同值（4 秒）。
// 为什么中间件要自己写一份而不能 import 那个包：middleware 依赖 relay 渠道包
// 会把依赖方向绑紧（且容易撞 import 环）。两边同值由测试锁定
// （见 TestCapcutDurationBoundsMatchUpstream）。
const capcutMinDuration = 4

// capcutMaxDuration25 是 CapCut 2.5 系列的时长上界（秒）。
//
// 与 relay/channel/task/manwu 的 capcutMaxDuration25 同值（30 秒）。
const capcutMaxDuration25 = 30

// capcutMaxDurationFast 是 CapCut 2.0 系列的时长上界（秒）。
//
// 与 relay/channel/task/manwu 的 capcutMaxDuration 同值（15 秒）。
// 规则六第三分支的目标（cp-seedance-2.0fast）走这个上界。
const capcutMaxDurationFast = 15

// capcutMaxReferenceImages 是 CapCut 的参考图张数上限。
//
// 与 relay/channel/task/manwu 的 capcutMaxReferenceImages 同值（30 张）。
// 源链路（有赞）只有 10 张上限，所以源链路的请求天然不超过这个数 ——
// 这里仍然写下来做**显式校验**：万一将来源链路放宽到 30 张以上，
// 改派过去会撞 CapCut 的上限，而那时是 400 报错，不是静默丢图。
const capcutMaxReferenceImages = 30

// capcutAllowedRatios 是 CapCut 链路支持的比例档位。
//
// 与 relay/channel/task/manwu 的 allowedRatios 同口径：
// 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9。
//
// ⚠️ **不含 adaptive / auto**：源链路的缺省档是 adaptive（跟随输入图比例），
// CapCut 没有对应档位。客户没指定画幅时替他挑一个 = 替他改需求 → 挡掉
// （除非请求里根本没有参考图 —— 那时 adaptive 的等价画幅是 16:9，
//
//	与 CapCut 自己的缺省一致，见 prepareCapcutRoute 的比例判定）。
var capcutAllowedRatios = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// capcutDefaultRatio 是「源链路没传比例、且请求没有参考图」时使用的画幅。
//
// 源链路的缺省是 adaptive：上游在**没有输入图**时按默认画布出片，
// 实践上就是 16:9。CapCut 的缺省也是 16:9（manwu 的 defaultRatio），
// 所以这里显式写 16:9 —— 与「什么都不写、让目标链路用自己的缺省」等价，
// 但显式写出来更可控（不会因为上游哪天改了缺省就跟着漂）。
const capcutDefaultRatio = "16:9"

// prepareEcommerceRoute 组装「规则七（电商内容 → CapCut 快速档）」的改派请求体。
//
// 返回的第二个值 false = 放弃路由（放行原请求走源链路）。
func prepareEcommerceRoute(c *gin.Context, body map[string]interface{}, targetModel string, seconds int) (map[string]interface{}, bool) {
	return prepareCapcutRoute(c, body, targetModel, seconds, capcutMaxDuration25, "电商规则七")
}

// prepareShortCapcutRoute 组装「规则六第三分支（≤10 秒 → CapCut 快速档）」的改派请求体。
//
// 与规则七共用准入逻辑，唯一差别是时长上界（2.0 Fast 只有 15 秒）。
func prepareShortCapcutRoute(c *gin.Context, body map[string]interface{}, targetModel string, seconds int) (map[string]interface{}, bool) {
	return prepareCapcutRoute(c, body, targetModel, seconds, capcutMaxDurationFast, "短时长CapCut分支")
}

// prepareCapcutRoute 是「改派到 CapCut 链路」的**唯一**准入与改写入口。
//
// label 只进日志，用于区分是哪条规则放行的（电商规则七 / 短时长CapCut分支）。
//
// 准入检查的顺序按**「越便宜的检查越先做」**排列：
// 参考视频/音频 → 首尾帧 → 分辨率 → 时长 → 参考图 → 比例 → 渠道可用性。
// 前面的检查都是纯内存判定（零开销），渠道可用性要走缓存查询放最后。
func prepareCapcutRoute(
	c *gin.Context,
	body map[string]interface{},
	targetModel string,
	seconds int,
	maxDuration int,
	label string,
) (map[string]interface{}, bool) {
	// ① 参考视频：老板明确要求「有视频参考的不路由」。
	//
	// ⚠️ 为什么不能像 CapCut 链路那样「支持参考视频」就放行：
	// CapCut 确实收参考视频（2026-11-14 放开，限 1 个），但：
	//   - 源链路有赞的参考视频有**总量 ≤15 秒 + 生成 ≤15 秒**的硬限制，
	//     CapCut 那边是另一套限制（单条 ≤? 秒，探测口径不同）；
	//   - 老板的口径是明确的「有视频参考的不路由」——
	//     这是**产品决策**（视频参考的成片质量与素材一致性没验证过），
	//     不是能力缺口。能力够不等于产品上要放。
	//
	// 所以这里一律挡，不做「够短就放行」的自作主张。
	//
	// ⚠️ 判定口径覆盖**顶层 videos / video**：源链路（有赞）不消费这两个字段，
	// 但目标链路（CapCut）会消费 —— 源链路忽略、目标链路认，
	// 改派过去就变成「客户按带参考视频下单、拿到一条没有参考视频的成片」。
	// 统一按「任何形态出现视频 = 带参考视频」挡掉，不去猜哪条链路会真的送上去。
	if hasReferenceVideo(body) {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：请求带参考视频（该规则只接纯文生/图生）", label))
		return nil, false
	}

	// ② 参考音频：CapCut 会**静默丢弃**音频。
	//
	// 静默丢 = 客户按「带音频参考」下单却拿到没音频参考的成片（错交付），
	// 且没有任何报错。与其让它悄悄丢，不如不路由。
	if hasUnsupportedMedia(body) {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：请求带参考视频/音频（该链路不支持）", label))
		return nil, false
	}

	// ③ 首尾帧：CapCut 只收参考图，没有首尾帧语义。
	//
	// 源链路早已把首尾帧降级成参考图（上游 2026-10-28 取消入参），
	// 这里挡住是为了不制造「看起来带首尾帧、其实只是参考图」的错交付。
	if hasFrameMedia(body) {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：请求带首尾帧（该链路只收参考图）", label))
		return nil, false
	}

	// ④ 分辨率：CapCut 只卖 480p / 720p，1080P 改派过去必被上游 400。
	//
	// 与规则二（wan3 目标只交付 720P）的处理**不同**：那里是「降级交付」，
	// 这里源链路本来就能给 1080P，改派过去是「直接失败」—— 宁可交回源链路。
	if tier := sourceResolutionTier(body); tier == "1080P" {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：用户要求 1080P，CapCut 只交付 480p/720p", label))
		return nil, false
	}
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		if tier := explicitResolutionTier(metadata["size"]); tier == "1080P" {
			logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：用户要求 1080P，CapCut 只交付 480p/720p", label))
			return nil, false
		}
	}

	// ⑤ 时长：CapCut 的区间是 [4, maxDuration] 秒。
	//
	// 源链路 2–30 秒，所以 2–3 秒的请求改派过去必被上游拒 → 挡。
	// 2.0 Fast 的上界只有 15 秒 —— 规则六只接 ≤10 秒，天然落在里面。
	//
	// ⚠️ 用**触发判定拿到的那份秒数**（seconds），不是重新解析 body ——
	// 规则六/规则一的教训：metadata.seconds 形式的请求，
	// 重新解析会拿到另一个值（缺省 5 秒），改派过去就是「客户要 3 秒拿到 5 秒」。
	if seconds > 0 && seconds < capcutMinDuration {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：时长 %d 秒低于该链路下限 %d 秒", label, seconds, capcutMinDuration))
		return nil, false
	}
	if seconds > maxDuration {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：时长 %d 秒超过该链路上限 %d 秒", label, seconds, maxDuration))
		return nil, false
	}

	// ⑥ 参考图：必须全是公网 http(s) 直链。
	//
	// ⚠️ allowDataURL=false：CapCut（manwu）的 assertHTTPURL 只收 http(s)，
	// data URI 参考图改派过去必 400（error 里会回显整段 base64 ——
	// 那是日志与响应体的双重事故，见 manwu 的 RedactMaterialURL）。
	images, hasUnsupported := collectReferenceImages(body, false)
	if hasUnsupported {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：素材无法安全转换（参考视频/音频或非 http(s) 参考图）", label))
		return nil, false
	}
	if len(images) > capcutMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：参考图 %d 张超过该链路上限 %d 张",
			label, len(images), capcutMaxReferenceImages))
		return nil, false
	}

	// ⑦ 比例：必须在 CapCut 的候选档位里。
	//
	// ⚠️ 客户**没传**比例时源链路用 adaptive（跟随输入图比例）：
	//   - **有参考图** → adaptive 的画幅取决于那张图，我们推不出来 →
	//     替他挑一档就是替他改需求 → 挡；
	//   - **没有参考图** → adaptive 的等价画幅是默认画布 16:9，
	//     与 CapCut 自己的缺省一致 → 落 16:9（与规则二/心笔分流同一套口径：
	//     不给目标链路塞一个没被要求的比例）。
	ratio, hasRatio := resolveRequestRatio(body)
	if !hasRatio {
		if len(images) > 0 {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过%s：请求未指定比例且带参考图，该链路没有自适应档", label))
			return nil, false
		}
		ratio = capcutDefaultRatio
	}
	if !capcutAllowedRatios[ratio] {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：比例 %s 不在该链路支持的档位内", label, ratio))
		return nil, false
	}

	// ⑧ 目标模型没有可用渠道 → 不路由。
	//
	// 与其它规则同一理由：改写 body 之后重试只会用同一个模型名重选渠道，
	// 不会退回源模型。预检能让请求自然落回源链路，客户完全无感。
	if !channelAvailabilityProbe(c, targetModel) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：目标 %s 在当前分组下没有可用渠道", label, targetModel))
		return nil, false
	}

	// 分辨率：源链路的实际档位映射到 CapCut 的档位（480P→480p，其余→720p）。
	//
	// ⚠️ 必须在准入检查**之后**算：准入已经挡掉了 1080P，
	// 所以这里一定落在 480p / 720p 两个上游认的档位里。
	resolution := capcutResolutionFor(body)

	return rewriteBodyForCapcut(body, targetModel, ratio, resolution, seconds), true
}

// sourceModelSizeRatio 返回「源模型口径」的分辨率倍率（有赞 tehui 档）。
//
// 口径来自 youzanwan3.RouteBillingSizeRatio —— 与该 adaptor 的 EstimateBilling
// **完全一致**（同一个 resolutionRatioForModel），所以「动态调价改了」
// 与「这里报的倍率」永远同步，不会出现两套价格。
//
// 为什么必须取源模型的倍率：改派后 adaptor 换成 manwu（CapCut），
// 它给的 size 倍率是 **CapCut 档位差价**（720p ×2.0）——
// 那是目标链路的定价结构，不是客户买的那档。计费按源模型算，
// 就必须用源模型的档位差价，否则 720P 请求会被乘 2 倍（超收 100%）。
func sourceModelSizeRatio(sourceModel, resolution string) float64 {
	return youzanwan3.RouteBillingSizeRatio(sourceModel, resolution)
}

// hasReferenceVideo 判断请求里是否带了参考视频。
//
// 口径分两段：
//
//	① collectReferenceVideos：顶层 media > metadata.media > legacy 图片字段里的
//	  .mp4/.mov（与源链路 buildWan3Media 的推断口径一致）；
//	② 顶层 videos / video 字段：**任何非空值**都算带参考视频。
//
// 为什么 ② 要单独判：collectReferenceVideos 不看顶层 videos/video
// （源链路 youzanwan3 也不消费这两个字段），但**目标链路 CapCut 会消费**
// （manwu 的 resolveCapcutReferenceMaterials 收 req.Videos / req.Video）。
// 源链路忽略、目标链路认 —— 这是两条链路契约不一致的地方，
// 一旦改派过去，客户会拿到一条「源链路根本没跑的参考视频」，
// 与「不带参考视频」的成片一模一样，但客户是按带参考视频下单的。
//
// 所以规则七一律挡：口径统一成「请求里任何形态出现视频 = 带参考视频」，
// 不去猜哪条链路会真的把它送上去。
func hasReferenceVideo(body map[string]interface{}) bool {
	if len(collectReferenceVideos(body)) > 0 {
		return true
	}
	return len(topLevelReferenceVideos(body)) > 0
}

// topLevelReferenceVideos 返回顶层 videos / video 字段里的参考视频地址。
//
// 复用 stringListValue（字符串或字符串数组都收），与 relay/common 的
// TaskSubmitReq 自定义解码口径一致。
func topLevelReferenceVideos(body map[string]interface{}) []string {
	var videos []string
	videos = append(videos, stringListValue(body["videos"])...)
	videos = append(videos, stringListValue(body["video"])...)
	return videos
}

// rewriteBodyForCapcut 组装改派到 CapCut 链路的请求体。
//
// ⚠️ 三处「摘掉」是必须的，漏任何一处都会造成**同图投两次**：
//
//	· metadata：源链路的素材/比例放在 metadata.media 与 metadata.input.media，
//	  CapCut 的 manwu adaptor 也读这两处（parseCapcutMetadataMediaEntries）——
//	  参考图已经抬到顶层 input_reference，再留着 metadata.media 就是同一张图投两次。
//	· 顶层 media：同上，manwu 的 parseCapcutMediaEntries 会收。
//
// 所以这里**整块丢掉 metadata**，只把需要的内容挑出来重写。
//
// 与 rewriteBodyForDoubaoWithDuration 的差别：那条是「有赞 → 豆包」，
// 目标同样只收顶层 input_reference；本条目标也是只收顶层 input_reference，
// 但**多了一个 ratio 显式落字段**与**分辨率钉档**（见下）。
func rewriteBodyForCapcut(body map[string]interface{}, target, ratio, resolution string, seconds int) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+3)
	for key, value := range body {
		switch key {
		case "model", "metadata":
			// model 要换；metadata 整块丢掉（素材已抬到顶层，比例/分辨率已显式落字段）。
			continue
		case "media", "images", "image", "input_reference", "reference_images",
			"reference_image", "image_url", "image_urls", "imageUrl", "imageUrls",
			"referenceImage", "referenceImages", "input_images", "inputImages",
			"inputReference":
			// 素材统一收到顶层 input_reference，不在原位置留副本 ——
			// media / images 漏剥会与 input_reference 重复投递。
			continue
		case "resolution", "size":
			// 分辨率在下面显式落字段；size 在 manwu 那边是**比例**语义 ——
			// 原样透传会被当成比例误读（"1080P" 不在候选里 → 400）。
			continue
		case "audio", "audios", "video", "videos":
			// 参考音视频已在准入检查里挡掉了，这里再摘一次是双保险：
			// 万一将来放宽了准入条件，漏摘会让上游拿到它不认的字段。
			continue
		}
		out[key] = value
	}
	out["model"] = target

	// 参考图：统一收到顶层 input_reference。
	images, _ := collectReferenceImages(body, false)
	if len(images) > 0 {
		out["input_reference"] = images
	}

	// 时长：字符串形态最通用（与规则一一致）。
	out["seconds"] = strconv.Itoa(seconds)

	// 分辨率：CapCut 只卖 480p / 720p，缺省 480p。
	//
	// ⚠️ **必须显式落**：CapCut 的缺省是 480p，而源链路（prime）缺省 720P ——
	// 不显式写就是「客户按 720P 的钱拿到 480p 的片」（静默降档）。
	//
	// 源链路的实际档位口径用 sourceResolutionTier（顶层 resolution →
	// metadata.resolution → 顶层 size），与 adaptor 逐字一致；
	// 解析不出（或客户要 1080P，已在准入挡掉）时钉 720P ——
	// 720P 是源链路的缺省档，且 CapCut 支持。
	out["resolution"] = resolution

	// 比例：显式落到顶层 ratio。
	//
	// manwu 的 resolveRatio 读 metadata.ratio / metadata.aspect_ratio / 顶层 ratio，
	// 优先级是 metadata 优先 —— metadata 已被整块丢掉，所以顶层 ratio 生效。
	// ⚠️ **不能留 size**：manwu 的 SizeFirst=true 契约下 size 优先于 ratio，
	// 而 size 已经被摘掉了，这里不写 size 才是对的。
	out["ratio"] = ratio

	return out
}

// capcutResolutionFor 返回改派到 CapCut 时要下发的分辨率档位。
//
// ⚠️ **恒为 720p**，不做 480P→480p 的映射。
//
// 理由：源链路（有赞 prime）**只提供 720P / 1080P 两档** —— 客户传 480P 时
// prime 按最低档 720P 执行（见 youzanwan3.normalizeResolutionForModel：
// "480P" → primeDefaultResolution = "720P"）。所以源链路交付的永远是 720P
// 或 1080P：
//
//	· 客户要 1080P → 准入已挡（CapCut 不卖 1080p，改派过去必 400）；
//	· 客户要 480P / 没传 → 源链路实际交付 720P。
//
// 改派过去钉 480p 就是**静默降档**：客户按源模型（720P 价）付费，
// 拿到的是 480p 的片。这与规则六心笔路径「480P 生成 + 超分 720P」的口径
// 一致 —— 改派不能降档。
//
// ⚠️ 另一个必须显式落的理由：CapCut 的缺省是 480p，源链路的缺省是 720P。
// 不显式写就是「客户按 720P 的钱拿到 480p 的片」。
func capcutResolutionFor(body map[string]interface{}) string {
	// body 参数保留是为了与调用点的签名一致（并留出将来分档的空间），
	// 但当前口径下**恒定 720p** —— 1080P 已在准入阶段挡掉。
	_ = body
	return "720p"
}

// ecommerceRouteTriggered 判断请求是否命中规则七的触发条件。
//
// 命中 = 内容命中（电商/产品推荐关键词）**或** 参数命中（勾选的条件项）。
//
// 四个返回值：
//
//	triggered        = 是否命中；
//	reason           = 命中原因（给日志看，排查「为什么这条走了那条没走」）；
//	contentMatched   = 是否是内容命中（内容命中与参数命中的运营含义不同：
//	                    内容命中说明「这是电商内容」，参数命中说明「这是竖屏/长视频」）。
func ecommerceRouteTriggered(body map[string]interface{}, policy seedanceroute_setting.Policy, seconds int) (bool, string, bool) {
	// ① 内容命中：提示词命中电商/产品推荐关键词。
	if prompt := requestPrompt(body); prompt != "" {
		if seedanceroute_setting.MatchesEcommercePrompt(prompt, policy.EcommerceKeywords()) {
			return true, "提示词命中电商/产品推荐关键词", true
		}
	}

	// ② 参数命中：勾选的条件项里任意一项命中即可。
	//
	// ⚠️ 「勾选」才是开关：运营没勾「9:16」这一项时，
	// 即使请求是 9:16 也不该走规则七 —— 否则这个配置项形同虚设，
	// 运营会以为「没勾 = 不生效」而实际一直生效。
	if policy.EcommerceRatioEnabled() {
		if matchesTriggerRatio(body, seedanceroute_setting.DefaultEcommerceRatioCondition) {
			return true, fmt.Sprintf("命中 %s 比例条件", seedanceroute_setting.DefaultEcommerceRatioCondition), false
		}
	}
	if policy.EcommerceSecondsEnabled() {
		if seconds > policy.EcommerceTriggerSeconds() {
			return true, fmt.Sprintf("命中时长条件（%d 秒 > %d 秒）", seconds, policy.EcommerceTriggerSeconds()), false
		}
	}

	return false, "", false
}

// requestPrompt 从请求体里取提示词。
//
// 与源链路（youzanwan3）读的是同一个字段（顶层 prompt）。
func requestPrompt(body map[string]interface{}) string {
	prompt, _ := body["prompt"].(string)
	return strings.TrimSpace(prompt)
}

// setRouteBillingContext 记下改派后的计费口径（源模型口径的秒数与 size 倍率）。
//
// 改派不改变对客价格：客户买的是源模型（seedance2.5-特惠 / -活动），
// 账单按源模型的 ModelPrice 算。所以这里记的是**源模型口径**：
//
//	· seconds：触发判定拿到的那份秒数（客户请求的秒数）；
//	· size：源模型口径的分辨率倍率（有赞 tehui 档 720P=1.0 / 1080P=1.5）。
//
// ⚠️ CapCut adaptor 自己会给 size 倍率（480p=1.0 / 720p=×2.0），
// 那是**CapCut 档位差价**，与源模型的档位差价不是同一个维度。
// 不覆盖的话，客户按源模型 720P 付费却按 CapCut 720p（×2.0）结算 ——
// 那是 2 倍超收。applyRouteBillingSizeRatio 会用这里记的值覆盖它。
func setRouteBillingContext(c *gin.Context, seconds int, sizeRatio float64) {
	if seconds > 0 {
		common.SetContextKey(c, constant.ContextKeyRouteBillingSeconds, float64(seconds))
	}
	if sizeRatio > 0 {
		common.SetContextKey(c, constant.ContextKeyRouteBillingSizeRatio, sizeRatio)
	}
}
