package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则八（全量兜底 → cp-seedance-2.5flash）的配置层测试。
//
// 锁定三件事：
//  1. 默认关闭 —— 旧生产 option JSON 里没有这个字段，解析后必须是「不生效」；
//  2. 只打开它也要算「策略配全」（否则 UpdateFromJSONString 会把它判成
//     一条都没配而把整份策略关掉，开关亮着却什么都没发生）；
//  3. 它**不依赖** Enabled（规则总开关）—— 应急救援开关不能被「规则全停」连坐。

func TestTotalFallbackDefaultsOff(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-特惠"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 20,
	})

	assert.False(t, IsTotalFallbackEnabled(), "旧 option JSON 没有该字段时必须默认关闭")
	assert.False(t, TotalFallbackEnabledFor("seedance2.5-特惠"))
}

// 只打开应急开关（其它规则全没配）时，整份策略不能被判成「没配全」。
//
// 这是最容易踩的一条：UpdateFromJSONString 在 anyRuleReady 为 false 时
// 会把 Enabled 置 false —— 那条路径下开关亮着、请求照旧打挂掉的源链路。
func TestTotalFallbackAloneKeepsPolicyReady(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{"seedance2.5-特惠"},
		"total_fallback_enabled": true,
	})

	policy := Get()
	require.True(t, policy.Enabled, "只凭规则八也要算「策略配全」，不能被整体关掉")
	assert.True(t, IsAnyRouteEnabled(), "中间件不能因为其它规则没配就早退")
	assert.True(t, IsTotalFallbackEnabled())
}

// 规则八不依赖规则总开关：Enabled=false 时打开它仍然要生效。
//
// 设计理由见 total_fallback.go 包头注释：救火时不该先确认别的开关。
func TestTotalFallbackIgnoresMasterEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                false,
		"source_models":          []string{"seedance2.5-特惠"},
		"total_fallback_enabled": true,
	})

	assert.False(t, Get().Enabled)
	assert.True(t, IsTotalFallbackEnabled(), "总开关关着也不能让应急开关失效")
	assert.True(t, IsAnyRouteEnabled())
}

// 只对配置里的源模型生效，且容忍首尾空格（生产上真出现过带空格的模型名）。
func TestTotalFallbackEnabledForOnlyConfiguredSources(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{" seedance2.5-特惠 ", "seedance2.5-活动"},
		"total_fallback_enabled": true,
	})

	assert.True(t, TotalFallbackEnabledFor("seedance2.5-特惠"))
	assert.True(t, TotalFallbackEnabledFor("seedance2.5-活动"))
	assert.False(t, TotalFallbackEnabledFor("seedance-2-5"), "非源模型不该被全量兜底抓到")
	assert.False(t, TotalFallbackEnabledFor("cp-seedance-2.5flash"), "目标模型自己不能被再次改派")
}

// 目标模型是常量，不存在「配错名字」这种静默故障。
func TestTotalFallbackTargetModelIsCapcutFlash(t *testing.T) {
	assert.Equal(t, "cp-seedance-2.5flash", TotalFallbackTargetModel())
	assert.Equal(t, DefaultTotalFallbackTargetModel, TotalFallbackTargetModel())
}

// WithTotalFallbackEnabled 只改这一个字段，不能顺手冲掉线上的其它调度状态。
func TestWithTotalFallbackEnabledKeepsOtherState(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                    true,
		"source_models":              []string{"seedance2.5-特惠"},
		"target_model":               "seedance-2-5",
		"trigger_seconds":            20,
		"seconds_rule_paused_until":  int64(4102415999),
		"pause_source":               "manual",
		"doubao_link_accepted_tasks": 2,
		"short_youzan_enabled":       true,
	})

	updated := WithTotalFallbackEnabled(true)

	assert.True(t, updated.TotalFallbackEnabled)
	assert.Equal(t, int64(4102415999), updated.SecondsRulePausedUntil, "暂停截止时间不能被冲掉")
	assert.Equal(t, "manual", updated.PauseSource)
	assert.Equal(t, 2, updated.DoubaoLinkAcceptedTasks, "豆包接单计数不能被冲掉")
	assert.Equal(t, "seedance-2-5", updated.TargetModel)
	assert.Equal(t, 20, updated.TriggerSeconds)
	assert.True(t, updated.ShortYouzanEnabled)
}

// 关掉之后必须立刻回到「不参与调度」。
func TestWithTotalFallbackDisabledRestoresDefaultBehavior(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{"seedance2.5-特惠"},
		"total_fallback_enabled": true,
	})
	require.True(t, IsTotalFallbackEnabled())

	applyPolicyValue(t, WithTotalFallbackEnabled(false))

	assert.False(t, IsTotalFallbackEnabled())
	assert.False(t, TotalFallbackEnabledFor("seedance2.5-特惠"))
}
