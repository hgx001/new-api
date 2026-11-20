// Package seedanceroute_setting 的「带参考视频的万相请求 → hk-wan3」开关。
//
// 背景（2026-11-20 用户口径）：
//
//	万相族在本站有三条在售链路，对**参考视频**的处置完全不同：
//
//	  wan3.0-video-官网  DashScope 适配器只下发 prompt/resolution/ratio/duration，
//	                    参考图/视频/音频**整段丢弃** —— 客户传了也等于没传；
//	  wan3.0-video-tj    客美渠道，2026-11-20 起参考视频秒数**并入计费**
//	                    （2 秒生成 + 2 秒参考 = 按 4 秒收）；
//	  hk-wan3            有赞 WAN3 官渠，**参考视频不额外收费**
//	                    （上游只按生成秒数算，本站也只取生成秒数）；
//
// 所以带 `reference_video` 的请求应该走 hk-wan3：客户拿到真正带素材的成片，
// 且不多付参考视频的钱。这是**链路纠正**而不是价格试验 —— 默认开启。
//
// 生效范围（中间件 middleware/wan3_ref_video_hk_route.go 负责）：
//
//	· 源模型 = 官网 / tj / prime-tj 三者之一（万相族），且带参考视频；
//	· 生成时长必须落在 **4–15 秒**：hk-wan3 下界 4 秒（更低必 400），
//	  且带参考视频时生成时长上限 15 秒（上游硬约束）；
//	· 画幅不能是 adaptive（hk-wan3 没有这一档，会回落 16:9）；
//	· 不能带首尾帧（hk-wan3 的首尾帧与参考素材互斥）；
//	· 素材数量不能超 hk-wan3 的上限（图 10 / 视频 5 / 音频 5 / 合计 20）。
//
// 任何一条不满足就**留在原链路**（开关写着开也不改派）：宁可少省一次成本，
// 也不能把一条本来能跑完的请求改成必然 400。
package seedanceroute_setting

// Wan3RefVideoHkTargetModel 是改派的**目标模型**（有赞 WAN3 官渠）。
//
// 为什么写死常量、不给 Policy 字段：目标链路的准入（4–15 秒、素材上限、
// 首尾帧互斥、resolution+ratio 契约）全部按 hk-wan3 的能力写死在中间件里，
// 开放成可配置项等于允许配一个「看起来能路由、实际必挂」的模型名。
const Wan3RefVideoHkTargetModel = "hk-wan3"

// Wan3FamilySourceModels 是这条路由适用的**源模型**（万相族三条在售链路）。
//
// 只认这三个名字：wan3.0-720p-官网 / wan3.0-720p-特惠 等走的是别的链路与
// 别的价格口径，不在本开关范围内 —— 一起改派会让「按 720P 特惠价买的客户
// 拿到 hk-wan3 的货」，账单与成本都对不上。
const (
	// Wan3TjModel 是客美渠道的 tj 档（2026-11-19 上线）。
	Wan3TjModel = "wan3.0-video-tj"

	// Wan3PrimeTjModel 是客美渠道的 prime-tj 档。
	Wan3PrimeTjModel = "wan3.0-video-prime-tj"
)

var Wan3FamilySourceModels = []string{
	Wan3GuanwangModel,
	Wan3TjModel,
	Wan3PrimeTjModel,
}

// Wan3RefVideoHkMinSeconds / max 是 hk-wan3 这条路由允许的生成时长区间（秒）。
//
//   - 下界 4：hk-wan3 上游 4 秒起（显式 2–3 秒会 400）；
//   - 上界 15：hk-wan3 带参考视频时生成时长上限 15 秒（与有赞标准版同一条约束）。
//
// 超过这个区间的请求**不路由**，留在源链路 —— 改派过去就是必然 400。
const (
	Wan3RefVideoHkMinSeconds = 4
	Wan3RefVideoHkMaxSeconds = 15
)

// IsWan3RefVideoHkEnabled 判断「带参考视频 → hk-wan3」开关当前是否生效。
//
// ⚠️ 三态语义：nil（配置里没这个字段）= 开启。旧的生产 option JSON 里
// 没有这个字段，解析后是 nil —— 这里返回 true 意味着**升级当天即生效**，
// 这是刻意的（见包头注释：留在官网等于丢素材）。
func IsWan3RefVideoHkEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if current.Wan3RefVideoHkEnabled == nil {
		return true
	}
	return *current.Wan3RefVideoHkEnabled
}

// WithWan3RefVideoHkEnabled 返回「把开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成。
func WithWan3RefVideoHkEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	value := enabled
	out.Wan3RefVideoHkEnabled = &value
	out.UpdatedAt = nowUnix()
	return out
}
