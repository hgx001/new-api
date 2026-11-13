package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/wan3"
	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 条件路由：把「某个对外模型名 + 特定时长」的请求改派到另一个对外模型名。
//
// 为什么放在 Distribute 之前：渠道是在 middleware/distributor.go 的 getModelRequest(c)
// 里读 body 的 model 字段选定的。改写 body 之后，选渠道、挑 adaptor、算 ModelPrice
// 全都自动跟着新模型名走 —— **不需要改任何 adaptor，也不需要动 abilities 表**。
//
// 为什么不在 controller 里改：controller/relay.go 的 getChannel 只是兜底读
// c.GetInt("channel_id")，那时渠道早就选完了，改名只会让「渠道 20 的 wan3 adaptor
// 收到 seedance-2-5 的请求」这种错配发生。
//
// 失败原则：**任何一步不确定就放弃路由**（放行原请求走源模型），
// 绝不挡客户、绝不带着少掉的素材建单。

// routeOriginModelKey 记录改派前的原始模型名。
//
// **计费与展示都以它为优先**（老板 2026-10-30 定稿）：客户请求哪个模型，
// 就按哪个模型的价格收。改派是我们为了省上游成本做的内部调度，
// 不能让客户看到「我买 A，账单按 B 算」，也不能让对客价格随调度策略漂移。
//
// 计费链取值入口是 RelayInfo.BillingModelName()：
//
//	relay/helper/price.go  ratio_setting.GetModelPrice(info.BillingModelName(), true)
//
// 所以 30 秒改派到目标模型 seedance-2-5 后，客户付的仍是 seedance2.5-特惠 的
// ¥0.20/秒 × 30 = ¥6.00，而不是 seedance-2-5 的按次对外价（价目表现行值）。
// 差额是内部调度收益，不体现在客户账单上。
//
// ⚠️ 但光有模型名不够：源模型按秒计价，而目标 adaptor（按次计费返回 nil 倍率）
// 没有 seconds 就会把 30 秒算成 1 秒。所以规则一命中时
// 还要把秒数记进 ContextKeyRouteBillingSeconds（见 relay_task.go 第 5.5 步）。
//
// properties.origin_model_name 记「实际执行的模型」是自洽且诚实的：
// 它回答的是「这条任务在哪个链路上跑」，与「按谁收费」是两个问题。
const routeOriginModelKey = string(constant.ContextKeyRouteOriginModel)

// promptRouteLimitRunes 是 wan3 上游的提示词上限（字符数，按 rune 算）。
//
// ⚠️ 2026-11-11 起它**不再是条件路由的触发条件**：原「规则四（提示词超长 →
// 豆包）」已删除。超长提示词统一由链路后一道 Wan3PromptCompress 处理
// （能压就压、压不下来明确 400），不再悄悄换链路。
//
// 这个常量仍被 wan3_prompt_compress.go / _guard.go 用作「上游限长」与
// 「可压缩上界」的事实来源，所以留在这里而不是搬走。按字符而不是字节算：
// 5000 个汉字按字节是 15000，用 len() 会把刚好合规的中文提示词误判成超长。
const promptRouteLimitRunes = 5000

// doubaoSecondsFor 校验客户请求的时长是否可以原样发给规则一的目标链路。
//
// 2026-11-09 用户口径：new-api 传多少秒就下发多少秒，**禁止吸附/回落改写**。
// 函数名里的 doubao 是历史；目标 seedance-2-5 的常驻渠道 2026-11-12 起是
// 豆包官网 ch20（漫屋ArcReel），此前一天曾临时迁到视频生成车站 ch27（yuanmeng，
// 时长 2–30 秒）。豆包 2.5 支持**任意正整数秒**（见 manwu 的
// resolveDoubaoDuration），规则一命中区间是严格大于阈值（生产 20 秒）。
// 超出目标链路自身能力范围的请求由它自己拒绝（fail-loud），不在这里静默改值。
//
// 返回 false 仅表示「秒数非法（<=0）」，不再有最大档限制 —— 以前 21–29 秒
// 向上吸附到 30、>30 秒放弃改派的行为都已删除。
func doubaoSecondsFor(seconds int) (int, bool) {
	if seconds <= 0 {
		return 0, false
	}
	return seconds, true
}

// RouteOriginModel 返回改派前的原始模型名；没发生改派时返回空串。
func RouteOriginModel(c *gin.Context) string {
	return c.GetString(routeOriginModelKey)
}

// Seedance25TeHuiRoute 在满足条件时把请求改派到另一个对外模型。
//
// 四条规则，**顺序即优先级**：
//
//	规则五（故障转移）：seedance-2-5 源链路不可用 → 备胎（最优先，独立字段判定）
//	规则三（首尾帧）：  源模型 + media 含 first_frame/last_frame → frames_target_model
//	规则一（时长）：    源模型 + 时长 > 阈值（生产 20 秒）→ target_model（seedance-2-5 按次）
//	规则二（比例）：    源模型 + 命中 trigger_ratio → ratio_target_model
//	                    生产配 9:16 → wan3.0-720p-特惠-内链（心笔 480P 链路，ch25）
//
// 2026-11-11 删除原「规则四（提示词 >5000 字符 → 豆包）」：它几乎总被规则一
// 抢先，且超长提示词本来就该由后面的 Wan3PromptCompress 压缩（压不下来明确
// 400）。规则四没有独立开关、跟着时长规则走，留着只会让同一份长提示词在
// 「改派豆包」与「留在源链路压缩」之间漂移。删掉后长提示词**不再触发任何改派**。
//
// 规则三为什么排最前：源链路 youzan wan3 **已彻底不收首尾帧**（2026-10-28 上游
// 取消 firstFrameAssetId/lastFrameAssetId），而规则一的改写也只搬参考图、
// 不搬运首尾帧语义（首尾帧被降级成参考图 —— 拿到的不是客户要的首尾帧成片）。
// 「带首尾帧」的请求只有 wan3 直连能跑 —— 这是能力约束，优先级高于时长/比例的
// 成本优化。排在规则一之后就漏了「30 秒 + 首尾帧」这个组合（会被规则一改写、
// 丢掉首尾帧语义）。
//
// 规则一/二之间仍是**时长优先**（老板 2026-10-30 拍板「30 秒优先，豆包仍接 9:16」），
// 所以规则二实际只接「未超过时长阈值」的 9:16 请求。
//
// 改派**只决定内部走哪条链路，不改变对客价格**：四条规则的计费都按源模型
// （客户请求的那一个）算，RouteOriginModel 记的就是它。
//
// 源/目标/触发值全部从 seedanceroute_setting 读，改配置不用改代码。
//
// 不路由的情形（**都不是错误**，一律放行原请求）：
//   - 开关关闭（默认）或规则都没配全
//   - 模型名不匹配
//   - 时长规则：时长不大于触发阈值（含不传时长）
//   - 时长规则：带了参考视频 / 参考音频（规则一的改写只搬参考图 —— 改派会
//     静默丢音视频，见 hasUnsupportedMedia；历史上的豆包官网链路同样不收）
//   - **目标模型在当前分组下没有可用渠道**（见 hasAvailableChannel）
//   - 比例规则：目标链路的能力边界不满足（见 prepareWan3Route / tehuiXinbiCompatible）
//   - 请求不是 JSON，或 body 解析/转换失败
//
// 2026-11-12 常驻迁移：seedance-2-5 的常驻渠道由豆包官网（ch20）改为视频
// 生成车站（ch27，wan3.0-班车）。此后规则一的改派与规则五的降级都落在班车
// 链路上；规则五保留，但源模型与备胎当前解析到同一批渠道 —— 此时改名没有
// 路由收益，failoverSharedChannelChain 会显式放弃改写、放行原请求
// （含额度探针触发的形态，计费原点逻辑不受影响）。
func Seedance25TeHuiRoute() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsAnyRouteEnabled() {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
			c.Next()
			return
		}
		// /remix 是基于**已有任务**再生成，模型与参数由原任务决定
		// （relay.ResolveOriginTask 负责还原），条件路由对它没有意义，
		// 而且改写 body 会破坏 remix 的原任务语义。
		if strings.HasSuffix(c.Request.URL.Path, "/remix") {
			c.Next()
			return
		}

		policy := seedanceroute_setting.Get()
		body, ok := readJSONRequestBody(c)
		if !ok {
			c.Next()
			return
		}

		// ── 规则五（故障转移）最先判 ──
		//
		// 为什么排在最前、且排在 IsSourceModel 检查**之前**：
		//
		//	1. 触发源不同：前三条规则看的是「请求长什么样」，
		//	   规则五看的是「源链路还能不能用」。源链路都挂了，
		//	   再去优化「这条 30 秒的请求该走哪条更便宜的链路」没有意义 ——
		//	   客户要的是能出片，不是最便宜的片。
		//	2. 源模型口径不同：规则五的源是 seedance-2-5，
		//	   它**不在** policy.SourceModel / SourceModels 里 ——
		//	   那两个字段是促销族的源，而 seedance-2-5 恰恰是规则一的**目标**。
		//	   如果把它塞进 source_models，规则一命中后会把请求改派成同一个
		//	   模型名，形成「改派到自己」的循环。
		//
		// 所以规则五走独立的字段（failover_source_model）与独立的判定函数，
		// 必须在 IsSourceModel 的早退**之前**判。
		//
		// ⚠️ 但只判这一次**不够**：规则一的目标模型恰恰就是 seedance-2-5，
		// 也就是说规则五的源模型是规则一的**目标**。源链路（当前即豆包 ch20）
		// 不可用时，规则一把 30 秒请求改派过去必然失败 —— 客户白等一轮 +
		// 白烧一次预扣。所以规则一改派成功后**必须再判一次规则五**（见
		// tryFailoverAfterDoubaoRoute），源与备胎**不同渠道**时把请求继续
		// 改派到备胎，形成完整降级链：
		//
		//	促销族 → seedance-2-5（豆包 ch20）→ 备胎（wan3.0-班车，ch27）
		//
		// 2026-11-12 21:50 把 seedance-2-5 常驻渠道回迁豆包 ch20 后，源与备胎
		// 重新落在**不同渠道**，第二段降级链恢复为真实生效（此前常驻临时
		// 迁到 ch27 时同渠道，会被 failoverSharedChannelChain 拦住）。
		// 该函数本身是数据驱动的（按渠道集合判定，不写死渠道号），
		// 备胎再分到哪批渠道都能正确判断。
		//
		// 计费原点：顶层这一段，客户请求的就是 seedance-2-5，
		// 所以计费口径 = seedance-2-5（¥3.50/次，按次不叠时长倍率）。
		requestedModel, _ := body["model"].(string)
		if routed, _ := tryFailoverRoute(c, body, strings.TrimSpace(requestedModel)); routed {
			c.Next()
			return
		}

		modelName := requestedModel
		if !policy.IsSourceModel(modelName) {
			c.Next()
			return
		}
		// 命中的源模型名（多源化之后可能不是 policy.SourceModel 那个）。
		// 日志必须打**它** —— 改派日志是排查的唯一凭据，
		// 写死成某一个源模型名会让「客户请求活动版、日志却显示特惠版」完全对不上账。
		sourceModel := strings.TrimSpace(modelName)

		// ── 按次档：先把时长区间锁定 ──
		//
		// 为什么必须在这一步做（2026-11-02 上架 seedance2.5-活动 时查出来的绕过路径）：
		//
		//	「越界时长 → 400」这个 fail-loud 门控写在**源链路**
		//	（youzanwan3 / xinbiapi 的按次校验），而条件路由中间件发生在
		//	**源 adaptor 校验之前**。改派后 req.Model 已经是目标模型，
		//	源链路那道门控根本不会执行 ——
		//
		//	· 客户显式传 1 秒（2026-11-11 前是 5 秒）→ 可能命中规则二/三被改派
		//	  → 门控被绕过 → 越界请求被静默放行，没有报错；
		//	· 客户不传时长 → 源链路本应归一到 30 秒 → 改派到 wan3 后
		//	  wan3 的 resolveDuration 缺省是 **5 秒** → 「按 30 秒卖、实际出 5 秒」
		//	  的**错交付**。
		//
		// 所以按次档的时长必须先在**路由层**（改派之前）校验，不能只依赖源 adaptor。
		//
		// 处置口径：
		//	· 显式传了越界时长 → **不放行路由**（让请求原样走到源链路，
		//	  由源链路报 400）—— 不做就近吸附，静默改档比报错更糟；
		//	· 没传时长 → 归一成默认 30 秒再继续判规则（这样 30 秒规则能正常命中）。
		if constant.IsSeedance25PerCall(sourceModel) {
			normalized, ok := normalizeFixedTierSeconds(body)
			if !ok {
				// 显式传了越界时长：放弃路由，放行原请求 → 源链路会返回 400。
				logger.LogInfo(c, fmt.Sprintf(
					"条件路由：按次固定档模型 %s 的时长越界（合法区间 %d-%d 秒），放弃路由（保持源链路 fail-loud 门控）",
					sourceModel, perCallMinSeconds, perCallMaxSeconds))
				c.Next()
				return
			}
			body = normalized
		}

		// ── 规则三（首尾帧）最优先 ──
		//
		// 必须排在时长/比例规则之前：源链路 youzan wan3 已彻底不收首尾帧
		// （2026-10-28 上游取消 firstFrameAssetId/lastFrameAssetId），而规则一的
		// 改写同样不搬运首尾帧语义。所以「带首尾帧」的请求只有 wan3 直连能跑，
		// 这个能力约束优先级高于时长/比例的成本优化 —— 否则 30 秒的首尾帧请求
		// 会被规则一改写、首尾帧被降级成参考图。
		//
		// 目标模型按**源模型**取：活动版可以单独指向自己的首尾帧目标，
		// 母本仍走原来的 wan3.0-720p-特惠。
		framesTarget := policy.FramesTargetModelFor(sourceModel)
		if seedanceroute_setting.IsFramesRouteEnabledFor(sourceModel) && hasFrameMedia(body) {
			if newBody, routed := prepareFramesRoute(c, body, policy, framesTarget); routed {
				if applyRewrittenBody(c, newBody, sourceModel) {
					logger.LogInfo(c, fmt.Sprintf(
						"条件路由：命中首尾帧，改派 %s → %s",
						sourceModel, framesTarget))
					c.Next()
					return
				}
			}
		}

		// ── 规则一（时长）──
		//
		// 阈值是**严格大于** TriggerSeconds（生产配 20 秒），不再要求恰好等于 30 秒。
		// 成本动机：源链路按秒计价（¥0.20/秒），目标 seedance-2-5 按次计费 ——
		// 21 秒以后源链路成本已超过目标的按次成本，非等到 30 秒整才改派纯属亏钱。
		// ⚠️ 20 秒这个阈值是 2026-10-30 按豆包官网成本口径拍板的；2026-11-12
		// 常驻渠道在「班车 ch27 ↔ 豆包 ch20」之间来回搬过一趟，阈值始终没按
		// 搬运后的上游成本重算（要调只改 seedanceroute_setting 的
		// trigger_seconds，不用改代码）。
		//
		// 目标模型按**源模型**取：两个源模型的**兜底目标都是 seedance-2-5**，
		// 但它们是两条不同的商品：
		//   - 母本 seedance2.5-特惠（按秒 ¥0.20/秒）：常驻渠道 17 有赞，
		//     超过阈值改派 seedance-2-5（按次 ¥3.50 对外；当前上游为豆包
		//     官网 ch20，成本以渠道台账为准）；
		//   - 活动版 seedance2.5-活动（按次 ¥4.50/次）：常驻渠道 17 有赞
		//     （2026-11-11 从心笔切回原生 720P），超过阈值也改派 seedance-2-5 ——
		//     把长时长的量导到按次目标上，既省源链路的按秒成本，也与母本
		//     共用同一条目标链路。
		//
		// 两者的计费口径不同，所以仍按源模型分别取目标，不能共用一个字段。
		secondsTarget := policy.TargetModelFor(sourceModel)
		if seedanceroute_setting.IsSecondsRuleEnabledFor(sourceModel) {
			if seconds, hasSeconds := extractRequestSeconds(body); hasSeconds && policy.ExceedsTriggerSeconds(seconds) &&
				secondsRatioGatePasses(body, policy.SecondsRuleRatioGate) {
				if newBody, routed := prepareSecondsRoute(c, body, policy, secondsTarget, seconds); routed {
					if applyRewrittenBody(c, newBody, sourceModel) {
						// 计费按源模型（按秒）算，而目标 adaptor 是按次的、不给 seconds。
						// 把判定命中的秒数记下来，交给 relay_task.go 补进 OtherRatios。
						// ⚠️ 记的是客户**请求时填的秒数**；2026-11-09 起下发值也是同一个
						// 秒数（不再吸附档位），不会出现「请求 23 秒、出片 30 秒」。
						common.SetContextKey(c, constant.ContextKeyRouteBillingSeconds, float64(seconds))
						// 这条日志是排查「请求到底被哪条规则改派」的唯一凭据：
						// 改派后下游只能看到目标模型名，不打日志就分不清是
						// 「时长超阈值」还是「故障转移」把它送过去的。
						logger.LogInfo(c, fmt.Sprintf(
							"条件路由：命中时长 %d 秒（超过阈值 %d 秒），改派 %s → %s",
							seconds, policy.TriggerSeconds, sourceModel, secondsTarget))
						// 规则五的第二段：目标链路本身也不可用（渠道被关 /
						// 额度探针标记）且源与备胎不同渠道时，继续降级到备胎。
						// 迁移后源备同渠道时这一段被 failoverSharedChannelChain
						// 拦住，请求停在规则一的改写结果上。
						//
						// ⚠️ 计费原点必须是 **sourceModel**（客户最初请求的促销族模型），
						// 不能是 secondsTarget —— 客户买的是 seedance2.5-特惠，
						// 降级后仍按它计费（¥0.20/秒），不是按目标模型的按次价。
						if failRouted, _ := tryFailoverAfterDoubaoRoute(c, newBody, sourceModel); failRouted {
							c.Next()
							return
						}
						c.Next()
						return
					}
				}
			}
		}

		// ── 规则六（短时长分流）：排在规则二之前 ──
		//
		// 2026-11-13 老板定：请求时长 ≤ 阈值（默认 10 秒）→ 改派到**心笔内链**
		// （wan3.0-720p-特惠-内链，ch25：480P 生成 + 超分交付 720P）。
		//
		// 为什么目标从有赞标准版换成心笔：上游 prime「暂时不受理低于 10 秒」
		// 的任务（PRIME_DURATION_TOO_SHORT），客户 threeFatAi 一天连续 5 次失败，
		// 每次都是「预扣 → 上游拒绝 → 退款」。心笔内链 480P 生成、统一超分到
		// 720P 交付，与源链路承诺的交付档一致。
		//
		// 为什么排在规则二（比例）之前：老板口径是「短任务优先」——
		// 9:16 的短任务也走内链，不再落到比例目标链路。
		//
		// 为什么排在规则三（首尾帧）之后：首尾帧是**能力约束**，心笔兼容口
		// 只收参考图。带首尾帧的请求只有规则三的目标链路能跑，优先级高于分流优化。
		//
		// 与规则一的时长区间不重叠（≤10 与 >阈值 20），两条规则不会互相抢单；
		// 万一将来把阈值调成重叠，规则一在前 —— 长时长优先仍然是既有口径。
		shortTarget := policy.ShortTargetModelFor(sourceModel)
		if seedanceroute_setting.IsShortRouteEnabledFor(sourceModel) {
			if seconds, hasSeconds := extractRequestSeconds(body); hasSeconds && policy.WithinShortTrigger(seconds) {
				// 目标只支持心笔内链。改派跨 adaptor（youzanwan3 → xinbiapi），
				// 入参契约完全不同，必须走 prepareTehuiXinbiRoute 的准入与改写；
				// 配置成别的模型名时宁可不用这条规则，也不能按有赞契约改写。
				if !isXinbiTehuiTarget(shortTarget) {
					logger.LogInfo(c, "条件路由跳过短时长分流：short_target_model 不是心笔内链，规则六不生效")
				} else {
					if tier := sourceResolutionTier(body); tier == "1080P" {
						// 不挡，但要留痕：心笔内链只交付 720P，且按 720P 计价。
						// 降档交付是可观测的，不是静默的（与规则三同口径）。
						logger.LogInfo(c, "条件路由：短时长请求指定 1080P，心笔内链只交付 720P（按 720P 计价）")
					}
					if newBody, routed := prepareTehuiXinbiRouteWithSeconds(c, body, shortTarget, seconds); routed {
						if applyRewrittenBody(c, newBody, sourceModel) {
							// 计费按**源模型**（seedance2.5-特惠）的按秒价算。
							// 心笔 adaptor 会自己给 seconds（读改写后的顶层 seconds），
							// 这里再记一份是兜底：GetTaskRequest 万一失败，它的
							// EstimateBilling 会退到缺省 5 秒，applyRouteBillingSeconds
							// 能用这份把秒数补回来。size 倍率**不记** —— 交付恒 720P。
							common.SetContextKey(c, constant.ContextKeyRouteBillingSeconds, float64(clampTehuiXinbiSeconds(seconds)))
							logger.LogInfo(c, fmt.Sprintf(
								"条件路由：命中短时长 %d 秒（≤ %d 秒），改派 %s → %s（480P 生成 + 超分 720P）",
								seconds, policy.ShortTriggerSecondsOrDefault(), sourceModel, shortTarget))
							c.Next()
							return
						}
					}
				}
			}
		}

		// ── 规则二（比例）：前面几条都没命中才走这里 ──
		//
		// 目标模型按源模型取，与规则一/三同口径。
		//
		// 目标链路有两种形状，改写逻辑必须分开（2026-11-11）：
		//   · wan3.0-720p-特惠（Wan3 官网 ch12）：入参契约与源链路同源，
		//     换模型名 + 把比例落进 metadata.ratio 即可；
		//   · wan3.0-720p-特惠-内链（心笔 ch25，480P 生成 + 720P 交付）：
		//     兼容口只认顶层 reference_images[] / metadata.ratio / seconds，
		//     必须复用特惠→心笔分流的准入检查与改写，否则参考图会被静默丢弃。
		//
		// 生产 9:16 配的是后者：直连心笔，不再经 Wan3 官网两跳。
		ratioTarget := policy.RatioTargetModelFor(sourceModel)
		if seedanceroute_setting.IsRatioRouteEnabledFor(sourceModel) {
			if matchesTriggerRatio(body, policy.TriggerRatio) {
				var newBody map[string]interface{}
				var routed bool
				if isXinbiTehuiTarget(ratioTarget) {
					newBody, _, routed = prepareTehuiXinbiRoute(c, body, ratioTarget)
				} else {
					newBody, routed = prepareWan3Route(c, body, policy, ratioTarget)
				}
				if routed && applyRewrittenBody(c, newBody, sourceModel) {
					logger.LogInfo(c, fmt.Sprintf(
						"条件路由：命中比例 %s，改派 %s → %s",
						policy.TriggerRatio, sourceModel, ratioTarget))
					c.Next()
					return
				}
			}
		}

		c.Next()
	}
}

// doubaoTargetModel 是规则一的目标模型。
//
// 常驻渠道搬过两趟：豆包官网 ch20 → 视频生成车站 ch27（2026-11-12 白天）
// → 豆包官网 ch20（2026-11-12 21:50 回迁）。当前是豆包官网 ch20 ——
// doubao 这个名字是历史遗留，但当前口径与名字是对得上的。
const doubaoTargetModel = "seedance-2-5"

// isDoubaoTarget 判断目标模型是不是规则一的目标（seedance-2-5）。
//
// 唯一的使用方是链路后一道 Wan3PromptCompress：它按**字节预算**决定压不压
// （预算内放行，超预算压缩 —— 否则建单时被上游 400）。
//
// 字节预算取 failoverMaxPromptLength（20000），比豆包官网的 20000 **字符**
// 上限更严格 —— 中文每字 3 字节，约 6667 字才到预算，超出就压缩。
// 方向是偏保守的：宁可多压一次（压到 ≤5000 字符必然安全），
// 也不放行注定被上游 400 的提示词。
//
// 故障转移改派到班车（wan3.0-班车）时 body.model 已经不是本模型，
// 走的是 wan3 的 5000 字符分支，两条链路上限各自生效。
// 规则四（超长改派豆包）删除后，条件路由本身不再调用它。
func isDoubaoTarget(targetModel string) bool {
	return trimSpace(targetModel) == doubaoTargetModel
}

// perCallFixedTierSeconds 是按次档的**默认时长**（秒）。
//
// ⚠️ 它是 constant.Seedance25PerCallFixedSeconds 的**派生**（不是副本）——
// 源链路（youzanwan3 的 perCallFixedDuration）也从同一个常量派生。
// 两处必须一致：源链路用它补「客户没传」时的默认值，路由层用它做归一。
//
// 2026-11-11 起活动版时长放开为 2-30 秒逐秒可选（售价恒 ¥4.5/条），
// 所以这里不再是「唯一合法时长」，只表达「没传时算 30 秒」。
// 为什么放 constant 包而不是各自定义：常量语义属于「能力定义」，
// 而能力定义的唯一来源就是 constant.Seedance25Family 所在的文件。
// 之前这里是手写的 `= 30`，与 youzanwan3 的 `= 30` 各写一份 ——
// 任何一边单独改动都不会编译失败，只会让「按 30 秒卖」和
// 「实际跑多久」在客户侧悄悄脱节。派生关系把这个失败模式彻底消除。
const perCallFixedTierSeconds = constant.Seedance25PerCallFixedSeconds

// 按次档允许的时长区间。2026-11-11 起活动版与 wan3.0-720p-按次
// 都放开 2–30 秒（此前锁死 30 秒），按次价格仍不随时长变化。
const (
	perCallMinSeconds = 2
	perCallMaxSeconds = 30
)

// normalizeFixedTierSeconds 把按次档请求的时长归一成默认值。
//
// 返回值：
//   - (body, true)：时长在合法区间 2–30 秒内（没传 → 已归一成默认值）；
//   - (nil, false)：显式传了越界时长 → 放弃路由。
//
// 为什么「没传」要归一而不是放弃：
//
//	源链路（youzanwan3）的按次档语义就是「不传 → 30 秒」。如果路由层放弃，
//	活动版未传时长的请求就走不到 30 秒规则（那条规则的成本优势 ¥4.50）——
//	那是**少赚**而不是报错。归一后才能正确命中规则一。
//
// 为什么「显式传其它时长」要放弃：
//
//	2026-11-11 起活动版支持 2-30 秒逐秒可选，传 10 秒是**合法请求**，
//	不该被改派到规则一目标（seedance-2-5，只收 >阈值 的长片），也不该被归一成 30 秒
//	（那是静默改档：客户要 10 秒的片、拿到 30 秒的片，或按 30 秒的价收钱）。
//	放行原请求让它走源链路（有赞支持 2-30 秒逐秒），价格仍是 ¥4.5/条。
//
// ⚠️ 归一时**必须写回请求体**：改派后目标链路是 wan3（缺省 5 秒）或
// seedance-2-5（时长原样下发，不改写）。不写回 seconds，客户会「按 30 秒的价拿到 5 秒的片」。
func normalizeFixedTierSeconds(body map[string]interface{}) (map[string]interface{}, bool) {
	seconds, hasSeconds := extractRequestSeconds(body)
	if hasSeconds && (seconds < perCallMinSeconds || seconds > perCallMaxSeconds) {
		return nil, false
	}
	if !hasSeconds {
		// 没传时长：复制一份 body 并写入固定时长。
		//
		// 为什么要复制：body 可能是读出来的原始 map，改它会影响后续
		// 「请求体逐字节不变」的兜底路径（放行原请求时必须原样）。
		out := make(map[string]interface{}, len(body)+1)
		for key, value := range body {
			out[key] = value
		}
		out["seconds"] = strconv.Itoa(perCallFixedTierSeconds)
		return out, true
	}
	return body, true
}

// readJSONRequestBody 读出请求体并解析成 map。
//
// 失败返回 false，调用方放行原请求 —— 解析不了就不该猜，更不该改写。
func readJSONRequestBody(c *gin.Context) (map[string]interface{}, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, false
	}
	var body map[string]interface{}
	if err := common.Unmarshal(raw, &body); err != nil {
		return nil, false
	}
	return body, true
}

// applyRewrittenBody 把改写后的 body 换回 context 与 Request.Body。
//
// 失败返回 false（调用方放行原请求）。此时原 body 仍在 storage 里，
// 但 storage 的读位置可能已被 Bytes() 消费 —— GetBodyStorage 返回的
// storage 支持随机读，Bytes() 不改变语义；真正保险的是 apply 成功后才算路由成功。
func applyRewrittenBody(c *gin.Context, body map[string]interface{}, originModel string) bool {
	newRaw, err := common.Marshal(body)
	if err != nil {
		return false
	}
	newStorage, err := common.CreateBodyStorage(newRaw)
	if err != nil {
		return false
	}
	c.Set(common.KeyBodyStorage, newStorage)
	c.Request.Body = io.NopCloser(newStorage)
	c.Request.ContentLength = int64(len(newRaw))
	c.Set(routeOriginModelKey, originModel)
	return true
}

// prepareSecondsRoute 组装「规则一（时长）」的改派请求体。
//
// 目标模型按源模型取（可能与公共 TargetModel 不同）：
//   - seedance-2-5 目标（母本 seedance2.5-特惠、活动版 seedance2.5-活动，
//     当前常驻豆包官网 ch20）：素材/分辨率要转换，比例要改名 ——
//     改写产物仍是扁平别名形态（input_reference / 顶层 ratio / 秒数），
//     manwu 的 buildMedia / resolveRatio 兼容这批别名，可直接下发；
//   - 心笔按次链路（wan3.0-720p-按次）：本改派只转参考图；带首尾帧/
//     参考视频/参考音频/seed 的请求留在源链路，时长 2–30 秒（默认 30）、只交付 720P。
//
// ⚠️ 2026-11-03 活动版曾把常驻链路换到心笔，2026-11-11 又切回有赞原生 720P
// （老板口径：有赞原生 720P 效果更好）。现在母本与活动版**同渠道常驻**，
// 只剩「>阈值改派 seedance-2-5」一条差异；但两条链路的入参契约不同，
// 仍必须分开处理 —— 用同一套改写逻辑会让其中一条必然失败
// （历史上踩过：豆包官网链路不认 metadata.media → 丢图）。
func prepareSecondsRoute(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy, targetModel string, requestedSeconds int) (map[string]interface{}, bool) {
	if isXinbiPerCallTarget(targetModel) {
		// 心笔按次档 2026-11-11 起支持 2–30 秒，生成时长必须与客户请求一致。
		return prepareXinbiPerCallRoute(c, body, targetModel)
	}
	if !doubaoTargetUsable(c, body, targetModel) {
		return nil, false
	}
	// 2026-11-09：时长原样透传，不做任何吸附（用户口径：传多少秒就多少秒）。
	routeSeconds, ok := doubaoSecondsFor(requestedSeconds)
	if !ok {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：跳过 %d 秒改派（时长非法）", requestedSeconds))
		return nil, false
	}
	return rewriteBodyForDoubaoWithDuration(c, body, policy, targetModel, routeSeconds)
}

// xinbiPerCallTargetModel 是心笔（xinbiapi）的按次对外模型名。
//
// 它是 relay/channel/task/xinbiapi 的 ModelNamePayPerCall 的镜像。
// 中间件不能 import 那个包（会把 middleware 和 relay 渠道包绑在一起），
// 所以这里写一份常量，并在测试里断言两边一致。
const xinbiPerCallTargetModel = "wan3.0-720p-按次"

// isXinbiPerCallTarget 判断目标模型是不是心笔按次链路。
func isXinbiPerCallTarget(targetModel string) bool {
	return trimSpace(targetModel) == xinbiPerCallTargetModel
}

// prepareXinbiPerCallRoute 组装「改派到心笔按次链路」的请求体。
//
// 心笔链路（xinbiapi 兼容口 POST /newapi/v1/video/generations）的入参契约
// （2026-11-01 与 11-03 实测锁定）：
//
//	接受：model / prompt / duration / metadata{resolution,ratio} / reference_images[]
//	拒绝：first_frame / last_frame / reference_videos / reference_audios
//	      / media[] / input_reference / negative_prompt / seed
//
// 注意：心笔链路本体 2026-11-11 起已支持参考视频 / 参考音频（各限 5 个），
// 但**本改派函数**只转 reference_images。带音视频的请求继续留在源链路 ——
// 源链路（有赞：母本与活动版同渠道）都能跑，能力不受影响，只是不走这条低成本改派。
//
// 时长：心笔按次档同期放开 2–30 秒（默认 30），不再要求恰好 30 秒。
func prepareXinbiPerCallRoute(c *gin.Context, body map[string]interface{}, targetModel string) (map[string]interface{}, bool) {
	// 参考视频 / 参考音频：本改派只转 reference_images，留在源链路跑。
	if hasUnsupportedMedia(body) {
		logger.LogInfo(c, "条件路由跳过心笔按次：请求带参考视频/参考音频，本改派只转参考图")
		return nil, false
	}

	// 首尾帧：兼容口没有声明尾帧字段，留在源链路。
	if hasFrameMedia(body) {
		logger.LogInfo(c, "条件路由跳过心笔按次：请求带首尾帧，该链路只收参考图")
		return nil, false
	}

	// seed：上游 parameterSchema 是 additionalProperties:false，
	// 带 seed 会吃 UNKNOWN_MEDIA_PARAMETER（422）。心笔 adaptor 自己也会本地 400，
	// 但在路由层就挡掉更好 —— 留在源链路至少不会让客户看到「上游不认识这个字段」。
	if _, hasSeed := body["seed"]; hasSeed {
		logger.LogInfo(c, "条件路由跳过心笔按次：请求带 seed，该链路不支持")
		return nil, false
	}

	// 目标模型没有可用渠道 → 不路由。
	//
	// 与规则一（时长）同一理由：中间件改写 body 之后，重试只会用同一个模型名
	// 重选渠道，不会退回源模型。渠道挂掉时预检能让请求自然落回源链路。
	if !channelAvailabilityProbe(c, targetModel) {
		return nil, false
	}

	// 用户明确要了 1080P → 不路由。心笔按次只交付 720P。
	if wants1080P(body) {
		logger.LogInfo(c, "条件路由跳过心笔按次：用户要求 1080P，该链路只交付 720P")
		return nil, false
	}

	// 用户明确要了非 720P 的分辨率 → 同样不路由（心笔只交付 720P）。
	// 480P 需求被 720P 成片满足，不挡。
	if tier := explicitResolutionTier(body["resolution"]); tier != "" && tier != "720P" {
		logger.LogInfo(c, "条件路由跳过心笔按次：用户要求 "+tier+"，该链路只交付 720P")
		return nil, false
	}

	images, hasUnsupported := collectReferenceImages(body, true)
	if hasUnsupported {
		// hasUnsupportedMedia 已在更早拦掉参考视频/音频，能到这里说明
		// metadata.input.media 里的素材形式连 data URI 都不算（如本地路径）。
		logger.LogInfo(c, "条件路由跳过心笔按次：metadata.input.media 里有不可搬运的素材形式")
		return nil, false
	}
	// 参考图数量上限：心笔上游 mediaConstraints 限 10 张（2026-11-11 实测），
	// 与源链路（有赞）一致；超过时改派过去必被上游 400。
	if len(images) > xinbiMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过心笔按次：参考图 %d 张超过该链路上限 %d 张",
			len(images), xinbiMaxReferenceImages))
		return nil, false
	}

	return rewriteBodyForXinbiPerCall(body, targetModel), true
}

// xinbiMaxReferenceImages 是心笔链路的参考图张数上限。
//
// 2026-11-11 实测上游 mediaConstraints：reference_image max = 10（原先是 9），
// 与源链路（有赞）一致，因此 10 张的请求改派过去不再被拒。
const xinbiMaxReferenceImages = 10

// rewriteBodyForXinbiPerCall 组装改派到心笔按次链路的请求体。
//
// 与豆包规则的差别：心笔兼容口**认** metadata{resolution,ratio} 与
// duration，参考图认**顶层** images[]。
//
// ⚠️ 参考图必须写顶层 `images`，不能写上游 API 的 `reference_images`（2026-11-13
// 生产事故）：改写产物要先过 relay/common.TaskSubmitReq 解码，该结构没有
// reference_images 字段、自定义 UnmarshalJSON 也不处理 —— Go json 会把这个
// 字段静默丢弃，adaptor 一个图都收不到，上游建单 inputFiles 为空、任务照跑
// 照扣费、成片没有参考图。TaskSubmitReq 能解析且 xinbi adaptor 无条件收集的
// 顶层位置是 `images`（req.Images，data URI 与 https 直链都收）。
//
// 顶层 media / metadata.media 同理必须摘掉：xinbi adaptor 对 req.Media 与
// metadata 里的素材是**并行收集**，图已抬到顶层 images 后再留着就是同图投两次。
func rewriteBodyForXinbiPerCall(body map[string]interface{}, targetModel string) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		switch key {
		case "model", "metadata":
			// model 要换；metadata 整块丢掉，下面按心笔契约重写。
			continue
		case "resolution", "size":
			// 顶层分辨率心笔不认（它只认 metadata.resolution），
			// 而 size 在心笔那边是比例语义 —— 留着会被当成比例误读。
			// 用户明确要 1080P 的已在 prepareXinbiPerCallRoute 里挡掉。
			continue
		case "images", "image", "input_reference", "reference_images", "media":
			// 下面统一收成顶层 images[]，不在原位置留副本；
			// media / reference_images 漏剥会与 images 重复投递。
			continue
		case "seed":
			// 上游不认，已在 prepareXinbiPerCallRoute 里挡掉。
			continue
		}
		out[key] = value
	}
	out["model"] = targetModel

	// 参考图：统一收到顶层 images[]（见函数头注释，不能写 reference_images）。
	images, _ := collectReferenceImages(body, true)
	if len(images) > 0 {
		out["images"] = images
	}

	// 时长：保留客户请求的秒数（2–30 区间内），没传时落默认 30 秒。
	//
	// 2026-11-11 前这里写死 30 秒 —— 那时心笔按次只接受恰好 30 秒。
	// 上游放开 2–30 秒后仍写死会让「请求 22 秒」的客户拿到 30 秒的片，
	// 属于静默改档；按次售价与时长无关，没有理由不按客户要的时长生成。
	// 越界值不会走到这里：按次源已在 normalizeFixedTierSeconds 放弃路由，
	// 按秒源由源链路夹取到 2–30。
	seconds := perCallFixedTierSeconds
	if requested, ok := extractRequestSeconds(body); ok &&
		requested >= perCallMinSeconds && requested <= perCallMaxSeconds {
		seconds = requested
	}
	out["seconds"] = strconv.Itoa(seconds)

	// 比例：落进 metadata.ratio。
	// 心笔的 ratio 枚举是 adaptive/16:9/9:16/1:1/4:3/3:4 —— 与源链路同口径，
	// 不需要改名（豆包那条才要把 adaptive 映射成 auto）。
	metadata := map[string]interface{}{}
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			// ⚠️ **media / input 必须丢掉**：源链路把素材放在 metadata.media
			// 与 metadata.input.media（Wan3/ArcReel 形状），心笔的兼容口不认
			// 这些字段（上游 parameterSchema 是 additionalProperties:false，
			// 带上去会被拒）。参考图已经收进顶层 images[] 了，adaptor 对
			// req.Images 与 metadata 素材是并行收集 —— 留着就是同图投两次。
			// （规则六/特惠按秒的改写是整块重建 metadata，只有这里漏了。）
			if key == "media" || key == "input" {
				continue
			}
			metadata[key] = value
		}
	}
	if ratio, ok := resolveRequestRatio(body); ok {
		metadata["ratio"] = ratio
	}
	out["metadata"] = metadata

	return out
}

// trimSpace 去掉首尾空格。中间件里到处都要 Trim 模型名，
// 单独一个小工具比每次都写 strings.TrimSpace 更不容易漏。
func trimSpace(value string) string {
	return strings.TrimSpace(value)
}

// doubaoTargetUsable 是「规则一（时长）改派到 seedance-2-5」的前置检查。
//
// 素材、渠道可用性这两条与触发条件无关，是目标链路的固有约束。
// 任何一条漏判，结果都是「请求被改写到一条跑不了的链路上」，比不改派更糟。
func doubaoTargetUsable(c *gin.Context, body map[string]interface{}, targetModel string) bool {
	// 改写函数只搬参考图。带参考视频/音频时不路由 —— 走源模型继续跑，
	// 而不是报错打断客户（老板 2026-10-30 定的口径）。
	if hasUnsupportedMedia(body) {
		return false
	}

	// 分辨率：目标链路支持 480P/720P/1080P 三档全量（2026-11-09 确认），
	// 只要客户给的是这三档之一就照常改派，档位在 rewriteBodyForDoubaoWithDuration
	// 里抬到顶层透传。解析不出档位的值（如纯比例 "16:9"）本来就不是分辨率，
	// 交给下游按未指定处理 —— 这里不做分辨率挡板。

	// 目标模型没有可用渠道 → 不路由。
	//
	// 为什么必须有这一步：中间件改写了 body 的 model 字段，之后的重试
	// （shouldRetryTaskRelay）只会**用同一个模型名重选渠道**，不会退回源模型。
	// seedance-2-5 在生产当前只有豆包官网 ch20 一个渠道（2026-11-12 白天曾临时
	// 迁到视频生成车站 ch27，当晚 21:50 已回迁）—— 渠道不可用时 Distribute 直接返回 503
	//「当前分组下没有可用渠道」，客户撞错误。
	// 预检之后这种请求会自然落回源链路（有赞），客户完全无感。
	return channelAvailabilityProbe(c, targetModel)
}

// prepareWan3Route 组装「改派到 wan3.0-720p-特惠」的请求体。
//
// 与豆包规则的关键差别：wan3 渠道的入参契约与源链路**几乎一致**
// （buildWan3Media 的素材优先级同样是 顶层 media > metadata.media > images），
// 所以**不需要**转换参考图格式，也不需要摘掉 resolution / size。
func prepareWan3Route(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy, targetModel string) (map[string]interface{}, bool) {
	// 目标模型固定交付 720P（480P 生成 + 提升）。用户明确要 1080P 时做不到，
	// 路由过去等于降级交付 —— 宁可不路由，交回源链路按用户要的跑。
	if wants1080P(body) {
		return nil, false
	}

	if !channelAvailabilityProbe(c, targetModel) {
		return nil, false
	}

	// 参考视频挡板：wan3 比源链路严格，超限会在提交阶段 400。
	//
	// 两边的限制并不相同（2026-10-31 实测更正，此前以为源链路不限时长）：
	//	· 源链路 youzanwan3：单条 ≤15 秒（上游原文「参考素材时长过长：当前为
	//	  31.609 秒，最长允许 15 秒」），但**不限制「参考+生成」总量**；
	//	· 目标链路 wan3：单条 ≤15 秒、总量 ≤15 秒，且「输入视频总时长 + 输出
	//	  视频时长 ≤30 秒」。
	//
	// 所以这里的价值集中在后两条：单条超 15 秒时两边都会拒，改不改派都失败，
	// 但「参考+生成 >30 秒」只有 wan3 拒 —— 挡掉它，请求落回源链路还能跑成功。
	// 生产实例：2026-10-31 01:10，logs id=9379，参考视频 32.39 秒。
	//
	// 代价：带参考视频的 9:16 请求会多一次视频下载。moov 在头部时几十 KB 即可
	// 拿到 mvhd，但在尾部时要下全文件（探测上限 64MB）—— 所以 wan3 包给单条
	// 10 秒、整组 30 秒的超时预算，超时按「预检不通过」处理，交回源链路。
	// 不带参考视频的请求（绝大多数）不触发，零开销。
	if outOfRange, reason := wan3ReferenceVideosOutOfRange(body); outOfRange {
		logger.LogInfo(c, "条件路由跳过 wan3，交回源链路："+reason)
		return nil, false
	}

	return rewriteBodyForWan3(body, policy, targetModel), true
}

// prepareTehuiXinbiRouteWithSeconds 与 prepareTehuiXinbiRoute 相同，但秒数由调用方给。
//
// 为什么需要这个变体：规则六（短时长分流）在触发判定时用 extractRequestSeconds
// 按源链路口径解析过秒数（含 metadata.seconds / metadata.duration），
// 而 tehuiXinbiSeconds 只读顶层 duration / seconds —— 直接复用的话，
// 「metadata.seconds=8」的请求会被改写成缺省 5 秒：客户要 8 秒拿到 5 秒，
// 还按 8 秒收钱，是实打实的多扣。
func prepareTehuiXinbiRouteWithSeconds(c *gin.Context, body map[string]interface{}, targetModel string, seconds int) (map[string]interface{}, bool) {
	if !tehuiXinbiCompatible(c, body, targetModel) {
		return nil, false
	}
	return rewriteBodyForXinbiPerSecond(body, targetModel, clampTehuiXinbiSeconds(seconds)), true
}

// sourceResolutionTier 按**源链路（youzanwan3）的读取顺序**解析分辨率档位：
// 顶层 resolution → metadata.resolution → 顶层 size。
//
// 为什么不能直接用 doubaoResolutionTier：它的顺序是
// 顶层 resolution → 顶层 size → metadata.resolution，
// 在「顶层 size 与 metadata.resolution 同时存在且不一致」时会取错档 ——
// 而我们要报的是「源链路会跑哪一档」，口径必须与 adaptor 逐字一致。
func sourceResolutionTier(body map[string]interface{}) string {
	if tier := explicitResolutionTier(body["resolution"]); tier != "" {
		return tier
	}
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		if tier := explicitResolutionTier(metadata["resolution"]); tier != "" {
			return tier
		}
	}
	return explicitResolutionTier(body["size"])
}

// prepareFramesRoute 组装「带首尾帧 → wan3 直连」的请求体。
//
// 第二个返回值 false = 放弃路由（放行原请求）。
//
// 与规则二（比例）的关键差别：**不改比例**。规则二是「把请求钉成 9:16」，
// 而首尾帧请求的画幅由用户自己定（16:9 / 9:16 / 1:1 都可能），
// 这里只把能解析到的比例**原样落到 metadata.ratio**，不能覆盖成触发值。
//
// 为什么不做 1080P 挡板（规则二有）：wan3.0-720p-特惠 只交付 720P，用户要 1080P
// 时路由过去等于降级。但源链路对带首尾帧的请求是**必然 400**
// （youzan wan3 no longer supports first_frame/last_frame），
// 挡下来只会把「降级出片」换成「彻底失败」—— 两害相权，路由过去并记日志。
func prepareFramesRoute(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy, targetModel string) (map[string]interface{}, bool) {
	if !channelAvailabilityProbe(c, targetModel) {
		return nil, false
	}
	if wants1080P(body) {
		// 不挡，但要留痕：降级交付是可观测的，不是静默的。
		logger.LogInfo(c, "条件路由：首尾帧请求指定 1080P，目标链路只交付 720P")
	}
	return rewriteBodyForFramesRoute(body, targetModel), true
}

// rewriteBodyForFramesRoute 只把 model 换成目标模型，其余字段原样保留。
//
// 为什么不像 rewriteBodyForWan3 那样强制写 metadata.ratio：那条规则要把请求
// 钉成触发比例（9:16），而首尾帧规则没有触发比例 —— 覆盖用户画幅是错的。
func rewriteBodyForFramesRoute(body map[string]interface{}, target string) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+1)
	for key, value := range body {
		if key == "model" {
			continue
		}
		out[key] = value
	}
	out["model"] = target

	// 比例：源链路 youzanwan3 与目标链路 wan3 都只认 metadata.ratio /
	// metadata.aspect_ratio，顶层同名字段会被静默丢弃。用户若把比例写在顶层
	// （或只给了 size），改派后就会丢画幅 —— 这里把它落进 metadata.ratio。
	// 解析不到就什么都不写，让 wan3 用自己的默认值，绝不凭空造一个比例。
	if ratio, ok := resolveRequestRatio(body); ok {
		metadata := make(map[string]interface{})
		if existing, ok := body["metadata"].(map[string]interface{}); ok {
			for key, value := range existing {
				metadata[key] = value
			}
		}
		metadata["ratio"] = ratio
		out["metadata"] = metadata
	}
	return out
}

// hasFrameMedia 判断请求里是否带了首尾帧（media[].type = first_frame / last_frame）。
//
// 读取优先级与源链路 youzanwan3 的 buildMedia 完全一致：
// 顶层 media > metadata.media > metadata.input.media。
//
// **不看旧版扁平字段**（images / image / input_reference）：它们只能表达参考图，
// 在源链路里一律被当成 reference_image，表达不了首尾帧。
func hasFrameMedia(body map[string]interface{}) bool {
	for _, item := range collectMediaItems(body) {
		switch strings.TrimSpace(item.Type) {
		case "first_frame", "last_frame":
			return true
		}
	}
	return false
}

// mediaItem 只解析判定需要的 type 字段。
//
// 刻意不 import relay/common 的 TaskMedia：中间件只需要类型名，
// 引进来会把 middleware 和 relay 包的依赖方向绑得更紧（也更容易撞 import 环）。
type mediaItem struct {
	Type string `json:"type"`
}

// collectMediaItems 按源链路的优先级取出 media 列表。
//
// 解析失败返回 nil（调用方视为「没有首尾帧」→ 不路由），与整个中间件
// 「任何一步不确定就放弃路由」的原则一致。
//
// ⚠️ 逐条按 interface{} 解析而不是直接 unmarshal 成 []mediaItem：media 数组里
// **允许混入裸字符串**（等价于 reference_image）。若整体反序列化成结构体切片，
// 一条字符串就会让整组解析失败 → 漏判首尾帧 → 带首尾帧的请求被打回源链路必挂。
func collectMediaItems(body map[string]interface{}) []mediaItem {
	raw, ok := body["media"]
	if !ok || raw == nil {
		metadata, _ := body["metadata"].(map[string]interface{})
		if metadata == nil {
			return nil
		}
		raw, ok = metadata["media"]
		if !ok || raw == nil {
			if input, inputOK := metadata["input"].(map[string]interface{}); inputOK {
				raw = input["media"]
			}
		}
	}
	if raw == nil {
		return nil
	}
	encoded, err := common.Marshal(raw)
	if err != nil {
		return nil
	}
	var entries []interface{}
	if err := common.Unmarshal(encoded, &entries); err != nil {
		return nil
	}
	items := make([]mediaItem, 0, len(entries))
	for _, entry := range entries {
		// 裸字符串（或任何非对象条目）表达不了首尾帧，跳过即可，不影响其它条目。
		object, isObject := entry.(map[string]interface{})
		if !isObject {
			continue
		}
		kind, _ := object["type"].(string)
		items = append(items, mediaItem{Type: kind})
	}
	return items
}

// wan3ReferenceVideosOutOfRange 判断请求里的参考视频是否超出 wan3 的硬限制。
//
// 返回 true 表示**不能改派**（交回源链路），reason 是给日志看的原因。
//
// 四个失败都按「不改派」处理 —— 与整个中间件的失败原则一致：
// 任何一步不确定就放弃路由，绝不挡客户。这里尤其如此：
// 探测失败意味着无法判断会不会超限，而改派过去一旦超限就是一次必然的 400。
func wan3ReferenceVideosOutOfRange(body map[string]interface{}) (bool, string) {
	urls := collectReferenceVideos(body)
	if len(urls) == 0 {
		return false, ""
	}
	_, realSeconds, err := wan3.ProbeReferenceVideos(urls)
	if err != nil {
		// 单条超 15 秒也走这里：probeReferenceVideoSeconds 内部已校验单条上限，
		// 超限时返回的是 "exceeds the 15s per-video limit" 错误，不是网络错误。
		// 所以文案用中性表述，具体原因由 err 带出 —— 写成「探测失败」会让运维
		// 以为是下载不通，实际多半是视频本身超限。
		return true, "参考视频预检不通过：" + err.Error()
	}
	if realSeconds > float64(wan3.MaxReferenceVideoTotalSeconds)+routeDurationEpsilon {
		return true, fmt.Sprintf("参考视频合计 %.2f 秒超过 %d 秒上限",
			realSeconds, wan3.MaxReferenceVideoTotalSeconds)
	}
	generation := routeGenerationSeconds(body)
	if realSeconds+float64(generation) > float64(wan3.MaxTotalDurationWithVideo)+routeDurationEpsilon {
		return true, fmt.Sprintf("参考视频 %.2f 秒 + 生成 %d 秒超过 %d 秒上限",
			realSeconds, generation, wan3.MaxTotalDurationWithVideo)
	}
	// 生成时长上限与**源链路**对齐（老板 2026-10-31 定：有参考视频时生成也要 ≤15 秒）。
	//
	// 为什么必须在这里也拦：源链路 youzanwan3 有这条限制，而改派目标链路 wan3 只有
	// 「输入+输出 ≤30 秒」。不拦就会出现「同一份素材、同样参数，16:9 被源链路拒、
	// 9:16 改派后能跑」—— 客户看到的是「这个模型时好时坏」。
	//
	// 拦下来之后交回源链路：源链路的 validate 阶段会给出中文提示
	//「有参考视频时生成时长不能超过 15 秒」，客户能看懂该改什么。
	if generation > youzanwan3.MaxGenerationSecondsWithVideo {
		return true, fmt.Sprintf("有参考视频时生成 %d 秒超过 %d 秒上限",
			generation, youzanwan3.MaxGenerationSecondsWithVideo)
	}
	return false, ""
}

// routeDurationEpsilon 是浮点比较容差，与 wan3 包内部的 durationEpsilon 同值。
//
// mvhd 的 duration/timescale 除法会带出 15.0000000001 这种尾巴，
// 严格大于会把「刚好 15 秒」误判成超限。
const routeDurationEpsilon = 1e-6

// routeGenerationSeconds 取请求的**生成时长**，口径与 wan3 的 resolveDuration 一致。
//
// 必须钳制：不传时长时上游默认 5 秒（不是 0，也不是 30）。用 0 会让
// 「参考视频 15 秒 + 默认 5 秒 = 20 ≤ 30」这类本该放行的请求被误挡；
// 用 30 会让所有带参考视频的请求都被挡。
func routeGenerationSeconds(body map[string]interface{}) int {
	const (
		routeMinDuration     = 2
		routeMaxDuration     = 30
		routeDefaultDuration = 5
	)
	seconds, ok := extractRequestSeconds(body)
	if !ok || seconds <= 0 {
		return routeDefaultDuration
	}
	if seconds < routeMinDuration {
		return routeMinDuration
	}
	if seconds > routeMaxDuration {
		return routeMaxDuration
	}
	return seconds
}

// collectReferenceVideos 收集请求里的参考视频 URL 列表。
//
// ⚠️ 必须与 wan3 的 buildWan3Media **逐条对齐**，否则挡板会判错方向。
// buildWan3Media 的优先级是：
//
//	顶层 media 非空      → 只看顶层（哪怕里面全是图片）
//	否则 metadata.media 非空 → 只看 metadata
//	否则 legacy images / image / input_reference，按地址扩展名推断类型
//
// 写成「顶层 media 里有视频才看顶层」是错的：顶层 media 只有图片、
// metadata.media 里挂着视频时，实际提交给 wan3 的只有那些图片（视频根本不发），
// 我们却会去探测 metadata 里的视频，把一条本来合规的请求挡掉。
//
// 反过来漏掉 legacy 分支同样危险：客户用 images:["a.mp4"] 传视频时，
// buildWan3Media 会把它推断成 reference_video 发给上游，这里却看不见它 ——
// 挡板失效，超长视频照样被改派到 wan3 撞 400（就是这次要修的那个故障）。
func collectReferenceVideos(body map[string]interface{}) []string {
	if media, ok := body["media"].([]interface{}); ok && len(media) > 0 {
		return referenceVideosFromMedia(media)
	}
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if media, ok := metadata["media"].([]interface{}); ok && len(media) > 0 {
			return referenceVideosFromMedia(media)
		}
	}
	return legacyReferenceVideos(body)
}

// legacyReferenceVideos 按 buildWan3Media 的 legacy 规则挑出视频地址：
// images 优先，其次 image，再次 input_reference；类型由地址推断。
func legacyReferenceVideos(body map[string]interface{}) []string {
	legacy := stringListValue(body["images"])
	if len(legacy) == 0 {
		legacy = stringListValue(body["image"])
	}
	if len(legacy) == 0 {
		legacy = stringListValue(body["input_reference"])
	}
	var videos []string
	for _, url := range legacy {
		if isLegacyVideoURL(url) {
			videos = append(videos, url)
		}
	}
	return videos
}

// stringListValue 把字符串或字符串数组统一成 []string（丢掉空串）。
func stringListValue(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case []interface{}:
		var out []string
		for _, item := range typed {
			out = append(out, stringListValue(item)...)
		}
		return out
	}
	return nil
}

// isLegacyVideoURL 与 wan3 的 inferLegacyMediaType 同口径：
// 只认 data:video/ 前缀与 .mp4 / .mov 后缀（忽略查询串）。
func isLegacyVideoURL(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	if strings.HasPrefix(lower, "data:video/") {
		return true
	}
	path := strings.Split(lower, "?")[0]
	return strings.HasSuffix(path, ".mp4") || strings.HasSuffix(path, ".mov")
}

// referenceVideosFromMedia 从一个 media 数组里挑出 reference_video 的地址。
func referenceVideosFromMedia(value interface{}) []string {
	media, ok := value.([]interface{})
	if !ok {
		return nil
	}
	var videos []string
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := entry["type"].(string)
		if strings.TrimSpace(mediaType) != "reference_video" {
			continue
		}
		if url, ok := entry["url"].(string); ok && strings.TrimSpace(url) != "" {
			videos = append(videos, strings.TrimSpace(url))
		}
	}
	return videos
}

// channelAvailabilityProbe 判断「目标模型在当前分组下是否还有可用渠道」。
//
// 声明成变量而不是直接调 hasAvailableChannel，**只为测试可替换**：
// 真实实现要读渠道缓存（group2model2channels）或查数据库，
// 单测里没有这些环境，硬造一个渠道缓存会把测试和全局状态耦合在一起。
// 生产代码路径永远走默认值 hasAvailableChannel，没有任何分支。
var channelAvailabilityProbe = hasAvailableChannel

// hasAvailableChannel 预检「该模型在当前分组下是否还有可用渠道」。
//
// 用 GetRandomSatisfiedChannel 而不是真的选中渠道：它只读缓存
// （group2model2channels）并按权重挑一个，不占用负载计数，可以安全地当探针用。
// 返回 nil 且无错误 = 该模型没有可用渠道。
//
// ⚠️ 传 requestPath 是故意的：渠道支持哪些路径也是可用性的一部分
// （filterChannelsByRequestPath），只按模型名判断会高估可用性。
func hasAvailableChannel(c *gin.Context, modelName string) bool {
	if strings.TrimSpace(modelName) == "" {
		return false
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "" {
		group = "default"
	}
	channel, err := model.GetRandomSatisfiedChannel(group, modelName, 0, c.Request.URL.Path)
	if err != nil {
		return false
	}
	return channel != nil
}

// channelIDsProbe 返回「模型在当前分组下可被路由到的渠道 ID 集合」。
//
// 与 channelAvailabilityProbe 同族、同理由：声明成变量只为测试可替换
// （真实实现读渠道缓存/数据库，单测没有这些环境）。
// 生产代码路径永远走默认值 channelIDsForModel。
var channelIDsProbe = channelIDsForModel

// channelIDsForModel 是 channelIDsProbe 的默认实现：分组口径与
// hasAvailableChannel 完全一致（ContextKeyUsingGroup，缺省 default），
// 保证「有没有渠道」与「有哪些渠道」两个探针不会互相矛盾。
func channelIDsForModel(c *gin.Context, modelName string) []int {
	if strings.TrimSpace(modelName) == "" {
		return nil
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "" {
		group = "default"
	}
	return model.GetChannelIDsForModel(group, modelName, c.Request.URL.Path)
}

// HasAvailableChannel 是 hasAvailableChannel 的对外版本，供管理后台
// 展示「源链路当前还有没有渠道」。
//
// 为什么后台需要它：故障转移的静态触发条件是「源模型在当前分组下没有
// 启用中的渠道」。运营在后台看到的必须与中间件判定的是**同一个事实**，
// 否则会出现「后台显示源链路正常、请求却全被改派」这种无法解释的状态。
// 复用同一个实现才能保证两边一致。
//
// ⚠️ 分组口径：后台请求走的是管理员的分组（通常是 default），
// 而客户请求可能走别的分组 —— 所以这里给的是「管理员视角」的可用性，
// 仅供参考，不能当成全站结论。
func HasAvailableChannel(c *gin.Context, modelName string) bool {
	return hasAvailableChannel(c, modelName)
}

// extractRequestSeconds 从请求体里解析时长。
//
// 覆盖客户可能的所有写法（与 relaycommon.TaskSubmitReq 的自定义 UnmarshalJSON 同口径）：
//   - duration：数字或字符串
//   - seconds：数字或字符串
//   - metadata.duration / metadata.seconds：部分客户端塞 metadata
//
// 任一能解析出值就返回它。**解析不出返回 ok=false** —— 调用方据此判定「不命中」，
// 而不是当成某个默认值（不传时长时上游默认 5 秒，不是 30 秒，判定必须一致）。
func extractRequestSeconds(body map[string]interface{}) (int, bool) {
	if seconds, ok := parseSecondsValue(body["duration"]); ok {
		return seconds, true
	}
	if seconds, ok := parseSecondsValue(body["seconds"]); ok {
		return seconds, true
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	if seconds, ok := parseSecondsValue(metadata["duration"]); ok {
		return seconds, true
	}
	if seconds, ok := parseSecondsValue(metadata["seconds"]); ok {
		return seconds, true
	}
	return 0, false
}

// parseSecondsValue 把一个可能是数字、字符串或 nil 的值解析成秒数。
func parseSecondsValue(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		if seconds, err := typed.Int64(); err == nil {
			return int(seconds), true
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		if seconds, err := strconv.Atoi(trimmed); err == nil {
			return seconds, true
		}
	}
	return 0, false
}

// hasUnsupportedMedia 判断请求里有没有目标链路不支持的素材（参考视频 / 参考音频）。
//
// 有赞格式的素材在三处，type 取值：reference_image / reference_video / reference_audio：
//   - 顶层 media = [{type,url}]
//   - metadata.media = [{type,url}]
//   - metadata.input.media = [{type,url}]（ArcReel 的 Wan3 形状）
//
// 另外顶层 audio / audios 是**参考音频**，同样不被目标链路支持。
//
// 顶层 images / image / input_reference 之类的键是**图片**，不算不支持。
//
// ⚠️ 漏判的后果：参考视频/音频会被静默丢掉，任务照跑、照扣费，
// 但客户要的素材根本没进去 —— 比报错更糟。
func hasUnsupportedMedia(body map[string]interface{}) bool {
	// 顶层参考音频
	for _, key := range []string{"audio", "audios"} {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
		if value, ok := body[key].([]interface{}); ok && len(value) > 0 {
			return true
		}
	}

	// 顶层 media
	if media, ok := body["media"].([]interface{}); ok {
		for _, item := range media {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			mediaType, _ := entry["type"].(string)
			switch strings.TrimSpace(mediaType) {
			case "reference_video", "reference_audio":
				return true
			}
		}
	}

	// metadata.media / metadata.input.media
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	if hasUnsupportedMediaEntries(metadata["media"]) {
		return true
	}
	if input, ok := metadata["input"].(map[string]interface{}); ok {
		if hasUnsupportedMediaEntries(input["media"]) {
			return true
		}
	}
	return false
}

// hasUnsupportedMediaEntries 判断 media 数组里是否含规则一改写**不搬运**的
// 视频/音频（参考视频/参考音频）：改派只转参考图，带音视频的请求放行原链路
// 才不会静默丢素材（历史上的豆包官网链路同样不收）。
func hasUnsupportedMediaEntries(value interface{}) bool {
	media, ok := value.([]interface{})
	if !ok {
		return false
	}
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := entry["type"].(string)
		switch strings.TrimSpace(mediaType) {
		case "reference_video", "reference_audio":
			return true
		}
	}
	return false
}

// rewriteBodyForTarget 把请求体改写成目标模型能认的形状。
//
// 第二个返回值 false 表示放弃路由（调用方放行原请求）。
//
// 必须做对的三件事：
//  1. 参考图：源链路用 metadata.media=[{type,url}]，目标链路（漫屋）只认**顶层**
//     input_reference / images / image_url / reference_image 等一串别名，
//     **不含 metadata.media** —— 不转换就会静默丢图（2026-10-22 丢图事故同型）。
//     转换后**必须断言图数一致**，不一致就放弃路由。
//  2. 比例：源链路的自适应档叫 adaptive，目标链路叫 auto。
//  3. 分辨率：源链路支持 resolution（顶层或 metadata.resolution，取值 480P/720P/1080P），
//     目标链路**三档全支持**（480P/720P/1080P；2026-11-09 豆包官网确认）——
//     - 顶层 resolution 可以直接透传；
//     - 顶层 size 不能透传：漫屋把 size 当**比例**读
//     （firstNonEmpty(Size, Ratio, AspectRatio)），"1080P" 不在
//     allowedDoubaoRatios 里 → 400。所以 size 里表达的分辨率要**抬到顶层 resolution**；
//     - metadata.resolution / metadata.size 同理：必须抬到顶层；
func rewriteBodyForDoubaoWithDuration(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy, targetModel string, seconds int) (map[string]interface{}, bool) {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		// model 要换成目标模型；metadata 整块丢掉（源链路的素材/比例格式目标链路不认，
		// 逐个字段挑出来重写比整块保留更不容易出错）。
		// size 必须摘掉：漫屋把 size 当比例读，透传会被 400（分辨率已在下面单独抬出）。
		if key == "model" || key == "metadata" || key == "resolution" || key == "size" {
			continue
		}
		out[key] = value
	}
	out["model"] = targetModel

	// 分辨率统一抬到顶层 resolution：源链路的四个入口（顶层 resolution/size、
	// metadata.resolution/size）漫屋只认第一个，其余三个不抬就是静默丢参数。
	if tier := doubaoResolutionTier(body); tier != "" {
		out["resolution"] = strings.ToLower(tier)
	}

	// allowDataURL=false：漫屋只收公网 http(s) URL，data URI 参考图改派过去必 400。
	images, hasUnsupported := collectReferenceImages(body, false)
	if hasUnsupported {
		// 素材没法安全转成漫屋的 input_reference（参考视频/音频，或只能内联的
		// data URI 参考图）：明确放弃改派，让原请求继续走能处理它的源链路。
		// 绝不能像 2026-11-09 gen-7cdab… 那样整块丢 metadata 后继续提交。
		logger.LogInfo(c, "条件路由放弃改派豆包：素材无法安全转换（参考视频/音频或非 http(s) 参考图）")
		return nil, false
	}
	if len(images) > 0 {
		out["input_reference"] = images
	}

	// 时长原样下发：字符串形态最通用（2026-11-09 起不再吸附档位）。
	out["seconds"] = strconv.Itoa(seconds)

	if ratio, ok := extractRatio(body); ok {
		out["ratio"] = mapAdaptiveRatio(ratio)
	}
	return out, true
}

// rewriteBodyForWan3 组装改派到 wan3.0-720p-特惠 的请求体。
//
// 与豆包规则的差别：wan3 渠道的入参契约和源链路（有赞 Wan3）**同源**，
// 所以只做最小改动 —— 换模型名 + 把比例落到 metadata.ratio，
// 其余字段（media / resolution / size / prompt / seconds）原样保留。
//
// 为什么素材不用转换：relay/channel/task/wan3 的 buildWan3Media 优先级是
// 顶层 media > metadata.media > images/image/input_reference，
// 与 youzanwan3 的 buildMedia **完全一致**，原样传过去不会丢素材。
// （这与豆包规则相反 —— 豆包只认顶层 input_reference，必须转换。）
//
// 为什么分辨率不用摘：wan3 的 resolveGenerationResolution 对
// wan3.0-720p-特惠 强制 480P 生成（再提升到 720P），用户传 480P/720P
// 都不会报错也不会多花钱；1080P 已在 prepareWan3Route 里挡掉。
func rewriteBodyForWan3(body map[string]interface{}, policy seedanceroute_setting.Policy, targetModel string) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+1)
	for key, value := range body {
		if key == "model" {
			continue
		}
		out[key] = value
	}
	out["model"] = targetModel

	// 比例统一落到 metadata.ratio：wan3 的 resolveRatio 读 metadata.ratio /
	// metadata.aspect_ratio。统一写 ratio 是因为判定时可能命中的是
	// 顶层字段或 size 推断，而源链路只认 metadata 里的值。
	metadata := make(map[string]interface{})
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			metadata[key] = value
		}
	}
	metadata["ratio"] = policy.TriggerRatio
	out["metadata"] = metadata
	return out
}

// matchesTriggerRatio 判断请求里的比例是否命中触发值。
func matchesTriggerRatio(body map[string]interface{}, trigger string) bool {
	want := normalizeRatioText(trigger)
	if want == "" {
		return false
	}
	got, ok := resolveRequestRatio(body)
	if !ok {
		return false
	}
	return got == want
}

// secondsRatioGatePasses 规则一的比例闸门。
//
// gate 为空 = 不限比例（2026-11-12 之前的生产 JSON 没有该字段，
// 必须保持「任何比例超阈值都改派」的旧行为）。非空时只有比例命中
// 才放行 —— 没写比例的请求也**不放行**：客户没要求 9:16，
// 就不该替他改派到只对 9:16 效果好的链路上。
func secondsRatioGatePasses(body map[string]interface{}, gate string) bool {
	if strings.TrimSpace(gate) == "" {
		return true
	}
	return matchesTriggerRatio(body, gate)
}

// resolveRequestRatio 从请求里推断比例。
//
// 读取顺序与源链路一致：metadata.ratio / metadata.aspect_ratio 优先，
// 其次顶层同名字段，最后从 size（"720x1280" 这类）推断。
//
// 注意源链路 youzanwan3 只认 metadata 里的比例，顶层会被 Go 解码器静默丢弃；
// 这里多读顶层是**为了判定**（客户端真传了 9:16 就该算命中），
// 而不是为了让顶层值生效 —— 改写时统一写进 metadata.ratio。
func resolveRequestRatio(body map[string]interface{}) (string, bool) {
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := metadata[key].(string); ok {
				if ratio := normalizeRatioText(value); ratio != "" {
					return ratio, true
				}
			}
		}
	}
	for _, key := range []string{"ratio", "aspect_ratio"} {
		if value, ok := body[key].(string); ok {
			if ratio := normalizeRatioText(value); ratio != "" {
				return ratio, true
			}
		}
	}
	if value, ok := body["size"].(string); ok {
		if ratio, inferred := ratioFromSizeText(value); inferred {
			return ratio, true
		}
	}
	if metadata != nil {
		if value, ok := metadata["size"].(string); ok {
			if ratio, inferred := ratioFromSizeText(value); inferred {
				return ratio, true
			}
		}
	}
	return "", false
}

// normalizeRatioText 把 "9:16" / "9：16" / "9/16" 这类写法归一成 "9:16"。
//
// **不认** adaptive / auto 这类档位名（返回空）—— 它们不是具体比例，
// 不该被当成触发比例命中。
func normalizeRatioText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "：", ":")
	text = strings.ReplaceAll(text, "/", ":")
	parts := strings.Split(text, ":")
	if len(parts) != 2 {
		return ""
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ""
	}
	return strconv.Itoa(w) + ":" + strconv.Itoa(h)
}

// ratioFromSizeText 把 "1280x720" / "1280*720" 这类尺寸映射到最接近的比例。
//
// 误差超过 0.25 就认为不是常见比例（返回 false），避免把任意尺寸硬塞进某一档。
// 与 relay/channel/task/wan3 的 ratioFromSize 同口径。
func ratioFromSizeText(value string) (string, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), "*", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	candidates := []struct {
		name  string
		value float64
	}{
		{"16:9", 16.0 / 9.0},
		{"9:16", 9.0 / 16.0},
		{"4:3", 4.0 / 3.0},
		{"3:4", 3.0 / 4.0},
		{"1:1", 1.0},
	}
	actual := float64(w) / float64(h)
	best := ""
	bestDiff := math.MaxFloat64
	for _, candidate := range candidates {
		if diff := math.Abs(actual - candidate.value); diff < bestDiff {
			bestDiff = diff
			best = candidate.name
		}
	}
	if bestDiff > 0.25 {
		return "", false
	}
	return best, true
}

// wants1080P 判断用户是否**明确要求了 1080P**。
//
// 规则二的目标模型 wan3.0-720p-特惠 固定交付 720P（480P 生成 + 提升），
// 用户要 1080P 时路由过去就是降级交付 → 不路由，交回源链路按用户要的跑。
//
// 480P / 720P 不挡：480P 需求被 720P 成片满足（升级），720P 需求正好命中。
func wants1080P(body map[string]interface{}) bool {
	for _, key := range []string{"resolution", "size"} {
		if explicitResolutionTier(body[key]) == "1080P" {
			return true
		}
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"resolution", "size"} {
		if explicitResolutionTier(metadata[key]) == "1080P" {
			return true
		}
	}
	return false
}

// explicitResolutionTier 把一个分辨率取值归一成 480P / 720P / 1080P。
//
// 支持档位字面量（"720P"）与像素尺寸（"1280x720"）。无法解析返回空串。
// 尺寸按**短边**分档，与 relay/channel/task/wan3 的 resolutionFromSize 同口径。
func explicitResolutionTier(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case "480", "480P":
		return "480P"
	case "720", "720P":
		return "720P"
	case "1080", "1080P":
		return "1080P"
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(text), "*", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return ""
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ""
	}
	short := w
	if h < short {
		short = h
	}
	switch {
	case short <= 600:
		return "480P"
	case short <= 900:
		return "720P"
	default:
		return "1080P"
	}
}

// doubaoResolutionTier 从请求体里找出用户要的分辨率档位，返回 480P / 720P / 1080P。
//
// 覆盖源链路（youzanwan3）读分辨率的全部入口：
//   - 顶层 resolution
//   - 顶层 size（有赞也把 size 当分辨率用，见 normalizeResolutionForModel）
//   - metadata.resolution
//   - metadata.size
//
// 只认**能解析成合法分辨率**的值（480/480P/720/720P/1080/1080P 或像素尺寸），
// 客户端习惯性传 `size: "16:9"`（比例语义）时解析不出档位，返回空串 ——
// 那种值本来就不是分辨率，不该被当成分辨率处理。
func doubaoResolutionTier(body map[string]interface{}) string {
	for _, key := range []string{"resolution", "size"} {
		if tier := explicitResolutionTier(body[key]); tier != "" {
			return tier
		}
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return ""
	}
	for _, key := range []string{"resolution", "size"} {
		if tier := explicitResolutionTier(metadata[key]); tier != "" {
			return tier
		}
	}
	return ""
}

// collectReferenceImages 收集请求里的参考图 URL 列表。
//
// 素材来源优先级与源链路（youzanwan3 的 buildMedia）**保持一致**：
// 顶层 media > metadata.media > metadata.input.media > 顶层 images / image / input_reference。
// 优先级错了就会**丢图**：源链路认顶层 media 时我们只读 metadata.media，
// 转换出来的图数就少了（2026-10-22 丢图事故同型）。
//
// 第二个返回值 true 表示遇到了目标链路不支持的素材（参考视频/音频，
// 或 allowDataURL=false 时的 data URI 参考图）。
//
// allowDataURL 按**目标链路的真实契约**传，不能一刀切：
//   - 心笔兼容口（xinbiapi）明确收 https 直链「或 data: URL」（平台代取入库
//     对后者做 base64 解码），所以规则六/规则二/特惠分流传 true ——
//     arcreel 客户端把 base64 参考图编码在 metadata.input.media（Wan3 形状）
//     里，2026-11-13 曾因这里一刀切拒绝，短请求全部被拦回原链路、
//     撞上有赞 prime 的 <10 秒保护（PRIME_DURATION_TOO_SHORT）；
//   - 漫屋/豆包链路只收公网 http(s) URL，改派它们的路径传 false。
func collectReferenceImages(body map[string]interface{}, allowDataURL bool) ([]string, bool) {
	var images []string

	// 1) 顶层 media（源链路优先级最高）
	if media, ok := body["media"].([]interface{}); ok {
		for _, item := range media {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			mediaType, _ := entry["type"].(string)
			switch strings.TrimSpace(mediaType) {
			case "reference_image":
				images = appendImageURL(images, entry["url"])
			case "reference_video", "reference_audio":
				return nil, true
			}
		}
		if len(images) > 0 {
			return images, false
		}
	}

	// 2) metadata.media
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if media, ok := metadata["media"].([]interface{}); ok {
			for _, item := range media {
				entry, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				mediaType, _ := entry["type"].(string)
				switch strings.TrimSpace(mediaType) {
				case "reference_image", "first_frame", "last_frame":
					images = appendImageURL(images, entry["url"])
				case "reference_video", "reference_audio":
					return nil, true
				}
			}
		}
		if len(images) > 0 {
			return images, false
		}
	}

	// 2b) metadata.input.media（Wan3 形状）
	//
	// ArcReel 在「非豆包形状」下把参考图编码在 metadata.input.media 里
	// （Wan3 体 {"input": {"media": [...]}}）。改派豆包时本函数上面的
	// rewriteBodyForDoubaoWithDuration 会整块丢弃 metadata，只保留这里收集到的图；
	// 不读这里就会静默丢图（2026-11-09 gen-7cdab0938eba1d6229b01a5e：26 秒带 7 张图，
	// 远端 job refs=0，成片没有任何参考图）。
	//
	// URL 形式按 allowDataURL 区分：漫屋/豆包（false）只收 http(s) 直链，
	// data URI / 本地路径返回 hasUnsupported 让调用方放弃改派；心笔（true）
	// 连 data: 内联一起收（它对 data URI 做平台侧解码入库）。
	if input, ok := metadata["input"].(map[string]interface{}); ok {
		if media, ok := input["media"].([]interface{}); ok {
			for _, item := range media {
				entry, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				mediaType, _ := entry["type"].(string)
				switch strings.TrimSpace(mediaType) {
				case "reference_image", "first_frame", "last_frame":
					urls := appendImageURL(nil, entry["url"])
					for _, url := range urls {
						if !allowDataURL && !isHTTPURL(url) {
							return nil, true
						}
					}
					images = append(images, urls...)
				case "reference_video", "reference_audio":
					return nil, true
				}
			}
			if len(images) > 0 {
				return images, false
			}
		}
	}
	// 3) 顶层 images / image / input_reference
	images = appendImageURL(images, body["images"])
	images = appendImageURL(images, body["input_reference"])
	images = appendImageURL(images, body["image"])

	return images, false
}

// appendImageURL 把一个可能是字符串或数组的值里的图片地址追加进列表。
func appendImageURL(images []string, value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			images = append(images, trimmed)
		}
	case []interface{}:
		for _, item := range typed {
			images = appendImageURL(images, item)
		}
	}
	return images
}

// isHTTPURL 判断是否为可直传的公网 http(s) 地址（data URI / 本地路径一律不算）。
func isHTTPURL(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// extractRatio 从请求里取比例值（metadata.ratio 优先，其次顶层 ratio）。
func extractRatio(body map[string]interface{}) (string, bool) {
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if ratio, ok := metadata["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
			return strings.TrimSpace(ratio), true
		}
	}
	if ratio, ok := body["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
		return strings.TrimSpace(ratio), true
	}
	return "", false
}

// mapAdaptiveRatio 把源链路的自适应档名映射成目标链路的叫法。
//
// 源（有赞 Wan3）用 adaptive，目标（seedance-2-5 链路）用 auto ——
// 不映射的话，adaptive 会被当成一个不认识的字面量发给目标链路。
// 目标常驻在班车 ch27 的那段时间，两条链路都认这两个名字
// （班车把 auto 归一回 adaptive），映射方向不冲突；当前常驻豆包官网 ch20，
// 豆包原生的档位名就是 auto，此处映射正是它需要的。
func mapAdaptiveRatio(ratio string) string {
	if ratio == "adaptive" {
		return "auto"
	}
	return ratio
}
