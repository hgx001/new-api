package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance2.5-活动 上广场白名单（2026-11-02）。
//
// 活动版是促销商品，必须在广场里能被客户看到 —— 否则建了渠道、配了价，
// 客户在广场找不到入口，等于白做。
//
// ⚠️ 名字必须**精确匹配**：站内还有 seedance-2-5（豆包按次）、seedance-2.5（dola）、
// seedance2.5特惠版（历史旧名）等一堆相近名，前缀匹配会误放行别家模型。
func TestIsAllowedPricingModelSeedance25HuoDong(t *testing.T) {
	require.True(t, isAllowedPricingModel(constant.Seedance25HuoDongModel),
		"seedance2.5-活动 必须出现在模型广场")

	// 母本仍在广场（回归守卫）：两个模型是并列的两个商品，不能只留一个。
	require.True(t, isAllowedPricingModel(constant.Seedance25TeHuiModel),
		"母本 seedance2.5-特惠 必须仍在广场")
}

// 相近名**不得**被活动版的放行规则误命中。
//
// 如果有人把白名单写成 `strings.HasPrefix(name, "seedance2.5")`，
// 就会连带放行 seedance2.5特惠版（历史按次旧名）等不该上架的名字。
//
// ⚠️ 注意：seedance-2-5 / seedance-2.5 / seedance-2-0-fast 是**别的在售商品**
// （豆包官网 / dola 链路），它们本来就在广场里，不在这里做断言 ——
// 这里只断言「不该上架的名字」，防止前缀匹配把它们误当成活动版的别名。
func TestIsAllowedPricingModelSeedance25HuoDongNoPrefixLeak(t *testing.T) {
	for _, hidden := range []string{
		"seedance2.5-特惠-旧", // 假想的母本旧名
		"seedance2.5-活动版",  // 假想的另一个名字
		"seedance2.5-活动-旧", // 假想的活动版旧名
	} {
		assert.False(t, isAllowedPricingModel(hidden),
			"%q 是另一个名字，不得被 seedance2.5-活动 的放行规则带进广场", hidden)
	}
}
