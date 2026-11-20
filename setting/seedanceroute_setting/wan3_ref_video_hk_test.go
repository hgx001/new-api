package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「带参考视频的万相请求 → hk-wan3」开关的测试（2026-11-20）。
//
// 本文件锁四件事：
//  1. **缺省是开启**（nil = 开）：这是链路纠正而不是价格试验，
//     旧的生产 option 里没有这个字段也必须生效；
//  2. 显式 false 能真的关掉（运营按了关就得生效，不能被「nil 也算开」吞掉）；
//  3. With* 是纯函数，且开关是**指针深拷贝**（浅拷贝会让副本改到全局）；
//  4. 与促销族总开关 Enabled 无关。
func TestWan3RefVideoHkDefaultsOn(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
		"target_model":  "seedance-2-5",
	})

	assert.True(t, IsWan3RefVideoHkEnabled(), "没配 wan3_ref_video_hk_enabled 时应默认开启")
}

func TestWan3RefVideoHkExplicitOff(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                   true,
		"source_models":             []string{"seedance2.5-特惠"},
		"target_model":              "seedance-2-5",
		"wan3_ref_video_hk_enabled": false,
	})

	assert.False(t, IsWan3RefVideoHkEnabled(), "显式 false 必须真的关掉")
}

func TestWithWan3RefVideoHkEnabledIsPureAndDeepCopied(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                   true,
		"source_models":             []string{"seedance2.5-特惠"},
		"target_model":              "seedance-2-5",
		"wan3_ref_video_hk_enabled": true,
	})

	updated := WithWan3RefVideoHkEnabled(false)
	require.NotNil(t, updated.Wan3RefVideoHkEnabled)
	assert.False(t, *updated.Wan3RefVideoHkEnabled)
	assert.True(t, IsWan3RefVideoHkEnabled(), "With* 是纯函数，不改全局状态")

	// 深拷贝：改副本的指针目标不能影响全局。
	*updated.Wan3RefVideoHkEnabled = true
	assert.True(t, IsWan3RefVideoHkEnabled(), "全局状态不应被子孙指针影响")
}

func TestWan3RefVideoHkIndependentOfEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                   false,
		"wan3_ref_video_hk_enabled": true,
	})

	assert.True(t, IsWan3RefVideoHkEnabled(),
		"促销族规则总开关关闭不该让万相参考视频路由失灵")
}

func TestWan3FamilySourceModelsListsThreeChains(t *testing.T) {
	assert.Equal(t, []string{
		"wan3.0-video-官网",
		"wan3.0-video-tj",
		"wan3.0-video-prime-tj",
	}, Wan3FamilySourceModels)
	assert.Equal(t, "hk-wan3", Wan3RefVideoHkTargetModel)
}
