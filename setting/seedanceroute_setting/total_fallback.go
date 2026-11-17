// Package seedanceroute_setting 的「规则八：全量兜底 → cp-seedance-2.5」。
//
// 背景（2026-11-15 老板需求）：
//
//	seedance2.5-特惠 与 seedance2.5-活动 的**常驻链路是有赞（prime）**。
//	如果哪天有赞彻底不能用（渠道被关 / 账号封 / 上游大面积失败），
//	这两个对外模型名就整条链路上没有出口了 —— 前七条规则无论怎么配，
//	都建立在「源链路至少能跑一部分请求」这个前提上。
//
//	所以需要第八条规则：一个**一键逃生口**。打开之后，
//	这两个模型收到的请求**无条件**改派到 cp-seedance-2.5，
//	不看时长、不看比例、不看提示词内容、不看有没有首尾帧。
//
// ── 「无条件」到底无到什么程度 ──
//
//	无的是**业务触发条件**（前七条规则各自的门槛），
//	**不是**准入检查。下面四类请求仍然留在原链路：
//
//	  1. 1080P —— CapCut 只卖 480p / 720p，改派过去必 400；
//	  2. 2–3 秒 —— CapCut 时长下界 4 秒，改派过去必 400；
//	  3. 带参考视频 / 参考音频 —— 现口径一律不路由（见 prepareCapcutRoute ① ②）；
//	  4. 自适应比例 + 带参考图 —— CapCut 没有 adaptive 档，替他挑一档 = 改需求。
//
//	这四类「留在原链路」与「改派过去」的结果都是失败（前者 503 没渠道、
//	后者 400 参数非法），但**留原链路能保住一个准确的原因**，
//	而改派过去会把「这条链路挂了」误报成「你的参数不合法」——
//	客户看到 400 只会去改参数，永远发现不了是我们的链路问题。
//	所以准入不是「不给逃生」，而是「不把故障伪装成客户错误」。
//
// ── 与规则七（电商内容）的关系 ──
//
//	两者的目标模型是同一条 CapCut 链路、同一套准入与改写（prepareCapcutRoute），
//	区别只在**触发条件**：
//
//	  规则七：内容命中电商/产品推荐关键词，或勾选的参数条件命中；
//	  规则八：没有任何条件，开就全量。
//
//	所以规则八必须排在规则七**之前**（见 middleware/seedance_route.go）：
//	规则八开着时，规则七的判定结果已经没有意义（全量都走同一条），
//	排在后面只会多算一次关键词表。
//
// ── 与总开关 Enabled 的关系（刻意的例外） ──
//
//	其它七条规则都要求 `enabled: true`（规则总开关）才生效，规则八**不要求**。
//	理由：规则八是「链路挂了」时的应急救援开关，它的使用者是在救火，
//	没有余裕先去确认「规则总开关是不是开着的」。如果它也依赖 Enabled，
//	就会出现「应急开关亮着、请求却全部失败」这种最难排查的状态 ——
//	而那个状态下运营只会以为「CapCut 也挂了」。
//
//	代价是 Enabled=false（所有规则停用）时打开本开关仍会改派 ——
//	这是**有意的**：停用规则 ≠ 链路可用，两者是不同的事。
package seedanceroute_setting

// DefaultTotalFallbackTargetModel 是规则八的目标模型（CapCut Seedance 2.5 Flash）。
//
// 2026-11-17 口径（老板）：促销族所有 CapCut 出口（规则六/七/八/九）统一走
// cp-seedance-2.5flash；与取消 480p+超分、改 720p 原生直出同批执行。
// 豆包备胎（DefaultFailoverAltModel）本就是 2.5 Flash，仍保持原样。
//
// ⚠️ 生成档位是 720p 原生直出（middleware 的 capcutGenerationResolutionValue），
// 2026-11-17 起不再 480p 生成 + 超分（超分画质不达标）。对客售价与交付档都不变。
//
// 为什么固定成常量、不给 Policy 字段：
// 规则八的准入与改写走的是 prepareCapcutRoute，它**只适配了这一条契约**
// （480p/720p、4–30 秒、CapCut 比例表、参考视频/音频各 ≤5 个）。
// 开放成可配置项的话，配成别的模型名就是一个「看起来配了、实际必挂」的
// 静默故障 —— 与本包其它「配错就不生效」的字段不同，这个字段配错
// 不是不生效，而是把请求改写到一条没适配过的链路上。
//
// 要换目标时改这一行（并补对应准入），不要加配置项。
const DefaultTotalFallbackTargetModel = "cp-seedance-2.5flash"

// IsTotalFallbackEnabled 判断规则八的开关是否打开（不看源模型，也不看规则总开关）。
//
// 用途：管理后台状态展示、route 中间件早退判定。
func IsTotalFallbackEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return totalFallbackConfigured(current)
}

// TotalFallbackEnabledFor 判断「规则八对这个源模型是否生效」。
//
// 两层闸门：开关打开（且配了源模型）**且** 这个模型确实是配置里的源模型。
// 第二层不是多余的：中间件是「先判 IsSourceModel 再逐条规则判」，
// 但状态接口与将来的调用点未必走同一条路径，多判一次不会错，
// 漏判会让一个非源模型的请求被改写到 CapCut。
//
// ⚠️ 刻意**不要求** p.Enabled（规则总开关）：理由见本文件包头注释
// 「与总开关 Enabled 的关系」——应急救援开关不能被「规则全停」连坐。
func TotalFallbackEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return totalFallbackReadyFor(current, sourceModel)
}

// totalFallbackReadyFor 判断「规则八对某个源模型是否生效」。
func totalFallbackReadyFor(p Policy, sourceModel string) bool {
	return totalFallbackConfigured(p) && p.IsSourceModel(sourceModel)
}

// totalFallbackConfigured 判断「规则八配全了没有」（不看具体源模型）。
//
// 必要条件只有两个：开关打开 + 至少配了一个源模型。
// 目标模型是常量、没有触发值可配，所以不存在「配了一半」的形态。
func totalFallbackConfigured(p Policy) bool {
	return p.TotalFallbackEnabled && p.hasSourceModel()
}

// TotalFallbackTargetModel 返回规则八的目标模型名。
//
// 恒等于 DefaultTotalFallbackTargetModel —— 留这个访问器是为了让
// controller / 前端拿到的取值与中间件改写用的取值同源，
// 避免两处各写一份字符串常量（改目标时必然漏改一处）。
func TotalFallbackTargetModel() string {
	return DefaultTotalFallbackTargetModel
}

// WithTotalFallbackEnabled 返回「把规则八开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由 controller 用 model.UpdateOption 完成，
// 与 WithEcommerceEnabled / WithShortCapcutEnabled 同一套做法。
//
// ⚠️ 只改这一个字段：不动规则总开关、不动前七条规则的目标与条件、
// 不动豆包链路开关的暂停/标记状态。这份策略里还压着线上正在跑的
// 全部调度配置，顺手改别的就是没有任何报错的静默事故。
func WithTotalFallbackEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.TotalFallbackEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}
