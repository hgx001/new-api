// Package seedanceroute_setting 的「短时长分流（规则六）有赞标准版兜底」。
//
// 背景（2026-11-14）：规则六原来只有心笔内链一个目标。心笔不可用
// （余额耗尽 / AutoBan）时，≤阈值的短请求会回落到源链路 prime，撞上
// PRIME_DURATION_TOO_SHORT —— 客户直接失败。老板要一个独立的按钮：
//
//	打开：短请求在心笔接不住时改派有赞标准版 wan3.0-video；
//	关闭：不兜底（默认），行为与改造前一致。
//
// 优先级：心笔内链开关（short_link_enabled）> 本开关。
// 两个开关都关闭 → 不路由，短请求走原链路。
package seedanceroute_setting

// DefaultShortYouzanTargetModel 是规则六有赞兜底的目标模型（有赞标准版）。
//
// 它与源模型 seedance2.5-特惠 落在同一个渠道、同一个 adaptor
// （relay/channel/task/youzanwan3），入参契约完全一致 —— 改派只换 model，
// 并把分辨率钉成源链路实际执行的档位（prime 缺省 720P）。
const DefaultShortYouzanTargetModel = "wan3.0-video"

// IsShortYouzanEnabled 判断规则六的有赞标准版兜底开关当前是否开启。
func IsShortYouzanEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.ShortYouzanEnabled
}

// IsShortYouzanEnabledFor 判断「有赞标准版兜底对某个源模型是否生效」。
//
// 上层闸门与心笔路径共用按源模型的 short_rule_enabled：
// 活动版的规则六本来就被关掉（按次档没有短时长成本问题），
// 不能因为运营打开有赞按钮就被带进标准版链路。
func IsShortYouzanEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return shortYouzanReadyFor(current, sourceModel)
}

// shortYouzanReadyFor 判断「有赞兜底对某个源模型是否生效」。
func shortYouzanReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.ShortYouzanEnabled &&
		p.shortRuleEnabledFor(sourceModel)
}

// WithShortYouzanEnabled 返回「把有赞标准版兜底开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption 完成，
// 与 WithShortLinkEnabled / WithDoubaoLinkEnabled 同一套做法。
func WithShortYouzanEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.ShortYouzanEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}
