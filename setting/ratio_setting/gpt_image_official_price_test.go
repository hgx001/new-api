package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// gpt-image-2.5-官方（2026-11-07 上架）：官方渠道图片模型，走
// POST /v1/images/generations，按张计费 ¥0.15/张，与尺寸无关。
//
// 上游同 gpt-image-2（独立命名以区分二手渠道的 gpt-image-2 / gpt-image-2.5，
// 见 model/pricing.go 的 isAllowedPricingModel 注释）。
//
// 关键约束：
//   - ModelPrice 必须显式配价 —— 不配价时 ModelPriceHelperPerCall 的
//     「查不到价」分支会静默免单（¥0），比报错更难发现。
//   - 不得影响 gpt-image-2（¥0.13/张）和 gpt-image-2.5（¥0.15/张）
//     两个二手渠道模型的既有价格。
func TestGptImageOfficialPrice(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("gpt-image-2.5-官方", false)
	require.True(t, ok, "gpt-image-2.5-官方 must have an explicit price")
	require.InDelta(t, 0.15/USD2RMB, price, 1e-12,
		"gpt-image-2.5-官方 must be ¥0.15 per image")
}

// gpt-image-2.5-官方 不是按秒计费 —— isPerSecondModel 应返回 false
// （前端 model-helpers.test.ts 的 perRequest 列表锁定同一口径）。
func TestGptImageOfficialIsPerRequest(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("gpt-image-2.5-官方", false)
	require.True(t, ok)
	require.NotZero(t, price)
}
