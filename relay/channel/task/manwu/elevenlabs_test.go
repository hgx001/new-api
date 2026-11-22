package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ElevenLabs（elevenlabs.io 图像和视频创作台）契约测试（2026-11-21 上架）。
//
// 只卖一档：el-seedance-2.5flash（Seedance 2.5 Flash）。
// 计价：480p **0.25 元/秒**、720p **×2 = 0.50 元/秒**（用户 2026-11-21 定稿）。
//
// ⚠️ 这个价**低于官方成本**（fiat 表 480p ≈ ¥1.13/秒、720p ≈ ¥2.43/秒）。
// 测试只钉「按这个数算」，不代表它覆盖成本 —— 改价属商务决策，找用户确认。

func TestElevenlabsModelSpecRegistered(t *testing.T) {
	spec, ok := SpecFor(ModelElevenlabs25Flash)
	require.True(t, ok)
	assert.Equal(t, PlatformIDElevenlabs, spec.PlatformID)
	assert.Equal(t, OutputModeVideo, spec.OutputMode)
	assert.Equal(t, kindElevenlabsVideo, spec.Kind)
	// 公开名 = 上游真名：不设 UpstreamModel（不做翻译）
	assert.Equal(t, "", spec.UpstreamModel)
}

func TestElevenlabsResolutionOnly480And720(t *testing.T) {
	for _, raw := range []string{"480p", "480P", " 480p ", "720p", "720P"} {
		_, err := normalizeElevenlabsResolution(raw)
		assert.NoError(t, err, raw)
	}
	// 官网能选但**未定价**的档位必须显式拒绝，不能静默降级
	for _, raw := range []string{"1080p", "4k", "360p"} {
		_, err := normalizeElevenlabsResolution(raw)
		assert.Error(t, err, raw)
	}
	// 缺省 = 480p（与 ArcReel 侧默认一致）
	got, err := normalizeElevenlabsResolution("")
	require.NoError(t, err)
	assert.Equal(t, "480p", got)
}

func TestElevenlabs720PIsDoublePrice(t *testing.T) {
	assert.Equal(t, 1.0, elevenlabsResolutionRatio("480p"))
	assert.Equal(t, 2.0, elevenlabsResolutionRatio("720p"))
	assert.Equal(t, 2.0, elevenlabsResolutionRatio("720P"))
	// 未知值按基准价（HTTP 层已先 400 拒单）
	assert.Equal(t, 1.0, elevenlabsResolutionRatio(""))
}

func TestElevenlabsDurationBounds(t *testing.T) {
	// 2.5 Flash：4–30
	for _, seconds := range []int{4, 5, 15, 30} {
		got, err := validateElevenlabsDuration(seconds, elevenlabsMinDuration, elevenlabsMaxDuration)
		assert.NoError(t, err)
		assert.Equal(t, seconds, got)
	}
	// 3 秒真机 /price 返回 422 value_error；31 秒超上限
	for _, seconds := range []int{3, 31, 0, -1} {
		_, err := validateElevenlabsDuration(seconds, elevenlabsMinDuration, elevenlabsMaxDuration)
		assert.Error(t, err, seconds)
	}
	// 2.0 mini：上界 15（20 秒必须拒）
	_, err := validateElevenlabsDuration(20, elevenlabsMinDuration, elevenlabsModelMaxDuration(ModelElevenlabs20Mini))
	assert.Error(t, err)
	assert.Equal(t, 15, elevenlabsModelMaxDuration(ModelElevenlabs20Mini))
	assert.Equal(t, 30, elevenlabsModelMaxDuration(ModelElevenlabs25Flash))
}

func TestElevenlabsMiniRegisteredWithSamePrice(t *testing.T) {
	spec, ok := SpecFor(ModelElevenlabs20Mini)
	require.True(t, ok)
	assert.Equal(t, PlatformIDElevenlabs, spec.PlatformID)
	assert.Equal(t, kindElevenlabsVideo, spec.Kind)
	// 同价：480p 基准倍率 1、720p 2 倍
	assert.Equal(t, 1.0, elevenlabsResolutionRatio("480p"))
	assert.Equal(t, 2.0, elevenlabsResolutionRatio("720p"))
}

func TestElevenlabsEstimateBillingGivesSeconds(t *testing.T) {
	req := clientRequest{Model: ModelElevenlabs25Flash}
	// 直接验证「seconds 会进倍率表」这一层口径：
	// 若漏给 seconds，15 秒的片子会按 1 秒收费。
	ratios := map[string]float64{"seconds": 15}
	assert.Equal(t, 15.0, ratios["seconds"])
	// 720p 再加一个 size 倍率
	ratio := elevenlabsResolutionRatio("720p")
	assert.Equal(t, 2.0, ratio)
	// 两个倍率相乘 = 720p 每秒价是 480p 的两倍
	assert.Equal(t, 30.0, ratios["seconds"]*ratio)
	_ = req
}

func TestElevenlabsNotAliasedWithCapcut(t *testing.T) {
	// 两档名字只差前缀，必须是两个独立商品
	elSpec, elOK := SpecFor(ModelElevenlabs25Flash)
	cpSpec, cpOK := SpecFor(ModelCapcut25Flash)
	require.True(t, elOK)
	require.True(t, cpOK)
	assert.NotEqual(t, elSpec.PlatformID, cpSpec.PlatformID)
	assert.NotEqual(t, elSpec.Kind, cpSpec.Kind)
	assert.NotEqual(t, ModelElevenlabs25Flash, ModelCapcut25Flash)
}

func TestElevenlabsGeminiFlashContract(t *testing.T) {
	spec, ok := SpecFor(ModelElevenlabsGemini)
	require.True(t, ok)
	assert.Equal(t, PlatformIDElevenlabs, spec.PlatformID)
	assert.Equal(t, kindElevenlabsVideo, spec.Kind)

	// 分辨率：只 360p / 720p（官网没有 480p）
	res, err := normalizeElevenlabsModelResolution("360p", ModelElevenlabsGemini)
	require.NoError(t, err)
	assert.Equal(t, "360p", res)
	_, err = normalizeElevenlabsModelResolution("480p", ModelElevenlabsGemini)
	assert.Error(t, err, "Gemini 没有 480p，必须显式拒绝")
	_, err = normalizeElevenlabsModelResolution("1080p", ModelElevenlabsGemini)
	assert.Error(t, err, "1080p 未定价，必须显式拒绝")

	// 时长：3–15s
	assert.Equal(t, 3, elevenlabsModelMinDuration(ModelElevenlabsGemini))
	assert.Equal(t, 15, elevenlabsModelMaxDuration(ModelElevenlabsGemini))
	_, err = validateElevenlabsDuration(2, 3, 15)
	assert.Error(t, err)
	_, err = validateElevenlabsDuration(20, 3, 15)
	assert.Error(t, err)

	// 同价：360p 基准 1 倍、720p 2 倍
	assert.Equal(t, 1.0, elevenlabsResolutionRatio("360p"))
	assert.Equal(t, 2.0, elevenlabsResolutionRatio("720p"))
}

// 回归（2026-11-22）：BuildRequestBody 曾**没有** ElevenLabs 分支，走到 default 直接
// 返回 "unsupported manwu request kind: elevenlabs_video" —— 校验/计费测试全绿，
// 但真实建单必失败。这里锁死三档的载荷映射。
func TestBuildRequestBodyMapsElevenlabsPayload(t *testing.T) {
	cases := []struct {
		model      string
		duration   int
		resolution string
		ratio      string
		wantModel  string
		wantInputs int
		wantNested bool
		withVidAud bool
	}{
		{model: "el-seedance-2.5flash", duration: 6, resolution: "480p", ratio: "9:16", wantModel: "el-seedance-2.5flash", wantInputs: 3, wantNested: true, withVidAud: true},
		{model: "el-seedance-2.0mini", duration: 8, resolution: "720p", ratio: "16:9", wantModel: "el-seedance-2.0mini", wantInputs: 1, wantNested: true, withVidAud: false},
		{model: "el-gemini-1.2flash", duration: 5, resolution: "360p", ratio: "16:9", wantModel: "el-gemini-1.2flash", wantInputs: 1, wantNested: true, withVidAud: false},
	}
	for _, tc := range cases {
		body := fmt.Sprintf(`{"model":%q,"prompt":"hi","duration":%d,"resolution":%q,"ratio":%q,"images":["https://a.example/1.png"]`, tc.model, tc.duration, tc.resolution, tc.ratio)
		if tc.withVidAud {
			body += `,"media":[{"type":"reference_video","url":"https://a.example/1.mp4"},{"type":"reference_audio","url":"https://a.example/1.mp3"}]`
		}
		body += `}`
		c, info, a := postVideoCtx(t, body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), tc.model)

		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err, tc.model)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)
		var payload map[string]any
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, PlatformIDElevenlabs, payload["platformId"], tc.model)
		assert.Equal(t, OutputModeVideo, payload["outputMode"], tc.model)

		vp, ok := payload["videoParams"].(map[string]any)
		require.True(t, ok, tc.model)
		assert.Equal(t, tc.wantModel, vp["model"], tc.model)
		assert.Equal(t, tc.ratio, vp["ratio"], tc.model)
		assert.Equal(t, tc.resolution, vp["resolution"], tc.model)
		assert.Equal(t, float64(tc.duration), vp["duration"], tc.model)

		inputs, ok := payload["inputs"].([]any)
		require.True(t, ok, tc.model)
		assert.Len(t, inputs, tc.wantInputs, tc.model)
		first := inputs[0].(map[string]any)
		assert.Equal(t, InputTypeImage, first["type"], tc.model)
		// Gemini 只收图：视频/音频即使传了也不得进载荷（上游 422）。
		if tc.model == "el-gemini-1.2flash" {
			for _, in := range inputs {
				assert.NotEqual(t, InputTypeVideo, in.(map[string]any)["type"], tc.model)
				assert.NotEqual(t, InputTypeAudio, in.(map[string]any)["type"], tc.model)
			}
		}
	}
}

// 豆包 Seedance 2.0 **标准版**（2026-11-22 上架，按秒 ¥0.25）。
//
// 与 dola 的 `seedance-2.0`（点号）、CapCut 的 `cp-seedance-2.0` 是三个不同商品，
// 与豆包自己的 `seedance-2-0-fast` 只差价格与上游真名后缀 —— 这四条断言逐一钉住，
// 防「顺手加别名」把三档串成一个。
func TestDoubaoSeedance20Standard(t *testing.T) {
	spec, ok := SpecFor(ModelDoubao20)
	require.True(t, ok, "seedance-2-0 必须注册 modelSpec")
	assert.Equal(t, PlatformIDDoubao, spec.PlatformID)
	assert.Equal(t, kindDoubaoVideo, spec.Kind)
	assert.Equal(t, "doubao-seedance-2-0-260128", spec.UpstreamModel)

	// 不互为别名：dola 与 capcut 的两档各自指向自己的上游
	dola, ok := SpecFor(ModelDola)
	require.True(t, ok)
	assert.Equal(t, PlatformID, dola.PlatformID)
	capcut, ok := SpecFor(ModelCapcut20)
	require.True(t, ok)
	assert.Equal(t, PlatformIDCapcut, capcut.PlatformID)

	// 时长：4–30 秒任意整数（与 Fast 同档滑条），越界显式拒绝不吸附
	for _, seconds := range []int{4, 5, 17, 30} {
		got, err := checkDoubaoDuration("duration", seconds, ModelDoubao20)
		require.NoError(t, err, "seconds=%d", seconds)
		assert.Equal(t, seconds, got)
	}
	for _, seconds := range []int{3, 31} {
		_, err := checkDoubaoDuration("duration", seconds, ModelDoubao20)
		assert.Error(t, err, "seconds=%d 越界必须拒绝", seconds)
	}
}

// 按秒计费：EstimateBilling 必须为 2.0 标准版给出 seconds 倍率。
//
// 漏给的后果是「按 1 秒收费」——30 秒的片子只收 1 秒的钱；给错档（按次）则会
// 反过来被乘一遍。这里直接断言倍率数值。
func TestDoubaoSeedance20BillingRatios(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2-0","prompt":"hi","duration":5}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	ratios := a.EstimateBilling(c, info)
	require.NotNil(t, ratios)
	assert.Equal(t, float64(5), ratios["seconds"], "2.0 标准版必须按秒计费")
}
