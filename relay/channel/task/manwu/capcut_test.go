package manwu

import (
	"encoding/json"
	"io"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapCut（剪映国际版）路由与计价契约（2026-11-13 接入）。
//
// 口径：
//   - 公开名 cp-seedance-2.0 / cp-seedance-2.0fast → 官网档位真名 seedance_2.0 / seedance_2.0_fast；
//   - 平台 platform_id = capcut（一级建单，不走豆包路由）；
//   - 时长 4–15 秒任意整数秒（原样透传，不吸附）；
//   - 分辨率只卖 480p（其它档显式 400，不静默降级）；
//   - 参考图 ≤9 张（超出显式 400，不静默截断）；
//   - 计价：2.0 **按次**（EstimateBilling 返回 nil）、2.0 Fast **按秒**（seconds 倍率）。

func TestCapcutModelSpecsRouteToCapcutPlatform(t *testing.T) {
	spec, ok := SpecFor(ModelCapcut20)
	require.True(t, ok)
	assert.Equal(t, PlatformIDCapcut, spec.PlatformID)
	assert.Equal(t, OutputModeVideo, spec.OutputMode)
	assert.Equal(t, kindCapcutVideo, spec.Kind)
	assert.Equal(t, capcutUpstreamModel20, spec.UpstreamModel)

	fast, ok := SpecFor(ModelCapcut20Fast)
	require.True(t, ok)
	assert.Equal(t, PlatformIDCapcut, fast.PlatformID)
	assert.Equal(t, kindCapcutVideo, fast.Kind)
	assert.Equal(t, capcutUpstreamModel20Fast, fast.UpstreamModel)
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
	assert.Contains(t, names, ModelCapcut20)
	assert.Contains(t, names, ModelCapcut20Fast)
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
}

// 越界时长在入口就 400（不建单、不扣费）。
func TestCapcutRejectsOutOfRangeDuration(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":30}`)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 只卖 480p：其它分辨率显式拒绝，不静默降级。
func TestCapcutRejectsNon480pResolution(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","resolution":"720p"}`)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 参考图第 10 张显式拒绝（不静默截断到 9 张）。
func TestCapcutRejectsTenReferenceImages(t *testing.T) {
	body := `{"model":"cp-seedance-2.0","prompt":"hi","input_reference":["https://a/1.jpg","https://a/2.jpg","https://a/3.jpg","https://a/4.jpg","https://a/5.jpg","https://a/6.jpg","https://a/7.jpg","https://a/8.jpg","https://a/9.jpg","https://a/10.jpg"]}`
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 计价：2.0 按次（nil），2.0 Fast 按秒（seconds 倍率）。
func TestCapcutBillingModes(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":8}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info), "按次模型不能返回倍率，否则 relay_task 会再乘一遍")

	c2, info2, a2 := postVideoCtx(t, `{"model":"cp-seedance-2.0fast","prompt":"hi","seconds":8}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	ratios := a2.EstimateBilling(c2, info2)
	require.NotNil(t, ratios)
	assert.Equal(t, float64(8), ratios["seconds"])
}

// 建单载荷：model 传官网档位真名，时长/比例透传。
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
	_, hasResolution := vp["resolution"]
	assert.False(t, hasResolution, "未传分辨率不应下发键")
}

// 平台常量必须与 Worker 侧 RemotePlatformId / ArcReel 侧 platform_id 一致。
func TestCapcutPlatformIDConstant(t *testing.T) {
	assert.Equal(t, "capcut", PlatformIDCapcut)
	assert.Equal(t, constant.ChannelTypeManwu, constant.ChannelTypeManwu)
}
