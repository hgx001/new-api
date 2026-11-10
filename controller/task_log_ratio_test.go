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
	// 生效来源必须标出来 —— 一眼区分「客户端没传」与「传了但被降级」。
	require.Equal(t, string(relaycommon.TaskRatioSourceTopLevelRatio), params["ratio_source"])
	require.Equal(t, "9:16", params["ratio_top_level"])
}

func TestSubmitSnapshotRecordsTopLevelAspectRatio(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:       "seedance2.5-特惠",
		Prompt:      "test prompt",
		AspectRatio: "9:16",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", params["ratio"])
	require.Equal(t, string(relaycommon.TaskRatioSourceTopLevelAspect), params["ratio_source"])
	require.Equal(t, "9:16", params["aspect_ratio_top_level"])
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
	require.Equal(t, string(relaycommon.TaskRatioSourceMetadataRatio), params["ratio_source"])
	require.Equal(t, "9:16", params["ratio_metadata"])
}

// metadata 与顶层都有值时，生效的是 metadata（站内已验证的口径），
// 但**两条原始写法都要保留** —— 排查时要看得出「客户端两边传得不一致」。
func TestSubmitSnapshotKeepsBothRatioSources(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:       "seedance2.5-特惠",
		Prompt:      "test prompt",
		Ratio:       "16:9",
		AspectRatio: "4:3",
		Metadata: map[string]any{
			"ratio":        "9:16",
			"aspect_ratio": "1:1",
		},
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	// 生效值 = metadata.ratio（全站统一口径：metadata 优先于顶层）
	require.Equal(t, "9:16", params["ratio"])
	require.Equal(t, string(relaycommon.TaskRatioSourceMetadataRatio), params["ratio_source"])
	// 原始写法全留
	require.Equal(t, "16:9", params["ratio_top_level"])
	require.Equal(t, "4:3", params["aspect_ratio_top_level"])
	require.Equal(t, "9:16", params["ratio_metadata"])
	require.Equal(t, "1:1", params["aspect_ratio_metadata"])
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

// 非法比例必须单独记（ratio_invalid），不能混进 ratio ——
// 否则会把「非法值」误当成「生效值」，排查时得出错误结论。
func TestSubmitSnapshotRecordsInvalidRatioSeparately(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:  "seedance2.5-特惠",
		Prompt: "test prompt",
		Ratio:  "99:1",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "99:1", params["ratio_invalid"])
	// 非法比例不是生效值 —— ratio 不能等于它
	if ratio, exists := params["ratio"]; exists {
		require.NotEqual(t, "99:1", ratio, "非法比例不能记成生效值")
	}
}

// size 尺寸能推断出比例时，快照要记下来（客户没写比例但写了尺寸，
// 也是明确的画幅意图）。
func TestSubmitSnapshotInfersRatioFromSize(t *testing.T) {
	snapshot := buildTaskSubmitSnapshotForTest(relaycommon.TaskSubmitReq{
		Model:  "seedance2.5-特惠",
		Prompt: "test prompt",
		Size:   "720x1280",
	})

	params, ok := snapshot["parameters"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "9:16", params["ratio"])
	require.Equal(t, string(relaycommon.TaskRatioSourceSize), params["ratio_source"])
}
