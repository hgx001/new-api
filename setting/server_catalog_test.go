package setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeServerQuoteGPUFlagship(t *testing.T) {
	quote, err := ComputeServerQuote(ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       20,
		DurationId: "1mo",
	})
	require.NoError(t, err)

	// 摘要三行相加必须正好等于单价，用户才能自己把账加出来。
	//
	// PRO 6000 的包月机型价是 ¥6,580（用户指定），不是「基准月价 × 系数」
	// 推出的 ¥7,680，所以加价一项是 ¥6,580 − ¥1,280 = ¥5,300。
	assert.Equal(t, 1280.0, quote.PlanPrice)
	assert.Equal(t, 5300.0, quote.SpecPrice)
	assert.Equal(t, 30.0, quote.BandwidthPrice)
	assert.Equal(t, 6610.0, quote.UnitPrice)
	assert.Equal(t, 6610.0, quote.Total)
	assert.Equal(t, "month", quote.Unit)
	assert.Equal(t, int64(30*24*3600), quote.PeriodSeconds)
}

func TestComputeServerQuoteHourlyKeepsAnchorPrice(t *testing.T) {
	quote, err := ComputeServerQuote(ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       5,
		DurationId: "1h",
	})
	require.NoError(t, err)

	// 用户明确要求 RTX PRO 6000 按 ¥15/小时卖；系数 6 × 基准 2.5。
	assert.Equal(t, 15.0, quote.UnitPrice)
	assert.Equal(t, 15.0, quote.Total)
	assert.Equal(t, "hour", quote.Unit)
	assert.Equal(t, int64(3600), quote.PeriodSeconds)
}

func TestComputeServerQuoteAppliesRegionAndDurationDiscount(t *testing.T) {
	quote, err := ComputeServerQuote(ServerConfig{
		Region:     "shanghai",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       5,
		DurationId: "7d",
	})
	require.NoError(t, err)

	// 上海 1.08 倍；7 天档 168 小时、9 折。
	assert.Equal(t, 1.08, quote.RegionFactor)
	assert.Equal(t, 16.2, quote.UnitPrice)
	assert.Equal(t, 2721.6, quote.ListTotal)
	assert.Equal(t, 2449.44, quote.Total)
	assert.InDelta(t, 272.16, quote.Saved, 0.001)
	assert.Equal(t, int64(168*3600), quote.PeriodSeconds)
}

func TestServerBandwidthLadderKeepsThreeTimesAnchor(t *testing.T) {
	ten, ok := GetServerBandwidthByMbps(10)
	require.True(t, ok)
	hundred, ok := GetServerBandwidthByMbps(100)
	require.True(t, ok)

	// 产品口径：100 Mbps 必须是 10 Mbps 的 3 倍，整条阶梯不能倒挂。
	assert.Equal(t, ten.PricePerMonth*3, hundred.PricePerMonth)

	previousPrice := -1.0
	previousUnit := 1e9
	for _, bandwidth := range ServerBandwidths {
		if previousPrice >= 0 {
			assert.Greater(t, bandwidth.PricePerMonth, previousPrice, "带宽总价必须随档位递增")
		}
		if bandwidth.Mbps > 5 {
			unit := bandwidth.PricePerMonth / float64(bandwidth.Mbps)
			assert.Less(t, unit, previousUnit, "单位带宽价格必须随档位递减")
			previousUnit = unit
		}
		previousPrice = bandwidth.PricePerMonth
	}
}

func TestComputeServerQuoteRejectsUnknownConfig(t *testing.T) {
	base := ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       20,
		DurationId: "1mo",
	}

	cases := map[string]ServerConfig{
		"未知机型":  {Region: base.Region, Plan: "quantum", SpecId: base.SpecId, Mbps: base.Mbps, DurationId: base.DurationId},
		"跨机型规格": {Region: base.Region, Plan: "lite", SpecId: base.SpecId, Mbps: base.Mbps, DurationId: base.DurationId},
		"未知带宽":  {Region: base.Region, Plan: base.Plan, SpecId: base.SpecId, Mbps: 25, DurationId: base.DurationId},
		"未知地区":  {Region: "mars", Plan: base.Plan, SpecId: base.SpecId, Mbps: base.Mbps, DurationId: base.DurationId},
		"未知时长":  {Region: base.Region, Plan: base.Plan, SpecId: base.SpecId, Mbps: base.Mbps, DurationId: "2w"},
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ComputeServerQuote(config)
			assert.Error(t, err)
		})
	}
}

func TestComputeServerQuoteRTXPRO6000KeepsFifteenPerHour(t *testing.T) {
	quote, err := ComputeServerQuote(ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       5,
		DurationId: "1h",
	})
	require.NoError(t, err)

	// 用户要求这档卖 ¥15/小时：系数 6 × 基准时价 ¥2.50。
	assert.Equal(t, 15.0, quote.UnitPrice)
	assert.Equal(t, 15.0, quote.Total)
	assert.Equal(t, "hour", quote.Unit)
	assert.Equal(t, int64(3600), quote.PeriodSeconds)

	// 包月价由 MonthlyPrice 显式指定：¥6,580/月（用户指定）。
	//
	// 注意它不是「基准月价 × 按量系数」—— ¥6,580 ÷ ¥1,280 = 5.140625，
	// 与按量系数 6 不符。按量与包月是两套独立定价，这里各走各的。
	monthly, err := ComputeServerQuote(ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       5,
		DurationId: "1mo",
	})
	require.NoError(t, err)
	assert.Equal(t, 6580.0, monthly.UnitPrice)
	assert.Equal(t, 6580.0, monthly.Total)
	// 摘要分项自洽：套餐 ¥1,280 + 配置加价 = 月单价
	assert.Equal(t, 1280.0, monthly.PlanPrice)
	assert.Equal(t, 5300.0, monthly.SpecPrice)
}

func TestServerRTXPRO6000SpecMatchesPublishedConfig(t *testing.T) {
	// 配置按实物照抄，采购与展示都以这份为准：
	// 24 vCPU / 96 GB 内存 / 1000 GB 磁盘、单卡 RTX PRO 6000 96 GB。
	var spec *ServerSpec
	for i := range ServerPlans {
		if ServerPlans[i].Id != "gpu" {
			continue
		}
		for j := range ServerPlans[i].Specs {
			if ServerPlans[i].Specs[j].Id == "gpu-24c96g-rtxpro6000" {
				spec = &ServerPlans[i].Specs[j]
			}
		}
	}
	require.NotNil(t, spec)
	assert.Equal(t, 24, spec.CPU)
	assert.Equal(t, 96, spec.Memory)
	assert.Equal(t, 1000, spec.Disk)
	require.NotNil(t, spec.GPU)
	assert.Equal(t, "RTX PRO 6000", spec.GPU.Model)
	assert.Equal(t, 96, spec.GPU.VRAM)
	assert.Equal(t, 1, spec.GPU.Count)
	// 包月价显式指定为 ¥6,580/月，按量仍走系数 6
	require.NotNil(t, spec.MonthlyPrice)
	assert.Equal(t, 6580.0, *spec.MonthlyPrice)
	assert.Equal(t, 6.0, spec.Multiplier)
}

// GPU 机型只保留三档在售：4090 / 5090 / RTX PRO 6000。
// 多加一档会让用户以为有另一种配置，实际机器上没有 —— 采购对不上账。
func TestServerGPUPlansSellExactlyThreeSpecs(t *testing.T) {
	var gpu *ServerPlan
	for i := range ServerPlans {
		if ServerPlans[i].Id == "gpu" {
			gpu = &ServerPlans[i]
		}
	}
	require.NotNil(t, gpu)
	ids := make([]string, 0, len(gpu.Specs))
	for _, spec := range gpu.Specs {
		ids = append(ids, spec.Id)
	}
	assert.Equal(t, []string{"gpu-16c64g", "gpu-16c64g-rtx5090", "gpu-24c96g-rtxpro6000"}, ids)
}

func TestServerPeriodSeconds(t *testing.T) {
	month, ok := GetServerDurationById("1mo")
	require.True(t, ok)
	assert.Equal(t, int64(30*24*3600), ServerPeriodSeconds(month))

	year, ok := GetServerDurationById("1y")
	require.True(t, ok)
	// 12 个月 × 30 天 = 360 天：档位文案是「12 个月」，不是自然年。
	assert.Equal(t, int64(360*24*3600), ServerPeriodSeconds(year))

	hour, ok := GetServerDurationById("1h")
	require.True(t, ok)
	assert.Equal(t, int64(3600), ServerPeriodSeconds(hour))
}

func TestServerQuotaForAmountUsesExchangeRate(t *testing.T) {
	// 余额以美元计价：¥5150 ÷ 7.3 × 500000，向上取整到 1 额度。
	assert.Equal(t, 352739727, ServerQuotaForAmount(5150))
	assert.Equal(t, 0, ServerQuotaForAmount(0))
	assert.Equal(t, 0, ServerQuotaForAmount(-1))

	// 反向换算回人民币必须能对回原价（允许 1 分以内的取整误差）。
	assert.InDelta(t, 5150, ServerAmountFromQuota(ServerQuotaForAmount(5150)), 0.01)
}
