// Package seedanceroute_setting 的「规则六第四分支：短时长 / 非 720P → 豆包 Fast」。
//
// 背景（2026-11-18 老板需求）：
//
//	促销族常驻上游换成 SJ-wan3-prime 后，它只接 **15–30 秒、仅 720P**。
//	对外仍要卖 5–30 秒，所以 5–14 秒的 720P 请求必须改派到
//	seedance-2-0-fast（豆包官网，4–30 秒、480P/720P/1080P、按秒 ¥0.1）。
//
// 为什么单独一条分支而不是复用 CapCut：
//   - CapCut 链路的准入是 CapCut 契约（只收 http(s) 参考图、只卖 480p/720p、
//     4–15 秒），与豆包官网不同；用 CapCut 的准入去改派 Fast 会误拒 1080P、
//     误判素材形态。
//   - 两条链路的目标渠道虽然同为 ch20 manwu，但 adaptor 内的 Kind 不同
//     （kindCapcutVideo vs kindDoubaoVideo），契约不能混用。
//
// 与另外三条短时长分支**互斥**：心笔 > 有赞标准版 > 豆包 Fast > CapCut。
// 写入口在 controller 侧保证（开一个自动关其余），读侧再按优先级兜底。
//
// ⚠️ 用 bool 而不是三态指针：这是**全新**的分支开关，旧的生产 option JSON 里
// 根本没有这个字段，解析后是零值 false = 不启用，语义正好是「没配过就不生效」，
// 升级当天线上流量逐字节不变（与 ShortYouzanEnabled / ShortCapcutEnabled 同一口径）。
package seedanceroute_setting

// DefaultShortFastTargetModel 是规则六第四分支的目标模型（豆包 Seedance 2.0 Fast）。
//
// 它是 manwu 渠道的**公开模型名**（ch20），客户可以直接调用（¥0.1/秒）；
// 规则六改派过去后计费仍按**源模型**口径算（seedance2.5-特惠 ¥0.20/秒），
// 差价是毛利，不改变对客价格。
const DefaultShortFastTargetModel = "seedance-2-0-fast"

// ShortFastMaxSeconds 是「720P / 未指定分辨率」走 Fast 的时长上界（秒，**含**）。
//
// 2026-11-18 老板口径：15 秒以下 → Fast，15 秒及以上 → SJ-wan3-prime。
// 所以 ≤14 秒命中，15 秒留给有赞。
//
// ⚠️ 这个值必须与 relay/channel/task/youzanwan3 的 promoShortRouteMaxSeconds
// 保持一致：路由层按它分流，适配器按那个做兜底校验。两处分叉会出现
// 「路由层认为该改派、适配器认为该拒」或反过来的错位。
const ShortFastMaxSeconds = 14

// ShortFastMinSeconds 是 Fast 链路的时长下界（秒，**含**）。
//
// 豆包 2.0 Fast 上游口径是 4–30 秒，但促销族对外下界是 5 秒
// （youzanwan3 的 promoMinDuration），两边取更严的 5。
const ShortFastMinSeconds = 5

// IsShortFastEnabled 判断规则六第四分支（豆包 Fast）开关当前是否开启。
func IsShortFastEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.ShortFastEnabled
}

// IsShortFastEnabledFor 判断「豆包 Fast 分支对某个源模型是否生效」。
//
// 上层闸门与另外三条短时长分支共用按源模型的 short_rule_enabled：
// 活动版的规则六本来就被关掉（按次档没有短时长成本问题），
// 不能因为运营打开 Fast 按钮就被带进短时长链路。
func IsShortFastEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return shortFastReadyFor(current, sourceModel)
}

// shortFastReadyFor 判断「豆包 Fast 分支对某个源模型是否生效」。
func shortFastReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.ShortFastEnabled &&
		p.shortRuleEnabledFor(sourceModel)
}

// WithShortFastEnabled 返回「把豆包 Fast 分支开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption 完成，
// 与 WithShortYouzanEnabled / WithShortCapcutEnabled 同一套做法。
//
// ⚠️ 这里**不**自动关掉另外三条分支：互斥是**经营意图**，由 controller 在
// 写入口显式处理。设置包只负责「设值」这一件事。
func WithShortFastEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.ShortFastEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyShortFastEnabled 把「豆包 Fast 分支开关」设到**给定策略**上，返回新副本。
//
// 与 WithShortFastEnabled 的区别：后者从全局状态取基底，
// 这个从调用方给的策略取基底 —— 四路互斥需要**一次性**改四个字段，
// 如果分四次调用全局版本，每次都从全局取基底，前一次改的结果就被覆盖了。
func ApplyShortFastEnabled(p Policy, enabled bool) Policy {
	out := clonePolicy(p)
	out.ShortFastEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ShortFastActive 判断当前策略下「豆包 Fast」是否是生效的那一条短时长分支。
//
// 读侧兜底用：四条分支同时只能有一条真正跑，优先级
// 心笔 > 有赞标准版 > 豆包 Fast > CapCut。
func ShortFastActive() bool {
	mu.RLock()
	defer mu.RUnlock()
	p := current
	if !p.Enabled || !p.hasSourceModel() || !p.ShortFastEnabled {
		return false
	}
	if p.ShortLinkEnabled != nil && *p.ShortLinkEnabled {
		return false
	}
	if p.ShortYouzanEnabled {
		return false
	}
	return true
}
