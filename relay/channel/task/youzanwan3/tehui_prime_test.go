package youzanwan3

// ════════════════════════════════════════════════════════════════════════════
// 活动版（按次档）与 prime 直连的历史上游映射
// ════════════════════════════════════════════════════════════════════════════
//
// ⚠️ 2026-11-20 变更：促销族母本（seedance2.5-特惠）的常驻上游已换成
// **mw-sd2.5-flash**，母本的映射断言迁到 mw_sd25_flash_test.go。
// 活动版（seedance2.5-活动）同日**下架**，但它的映射保持不变 ——
// 在途任务轮询与回滚都依赖这条历史口径，不要顺手改掉。
//
// 活动版（seedance2.5-活动，按次 ¥4.5/条）：
//   - 30 秒 → prime（成本基准 ¥0.15/秒 × 30 = ¥4.5）；
//   - 其它秒数 → 标准版 wan3.0-video。
//
// prime 直连（wan3.0-video-prime）：仅 30 秒、仅 720P，不受影响。

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/require"
)

// tehuiReq 造一条特惠请求（时长/分辨率可配）。
func tehuiReq(seconds int, resolution string) relaycommon.TaskSubmitReq {
	req := relaycommon.TaskSubmitReq{
		Model:  tehuiModel,
		Prompt: "a cat",
	}
	if seconds > 0 {
		req.Duration = seconds
	}
	if resolution != "" {
		req.Resolution = resolution
	}
	return req
}

// 母本常驻映射的断言已迁到 mw_sd25_flash_test.go（2026-11-20 上游换成
// mw-sd2.5-flash）。本文件只保留活动版与 prime 直连的历史口径 ——
// 活动版已下架，但映射保持不变，避免在途任务/回滚被改坏。

// 活动版（按次档）30 秒仍走 prime —— 成本基准 ¥0.15/秒 × 30 = ¥4.5。
func TestHuoDong_ThirtySecondsUsesPrime(t *testing.T) {
	got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model:    huoDongModel,
		Prompt:   "a cat",
		Duration: 30,
	})
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"活动版 30 秒必须走 prime —— 按次 ¥4.5 的成本基准就是 prime 的 ¥0.15×30")
}

// 活动版不传时长 → 缺省 30 秒 → 同样走 prime。
func TestHuoDong_DefaultDurationUsesPrime(t *testing.T) {
	got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model:  huoDongModel,
		Prompt: "a cat",
	})
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"活动版不传时长缺省 30 秒，应走 prime")
}

// 活动版非 30 秒 → 标准版（prime 只接 30 秒）。
func TestHuoDong_NonThirtySecondsStaysStandard(t *testing.T) {
	for _, seconds := range []int{5, 8, 10, 15, 20, 29} {
		got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
			Model:    huoDongModel,
			Prompt:   "a cat",
			Duration: seconds,
		})
		require.NoError(t, err)
		require.Equal(t, "wan3.0-video", got,
			"活动版 %d 秒必须留标准版（prime 仅接 30 秒）", seconds)
	}
}

// 活动版的模型名维度映射不变：upstreamModelFor 仍返回标准版。
func TestHuoDong_ModelOnlyMappingUnchanged(t *testing.T) {
	got, err := upstreamModelFor(huoDongModel)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video", got,
		"活动版的默认档仍是标准版，逐秒例外只在 upstreamModelForRequest 里")
}

// prime 直连不受影响（它本来就只接 30 秒）。
func TestDirectPrimeUnaffected(t *testing.T) {
	got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model:    "wan3.0-video-prime",
		Prompt:   "a cat",
		Duration: 30,
	})
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got)
}
