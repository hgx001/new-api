package constant

import "strings"

// ════════════════════════════════════════════════════════════════════════════
// CapCut（剪映国际版）五档 —— 能力定义的**唯一来源**
// ════════════════════════════════════════════════════════════════════════════
//
// 背景：CapCut 五档是**五个独立商品**，不是同一模型的不同别名：
//   - 上游官网真名各不相同（seedance_2.0_mini / seedance_2.0_fast /
//     seedance_2.0 / seedance_2.5_flash / seedance_2.5）；
//   - 时长上界不同（2.5 与 2.5 Flash = 30 秒，其余 = 15 秒）；
//   - 720P 倍率不同（mini / fast = ×1.5，2.0 / 2.5 / 2.5 Flash = ×2.0）。
//
// 五档**统一按秒计费**（ModelPrice 配 480p 基准单价，720p 由 size 倍率乘回），
// 所以前端「/秒 vs /次」必须全部按秒显示。
//
// 为什么要有这个文件：判定点散落在多处（广场白名单 isAllowedPricingModel、
// 前端 isPerSecondModel、前端示例代码与参数表）。复制一份模型就要在每处各写
// 一遍名字 —— 于是必然出现「加了模型但没上广场」「上广场了却显示成按次」这类
// 静默不一致，而且没人会发现。
//
// 这个文件把「谁是 CapCut 五档」集中成一份定义，所有判定点改为查它：
// **新增成员只改这一处，其余全部自动同步。**
//
// ⚠️ 为什么放 constant 包：model 包不能 import relay/... （会成 import 环），
// 而广场白名单（model/pricing.go）必须能引用这份定义。constant 包不 import
// 任何业务包（已核实），所以 model / relay / middleware / setting 都能单向依赖它。
//
// ⚠️ 必须用**精确集合**，不能用前缀：
//   公开名 `cp-seedance-2.5flash` **包含**子串 `cp-seedance-2.5`，任何
//   strings.Contains / 前缀匹配都会把 2.5 Flash 误判成 2.5 档（时长上界、
//   720P 倍率都跟着错）。查表 = 精确匹配，天然免疫。
//
// ⚠️ `cp-` 前缀是为了与豆包官网档位区分：豆包是 `seedance-2-0-fast`
// （4–30 秒任意秒），这里是 CapCut 官网档位（4–15 秒、参考图 ≤9）。
// 两者同名族但**不互为别名**，归一表分开登记，路由不互相命中。

const (
	// Capcut20MiniModel 是 CapCut 的 **Seedance 2.0 Mini** 档（最便宜的一档）。
	//
	// 时长 4–15 秒；参考图 ≤9；480P 基准价 ¥0.05/秒，720P ¥0.075/秒（×1.5）。
	Capcut20MiniModel = "cp-seedance-2.0mini"

	// Capcut20FastModel 是 CapCut 的 **Seedance 2.0 Fast** 档。
	//
	// 时长 4–15 秒；参考图 ≤9；480P 基准价 ¥0.08/秒，720P ¥0.12/秒（×1.5）。
	Capcut20FastModel = "cp-seedance-2.0fast"

	// Capcut20Model 是 CapCut 的 **Seedance 2.0** 档（标准档）。
	//
	// 时长 4–15 秒；参考图 ≤9；480P 基准价 ¥0.15/秒，720P ¥0.30/秒（×2.0）。
	Capcut20Model = "cp-seedance-2.0"

	// Capcut25FlashModel 是 CapCut 的 **Seedance 2.5 Flash** 档。
	//
	// 时长 4–30 秒（官网 2.5 系列）；参考图 ≤9；480P 基准价 ¥0.15/秒，
	// 720P ¥0.30/秒（×2.0）。
	//
	// ⚠️ 公开名 `cp-seedance-2.5flash` **包含**子串 `cp-seedance-2.5`：
	//   任何用 strings.Contains / 前缀匹配来判定「是不是 2.5 档」的写法都会把
	//   本档误判成 2.5。本包内所有按档位分支的逻辑一律走**精确匹配**
	//   （map 查表 / == 比较）。
	//
	// 与 Capcut25Model 是**两个独立商品**：上游真名不同（seedance_2.5 vs
	// seedance_2.5_flash）、时长上界相同但定价独立，**不做互为别名**。
	Capcut25FlashModel = "cp-seedance-2.5flash"

	// Capcut25Model 是 CapCut 的 **Seedance 2.5** 档（最高档）。
	//
	// 时长 4–30 秒（官网 2.5 系列）；参考图 ≤9；480P 基准价 ¥0.20/秒，
	// 720P ¥0.40/秒（×2.0）。
	Capcut25Model = "cp-seedance-2.5"
)

// CapcutFamily 是 CapCut（剪映国际版）五档的**成员集合**。
//
// 这是能力定义的唯一来源。判定点（广场白名单、计费单位、前端参数表）都查它，
// 不各自维护名单。
//
// ⚠️ 新增成员只改这里 —— 其余判定点自动同步。
var CapcutFamily = map[string]bool{
	Capcut20MiniModel:  true,
	Capcut20FastModel:  true,
	Capcut20Model:      true,
	Capcut25FlashModel: true,
	Capcut25Model:      true,
}

// CapcutPerSecondModels 是族内**按秒计费**的成员。
//
// 按秒 = 售价 = 基准价 × 时长倍率 × 分辨率倍率
// （EstimateBilling 返回 {"seconds": N, "size": R}）。
// 前端据此显示「/秒」而不是「/次」。
//
// ⚠️ CapCut 五档**全部**按秒，没有按次档 —— 本表与族集合内容相同，
// 但保持独立集合是为了与 Seedance25 促销族的结构对齐：将来若新增按次档，
// 这里只需把它移出去，前端判定自动跟着改。
var CapcutPerSecondModels = map[string]bool{
	Capcut20MiniModel:  true,
	Capcut20FastModel:  true,
	Capcut20Model:      true,
	Capcut25FlashModel: true,
	Capcut25Model:      true,
}

// IsCapcutFamily 判断模型名是否属于 CapCut 五档。
//
// 对外模型名可能有首尾空格（生产上真出现过「 seedance2.5-特惠」），
// 所以先 TrimSpace 再比对 —— 与 model/log.go 里那条 Trim 口径一致。
func IsCapcutFamily(model string) bool {
	return CapcutFamily[strings.TrimSpace(model)]
}

// IsCapcutPerSecond 判断模型名是否属于 CapCut 族内的**按秒**成员。
func IsCapcutPerSecond(model string) bool {
	return CapcutPerSecondModels[strings.TrimSpace(model)]
}
