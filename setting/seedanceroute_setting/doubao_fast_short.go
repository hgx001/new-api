// Package seedanceroute_setting 的「规则十：seedance-2-5 短时长 → 豆包 Fast」。
//
// 背景（2026-11-18 老板需求）：
//
//	seedance-2-5 是对外的一口价商品（ModelPrice 按次 ¥3.50），常驻豆包官网 ch20。
//	15 秒及以下（含 15 秒）的请求改派到 seedance-2-0-fast（同渠道 ch20、
//	同 adaptor kindDoubaoVideo，按秒 ¥0.1），交付更快、上游成本更低；
//	对客计费仍是 seedance-2-5 的一口价 —— 改派只影响执行链路，不影响账单。
//
// 为什么不复用规则六的 short_fast_enabled / source_models：
//
//	规则六的源是促销族（source_models = seedance2.5-特惠 / -活动），
//	而 seedance-2-5 恰恰是规则一的**目标**。把它塞进 source_models 会同时
//	激活规则一（目标又是自己）、规则二/三/七/八（全量兜底、电商、首尾帧），
//	形成「改派到自己」和「一开全开」的连锁反应 —— 见 middleware/seedance_route.go
//	规则五那段注释（2026-11-12 已经踩过一次）。
//
//	所以这条规则用**独立字段**，并在 IsSourceModel 早退之前单独判定。
//
// ⚠️ 开关用 bool 而不是三态指针：这是全新规则，旧的生产 option JSON 里没有
// 这个字段，解析后零值 false = 不启用，升级当天线上流量逐字节不变。
package seedanceroute_setting

import "strings"

const (
	// DefaultDoubaoFastSourceModel 是规则十的源模型（对外一口价商品）。
	DefaultDoubaoFastSourceModel = "seedance-2-5"

	// DefaultDoubaoFastTargetModel 是规则十的目标模型（豆包 Seedance 2.0 Fast）。
	//
	// 与规则六第四分支共用同一个目标：两者都是 manwu ch20 的 kindDoubaoVideo，
	// 只是触发源模型不同（促销族 vs seedance-2-5）。
	DefaultDoubaoFastTargetModel = "seedance-2-0-fast"

	// DefaultDoubaoFastTriggerSeconds 是规则十的时长上界（秒，**含**）。
	//
	// 老板口径：15 秒以下包含 15 秒 → Fast，>15 秒留在 seedance-2-5。
	DefaultDoubaoFastTriggerSeconds = 15

	// DoubaoFastMinSeconds 是目标链路 seedance-2-0-fast 的**硬下界**（秒，含）。
	//
	// 来源是 relay/channel/task/manwu 的 doubao20FastMinDuration = 4：
	// 小于 4 秒的请求改写过去会被上游拒单，所以留在源链路（豆包 2.5 支持
	// 任意正整数秒）。
	DoubaoFastMinSeconds = 4
)

// DoubaoFastShortReadyFor 判断「规则十对某个源模型是否生效」。
//
// 只认 DefaultDoubaoFastSourceModel 一个名字：这是给 seedance-2-5 做的
// 专属优化，不是通用分流器。运营想换源模型必须改代码常量 ——
// 避免误配成促销族后与规则六互相抢单。
func DoubaoFastShortReadyFor(p Policy, model string) bool {
	if !p.Enabled || !p.DoubaoFastShortEnabled {
		return false
	}
	return strings.TrimSpace(model) == DefaultDoubaoFastSourceModel
}

// IsDoubaoFastShortEnabledFor 是读侧的规则十判定（供中间件调用）。
func IsDoubaoFastShortEnabledFor(model string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return DoubaoFastShortReadyFor(current, model)
}

// IsDoubaoFastShortEnabled 判断规则十开关当前是否开启（管理端展示用）。
func IsDoubaoFastShortEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Enabled && current.DoubaoFastShortEnabled
}

// DoubaoFastShortTriggerSeconds 返回规则十的生效时长上界（秒，含）。
func DoubaoFastShortTriggerSeconds() int {
	mu.RLock()
	defer mu.RUnlock()
	return current.DoubaoFastShortTriggerSecondsOrDefault()
}

// DoubaoFastShortTriggerSecondsOrDefault 返回策略里的上界，没配就用默认 15。
//
// 为什么缺省不是 0：0 会让「只打开开关」变成「一秒都不改派」，
// 运营看到按钮亮着却没有任何效果，且没有任何报错。
func (p Policy) DoubaoFastShortTriggerSecondsOrDefault() int {
	if p.DoubaoFastShortTriggerSeconds > 0 {
		return p.DoubaoFastShortTriggerSeconds
	}
	return DefaultDoubaoFastTriggerSeconds
}

// DoubaoFastShortTarget 返回规则十的目标模型名（空则回落默认值）。
func DoubaoFastShortTarget() string {
	mu.RLock()
	defer mu.RUnlock()
	if target := strings.TrimSpace(current.DoubaoFastShortTargetModel); target != "" {
		return target
	}
	return DefaultDoubaoFastTargetModel
}

// WithDoubaoFastShortEnabled 返回「把规则十开关设成 enabled」的策略副本。
//
// 纯函数，不落库 —— 落库由调用方用 model.UpdateOption 完成，
// 与 WithShortFastEnabled 等同一套做法。
func WithDoubaoFastShortEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.DoubaoFastShortEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyDoubaoFastShortEnabled 把开关设到**给定策略**上（批量改字段时用）。
func ApplyDoubaoFastShortEnabled(p Policy, enabled bool) Policy {
	out := clonePolicy(p)
	out.DoubaoFastShortEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}
