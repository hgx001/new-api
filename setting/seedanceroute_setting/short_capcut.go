// Package seedanceroute_setting 的「规则六第三分支：短时长 → CapCut 满血档」。
//
// 背景（2026-11-14 老板需求）：
//
//	目前 ≤10 秒的请求走有赞标准版（wan3.0-video）。老板要再加一条路：
//	≤10 秒的请求改派到 cp-seedance-2.0（CapCut 剪映国际版 Seedance 2.0 满血档）。
//
// ⚠️ **三条短时长分支同时只能有一条生效**：
//
//  1. 心笔内链（short_link_enabled）—— wan3.0-720p-特惠-内链，480P 生成 + 超分 720P；
//  2. 有赞标准版（short_youzan_enabled）—— wan3.0-video，绕开 prime 的 <10 秒保护；
//  3. CapCut 满血档（short_capcut_enabled）—— cp-seedance-2.0。
//
// 为什么必须互斥：三条是**同一个时长区间的三个出口**。
// 同时开着两三条时，同一个短请求会按优先级落到其中一条 ——
// 但「落到哪一条」对运营来说必须是**明确意图**，而不是「谁写在配置里靠前」。
// 两条都开着时，运营看着两个按钮都亮着，却只有一条真正生效（另一条是死配置），
// 这是「静默失效」型故障：没有任何报错，运营只会看到「我明明开了怎么没走那条」。
//
// 所以互斥在**写入口**保证（controller 侧开一个自动关另两个），
// 读侧再做一次**显式优先级兜底**（万一配置被 SQL 手改过也不会两条同时生效）。
//
// 优先级（读侧兜底）：心笔内链 > 有赞标准版 > CapCut 满血档。
//
// 为什么心笔排第一：它是**成本最低**的一条（480P 生成 + 超分），
// 且能力边界最宽（不挑素材形态）。有赞标准版次之（同渠道同 adaptor，最不容易失败）。
// CapCut 满血档排最后：它是**跨渠道**的（ch20 manwu），契约限制最多
// （只收 http(s) 参考图、只卖 480p/720p、时长 4–15 秒）。
//
// ⚠️ 用 bool 而不是三态指针：这是**全新**的分支开关，旧的生产 option JSON 里
// 根本没有这个字段，解析后是零值 false = 不启用，语义正好是「没配过就不生效」，
// 升级当天线上流量逐字节不变（与 ShortYouzanEnabled 同一口径）。
package seedanceroute_setting

// DefaultShortCapcutTargetModel 是规则六第三分支的目标模型（CapCut Seedance 2.0 满血档）。
//
// 2026-11-15 老板口径：<10 秒的请求改派 **cp-seedance-2.0**（满血档），不是 2.0fast。
//
// 为什么换档：
//   - 2.0 满血 ¥0.0205/秒，2.0fast ¥0.011/秒。老板选满血档是要更好的出片质量
//     （2.0fast 是「快速档」，定位是便宜不是好）；
//   - 两档的时长上界相同（4–15 秒），5–9 秒都在区间内，换档不损失时长覆盖；
//   - 两档的 720p 倍率不同（2.0 是 ×2.0，2.0fast 是 ×1.5）—— 但改派后计费
//     **按源模型口径**算（setRouteBillingContext 覆盖 adaptor 的 size 倍率），
//     换档不会改变对客价格。
//
// ⚠️ 换档不影响超分：capcutUpscaleDeliveryOrigin 按「计费原点是促销族」判定，
//   五档 CapCut 商品共用同一条超分前提（见 manwu 的 isCapcutVideoExecutionModel）。
const DefaultShortCapcutTargetModel = "cp-seedance-2.0"

// ShortCapcutMinSeconds 是规则六第三分支的**时长下界**（秒）。
//
// 2026-11-15 老板口径：<10 秒的走 CapCut 满血档，10 秒及以上留给有赞标准版。
//
// 为什么是这个分界：
//   - 上游有赞标准版 wan3.0-video 的下界就是 10 秒（2026-11-15 切换后），
//     10 秒及以上它能跑，且同渠道同 adaptor、最不容易失败；
//   - <10 秒有赞标准版接不住（会被上游 400），CapCut 满血档 4–15 秒能接住，
//     属于「标准版的能力缺口，CapCut 来补」。
//
// ⚠️ 这条门槛与「规则六的 ≤10 秒触发阈值」是两件事：
//   触发阈值决定「哪些请求进规则六」（≤ short_trigger_seconds，当前 10），
//   本下界决定「进规则六后走哪条分支」（<10 → CapCut，≥10 → 有赞标准版）。
//   两条线都在 10 这个点上对齐，是刻意选的：规则六的阈值改成 15 秒时，
//   10–15 秒仍由有赞标准版接住，不会掉进 CapCut。
const ShortCapcutMinSeconds = 10

// IsShortCapcutEnabled 判断规则六第三分支（CapCut 满血档）开关当前是否开启。
func IsShortCapcutEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.ShortCapcutEnabled
}

// IsShortCapcutEnabledFor 判断「CapCut 满血档分支对某个源模型是否生效」。
//
// 上层闸门与另外两条短时长分支共用按源模型的 short_rule_enabled：
// 活动版的规则六本来就被关掉（按次档没有短时长成本问题），
// 不能因为运营打开 CapCut 按钮就被带进满血档链路。
func IsShortCapcutEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return shortCapcutReadyFor(current, sourceModel)
}

// shortCapcutReadyFor 判断「CapCut 满血档分支对某个源模型是否生效」。
func shortCapcutReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.ShortCapcutEnabled &&
		p.shortRuleEnabledFor(sourceModel)
}

// WithShortCapcutEnabled 返回「把 CapCut 满血档分支开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption 完成，
// 与 WithShortYouzanEnabled / WithShortLinkEnabled 同一套做法。
//
// ⚠️ 这里**不**自动关掉另外两条分支：互斥是**经营意图**，由 controller 在写入口
// 显式处理（开一个自动关另两个）。设置包只负责「设值」这一件事，
// 在这里隐式关别的开关会让「关掉 CapCut 顺带关掉心笔」这种副作用发生 —
// 运营关掉一条分支时往往只是想换一条，不是想全停。
func WithShortCapcutEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.ShortCapcutEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyShortCapcutEnabled 把「CapCut 满血档分支开关」设到**给定策略**上，返回新副本。
//
// 与 WithShortCapcutEnabled 的区别：后者从全局状态取基底，
// 这个从调用方给的策略取基底 —— 三路互斥需要**一次性**改三个字段，
// 如果分三次调用全局版本，每次都从全局取基底，前一次改的结果就被覆盖了。
func ApplyShortCapcutEnabled(p Policy, enabled bool) Policy {
	out := clonePolicy(p)
	out.ShortCapcutEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyShortLinkEnabled 把「心笔内链分支开关」设到**给定策略**上，返回新副本。
//
// 语义与 ApplyShortCapcutEnabled 同款：给三路互斥用。
func ApplyShortLinkEnabled(p Policy, enabled bool) Policy {
	out := clonePolicy(p)
	value := enabled
	out.ShortLinkEnabled = &value
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyShortYouzanEnabled 把「有赞标准版兜底开关」设到**给定策略**上，返回新副本。
//
// 语义与 ApplyShortCapcutEnabled 同款：给三路互斥用。
func ApplyShortYouzanEnabled(p Policy, enabled bool) Policy {
	out := clonePolicy(p)
	out.ShortYouzanEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ShortRouteTargets 返回「规则六当前启用的分支列表」，按优先级排序。
//
// 用途：管理后台展示（运营要能看到「现在到底哪条分支在生效」），
// 以及中间件的兜底判定（见 ActiveShortRouteTarget）。
//
// 优先级：心笔内链 > 有赞标准版 > CapCut 满血档。
func ShortRouteTargets() []string {
	mu.RLock()
	defer mu.RUnlock()
	return current.activeShortRouteTargets()
}

func (p Policy) activeShortRouteTargets() []string {
	out := make([]string, 0, 3)
	if p.shortLinkEnabledFor() {
		out = append(out, p.ShortTargetModel)
	}
	if p.ShortYouzanEnabled {
		out = append(out, DefaultShortYouzanTargetModel)
	}
	if p.ShortCapcutEnabled {
		out = append(out, DefaultShortCapcutTargetModel)
	}
	return out
}

// ActiveShortRouteTarget 返回「当前该走哪条短时长分支」。
//
// 返回空串 = 不路由（短请求走原链路）。
//
// ⚠️ 这是**读侧兜底**：正常情况下写入口已经保证只有一条分支开着，
// 但配置可能被 SQL 直接改过（生产上真出过带空格模型名这类事故）。
// 这里显式按优先级取第一条，保证「至多一条生效」这个不变量在任何配置下都成立。
func ActiveShortRouteTarget() string {
	mu.RLock()
	defer mu.RUnlock()
	targets := current.activeShortRouteTargets()
	if len(targets) == 0 {
		return ""
	}
	return targets[0]
}
