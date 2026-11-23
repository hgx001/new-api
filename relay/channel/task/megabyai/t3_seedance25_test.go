package megabyai

// ════════════════════════════════════════════════════════════════════════════
// T3-seedance-2.5（2026-11-23 重新接入 MegaAI 时上架）的能力契约
// ════════════════════════════════════════════════════════════════════════════
//
// 上游目录原文：「30图10视频10音频，480p/720p，4-30秒，不卡脸，支持全比例」，
// 按次计费、分辨率分档（上游 480p ¥3 / 720p ¥4；本站对外 480p ¥4 / 720p ¥5）。
//
// 本文件把这些边界变成可执行断言，重点守三件会直接伤到钱或客户预期的事：
//
//  1. **没有 1080p 档** —— 客户传 1080p 必须本地 400。静默当 720p 发上去，
//     客户拿到的清晰度低于他要求的；静默当 1080p 计费则是我们不存在的档位。
//  2. **时长 4–30 秒** —— 越界必须 400，不能截断成 4/30 秒（客户看不出成片为什么短了）。
//  3. **素材上限 30/10/10** —— 比 wan-3.0 的 10/5/5 高；沿用旧上限会把合法请求拒掉。

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 能力表本身：档位、倍率、素材上限、时长区间。
func TestT3Capabilities(t *testing.T) {
	caps := capabilityFor(T3Seedance25Model)

	assert.Equal(t, []string{"480p", "720p"}, caps.Resolutions,
		"T3 只有 480p / 720p 两档，1080p 必须走不了")
	assert.Equal(t, 1.0, caps.SizeRatios["480p"], "480p 是基准档")
	assert.InDelta(t, 1.25, caps.SizeRatios["720p"], 1e-9,
		"720p 相对 480p 的倍率 = 5.0/4.0（对外价 ¥5 / ¥4）")
	assert.Equal(t, 30, caps.MaxImages)
	assert.Equal(t, 10, caps.MaxVideos)
	assert.Equal(t, 10, caps.MaxAudios)
	assert.Equal(t, 4, caps.MinSeconds)
	assert.Equal(t, 30, caps.MaxSeconds)
}

// wan-3.0 的能力不能被 T3 的改动带偏（回归）。
func TestWan30CapabilitiesUnchanged(t *testing.T) {
	caps := capabilityFor(wan30Model)

	assert.Equal(t, wan30Resolutions, caps.Resolutions, "wan-3.0 仍是三档（含 1080p）")
	assert.InDelta(t, wan30ResolutionSizeRatio["720p"], caps.SizeRatios["720p"], 1e-9)
	assert.Equal(t, 10, caps.MaxImages)
	assert.Equal(t, 5, caps.MaxVideos)
	assert.Equal(t, 5, caps.MaxAudios)
	assert.Equal(t, 0, caps.MinSeconds, "wan-3.0 不做时长区间校验（时长不参与计价）")
}

// 未知模型回落到 wan-3.0 的默认能力表（不能被当成「无能力 = 全拒」）。
func TestUnknownModelFallsBackToWan30Caps(t *testing.T) {
	caps := capabilityFor("some-passthrough-model")
	assert.Equal(t, wan30Resolutions, caps.Resolutions)
	assert.Equal(t, wan30MaxReferenceImages, caps.MaxImages)
}

// 分辨率：480p/720p 通过，1080p 显式拒绝（不静默降档）。
func TestT3ResolutionWhitelist(t *testing.T) {
	caps := capabilityFor(T3Seedance25Model)

	for _, ok := range []string{"480p", "720p", "720P", "480"} {
		resolution, err := resolveMegabyaiResolution(relaycommon.TaskSubmitReq{Resolution: ok}, caps)
		require.NoError(t, err, "分辨率 %q 应通过", ok)
		assert.Contains(t, caps.Resolutions, resolution)
	}

	_, err := resolveMegabyaiResolution(relaycommon.TaskSubmitReq{Resolution: "1080p"}, caps)
	require.Error(t, err, "T3 没有 1080p 档，必须显式 400 而不是静默降到 720p")
	assert.Contains(t, err.Error(), "1080p")
	assert.Contains(t, err.Error(), "480p", "错误文案要告诉客户能用哪几档")
}

// 未指定分辨率 → 显式下发 480p 基准档（保证收的价与下发的档是同一档）。
func TestT3DefaultResolutionIsBaseTier(t *testing.T) {
	resolution, err := resolveMegabyaiResolution(relaycommon.TaskSubmitReq{}, capabilityFor(T3Seedance25Model))
	require.NoError(t, err)
	assert.Equal(t, "480p", resolution)
}

// 时长 4–30 秒：未给 → 不下发（按次计费，时长不参与算钱）；
// 给了越界值 → 400，不截断。
func TestT3DurationRange(t *testing.T) {
	caps := capabilityFor(T3Seedance25Model)

	for _, seconds := range []int{4, 10, 30} {
		duration, err := resolveMegabyaiDuration(relaycommon.TaskSubmitReq{Duration: seconds}, caps)
		require.NoError(t, err, "%d 秒应在 4–30 区间内", seconds)
		require.NotNil(t, duration)
		assert.Equal(t, seconds, *duration)
	}

	for _, seconds := range []int{1, 3, 31, 60} {
		_, err := resolveMegabyaiDuration(relaycommon.TaskSubmitReq{Duration: seconds}, caps)
		require.Error(t, err, "%d 秒越界，必须 400", seconds)
		assert.Contains(t, err.Error(), "4-30")
	}

	duration, err := resolveMegabyaiDuration(relaycommon.TaskSubmitReq{}, caps)
	require.NoError(t, err)
	assert.Nil(t, duration, "没给时长就不下发，让上游用它自己的默认值")
}

// 素材上限 30/10/10：30 张图、10 个视频、10 段音频全部通过；超一个就拒。
func TestT3MediaLimits(t *testing.T) {
	caps := capabilityFor(T3Seedance25Model)

	build := func(n int, suffix string) relaycommon.TaskSubmitReq {
		items := make([]relaycommon.TaskMedia, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, relaycommon.TaskMedia{
				Type: mediaTypeFor(suffix),
				URL:  "https://a/" + suffix + itoaTest(i) + ".bin",
			})
		}
		return relaycommon.TaskSubmitReq{Media: items}
	}

	images, videos, audios, err := buildMegabyaiMedia(build(30, "img"), caps)
	require.NoError(t, err, "30 张参考图是 T3 的上限，必须通过")
	assert.Len(t, images, 30)
	assert.Empty(t, videos)
	assert.Empty(t, audios)

	_, _, _, err = buildMegabyaiMedia(build(31, "img"), caps)
	require.Error(t, err, "31 张参考图超出 T3 上限，必须 400")
	assert.Contains(t, err.Error(), "30")

	_, videosOut, _, err := buildMegabyaiMedia(build(10, "vid"), caps)
	require.NoError(t, err, "10 个参考视频是 T3 的上限")
	assert.Len(t, videosOut, 10)

	_, _, _, err = buildMegabyaiMedia(build(11, "vid"), caps)
	require.Error(t, err, "11 个参考视频超出 T3 上限，必须 400")
}

// 同样是 11 张图，wan-3.0 必须拒（旧上限 10）—— 证明上限是按模型区分的，
// 不是全局放宽。
func TestWan30KeepsStricterMediaLimits(t *testing.T) {
	items := make([]relaycommon.TaskMedia, 0, 11)
	for i := 0; i < 11; i++ {
		items = append(items, relaycommon.TaskMedia{
			Type: "reference_image",
			URL:  "https://a/" + itoaTest(i) + ".png",
		})
	}

	_, _, _, err := buildMegabyaiMedia(relaycommon.TaskSubmitReq{Media: items}, capabilityFor(wan30Model))
	require.Error(t, err, "wan-3.0 的参考图上限仍是 10，11 张必须拒")
	assert.Contains(t, err.Error(), "10")
}

// 计费：按分辨率档给 size 倍率，且只给 size（不能带 seconds，否则按秒多扣）。
func TestT3EstimateBillingUsesResolutionRatio(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: T3Seedance25Model}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(wan30ResolutionContextKey, "720p")
	got := adaptor.EstimateBilling(c, info)
	require.Len(t, got, 1, "只能带 size 倍率（按次计费，时长不参与）")
	assert.InDelta(t, 1.25, got["size"], 1e-9)

	c480, _ := gin.CreateTestContext(httptest.NewRecorder())
	c480.Set(wan30ResolutionContextKey, "480p")
	got480 := adaptor.EstimateBilling(c480, info)
	assert.InDelta(t, 1.0, got480["size"], 1e-9)
}

func mediaTypeFor(suffix string) string {
	switch suffix {
	case "img":
		return "reference_image"
	case "vid":
		return "reference_video"
	default:
		return "reference_audio"
	}
}

func itoaTest(v int) string {
	if v == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for v > 0 {
		digits = append(digits, byte('0'+v%10))
		v /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}
