package youzanwan3_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetPolicy 把全局状态恢复到默认值，避免用例之间互相污染。
func resetPolicy(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		mu.Lock()
		current = defaultPolicy()
		mu.Unlock()
	})
	mu.Lock()
	current = defaultPolicy()
	mu.Unlock()
}

// 默认策略必须与 2026-10-29 第二轮之后的线上售价一致：
// 标准版 ¥0.23/秒三档同价，prime 720P ¥0.25 / 1080P ¥0.30（两档加价都是 ¥0.10）。
func TestDefaultPolicyMatchesProductionPrices(t *testing.T) {
	resetPolicy(t)

	for _, resolution := range []string{"480P", "720P", "1080P"} {
		ratio, ok := GetResolutionRatio("wan3.0-video", resolution)
		require.True(t, ok, resolution)
		assert.Equal(t, 1.0, ratio, resolution)
	}
	ratio720, ok := GetResolutionRatio("wan3.0-video-prime", "720P")
	require.True(t, ok)
	assert.Equal(t, 1.0, ratio720)
	ratio1080, ok := GetResolutionRatio("wan3.0-video-prime", "1080P")
	require.True(t, ok)
	assert.Equal(t, 1.2, ratio1080)

	markup, ok := MarkupFor("wan3.0-video-prime", "720P")
	require.True(t, ok)
	assert.Equal(t, 0.10, markup)
	markup, ok = MarkupFor("wan3.0-video-prime", "1080P")
	require.True(t, ok)
	assert.Equal(t, 0.10, markup)
	// 未列出的档位回退到 "*"
	markup, ok = MarkupFor("wan3.0-video", "1080P")
	require.True(t, ok)
	assert.Equal(t, 0.05, markup)
}

func TestGetResolutionRatioUnknownModel(t *testing.T) {
	resetPolicy(t)
	_, ok := GetResolutionRatio("not-a-model", "720P")
	assert.False(t, ok)
	_, ok = MarkupFor("not-a-model", "720P")
	assert.False(t, ok)
}

// 管理员只改加价规则时，已经同步出来的倍率不能被清空 ——
// 清空会让 EstimateBilling 回退到代码常量，等于把上游驱动的档差丢了。
func TestUpdateFromJSONStringKeepsRatiosWhenOmitted(t *testing.T) {
	resetPolicy(t)
	SetRatios(map[string]map[string]float64{
		"wan3.0-video-prime": {"720P": 1.0, "1080P": 2.0},
	}, 1700000000)

	require.NoError(t, UpdateFromJSONString(`{"markup":{"wan3.0-video-prime":{"720P":0.05,"1080P":0.20}},"auto_apply":true}`))

	ratio, ok := GetResolutionRatio("wan3.0-video-prime", "1080P")
	require.True(t, ok)
	assert.Equal(t, 2.0, ratio, "ratios 应保留")
	assert.Equal(t, int64(1700000000), Get().UpdatedAt)

	markup, ok := MarkupFor("wan3.0-video-prime", "1080P")
	require.True(t, ok)
	assert.Equal(t, 0.20, markup, "markup 应更新")
}

func TestUpdateFromJSONStringRejectsMalformed(t *testing.T) {
	resetPolicy(t)
	require.Error(t, UpdateFromJSONString(`{not json`))
	// 解析失败不能破坏当前状态
	ratio, ok := GetResolutionRatio("wan3.0-video-prime", "1080P")
	require.True(t, ok)
	assert.Equal(t, 1.2, ratio)
}

// Get 必须返回深拷贝，否则调用方改返回值会静默改掉全局配置。
func TestGetReturnsDeepCopy(t *testing.T) {
	resetPolicy(t)
	policy := Get()
	policy.Ratios["wan3.0-video-prime"]["1080P"] = 99
	policy.Markup["wan3.0-video"]["*"] = 99

	ratio, _ := GetResolutionRatio("wan3.0-video-prime", "1080P")
	assert.Equal(t, 1.2, ratio)
	markup, _ := MarkupFor("wan3.0-video", "480P")
	assert.Equal(t, 0.05, markup)
}

// 序列化结果要能被自己重新解析（写回 option 再读出来不丢字段）。
func TestMarshalRoundTrip(t *testing.T) {
	resetPolicy(t)
	raw := MarshalJSONString()

	var parsed Policy
	require.NoError(t, common.Unmarshal([]byte(raw), &parsed))
	assert.Equal(t, 0.30, parsed.MaxChangeRatio)
	assert.True(t, parsed.AutoApply)
	assert.Equal(t, 1.2, parsed.Ratios["wan3.0-video-prime"]["1080P"])
	assert.Equal(t, 0.10, parsed.Markup["wan3.0-video-prime"]["1080P"])
	// FixedPrices 也不能丢 —— 丢了等于钉死价失效，自动调价会改写对外报价。
	assert.Equal(t, 0.20, parsed.FixedPrices["wan-3.0-yz"]["720P"])
	assert.Equal(t, 0.25, parsed.FixedPrices["wan-3.0-yz"]["1080P"])
}

// wan-3.0-yz 的对外报价是**承诺价**（720P ¥0.20/秒、1080P ¥0.25/秒），
// 必须钉死、不随上游成本浮动。
//
// 这条守卫锁两件事：
//  1. 钉死价确实能读出来（GetFixedPrice）；
//  2. 钉死价模型的**倍率**是 0.25/0.20 = 1.25 —— 基准档取最低价那档，
//     否则高档位会少收钱（收 1080P 的钱、按 720P 的价结算）。
func TestFixedPriceForYouzanWan30(t *testing.T) {
	resetPolicy(t)

	price720, ok := GetFixedPrice("wan-3.0-yz", "720P")
	require.True(t, ok, "wan-3.0-yz 必须有钉死价")
	assert.Equal(t, 0.20, price720)

	price1080, ok := GetFixedPrice("wan-3.0-yz", "1080P")
	require.True(t, ok)
	assert.Equal(t, 0.25, price1080)

	// 未钉死的模型读不到（走上游成本 + 加价）。
	_, ok = GetFixedPrice("wan3.0-video", "720P")
	assert.False(t, ok, "只有钉死价表里的模型才有钉死价")

	// 倍率：基准档 720P = 1.0，1080P = 0.25/0.20 = 1.25。
	ratio720, ok := GetResolutionRatio("wan-3.0-yz", "720P")
	require.True(t, ok)
	assert.Equal(t, 1.0, ratio720)
	ratio1080, ok := GetResolutionRatio("wan-3.0-yz", "1080P")
	require.True(t, ok)
	assert.Equal(t, 1.25, ratio1080)

	assert.Contains(t, FixedPriceModels(), "wan-3.0-yz")
	assert.True(t, HasFixedPrices())
}
