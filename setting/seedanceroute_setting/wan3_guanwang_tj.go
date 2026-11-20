// Package seedanceroute_setting 的「万相官网 → 客美 tj」手动改派开关。
//
// 背景（2026-11-19 用户口径）：同一个万相 wan3.0 商品在本站有两条供货链路：
//
//	wan3.0-video-官网  渠道 12 DashScope-Wan3（type 60，官网直连）
//	wan3.0-video-tj    渠道 32 客美 Kemeitv（type 71，上游 ai.kemeitv.cn）
//
// 两条链路的**入参契约基本一致**（resolution + ratio + duration +
// images/videos/audios），但成本与能力不同：
//
//	· 客美 tj 的 480P 成本 ¥0.138/秒，官网 ¥0.20/秒 —— 走客美更便宜；
//	· 官网适配器只下发 prompt/resolution/ratio/duration，
//	  **参考图/视频/音频会被静默丢弃**；客美收 images/videos/audios，
//	  带素材的请求只有走客美才真的用得上素材。
//
// 所以需要一个**手动开关**：运营想换货时打开，全部官网流量改派客美；
// 客美出问题时关掉，流量立刻回官网。不做自动判定 —— 两条链路都是长期
// 在售的货，什么时候切换是经营决定，不是代码能替运营决定的。
//
// ── 这个开关与既有那批规则的关系 ──
//
// 那批规则（Enabled / 规则一~九）全部围绕促销族 seedance2.5 的成本调度，
// 源模型是 seedance2.5-特惠 / -活动。本开关的源模型是 wan3.0-video-官网，
// 两者没有交集，因此：
//
//  1. 本开关**不依赖** Policy.Enabled（规则总开关）—— 用途不同的开关
//     互相连坐只会制造「按钮亮着、实际没生效」的静默故障；
//  2. 中间件是**独立一个**（middleware/wan3_guanwang_tj_route.go），
//     塞进 Seedance25TeHuiRoute 会让它按促销族的源模型清单早退，
//     还要额外加一堆「这个模型不适用那条规则」的例外。
package seedanceroute_setting

// Wan3GuanwangModel 是改派的**源模型**（对外名，客户请求里写的那个）。
//
// 只认这一个名字：官网渠道上另外两个模型（wan3.0-720p-官网 /
// wan3.0-720p-特惠）走的是别的链路与别的价格口径，不在本开关范围内 ——
// 一起改派会让「按 720P 特惠价买的客户拿到 tj 的货」，账单与成本都对不上。
const Wan3GuanwangModel = "wan3.0-video-官网"

// Wan3TjTargetModel 是改派的**目标模型**（客美 Kemeitv 渠道 32 的对外名）。
//
// ⚠️ 不用 wan3.0-video-prime-tj：prime 是客美那边的另一个商品（¥0.24/秒），
// 与官网模型不是同一档货。改派目标必须是同名同档的 tj。
const Wan3TjTargetModel = "wan3.0-video-tj"

// IsWan3GuanwangTjEnabled 判断「官网 → 客美 tj」改派开关当前是否开启。
//
// ⚠️ 刻意不看 Policy.Enabled：见包头注释第 1 条。
func IsWan3GuanwangTjEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Wan3GuanwangTJEnabled
}

// WithWan3GuanwangTjEnabled 返回「把开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成，
// 与 WithShortLinkEnabled / WithTotalFallbackEnabled 同一套做法。
func WithWan3GuanwangTjEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.Wan3GuanwangTJEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}
