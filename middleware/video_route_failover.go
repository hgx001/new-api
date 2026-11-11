package middleware

// 规则五：故障转移（failover）——「源链路不可用时改派到备胎模型」。
//
// 与另外三条规则的根本区别：
//
//	前三条（时长/比例/首尾帧）：触发源是**请求特征**，
//	    源模型是促销族（seedance2.5-特惠 / -活动），目标是别的链路。
//	规则五（故障转移）：触发源是**源链路可用性**，
//	    源模型是 seedance-2-5（豆包官网链路），备胎是 wan3.0-班车。
//
// 为什么单独一个文件：seedance_route.go 已经很长，再往里塞会让
// 「规则五的语义」淹没在特征规则里。这个文件只讲一件事 ——
// 「源链路挂了怎么办」，读它的人不用先理解促销族的三条特征规则。
//
// ───────────────────────── 源链路「不可用」的两种形态 ─────────────────────────
//
//	静态（渠道状态）：源模型在当前分组下没有**启用中**的渠道。
//	    渠道被 AutoBan 自动下线、或被管理员手工关闭，都会落到这个形态。
//	    渠道一恢复就自动恢复 —— 不需要任何状态位，也不会「恢复不了」。
//
//	动态（额度探针）：渠道还活着，但上游额度用完了（上游只在任务终态
//	    回一句「今日额度已用完」）。这种失败**不会**下线渠道，
//	    所以静态探测看不出来，必须靠探针把「不可用」这个事实记下来。
//	    探针是事件驱动的，只在任务失败那一刻跑一次；没有时间戳的话，
//	    下一个请求进来时中间件不知道源链路已经坏了，会再送过去失败一次
//	    （客户白等一轮 + 白烧一次预扣）。
//
// 两种形态**都算不可用**，取并集。只认其中一种的话：渠道真被关了但探针没跑过时
// 会继续撞坏渠道；额度用完但渠道没被下线时会继续白烧预扣。
//
// ───────────────────────── 改派原则：只搬运源链路「真正生效」的参数 ─────────────────────────
//
// 这是本文件最容易写错、后果最隐蔽的一层。
//
// 故障转移**不是**「换个模型名重发一遍」，而是「把源链路**实际会用的**那部分
// 参数，原样搬到备胎链路」。两者如果对不齐，就会出现两种坏结果：
//
//	搬多了：客户没要的东西被送上去 → 备胎链路的素材上限/时长上限触发，
//	        请求失败，而失败原因指向一个客户根本没碰过的维度。
//	搬少了：客户要的东西没带过去 → 静默丢图 / 静默改比例 / 静默改时长。
//
// 所以这里**不复用**通用的 collectReferenceImages / extractRatio /
// extractRequestSeconds —— 那三个是给「促销族 → 豆包」用的宽松口径
// （多读几个字段、比例取 metadata 优先、时长 duration 优先）。
// 豆包链路（源）的真实口径是：
//
//	参考图：按固定优先级取**第一个有值的字段**，只取那一个字段
//	        （input_reference > images > image > image_url > …）。
//	        **不是**把所有别名的图合并 —— 豆包只认第一个有值的。
//	比例  ：firstNonEmpty(size, ratio, aspect_ratio)，只认豆包 7 档
//	        （auto / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9），
//	        空 = 不编造（保持官网当前状态），非法 = 400。
//	时长  ：seconds 优先于 duration，缺省 5 秒；正整数原样下发，不改写。
//	素材  ：**只读顶层那批别名**。豆包的请求结构里**没有 media 数组**，
//	        顶层 media / metadata.media / audio / audios / videos / video
//	        豆包链路一律**忽略** —— 改派时也必须一并丢掉，
//	        否则备胎链路会把它们当参考图/参考音频收下，等于替客户加需求。
//
// 班车的目标口径（与 relay/channel/task/yuanmeng 同源）：
//
//	参考图：media[] 里 type=reference_image
//	比例  ：metadata.ratio（7 档，含 adaptive）
//	时长  ：seconds（字符串），2–30 秒
//	素材合计 ≤20、参考图 ≤10、参考视频 ≤5、参考音频 ≤5
//
// 比例映射：豆包的 auto → 班车的 adaptive（两者都是「让平台自适应」，
// 其余六档两边同名）。
//
// ───────────────────────── 为什么必须挡能力差异 ─────────────────────────
//
// 改派只是把 body 的 model 换成备胎模型，**后续的重试只会用同一个模型名
// 重选渠道，不会退回源模型**。所以改派前必须确认「备胎链路真的能接住这个请求」，
// 否则就是「把能跑的请求改成跑不了的」。
//
// 豆包（seedance-2-5）与班车（wan3.0-班车）的契约差异：
//
//	时长：豆包正整数原样（2.5 任意，2.0 Fast 4–30）；班车 2–30 任意整数
//	分辨率：豆包**显式拒绝** resolution；班车支持 480P/720P/1080P
//	比例：豆包认顶层 size/ratio/aspect_ratio；班车认 metadata.ratio
//	参考图：豆包认 input_reference 及一堆别名；班车认 media[].type=reference_image
//
// 唯一需要挡的是「备胎接不住的形态」，以及「源链路本来会报错、改派过去
// 语义会变」的形态（非法比例等）。

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// failoverTargetModel 是故障转移的目标模型（备胎）的默认对外名。
//
// 它是 relay/channel/task/yuanmeng 的 PublicModel 镜像。中间件不能 import
// 那个包（会把 middleware 和 relay 渠道包绑在一起），所以这里写一份常量，
// 并在测试里断言两边一致。
const failoverTargetModel = "wan3.0-班车"

// failoverMinSeconds / failoverMaxSeconds 是班车链路的时长范围。
//
// 与 relay/channel/task/yuanmeng 的 minDurationSeconds/maxDurationSeconds 同口径。
const (
	failoverMinSeconds = 2
	failoverMaxSeconds = 30
)

// failoverAllowedDurations 是**改派到班车**时保留的保守白名单。
//
// 2026-11-09 起源链路（豆包 2.5）已接受任意正整数秒，本表不再用于推断
// 源链路合法性，只用来限制改派面：非档位值在源链路失败时直接报错，
// 不悄悄换到班车（班车自身仍限 2–30 秒）。要放开需单独评审。
var failoverAllowedDurations = map[int]bool{
	5:  true,
	10: true,
	15: true,
	30: true,
}

// failoverDefaultSeconds 是没传时长时的取值。
//
// 与豆包链路的缺省时长一致（relay/channel/task/manwu 的 doubaoDefaultDuration），
// 也是班车的缺省时长。两边相同，所以显式写回去只是为了让语义一目了然。
const failoverDefaultSeconds = 5

// failoverMaxReferenceImages 是班车链路的参考图张数上限。
//
// 与 relay/channel/task/yuanmeng 的 maxReferenceImages 同口径。
// 豆包链路的上限也是 10，所以正常不会触发；留着是为了防止上游放宽后静默截断。
const failoverMaxReferenceImages = 10

// failoverMinPromptLength / failoverMaxPromptLength 是班车链路的提示词长度区间。
//
// 与 relay/channel/task/yuanmeng 的 BuildRequestBody 一致（上游 5～20000 字符）。
//
// ⚠️ **单位是字节，不是字符**：班车 adaptor 用 `len(prompt)`（Go 字符串字节数）
// 判上下限，不是 `len([]rune(prompt))`。中文提示词每个字 3 字节，
// 用字符数判会放行 3 倍长的提示词 —— 改派过去必被上游 400，
// 而留在源链路还能拿到源链路的明确报错。
// 所以这里的校验**必须用字节口径**，与 adaptor 完全对齐。
const (
	failoverMinPromptLength = 5
	failoverMaxPromptLength = 20000
)

// failoverImageFields 是豆包链路参考图字段的**优先级顺序**。
//
// 与 relay/channel/task/manwu 的 referenceImageFields **同序同字段**。
//
// ⚠️ 顺序是语义的一部分：豆包的 resolveReferenceImages 命中**第一个有值的
// 字段**就 return，只用那一个字段的图。所以客户端同时传 images 和
// input_reference 时，豆包链路只用 input_reference 的图。
// 改派若把所有别名的图合并，就等于替客户多传了图 —— 备胎链路的参考图上限
// 会被触发，或者提示词里的 @图N 引用数与实际张数对不上（豆包链路会做
// 引用覆盖校验）。
//
// 两条各写一份的必然漂移由 middleware/video_route_failover_test.go 断言
// （拿 relay 包的字段表做交叉校验）。
var failoverImageFields = []string{
	"input_reference", "inputReference",
	"images", "image",
	"image_url", "imageUrl",
	"image_urls", "imageUrls",
	"reference_image", "referenceImage",
	"reference_images", "referenceImages",
	"input_images", "inputImages",
}

// failoverDoubaoRatios 是豆包链路接受的比例档位。
//
// 与 relay/channel/task/manwu 的 allowedDoubaoRatios 同口径。
var failoverDoubaoRatios = map[string]bool{
	"auto": true,
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// failoverRatioMap 把豆包的比例档位映射到班车能认的档位。
//
// 豆包的 auto（官网「自动」档）→ 班车的 adaptive（「让平台自适应」）。
// 其余六档两边同名。
//
// ⚠️ 不能直接沿用 middleware 里的 mapAdaptiveRatio：那个函数是
// 「有赞 adaptive → 豆包 auto」方向的（规则一/二改派到豆包时用），
// 方向正好相反。用错会把 16:9 映成 auto。
var failoverRatioMap = map[string]string{
	"auto": "adaptive",
	"16:9": "16:9",
	"9:16": "9:16",
	"1:1":  "1:1",
	"4:3":  "4:3",
	"3:4":  "3:4",
	"21:9": "21:9",
}

// failoverPassthroughFields 是「原样透传给备胎链路」的顶层字段。
//
// 只有这几个：prompt（必填）、resolution（客户明确要的分辨率）、
// idempotency_key（幂等键）。
//
// 其余字段全部**重建**（model / seconds / metadata / media）或不带
// （见文件头的「只搬运源链路真正生效的参数」）。
//
// 为什么 resolution 要透传：豆包链路显式拒绝 resolution（会 400），
// 备胎链路支持 480P/720P/1080P 且三档同价。透传它 = 尊重客户明确要的
// 规格，而不是默默给 720P。
var failoverPassthroughFields = map[string]bool{
	"prompt":          true,
	"resolution":      true,
	"idempotency_key": true,
}

// tryFailoverRoute 尝试把请求改派到故障转移的备胎模型。
//
// billingOriginModel 是「计费口径用的模型名」= 客户**最初**请求的模型。
//
// ⚠️ 这个参数不能简单地填「当前 body 里的 model」：
//
//	顶层调用：客户直接请求 seedance-2-5 → billingOrigin = seedance-2-5。
//	规则一之后调用：请求已经从促销族改派到豆包了，但**计费口径必须回到
//	  促销族**（客户买的是 seedance2.5-特惠，¥0.20/秒；不是 seedance-2-5
//	  的 ¥3.50/次）。填错就会「客户买 A、按 B 收费」。
//
// 返回 (是否改派, 原因)。改派失败时**一律不是错误**，只放行原请求：
// 源链路不可用但备胎也接不住时，客户看到的应该是源链路原本的错误
// （比如「今日额度已用完」），而不是「路由失败」这种内部术语。
func tryFailoverRoute(c *gin.Context, body map[string]interface{}, billingOriginModel string) (bool, string) {
	// 豆包链路开关是唯一事实源：开关关闭时，规则五总开关也挡不住改派 ——
	// 否则 seedance-2-5 会被留在已宣布关闭的豆包链路上（静默全挂）。
	if !seedanceroute_setting.IsFailoverRouteEnabled() && !seedanceroute_setting.IsDoubaoLinkOff() {
		return false, ""
	}
	modelName, _ := body["model"].(string)
	if !seedanceroute_setting.IsFailoverSource(modelName) {
		return false, ""
	}
	target := seedanceroute_setting.FailoverTargetModel()
	if target == "" {
		return false, ""
	}

	// 源链路不可用吗？两种形态取并集。
	unavailable, reason := sourceChainUnavailable(c, modelName)
	if !unavailable {
		return false, ""
	}

	newBody, ok := prepareFailoverRoute(c, body, target)
	if !ok {
		return false, reason
	}
	if !applyRewrittenBody(c, newBody, billingOriginModel) {
		return false, reason
	}
	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：源链路 %s 不可用（%s），改派 %s → %s",
		modelName, reason, modelName, target))
	return true, reason
}

// tryFailoverAfterDoubaoRoute 在「已经改派到豆包链路」之后再判一次故障转移。
//
// 为什么需要这一段（规则五与规则一是**串联**的）：
//
//	规则一把促销族请求改派到 seedance-2-5（豆包官网链路）。
//	而 seedance-2-5 **恰好就是规则五的源模型** —— 豆包额度按天计，
//	额度用完时规则一把请求导过去必然失败（客户白等一轮 + 白烧一次预扣）。
//
//	所以降级链必须是完整的一条：
//
//		促销族 → 豆包 → 班车
//
//	只做第一段（顶层 tryFailoverRoute）的话，豆包额度用完时
//	「整个 30 秒档位全挂」；做了第二段，豆包额度用完时请求继续降级到班车，
//	客户只是等得久一点，但**能出片**。
//
// 与顶层 tryFailoverRoute 的两处关键差异：
//
//  1. **入参形态不同**：这里的 body 已经是「豆包形态」（rewriteBodyForDoubao
//     的产物：input_reference 装着图片、ratio 是豆包档位、seconds 是字符串、
//     metadata/resolution/size 已被摘掉）。不能拿原始请求的解析口径去读它。
//  2. **计费原点不同**：billingOriginModel 必须是**客户最初请求的促销族模型**，
//     不能是豆包。客户买的是 seedance2.5-特惠（¥0.20/秒），
//     降级到班车后仍按 seedance2.5-特惠 计费 —— 改派只是内部调度。
func tryFailoverAfterDoubaoRoute(
	c *gin.Context,
	doubaoBody map[string]interface{},
	billingOriginModel string,
) (bool, string) {
	if !seedanceroute_setting.IsFailoverRouteEnabled() {
		return false, ""
	}
	modelName, _ := doubaoBody["model"].(string)
	if !seedanceroute_setting.IsFailoverSource(modelName) {
		return false, ""
	}
	target := seedanceroute_setting.FailoverTargetModel()
	if target == "" {
		return false, ""
	}

	unavailable, reason := sourceChainUnavailable(c, modelName)
	if !unavailable {
		return false, ""
	}

	newBody, ok := prepareDoubaoBodyFailover(c, doubaoBody, target)
	if !ok {
		return false, reason
	}
	if !applyRewrittenBody(c, newBody, billingOriginModel) {
		return false, reason
	}
	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：源链路 %s 不可用（%s），降级 %s → %s（计费仍按 %s）",
		modelName, reason, modelName, target, billingOriginModel))
	return true, reason
}

// prepareDoubaoBodyFailover 把「豆包形态」的请求体转成备胎形态。
//
// 与 prepareFailoverRoute 的差别在**读什么**：
//
//	prepareFailoverRoute       读的是**原始请求**（客户直接请求豆包时），
//	                          走豆包 adaptor 的口径：第一个有值的参考图字段、
//	                          size>ratio>aspect_ratio、seconds 优先于 duration。
//	prepareDoubaoBodyFailover  读的是**豆包改写后的产物**，那已经是归一过的形态：
//	                          图片在 input_reference、比例在顶层 ratio、
//	                          时长在顶层 seconds。
//
// 两者产出的备胎请求体必须等价 —— 不等价就意味着「客户直接请求豆包」与
// 「经促销族降级到豆包」两条路给出的参数不同，那是不可解释的差异。
func prepareDoubaoBodyFailover(
	c *gin.Context,
	doubaoBody map[string]interface{},
	target string,
) (map[string]interface{}, bool) {
	// 备胎链路自己没有可用渠道 → 不能改派。
	if !channelAvailabilityProbe(c, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：备胎 %s 在当前分组下没有可用渠道", target))
		return nil, false
	}

	// 提示词：班车要求 5～20000 **字节**（与班车 adaptor 的 len() 同口径）。
	// 中文按字符数判会放行 3 倍长的提示词，改派过去必被上游 400。
	prompt, _ := doubaoBody["prompt"].(string)
	promptLength := len(strings.TrimSpace(prompt))
	if promptLength < failoverMinPromptLength || promptLength > failoverMaxPromptLength {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：提示词 %d 字节超出备胎链路范围 %d-%d 字节",
			promptLength, failoverMinPromptLength, failoverMaxPromptLength))
		return nil, false
	}

	seconds, secondsOK := failoverSourceSeconds(doubaoBody)
	if !secondsOK {
		logger.LogInfo(c, "条件路由跳过故障转移：时长不是合法整数，源链路会直接报错")
		return nil, false
	}
	// 时长先过**保守改派白名单**（failoverAllowedDurations），再过备胎的范围。
	//
	// 该白名单不是源链路的时长契约（豆包本就支持任意正整数秒，原样下发），
	// 只是为了限制改派面：非白名单时长不改派，让原请求留在源链路。
	if !failoverAllowedDurations[seconds] {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：时长 %d 秒不在源链路允许的档位内", seconds))
		return nil, false
	}
	if seconds < failoverMinSeconds || seconds > failoverMaxSeconds {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：时长 %d 秒超出备胎链路范围 %d-%d 秒",
			seconds, failoverMinSeconds, failoverMaxSeconds))
		return nil, false
	}

	// 图片：豆包形态里已经归一到 input_reference。
	images, imageErr := failoverParseStringList(doubaoBody["input_reference"])
	if imageErr != nil {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：参考图解析失败（%v）", imageErr))
		return nil, false
	}
	if len(images) > failoverMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：参考图 %d 张超过备胎链路上限 %d 张",
			len(images), failoverMaxReferenceImages))
		return nil, false
	}

	// 比例：豆包形态里已经归一到顶层 ratio（豆包档位）。
	ratio, ratioOK := failoverSourceRatio(doubaoBody)
	if ratioOK {
		if _, mapped := failoverRatioMap[ratio]; !mapped {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过故障转移：比例 %q 不是源链路支持的档位", ratio))
			return nil, false
		}
	}

	return rewriteDoubaoBodyForFailover(doubaoBody, target, prompt, seconds, images, ratio, ratioOK), true
}

// rewriteDoubaoBodyForFailover 把豆包形态的请求体转成备胎形态。
//
// 与 rewriteBodyForFailover 的差别：字段来源是豆包形态（input_reference /
// 顶层 ratio / 顶层 seconds），而**丢弃规则**相同 ——
// 顶层 media / metadata.media / audio / audios / videos / video 一律不带
// （豆包链路根本不读它们，带过去等于替客户加需求）。
func rewriteDoubaoBodyForFailover(
	doubaoBody map[string]interface{},
	target string,
	prompt string,
	seconds int,
	images []string,
	ratio string,
	ratioOK bool,
) map[string]interface{} {
	out := make(map[string]interface{}, len(doubaoBody)+4)
	for key, value := range doubaoBody {
		switch key {
		case "model", "metadata", "media", "input_reference",
			"audio", "audios", "videos", "video",
			"ratio", "aspect_ratio", "size":
			// 这些要么会被重建，要么豆包链路根本不读 —— 一律不带。
			continue
		}
		out[key] = value
	}
	out["model"] = target
	out["prompt"] = prompt
	out["seconds"] = strconv.Itoa(seconds)

	if len(images) > 0 {
		media := make([]map[string]interface{}, 0, len(images))
		for _, url := range images {
			media = append(media, map[string]interface{}{"type": "reference_image", "url": url})
		}
		out["media"] = media
	}

	metadata := make(map[string]interface{})
	if existing, ok := doubaoBody["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			if key == "media" {
				continue
			}
			metadata[key] = value
		}
	}
	if ratioOK {
		metadata["ratio"] = failoverRatioMap[ratio]
	}
	if len(metadata) > 0 {
		out["metadata"] = metadata
	}

	return out
}

// sourceChainUnavailable 判断源链路当前是否不可用，并给出原因文案。
//
// 静态判定（渠道状态）优先：渠道真被下线了，探针状态位多半还没写。
// 动态判定（探针时间戳）兜底：渠道还活着但额度用完了。
func sourceChainUnavailable(c *gin.Context, modelName string) (bool, string) {
	// 豆包链路开关：运营人工宣布「豆包不可用」，先于渠道探测生效 ——
	// 渠道可用也照样改派（开关是主动意图，不是故障推断）。
	// 只对豆包模型生效：开关表达的是豆包通不通，不能殃及别的源链路。
	if modelName == doubaoTargetModel && seedanceroute_setting.IsDoubaoLinkOff() {
		return true, "豆包链路已关闭（凌晨 2 点自动恢复）"
	}
	// 用 channelAvailabilityProbe（而不是直接调 hasAvailableChannel），
	// 这样单测替换一个变量就能分别控制「源链路可用」与「备胎可用」——
	// 两者的组合是本规则的全部判定矩阵。
	if !channelAvailabilityProbe(c, modelName) {
		return true, "渠道不可用"
	}
	if seedanceroute_setting.FailoverUnavailable(time.Now().Unix()) {
		reason := seedanceroute_setting.FailoverReason()
		if reason == "" {
			reason = "上游额度已用完"
		}
		return true, reason
	}
	return false, ""
}

// prepareFailoverRoute 组装改派到备胎模型的请求体。
//
// 返回 (新 body, 是否可改派)。不可改派时把原因写进日志，让运营能看出
// 「为什么源链路挂了却没走备胎」。
func prepareFailoverRoute(c *gin.Context, body map[string]interface{}, target string) (map[string]interface{}, bool) {
	// 备胎链路自己没有可用渠道 → 不能改派。
	//
	// 与其他改派规则同一理由：改写 body 之后，重试只会用同一个模型名重选渠道，
	// 不会退回源模型。备胎也挂了的话，改派过去只是把「额度用完」换成「没有可用渠道」。
	if !channelAvailabilityProbe(c, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：备胎 %s 在当前分组下没有可用渠道", target))
		return nil, false
	}

	// 提示词：班车要求 5～20000 **字节**。
	//
	// 豆包链路只检查「非空」，长度上限在更上游（官网/App 侧）。
	// 短于 5 字节的提示词在豆包链路可能能过（但内容毫无意义），
	// 改派到班车一定被拒 → 留在源链路。
	//
	// ⚠️ 单位必须与班车 adaptor 一致（字节，不是字符）：
	// relay/channel/task/yuanmeng 用 `len(prompt)` 判上下限。
	// 中文提示词按字符数判会放行 3 倍长的提示词，改派过去必被上游 400。
	prompt, _ := body["prompt"].(string)
	promptLength := len(strings.TrimSpace(prompt))
	if promptLength < failoverMinPromptLength || promptLength > failoverMaxPromptLength {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：提示词 %d 字节超出备胎链路范围 %d-%d 字节",
			promptLength, failoverMinPromptLength, failoverMaxPromptLength))
		return nil, false
	}

	// 时长：取源链路口径（seconds 优先于 duration，缺省 5 秒），
	// **先过保守改派白名单**，再确认备胎链路吃得住（2–30 秒）。
	//
	// 该白名单只限制改派面，不是源链路的时长契约：豆包本身就支持任意正整数秒，
	// 原样下发、不改写。非白名单时长不改派，让原请求留在源链路。
	//
	// ⚠️ 时长非法（小数/非数字）时**也放弃改派**：源链路 manwu 对这类值是
	// fail-loud 的（会报 invalid_duration），改派过去等于把「源链路会报错的请求」
	// 变成「备胎按某个截断值跑」——静默改了客户要的时长。
	seconds, secondsOK := failoverSourceSeconds(body)
	if !secondsOK {
		logger.LogInfo(c, "条件路由跳过故障转移：时长不是合法整数，源链路会直接报错")
		return nil, false
	}
	if !failoverAllowedDurations[seconds] {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：时长 %d 秒不在源链路允许的档位内", seconds))
		return nil, false
	}
	if seconds < failoverMinSeconds || seconds > failoverMaxSeconds {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：时长 %d 秒超出备胎链路范围 %d-%d 秒",
			seconds, failoverMinSeconds, failoverMaxSeconds))
		return nil, false
	}

	// 参考图：按豆包的优先级取**第一个有值的字段**。
	//
	// 豆包链路只认那一个字段，所以改派也只搬那一个 —— 合并所有别名
	// 会把客户没让用的图也送上去（见文件头的说明）。
	images, imageErr := failoverSourceImages(body)
	if imageErr != nil {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：参考图解析失败（%v）", imageErr))
		return nil, false
	}
	if len(images) > failoverMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移：参考图 %d 张超过备胎链路上限 %d 张",
			len(images), failoverMaxReferenceImages))
		return nil, false
	}

	// 比例：按豆包的优先级取（size > ratio > aspect_ratio），
	// 必须在豆包 7 档之内 —— 非法值源链路会 400，改派过去若不写比例，
	// 备胎链路会按缺省档出片，等于「静默改了客户要的画幅」。
	//
	// 空 = 不写比例（源链路也是「不编造」，保持官网当前状态）；
	// 备胎链路缺省 16:9 —— 这里语义不完全一致，但因为豆包那一档是
	// 「自动」，宁可让备胎按自己的缺省出，也不要替客户编一个档位。
	ratio, ratioOK := failoverSourceRatio(body)
	if ratioOK {
		if _, mapped := failoverRatioMap[ratio]; !mapped {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过故障转移：比例 %q 不是源链路支持的档位", ratio))
			return nil, false
		}
	}

	return rewriteBodyForFailover(body, target, prompt, seconds, images, ratio, ratioOK), true
}

// rewriteBodyForFailover 组装改派到备胎链路的请求体。
//
// 字段翻译（豆包契约 → 班车契约）：
//
//	model                → 换成备胎模型名
//	prompt / resolution  → 原样透传
//	参考图（第一个有值的别名字段）→ media[] 里 type=reference_image 的项
//	比例（size > ratio > aspect_ratio）→ metadata.ratio（auto → adaptive）
//	seconds / duration   → 统一写成 seconds（字符串），取源链路口径的值
//
// 其它顶层字段（media / metadata.media / audio / audios / videos / video /
// 各个参考图别名 / 顶层比例字段 / n / count …）一律**不带**：
// 它们要么是源链路根本不读的东西（带过去等于替客户加需求），
// 要么已经被上面重建过（留着会重复）。
//
// ⚠️ 计费口径：改派**不改变对客价格**。客户请求 seedance-2-5，
// 就按 seedance-2-5 的价收（¥3.50/次，按次不叠时长倍率）——
// 备胎链路是按次计费的，与源模型口径一致，不需要补 seconds 倍率。
// 见 constant.ContextKeyRouteOriginModel 的注释：计费按源模型算。
func rewriteBodyForFailover(
	body map[string]interface{},
	target string,
	prompt string,
	seconds int,
	images []string,
	ratio string,
	ratioOK bool,
) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+4)
	for key, value := range body {
		if failoverPassthroughFields[key] {
			out[key] = value
		}
	}
	out["model"] = target
	out["prompt"] = prompt

	// 时长：写成字符串形态最通用（班车 resolveDuration 优先读 Duration，
	// 回退读 Seconds 字符串）。源模型是按次计费，不需要补倍率。
	out["seconds"] = strconv.Itoa(seconds)

	// 素材：拼成备胎链路认识的 media[] 结构。
	//
	// 只有参考图 —— 源链路（豆包）根本不收参考视频/参考音频/首尾帧，
	// 带上它们等于替客户加需求（而且备胎链路的「参考视频时长 + 生成时长 ≤30 秒」
	// 会直接让请求失败）。
	if len(images) > 0 {
		media := make([]map[string]interface{}, 0, len(images))
		for _, url := range images {
			media = append(media, map[string]interface{}{"type": "reference_image", "url": url})
		}
		out["media"] = media
	}

	// metadata：保留客户自带的其它键（如 title，班车用它当任务标题），
	// 但**丢掉 media**（源链路不读它，带过去会被当参考图），
	// 并把比例写进 ratio。
	metadata := make(map[string]interface{})
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			if key == "media" {
				continue
			}
			metadata[key] = value
		}
	}
	if ratioOK {
		metadata["ratio"] = failoverRatioMap[ratio]
	}
	if len(metadata) > 0 {
		out["metadata"] = metadata
	}

	return out
}

// failoverSourceImages 按豆包链路的优先级取出参考图。
//
// 语义与 relay/channel/task/manwu 的 resolveReferenceImages 一致：
//   - 按 failoverImageFields 的顺序，取**第一个有值**的字段；
//   - 该字段可以是字符串、字符串数组，或 {"url": "..."} 对象；
//   - 形态非法 → 返回错误（调用方放弃改派）。
func failoverSourceImages(body map[string]interface{}) ([]string, error) {
	for _, field := range failoverImageFields {
		raw, ok := body[field]
		if !ok {
			continue
		}
		images, err := failoverParseStringList(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		if len(images) == 0 {
			continue
		}
		return images, nil
	}
	return nil, nil
}

// failoverParseStringList 把「字符串 / 字符串数组 / {"url":...} 对象」
// 三种形态统一成 []string。
//
// 与 relay/channel/task/manwu 的 parseStringList 同口径：
//   - 空串 / null / 空数组 → 没有值（不是错误）；
//   - 数组里逐个元素按同样规则解析；
//   - 其它形态 → 错误（源链路会 400，改派过去也一样，不该静默丢图）。
func failoverParseStringList(raw interface{}) ([]string, error) {
	switch typed := raw.(type) {
	case nil:
		return nil, nil
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil, nil
		}
		return []string{trimmed}, nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			values, err := failoverParseStringList(item)
			if err != nil {
				return nil, err
			}
			out = append(out, values...)
		}
		return out, nil
	case map[string]interface{}:
		url, _ := typed["url"].(string)
		url = strings.TrimSpace(url)
		if url == "" {
			return nil, fmt.Errorf("must be a string or an array of strings")
		}
		return []string{url}, nil
	}
	return nil, fmt.Errorf("must be a string or an array of strings")
}

// failoverSourceRatio 按豆包链路的优先级取出比例。
//
// 语义与 relay/channel/task/manwu 的 kindDoubaoVideo 分支一致：
// firstNonEmpty(size, ratio, aspect_ratio)。
//
// 注意**不读 metadata.ratio**：豆包的请求结构里没有 metadata 字段，
// 客户把它塞进 metadata 是没用的（豆包链路会忽略）。读它等于把客户
// 没生效的意图搬过来。
//
// 第二个返回值 false = 请求里没有比例（源链路保持「不编造」）。
func failoverSourceRatio(body map[string]interface{}) (string, bool) {
	for _, key := range []string{"size", "ratio", "aspect_ratio"} {
		if value, ok := body[key].(string); ok {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed, true
			}
		}
	}
	return "", false
}

// failoverSourceSeconds 按豆包链路的优先级取出时长（秒）。
//
// 语义与 relay/channel/task/manwu 的 resolveDoubaoDuration 一致：
// **seconds 优先于 duration**（不是 middleware 里 extractRequestSeconds 的
// duration 优先 —— 那是给促销族链路用的宽松口径），缺省 5 秒。
//
// 数值与字符串两种写法都接（源链路用 parseFlexibleInt）。
//
// ⚠️ 第二个返回值是「这个值是否合法」：
// 源链路对非法值是 fail-loud 的（parseFlexibleInt 遇到小数/非数字直接报错），
// 而这里的解析若继续「尽量解析出一个数」，就会把 `7.5` 截断成 `7` 当成合法值
// 搬给备胎 —— 客户拿到的是 7 秒的片，源链路本该报错的那一单被静默改了语义。
// 所以解析不出来时必须**如实报非法**，由调用方放弃改派（留在源链路报错）。
func failoverSourceSeconds(body map[string]interface{}) (int, bool) {
	// ⚠️ 「字段存在」不等于「字段有值」：
	//
	//	源链路 manwu 的 hasValue 把 null / "" / [] 一律视为「没传」，
	//	于是 resolveDoubaoDuration 会继续回退到 duration，再回退到默认值 5。
	//	这里如果用 `raw != nil` 判断，seconds:null 会被判成「字段存在但解析不出」
	//	→ 非法 → 放弃改派 —— 那就比源链路更严：源链路能跑的单子，改派侧不敢接。
	//
	// 所以判定顺序必须是：先按 hasValue 同口径判断「有没有值」，
	// 有值但解析不出整数才算非法。
	if raw, ok := body["seconds"]; ok && failoverHasValue(raw) {
		if seconds, valid := failoverParseInt(raw); valid {
			return seconds, true
		}
		// 字段有值但解析不出整数 → 非法（与源链路同纪律：报错，不截断）。
		return 0, false
	}
	if raw, ok := body["duration"]; ok && failoverHasValue(raw) {
		if seconds, valid := failoverParseInt(raw); valid {
			return seconds, true
		}
		return 0, false
	}
	return failoverDefaultSeconds, true
}

// failoverHasValue 判断字段是不是「有值」。
//
// 与 relay/channel/task/manwu 的 hasValue 同口径：
// null、空串、空数组都视为「没传」。
//
// ⚠️ 空白字符串（"  "）**算有值**：源链路的 hasValue 是拿原始 JSON 文本
// 比对 `""` 字面量，"  " 不匹配 → 当有值 → 进 parseFlexibleInt → Atoi 失败
// → 报「is not a valid integer」。这里若用 TrimSpace 判空，
// "  " 就会被当成「没传」而回退到默认值 —— 那比源链路宽松，同样是语义漂移。
//
// 注意：map 里的空数组是 []interface{}{}，空串是 ""，
// 而 JSON null 解到 interface{} 上是 nil —— 三者都要当「没值」。
func failoverHasValue(raw interface{}) bool {
	switch typed := raw.(type) {
	case nil:
		return false
	case string:
		return typed != ""
	case []interface{}:
		return len(typed) > 0
	default:
		return true
	}
}

// failoverParseInt 把「数字 / 数字字符串 / null」解析成 int。
//
// ⚠️ **小数不算合法**：源链路 manwu 的 parseFlexibleInt 用 json.Unmarshal 到
// *int，小数（7.5）会直接 Unmarshal 失败 → 源链路报「is not a valid integer」。
// 这里如果用 int(typed) 截断，7.5 会变成 7 —— 把「源链路会报错的一单」
// 变成「备胎按 7 秒跑」的一单，静默改了客户要的时长。
// 所以小数一律判为非法。
func failoverParseInt(raw interface{}) (int, bool) {
	switch typed := raw.(type) {
	case float64:
		// JSON 数字统一解成 float64，整数值没有小数部分才算合法。
		if typed != math.Trunc(typed) {
			return 0, false
		}
		return int(typed), true
	case int:
		return typed, true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		value, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return value, true
	}
	return 0, false
}
