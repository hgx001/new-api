package seedanceroute_setting

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
)

// 豆包链路的「接单上限」（2026-11-13 老板定）。
//
// 语义：豆包链路上游的额度是运营自己掌握的一小批（不是按天重置的那种），
// 运营要能说「这批额度只接 3 单，接满就关掉，我手动再打开时又来 3 单」。
//
// 三条硬约束（本文件逐条锁）：
//
//  1. **上限 0 = 不限**：旧的生产 option JSON 里没有这个字段，解析后是 0，
//     行为必须与改造前逐字节一致 —— 不能因为加了字段就静默改变线上接单。
//  2. **计数只在「人工重新打开」时归零**：自动恢复（凌晨 2 点）**不许**归零，
//     否则额度还没重置就把额度又发出去，等于白烧。
//  3. **限额关闭不许自动恢复**：手动关闭有「凌晨 2 点恢复」的语义，
//     限额关闭没有 —— 额度是运营手动给的，必须由人再打开一次。

// applyLimitPolicy 覆盖当前策略并注册恢复钩子。
func applyLimitPolicy(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	previous := Get()
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = UpdateFromJSONString(string(restored))
	})
}

// limitBasePayload 构造「规则一 + 故障转移」配全的策略底座。
func limitBasePayload() map[string]interface{} {
	return map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":          "seedance-2-5",
		"trigger_seconds":       20,
		"failover_enabled":      true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车",
	}
}

// 上限 0 = 不限：计数照常记，但链路永不因限额关闭。
func TestDoubaoLinkLimitZeroMeansUnlimited(t *testing.T) {
	applyLimitPolicy(t, limitBasePayload())

	assert.Equal(t, 0, DoubaoLinkMaxTasks())
	assert.Equal(t, 0, DoubaoLinkAcceptedTasks())
	assert.True(t, DoubaoLinkAccepting(), "没配上限时必须照常接单")
	assert.False(t, DoubaoLinkLimitReached())
	assert.False(t, IsDoubaoLinkOff(), "没配上限时链路不许被限额关掉")
}

// 上限 3：接 3 单后链路关闭（豆包不再接单，请求落备胎班车）。
func TestDoubaoLinkLimitClosesAfterNAccepted(t *testing.T) {
	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 3
	applyLimitPolicy(t, payload)

	// 前两单：照常接。
	for i := 1; i <= 2; i++ {
		accepted := WithDoubaoLinkAcceptedTasks(2)
		require.NoError(t, persistLimitPolicy(accepted))
		assert.Equal(t, 2, DoubaoLinkAcceptedTasks())
		assert.True(t, DoubaoLinkAccepting(), "第 %d 单后仍应接单", i)
		assert.False(t, IsDoubaoLinkOff())
	}

	// 第三单：接完就关。
	accepted := WithDoubaoLinkAcceptedTasks(3)
	require.NoError(t, persistLimitPolicy(accepted))

	assert.Equal(t, 3, DoubaoLinkAcceptedTasks())
	assert.True(t, DoubaoLinkLimitReached())
	assert.False(t, DoubaoLinkAccepting())
	assert.True(t, IsDoubaoLinkOff(), "接满上限后豆包链路必须关闭")
	assert.Equal(t, 0, DoubaoLinkRemaining(), "接满后剩余可接单数必须是 0")
}

// 限额关闭**不许**带「凌晨 2 点自动恢复」：额度是运营手动给的，
// 必须由人再打开一次 —— 到点自愈会把没给的额度又发出去。
func TestDoubaoLinkLimitCloseHasNoAutoRecover(t *testing.T) {
	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 1
	applyLimitPolicy(t, payload)

	accepted := WithDoubaoLinkAcceptedTasks(1)
	require.NoError(t, persistLimitPolicy(accepted))

	assert.True(t, IsDoubaoLinkOff())
	assert.Zero(t, Get().DoubaoLinkOffUntil, "限额关闭不许写自动恢复截止时间")
	assert.Equal(t, DoubaoLinkOffReasonLimit, DoubaoLinkOffReason())
}

// 人工重新打开：计数归零、上限保留（下一轮又能接满 N 单）。
func TestDoubaoLinkManualReopenResetsCounter(t *testing.T) {
	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 3
	applyLimitPolicy(t, payload)

	accepted := WithDoubaoLinkAcceptedTasks(3)
	require.NoError(t, persistLimitPolicy(accepted))
	require.True(t, IsDoubaoLinkOff())

	reopened := WithDoubaoLinkEnabled(true)
	require.NoError(t, persistLimitPolicy(reopened))

	assert.Equal(t, 0, DoubaoLinkAcceptedTasks(), "重新打开必须把计数归零")
	assert.Equal(t, 3, DoubaoLinkMaxTasks(), "上限是配置项，重新打开不许清掉")
	assert.False(t, DoubaoLinkLimitReached())
	assert.False(t, IsDoubaoLinkOff())
	assert.True(t, DoubaoLinkAccepting())
	assert.Equal(t, 3, DoubaoLinkRemaining(), "重新打开后又能接满 3 单")
}

// 剩余可接单数：上限减已接。无限限时返回 0（表示「不限制」）。
func TestDoubaoLinkRemaining(t *testing.T) {
	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 5
	applyLimitPolicy(t, payload)

	assert.Equal(t, 5, DoubaoLinkRemaining())

	accepted := WithDoubaoLinkAcceptedTasks(2)
	require.NoError(t, persistLimitPolicy(accepted))
	assert.Equal(t, 3, DoubaoLinkRemaining())

	// 没配上限 = 不限，剩余数没有意义，用 0 表示。
	applyLimitPolicy(t, limitBasePayload())
	assert.Equal(t, 0, DoubaoLinkRemaining())
}

// 设置上限：正数 = 限额，0 = 不限。
func TestWithDoubaoLinkMaxTasks(t *testing.T) {
	applyLimitPolicy(t, limitBasePayload())

	out := WithDoubaoLinkMaxTasks(5)
	assert.Equal(t, 5, out.DoubaoLinkMaxTasks)

	// 从「限额已满」改成「不限」必须同时清掉满员状态与计数：
	// 否则「不再限额」却仍显示「已关闭」，运营会以为额度还锁着。
	out = WithDoubaoLinkMaxTasks(0)
	assert.Equal(t, 0, out.DoubaoLinkMaxTasks)
	assert.False(t, doubaoLinkLimitReached(out), "改成不限后不该还判为已满")
	assert.Zero(t, out.DoubaoLinkAcceptedTasks, "改成不限后计数没有意义，应清零")
}

// 手动关闭（有自动恢复时间）与限额关闭是两种「关」，原因必须能区分。
func TestDoubaoLinkOffReasonDistinguishesManualAndLimit(t *testing.T) {
	applyLimitPolicy(t, limitBasePayload())
	assert.Equal(t, "", DoubaoLinkOffReason(), "开着时没有关闭原因")

	manual := WithDoubaoLinkEnabled(false)
	require.NoError(t, persistLimitPolicy(manual))
	assert.Equal(t, DoubaoLinkOffReasonManual, DoubaoLinkOffReason())
	assert.True(t, Get().DoubaoLinkOffUntil > 0, "手动关闭带自动恢复时间")
}

// 人工关闭期间不许记限额满员：链路已经关了，计多少都无所谓，
// 而且会把「已关闭」的原因写成限额，掩盖真实原因。
func TestDoubaoLinkManualOffKeepsManualReason(t *testing.T) {
	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 1
	applyLimitPolicy(t, payload)

	manual := WithDoubaoLinkEnabled(false)
	require.NoError(t, persistLimitPolicy(manual))

	accepted := WithDoubaoLinkAcceptedTasks(1)
	require.NoError(t, persistLimitPolicy(accepted))

	assert.Equal(t, DoubaoLinkOffReasonManual, DoubaoLinkOffReason(),
		"人工关闭期间的原因不许被限额覆盖")
	assert.True(t, Get().DoubaoLinkOffUntil > 0)
}

// 已受理单数不得为负：负数会让「剩余」算成比上限还多。
func TestDoubaoLinkAcceptedTasksNeverNegative(t *testing.T) {
	applyLimitPolicy(t, limitBasePayload())

	out := WithDoubaoLinkAcceptedTasks(-5)
	assert.Equal(t, 0, out.DoubaoLinkAcceptedTasks)
}

// 自动恢复（凌晨 2 点）不许把计数归零：额度还没重置就把额度又发出去。
func TestDoubaoLinkAutoRecoverKeepsCounter(t *testing.T) {
	now := time.Date(2026, 11, 13, 23, 30, 0, 0, beijingZone).Unix()
	stubNowUnix(t, now)

	payload := limitBasePayload()
	payload["doubao_link_max_tasks"] = 3
	payload["doubao_link_accepted_tasks"] = 1
	applyLimitPolicy(t, payload)

	// 手动关闭 → 到点自动恢复。
	manual := WithDoubaoLinkEnabled(false)
	require.NoError(t, persistLimitPolicy(manual))

	stubNowUnix(t, NextBeijingQuotaReset(now))
	assert.False(t, IsDoubaoLinkOff(), "到点后应自动恢复")
	assert.Equal(t, 1, DoubaoLinkAcceptedTasks(), "自动恢复不许把计数归零")
	assert.Equal(t, 2, DoubaoLinkRemaining(), "还剩 2 单额度")
}

// persistLimitPolicy 把一份策略写回内存状态（等价于落库后的分发结果）。
func persistLimitPolicy(p Policy) error {
	data, err := common.Marshal(p)
	if err != nil {
		return err
	}
	return UpdateFromJSONString(string(data))
}
