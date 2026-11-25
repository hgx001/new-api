package model

// ════════════════════════════════════════════════════════════════════════════
// 模型广场置顶机制：`models.sort_weight`
// ════════════════════════════════════════════════════════════════════════════
//
// 改造前：前端写死一份 `PINNED_MODELS` 名单（`web/default/src/features/pricing/
// lib/filters.ts`），每上架一个新品都要改代码 + 发版，和后端数据不同步。
//
// 改造后：权重存在 models 表里、由 /api/pricing 下发，前端只按
// `sort_weight DESC, 名称 ASC` 排。**新建模型卡时自动取 MAX+1**，
// 所以「新上架的模型排在广场最前面」是机制，不是手工操作。
//
// 本文件守三件事：
//  1. 新建模型卡自动拿到最大权重（新模型置顶）；
//  2. 老卡片（权重 0）保持 0，不与新模型抢位；
//  3. 显式指定权重时尊重调用方（含负数，用于把模型压到后面）。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupSortWeightDB(t *testing.T) {
	t.Helper()
	setupVendorDB(t)
	require.NoError(t, DB.AutoMigrate(&Channel{}, &Ability{}))
	// pricing 是包级内存缓存，测试间必须清，否则先跑的用例会污染后跑的。
	InvalidatePricingCache()
	t.Cleanup(InvalidatePricingCache)
}

// 新建模型卡必须自动排到最前（MAX+1），无需任何手工设置。
func TestModelInsertAutoAssignsTopSortWeight(t *testing.T) {
	setupSortWeightDB(t)

	// 两张老卡片：权重 0（改造前的全部数据都是这个样子）。
	require.NoError(t, DB.Create(&Model{ModelName: "old-a", Status: 1}).Error)
	require.NoError(t, DB.Create(&Model{ModelName: "old-b", Status: 1}).Error)

	first := &Model{ModelName: "new-1", Status: 1}
	require.NoError(t, first.Insert())
	assert.Equal(t, 1, first.SortWeight,
		"第一张新卡片应拿到 MAX(0)+1 = 1，从而排在所有权重 0 的老卡片之前")

	second := &Model{ModelName: "new-2", Status: 1}
	require.NoError(t, second.Insert())
	assert.Equal(t, 2, second.SortWeight,
		"后上架的权重更大 —— 广场顺序才是「最新在最前」")

	// 老卡片不受影响，仍然是 0（= 不置顶，按名称排在后面）。
	var old Model
	require.NoError(t, DB.Where("model_name = ?", "old-a").First(&old).Error)
	assert.Zero(t, old.SortWeight, "老卡片不能被顺手改写，否则整个广场顺序会抖")
}

// 显式指定权重时不得被自动逻辑覆盖（运营可能想把它压到后面）。
func TestModelInsertRespectsExplicitSortWeight(t *testing.T) {
	setupSortWeightDB(t)

	require.NoError(t, DB.Create(&Model{ModelName: "anchor", Status: 1, SortWeight: 10}).Error)

	pinned := &Model{ModelName: "manual-top", Status: 1, SortWeight: 50}
	require.NoError(t, pinned.Insert())
	assert.Equal(t, 50, pinned.SortWeight, "显式权重优先，不能用 MAX+1 覆盖")

	buried := &Model{ModelName: "manual-bottom", Status: 1, SortWeight: -1}
	require.NoError(t, buried.Insert())
	assert.Equal(t, -1, buried.SortWeight,
		"负数权重是合法的「压到后面」表达，不能被当成「未设置」重新置顶")

	auto := &Model{ModelName: "auto", Status: 1}
	require.NoError(t, auto.Insert())
	assert.Equal(t, 51, auto.SortWeight,
		"不带权重的仍然取当前最大(50)+1，而不是被负数带偏")
}

// 权重必须出现在 /api/pricing 的数据里 —— 否则前端拿不到、机制等于没接。
func TestPricingExposesSortWeight(t *testing.T) {
	setupSortWeightDB(t)

	channel := &Channel{Id: 1, Name: "c1", Type: 1, Status: 1, Models: "seedance-2.0-m5"}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, DB.Create(&Ability{
		Group:     "default",
		Model:     "seedance-2.0-m5",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	require.NoError(t, DB.Create(&Model{
		ModelName:   "seedance-2.0-m5",
		Status:      1,
		SortWeight:  7,
		Description: "test card",
	}).Error)

	InvalidatePricingCache()
	pricing := GetPricing()

	var found bool
	for _, p := range pricing {
		if p.ModelName == "seedance-2.0-m5" {
			found = true
			assert.Equal(t, 7, p.SortWeight, "SortWeight 必须随 /api/pricing 下发，前端才能排序")
		}
	}
	require.True(t, found, "上架的模型必须能在 pricing 里查到（白名单/能力行都对）")
}
