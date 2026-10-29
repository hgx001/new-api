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

// 默认策略必须与 2026-10-29 上线时的线上售价一致：
// 标准版 ¥0.23/秒三档同价，prime 720P ¥0.20 / 1080P ¥0.30。
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
	assert.Equal(t, 1.5, ratio1080)

	markup, ok := MarkupFor("wan3.0-video-prime", "1080P")
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
	assert.Equal(t, 1.5, ratio)
}

// Get 必须返回深拷贝，否则调用方改返回值会静默改掉全局配置。
func TestGetReturnsDeepCopy(t *testing.T) {
	resetPolicy(t)
	policy := Get()
	policy.Ratios["wan3.0-video-prime"]["1080P"] = 99
	policy.Markup["wan3.0-video"]["*"] = 99

	ratio, _ := GetResolutionRatio("wan3.0-video-prime", "1080P")
	assert.Equal(t, 1.5, ratio)
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
	assert.Equal(t, 1.5, parsed.Ratios["wan3.0-video-prime"]["1080P"])
	assert.Equal(t, 0.10, parsed.Markup["wan3.0-video-prime"]["1080P"])
}
