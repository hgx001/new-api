package controller

import (
	"strings"
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
//
// ⚠️ known 集合里包含新旧**四个**模型（2026-11-18 起上游新上架 wan-3.0 /
// SJ-wan3-prime，旧名仍在列）。上游目录只回其中一部分时，其余几个会被
// 报成「已下架」—— 这是**已知噪音**：上游目录接口在两次刷新之间可能只
// 返回部分模型。告警文案的用途是「提醒看一眼」，不是「按它自动下架」。
func TestUpstreamCatalogWarnings(t *testing.T) {
	models := []youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-pro"},
	}
	warnings := upstreamCatalogWarnings(models)
	require.NotEmpty(t, warnings)
	joined := strings.Join(warnings, " | ")
	assert.Contains(t, joined, "wan3.0-video-pro", "新增模型要告警")
	assert.Contains(t, joined, "wan3.0-video-prime", "下架模型要告警")
}

// 上游目录与 known 集合完全一致时不告警。
func TestUpstreamCatalogWarningsQuietWhenUnchanged(t *testing.T) {
	models := []youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-prime"},
		{ID: "wan-3.0"},
		{ID: "SJ-wan3-prime"},
		{ID: "wan-3.0-pro"},
	}
	assert.Empty(t, upstreamCatalogWarnings(models))
}

// 上游 ID → 对外公开名的映射：告警里要说客户认识的名字。
//
// ⚠️ 这条锁住一个容易写反的地方：known 集合里放的**必须是上游 ID**
// （上游目录里只可能出现 ID，公开名 wan-3.0-yz 永远不会出现）。
// 把公开名混进 known 集合 → 「上游已下架」那条检查永远报假告警。
func TestUpstreamCatalogWarningsUsePublicNameForDownstream(t *testing.T) {
	// 上游有 wan-3.0、没有其它 → 只该报一条下架告警（用的是公开名）。
	models := []youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-prime"},
		{ID: "wan-3.0"},
		{ID: "SJ-wan3-prime"},
		{ID: "wan-3.0-pro"},
	}
	_ = models

	// 上游少了 wan-3.0（公开名 wan-3.0-yz）→ 告警必须点名公开名。
	warnings := upstreamCatalogWarnings([]youzanwan3.UpstreamModel{
		{ID: "wan3.0-video"},
		{ID: "wan3.0-video-prime"},
		{ID: "SJ-wan3-prime"},
		{ID: "wan-3.0-pro"},
	})
	joined := strings.Join(warnings, " | ")
	assert.Contains(t, joined, "wan-3.0-yz", "下架告警要用对外公开名")
	assert.NotContains(t, joined, "wan-3.0：", "不能把上游 ID 直接当成商品名列出来")
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

// 上游 ID → 对外公开名的映射必须正确。
//
// ⚠️ 这是改名（wan-3.0 → wan-3.0-yz）之后最容易出错的一环：
// ModelPrice 与倍率表的键都是**对外公开名**（计费链路读客户请求里的名字），
// 而同步任务拿到的是**上游 ID**。映射错了，价格就写到没人调用的键上
// → 真正在用的 wan-3.0-yz 查不到价 → 按秒计费落到「查不到价」分支，
// 任务静默算成 ¥0（免费白送）。
func TestPublicModelNameMapsUpstreamToPublic(t *testing.T) {
	assert.Equal(t, "wan-3.0-yz", youzanwan3.PublicModelName("wan-3.0"))
	// 未登记的上游 ID 原样返回（绝大多数模型公开名 == 上游名）。
	for _, id := range []string{"wan3.0-video", "wan3.0-video-prime", "SJ-wan3-prime", "wan-3.0-pro"} {
		assert.Equal(t, id, youzanwan3.PublicModelName(id))
	}
}

// 钉死价模型的基准档必须是**最低价**那一档。
//
// ModelPrice 只能配一个数，广场徽标直接显示它；基准档配最高价的话，
// 低档位客户就会按高档价付钱（多收），或高档位按低档价结算（少收）。
// 所以这里锁死「基准档 = 售价最低的档位」。
func TestLowestPricedTierPicksCheapest(t *testing.T) {
	resolution, price := lowestPricedTier(map[string]float64{
		"720P":  0.20,
		"1080P": 0.25,
	})
	assert.Equal(t, "720P", resolution)
	assert.Equal(t, 0.20, price)

	// 并列时按档位名排序取第一个（结果确定，不随 map 迭代顺序变）。
	resolution, price = lowestPricedTier(map[string]float64{
		"1080P": 0.25,
		"720P":  0.25,
	})
	assert.Equal(t, "1080P", resolution)
	assert.Equal(t, 0.25, price)
}
