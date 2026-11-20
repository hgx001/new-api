package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「万相官网 → 客美 tj」手动改派开关的测试（2026-11-19）。
//
// 本文件锁四件事：
//  1. 没配 wan3_guanwang_tj_enabled = 默认关闭（升级当天线上流量逐字节不变）；
//  2. WithWan3GuanwangTjEnabled 是纯函数：改返回值不改全局状态；
//  3. option JSON → 内存状态的往返不丢这个字段；
//  4. 它与 Policy.Enabled（促销族规则总开关）**互不连坐** —— 这是本开关
//     最容易被人「顺手统一」掉的性质：用途完全不同的两个开关连坐，
//     会制造「按钮亮着、实际没生效」的静默故障。
func TestWan3GuanwangTjDefaultsOff(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
		"target_model":  "seedance-2-5",
	})

	assert.False(t, IsWan3GuanwangTjEnabled(), "没配 wan3_guanwang_tj_enabled 时应默认关闭")
}

func TestWithWan3GuanwangTjEnabledIsPure(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
		"target_model":  "seedance-2-5",
	})
	before := Get()

	updated := WithWan3GuanwangTjEnabled(true)

	assert.True(t, updated.Wan3GuanwangTJEnabled, "返回的策略副本应已打开开关")
	assert.Positive(t, updated.UpdatedAt, "改开关应刷新 UpdatedAt")
	assert.False(t, IsWan3GuanwangTjEnabled(), "With* 是纯函数，不该改动全局状态")
	assert.Equal(t, before.SourceModels, updated.SourceModels, "其它字段必须原样保留")
	assert.Equal(t, before.TargetModel, updated.TargetModel, "其它字段必须原样保留")
}

func TestWan3GuanwangTjOptionRoundTrip(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                  true,
		"source_models":            []string{"seedance2.5-特惠"},
		"target_model":             "seedance-2-5",
		"wan3_guanwang_tj_enabled": true,
	})

	require.True(t, IsWan3GuanwangTjEnabled(), "option JSON 里的 true 必须落到内存状态")

	encoded := MarshalJSONString()
	assert.Contains(t, encoded, "wan3_guanwang_tj_enabled", "写回 option 时字段不能丢")
}

// 总开关关掉时本开关仍然有效：规则停用 ≠ 万相不能换货。
func TestWan3GuanwangTjIndependentOfEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                  false,
		"wan3_guanwang_tj_enabled": true,
	})

	assert.True(t, IsWan3GuanwangTjEnabled(),
		"促销族规则总开关关闭不该让万相换货开关失灵")
}
