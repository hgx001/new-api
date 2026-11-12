package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则六（短时长分流）的配置层测试。
//
// 规则语义（2026-11-13 老板定）：源模型 + 请求时长 ≤ 阈值（默认 10 秒）
// → 改派到有赞标准版 wan3.0-video。
//
// 与规则一的**方向相反**：规则一是「太长 → 换链路」，规则六是「太短 → 换链路」。
// 两者时长区间互不重叠（≤10 与 >20），所以判定顺序不影响结果 ——
// 但规则六必须排在规则二（比例）**之前**，老板口径是「短任务优先」。
//
// ⚠️ 这个文件只测配置层（字段解析 / 就绪判定 / 按源模型覆盖）。
// 真实改派行为在 middleware/seedance_route_short_test.go 里测。

// restorePolicy 在用例结束时把全局策略恢复原状。
func restorePolicy(t *testing.T) {
	t.Helper()
	previous := Get()
	t.Cleanup(func() {
		encoded, _ := common.Marshal(previous)
		_ = UpdateFromJSONString(string(encoded))
	})
}

// writePolicy 把一段 JSON 写进全局策略（走真实的反序列化 + 校验链）。
func writePolicy(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, UpdateFromJSONString(string(encoded)))
}

// 没配 short_target_model 时规则六必须完全不生效。
//
// 这是硬要求：旧的生产 option JSON 里没有这个字段，升级后行为必须与之前
// 逐字节一致 —— 否则「今天部署完，短任务突然全换了链路」且没有任何报错。
func TestShortRuleDisabledByDefault(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_model":    "seedance2.5-特惠",
		"target_model":    "seedance-2-5",
		"trigger_seconds": 20,
	})

	assert.Empty(t, Get().ShortTargetModel)
	assert.False(t, IsShortRouteEnabledFor("seedance2.5-特惠"),
		"没配目标模型时规则六不能生效")
}

// 阈值语义是**小于等于**：10 秒命中、11 秒不命中。
//
// 老板原话「小于等于10秒以下」，所以边界是闭区间 —— 10 秒必须走标准版。
func TestShortRuleBoundaryIsInclusive(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"target_model":       "seedance-2-5",
		"trigger_seconds":    20,
		"short_target_model": "wan3.0-video",
	})

	policy := Get()
	assert.True(t, policy.WithinShortTrigger(1))
	assert.True(t, policy.WithinShortTrigger(5))
	assert.True(t, policy.WithinShortTrigger(10), "10 秒是闭区间边界，必须命中")
	assert.False(t, policy.WithinShortTrigger(11), "11 秒已超阈值")
	assert.False(t, policy.WithinShortTrigger(0), "0 秒是非法时长，不能当命中")
	assert.False(t, policy.WithinShortTrigger(-3), "负数时长不能当命中")
}

// 没配 short_trigger_seconds 时按默认 10 秒 ——
// 「配了目标就生效」比「少配一个字段就静默不生效」安全得多。
func TestShortRuleDefaultsToTenSeconds(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"target_model":       "seedance-2-5",
		"trigger_seconds":    20,
		"short_target_model": "wan3.0-video",
	})

	policy := Get()
	assert.Equal(t, DefaultShortTriggerSeconds, policy.ShortTriggerSeconds)
	assert.Equal(t, 10, DefaultShortTriggerSeconds)
	assert.True(t, policy.WithinShortTrigger(10))
	assert.False(t, policy.WithinShortTrigger(11))
}

// 阈值可调：配 6 秒后 6 秒命中、7 秒不命中。
func TestShortRuleHonoursCustomTrigger(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":               true,
		"source_model":          "seedance2.5-特惠",
		"target_model":          "seedance-2-5",
		"trigger_seconds":       20,
		"short_target_model":    "wan3.0-video",
		"short_trigger_seconds": 6,
	})

	policy := Get()
	assert.Equal(t, 6, policy.ShortTriggerSeconds)
	assert.True(t, policy.WithinShortTrigger(6))
	assert.False(t, policy.WithinShortTrigger(7))
}

// 只配规则六（其余规则全空）时，整份策略必须算「配全」——
// 否则 UpdateFromJSONString 会把 Enabled 静默降级成 false，规则六永不生效。
func TestShortRuleAloneKeepsPolicyEnabled(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"short_target_model": "wan3.0-video",
	})

	assert.True(t, Get().Enabled, "只配规则六时总开关不能被降级")
	assert.True(t, IsShortRouteEnabledFor("seedance2.5-特惠"))
	assert.True(t, IsAnyRouteEnabled(), "中间件早退不能把规则六漏掉")
}

// 按源模型覆盖目标：母本走 wan3.0-video，活动版可以指向自己的标准版链路。
func TestShortRulePerSourceTargetOverride(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"short_target_model": "wan3.0-video",
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"short_target_model": "wan3.0-720p-特惠",
			},
		},
	})

	policy := Get()
	assert.Equal(t, "wan3.0-video", policy.ShortTargetModelFor("seedance2.5-特惠"))
	assert.Equal(t, "wan3.0-720p-特惠", policy.ShortTargetModelFor("seedance2.5-活动"),
		"专属覆盖必须优先生效")
}

// 按源模型关闭规则六：活动版留在常驻链路，母本照旧分流。
func TestShortRulePerSourceDisable(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"short_target_model": "wan3.0-video",
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"short_rule_enabled": false,
			},
		},
	})

	assert.True(t, IsShortRouteEnabledFor("seedance2.5-特惠"), "没配覆盖的源模型默认开启")
	assert.False(t, IsShortRouteEnabledFor("seedance2.5-活动"), "显式关掉的不生效")
}

// 开关是三态指针：显式 true 能把别人关掉的开关改回来。
//
// 用 bool 而不是指针就区分不了「没配」和「配了 false」——
// 升级当天会把全站短任务分流静默停掉。
func TestShortRuleExplicitTrueReenables(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"short_target_model": "wan3.0-video",
		"source_overrides": map[string]interface{}{
			"seedance2.5-特惠": map[string]interface{}{
				"short_rule_enabled": true,
			},
		},
	})

	assert.True(t, IsShortRouteEnabledFor("seedance2.5-特惠"))
}

// 非源模型的模型名不能拿到规则六的目标 —— 调用方会拿它去改派请求。
func TestShortRuleTargetIgnoredForUnknownSource(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"short_target_model": "wan3.0-video",
	})

	policy := Get()
	assert.Equal(t, "wan3.0-video", policy.ShortTargetModelFor("seedance2.5-特惠"))
	assert.False(t, policy.IsSourceModel("wan3.0-video"),
		"目标模型不能反过来被当成源模型")
}

// 深拷贝必须复制 short_rule_enabled 指针，否则 Get() 的副本一改就污染全局。
func TestShortRuleCloneIsolatesPointer(t *testing.T) {
	restorePolicy(t)
	writePolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_model":       "seedance2.5-特惠",
		"short_target_model": "wan3.0-video",
		"source_overrides": map[string]interface{}{
			"seedance2.5-特惠": map[string]interface{}{
				"short_rule_enabled": true,
			},
		},
	})

	copied := Get()
	disabled := false
	override := copied.SourceOverrides["seedance2.5-特惠"]
	override.ShortRuleEnabled = &disabled
	copied.SourceOverrides["seedance2.5-特惠"] = override

	assert.True(t, IsShortRouteEnabledFor("seedance2.5-特惠"),
		"改副本不能影响全局策略")
}
