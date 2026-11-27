// Package seedanceroute_setting 的「万相官网 → wan3-新大陆」手动改派开关。
//
// 背景（2026-11-27 用户口径）：wan3.0 商品新增第三条供货链路：
//
//	wan3.0-video-官网  渠道 12 DashScope-Wan3（type 60，官网直连）
//	wan3.0-video-tj    渠道 32 客美 Kemeitv（type 71）
//	wan3-新大陆        渠道 34 二春 Erchun（type 65，上游阿里云 Wan 3.0 新线路）
//
// 三条链路的入参契约同源（resolution + ratio + duration + images/videos/audios），
// 但成本不同：新大陆 480P 成本 0.096 credits/秒，是目前最便宜的 480P 货源。
//
// 开关语义与「官网 → 客美 tj」完全一致：
//
//	打开 = 所有 wan3.0-video-官网 的请求改派 wan3-新大陆；
//	关闭 = 完全不介入，走官网渠道。
//
// ⚠️ 本开关与 Wan3GuanwangTJEnabled **互斥**：两者都表达「官网商品由谁跑」，
// 同时打开会让同一请求被两个中间件先后改写。写入任一为 true 时另一个自动置 false。
package seedanceroute_setting

// Wan3XindaluTargetModel 是改派的**目标模型**（二春 Erchun 渠道 34 的对外名）。
const Wan3XindaluTargetModel = "wan3-新大陆"

// IsWan3GuanwangXindaluEnabled 判断「官网 → 新大陆」改派开关是否开启。
//
// ⚠️ 刻意不看 Policy.Enabled：与客美开关同一口径，不同商品的开关不互相连坐。
func IsWan3GuanwangXindaluEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Wan3GuanwangXindaluEnabled
}

// WithWan3GuanwangXindaluEnabled 返回「把开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption 完成。
func WithWan3GuanwangXindaluEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.Wan3GuanwangXindaluEnabled = enabled
	if enabled {
		// 与「官网 → 客美 tj」互斥：同一商品只能有一个换货目标。
		out.Wan3GuanwangTJEnabled = false
	}
	out.UpdatedAt = nowUnix()
	return out
}
