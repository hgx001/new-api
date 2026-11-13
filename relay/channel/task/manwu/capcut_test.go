package manwu

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapCut（剪映国际版）路由与计价契约（2026-11-13 接入，同日扩到四档）。
//
// 口径：
//   - 公开名 cp-seedance-2.5 / -2.0 / -2.0fast / -2.0mini
//     → 官网档位真名 seedance_2.5 / seedance_2.0 / seedance_2.0_fast / seedance_2.0_mini；
//   - 平台 platform_id = capcut（一级建单，不走豆包路由）；
//   - 时长：2.5 = 4–30 秒，其余 4–15 秒，整数秒原样透传、不吸附；
//   - 分辨率 480p / 720p（1080p / 4K 未定价 → 显式 400，不静默降级）；
//   - 参考图 ≤9 张（超出显式 400，不静默截断）；
//   - 计价：四档**统一按秒**（seconds 倍率），720p 再加 size 倍率。

func TestCapcutModelSpecsRouteToCapcutPlatform(t *testing.T) {
	cases := map[string]string{
		ModelCapcut25:     capcutUpstreamModel25,
		ModelCapcut20:     capcutUpstreamModel20,
		ModelCapcut20Fast: capcutUpstreamModel20Fast,
		ModelCapcut20Mini: capcutUpstreamModel20Mini,
	}
	for model, upstream := range cases {
		spec, ok := SpecFor(model)
		require.True(t, ok, model)
		assert.Equal(t, PlatformIDCapcut, spec.PlatformID, model)
		assert.Equal(t, OutputModeVideo, spec.OutputMode, model)
		assert.Equal(t, kindCapcutVideo, spec.Kind, model)
		assert.Equal(t, upstream, spec.UpstreamModel, model)
	}
}

// 与豆包 `seedance-2-0-fast` 同名族但**不互为别名**：豆包公开名不能路由到 CapCut。
func TestCapcutPublicNamesDoNotCollideWithDoubao(t *testing.T) {
	doubao, ok := SpecFor(ModelDoubao20Fast)
	require.True(t, ok)
	assert.Equal(t, PlatformIDDoubao, doubao.PlatformID)
	assert.NotEqual(t, PlatformIDCapcut, doubao.PlatformID)

	_, ok = SpecFor("seedance_2.0_fast")
	assert.False(t, ok, "官网真名不是对外公开名，不能被直接调用")
}

func TestModelListContainsCapcutModels(t *testing.T) {
	names := ModelList()
	for _, model := range []string{
		ModelCapcut25, ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini,
	} {
		assert.Contains(t, names, model)
	}
}

func TestValidateCapcutDurationRange(t *testing.T) {
	for _, v := range []int{capcutMinDuration, 10, capcutMaxDuration} {
		got, err := validateCapcutDuration("seconds", v, ModelCapcut20)
		require.NoError(t, err)
		assert.Equal(t, v, got)
	}
	for _, v := range []int{3, 16, 30, 0} {
		_, err := validateCapcutDuration("seconds", v, ModelCapcut20)
		require.Error(t, err, "越界时长必须显式失败")
	}
}

// 2.5 的上界放宽到 30 秒（真机滑杆 max=30000ms），其余模型仍锁 15 秒。
func TestValidateCapcutDurationUpperBoundDependsOnModel(t *testing.T) {
	got, err := validateCapcutDuration("seconds", 30, ModelCapcut25)
	require.NoError(t, err)
	assert.Equal(t, 30, got)

	_, err = validateCapcutDuration("seconds", 31, ModelCapcut25)
	require.Error(t, err)

	for _, model := range []string{ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini} {
		_, err := validateCapcutDuration("seconds", 30, model)
		require.Error(t, err, "%s 不能到 30 秒", model)
	}
}

func TestResolveCapcutDurationFallsBackToPanelDefault(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, capcutDefaultDuration, req.Duration)

	c2, info2, a2 := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":12}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	req2, err := getNormalizedRequest(c2)
	require.NoError(t, err)
	assert.Equal(t, 12, req2.Duration)

	c3, info3, a3 := postVideoCtx(t, `{"model":"cp-seedance-2.5","prompt":"hi","seconds":24}`)
	require.Nil(t, a3.ValidateRequestAndSetAction(c3, info3))
	req3, err := getNormalizedRequest(c3)
	require.NoError(t, err)
	assert.Equal(t, 24, req3.Duration)
}

// 越界时长在入口就 400（不建单、不扣费）。
func TestCapcutRejectsOutOfRangeDuration(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":30}`)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 分辨率只卖 480p / 720p：1080p、4K 显式拒绝，不静默降级。
func TestCapcutRejectsUnpricedResolution(t *testing.T) {
	for _, resolution := range []string{"1080p", "4k", "360p"} {
		c, info, a := postVideoCtx(t,
			`{"model":"cp-seedance-2.0","prompt":"hi","resolution":"`+resolution+`"}`)
		err := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, err, resolution)
	}
}

// 720p 是已定价档位，必须放行并原样落到 videoParams。
func TestCapcutAccepts720p(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "720p", req.Resolution)
}

// 参考图第 10 张显式拒绝（不静默截断到 9 张）。
func TestCapcutRejectsTenReferenceImages(t *testing.T) {
	body := `{"model":"cp-seedance-2.0","prompt":"hi","input_reference":["https://a/1.jpg","https://a/2.jpg","https://a/3.jpg","https://a/4.jpg","https://a/5.jpg","https://a/6.jpg","https://a/7.jpg","https://a/8.jpg","https://a/9.jpg","https://a/10.jpg"]}`
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 计价：四档全部按秒（漏一档 = 只收 1 秒的钱）。
func TestCapcutBillingIsPerSecondForAllModels(t *testing.T) {
	for _, model := range []string{
		ModelCapcut25, ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini,
	} {
		c, info, a := postVideoCtx(t,
			`{"model":"`+model+`","prompt":"hi","seconds":8}`)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), model)
		ratios := a.EstimateBilling(c, info)
		require.NotNil(t, ratios, model)
		assert.Equal(t, float64(8), ratios["seconds"], model)
		_, hasSize := ratios["size"]
		assert.False(t, hasSize, "%s 480p 不应有 size 倍率", model)
	}
}

// 720p 的 size 倍率：mini / fast = 1.5，2.0 / 2.5 = 2。
func TestCapcut720pSizeRatio(t *testing.T) {
	cases := map[string]float64{
		ModelCapcut20Mini: 1.5,
		ModelCapcut20Fast: 1.5,
		ModelCapcut20:     2.0,
		ModelCapcut25:     2.0,
	}
	for model, want := range cases {
		c, info, a := postVideoCtx(t,
			`{"model":"`+model+`","prompt":"hi","seconds":5,"resolution":"720p"}`)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), model)
		ratios := a.EstimateBilling(c, info)
		require.NotNil(t, ratios, model)
		assert.Equal(t, float64(5), ratios["seconds"], model)
		assert.Equal(t, want, ratios["size"], model)
	}
}

// 建单载荷：model 传官网档位真名，时长/比例/分辨率透传（默认 480p 显式下发）。
func TestCapcutBuildRequestBodyUsesUpstreamModelKey(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0fast","prompt":"hi","seconds":9,"ratio":"9:16"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capcutUpstreamModel20Fast, vp["model"])
	assert.Equal(t, float64(9), vp["duration"])
	assert.Equal(t, "9:16", vp["ratio"])
	assert.Equal(t, "480p", vp["resolution"], "缺省分辨率显式下发 480p（价格基准档）")
}

func TestCapcutBuildRequestBodyForMini720p(t *testing.T) {
	c, info, a := postVideoCtx(t,
		`{"model":"cp-seedance-2.0mini","prompt":"hi","seconds":4,"ratio":"9:16","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capcutUpstreamModel20Mini, vp["model"])
	assert.Equal(t, float64(4), vp["duration"])
	assert.Equal(t, "720p", vp["resolution"])
}

// 平台常量必须与 Worker 侧 RemotePlatformId / ArcReel 侧 platform_id 一致。
func TestCapcutPlatformIDConstant(t *testing.T) {
	assert.Equal(t, "capcut", PlatformIDCapcut)
	assert.Equal(t, constant.ChannelTypeManwu, constant.ChannelTypeManwu)
}
