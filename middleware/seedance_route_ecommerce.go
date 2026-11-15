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
//	· 参考视频：源链路收，CapCut 也收 → **透传**（2026-11-15 放开）。
//	  改派时抬到顶层 `videos`；条数上限 5 个、合计时长 ≤15 秒由目标链路的
//	  adaptor 校验（网关这里不重复判，判两遍只会让两处口径漂）。
//
//	· 参考音频：CapCut 旧实现静默丢弃音频，所以旧口径是「带音频不路由」。
//	  现在音频会下发到 Worker 并上传到 CapCut 素材槽 → **透传**（2026-11-15），
//	  抬到顶层 `audios`；上限 5 个。
//
//	⚠️ 音视频是**改写责任**，不是「删掉拦截就行」：rewriteBodyForCapcut
//	  会整块摘掉 metadata 与 media（防止同一素材投两次），素材不显式抬到
//	  顶层就是静默丢 —— 比「不路由」更坏，因为客户看不到任何报错。
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
// 与**规则八**（全量兜底 → cp-seedance-2.5flash）复用 —— 三条规则的目标都在
// 同一条 CapCut 链路（ch20），契约完全一致，只有「时长上界」与「触发条件」不同
// （2.5 Flash 30 秒 / 2.0 Fast 15 秒；规则六/七有条件、规则八无条件）。
// 同一套准入逻辑写三遍 = 迟早漂，所以抽成 prepareCapcutRoute。

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

// prepareTotalFallbackRoute 组装「规则八（全量兜底 → CapCut 2.5 Flash）」的改派请求体。
//
// 与规则七共用 prepareCapcutRoute 的**全部**准入与改写：目标链路是同一条，
// 它的契约限制不会因为「这次是应急改派」就消失。
//
// 与规则七的唯一差别：**没有触发条件**（规则七要命中电商关键词或勾选的条件），
// 规则八是「开就全量」。所以这里连 seconds 都由调用方按缺省补齐后传入 ——
// 规则七能保证 hasSeconds，规则八不能（客户完全不传时长是常见形态）。
func prepareTotalFallbackRoute(c *gin.Context, body map[string]interface{}, targetModel string, seconds int) (map[string]interface{}, bool) {
	return prepareCapcutRoute(c, body, targetModel, seconds, capcutMaxDuration25, "全量兜底规则八")
}

// prepareCapcutRoute 是「改派到 CapCut 链路」的**唯一**准入与改写入口。
//
// label 只进日志，用于区分是哪条规则放行的（电商规则七 / 短时长CapCut分支）。
//
// 准入检查的顺序按**「越便宜的检查越先做」**排列：
// 首尾帧 → 分辨率 → 时长 → 素材形态 → 参考图张数 → 比例 → 渠道可用性。
// 前面的检查都是纯内存判定（零开销），渠道可用性要走缓存查询放最后。
//
// ⚠️ 参考视频 / 参考音频**不再是一道准入检查**（2026-11-15 放开），
// 但它们仍然是「素材形态」的一部分 —— 见 ⑤。
func prepareCapcutRoute(
	c *gin.Context,
	body map[string]interface{},
	targetModel string,
	seconds int,
	maxDuration int,
	label string,
) (map[string]interface{}, bool) {
	// ① 参考视频 / 参考音频：**照常路由**（2026-11-15 放开）。
	//
	// 旧口径（2026-10-30 ~ 2026-11-14）是「带音视频一律不路由」，两条理由现在
	// 都不成立了：
	//   - 视频：当初的理由是「视频参考的成片质量与素材一致性没验证过」——
	//     那是产品担忧而非能力缺口。2026-11-14 真机标定确认 CapCut 多模态参考
	//     模式接受视频，2026-11-15 决定放开；
	//   - 音频：当初的理由是「CapCut 会静默丢弃音频」——那是旧实现的事实。
	//     new-api 的 capcut 分支现在会把音频下发到 Worker（adaptor 的
	//     InputTypeAudio → inputs.reference_audio），转发链路已经通了。
	//
	// 继续挡不再产生任何保护效果，只会把客户留在源链路上白等。
	//
	// ⚠️ 「不挡」只是第一步，不是全部：素材必须在 rewriteBodyForCapcut 里被
	// **抬到顶层** videos / audios。只删这两段拦截而不改改写函数，音视频会在
	// 摘 metadata/media 时被一起丢掉 —— 那是比「不路由」更坏的静默丢素材：
	// 客户按「带音频」下单，拿到一条既没有音频也没有任何告警的成片。

	// ② 首尾帧：CapCut 只收参考图，没有首尾帧语义。
	//
	// 源链路早已把首尾帧降级成参考图（上游 2026-10-28 取消入参），
	// 这里挡住是为了不制造「看起来带首尾帧、其实只是参考图」的错交付。
	if hasFrameMedia(body) {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过%s：请求带首尾帧（该链路只收参考图）", label))
		return nil, false
	}

	// ③ 分辨率：CapCut 只卖 480p / 720p，1080P 改派过去必被上游 400。
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

	// ④ 时长：CapCut 的区间是 [4, maxDuration] 秒。
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

	// ⑤ 素材：三类分别收集，**都必须全是公网 http(s) 直链**。
	//
	// ⚠️ allowDataURL=false：CapCut（manwu）的 assertHTTPURL 只收 http(s)，
	// data URI 素材改派过去必 400（error 里会回显整段 base64 ——
	// 那是日志与响应体的双重事故，见 manwu 的 RedactMaterialURL）。
	//
	// ⚠️ tolerateAV=true：这里是**唯一**允许「请求里有音视频但图片收集照常
	// 返回」的调用点。音视频由下面两个函数单独收，混进 collectReferenceImages
	// 会让 mp4 落进 input_reference（CapCut 那边按图片解码 → 直接失败）。
	images, hasUnsupported := collectReferenceImagesInternal(body, false, true)
	videos := collectRouteReferenceVideos(body)
	audios := collectRouteReferenceAudios(body)
	if hasUnsupported || !allHTTPURL(videos) || !allHTTPURL(audios) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：素材无法安全转换（参考音视频或参考图不是 http(s) 直链）", label))
		return nil, false
	}
	if len(images) > capcutMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过%s：参考图 %d 张超过该链路上限 %d 张",
			label, len(images), capcutMaxReferenceImages))
		return nil, false
	}
	// 条数上限与 adaptor 同口径（视频/音频各 5）。超限**不在这里挡** ——
	// adaptor 会显式 400 并给出准确条数，比这里「悄悄留在源链路」更容易让
	// 客户发现是自己传多了。这里只做日志留痕，方便排查「为什么改派后报 400」。
	if maxRefVideos := 5; len(videos) > maxRefVideos {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由提示%s：参考视频 %d 个超过该链路上限 %d 个（交由 adaptor 显式拒绝）",
			label, len(videos), maxRefVideos))
	}
	if maxRefAudios := 5; len(audios) > maxRefAudios {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由提示%s：参考音频 %d 个超过该链路上限 %d 个（交由 adaptor 显式拒绝）",
			label, len(audios), maxRefAudios))
	}

	// ⑥ 比例：必须在 CapCut 的候选档位里。
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

	// ⑦ 目标模型没有可用渠道 → 不路由。
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

// hasReferenceVideo 已删除（2026-11-15 放开音视频准入）。
//
// 它原本用来回答「要不要因为这个请求带视频而拒绝改派」。现在不拒了，
// 「带不带视频」变成「怎么把视频透传下去」的问题，答案在
// collectRouteReferenceVideos / rewriteBodyForCapcut 里。
//
// ⚠️ 保留这段说明是因为它记录的**契约差异**依然成立且重要：
// 源链路（有赞 youzanwan3）**不消费**顶层 videos / video 字段，
// 而目标链路（CapCut manwu）**会消费**（resolveCapcutReferenceMaterials）。
// 所以任何「从源链路改派到 CapCut」的路径都必须显式处理这两个字段 ——
// 忽略它们不会再造成错交付（因为现在会透传），但漏抬会造成**丢素材**。

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

// collectRouteReferenceVideos 收集改派到 CapCut 时要下发的**全部**参考视频。
//
// 三个来源合并去重（缺一个就是静默丢素材）：
//
//	① media / metadata.media / legacy 图片字段里的 .mp4（collectReferenceVideos，
//	   与源链路 buildWan3Media 的推断口径一致）；
//	② 顶层 videos / video（源链路不消费，但 CapCut 会消费）；
//	③ 顶层 input_reference 里的 .mp4（legacyReferenceVideos 已覆盖）。
//
// 去重是必须的：客户端把同一条视频同时放 media 和 videos 是常见形态
// （「保险起见两边都写」），不去重就是同一条视频占两个槽位，而 CapCut 的
// 槽位是有上限的 —— 会挤掉真正的新素材。
func collectRouteReferenceVideos(body map[string]interface{}) []string {
	videos := collectReferenceVideos(body)
	videos = append(videos, topLevelReferenceVideos(body)...)
	return dedupeURLs(videos)
}

// collectRouteReferenceAudios 收集改派到 CapCut 时要下发的**全部**参考音频。
//
// 与视频不同，音频**没有 legacy 推断**：源链路（有赞）根本不消费音频字段，
// 不存在「图片字段里放了 mp3 按扩展名推断」这种历史形态 ——
// 硬加推断只会把某些奇怪命名的图片误判成音频。
//
// 扫的落点与 CapCut adaptor 的 resolveCapcutReferenceMaterials 严格对齐
// （media / metadata.media / metadata.input.media / 顶层 audios / audio），
// 少读一处 = 「准入说有音频、改写后没了」的静默丢素材。
func collectRouteReferenceAudios(body map[string]interface{}) []string {
	var audios []string
	audios = append(audios, referenceAudiosFromMedia(body["media"])...)
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		audios = append(audios, referenceAudiosFromMedia(metadata["media"])...)
		if input, ok := metadata["input"].(map[string]interface{}); ok {
			audios = append(audios, referenceAudiosFromMedia(input["media"])...)
		}
	}
	audios = append(audios, stringListValue(body["audios"])...)
	audios = append(audios, stringListValue(body["audio"])...)
	return dedupeURLs(audios)
}

// referenceAudiosFromMedia 从 media 数组里挑出 reference_audio 的 url。
func referenceAudiosFromMedia(value interface{}) []string {
	media, ok := value.([]interface{})
	if !ok {
		return nil
	}
	var audios []string
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := entry["type"].(string)
		if strings.TrimSpace(mediaType) != "reference_audio" {
			continue
		}
		if url, ok := entry["url"].(string); ok && strings.TrimSpace(url) != "" {
			audios = append(audios, strings.TrimSpace(url))
		}
	}
	return audios
}

// dedupeURLs 按首次出现顺序去重（保序，槽位顺序不能乱）。
func dedupeURLs(urls []string) []string {
	if len(urls) <= 1 {
		return urls
	}
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, url := range urls {
		if url == "" {
			continue
		}
		if _, ok := seen[url]; ok {
			continue
		}
		seen[url] = struct{}{}
		out = append(out, url)
	}
	return out
}

// allHTTPURL 判断一组 URL 是否全为公网 http(s) 直链。
//
// CapCut（manwu）的 assertHTTPURL 只收 http(s)：data URI / 本地路径改派过去必 400，
// 而 400 的响应体会回显整段 base64（日志与响应体的双重事故）。
// 所以形态不合格时放弃改派，让请求自然留在源链路 —— 与参考图同一处理。
func allHTTPURL(urls []string) bool {
	for _, url := range urls {
		if !isHTTPURL(url) {
			return false
		}
	}
	return true
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
			// 统一在函数末尾**重写回顶层**（videos / audios 复数形式）。
			//
			// 这里 continue 是必须的：单数键 audio / video 与复数键混在一起
			// 透传会让 manwu 的 resolveCapcutReferenceMaterials 读到两份
			// （它同时读单复数）→ 同一条素材占两个槽位。
			continue
		}
		out[key] = value
	}
	out["model"] = target

	// 参考图：统一收到顶层 input_reference。
	//
	// ⚠️ tolerateAV=true 与准入阶段同一口径：false 时带视频的请求会拿到
	// (nil, true)，图片被整个丢掉 —— 改成「挡掉视频」之后这里就成了
	// 「带视频 = 连图片一起丢」的静默丢图。
	images, _ := collectReferenceImagesInternal(body, false, true)
	if len(images) > 0 {
		out["input_reference"] = images
	}

	// 参考视频 / 参考音频：抬到顶层（2026-11-15 放开）。
	//
	// 键名固定用**复数**（videos / audios）：单数键已在 switch 里 continue 掉，
	// 只留复数就不会被 manwu 读成两份。
	if videos := collectRouteReferenceVideos(body); len(videos) > 0 {
		out["videos"] = videos
	}
	if audios := collectRouteReferenceAudios(body); len(audios) > 0 {
		out["audios"] = audios
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
