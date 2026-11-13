package service

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 豆包链路接单计数（2026-11-13 老板定）：
//
//	「这批额度只接 3 单，接满就把豆包链路关掉，我手动再打开时又来 3 单。」
//
// 这一层锁的是「计数 → 落库」的接线：
//	setting 包已经把「该不该接、还剩几单」做成纯函数，
//	这里只负责在**受理成功**后推进计数并写进 options 表。
//
// 为什么单独测：计数打在哪儿决定了额度准不准 ——
// 打在请求进来时会把失败请求也算进去（白扣额度），
// 打在受理成功之后才是「上游真接下的单」。

// limitPolicyJSON 是「规则一 + 故障转移 + 接单上限」配全的策略。
func limitPolicyJSON(maxTasks int, accepted int) string {
	return `{"enabled":true,"source_model":"seedance2.5-特惠",` +
		`"target_model":"seedance-2-5","trigger_seconds":30,` +
		`"failover_enabled":true,"failover_source_model":"seedance-2-5",` +
		`"failover_target_model":"wan3.0-班车",` +
		`"doubao_link_max_tasks":` + itoaTest(int64(maxTasks)) + `,` +
		`"doubao_link_accepted_tasks":` + itoaTest(int64(accepted)) + `}`
}

// 上限 3：受理 3 单后豆包链路关闭，后续请求落备胎班车。
func TestObserveDoubaoLinkAccepted_ClosesAtLimit(t *testing.T) {
	withRoutePolicy(t, limitPolicyJSON(3, 0))
	writes := capturePolicyWrites(t)

	assert.False(t, ObserveDoubaoLinkAccepted("seedance-2-5"), "第 1 单不该接满")
	assert.False(t, ObserveDoubaoLinkAccepted("seedance-2-5"), "第 2 单不该接满")
	reached := ObserveDoubaoLinkAccepted("seedance-2-5")

	assert.True(t, reached, "第 3 单应接满")
	assert.Equal(t, 3, seedanceroute_setting.DoubaoLinkAcceptedTasks())
	assert.True(t, seedanceroute_setting.IsDoubaoLinkOff(), "接满后豆包链路必须关闭")
	assert.Equal(t, seedanceroute_setting.DoubaoLinkOffReasonLimit, seedanceroute_setting.DoubaoLinkOffReason())
	assert.Len(t, *writes, 3, "每接一单写一次计数")
}

// 接满后不再计数：第四单不能再把计数推上去（那会超发额度）。
func TestObserveDoubaoLinkAccepted_StopsAtLimit(t *testing.T) {
	withRoutePolicy(t, limitPolicyJSON(2, 0))
	capturePolicyWrites(t)

	ObserveDoubaoLinkAccepted("seedance-2-5")
	ObserveDoubaoLinkAccepted("seedance-2-5")
	require.Equal(t, 2, seedanceroute_setting.DoubaoLinkAcceptedTasks())

	// 链路已关，正常不会再有豆包单进来；真进来了也不许把计数推过上限。
	ObserveDoubaoLinkAccepted("seedance-2-5")

	assert.Equal(t, 2, seedanceroute_setting.DoubaoLinkAcceptedTasks(), "计数不许超过上限")
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkRemaining())
}

// 上限 0（不限）= 默认状态：不写库、不计数。
//
// 这是最关键的一条：旧的生产 option JSON 里没有这个字段，
// 每天几千个视频请求不能因为加了这个功能就每单写一次 options 表。
func TestObserveDoubaoLinkAccepted_UnlimitedDoesNotWrite(t *testing.T) {
	withRoutePolicy(t, `{"enabled":true,"source_model":"seedance2.5-特惠",`+
		`"target_model":"seedance-2-5","trigger_seconds":30}`)
	writes := capturePolicyWrites(t)

	for i := 0; i < 5; i++ {
		assert.False(t, ObserveDoubaoLinkAccepted("seedance-2-5"))
	}

	assert.Empty(t, *writes, "不限时不许写库")
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkAcceptedTasks())
	assert.False(t, seedanceroute_setting.IsDoubaoLinkOff())
}

// 非豆包链路的模型不计数：备胎 wan3.0-班车 消耗的是备胎的额度。
func TestObserveDoubaoLinkAccepted_IgnoresOtherModels(t *testing.T) {
	withRoutePolicy(t, limitPolicyJSON(3, 0))
	writes := capturePolicyWrites(t)

	for _, modelName := range []string{
		"wan3.0-班车",          // 备胎
		"wan3.0-video",       // 短时长分流目标
		"wan3.0-720p-特惠-内链",  // 比例规则目标
		"",                   // 空
		"seedance-2-5-extra", // 名字相近但不是
	} {
		assert.False(t, ObserveDoubaoLinkAccepted(modelName))
	}

	assert.Empty(t, *writes, "非豆包链路的请求不计数")
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkAcceptedTasks())
}

// 落库失败不能让请求崩掉：任务已经受理、额度已经花出去，
// 计数只是台账，下次受理还会再试。
func TestObserveDoubaoLinkAccepted_SurvivesPersistError(t *testing.T) {
	withRoutePolicy(t, limitPolicyJSON(3, 0))
	previous := updateRoutePolicyOption
	updateRoutePolicyOption = func(key, value string) error {
		return errors.New("db is down")
	}
	t.Cleanup(func() { updateRoutePolicyOption = previous })

	assert.NotPanics(t, func() {
		ObserveDoubaoLinkAccepted("seedance-2-5")
	})
}

// 写回的是整份策略：任何字段被清空都会导致线上行为变化。
func TestObserveDoubaoLinkAccepted_PreservesOtherPolicyFields(t *testing.T) {
	withRoutePolicy(t, limitPolicyJSON(3, 0))
	capturePolicyWrites(t)

	ObserveDoubaoLinkAccepted("seedance-2-5")

	policy := seedanceroute_setting.Get()
	assert.True(t, policy.Enabled)
	assert.Equal(t, "seedance2.5-特惠", policy.SourceModel)
	assert.Equal(t, "seedance-2-5", policy.TargetModel)
	assert.Equal(t, 30, policy.TriggerSeconds)
	assert.Equal(t, "wan3.0-班车", policy.FailoverTargetModel)
	assert.Equal(t, 3, policy.DoubaoLinkMaxTasks, "上限必须保留")
}
