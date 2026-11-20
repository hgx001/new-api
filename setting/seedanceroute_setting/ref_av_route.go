// Package seedanceroute_setting 的「规则九：音视频参考改派 → cp-seedance-2.5」。
//
// 背景（2026-11-16 老板需求）：
//
//	有赞上游 2026-11-15 切换后，标准版 wan3.0-video 的 media 素材**仅收
//	reference_image**（youzanwan3 adaptor 的闸），促销族（落标准版执行）与
//	wan3.0-video 直连带参考视频/音频的请求一律 400 拒单。
//	而 CapCut 链路 2026-11-15 起参考音视频均可透传（prepareCapcutRoute ①，
//	音视频下发 Worker 上传素材槽）。
//
//	所以带音视频参考的请求不再拒单，改派 CapCut 满血档 —— 客户的诉求
//	（多模态参考出片）在另一条链路上仍然被满足，且 fail-loud 变成
//	fail-over：拒单退款 → 正常交付。
//
// ── 触发面 ──
//
//	youzanwan3 adaptor 的闸拦的是「除 prime 外的所有模型」，对外即两类：
//	促销族源模型（policy.IsSourceModel）与 wan3.0-video 直连。
//	中间件在 IsSourceModel 早退的**两侧**各挂一次判定（见 middleware）。
//	prime 直连不触发 —— 有赞 prime 路由仍收音视频，adaptor 对它豁免。
//
// ── 与规则七/八的关系 ──
//
//	目标模型、准入与改写与规则七/八完全同源（prepareCapcutRoute，
//	固定 cp-seedance-2.5，理由与 DefaultTotalFallbackTargetModel 相同：
//	该准入只适配了这一条契约，开放成可配置 = 配错即静默挂）。
//	区别只在触发条件：
//
//	  规则七：内容/参数命中电商条件（其注释里「带参考视频一律不路由」
//	          是 2026-11-15 放开前的旧口径，实际准入已放开）；
//	  规则八：无条件全量（逃生口）；
//	  规则九：**带参考音视频**（能力驱动：源链路物理接不住的请求）。
//
//	规则九排在规则三（首尾帧）之后：带首尾帧 + 音视频的请求 CapCut
//	接不住首尾帧语义，宁可留在源链路吃 400（原因准确），也不伪装成参考图。
package seedanceroute_setting

// DefaultRefAVTargetModel 是规则九的目标模型（CapCut Seedance 2.5 Flash）。
//
// 2026-11-17 口径（老板）：促销族所有 CapCut 出口（规则六/七/八/九）统一走
// cp-seedance-2.5flash；规则九的触发面正是这两个促销族源模型（外加 wan3.0-video
// 直连），所以跟着规则七/八一起换。豆包备胎（DefaultFailoverAltModel）本就是 2.5 Flash。
//
// 固定成常量、不给 Policy 字段的理由与 DefaultTotalFallbackTargetModel 一致：
// prepareCapcutRoute 只适配了这一条契约（480p/720p、4–30 秒、CapCut 比例表、
// 参考视频/音频各 ≤5 个）。要换目标时改这一行（并补对应准入），不要加配置项。
const DefaultRefAVTargetModel = "cp-seedance-2.5flash"

// refAVCapcutEnabled 是三态开关的纯判定（供 IsAnyRouteEnabled 复用，
// 后者持锁调用，不能再走带锁的 IsRefAVRouteEnabled）。
func refAVCapcutEnabled(p Policy) bool {
	return p.RefAVCapcutEnabled == nil || *p.RefAVCapcutEnabled
}

// IsRefAVRouteEnabled 判断规则九的全局开关（三态：nil = 默认开启）。
//
// 为什么默认开启而其它规则默认关闭：其它规则是**成本优化**（不改派也能跑，
// 只是贵），默认关闭符合「升级当天行为不变」；规则九是**能力补位** ——
// 不改派这些请求就是 400 拒单退款，不存在「留在源链路」这个可用选项。
// 默认关闭等于把一个已修好的洞继续敞着。
//
// 与规则总开关 Enabled 的关系：规则九**不要求** Enabled=true。
// 理由与规则八相同：它服务的是「源链路物理接不住」的请求，与运营的
// 成本调度开关是两个维度；且 IsAnyRouteEnabled 早退在中间件最前面，
// 若依赖 Enabled，运营关总开关时会顺带复活拒单问题，排查方向会被带偏。
func IsRefAVRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return refAVCapcutEnabled(current)
}

// IsRefAVRouteEnabledFor 判断「规则九对这个源模型是否生效」。
//
// 2026-11-20 起是两层与：全局开关（ref_av_capcut_enabled）**且**
// 该源模型没被单独关掉（source_overrides[模型].ref_av_rule_enabled）。
//
// 为什么需要按源模型关：规则九原本给「有赞链路仅收参考图」补位；
// 促销族母本换成 mw-sd2.5-flash 后它自己收 5 视频 + 5 音频，不改派反而更短、
// 更便宜。但 wan3.0-video 直连依旧只收参考图，规则九对它仍然必要 ——
// 所以只能按源模型停，不能动全局开关。
//
// ⚠️ 仍然**不要求** Policy.Enabled（规则总开关）：理由与 IsRefAVRouteEnabled 相同。
func IsRefAVRouteEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return refAVCapcutEnabled(current) && current.refAVRuleEnabledFor(sourceModel)
}
