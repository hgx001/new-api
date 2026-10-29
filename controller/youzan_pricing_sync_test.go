package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// productionPlans 复刻 2026-10-29 第二轮之后的线上配置：标准版 ¥0.23 三档同价、
// prime 720P ¥0.25 / 1080P ¥0.30（两档加价都是 ¥0.10，倍率 1.2）。
func productionPlans() []youzanwan3.ModelPricePlan {
	return []youzanwan3.ModelPricePlan{
		{
			Model:          "wan3.0-video",
			Prices:         map[string]float64{"480P": 0.23, "720P": 0.23, "1080P": 0.23},
			BaseResolution: "480P",
			BasePrice:      0.23,
			Ratios:         map[string]float64{"480P": 1.0, "720P": 1.0, "1080P": 1.0},
		},
		{
			Model:          "wan3.0-video-prime",
			Prices:         map[string]float64{"720P": 0.25, "1080P": 0.30},
			BaseResolution: "720P",
			BasePrice:      0.25,
			Ratios:         map[string]float64{"720P": 1.0, "1080P": 1.2},
		},
	}
}

// 线上价格就是按这套策略算出来的，所以「再算一遍」必须零变动。
// 这条是动态调价最重要的回归守卫：它一旦红，说明同步任务会自己改自己的价。
func TestDiffYouzanPricesNoChangeAgainstProduction(t *testing.T) {
	current := map[string]float64{
		"wan3.0-video":       0.23 / ratio_setting.USD2RMB,
		"wan3.0-video-prime": 0.25 / ratio_setting.USD2RMB,
	}
	changes, blocked := diffYouzanPrices(productionPlans(), current, 0.30)
	assert.Empty(t, changes, "价格没变时不应产生任何变动项")
	assert.Empty(t, blocked)
}

// 上游调价 12%，在 30% 阈值内 → 记录变动但不算拦截。
func TestDiffYouzanPricesWithinThreshold(t *testing.T) {
	plans := productionPlans()
	plans[1].Prices = map[string]float64{"720P": 0.22, "1080P": 0.30}
	plans[1].BasePrice = 0.22
	current := map[string]float64{
		"wan3.0-video":       0.23 / ratio_setting.USD2RMB,
		"wan3.0-video-prime": 0.25 / ratio_setting.USD2RMB,
	}
	changes, blocked := diffYouzanPrices(plans, current, 0.30)
	assert.Empty(t, blocked)
	require.Len(t, changes, 1)
	assert.Equal(t, "wan3.0-video-prime", changes[0].Model)
	assert.Equal(t, "720P", changes[0].Resolution)
	assert.InDelta(t, 0.12, changes[0].ChangeRatio, 1e-9)
}

// 上游翻倍（+100%）→ 超过 30% 阈值，必须整批拦下。
// 这是防止上游偶发错价直接变成我们售价的最后一道闸。
func TestDiffYouzanPricesBlocksLargeChange(t *testing.T) {
	plans := productionPlans()
	plans[1].Prices = map[string]float64{"720P": 0.40, "1080P": 0.30}
	plans[1].BasePrice = 0.40
	current := map[string]float64{
		"wan3.0-video":       0.23 / ratio_setting.USD2RMB,
		"wan3.0-video-prime": 0.25 / ratio_setting.USD2RMB,
	}
	changes, blocked := diffYouzanPrices(plans, current, 0.30)
	require.NotEmpty(t, blocked)
	assert.Contains(t, blocked[0], "wan3.0-video-prime")
	assert.NotEmpty(t, changes)
}

// 从来没配过价的模型（ModelPrice 里没有）算首次配置，不是「变动」，
// 不该被阈值拦下 —— 否则新上架的模型永远同步不进去。
func TestDiffYouzanPricesSkipsModelsWithoutCurrentPrice(t *testing.T) {
	changes, blocked := diffYouzanPrices(productionPlans(), map[string]float64{}, 0.30)
	assert.Empty(t, changes)
	assert.Empty(t, blocked)
}

// 上游目录多出/少了模型要告警，但不自动上架或下架。
func TestUpstreamCatalogWarnings(t *testing.T) {
	models := []youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-pro"},
	}
	warnings := upstreamCatalogWarnings(models)
	require.Len(t, warnings, 2)
	joined := warnings[0] + warnings[1]
	assert.Contains(t, joined, "wan3.0-video-pro", "新增模型要告警")
	assert.Contains(t, joined, "wan3.0-video-prime", "下架模型要告警")
}

func TestUpstreamCatalogWarningsQuietWhenUnchanged(t *testing.T) {
	models := []youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-prime"},
	}
	assert.Empty(t, upstreamCatalogWarnings(models))
}

// 跟价别名表本身不能指向不存在的上游模型。
//
// 同时钉住一条业务规则：seedance2.5-特惠 **不得**出现在这里。
// 它独立定价（720P ¥0.20 / 1080P ¥0.30），一旦被登记进来就会被同步任务
// 覆盖成 prime 的价，而且两边都不报错 —— 只能靠这条断言拦住。
func TestAliasTargetsExistInCatalog(t *testing.T) {
	for alias, target := range youzanAliasTargets {
		assert.NotEmpty(t, alias)
		assert.Contains(t, []string{"wan3.0-video", "wan3.0-video-prime"}, target,
			"别名 %s 指向了未知的上游模型 %s", alias, target)
	}
	_, followed := youzanAliasTargets["seedance2.5-特惠"]
	assert.False(t, followed,
		"seedance2.5-特惠 独立定价，不能登记为跟价别名（否则会被同步覆盖成 prime 的价）")
}
