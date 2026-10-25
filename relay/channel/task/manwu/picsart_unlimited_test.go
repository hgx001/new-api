package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Picsart 免费无限档 Wan 3.0（wan-3.0-无限，2026-10-24 上架） ──
//
// 商品口径（老板 2026-10-24 定稿）：固定 30 秒、分辨率 480P/720P 可调、
// 比例六档可选、按次 ¥3.00/条。链路是**一级建单平台**（platformId=picsart），
// 不再靠「豆包建单 + Worker 路由」那条老路。
//
// 与 MegaAI 渠道的 `wan-3.0` 是两个不同商品（不同渠道、不同上游、不同计价形态），
// 名字只差后缀，不要合并或互相别名化。

// 路由必须落到 picsart 平台 + picsart_video kind，且公开名翻译成官网档位 id。
func TestPicsartUnlimitedRoutesToPicsart(t *testing.T) {
	spec, ok := SpecFor(ModelWan3Unlimited)
	require.True(t, ok, "wan-3.0-无限 必须在路由表里")
	assert.Equal(t, PlatformIDPicsart, spec.PlatformID)
	assert.Equal(t, OutputModeVideo, spec.OutputMode)
	assert.Equal(t, kindPicsartVideo, spec.Kind)
	assert.Equal(t, "wan-3.0-video", spec.UpstreamModel,
		"发往 ArcReel/Worker 的必须是 Picsart 官网档位 id，不是公开名")
}

// 固定 30 秒：传 10 秒必须 400，不能静默改成 30（客户会以为拿到 10 秒）。
func TestPicsartUnlimitedRejectsNonThirtySeconds(t *testing.T) {
	for _, body := range []string{
		`{"model":"wan-3.0-无限","prompt":"x","duration":10}`,
		`{"model":"wan-3.0-无限","prompt":"x","seconds":15}`,
		`{"model":"wan-3.0-无限","prompt":"x","duration":"5"}`,
	} {
		c, info, a := postVideoCtx(t, body)
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, body)
		assert.Equal(t, "invalid_duration", taskErr.Code, body)
	}
}

// 30 秒或缺省都通过；缺省补 30。
func TestPicsartUnlimitedAcceptsThirtyOrOmitted(t *testing.T) {
	for _, body := range []string{
		`{"model":"wan-3.0-无限","prompt":"x","duration":30}`,
		`{"model":"wan-3.0-无限","prompt":"x","seconds":30}`,
		`{"model":"wan-3.0-无限","prompt":"x"}`,
	} {
		c, info, a := postVideoCtx(t, body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), body)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, 30, req.Duration, body)
	}
}

// 分辨率只开 480P/720P；1080P 与其他值 400；缺省落 720P。
func TestPicsartUnlimitedResolutionWhitelist(t *testing.T) {
	for _, raw := range []string{"480P", "480p", "720P", "720p"} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-无限","prompt":"x","resolution":%q}`, raw))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), raw)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(raw), req.Resolution, raw)
	}

	for _, raw := range []string{"1080P", "4k", "720"} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-无限","prompt":"x","resolution":%q}`, raw))
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, raw)
		assert.Equal(t, "invalid_resolution", taskErr.Code, raw)
	}

	// 缺省 720P：官网默认 1080P 不开放，不下发就会出客户没买的档。
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "720P", req.Resolution)
}

// 比例六档（含 adaptive）；21:9 不在 Wan 3.0 档位内必须 400；缺省不编造。
func TestPicsartUnlimitedRatioWhitelist(t *testing.T) {
	cases := map[string]string{
		"16:9": "16:9", "16/9": "16:9", "16：9": "16:9",
		"9:16": "9:16", "1:1": "1:1", "4:3": "4:3", "3:4": "3:4",
		"adaptive": "adaptive", "auto": "adaptive", "自适应": "adaptive",
	}
	for raw, want := range cases {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-无限","prompt":"x","ratio":%q}`, raw))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), raw)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, want, req.Ratio, raw)
	}

	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x","ratio":"21:9"}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "Wan 3.0 官网没有 21:9，必须拒")
	assert.Equal(t, "invalid_ratio", taskErr.Code)

	// 未指定 → 空串（Worker 保持官网 adaptive 默认档，不硬塞 16:9）。
	c, info, a = postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "", req.Ratio)
}

// 参考视频不收、参考图上限 10。
func TestPicsartUnlimitedReferenceConstraints(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x","videos":["https://a.example/v.mp4"]}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "picsart 路线不吃参考视频")
	assert.Equal(t, "invalid_input_reference", taskErr.Code)

	eleven := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		eleven = append(eleven, fmt.Sprintf(`"https://a.example/%d.png"`, i))
	}
	c, info, a = postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-无限","prompt":"x","images":[%s]}`, strings.Join(eleven, ",")))
	taskErr = a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "超过 10 张必须拒")
	assert.Equal(t, "invalid_input_reference", taskErr.Code)
}

// 建单载荷：platformId=picsart、model 是官网真名、30 秒、分辨率归一。
func TestPicsartUnlimitedBuildsOfficialPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{
		"model":"wan-3.0-无限","prompt":"海边日落","duration":30,
		"resolution":"480p","ratio":"9:16",
		"input_reference":["https://a.example/1.png"],
		"idempotency_key":"key-picsart"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, PlatformIDPicsart, payload["platformId"])
	assert.Equal(t, OutputModeVideo, payload["outputMode"])
	assert.Equal(t, "key-picsart", payload["idempotencyKey"])

	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "wan-3.0-video", vp["model"], "不能把公开名发给 ArcReel")
	assert.Equal(t, float64(30), vp["duration"])
	assert.Equal(t, "480P", vp["resolution"])
	assert.Equal(t, "9:16", vp["ratio"])

	inputs, ok := payload["inputs"].([]any)
	require.True(t, ok)
	require.Len(t, inputs, 1)
	assert.Equal(t, InputTypeImage, inputs[0].(map[string]any)["type"])
}

// 按次计费：EstimateBilling 必须返回 nil，否则 relay_task 会把 ¥3 再乘时长 → ¥90。
func TestPicsartUnlimitedBillingIsPerCall(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x","duration":30,"resolution":"720P"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info),
		"按次模型返回倍率会被再乘一遍（¥3 × 30 秒 = ¥90），必须 nil")
}

// ── 分分辨率定价公开名（2026-10-25 老板定稿：480p ¥1.50/次、720p ¥2.00/次） ──
//
// new-api 只能按模型名计价（ModelPrice 按名索引，relay/relay_task.go:181 的
// ModelPriceHelperPerCall 读 OriginModelName），单名字承载不了两个价格，
// 因此拆成 `-480p` / `-720p` 两个公开名，**同走 picsart 上游**。
// 分辨率必须由名字锁死：否则调用方传 `model=...-480p` + `resolution=720P`
// 就能花 480p 的钱拿 720p 的片。

// 两个分档名必须与单档名同一条上游（picsart / wan-3.0-video），只是多了锁分辨率。
func TestPicsartTieredNamesShareUpstream(t *testing.T) {
	for _, name := range []string{ModelWan3Unlimited480P, ModelWan3Unlimited720P} {
		spec, ok := SpecFor(name)
		require.True(t, ok, "%s 必须在路由表里", name)
		assert.Equal(t, PlatformIDPicsart, spec.PlatformID, name)
		assert.Equal(t, OutputModeVideo, spec.OutputMode, name)
		assert.Equal(t, kindPicsartVideo, spec.Kind, name)
		assert.Equal(t, "wan-3.0-video", spec.UpstreamModel, name,
			"分档名只是计价壳，发往 ArcReel/Worker 的仍是官网档位 id")
		assert.NotEmpty(t, spec.ForcedResolution, name, "分档名必须锁分辨率")
	}
	assert.Equal(t, "480P", mustSpec(t, ModelWan3Unlimited480P).ForcedResolution)
	assert.Equal(t, "720P", mustSpec(t, ModelWan3Unlimited720P).ForcedResolution)
}

func mustSpec(t *testing.T, name string) modelSpec {
	t.Helper()
	spec, ok := SpecFor(name)
	require.True(t, ok, name)
	return spec
}

// 锁分辨率：缺省按名字补档；显式传别的档 → 400（不静默改档）。
func TestPicsartTieredNamesLockResolution(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		wantErr     bool
		wantResolut string
	}{
		// 缺省 → 按名字补，不能落到默认 720P（否则 480p 名白拿 720p 片）。
		{ModelWan3Unlimited480P, `{"model":"wan-3.0-无限-480p","prompt":"x"}`, false, "480P"},
		{ModelWan3Unlimited720P, `{"model":"wan-3.0-无限-720p","prompt":"x"}`, false, "720P"},
		// 显式传同一档（大小写不敏感）→ 放行。
		{ModelWan3Unlimited480P, `{"model":"wan-3.0-无限-480p","prompt":"x","resolution":"480p"}`, false, "480P"},
		{ModelWan3Unlimited720P, `{"model":"wan-3.0-无限-720p","prompt":"x","resolution":"720P"}`, false, "720P"},
		// 显式传别的档 → 400。这是计价漏洞：名字说 480p、身体要 720p。
		{ModelWan3Unlimited480P, `{"model":"wan-3.0-无限-480p","prompt":"x","resolution":"720P"}`, true, ""},
		{ModelWan3Unlimited720P, `{"model":"wan-3.0-无限-720p","prompt":"x","resolution":"480P"}`, true, ""},
		// 非白名单值照旧 400。
		{ModelWan3Unlimited480P, `{"model":"wan-3.0-无限-480p","prompt":"x","resolution":"1080P"}`, true, ""},
	}
	for _, tc := range cases {
		c, info, a := postVideoCtx(t, tc.body)
		taskErr := a.ValidateRequestAndSetAction(c, info)
		if tc.wantErr {
			require.NotNil(t, taskErr, tc.body)
			assert.Equal(t, "invalid_resolution", taskErr.Code, tc.body)
			continue
		}
		require.Nil(t, taskErr, tc.body)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, tc.wantResolut, req.Resolution, tc.body)
		assert.Equal(t, 30, req.Duration, tc.body, "分档名不改时长口径，仍固定 30 秒")
	}
}

// 单档名 `wan-3.0-无限` 行为不变：分辨率仍随请求体（回归保护）。
func TestPicsartUnlimitedNameStillHonorsBodyResolution(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-无限","prompt":"x","resolution":"480p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "480P", req.Resolution, "单档名没有 ForcedResolution，不能被锁")

	spec, ok := SpecFor(ModelWan3Unlimited)
	require.True(t, ok)
	assert.Empty(t, spec.ForcedResolution, "单档名不该锁分辨率")
}

// 建单载荷：分档名的分辨率必须落到 videoParams，model 仍是官网真名。
func TestPicsartTieredNamesBuildOfficialPayload(t *testing.T) {
	for _, tc := range []struct{ name, wantResolution string }{
		{ModelWan3Unlimited480P, "480P"},
		{ModelWan3Unlimited720P, "720P"},
	} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":%q,"prompt":"x"}`, tc.name))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), tc.name)
		reader, err := a.BuildRequestBody(c, info)
		require.NoError(t, err, tc.name)
		data, err := io.ReadAll(reader)
		require.NoError(t, err)

		var payload map[string]any
		require.NoError(t, json.Unmarshal(data, &payload))
		assert.Equal(t, PlatformIDPicsart, payload["platformId"], tc.name)
		vp, ok := payload["videoParams"].(map[string]any)
		require.True(t, ok, tc.name)
		assert.Equal(t, "wan-3.0-video", vp["model"], tc.name)
		assert.Equal(t, tc.wantResolution, vp["resolution"], tc.name)
		assert.Equal(t, float64(30), vp["duration"], tc.name)
	}
}

// 按次计费不变：分档名也不能返回倍率（否则 ¥1.5 会被再乘 30 秒）。
func TestPicsartTieredNamesBillingIsPerCall(t *testing.T) {
	for _, name := range []string{ModelWan3Unlimited480P, ModelWan3Unlimited720P} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":%q,"prompt":"x"}`, name))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), name)
		assert.Nil(t, a.EstimateBilling(c, info), name,
			"按次模型返回倍率会被 relay_task 再乘一遍，必须 nil")
	}
}
