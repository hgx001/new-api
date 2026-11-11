package service

// 任务完成时长的落库验证。
//
// 为什么单独一组测试：使用日志里的「耗时」列对**任务类**请求是错的。
// 消费日志（type=2）在**提交时**写入，那时上游只回了「已受理」，
// use_time 记的是提交往返耗时（生产实测普遍 2.0s），
// 跟「视频实际跑了多久」完全无关 —— 用户在日志页看到 2.0s，
// 会以为视频 2 秒就生成了，实际上上游要跑 1~3 分钟。
//
// 真实完成时长只在轮询收敛（任务变 SUCCESS/FAILURE）那一刻才存在，
// 那时 task.FinishTime 已经落库。所以做法是：
//   提交时写的那条消费日志 → 任务终态时回写 task_duration。
//
// 这里锁住的行为：
//  1. 任务成功 → 提交时的消费日志被回写上完成时长
//  2. 任务失败 → 同样回写（失败也有耗时，是排障的重要信息）
//  3. 回写是幂等的：多轮轮询不会把时长累加或覆盖成乱数
//  4. 没有对应消费日志时不报错（不能因为统计而打断任务链路）
//  5. 客户侧日志不泄露渠道信息（耗时字段本身不是渠道信息，但要确保
//     新字段没有绕过 formatUserLogs 的脱敏）

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTaskConsumeLog 造一条「提交时写的消费日志」，带 task_id 关联键。
func seedTaskConsumeLog(t *testing.T, userId int, taskID string, modelName string) *model.Log {
	t.Helper()
	log := &model.Log{
		UserId:    userId,
		Username:  "duration_user",
		CreatedAt: time.Now().Unix(),
		Type:      model.LogTypeConsume,
		Content:   "操作 generate",
		ModelName: modelName,
		Quota:     100,
		UseTime:   2,
		TaskId:    taskID,
		Other:     `{"is_task":true}`,
	}
	require.NoError(t, model.LOG_DB.Create(log).Error)
	return log
}

// findTaskConsumeLog 找到该任务提交时写的那条消费日志。
func findTaskConsumeLog(t *testing.T, taskID string) *model.Log {
	t.Helper()
	var log model.Log
	require.NoError(t, model.LOG_DB.Where("task_id = ?", taskID).First(&log).Error)
	return &log
}

func TestTaskDurationWrittenOnSuccess(t *testing.T) {
	truncate(t)

	const channelID = 941
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dur_ok", "upstream_dur_ok", 100)
	task.SubmitTime = time.Now().Unix() - 90
	require.NoError(t, model.DB.Save(task).Error)

	seedTaskConsumeLog(t, task.UserId, task.TaskID, "wan3.0-1080p")

	adaptor := &upstreamFailureAdaptor{
		statusCode: 200,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	log := findTaskConsumeLog(t, "task_dur_ok")
	assert.Equal(t, 90, log.TaskDuration,
		"任务成功收敛后，提交时写的消费日志必须带上真实完成时长")
}

func TestTaskDurationWrittenOnFailure(t *testing.T) {
	truncate(t)

	const channelID = 942
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dur_fail", "upstream_dur_fail", 100)
	task.SubmitTime = time.Now().Unix() - 45
	require.NoError(t, model.DB.Save(task).Error)

	seedTaskConsumeLog(t, task.UserId, task.TaskID, "wan3.0-1080p")

	adaptor := &upstreamFailureAdaptor{
		statusCode: 200,
		body:       `{"status":"failed","error":"官网生成失败"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusFailure,
			Reason: "官网生成失败",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	log := findTaskConsumeLog(t, "task_dur_fail")
	assert.Equal(t, 45, log.TaskDuration,
		"任务失败也要记录耗时（排障时非常关键），不能只记成功")
}

// 幂等：同一任务被多轮轮询观察到终态时，耗时不能被累加。
func TestTaskDurationIsIdempotent(t *testing.T) {
	truncate(t)

	const channelID = 943
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dur_idem", "upstream_dur_idem", 100)
	task.SubmitTime = time.Now().Unix() - 60
	require.NoError(t, model.DB.Save(task).Error)

	seedTaskConsumeLog(t, task.UserId, task.TaskID, "wan3.0-1080p")

	adaptor := &upstreamFailureAdaptor{
		statusCode: 200,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	// 连跑三轮：只有第一轮真正发生状态迁移，后两轮应该是 no-op。
	pollOnce(t, channelID, task, adaptor)
	pollOnce(t, channelID, task, adaptor)
	pollOnce(t, channelID, task, adaptor)

	log := findTaskConsumeLog(t, "task_dur_idem")
	assert.Equal(t, 60, log.TaskDuration,
		"多轮轮询后耗时必须还是第一次收敛时的值，不能累加")
}

// 没有消费日志时（老任务、或日志被清理）不能报错 ——
// 统计功能绝对不能打断任务主链路。
func TestTaskDurationMissingLogDoesNotFailTask(t *testing.T) {
	truncate(t)

	const channelID = 944
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dur_nolog", "upstream_dur_nolog", 100)
	task.SubmitTime = time.Now().Unix() - 30
	require.NoError(t, model.DB.Save(task).Error)

	adaptor := &upstreamFailureAdaptor{
		statusCode: 200,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status,
		"没有消费日志时，任务本身必须照常成功")
}

// 端到端：提交写日志 → 任务跑完回写耗时 → 用户查日志看到真实耗时。
//
// 这条走的是**用户侧出口**（GetUserLogs → formatUserLogs），
// 与上面几条的读库路径不同：用户拿到的是脱敏后的日志。
// 必须单独验一遍，因为「客户能不能看到耗时」和「管理员能不能看到」
// 是两件事 —— 前者还要过一遍脱敏。
func TestTaskDurationVisibleToUserAfterCompletion(t *testing.T) {
	truncate(t)

	const channelID = 945
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dur_e2e", "upstream_dur_e2e", 100)
	task.SubmitTime = time.Now().Unix() - 137
	require.NoError(t, model.DB.Save(task).Error)

	// 提交时写的消费日志：task_duration 还是 0，use_time 是提交往返耗时。
	seedTaskConsumeLog(t, task.UserId, task.TaskID, "seedance2.5-特惠")

	adaptor := &upstreamFailureAdaptor{
		statusCode: 200,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	logs, _, err := model.GetUserLogs(task.UserId, model.LogTypeConsume, 0, 0, "", "", 0, 20, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)

	assert.Equal(t, 137, logs[0].TaskDuration,
		"用户侧看到的耗时应是真实完成时长 137s，而不是提交往返的 2s")
	assert.Equal(t, 2, logs[0].UseTime,
		"提交往返耗时保持原值不动 —— 它在流式场景下仍有意义")
	assert.Equal(t, 0, logs[0].ChannelId,
		"耗时能给客户看，渠道编号仍然必须脱敏")
}
