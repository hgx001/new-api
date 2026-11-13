// Package seedanceroute_setting 的「短时长分流（规则六）链路开关」。
//
// 背景（2026-11-13）：规则六把 ≤阈值（默认 10 秒）的请求改派到心笔内链
// （wan3.0-720p-特惠-内链，ch25），绕开上游 prime 的 PRIME_DURATION_TOO_SHORT。
// 老板要求与豆包链路开关同款的运营动作：
//
//	打开开关 → 短请求路由到心笔；
//	关闭开关 → 不路由，请求走原链路（源模型自己的常驻渠道）。
//
// 与豆包开关的差异：豆包关闭时会**强制启用**故障转移（seedance-2-5 → 班车）
// 并带凌晨 2 点自动恢复 —— 因为豆包额度按天重置。心笔内链没有那种日额度
// 概念，所以这里就是一个纯手动开关：关了就停、开了就恢复，没有截止时间、
// 没有接单计数、没有强制降级。保持简单 —— 没有真实需求就不要抄复杂度。
package seedanceroute_setting

// shortLinkEnabledFor 判断规则六的全局链路开关是否开启。
//
// 三态指针：nil（配置里没写这个字段）= 默认开启 —— 旧的生产 option JSON
// 里没有这个字段，解析后是 nil，若按 bool 零值处理，升级当天会静默停掉
// 线上正在跑的分流，且没有任何报错。所以默认值必须与「改造前行为」一致。
func (p Policy) shortLinkEnabledFor() bool {
	if p.ShortLinkEnabled == nil {
		return true
	}
	return *p.ShortLinkEnabled
}

// IsShortLinkOn 判断策略副本里规则六的全局链路开关是否开启（供测试与快照用）。
func (p Policy) IsShortLinkOn() bool {
	return p.shortLinkEnabledFor()
}

// IsShortLinkEnabled 判断规则六的全局链路开关当前是否开启（读全局状态）。
func IsShortLinkEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.shortLinkEnabledFor()
}

// IsShortLinkOff 判断规则六的全局链路开关当前是否关闭。
func IsShortLinkOff() bool {
	return !IsShortLinkEnabled()
}

// WithShortLinkEnabled 返回「把规则六的全局链路开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成，
// 与 WithDoubaoLinkEnabled / WithSourceSecondsRuleEnabled 同一套做法。
//
// 开关关闭时不做任何附带动作（不动 FailoverEnabled、不清计数）：心笔内链
// 关闭后短请求走原链路是**显式经营决策**，不是故障降级，不要替运营加戏。
func WithShortLinkEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	value := enabled
	out.ShortLinkEnabled = &value
	out.UpdatedAt = nowUnix()
	return out
}
