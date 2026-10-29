package wan3

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 对外任务状态的口径（2026-10-30 真机实测发现的问题）
//
// 现象：第一跳成功后，客户端 GET /v1/videos/{id} 立刻拿到 status=completed，
//	但库里任务还是 IN_PROGRESS —— 第二跳（成片提升）还在跑。
//
// 根因：ConvertToOpenAIVideo 原来用**上游回执**里的 TaskStatus 判定状态。
//	上游回执在第一跳成功那一刻就冻结了，写的是 "SUCCEEDED"，
//	含义是「第一跳生成完了」，不是「整条链路完了」。
//
// 后果（严重）：客户端以为完成了就去取片，拿到的��是**还没提升过的 480P** ——
//	正是这套链路要消灭的东西。相当于用户付了 720P 的钱拿到 480P。
//
// 口径：库内任务状态是两跳链路的真实进度，以它为准。
// ─────────────────────────────────────────────────────────────────────────────

// succeededUpstreamReceipt 的上游回执：task_status 已是 SUCCEEDED。
const succeededUpstreamReceipt = `{"request_id":"req-1","output":{"task_status":"SUCCEEDED","video_url":"https://wan3-oss.example.com/result/x.mp4"}}`

// TestConvertToOpenAIVideoReportsInProgressWhileSecondHopRuns 是本次缺陷的回归守卫。
//
// 第二跳在跑时（库里 IN_PROGRESS），对外必须是 in_progress，不能是 completed。
func TestConvertToOpenAIVideoReportsInProgressWhileSecondHopRuns(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_twohop",
		Status:   model.TaskStatusInProgress,
		Progress: "30%",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.Data = json.RawMessage(succeededUpstreamReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	require.Equal(t, dto.VideoStatusInProgress, video.Status,
		"第二跳还在跑，对外不能报 completed（否则客户端会去取还没提升的 480P）")
	require.NotContains(t, video.Metadata, "url",
		"第二跳没跑完时不能给下载地址")
}

// TestConvertToOpenAIVideoReportsCompletedAfterSuccess 锁定成功后的状态。
func TestConvertToOpenAIVideoReportsCompletedAfterSuccess(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_done",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.Data = json.RawMessage(succeededUpstreamReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	require.Equal(t, dto.VideoStatusCompleted, video.Status)
}

// TestConvertToOpenAIVideoReportsFailureFromTask 锁定失败状态。
//
// 上游回执里可能根本看不出失败（回执是提交时的快照），失败原因在 fail_reason。
func TestConvertToOpenAIVideoReportsFailureFromTask(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_failed",
		Status: model.TaskStatusFailure,
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.Data = json.RawMessage(succeededUpstreamReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	require.Equal(t, dto.VideoStatusFailed, video.Status)
}

// TestTaskVideoStatusFallsBackToUpstreamForUnknownStatus 锁定未知库内状态的兜底。
func TestTaskVideoStatusFallsBackToUpstreamForUnknownStatus(t *testing.T) {
	require.Equal(t, dto.VideoStatusCompleted,
		taskVideoStatus(&model.Task{Status: model.TaskStatus("WEIRD")}, "SUCCEEDED"),
		"库里状态不认识时回落到上游回执")
	require.Equal(t, dto.VideoStatusCompleted,
		taskVideoStatus(nil, "SUCCEEDED"),
		"task 为 nil 时回落到上游回执")
}

// TestTaskVideoStatusQueuedBeforeStart 锁定排队态。
func TestTaskVideoStatusQueuedBeforeStart(t *testing.T) {
	require.Equal(t, dto.VideoStatusQueued,
		taskVideoStatus(&model.Task{Status: model.TaskStatusQueued}, "PENDING"))
	require.Equal(t, dto.VideoStatusQueued,
		taskVideoStatus(&model.Task{Status: model.TaskStatusNotStart}, "PENDING"))
}
