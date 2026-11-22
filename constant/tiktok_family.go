package constant

import "strings"

// ════════════════════════════════════════════════════════════════════════════
// 两档视频模型（Seedance 2.0 Fast / 2.0）—— 能力定义的**唯一来源**
// ════════════════════════════════════════════════════════════════════════════
//
// ## 对外口径（用户 2026-11-22 定稿）
//
// 公开名用 **`sd-` 中性前缀**：模型 id 在 new-api 里对用户可见，前缀不能
// 透露渠道来源（`cp-` 是 CapCut、`el-` 是 ElevenLabs、`tk-` 是原站点）。
//
//	sd-seedance-2.0fast   ¥0.10/秒   （fast 档）
//	sd-seedance-2.0       ¥0.22/秒   （标准档）
//
// **固定 720p、固定 9:16** —— 不对用户暴露分辨率与比例选择，参数由服务端定死，
// 用户传了别的值也不会被采纳（宁可忽略，也不要静默降规格按高价卖）。
// 参考图上限跟随既有服务口径（≤4 张）。
//
// ## 为什么要有这个文件
//
// 判定点散落在多处（广场白名单 `model/pricing.go`、适配器 `adaptor.go` 的
// modelSpecs 与计价倍率、前端按秒/按次显示）。每加一档都要在每处各写一遍名字，
// 必然出现「加了模型但没上广场」「上广场了却显示成按次」这类静默不一致。
// 集中成一份定义，其余判定点只查它。
//
// ⚠️ 必须用**精确集合**，不能用前缀/`strings.Contains`：
//
//	`sd-seedance-2.0fast` 包含子串 `sd-seedance-2.0`，前缀匹配会把 fast 档
//	误判成标准档（价格差 2 倍多）。查表 = 精确匹配，天然免疫。
//
// ⚠️ 与 CapCut `cp-seedance-2.0fast`、ElevenLabs `el-seedance-2.0mini`、
//
//	豆包 `seedance-2-0-fast` **都不是别名**：不同官网、不同账号池、不同价，
//	归一表分开登记，路由不互相命中。
const (
	// Tiktok20FastModel 是 fast 档（速度优先、最便宜）。
	Tiktok20FastModel = "sd-seedance-2.0fast"

	// Tiktok20Model 是标准档（画质优先）。
	Tiktok20Model = "sd-seedance-2.0"
)

// TiktokFamily 是这一族的精确集合（定价、路由、前端判定都查它）。
var TiktokFamily = map[string]bool{
	Tiktok20FastModel: true,
	Tiktok20Model:     true,
}

// TiktokPerSecondModels 是按秒计费的子集。
//
// 本族**两档都按秒**，但保留独立表：将来若加入按时长档位（如只卖 5/10/12 秒的
// 整档价），这里可以只登记按秒的那几档，不必改动调用点。
var TiktokPerSecondModels = map[string]bool{
	Tiktok20FastModel: true,
	Tiktok20Model:     true,
}

// IsTiktokFamily 判断是否本族模型（精确匹配，忽略首尾空白）。
func IsTiktokFamily(model string) bool {
	return TiktokFamily[strings.TrimSpace(model)]
}

// IsTiktokPerSecond 判断是否本族按秒计费模型。
func IsTiktokPerSecond(model string) bool {
	return TiktokPerSecondModels[strings.TrimSpace(model)]
}
