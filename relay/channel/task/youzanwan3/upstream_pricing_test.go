package youzanwan3

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// realUpstreamCatalog 是 2026-10-29 从生产上游 /v1/models 抓回来的原文，
// 保持逐字一致 —— 这组数据的价值就在于它反映上游真实长什么样，
// 手写一个「理想结构」测不出真实字段名（unitPrice / resolutionPrices 混用）。
const realUpstreamCatalog = `{
  "object": "list",
  "data": [
    {
      "id": "wan3.0-video",
      "object": "model",
      "name": "Wan3 标准版",
      "billingUnit": "second",
      "currency": "⚡",
      "unitPrice": "0.18",
      "durationPricing": {"enabled": false, "bands": []},
      "durations": {"min": 2, "max": 30},
      "parameters": {"resolution": ["480P", "720P", "1080P"]}
    },
    {
      "id": "wan3.0-video-prime",
      "object": "model",
      "name": "Wan3 Prime",
      "billingUnit": "second",
      "currency": "⚡",
      "resolutionPrices": {"720P": "0.15", "1080P": "0.20"},
      "durations": {"min": 2, "max": 30},
      "supportsSmartDuration": false,
      "parameters": {"resolution": ["720P", "1080P"]}
    }
  ]
}`

func decodeCatalog(t *testing.T) []UpstreamModel {
	t.Helper()
	var parsed upstreamModelList
	require.NoError(t, common.Unmarshal([]byte(realUpstreamCatalog), &parsed))
	require.Len(t, parsed.Data, 2)
	return parsed.Data
}

func TestCostsByResolutionUniformPrice(t *testing.T) {
	models := decodeCatalog(t)
	costs, err := models[0].CostsByResolution()
	require.NoError(t, err)
	// 三档同价：上游只给 unitPrice，档位列表来自 parameters.resolution。
	assert.Equal(t, map[string]float64{
		"480P":  0.18,
		"720P":  0.18,
		"1080P": 0.18,
	}, costs)
}

func TestCostsByResolutionTieredPrice(t *testing.T) {
	models := decodeCatalog(t)
	costs, err := models[1].CostsByResolution()
	require.NoError(t, err)
	assert.Equal(t, map[string]float64{
		"720P":  0.15,
		"1080P": 0.20,
	}, costs)
}

// 上游如果改了计价口径，宁可同步失败也不能按错的数据改价。
func TestCostsByResolutionRejectsUnsupportedShapes(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*UpstreamModel)
		want   string
	}{
		{"按次计价", func(m *UpstreamModel) { m.BillingUnit = "request" }, "only per-second pricing"},
		{"非点数单位", func(m *UpstreamModel) { m.Currency = "USD" }, "1:1 CNY assumption"},
		{"开启阶梯时长", func(m *UpstreamModel) { m.DurationPricing.Enabled = true }, "duration-banded"},
		{"没有价格", func(m *UpstreamModel) {
			m.UnitPrice = ""
			m.ResolutionPrices = nil
		}, "neither unitPrice nor resolutionPrices"},
		{"价格为 0", func(m *UpstreamModel) { m.UnitPrice = "0" }, "non-positive"},
		{"价格非数字", func(m *UpstreamModel) { m.UnitPrice = "abc" }, "invalid price"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			models := decodeCatalog(t)
			model := models[0]
			model.ResolutionPrices = nil
			tc.mutate(&model)
			_, err := model.CostsByResolution()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// 用生产真实数据 + 当前策略，算出来的售价必须与线上完全一致。
// 这条断言是「动态调价不会偷偷改价」的核心保证。
func TestPlanSellingPricesMatchesProductionPolicy(t *testing.T) {
	models := decodeCatalog(t)
	plans, err := PlanSellingPrices(models, productionMarkupFor)
	require.NoError(t, err)
	require.Len(t, plans, 2)

	video := plans[0]
	assert.Equal(t, "wan3.0-video", video.Model)
	assert.Equal(t, map[string]float64{"480P": 0.23, "720P": 0.23, "1080P": 0.23}, video.Prices)
	assert.Equal(t, "480P", video.BaseResolution)
	assert.Equal(t, 0.23, video.BasePrice)
	assert.Equal(t, map[string]float64{"480P": 1.0, "720P": 1.0, "1080P": 1.0}, video.Ratios)

	prime := plans[1]
	assert.Equal(t, "wan3.0-video-prime", prime.Model)
	// 每档加价 ¥0.10：720P 0.15 + 0.10 = 0.25；1080P 0.20 + 0.10 = 0.30
	// （2026-10-29 第二轮：老板要求 720P 由 ¥0.20 提到 ¥0.25）
	assert.Equal(t, map[string]float64{"720P": 0.25, "1080P": 0.30}, prime.Prices)
	assert.Equal(t, "720P", prime.BaseResolution)
	assert.Equal(t, 0.25, prime.BasePrice)
	assert.Equal(t, map[string]float64{"720P": 1.0, "1080P": 1.2}, prime.Ratios)
}

// 上游降价时售价跟着降，倍率按新成本比例重算。
func TestPlanSellingPricesFollowsUpstreamPriceChange(t *testing.T) {
	models := decodeCatalog(t)
	// 1080P 成本从 0.20 涨到 0.40（翻倍），加价不变。
	models[1].ResolutionPrices = map[string]string{"720P": "0.15", "1080P": "0.40"}
	plans, err := PlanSellingPrices(models, productionMarkupFor)
	require.NoError(t, err)
	prime := plans[1]
	// 0.40 + 0.10 = 0.50；倍率 = 0.50 / 0.25（720P 基准）= 2.0
	assert.Equal(t, map[string]float64{"720P": 0.25, "1080P": 0.50}, prime.Prices)
	assert.Equal(t, 2.0, prime.Ratios["1080P"])
}

// 缺加价配置时必须报错，不能按 0 加价（等于成本价卖出）。
func TestPlanSellingPricesRejectsMissingMarkup(t *testing.T) {
	models := decodeCatalog(t)
	_, err := PlanSellingPrices(models, func(model, resolution string) (float64, bool) {
		return 0, false
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no markup configured")
}

// 单档模型（既没有 resolution 列表也没有 resolutionPrices）也要能算。
func TestPlanSellingPricesSingleTier(t *testing.T) {
	models := []UpstreamModel{{
		ID:          "single-tier",
		BillingUnit: "second",
		Currency:    creditCurrency,
		UnitPrice:   "1.00",
	}}
	plans, err := PlanSellingPrices(models, func(model, resolution string) (float64, bool) {
		return 0.5, true
	})
	require.NoError(t, err)
	require.Len(t, plans, 1)
	assert.Equal(t, 1.5, plans[0].BasePrice)
	assert.Equal(t, map[string]float64{"": 1.0}, plans[0].Ratios)
}

// productionMarkupFor 复刻生产策略（setting 包默认值），测试不依赖全局状态。
func productionMarkupFor(model, resolution string) (float64, bool) {
	switch model {
	case "wan3.0-video":
		return 0.05, true
	case "wan3.0-video-prime":
		// 2026-10-29 第二轮起两档统一加价 ¥0.10（720P ¥0.25 / 1080P ¥0.30）。
		return 0.10, true
	}
	return 0, false
}
