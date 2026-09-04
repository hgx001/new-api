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
}
