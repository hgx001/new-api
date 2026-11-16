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
//   3. 非 30 秒 → 上游模型是 wan3.0-video。
//
// ⚠️ 2026-11-16 晚口径扩展：活动版（seedance2.5-活动）的 30 秒**同样走 prime**。
// 理由是成本口径 —— 活动按次 ¥4.5/条，原本就是按 prime ¥0.15/秒 × 30 秒 = ¥4.50
// 测算的；落到标准版（¥0.18/秒 → ¥5.40）反而每单倒挂 ¥0.90。所以判定从
// 「只认特惠母本」放宽成「认整个促销族」，见 isPromoFamilyModel。

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

// 活动版（按次档）30 秒同样走 prime（2026-11-16 晚口径，与母本同一条路径）。
//
// 成本口径是这条改动的**唯一理由**：活动按次卖 ¥4.5/条，测算基准就是
// prime ¥0.15/秒 × 30 秒 = ¥4.50（毛利 0，靠量）。标准版 720P 是 ¥0.18/秒
// → 30 秒 ¥5.40，每单倒挂 ¥0.90 —— 跑得越多亏得越多。
//
// 能力上两者完全等价：活动版固定 30 秒、固定 720P，正好落在 prime 的
// 「仅 30 秒、仅 720P」边界内，不存在降档风险。
func TestTehuiPrime_HuoDongThirtySecondsUsesPrime(t *testing.T) {
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
//
// 缺省值必须和显式传 30 走同一条路：否则「大多数客户不传时长」的这条链路
// 会静默落在标准版上，按次价格倒挂，而且从外部完全看不出来。
func TestTehuiPrime_HuoDongDefaultDurationUsesPrime(t *testing.T) {
	got, err := upstreamModelForRequest(relaycommon.TaskSubmitReq{
		Model:  huoDongModel,
		Prompt: "a cat",
	})
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"活动版不传时长缺省 30 秒，应走 prime")
}

// 活动版非 30 秒 → 标准版（prime 只接 30 秒，别的秒数打过去必被上游拒单）。
//
// ⚠️ 这条和上面两条是一组：放宽判定范围**不能**顺手把「仅 30 秒」这条
// 边界一起放宽 —— prime 接不住 29 秒，硬塞就是 100% 失败率。
func TestTehuiPrime_HuoDongNonThirtySecondsStaysStandard(t *testing.T) {
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
//
// 这条守的是「别把逐秒例外写进 upstreamModelFor」—— 那个函数被
// GetModelList / 计费路径复用，改它会让所有活动请求都落到 prime。
func TestTehuiPrime_HuoDongModelOnlyMappingUnchanged(t *testing.T) {
	got, err := upstreamModelFor(huoDongModel)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video", got,
		"活动版的默认档仍是标准版，逐秒例外只在 upstreamModelForRequest 里")
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
