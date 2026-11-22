// Package seedanceroute_setting 的「hk-wan3 → 客美 tj」手动改派开关。
//
// 背景（2026-11-22 用户需求）：hk-wan3（渠道 17 有赞）与 wan3.0-video-tj
// （渠道 32 客美 Kemeitv）是同一档 wan3.0 商品的两种供货：
//
//	客户请求  hk-wan3           渠道 17 有赞（type 17，¥0.18/秒起）
//	内部改名为 wan3.0-video-tj   渠道 32 客美（type 71）
//
// 换货是**经营决定**，所以做成手动开关，不做自动判定：
//
//	打开 = 不带参考视频的 hk-wan3 请求改派客美 tj；
//	关闭（默认）= 完全不介入，行为与加这个开关之前逐字节一致。
//
// ⚠️ 带参考视频的请求**不改派**：客美自 2026-11-20 起把参考视频秒数并入
// 计费，而 hk-wan3 不收这部分钱 —— 改派过去会让同一个模型名的账单变高。
//
// 与 Policy.Enabled（促销族规则总开关）无关：不同商品、不同源模型。
package seedanceroute_setting

// Wan3HkModel 是本开关的**源模型**（对外名，客户请求里写的那个）。
const Wan3HkModel = "hk-wan3"

// IsWan3HkWan3TjEnabled 判断「hk-wan3 → 客美 tj」改派开关当前是否开启。
func IsWan3HkWan3TjEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Wan3HkWan3TjEnabled
}

// WithWan3HkWan3TjEnabled 返回「把开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成。
func WithWan3HkWan3TjEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.Wan3HkWan3TjEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}
