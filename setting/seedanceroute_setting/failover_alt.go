// Package seedanceroute_setting 的「规则五备选备胎：豆包不可用 → CapCut 2.5 Flash」。
//
// 背景（2026-11-14 老板需求）：
//
//	规则五现在只有一条退路 —— seedance-2-5 不可用（或豆包链路开关关闭）时
//	改派 wan3.0-班车。班车是「视频生成车站」的排队链路，出片时间不可控。
//	老板要一个**独立按钮**：
//
//	  打开：改派 cp-seedance-2.5flash（CapCut 剪映国际版 2.5 Flash），
//	        上游按 480p 生成，交付前用腾讯 MPS 超分到 720P
//	        （与规则六心笔内链同一套超分，见 relay/channel/task/wan3/postprocess_adaptor.go）；
//	  关闭（默认）：仍走 wan3.0-班车，行为与改造前逐字节一致。
//
// 前提不变：只有「豆包链路真的不可用」时才触发 —— 渠道被关/AutoBan、
// 额度探针标记、或运营用豆包链路开关人工宣布不可用。豆包好用的时候，
// 这条备选链路永远不参与调度。
//
// ⚠️ 为什么是 bool 而不是三态指针：这是**新增的备选出口**，旧的生产 option
// JSON 里没有这个字段，解析后零值 false = 走班车 —— 正是「没配过就保持现状」。
// 与 FailoverEnabled / ShortYouzanEnabled / ShortCapcutEnabled 同一口径。
package seedanceroute_setting

import "strings"

// DefaultFailoverAltModel 是备选备胎的模型名（CapCut Seedance 2.5 Flash 档）。
//
// 为什么是这一档而不是 2.0 Fast：规则五要接住的是**豆包 2.5 的原单**
// （2.5 的时长/比例/参考图能力最宽），2.5 Flash 与它同一代；
// 2.0 Fast 时长上界只有 15 秒，接不住 30 秒的降级单。
const DefaultFailoverAltModel = "cp-seedance-2.5flash"

// IsFailoverAltEnabled 判断规则五的备选备胎开关当前是否开启。
func IsFailoverAltEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.FailoverAltEnabled
}

// FailoverAltTargetModel 返回备选备胎的目标模型名（没配则用默认值）。
//
// 返回的是**去空白后的配置值**：渠道/模型配置里手滑多打一个空格在生产
// 出现过（" seedance2.5-特惠"），按精确名匹配的判定会静默失效。
func FailoverAltTargetModel() string {
	mu.RLock()
	defer mu.RUnlock()
	return failoverAltTargetFor(current)
}

// failoverAltTargetFor 是 FailoverAltTargetModel 的纯函数版本（调用方已持锁）。
func failoverAltTargetFor(p Policy) string {
	if target := strings.TrimSpace(p.FailoverAltModel); target != "" {
		return target
	}
	return DefaultFailoverAltModel
}

// WithFailoverAltEnabled 返回「把备选备胎开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption 完成，
// 与 WithShortLinkEnabled / WithFailoverEnabled 同一套做法。
func WithFailoverAltEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.FailoverAltEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyFailoverAltEnabled 在给定策略副本上设置开关（用于与其它开关一起落库）。
func ApplyFailoverAltEnabled(p Policy, enabled bool) Policy {
	p.FailoverAltEnabled = enabled
	p.UpdatedAt = nowUnix()
	return p
}

// WithFailoverAltModel 返回「把备选备胎目标模型设成 model」的策略副本。
//
// 传空串 = 回到默认值（cp-seedance-2.5flash），不是「关闭」——
// 关闭是开关的事，两者分开表达，避免「清空模型名顺手把开关关了」这种隐式效应。
//
// ⚠️ 这是**从全局快照重来**的版本（clonePolicy(current)）。要在一个已经改过
// 别的字段的策略副本上继续改，必须用 ApplyFailoverAltModel —— 否则前一次
// 改动会被这里的 current 覆盖（With* 系列都是纯函数，互相不叠加）。
func WithFailoverAltModel(model string) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	return ApplyFailoverAltModel(out, model)
}

// ApplyFailoverAltModel 在给定策略副本上设置目标模型（可与其它改动一起落库）。
func ApplyFailoverAltModel(p Policy, model string) Policy {
	p.FailoverAltModel = strings.TrimSpace(model)
	p.UpdatedAt = nowUnix()
	return p
}
