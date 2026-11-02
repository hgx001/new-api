package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按次档模型被条件路由改派后的计费口径测试。
//
// 背景（2026-11-02 上架 seedance2.5-活动 时踩到的坑）：
//
//	seedance2.5-活动 是**按次**档（¥4.5/次、固定 30 秒），母本 seedance2.5-特惠
//	是**按秒**档（¥0.20/秒）。两者共用同一条路由规则（30 秒 → 豆包链路）。
//
//	改派到豆包后，adaptor（manwu）对按次模型返回 nil 倍率 —— 没有 seconds。
//	于是 applyRouteBillingSeconds 会从 context 里补上 seconds=30，
//	再被 applyTaskOtherRatios 乘进 ¥4.5 的预扣额度 → **收 ¥135**。
//
// 这条测试盯的正是「按次档不补秒数」这一步：算错的后果是**多收 30 倍**，
// 而且没有任何报错，只能靠客户投诉 + 对账发现。
//
// ⚠️ 为什么不靠 PriceData.UsePrice 判定：母本（按秒）**也**配了 ModelPrice，
//	UsePrice 为 true —— 它区分不了按次与按秒。必须走能力组（constant 包），
//	与「谁是这一族 + 谁按次计费」的唯一来源保持一致。

// 按次档源模型（seedance2.5-活动）：30 秒改派到豆包后**不能**补 seconds。
func TestApplyRouteBillingSeconds_SkipsPerCallSourceModel(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel, nil)
	// 目标 adaptor（manwu）对按次模型返回 nil，所以 OtherRatios 是空的。
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	assert.NotContains(t, info.PriceData.OtherRatios, "seconds",
		"按次档补 seconds 会把 ¥4.5/次 乘成 ¥135 —— 30 倍超收")

	quota := applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
	assert.Equal(t, info.PriceData.Quota, quota, "按次档的预扣额度必须等于 ModelPrice 本身")
}

// 母本（按秒档）不受影响：30 秒改派后仍要补 seconds，按 ¥0.20×30 = ¥6.00 收。
//
// 这是**回归守卫**：本次改动不能把母本的按秒口径一起关掉 ——
// 那是少收 29 秒的钱（¥0.20 而不是 ¥6.00），同样是无声的钱差。
func TestApplyRouteBillingSeconds_StillBillsPerSecondMotherModel(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25TeHuiModel, nil)
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	require.Equal(t, 30.0, info.PriceData.OtherRatios["seconds"],
		"母本是按秒档，30 秒改派后必须补上秒数倍率")
}

// 判据必须走**计费口径**（源模型），不是执行模型。
//
// 场景：客户请求 seedance2.5-活动（按次），改派到 seedance-2-5 执行。
// 如果判据误用了 info.OriginModelName（执行模型 = 按次的豆包模型），
// 会「碰巧」得到正确结果 —— 但那是**偶然**对，逻辑是错的：
// 万一将来有个按秒模型被改派到按次模型，按秒档就会漏补秒数（少收钱）。
// 所以这里断言：判据只看源模型，与执行模型无关。
func TestApplyRouteBillingSeconds_PerCallDecisionUsesBillingModelOnly(t *testing.T) {
	// 执行模型是一个**明确按秒**的模型（wan3.0-720p-特惠），
	// 源模型是按次的活动版 —— 按次档仍然不能补 seconds。
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25HuoDongModel, nil)
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	assert.NotContains(t, info.PriceData.OtherRatios, "seconds",
		"按次档的判定只看源模型（计费口径），执行模型是按秒也不能改变结论")
}

// 非本族的按次模型（心笔 wan3.0-720p-按次）走的是**另一条链路**，
// 不受条件路由影响 —— 这里锁住「能力组之外不误伤」的行为：
// 判据只对能力组内的按次成员生效，其他模型一律按原逻辑处理。
func TestApplyRouteBillingSeconds_DoesNotAffectNonFamilyModels(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", "wan3.0-720p-按次", nil)
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	require.Equal(t, 30.0, info.PriceData.OtherRatios["seconds"],
		"能力组之外的模型行为不变（它们的按次口径由各自的 adaptor 保证）")
}

// 母本与活动版的**对称性**守卫：两个模型能力相同，只有计费口径不同。
// 这个测试把「同能力、不同口径」这条需求写死，防止将来有人把两个档位混成一样。
func TestSeedance25Family_PerSecondAndPerCallHaveDifferentBilling(t *testing.T) {
	require.True(t, constant.IsSeedance25PerSecond(constant.Seedance25TeHuiModel),
		"母本必须是按秒档")
	require.True(t, constant.IsSeedance25PerCall(constant.Seedance25HuoDongModel),
		"活动版必须是按次档")
	require.False(t, constant.IsSeedance25PerCall(constant.Seedance25TeHuiModel),
		"母本不能同时是按次档（会静默错价）")
	require.False(t, constant.IsSeedance25PerSecond(constant.Seedance25HuoDongModel),
		"活动版不能同时是按秒档（会静默错价）")
}

// 预扣额度口径锁死：¥4.5/次 换算成 quota 后必须**原样**预扣，
// 不被任何时长倍率改动。
//
// ¥4.5/次 → 0.6164383561643836 (USD) × 500000 = 308219.178... → 308219
func TestPerCallPreChargeQuota_NotMultipliedBySeconds(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel, nil)
	// 模拟 ModelPriceHelperPerCall 算出的预扣额度（¥4.5/次）。
	info.PriceData.Quota = 308219
	info.PriceData.UsePrice = true

	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	quota := applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
	assert.Equal(t, 308219, quota, "¥4.5/次 的预扣额度必须是 308219，不能是 308219×30")
	// 反向断言：绝不能是 30 倍那个数。
	assert.NotEqual(t, 308219*30, quota, "不能收 ¥135（30 倍）")
}

// types.PriceData 的 OtherRatios 在按次档下必须保持为空 ——
// 空 map 与 nil map 在 applyTaskOtherRatios 里的表现一致，
// 但「有 seconds 键」与「没有」在日志与结算上是两回事，必须钉死。
func TestApplyRouteBillingSeconds_PerCallLeavesOtherRatiosEmpty(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel, nil)
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	assert.Empty(t, info.PriceData.OtherRatios,
		"按次档不应产生任何倍率（含 size 分辨率倍率）")
}

// 确认 PriceData.OtherRatios 的类型与 AddOtherRatio 的行为没被这次改动影响。
func TestPerCallOtherRatiosType(t *testing.T) {
	var data types.PriceData
	data.AddOtherRatio("seconds", 30)
	assert.Equal(t, 30.0, data.OtherRatios["seconds"])
}
