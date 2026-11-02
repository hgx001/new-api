package ratio_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance2.5-活动（2026-11-02 上架）：seedance2.5 促销族的**按次档**。
//
// 它与母本 seedance2.5-特惠 是同一个商品（同货源 wan3.0-video-prime、同链路），
// 只是计费口径从「按秒 ¥0.20/秒」改成「按次 ¥4.5/次、固定 30 秒」。
//
// 本测试锁住三件事：
//  1. 售价精确等于 ¥4.5/次（写错一分就是错账）
//  2. **不能与母本同价** —— 两个名字一个价就失去了「差异化销售」的意义
//  3. 母本仍是按秒 ¥0.20/秒（回归守卫：新模型不能把母本价格带偏）
func TestDefaultModelPriceSeedance25HuoDongPerCall(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice(constant.Seedance25HuoDongModel, false)
	require.True(t, ok, "seedance2.5-活动 必须有明确的按次价，否则会落到「查不到价」分支算成 ¥0")
	require.InDelta(t, 4.5/USD2RMB, price, 1e-12, "seedance2.5-活动 必须等于 ¥4.5/次")
	require.InDelta(t, 0.6164383561643836, price, 1e-12, "seedance2.5-活动 必须等于 ¥4.5/次")

	// 与母本**不同价**：母本是按秒 ¥0.20/秒，活动版是按次 ¥4.5/次。
	// 同价会让「促销版」失去存在意义，也说明有人把两个档位搞混了。
	tehui, ok := GetModelPrice(constant.Seedance25TeHuiModel, false)
	require.True(t, ok, "母本 seedance2.5-特惠 必须有价")
	require.InDelta(t, 0.20/USD2RMB, tehui, 1e-12, "母本仍是按秒 ¥0.20/秒（回归守卫）")
	require.NotEqual(t, tehui, price, "活动版与母本必须不同价：两个名字一个价等于没做促销版")
}

// 活动版是**按次档**，不能被当成按秒模型配价。
//
// 失败形态：有人照抄母本的写法配成 0.20（以为是「便宜版」），
// 或者配成 30 × 0.20 = 6.0（以为「按 30 秒计价」）——
// 两种都让 ModelPrice 与真实计费口径脱节：前者等于白送，
// 后者等于标 ¥6.00 却按 ¥4.50 结算（客户账单与标价对不上）。
func TestDefaultModelPriceSeedance25HuoDongNotPerSecond(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice(constant.Seedance25HuoDongModel, false)
	require.True(t, ok)

	// 母本按秒价 = ¥0.20/秒。30 秒 × ¥0.20 = ¥6.00 —— 活动版若等于这个数，
	// 说明有人按「按秒算 30 秒」配的价，而按次档实际只收 ¥4.50。
	assert.NotEqual(t, 6.0/USD2RMB, price,
		"活动版是按次 ¥4.5/次，不是「按秒价 × 30 秒」= ¥6.00")

	// 也不能等于母本的按秒价（那是母本的档位，不是活动版的）。
	assert.NotEqual(t, 0.20/USD2RMB, price,
		"活动版不能复用母本的按秒价")
}

// 活动版必须**只**存在于按次口径，不能同时出现在按秒集合。
//
// 这是能力组最核心的不变量：按次/按秒互斥。破了这个不变量，
// 前端会显示「/秒」但后端按次结算 —— 静默错价，比报错更难发现。
func TestSeedance25HuoDongBillingUnitMutuallyExclusive(t *testing.T) {
	require.True(t, constant.IsSeedance25PerCall(constant.Seedance25HuoDongModel),
		"活动版必须在按次集合里")
	require.False(t, constant.IsSeedance25PerSecond(constant.Seedance25HuoDongModel),
		"活动版绝不能同时在按秒集合里（静默错价）")

	require.True(t, constant.IsSeedance25PerSecond(constant.Seedance25TeHuiModel),
		"母本必须在按秒集合里")
	require.False(t, constant.IsSeedance25PerCall(constant.Seedance25TeHuiModel),
		"母本不能进按次集合（回归守卫）")
}
