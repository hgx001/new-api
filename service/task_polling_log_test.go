package service

// 任务流水的写入验证：状态迁移与后处理阶段必须落 task_logs。
//
// 为什么单独测：这张表的价值全在「出事之后查得到」。如果 CAS 失败（另一个
// 进程已经推进过状态）也照样写一条迁移记录，就等于给排障埋假证据 ——
// 所以只断言「真的发生了并落库」的那一条。

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPollWritesTaskLogOnFailureTransition(t *testing.T) {
	truncate(t)

	const channelID = 331
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_log_fail", "upstream_log_fail", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode:  http.StatusOK,
		body:        `{"status":"failed","error":"官网生成失败"}`,
		parseResult: &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "官网生成失败"},
	}
	pollOnce(t, channelID, task, adaptor)

	logs, err := model.GetTaskLogs("task_log_fail", "", 20)
	require.NoError(t, err)
	require.NotEmpty(t, logs, "失败迁移必须留下流水")

	latest := logs[0]
	assert.Equal(t, model.TaskLogStagePoll, latest.Stage)
	assert.Equal(t, model.TaskLogLevelError, latest.Level)
	assert.Contains(t, latest.Message, string(model.TaskStatusFailure))
	assert.Contains(t, latest.Payload, "官网生成失败")
}

func TestPollWritesTaskLogOnSuccessTransition(t *testing.T) {
	truncate(t)

	const channelID = 332
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_log_ok", "upstream_log_ok", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusOK,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	logs, err := model.GetTaskLogs("task_log_ok", "", 20)
	require.NoError(t, err)
	require.NotEmpty(t, logs)
	assert.Equal(t, model.TaskLogLevelInfo, logs[0].Level)
	assert.Contains(t, logs[0].Message, string(model.TaskStatusSuccess))
}

// 上游临时故障（5xx / 无法解析）时状态不变，也不该写迁移流水 ——
// 否则「什么都没发生」被记成一次迁移，排障时会被误导。
func TestPollDoesNotWriteTransitionLogWhenStateUnchanged(t *testing.T) {
	truncate(t)

	const channelID = 333
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_log_noop", "upstream_log_noop", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusBadGateway,
		body:       `<html>bad gateway</html>`,
	}
	pollOnce(t, channelID, task, adaptor)

	logs, err := model.GetTaskLogs("task_log_noop", "", 20)
	require.NoError(t, err)
	for _, entry := range logs {
		assert.NotContains(t, entry.Message, "status ", "状态没变却写了迁移流水: %s", entry.Message)
	}
}
