package youzanwan3

// ════════════════════════════════════════════════════════════════════════════
// 促销族母本的上游契约（2026-11-20 改版：常驻 mw-sd2.5-flash）
// ════════════════════════════════════════════════════════════════════════════
//
// 老板 2026-11-20 口径：
//   - 有赞的 mw-sd2.5-flash 接进平台，特惠请求全部路由到它；
//   - 之前的 cp-seedance-2.5flash 撤掉（不再作为任何出口）；
//   - 活动版（seedance2.5-活动）下架，不做。
//
// 上游契约（api.youzan666.vip/docs 2026-11-19 版）：
//   - 时长 4–30 整数秒，缺省 5 秒；
//   - 分辨率 **只有 480p / 720p**（0.10 / 0.15 ⚡每秒）；
//   - 画幅 16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9 / auto，缺省 16:9；
//   - 素材：10 图 / 5 视频 / 5 音频，首尾帧也支持（尾帧依赖首帧、与普通参考互斥）；
//   - 参数形态 duration + **小写** resolution + ratio + media；
//   - 计费按（输出秒数 + 参考视频计时秒数）× 该分辨率每秒单价。
//
// 对客口径不变：seedance2.5-特惠 恒定交付 720P、¥0.20/秒。
//   1080P 显式请求 → 400（上游没有这一档，静默降档等于「收 1080P 的钱给 720P」）。

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeBody 把适配器产出的 JSON 解成 map，供字段断言。
func decodeBody(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var body map[string]interface{}
	require.NoError(t, common.Unmarshal(raw, &body))
	return body
}

// 特惠的常驻上游已换成 mw-sd2.5-flash —— 任意合法秒数都落它。
func TestTehui_MwFlashResidentForAllDurations(t *testing.T) {
	for _, seconds := range []int{5, 10, 15, 20, 29, 30} {
		got, err := upstreamModelForRequest(tehuiReq(seconds, "720P"))
		require.NoError(t, err)
		require.Equal(t, "mw-sd2.5-flash", got, "特惠 %d 秒应落 mw-sd2.5-flash", seconds)
	}
}

// 映射层只看模型名：非 720P 也在映射层落 mw，兜底 400 在校验阶段。
func TestTehui_MwFlashMappingIgnoresResolution(t *testing.T) {
	for _, resolution := range []string{"480P", "1080P"} {
		got, err := upstreamModelForRequest(tehuiReq(20, resolution))
		require.NoError(t, err)
		require.Equal(t, "mw-sd2.5-flash", got,
			"映射层不做「偷偷换模型」；%s 的处理在 validatePromoResolutionForModel", resolution)
	}
}

// 模型级映射（不含时长/分辨率）同样落 mw。
func TestTehui_ModelOnlyMappingIsMwFlash(t *testing.T) {
	got, err := upstreamModelFor(tehuiModel)
	require.NoError(t, err)
	require.Equal(t, "mw-sd2.5-flash", got)
}

// 显式 1080P 必须 400：上游只有 480p/720p。
func TestTehui_Explicit1080PRejected(t *testing.T) {
	taskErr := validatePromoResolutionForModel(tehuiReq(5, "1080P"))
	require.NotNil(t, taskErr, "1080P 必须 fail-loud，不能静默降档")
	assert.Equal(t, "invalid_resolution", taskErr.Code)
}

// 显式 480P 不报错，按最低交付档 720P 执行（升档，客户不吃亏）。
func TestTehui_480PUpgradedTo720P(t *testing.T) {
	req := tehuiReq(5, "480P")
	require.Nil(t, validatePromoResolutionForModel(req))
	require.Equal(t, "720P", resolveResolutionForModel(req, tehuiModel))
}

// 没传分辨率 → 720P（不是 1080P，也不是 480P）。
func TestTehui_DefaultResolution720P(t *testing.T) {
	require.Equal(t, "720P", resolveResolutionForModel(tehuiReq(5, ""), tehuiModel))
}

// 没传时长 → 5 秒（上游缺省，也是对外下界）。
func TestTehui_DefaultDurationFiveSeconds(t *testing.T) {
	require.Equal(t, 5, resolveDurationForModel(tehuiReq(0, "720P"), tehuiModel))
}

// 对外时长区间 5–30：4 秒与 31 秒都必须 400。
func TestTehui_DurationOutOfRangeRejected(t *testing.T) {
	require.NotNil(t, validateExplicitDurationForModel(tehuiReq(4, "720P")),
		"4 秒低于对外下界 5 秒")
	require.NotNil(t, validateExplicitDurationForModel(tehuiReq(31, "720P")),
		"31 秒超过上游上界 30 秒")
	require.Nil(t, validateExplicitDurationForModel(tehuiReq(5, "720P")))
	require.Nil(t, validateExplicitDurationForModel(tehuiReq(30, "720P")))
}

// 画幅白名单要覆盖 mw 的七档（含 21:9 与 auto）；adaptive 回退 16:9 而不是报错。
func TestTehui_RatioWhitelist(t *testing.T) {
	for _, ratio := range []string{"16:9", "9:16", "4:3", "3:4", "1:1", "21:9", "auto"} {
		req := tehuiReq(5, "720P")
		req.Ratio = ratio
		require.Equal(t, ratio, resolveRatio(req), "画幅 %s 应原样保留", ratio)
	}
	// 历史写法 adaptive：不在白名单 → 回退缺省，不中断请求。
	req := tehuiReq(5, "720P")
	req.Ratio = "adaptive"
	require.Equal(t, "16:9", resolveRatio(req))
}

// 参考视频/音频必须放行 —— mw-sd2.5-flash 支持，拦掉等于禁掉一半能力。
func TestTehui_ReferenceAudioVideoAllowed(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "https://example.com/a.jpg"},
		{Type: "reference_video", URL: "https://example.com/b.mp4"},
		{Type: "reference_audio", URL: "https://example.com/c.mp3"},
	}
	require.NoError(t, validateMedia(media, tehuiModel))
}

// 首尾帧放行；但尾帧缺首帧、首尾帧与普通参考混用必须本地 400。
func TestTehui_FirstLastFrameRules(t *testing.T) {
	ok := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: "https://example.com/f.jpg"},
		{Type: "last_frame", URL: "https://example.com/l.jpg"},
	}
	require.NoError(t, validateMedia(ok, tehuiModel), "首尾帧成对出现应放行")

	orphanLast := []relaycommon.TaskMedia{
		{Type: "last_frame", URL: "https://example.com/l.jpg"},
	}
	require.Error(t, validateMedia(orphanLast, tehuiModel), "只有尾帧必须拒")

	mixed := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: "https://example.com/f.jpg"},
		{Type: "reference_image", URL: "https://example.com/a.jpg"},
	}
	require.Error(t, validateMedia(mixed, tehuiModel), "首尾帧与普通参考互斥")
}

// 请求体契约：duration 整数、resolution 小写、ratio 原样、media 透传。
func TestTehui_RequestBodyShape(t *testing.T) {
	req := tehuiReq(8, "720P")
	req.Ratio = "9:16"
	req.Media = []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "https://example.com/a.jpg"},
	}
	adaptor := &TaskAdaptor{}
	raw, err := adaptor.buildRequestBody(req)
	require.NoError(t, err)

	body := decodeBody(t, raw)
	require.Equal(t, "mw-sd2.5-flash", body["model"])
	require.Equal(t, "a cat", body["prompt"])
	require.EqualValues(t, 8, body["duration"], "duration 必须是整数秒")
	require.Equal(t, "720p", body["resolution"], "resolution 必须小写")
	require.Equal(t, "9:16", body["ratio"])
	require.NotNil(t, body["media"])
	assert.Nil(t, body["size"], "mw 不收 size")
}

// 计费：特惠 720P 倍率恒为 1.0（单档交付，不叠档位差价）。
func TestTehui_BillingRatioIsOne(t *testing.T) {
	require.Equal(t, 1.0, resolutionRatioForModel(tehuiModel, "720P"))
}

// 活动版（已下架）的映射保持历史口径，避免在途任务/回滚时被改坏。
func TestHuoDong_MappingUnchangedAfterDelist(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Model: constant.Seedance25HuoDongModel, Prompt: "x", Duration: 10}
	got, err := upstreamModelForRequest(req)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video", got)
}
