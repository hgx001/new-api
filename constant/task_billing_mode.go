package constant

import "strings"

// ════════════════════════════════════════════════════════════════════════════
// 「按次计费」判据 —— 计费链的统一口径
// ════════════════════════════════════════════════════════════════════════════
//
// 为什么需要这个文件：计费链上有**三处**独立判定「这个模型是不是按次计费」，
// 以前各写各的：
//
//	relay/relay_task.go:218       预扣前是否应用 OtherRatios 倍率
//	service/task_billing.go:25    日志文案写「按次计费」还是「计算参数：seconds: N」
//	controller/relay.go:603        轮询阶段是否跳过差额结算
//
// 三处判据一旦不同步，后果都是**静默错价**，而且没有任何报错：
//   - 计费处判按次、日志处判按秒 → 账单金额对，但日志文案骗人，排查时被带偏；
//   - 计费处判按秒、日志处判按次 → 真实超收，但日志写着「按次计费」，事后对账更难查；
//   - 计费处判按次、轮询处判按秒 → 轮询阶段按完成态重算，¥4.5 的固定价被改写。
//
// 所以这里把判据收敛成**一个函数**，三处都调它。
//
// 覆盖两类按次模型：
//
//  1. **能力组按次成员**（constant.IsSeedance25PerCall）
//     seedance2.5-活动 等「一口价」档位 —— 售价固定、不乘任何倍率。
//
//  2. **TASK_PRICE_PATCH 环境变量命中**（constant.TaskPricePatches）
//     站内的 sora 等历史按次模型。它是 env 不是 option，改完要重启容器
//     （见 common/init.go）。本函数读的是**当前进程内**的变量值。
//
// ⚠️ 为什么不把 TaskPricePatches 也收进能力组：它是运维通过环境变量注入的
// 运行时名单，能力组是编译期常量。两者语义不同，但**判定结果必须一致** ——
// 所以统一在这里汇合。
//
// ⚠️ 判据一律用「计费口径模型名」（RelayInfo.BillingModelName = 客户请求的
// 源模型），**不是**实际执行的模型。改派后两者不同：用执行模型判定会让
// 「按次档被改派到按秒链路」时结论漂移 —— 母本（按秒）被改派到按次链路会漏补秒数
// （少收钱），活动版（按次）被改派到按秒链路会乘上秒数（超收 30 倍）。

// IsTaskPerCallModel 判断模型是否按「次」计费。
//
// 返回 true 意味着：
//   - 售价是**一口价**，不乘时长倍率、不乘分辨率倍率；
//   - 计费链不得应用 OtherRatios；
//   - 提交后不得按 adaptor 返回的倍率重算额度；
//   - 轮询阶段不得做完成态差额结算；
//   - 日志文案写「按次计费」。
//
// 空串返回 false（不是按次档）。
func IsTaskPerCallModel(model string) bool {
	target := strings.TrimSpace(model)
	if target == "" {
		return false
	}
	// 能力组按次成员：seedance2.5 促销族的按次档。
	if IsSeedance25PerCall(target) {
		return true
	}
	// TASK_PRICE_PATCH 环境变量命中：站内的历史按次模型（sora 等）。
	for _, name := range TaskPricePatches {
		if strings.TrimSpace(name) == target {
			return true
		}
	}
	return false
}
