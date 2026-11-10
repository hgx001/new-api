package controller

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// buildTaskSubmitSnapshotForTest 用真实的 gin context + StoreTaskRequest
// 走一遍快照构造，保证测的是真实链路而不是复制一份逻辑。
func buildTaskSubmitSnapshotForTest(req relaycommon.TaskSubmitReq) map[string]any {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	info := &relaycommon.RelayInfo{
		OriginModelName: req.Model,
		UserId:           36,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId: 17,
		},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	}
	relaycommon.StoreTaskRequest(context, info, "generate", req)
	return buildTaskSubmitSnapshot(context, info, 0)
}

// 2026-11-10 排障：任务 task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q 用户要 9:16，
// 上游实收 ratio=adaptive。排查时发现提交快照里**完全没有比例字段**，
// metadata_keys 也是空的 —— 只能绕道查上游有赞回执才定位到。
//
// 这里锁死快照必须记下比例（顶层 + metadata 两条来源）。

func TestSubmitSnapshotRecordsTopLevelRatio(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:  "seedance2.5-特惠",
		Prompt: "test prompt",
		Ratio:  "9:16",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok, "parameters 必须存在")
	require.Equal(t, "9:16", params["ratio"])
}

func TestSubmitSnapshotRecordsTopLevelAspectRatio(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:       "seedance2.5-特惠",
		Prompt:      "test prompt",
		AspectRatio: "9:16",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", params["aspect_ratio"])
}

// 回归护栏：metadata 路径（此前生产 38 条 9:16 依赖它）不能被破坏。
func TestSubmitSnapshotRecordsMetadataRatio(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:    "seedance2.5-特惠",
		Prompt:   "test prompt",
		Metadata: map[string]any{"ratio": "9:16"},
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", params["ratio"])
}

// 顶层与 metadata 都有值时，顶层优先 —— 但 aspect_ratio 要单独记，
// 不能因为 ratio 已被顶层占用就丢掉 metadata 里的 aspect_ratio。
func TestSubmitSnapshotKeepsBothRatioSources(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:       "seedance2.5-特惠",
		Prompt:      "test prompt",
		Ratio:       "16:9",
		AspectRatio: "9:16",
		Metadata: map[string]any{
			"ratio":        "4:3",
			"aspect_ratio": "1:1",
		},
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "16:9", params["ratio"])
	require.Equal(t, "9:16", params["aspect_ratio"])
}

// 没传比例时不得凭空造出比例字段。
func TestSubmitSnapshotNoRatioWhenAbsent(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:  "seedance2.5-特惠",
		Prompt: "test prompt",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	if ok {
		_, hasRatio := params["ratio"]
		require.False(t, hasRatio, "没传比例时不得凭空造出 ratio")
	}
}
