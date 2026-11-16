package youzanwan3

// ════════════════════════════════════════════════════════════════════════════
// 特惠母本（seedance2.5-特惠）「30 秒走 prime」的契约测试
// ════════════════════════════════════════════════════════════════════════════
//
// 2026-11-16 老板口径：「特惠 30 秒的请求要走有赞的 prime 模型（画质增强）」，
// 因为 prime 目前只支持 30 秒。
//
// 背景：上游 prime（wan3.0-video-prime）是**独立上游**，画质更好、上游更便宜
// （720P ¥0.15/秒 vs 标准版 ¥0.18/秒），但它**仅支持 30 秒、仅 720P**。
//
// 所以这条是**逐秒 + 逐分辨率**的判断，不是模型级映射：
//   - 恰好 30 秒 且 交付档 ≤720P → 走 prime；
//   - 其余秒数（5-29 秒）→ 走标准版 wan3.0-video；
//   - 30 秒但客户要 1080P → **仍走标准版**（prime 不支持 1080P，
//     静默降到 720P = 「收 1080P 的钱给 720P 的片」）。
//
// 本文件守三件事：
//   1. 30 秒 + 720P → 上游模型是 wan3.0-video-prime；
//   2. 30 秒 + 1080P → 上游模型是 wan3.0-video（不降档）；
//   3. 非 30 秒 → 上游模型是 wan3.0-video（活动版完全不受影响）。

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

// 30 秒 + 720P → prime（画质增强 + 上游更便宜）。
func TestTehuiPrime_ThirtySecondsUsesPrime(t *testing.T) {
	got, err := upstreamModelForRequest(tehuiReq(30, "720P"))
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"特惠 30 秒必须走 prime（老板 2026-11-16 口径）")
}

// 30 秒 + 没传分辨率 → 仍是 prime（缺省档就是 720P）。
func TestTehuiPrime_ThirtySecondsDefaultResolutionUsesPrime(t *testing.T) {
	got, err := upstreamModelForRequest(tehuiReq(30, ""))
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"特惠 30 秒不传分辨率时缺省 720P，仍应走 prime")
}

// 30 秒 + 480P → prime（480P 会被归一到 720P，实际执行档仍是 720P）。
func TestTehuiPrime_ThirtySeconds480PUpgradedToPrime(t *testing.T) {
	got, err := upstreamModelForRequest(tehuiReq(30, "480P"))
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"特惠 30 秒 480P 会归一到 720P，应走 prime")
}

// 30 秒 + 1080P → 标准版（prime 不支持 1080P，不能静默降档）。
func TestTehuiPrime_ThirtySeconds1080PStaysStandard(t *testing.T) {
	got, err := upstreamModelForRequest(tehuiReq(30, "1080P"))
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video", got,
		"特惠 30 秒 1080P 必须留标准版 —— prime 只提供 720P，降档就是静默降质")
}

// 非 30 秒 → 标准版（prime 只接 30 秒）。
func TestTehuiPrime_NonThirtySecondsStaysStandard(t *testing.T) {
	for _, seconds := range []int{5, 10, 15, 20, 29} {
		got, err := upstreamModelForRequest(tehuiReq(seconds, "720P"))
		require.NoError(t, err)
		require.Equal(t, "wan3.0-video", got,
			"特惠 %d 秒必须留标准版（prime 仅接 30 秒）", seconds)
	}
}

// 活动版完全不受影响（老板口径只针对特惠母本）。
func TestTehuiPrime_HuoDongUnaffected(t *testing.T) {
	for _, seconds := range []int{5, 10, 30} {
		req := relaycommon.TaskSubmitReq{
			Model:    huoDongModel,
			Prompt:   "a cat",
			Duration: seconds,
		}
		got, err := upstreamModelForRequest(req)
		require.NoError(t, err)
		require.Equal(t, "wan3.0-video", got,
			"活动版 %d 秒必须走标准版（老板口径只针对特惠）", seconds)
	}
}

// prime 直连不受影响（它本来就只接 30 秒）。
func TestTehuiPrime_DirectPrimeUnaffected(t *testing.T) {
	got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model:    "wan3.0-video-prime",
		Prompt:   "a cat",
		Duration: 30,
	})
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got)
}

// 恒等性：模型名维度的映射口径没有变（upstreamModelFor 仍返回标准版）。
//
// 这条是防「有人把特惠的 30 秒例外直接写进 upstreamModelFor」——
// 那会让**所有**特惠请求（哪怕 5 秒）都落到 prime，而 prime 只接 30 秒。
func TestTehuiPrime_ModelOnlyMappingUnchanged(t *testing.T) {
	got, err := upstreamModelFor(tehuiModel)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video", got,
		"模型名维度的映射必须保持标准版，30 秒例外只能在请求级判断")
}
