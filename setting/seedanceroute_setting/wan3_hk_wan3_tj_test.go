package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「hk-wan3 → 客美 tj」手动改派开关的测试（2026-11-22）。
//
//  1. 没配 wan3_hk_wan3_tj_enabled = 默认关闭（升级当天线上流量逐字节不变）；
//  2. WithWan3HkWan3TjEnabled 是纯函数：改返回值不改全局状态；
//  3. option JSON → 内存状态的往返不丢这个字段；
//  4. 它与 Policy.Enabled（促销族规则总开关）互不连坐。
func TestWan3HkWan3TjDefaultsOff(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
		"target_model":  "seedance-2-5",
	})

	assert.False(t, IsWan3HkWan3TjEnabled(), "没配 wan3_hk_wan3_tj_enabled 时应默认关闭")
}

func TestWithWan3HkWan3TjEnabledIsPure(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
		"target_model":  "seedance-2-5",
	})
	before := Get()

	updated := WithWan3HkWan3TjEnabled(true)

	assert.True(t, updated.Wan3HkWan3TjEnabled, "返回的策略副本应已打开开关")
	assert.Positive(t, updated.UpdatedAt, "改开关应刷新 UpdatedAt")
	assert.False(t, IsWan3HkWan3TjEnabled(), "With* 是纯函数，不该改动全局状态")
	assert.Equal(t, before.SourceModels, updated.SourceModels, "其它字段必须原样保留")
	assert.Equal(t, before.TargetModel, updated.TargetModel, "其它字段必须原样保留")
}

func TestWan3HkWan3TjOptionRoundTrip(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                 true,
		"source_models":           []string{"seedance2.5-特惠"},
		"target_model":            "seedance-2-5",
		"wan3_hk_wan3_tj_enabled": true,
	})

	require.True(t, IsWan3HkWan3TjEnabled(), "option JSON 里的 true 必须落到内存状态")

	encoded := MarshalJSONString()
	assert.Contains(t, encoded, "wan3_hk_wan3_tj_enabled", "写回 option 时字段不能丢")
}

func TestWan3HkWan3TjIndependentOfEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                 false,
		"wan3_hk_wan3_tj_enabled": true,
	})

	assert.True(t, IsWan3HkWan3TjEnabled(),
		"促销族规则总开关关闭不该让 hk-wan3 换货开关失灵")
}
