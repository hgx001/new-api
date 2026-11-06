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

	// 摘要四行相加必须正好等于单价，用户才能自己把账加出来。
	//
	// PRO 6000 的包月机型价是 ¥6,580（用户指定），拆成
	// 套餐 ¥300 + 配置 ¥180 + 显卡 ¥6,100。
	// 用户明确要求「显卡应该是最贵的、占大头」—— 显卡在这一档占 92.7%。
	assert.Equal(t, 300.0, quote.PlanPrice)
	assert.Equal(t, 180.0, quote.SpecPrice)
	assert.Equal(t, 6100.0, quote.GpuPrice)
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

	// 用户明确要求 RTX PRO 6000 按 ¥19.90/小时卖，由 HourlyPrice 显式指定，
	// 不再走「系数 6 × 基准 ¥2.50」的推导。
	assert.Equal(t, 19.9, quote.UnitPrice)
	assert.Equal(t, 19.9, quote.Total)
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

	// 上海 1.08 倍；7 天档 168 小时、9 折。¥19.90 × 1.08 = ¥21.49
	// （roundServerMoney 按分取整后再乘小时数）。
	assert.Equal(t, 1.08, quote.RegionFactor)
	assert.Equal(t, 21.49, quote.UnitPrice)
	assert.Equal(t, 3610.32, quote.ListTotal)
	assert.Equal(t, 3249.29, quote.Total)
	assert.InDelta(t, 361.03, quote.Saved, 0.001)
	assert.Equal(t, int64(168*3600), quote.PeriodSeconds)
}

// GPU 三档的按量时价是用户直接指定的三个数，任何一个被系数推导覆盖都会测挂。
func TestComputeServerQuoteGPUHourlyPrices(t *testing.T) {
	cases := []struct {
		specId string
		gpu    string
		price  float64
		// 显卡那部分：必须是机型价里最大的一项，否则「GPU 占大头」就名不副实
		gpuPrice float64
	}{
		{"gpu-16c64g", "RTX 4090", 5.6, 3.1},
		{"gpu-16c64g-rtx5090", "RTX 5090", 7.8, 5.3},
		{"gpu-24c96g-rtxpro6000", "RTX PRO 6000", 19.9, 16.9},
	}
	for _, tc := range cases {
		t.Run(tc.gpu, func(t *testing.T) {
			quote, err := ComputeServerQuote(ServerConfig{
				Region:     "guangzhou",
				Plan:       "gpu",
				SpecId:     tc.specId,
				Mbps:       5,
				DurationId: "1h",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.price, quote.UnitPrice)
			assert.Equal(t, tc.price, quote.Total)
			// 摘要分项必须自洽：套餐 + 配置 + 显卡 = 该档时价。
			assert.Equal(t, 2.5, quote.PlanPrice)
			assert.Equal(t, tc.gpuPrice, quote.GpuPrice)
			assert.Equal(t,
				roundServerMoney(tc.price-2.5-tc.gpuPrice), quote.SpecPrice)
			// 显卡必须是三项里最大的：主机只收成本价，钱主要花在卡上。
			assert.Greater(t, quote.GpuPrice, quote.SpecPrice,
				"显卡加价必须高于配置加价")
			assert.Greater(t, quote.GpuPrice, quote.PlanPrice,
				"显卡加价必须高于套餐基准价")
		})
	}
}

// 按量的 HourlyPrice 不能污染包月口径：包月仍走 Multiplier / MonthlyPrice。
func TestComputeServerQuoteHourlyPriceDoesNotLeakIntoPrepaid(t *testing.T) {
	cases := []struct {
		specId string
		price  float64
	}{
		{"gpu-16c64g", 1280.0},
		{"gpu-16c64g-rtx5090", 1792.0},
		{"gpu-24c96g-rtxpro6000", 6580.0},
	}
	for _, tc := range cases {
		t.Run(tc.specId, func(t *testing.T) {
			quote, err := ComputeServerQuote(ServerConfig{
				Region:     "guangzhou",
				Plan:       "gpu",
				SpecId:     tc.specId,
				Mbps:       5,
				DurationId: "1mo",
			})
			require.NoError(t, err)
			assert.Equal(t, tc.price, quote.UnitPrice)
		})
	}
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

func TestComputeServerQuoteRTXPRO6000KeepsNineteenNinetyPerHour(t *testing.T) {
	quote, err := ComputeServerQuote(ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       5,
		DurationId: "1h",
	})
	require.NoError(t, err)

	// 用户要求这档卖 ¥19.90/小时，由 HourlyPrice 显式指定。
	assert.Equal(t, 19.9, quote.UnitPrice)
	assert.Equal(t, 19.9, quote.Total)
	assert.Equal(t, "hour", quote.Unit)
	assert.Equal(t, int64(3600), quote.PeriodSeconds)

	// 包月价由 MonthlyPrice 显式指定：¥6,580/月（用户指定）。
	//
	// 按量与包月是两套独立定价：时价 ¥19.90、月价 ¥6,580，
	// 各自读自己的字段，互不推导、互不污染。
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
	// 摘要分项自洽：套餐 ¥300 + 配置 ¥180 + 显卡 ¥6,100 = 月单价 ¥6,580
	assert.Equal(t, 300.0, monthly.PlanPrice)
	assert.Equal(t, 180.0, monthly.SpecPrice)
	assert.Equal(t, 6100.0, monthly.GpuPrice)
}

func TestServerRTXPRO6000SpecMatchesPublishedConfig(t *testing.T) {
	// 配置按实物照抄，采购与展示都以这份为准：
	// 24 vCPU / 96 GB 内存 / 系统盘 30 GB + 数据盘 200 GB、
	// 单卡 RTX PRO 6000 96 GB。
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
	// 磁盘拆成系统盘 + 数据盘：合并成一个 1000 GB 的数字既对不上交付内容，
	// 也让「配置 +¥17.40/小时」看起来像在卖硬盘。数据盘 200 GB 够用。
	assert.Equal(t, 30, spec.Disk)
	assert.Equal(t, 200, spec.DataDisk)
	require.NotNil(t, spec.GPU)
	assert.Equal(t, "RTX PRO 6000", spec.GPU.Model)
	assert.Equal(t, 96, spec.GPU.VRAM)
	assert.Equal(t, 1, spec.GPU.Count)
	// 包月价显式指定为 ¥6,580/月；按量时价显式指定为 ¥19.90/小时。
	// Multiplier 保留给未显式定价的档位与历史数据展示，两个口径都不再靠它互推。
	require.NotNil(t, spec.MonthlyPrice)
	assert.Equal(t, 6580.0, *spec.MonthlyPrice)
	require.NotNil(t, spec.HourlyPrice)
	assert.Equal(t, 19.9, *spec.HourlyPrice)
	assert.Equal(t, 6.0, spec.Multiplier)
	// 大卡机器默认 100 Mbps：拉几十 GB 的权重，5M / 20M 根本不现实。
	assert.Equal(t, 100, spec.DefaultMbps)
}

// 摘要的每一行都必须是真钱：套餐 + 配置 + 显卡 恒等于机型价，
// 且不能出现负数加价（负数说明基准价定高了，把显卡的空间吃掉了）。
func TestServerCatalogPriceBreakdownIsConsistent(t *testing.T) {
	billings := []struct {
		pricing string
		mode    string
	}{{ServerBillingHourly, "1h"}, {ServerBillingPrepaid, "1mo"}}

	for _, plan := range ServerPlans {
		for _, spec := range plan.Specs {
			for _, billing := range billings {
				name := plan.Id + "/" + spec.Id + "/" + billing.mode
				t.Run(name, func(t *testing.T) {
					quote, err := ComputeServerQuote(ServerConfig{
						Region:     "guangzhou",
						Plan:       plan.Id,
						SpecId:     spec.Id,
						Mbps:       5,
						DurationId: billing.mode,
					})
					require.NoError(t, err)

					// 带宽在广州 5M 档是 0，所以单价就是机型价本身。
					machinePrice := roundServerMoney(
						quote.PlanPrice + quote.SpecPrice + quote.GpuPrice)
					assert.Equal(t, quote.UnitPrice, machinePrice,
						"套餐 + 配置 + 显卡必须等于机型价")
					assert.GreaterOrEqual(t, quote.SpecPrice, 0.0,
						"配置加价不能为负：负值说明基准价吃掉了显卡的空间")

					if spec.GPU != nil {
						// GPU 机器的钱主要花在卡上，这是用户明确的产品口径。
						assert.Greater(t, quote.GpuPrice, 0.0, "GPU 档必须单独计价")
						assert.Greater(t, quote.GpuPrice,
							quote.PlanPrice+quote.SpecPrice,
							"显卡应该是最贵的一项、占大头")
					} else {
						assert.Equal(t, 0.0, quote.GpuPrice, "无卡机型不该有显卡价")
					}
				})
			}
		}
	}
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
