package constant

import "strings"

// ════════════════════════════════════════════════════════════════════════════
// Higgsfield Seedance 2.5（一档）—— 能力定义的**唯一来源**
// ════════════════════════════════════════════════════════════════════════════
//
// ## 对外口径（用户 2026-11-25 定稿）
//
//	sd-seedance-2.5   ¥0.25/秒
//
// 公开名继续用 **`sd-` 中性前缀**（模型 id 对用户可见，不暴露渠道）。
// **分辨率固定 720p**：用户传了别的档位一律 400，绝不静默降/升规格交付
//（这是本商品唯一规格，不是「改编用户选择」）。
// 时长 4–30 秒逐秒可选（官网滑杆 4..30）；画面比例按官网白名单透传。
//
// ## 与本仓库其它近名商品的区别（互不命中，精确集合）
//
//	sd-seedance-2.0fast / sd-seedance-2.0   TikTok Symphony（本仓库 tiktok_family）
//	cp-seedance-2.5                          CapCut（480p/720p、4–30s）
//	el-seedance-2.5                          ElevenLabs
//	seedance-2-5 / seedance-2-0              豆包官网（连字符短名）
//
// ⚠️ 一律**精确匹配**，不能用前缀/Contains：
//
//	`sd-seedance-2.5` 与 `sd-seedance-2.0*` 前缀相近但属于不同上游与不同价，
//	查表匹配天然免疫这类误伤。
const (
	// Higgsfield25Model 是 Higgsfield Seedance 2.5 的对外公开名。
	Higgsfield25Model = "sd-seedance-2.5"
)

// HiggsfieldFamily 是本族的精确集合（定价、路由、前端判定都查它）。
var HiggsfieldFamily = map[string]bool{
	Higgsfield25Model: true,
}

// HiggsfieldPerSecondModels 是按秒计费的子集。
var HiggsfieldPerSecondModels = map[string]bool{
	Higgsfield25Model: true,
}

// IsHiggsfieldFamily 判断是否本族模型（精确匹配，忽略首尾空白）。
func IsHiggsfieldFamily(model string) bool {
	return HiggsfieldFamily[strings.TrimSpace(model)]
}

// IsHiggsfieldPerSecond 判断是否本族按秒计费模型。
func IsHiggsfieldPerSecond(model string) bool {
	return HiggsfieldPerSecondModels[strings.TrimSpace(model)]
}
