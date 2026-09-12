package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// gpt-5.6 家族定价：luna 为基准，terra = 5x luna，sol = 20x luna。
// 后端按 (prompt + completion*completionRatio) * modelRatio * groupRatio 结算，
// modelRatio 等比放大即等比放大整单费用，无需动 completionRatio。
func TestDefaultModelRatioGpt56Family(t *testing.T) {
	InitRatioSettings()

	luna, ok, _ := GetModelRatio("gpt-5.6-luna")
	require.True(t, ok, "gpt-5.6-luna must have an explicit ratio")
	require.Equal(t, 0.064212, luna, "luna price must stay unchanged")

	terra, ok, _ := GetModelRatio("gpt-5.6-terra")
	require.True(t, ok, "gpt-5.6-terra must have an explicit ratio")
	require.Equal(t, 0.32106, terra)
	require.InDelta(t, 5*luna, terra, 1e-12, "terra must be 5x luna")

	sol, ok, _ := GetModelRatio("gpt-5.6-sol")
	require.True(t, ok, "gpt-5.6-sol must have an explicit ratio")
	require.Equal(t, 1.28424, sol)
	require.InDelta(t, 20*luna, sol, 1e-12, "sol must be 20x luna")
}

// wan3.0-video 按秒计费：480P 基准 ¥0.27/秒，USD 计价（USD2RMB=7.3）。
// 0.27/7.3 = 0.0369863；任务链路按 ModelPrice * seconds * size(1/2/4) 扣费。
func TestDefaultModelPriceWan3PerSecond(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan3.0-video", false)
	require.True(t, ok, "wan3.0-video must have an explicit per-second price")
	require.InDelta(t, 0.27/USD2RMB, price, 1e-7, "480P base must equal ¥0.27/sec in USD")
	require.InDelta(t, 0.0369863, price, 1e-7, "480P base must be ¥0.27/sec in USD")

	primePrice, ok := GetModelPrice("wan3.0-video-prime", false)
	require.True(t, ok, "wan3.0-video-prime must have an explicit per-second price")
	require.InDelta(t, 0.405/USD2RMB, primePrice, 1e-8, "Prime 480P base must be ¥0.405/sec")
	require.InDelta(t, 1.5*price, primePrice, 1e-8, "Prime price must be 1.5x standard")
}

// sd-2.5 按次计费：上游 ¥2/次，我方 3 倍定价 ¥6/次，USD 计价（USD2RMB=7.3）。
// 6/7.3 = 0.821918；任务链路按 ModelPrice 固定扣费，禁止乘 seconds。
func TestDefaultModelPriceSD25PerUse(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("sd-2.5", false)
	require.True(t, ok, "sd-2.5 must have an explicit per-use price")
	require.InDelta(t, 6.0/USD2RMB, price, 1e-6, "sd-2.5 must equal ¥6/call in USD")
	require.InDelta(t, 0.821918, price, 1e-6, "sd-2.5 must be ¥6/call in USD")
}

func TestDefaultModelPriceAutoDLPerSecond(t *testing.T) {
	InitRatioSettings()

	want := 0.1 / USD2RMB
	models := []string{
		"autodl:minimax-h3-text-to-video",
		"autodl:minimax-h3-lightx2v-v5",
		"autodl:minimax-h3-lightx2v-v5-15s",
		"autodl:minimax-h3-b99-12s",
		"autodl:minimax-h3-u24",
		"autodl:minimax-h3-u08",
		"autodl:minimax-h3-image-audio-10s",
		"autodl:minimax-h3-image-audio-15s",
		"autodl:minimax-h3-lipsync",
	}

	for _, model := range models {
		price, ok := GetModelPrice(model, false)
		require.True(t, ok, "AutoDL model must have an explicit per-second price: %s", model)
		require.InDelta(t, want, price, 1e-12, "AutoDL base price must be ¥0.10/sec: %s", model)
	}
}
