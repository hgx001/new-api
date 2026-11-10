package dashscope

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查发现：dashscope 的 resolveRatio 只读 metadata.ratio，
// 客户端把比例放在**请求体顶层**时会被静默丢弃 → 上游收到 adaptive（1:1）。
// 与首轮修复的 youzanwan3 / megabyai 是同一个根因。
func TestResolveRatioTopLevelRatio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Ratio: "9:16"}
	require.Equal(t, "9:16", resolveRatio(req))
}

func TestResolveRatioTopLevelAspectRatio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{AspectRatio: "9:16"}
	require.Equal(t, "9:16", resolveRatio(req))
}

// metadata 仍保持最高优先级（站内已验证的传法，行为变更最小）。
func TestResolveRatioMetadataWinsOverTopLevel(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Ratio:    "16:9",
		Metadata: map[string]any{"ratio": "9:16"},
	}
	require.Equal(t, "9:16", resolveRatio(req))
}

// 顶层 ratio 优先于顶层 aspect_ratio。
func TestResolveRatioTopLevelRatioWinsOverAspectRatio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Ratio: "9:16", AspectRatio: "16:9"}
	require.Equal(t, "9:16", resolveRatio(req))
}

// 原有行为不能被破坏：size 尺寸推断仍然可用。
func TestResolveRatioSizeFallbackStillWorks(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Size: "1280x720"}
	require.Equal(t, "16:9", resolveRatio(req))
}

// 什么都没传 → 默认 16:9。
func TestResolveRatioDefault(t *testing.T) {
	req := relaycommon.TaskSubmitReq{}
	require.Equal(t, "16:9", resolveRatio(req))
}

// 非法比例走静默降级（DashScope 口径），落默认值而不是报错。
func TestResolveRatioInvalidFallsBackToDefault(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Ratio: "99:1"}
	require.Equal(t, "16:9", resolveRatio(req))
}
