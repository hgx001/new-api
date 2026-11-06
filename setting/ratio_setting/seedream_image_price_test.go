package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 豆包系图片两个模型的定价（2026-11-07 上架，用户口径「定价两毛一张」）：
//   - Seedream 5.0 Flash（豆包官网）
//   - Seedream 4.5（dola 官网）
//
// 两个关键约束：
//   ① 新模型 ¥0.2/张；
//   ② **不得**把既有的 Nano Banana Pro（¥0.3/张）一并降价 ——
//      「两毛一张」针对的是新增的两个模型，不是整个图片品类。
func TestSeedreamImagePriceIsTwoMaoPerImage(t *testing.T) {
	InitRatioSettings()

	for _, name := range []string{"Seedream 5.0 Flash", "Seedream 4.5"} {
		price, ok := GetModelPrice(name, false)
		require.True(t, ok, "%s must have an explicit price", name)
		// 内部按 USD 等值存储（对外显示按 ¥ 换算），0.2/7.3。
		require.InDelta(t, 0.2/USD2RMB, price, 1e-12, "%s must be ¥0.2 per image", name)
	}
}

// Nano Banana Pro 保持 ¥0.3/张：新增模型降价不得牵连既有商品。
func TestNanoBananaProPriceUnchanged(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("Nano Banana Pro", false)
	require.True(t, ok, "Nano Banana Pro must keep an explicit price")
	require.InDelta(t, 0.3/USD2RMB, price, 1e-12, "Nano Banana Pro must stay at ¥0.3 per image")
}

// 上游档位 id 不对外暴露 —— 只在 adaptor 的 modelSpecs 与 ArcReel 内部存在，
// 不该出现在价格表里（否则客户可能直接按上游 id 调用）。
func TestSeedreamUpstreamModelIdsAreNotPriced(t *testing.T) {
	InitRatioSettings()

	for _, upstream := range []string{"doubao-seedream-5-0-flash", "dola-seedream-4-5"} {
		_, ok := GetModelPrice(upstream, false)
		require.False(t, ok, "upstream id %s must not be a public priced model", upstream)
	}
}
