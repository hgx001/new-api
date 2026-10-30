package model

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddTaskLogPersistsSnapshot(t *testing.T) {
	taskId := "task_log_persist_" + time.Now().Format("150405.000000")
	AddTaskLog(taskId, "upstream-1", TaskLogStageSubmit, TaskLogLevelInfo, "task submitted",
		map[string]any{"prompt": "一个原始人采蜂蜜", "media": []string{"https://example.com/a.png"}})

	logs, err := GetTaskLogs(taskId, "", 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, TaskLogStageSubmit, logs[0].Stage)
	assert.Equal(t, TaskLogLevelInfo, logs[0].Level)
	assert.Equal(t, "upstream-1", logs[0].UpstreamTaskId)
	assert.Contains(t, logs[0].Payload, "一个原始人采蜂蜜")
	assert.Contains(t, logs[0].Payload, "https://example.com/a.png")
}

// 没有 task id 时不写库：避免产生无法归属的孤儿行。
func TestAddTaskLogSkipsEmptyTaskID(t *testing.T) {
	before := countTaskLogs(t)
	AddTaskLog("", "upstream-x", TaskLogStagePoll, TaskLogLevelInfo, "noop", nil)
	assert.Equal(t, before, countTaskLogs(t))
}

func TestGetTaskLogsByUpstreamID(t *testing.T) {
	upstreamId := "upstream-only-" + time.Now().Format("150405.000000")
	AddTaskLog("task_by_upstream", upstreamId, TaskLogStagePoll, TaskLogLevelError, "boom", nil)

	logs, err := GetTaskLogs("", upstreamId, 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, "task_by_upstream", logs[0].TaskId)
}

func TestGetTaskLogsRequiresSomeID(t *testing.T) {
	_, err := GetTaskLogs("", "", 10)
	require.Error(t, err)
}

func TestDeleteTaskLogsBeforeRemovesOnlyExpired(t *testing.T) {
	now := time.Now().Unix()
	expired := "task_expired_" + time.Now().Format("150405.000000")
	fresh := "task_fresh_" + time.Now().Format("150405.000000")
	AddTaskLog(expired, "", TaskLogStagePoll, TaskLogLevelInfo, "old", nil)
	AddTaskLog(fresh, "", TaskLogStagePoll, TaskLogLevelInfo, "new", nil)

	// 只把过期那条的时间戳往前推，模拟 30 天前的记录。
	require.NoError(t, DB.Model(&TaskLog{}).Where("task_id = ?", expired).
		Update("created_at", now-40*24*3600).Error)

	deleted, err := DeleteTaskLogsBefore(now-30*24*3600, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(1))

	expiredLogs, err := GetTaskLogs(expired, "", 10)
	require.NoError(t, err)
	assert.Empty(t, expiredLogs)

	freshLogs, err := GetTaskLogs(fresh, "", 10)
	require.NoError(t, err)
	assert.Len(t, freshLogs, 1)
}

// 上游可能把整页 HTML 塞进错误信息：入库前必须截断，且不能切碎汉字。
func TestTaskLogTruncatesMessageAndPayload(t *testing.T) {
	taskId := "task_truncate_" + time.Now().Format("150405.000000")
	longMessage := strings.Repeat("错", taskLogMessageLimit+50)
	longPayload := strings.Repeat("提示词", taskLogPayloadLimit)
	AddTaskLog(taskId, "", TaskLogStageSubmit, TaskLogLevelError, longMessage, map[string]any{"prompt": longPayload})

	logs, err := GetTaskLogs(taskId, "", 10)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.LessOrEqual(t, len([]rune(logs[0].Message)), taskLogMessageLimit+len("…(truncated)"))
	assert.Contains(t, logs[0].Message, "…(truncated)")
	assert.LessOrEqual(t, len([]rune(logs[0].Payload)), taskLogPayloadLimit+len("…(truncated)"))
	// 截断后仍是合法 UTF-8：不能出现半个汉字导致的替换字符
	assert.NotContains(t, logs[0].Message, "\uFFFD")
}

func TestTruncateTextKeepsShortValue(t *testing.T) {
	assert.Equal(t, "短文本", truncateText("短文本", 10))
}

func countTaskLogs(t *testing.T) int64 {
	t.Helper()
	var total int64
	require.NoError(t, DB.Model(&TaskLog{}).Count(&total).Error)
	return total
}
