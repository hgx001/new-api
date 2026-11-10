package service

import (
	"errors"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 这一层锁的是「额度探针 → 落库」的接线：
//
//	setting 包已经把「该不该暂停」做成纯函数（见 quota_test.go），
//	这里只负责在任务真正进入终态时把决定写进 options 表，以及
//	「不该写的时候一条都不写」。
//
// 为什么单独测：写库是最容易出错的一步 —— 写早了会在任务只是
// 进度更新时误停路由，写晚了会让一批请求白白改派到没额度的渠道。

const (
	quotaTargetModel = "seedance-2-5"
	quotaSourceModel = "seedance2.5-特惠"
)

// baselinePolicy 三条规则都配全、未暂停。
const baselinePolicy = `{"enabled":true,"source_model":"seedance2.5-特惠",` +
	`"target_model":"seedance-2-5","trigger_seconds":30,` +
	`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`

func withRoutePolicy(t *testing.T, raw string) {
	t.Helper()
	previous := seedanceroute_setting.MarshalJSONString()
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(raw))
	t.Cleanup(func() {
		_ = seedanceroute_setting.UpdateFromJSONString(previous)
	})
}

// capturePolicyWrites 把落库换成内存收集，避免测试依赖数据库。
//
// 钩子除了收集还要把 JSON 分发回内存状态 —— 真实的 model.UpdateOption
// 就是这么做的（先写库，再经 updateOptionMap → UpdateFromJSONString）。
// 少了这一步，断言读到的是旧策略，测试会误报。
func capturePolicyWrites(t *testing.T) *[]string {
	t.Helper()
	writes := []string{}
	previous := updateRoutePolicyOption
	updateRoutePolicyOption = func(key, value string) error {
		assert.Equal(t, seedanceroute_setting.OptionKey, key, "只能写路由策略这一个 option")
		writes = append(writes, value)
		return seedanceroute_setting.UpdateFromJSONString(value)
	}
	t.Cleanup(func() { updateRoutePolicyOption = previous })
	return &writes
}

func failedTask(modelName, reason string) *model.Task {
	return &model.Task{
		TaskID:     "task_quota_watch",
		Status:     model.TaskStatusFailure,
		FailReason: reason,
		Properties: model.Properties{OriginModelName: modelName},
	}
}

func successTask(modelName string) *model.Task {
	return &model.Task{
		TaskID:     "task_quota_watch_ok",
		Status:     model.TaskStatusSuccess,
		Properties: model.Properties{OriginModelName: modelName},
	}
}

func TestObserveVideoTaskOutcome_AutoPausesOnQuotaExhausted(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	observeVideoTaskOutcome(failedTask(quotaTargetModel, "渠道今日额度已用完，明天再来！"))

	require.Len(t, *writes, 1, "额度耗尽应写一次策略")
	policy := seedanceroute_setting.Get()
	assert.Equal(t, seedanceroute_setting.PauseSourceAuto, policy.PauseSource)
	assert.Equal(t, seedanceroute_setting.ReasonQuotaExhausted, policy.PauseReason)
	assert.Equal(t,
		seedanceroute_setting.NextBeijingQuotaReset(time.Now().Unix()),
		policy.SecondsRulePausedUntil,
		"应暂停到下一个北京时间凌晨 2 点（上游额度重置点）")
	assert.False(t, seedanceroute_setting.IsEnabled(), "规则一应立刻失效")
	assert.True(t, seedanceroute_setting.IsRatioRouteEnabled(), "比例规则不该被波及")
}

func TestObserveVideoTaskOutcome_IgnoresNonQuotaFailure(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	for _, reason := range []string{
		"内容审核不通过：检测到真人面部",
		"参考素材时长过长：当前为 31.609 秒，最长允许 15 秒",
		"上游生成失败（未提供具体原因），请稍后重试或联系客服",
		"",
	} {
		observeVideoTaskOutcome(failedTask(quotaTargetModel, reason))
	}

	assert.Empty(t, *writes, "非额度原因的失败不许动路由")
}

func TestObserveVideoTaskOutcome_IgnoresOtherModel(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	// 源模型自己报额度耗尽，是另一条链路的问题。停掉改派只会让请求
	// 继续压在坏链路上，还会掩盖真实故障。
	observeVideoTaskOutcome(failedTask(quotaSourceModel, "渠道今日额度已用完，明天再来！"))
	observeVideoTaskOutcome(failedTask("wan3.0-720p-特惠", "渠道今日额度已用完，明天再来！"))
	observeVideoTaskOutcome(failedTask("", "渠道今日额度已用完，明天再来！"))

	assert.Empty(t, *writes)
}

func TestObserveVideoTaskOutcome_AutoResumesOnSuccess(t *testing.T) {
	// 先造一个自动暂停的现状。
	until := seedanceroute_setting.NextBeijingMidnight(time.Now().Unix())
	withRoutePolicy(t, `{"enabled":true,"source_model":"seedance2.5-特惠",`+
		`"target_model":"seedance-2-5","trigger_seconds":30,`+
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16",`+
		`"seconds_rule_paused_until":`+itoaTest(until)+`,`+
		`"pause_source":"auto","pause_reason":"上游渠道今日已用完"}`)
	require.False(t, seedanceroute_setting.IsEnabled())
	writes := capturePolicyWrites(t)

	observeVideoTaskOutcome(successTask(quotaTargetModel))

	require.Len(t, *writes, 1, "额度恢复应写一次策略")
	policy := seedanceroute_setting.Get()
	assert.Zero(t, policy.SecondsRulePausedUntil)
	assert.Empty(t, policy.PauseSource)
	assert.Empty(t, policy.PauseReason)
	assert.True(t, seedanceroute_setting.IsEnabled(), "改派应立刻接回")
}

func TestObserveVideoTaskOutcome_KeepsManualPause(t *testing.T) {
	manualUntil := time.Now().Add(72 * time.Hour).Unix()
	withRoutePolicy(t, `{"enabled":true,"source_model":"seedance2.5-特惠",`+
		`"target_model":"seedance-2-5","trigger_seconds":30,`+
		`"seconds_rule_paused_until":`+itoaTest(manualUntil)+`,`+
		`"pause_source":"manual"}`)
	writes := capturePolicyWrites(t)

	// 人工暂停期间：出现成功不许自动解除，出现额度耗尽也不许改写截止时间。
	observeVideoTaskOutcome(successTask(quotaTargetModel))
	observeVideoTaskOutcome(failedTask(quotaTargetModel, "渠道今日额度已用完，明天再来！"))

	assert.Empty(t, *writes, "人工暂停期间自动逻辑必须闭嘴")
	assert.Equal(t, manualUntil, seedanceroute_setting.SecondsRulePausedUntil())
	assert.Equal(t, seedanceroute_setting.PauseSourceManual, seedanceroute_setting.Get().PauseSource)
}

func TestObserveVideoTaskOutcome_IgnoresNonTerminalStatus(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	for _, status := range []model.TaskStatus{
		model.TaskStatusInProgress,
		model.TaskStatusNotStart,
		model.TaskStatusQueued,
		"",
	} {
		task := failedTask(quotaTargetModel, "渠道今日额度已用完，明天再来！")
		task.Status = status
		observeVideoTaskOutcome(task)
	}

	assert.Empty(t, *writes, "只有终态才驱动额度探针")
}

func TestObserveVideoTaskOutcome_SurvivesPersistError(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	previous := updateRoutePolicyOption
	updateRoutePolicyOption = func(key, value string) error {
		return errors.New("db is down")
	}
	t.Cleanup(func() { updateRoutePolicyOption = previous })

	// 落库失败不能让轮询流程崩掉 —— 任务终态已经写好了，
	// 额度探针只是附带动作，下次失败还会再触发一次。
	assert.NotPanics(t, func() {
		observeVideoTaskOutcome(failedTask(quotaTargetModel, "渠道今日额度已用完，明天再来！"))
	})
}

func TestObserveVideoTaskOutcome_PreservesOtherPolicyFields(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	observeVideoTaskOutcome(failedTask(quotaTargetModel, "渠道今日额度已用完，明天再来！"))
	require.Len(t, *writes, 1)

	// 写回的是整份策略，任何字段被清空都会导致线上行为变化
	// （例如 source_model 被清空 → 规则一静默失效）。
	policy := seedanceroute_setting.Get()
	assert.True(t, policy.Enabled)
	assert.Equal(t, quotaSourceModel, policy.SourceModel)
	assert.Equal(t, quotaTargetModel, policy.TargetModel)
	assert.Equal(t, 30, policy.TriggerSeconds)
	assert.Equal(t, "wan3.0-720p-特惠", policy.RatioTargetModel)
	assert.Equal(t, "9:16", policy.TriggerRatio)
	assert.NotZero(t, policy.UpdatedAt)
}

func TestObserveVideoTaskOutcome_NilTaskIsSafe(t *testing.T) {
	withRoutePolicy(t, baselinePolicy)
	writes := capturePolicyWrites(t)

	assert.NotPanics(t, func() { observeVideoTaskOutcome(nil) })
	assert.Empty(t, *writes)
}

// itoaTest 把 int64 转成十进制字符串，拼 JSON 用。
func itoaTest(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
