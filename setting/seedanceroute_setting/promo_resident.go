package seedanceroute_setting

// ════════════════════════════════════════════════════════════════════════════
// 促销族（seedance2.5-特惠）常驻改派 + 自营短档（2026-11-23 老板口径）
// ════════════════════════════════════════════════════════════════════════════
//
// 老板口径原文：
//
//	「你把常驻的模型改为 wan3.0-video-tj 这个，然后有任务访问这个模型，
//	 带参考视频的就留在这个模型，否则大于 15 秒的路由到 seedance-2-5，
//	 小于等于 15 秒的路由到 seedance-2-0-fast，然后做一个开关可以手动切换
//	 是否路由到 sd-seedance-2.0fast 这个模型（这个模型必须是 9:16 的才路由）」
//
// 落地方式：**尽量复用已有开关**，只新增两处配置：
//
//	① source_overrides[特惠].promo_resident_model = "wan3.0-video-tj"
//	   → 「没有别的规则接手时」的常驻执行链路（本文件）；
//	② promo_short_own_enabled = true
//	   → ≤15 秒且 9:16 的请求走自营 sd-seedance-2.0fast（本文件）。
//
// 其余三段全部沿用现有开关，语义只是**改了阈值/文案**：
//
//	> 15 秒 → seedance-2-5        ← 规则一（seconds_rule_enabled + seconds_trigger_seconds=15）
//	≤ 15 秒 → seedance-2-0-fast   ← 规则六豆包 Fast（short_fast_enabled + short_trigger_seconds=15）
//	带参考视频/音频/首尾帧        ← 什么规则都不命中 → 落回常驻 tj（②之外的兜底）
//
// 为什么常驻要用 source_overrides 里的**模型名**而不是一个 bool：只有特惠要走
// 客美 tj，活动版（按次 ¥4.5/次）必须留在自己的链路；用「该源模型的常驻目标」
// 表达，活动版不配这个字段就天然不受影响，不需要在中间件里写死模型名。

const (
	// DefaultPromoResidentModel 常驻执行模型（客美 Kemeitv 的 tj 档）。
	DefaultPromoResidentModel = "wan3.0-video-tj"

	// DefaultPromoShortOwnModel 自营短档（漫屋 ch20：固定 720p / 9:16 /
	// 4–15 秒 / **9 张**参考图）。
	DefaultPromoShortOwnModel = "sd-seedance-2.0fast"

	// DefaultPromoShortFullModel 满血档（漫屋 ch20：豆包官网 Seedance 2.0
	// **标准版**，固定 720p / 4–15 秒，按秒 ¥0.25）。
	DefaultPromoShortFullModel = "seedance-2-0"

	// PromoShortFullMinSeconds / MaxSeconds 满血档的物理时长区间。
	//
	// 2026-11-22 用户口径：标准版只到 15 秒（Fast 才是 4–30）。
	// 越界不路由（落豆包 Fast），不做就近吸附 —— 吸附是静默改档。
	PromoShortFullMinSeconds = 4
	PromoShortFullMaxSeconds = 15

	// PromoShortOwnRatio 自营档只在 9:16 时启用（老板口径「必须是 9:16 才路由」）。
	// 自营档物理上固定 9:16，别的比例改派过去会被钉成 9:16 —— 那是静默改画幅。
	PromoShortOwnRatio = "9:16"

	// PromoShortOwnMinSeconds / MaxSeconds 自营档的物理时长区间。
	// 越界不路由（落豆包 Fast），不做就近吸附 —— 吸附是静默改档。
	PromoShortOwnMinSeconds = 4
	PromoShortOwnMaxSeconds = 15

	// PromoShortOwnMaxImages 自营档的参考图上限（Manwu 契约 **9 张**）。
	//
	// ⚠️ 2026-11-22 官网真机取证（Video 档上传弹层原文）：
	//   "Upload up to 12 reference files, with a maximum of 9 images, 3 videos,
	//    and 3 audio clips"
	// 旧值 4 读的是 **Agent 档**文案（"Upload up to 4 images"），两档不同。
	//
	// 目标 adaptor 的 tiktokMaxReferenceImages 也是 9 —— 路由层必须与它同值：
	// 这里写小了，9 张的请求会被挡回豆包 Fast（客户按 9 张准备素材，
	// 却永远拿不到自营档）；写大了会让请求在白等一轮后才被 adaptor 400。
	PromoShortOwnMaxImages = 9
)

// PromoResidentModelFor 返回该源模型的常驻改派目标（空 = 不改派，走源链路）。
//
// 只读 source_overrides[模型].promo_resident_model：没有这个字段的源模型
// （活动版）保持原链路，不存在「跟着母本一起被改派」的连带风险。
func PromoResidentModelFor(sourceModel string) string {
	mu.RLock()
	defer mu.RUnlock()
	override, ok := current.overrideFor(sourceModel)
	if !ok {
		return ""
	}
	return trimModelName(override.PromoResidentModel)
}

// PromoShortOwnEnabled 判断「≤阈值 且 9:16 → 自营 sd- 档」这条分支是否启用。
func PromoShortOwnEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.PromoShortOwnEnabled
}

// PromoShortOwnEnabledFor 判断该源模型是否走自营短档分支。
//
// 与其它短时长分支同一口径：还要过按源模型的 short_rule_enabled，
// 否则「打开按钮」会顺带把按次档（活动版）也带进短时长链路。
func PromoShortOwnEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if !promoShortOwnReadyFor(current, sourceModel) {
		return false
	}
	if override, ok := current.overrideFor(sourceModel); ok && override.PromoShortOwnEnabled != nil {
		return *override.PromoShortOwnEnabled
	}
	return true
}

// WithPromoShortOwnEnabled 返回「把自营短档开关设成 enabled」的策略副本（不落库）。
//
// ⚠️ 打开自营短档时顺手关掉满血档：两者三选一互斥（都不勾 → 豆包 Fast）。
// 只在 enabled=true 时联动 —— 关闭一条往往只是想换另一条，不该顺手全关。
func WithPromoShortOwnEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.PromoShortOwnEnabled = enabled
	if enabled {
		out.PromoShortFullEnabled = false
	}
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyPromoShortOwnEnabled 在给定策略副本上设置开关（与其它开关一起落库时用）。
func ApplyPromoShortOwnEnabled(p Policy, enabled bool) Policy {
	p.PromoShortOwnEnabled = enabled
	if enabled {
		p.PromoShortFullEnabled = false
	}
	p.UpdatedAt = nowUnix()
	return p
}

// PromoShortFullEnabledFor 判断该源模型是否走满血档分支。
//
// 与自营短档同口径：还要过按源模型的 short_rule_enabled，否则「打开按钮」
// 会顺带把按次档（活动版）也带进短时长链路。
//
// 两个档位同时打开只可能来自 SQL 手改（写入口互斥）。读侧兜底：
// 中间件先判自营档，9:16 归它；满血档只接它接不住的其它比例。
func PromoShortFullEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	if !promoShortFullReadyFor(current, sourceModel) {
		return false
	}
	if override, ok := current.overrideFor(sourceModel); ok && override.PromoShortFullEnabled != nil {
		return *override.PromoShortFullEnabled
	}
	return true
}

// WithPromoShortFullEnabled 返回「把满血档开关设成 enabled」的策略副本（不落库）。
//
// ⚠️ 打开满血档时顺手关掉自营短档：两者三选一互斥（都不勾 → 豆包 Fast）。
func WithPromoShortFullEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.PromoShortFullEnabled = enabled
	if enabled {
		out.PromoShortOwnEnabled = false
	}
	out.UpdatedAt = nowUnix()
	return out
}

// ApplyPromoShortFullEnabled 在给定策略副本上设置开关（与其它开关一起落库时用）。
func ApplyPromoShortFullEnabled(p Policy, enabled bool) Policy {
	p.PromoShortFullEnabled = enabled
	if enabled {
		p.PromoShortOwnEnabled = false
	}
	p.UpdatedAt = nowUnix()
	return p
}

// promoShortFullReadyFor 判断满血档分支是否就绪（供 IsAnyRouteEnabled 用）。
func promoShortFullReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.PromoShortFullEnabled &&
		p.shortRuleEnabledFor(sourceModel)
}

// IsPromoShortFullEnabled 判断开关本体是否打开（给管理接口/前端状态用）。
func IsPromoShortFullEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.PromoShortFullEnabled
}

// promoResidentConfiguredFor 判断该源模型是否配了常驻改派目标（不看开关）。
//
// 只认 source_overrides 里的字段：常驻改派不是全局开关，
// 它是「这个源模型没被其它规则接走时落到哪个模型」的数据。
func promoResidentConfiguredFor(p Policy, sourceModel string) bool {
	override, ok := p.overrideFor(sourceModel)
	if !ok {
		return false
	}
	return trimModelName(override.PromoResidentModel) != ""
}

// promoShortOwnReadyFor 判断自营短档分支是否就绪（供 IsAnyRouteEnabled 用）。
//
// 与 shortFastReadyFor 同口径：总开关 + 源模型清单 + 按源模型的短时长闸门。
// 漏判会让「按钮打开但中间件早退」变成没有任何报错的静默故障。
func promoShortOwnReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.PromoShortOwnEnabled &&
		p.shortRuleEnabledFor(sourceModel)
}

// IsPromoShortOwnEnabled 判断开关本体是否打开（给管理接口/前端状态用）。
func IsPromoShortOwnEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.PromoShortOwnEnabled
}

// PrimarySourceTriggerSeconds 返回**主源模型**的规则一时长下界（秒，0 = 不设下界）。
//
// 给管理快照/前端文案用：豆包卡的副文案写的是「超过 N 秒才改派豆包」，
// 这个 N 必须读真实配置，不能写死 20（2026-11-20 前的旧口径）或 15。
func PrimarySourceTriggerSeconds() int {
	mu.RLock()
	defer mu.RUnlock()
	return current.SecondsTriggerSecondsFor(current.PrimarySourceModel())
}
