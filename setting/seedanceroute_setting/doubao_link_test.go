package seedanceroute_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
)

// 豆包链路开关：运营用一个开关表达「豆包这条链路通不通」。
//
//   - 关闭：规则一对所有源模型停用（特惠/活动留在常驻链路）+ 故障转移强制生效
//     （seedance-2-5 全部改派班车），并在下一个北京时间凌晨 2 点（豆包额度
//     重置点）自动恢复为开启。
//   - 开启：恢复规则一（超时长改派豆包）；额度探针的 auto 标记/自动暂停保留，
//     不被人工开关推翻；旧式人工标记与人工暂停由开关接管并清除。

func stubNowUnix(t *testing.T, ts int64) {
	t.Helper()
	previous := nowUnix
	nowUnix = func() int64 { return ts }
	t.Cleanup(func() { nowUnix = previous })
}

// linkBasePolicy 构造「规则一 + 比例 + 首尾帧 + 故障转移」全部配置齐全的策略底座。
func linkBasePolicy(t *testing.T) {
	t.Helper()
	applyPolicy(t, map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":          "seedance-2-5",
		"trigger_seconds":       20,
		"ratio_rule_enabled":    true,
		"ratio_target_model":    "wan3.0-720p-特惠-内链",
		"trigger_ratio":         "9:16",
		"frames_rule_enabled":   true,
		"frames_target_model":   "wan3.0-720p-活动-内链",
		"failover_enabled":      true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车",
	})
}

// 关闭开关：截止时间必须落在下一个北京凌晨 2 点，且故障转移开关被强制打开。
func TestWithDoubaoLinkEnabledOffSetsUntilToQuotaReset(t *testing.T) {
	linkBasePolicy(t)
	// 周三 23:00 北京时间 → 下一个凌晨 2 点是次日 02:00。
	now := time.Date(2026, 11, 11, 23, 0, 0, 0, beijingZone).Unix()
	stubNowUnix(t, now)

	out := WithDoubaoLinkEnabled(false)

	assert.Equal(t, NextBeijingQuotaReset(now), out.DoubaoLinkOffUntil)
	assert.True(t, out.FailoverEnabled, "关闭豆包时故障转移必须开着，否则 seedance-2-5 改派不了班车")
	assert.NotZero(t, out.UpdatedAt)
}

// 关闭/开启都必须清掉旧式人工标记与人工暂停：开关接管后它们不再是事实源，
// 否则残留的远期人工标记会在凌晨 2 点把链路重新按死（自动恢复失效）。
func TestWithDoubaoLinkEnabledClearsLegacyManualState(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{true: "on", false: "off"}[enabled], func(t *testing.T) {
			linkBasePolicy(t)
			applyPolicy(t, map[string]interface{}{
				"failover_unavailable_until":  time.Now().Unix() + 86400,
				"failover_unavailable_reason": "手动切换：豆包链路关闭，全部改派班车",
				"failover_unavailable_source": PauseSourceManual,
				"seconds_rule_paused_until":   time.Now().Unix() + 86400,
				"pause_source":                PauseSourceManual,
				"pause_reason":                "手动关闭 30 天",
			})

			out := WithDoubaoLinkEnabled(enabled)

			assert.Zero(t, out.FailoverUnavailableUntil, "旧式人工不可用标记必须清除")
			assert.Empty(t, out.FailoverUnavailableSource)
			assert.Empty(t, out.FailoverUnavailableReason)
			assert.Zero(t, out.SecondsRulePausedUntil, "人工暂停必须清除")
			assert.Empty(t, out.PauseSource)
		})
	}
}

// 开启开关不能推翻额度探针的 auto 判断：探针说额度用完就是用完，
// 人工开关只代表「我意图让豆包可用」，不能覆盖真实额度状态。
func TestWithDoubaoLinkEnabledOnPreservesAutoProbeState(t *testing.T) {
	linkBasePolicy(t)
	until := time.Now().Unix() + 3600
	applyPolicy(t, map[string]interface{}{
		"failover_unavailable_until":  until,
		"failover_unavailable_reason": ReasonQuotaExhausted,
		"failover_unavailable_source": PauseSourceAuto,
		"seconds_rule_paused_until":   until,
		"pause_source":                PauseSourceAuto,
		"pause_reason":                ReasonQuotaExhausted,
	})

	out := WithDoubaoLinkEnabled(true)

	assert.Equal(t, until, out.FailoverUnavailableUntil, "额度探针的降级标记必须保留")
	assert.Equal(t, PauseSourceAuto, out.FailoverUnavailableSource)
	assert.Equal(t, until, out.SecondsRulePausedUntil, "额度探针的规则一暂停必须保留")
	assert.Equal(t, PauseSourceAuto, out.PauseSource)
}

// 关闭状态下的分流语义：规则一全停（IsEnabled/按源都为 false），比例与首尾帧
// 规则不受影响（它们不经过豆包），故障转移保持就绪。
func TestDoubaoLinkOffGatesSecondsRuleOnly(t *testing.T) {
	linkBasePolicy(t)
	stubNowUnix(t, time.Date(2026, 11, 11, 23, 30, 0, 0, beijingZone).Unix())

	applyPolicyValue(t, WithDoubaoLinkEnabled(false))

	require.True(t, IsDoubaoLinkOff(), "开关应处于关闭状态")
	assert.False(t, IsEnabled(), "关闭后规则一必须停用")
	assert.False(t, IsSecondsRuleEnabledFor("seedance2.5-特惠"), "特惠超时长必须留在常驻有赞链路")
	assert.False(t, IsSecondsRuleEnabledFor("seedance2.5-活动"), "活动超时长必须留在常驻心笔链路")
	assert.True(t, IsRatioRouteEnabled(), "比例规则不经过豆包，不应被开关影响")
	assert.True(t, IsFramesRouteEnabled(), "首尾帧规则不经过豆包，不应被开关影响")
	assert.True(t, IsFailoverRouteEnabled(), "关闭豆包时故障转移必须保持就绪")
}

// 自动恢复：until 到点后开关自动回到开启，规则一立即重新生效（凌晨 2 点豆包额度恢复）。
func TestDoubaoLinkAutoRecoversAtQuotaReset(t *testing.T) {
	linkBasePolicy(t)
	now := time.Date(2026, 11, 11, 23, 50, 0, 0, beijingZone).Unix()
	stubNowUnix(t, now)
	reset := NextBeijingQuotaReset(now)
	applyPolicyValue(t, WithDoubaoLinkEnabled(false))

	stubNowUnix(t, reset-1)
	require.True(t, IsDoubaoLinkOff(), "到点前应保持关闭")
	require.False(t, IsEnabled())

	stubNowUnix(t, reset)
	require.False(t, IsDoubaoLinkOff(), "到点后应自动恢复为开启")
	assert.True(t, IsEnabled(), "恢复后规则一必须立即重新生效")
	assert.True(t, IsSecondsRuleEnabledFor("seedance2.5-特惠"))
}

// applyPolicyValue 把一份策略副本应用为当前策略（依赖前面 applyPolicy 装的恢复钩子）。
func applyPolicyValue(t *testing.T, p Policy) {
	t.Helper()
	data, err := common.Marshal(p)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &payload))
	applyPolicy(t, payload)
}
