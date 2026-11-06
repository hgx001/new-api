package manwu

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────────────────────────────────────────────────────
// 豆包系图片模型（2026-11-07 上架）
//
// 用户口径：「命名就按照官网的命名用，定价两毛一张」：
//   - 豆包官网「图像生成」默认模型 → 公开名 `Seedream 5.0 Flash`
//   - dola 官网 /chat/create-image 默认模型 → 公开名 `Seedream 4.5`
//
// 关键约束（都有对应的锁）：
//   ① 公开名**就是官网显示名**，不做本地化改写；
//   ② 两条链路是**两个独立平台**（doubao / dola），互不通用；
//   ③ 价格 ¥0.20/张，且**不得**把既有的 Nano Banana Pro（¥0.30/张）一并降价；
//   ④ 图片比例「未指定不编造」：官网出厂档是「自动」，不补 16:9。
// ──────────────────────────────────────────────────────────────────────────

func TestSeedreamModelNamesFollowOfficialDisplayNames(t *testing.T) {
	// 公开名 = 官网显示名，一个字都不能改。
	assert.Equal(t, "Seedream 5.0 Flash", ModelSeedream50Flash)
	assert.Equal(t, "Seedream 4.5", ModelSeedream45)
}

func TestSeedreamRoutesToDistinctPlatforms(t *testing.T) {
	flash, ok := SpecFor(ModelSeedream50Flash)
	require.True(t, ok, "Seedream 5.0 Flash 必须可路由")
	assert.Equal(t, PlatformIDDoubao, flash.PlatformID, "Seedream 5.0 Flash 走豆包官网")
	assert.Equal(t, OutputModeImage, flash.OutputMode)
	assert.Equal(t, kindSeedreamImage, flash.Kind)

	seedream45, ok := SpecFor(ModelSeedream45)
	require.True(t, ok, "Seedream 4.5 必须可路由")
	assert.Equal(t, PlatformID, seedream45.PlatformID, "Seedream 4.5 走 dola 官网")
	assert.Equal(t, OutputModeImage, seedream45.OutputMode)
	assert.Equal(t, kindSeedreamImage, seedream45.Kind)
}

// 上游模型名（发给 ArcReel 的官网档位 id）必须与 Worker 侧白名单一致，
// 否则 Worker 归一时会回落默认档 —— 用户点了 4.5 却拿到别的模型。
func TestSeedreamUpstreamModelNames(t *testing.T) {
	flash, _ := SpecFor(ModelSeedream50Flash)
	assert.Equal(t, "doubao-seedream-5-0-flash", flash.UpstreamModel)

	seedream45, _ := SpecFor(ModelSeedream45)
	assert.Equal(t, "dola-seedream-4-5", seedream45.UpstreamModel)
}

// 模型必须出现在渠道模型列表里（客户要能在 new-api 看到并调用）。
func TestSeedreamAppearsInModelList(t *testing.T) {
	a := &TaskAdaptor{}
	names := a.GetModelList()
	assert.Contains(t, names, ModelSeedream50Flash)
	assert.Contains(t, names, ModelSeedream45)
}

// 图片任务不得带视频专属字段；比例白名单是**图片专用**八档
// （自动 / 9:16 / 2:3 / 3:4 / 1:1 / 4:3 / 3:2 / 16:9）。
//
// ⚠️ 图片档位与视频档位**不同**：图片多了 2:3 / 3:2、少了 21:9。
// 用视频白名单校验图片比例会误杀 2:3（Worker 侧
// src/platforms/doubao/params.ts 的 DOUBAO_IMAGE_ASPECT_RATIOS 同口径）。
func TestSeedreamImageAcceptsImageSpecificRatios(t *testing.T) {
	for _, ratio := range []string{"自动", "9:16", "2:3", "3:4", "1:1", "4:3", "3:2", "16:9"} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":%q,"prompt":"一只猫","ratio":%q}`, ModelSeedream50Flash, ratio))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), "比例 %s 必须放行", ratio)

		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, ratio, req.Ratio, "比例 %s 必须原样透传", ratio)
	}
}

// 「自动」是官网文案；英文 auto 也应被接受（第三方客户端常用英文）。
func TestSeedreamImageAcceptsAutoAlias(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 4.5","prompt":"一只猫","ratio":"auto"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "自动", req.Ratio)
}

// 视频档位的 21:9 在图片侧**不存在** → 显式 400，不静默回落。
func TestSeedreamImageRejectsVideoOnlyRatio(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"一只猫","ratio":"21:9"}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_ratio", taskErr.Code)
}

// 比例「未指定不编造」：官网图片模式出厂比例是「自动」，
// 用户没选时我们**不该**偷偷补成 16:9（那是视频侧的缺省口径）。
func TestSeedreamImageUnspecifiedRatioStaysEmpty(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"一只猫"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "", req.Ratio, "未指定比例时必须留空，不能补 16:9")

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	ip, ok := payload["imageParams"].(map[string]any)
	require.True(t, ok)
	_, hasRatio := ip["aspectRatio"]
	assert.False(t, hasRatio, "未指定比例时不得下发 aspectRatio 键")
}

// 建单载荷：platformId / outputMode / imageParams.imageModel 都要对。
func TestSeedreamImageBuildsCorrectPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"一只猫","ratio":"2:3","n":2}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)

	assert.Equal(t, PlatformIDDoubao, payload["platformId"])
	assert.Equal(t, OutputModeImage, payload["outputMode"])
	_, hasVideoParams := payload["videoParams"]
	assert.False(t, hasVideoParams, "图片任务不得带 videoParams")

	ip, ok := payload["imageParams"].(map[string]any)
	require.True(t, ok, "图片任务必须走 imageParams")
	assert.Equal(t, "doubao-seedream-5-0-flash", ip["imageModel"],
		"imageModel 必须是官网档位 id（Worker 侧白名单按它归一）")
	assert.Equal(t, "2:3", ip["aspectRatio"])
	assert.Equal(t, float64(2), ip["count"])
}

// dola 侧同构，但 platformId 与上游模型名都不同。
func TestSeedream45BuildsDolaPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 4.5","prompt":"一只猫"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)

	assert.Equal(t, PlatformID, payload["platformId"])
	assert.Equal(t, OutputModeImage, payload["outputMode"])

	ip, ok := payload["imageParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "dola-seedream-4-5", ip["imageModel"])
}

// 按张计费：张数作为 n 倍率乘到 ¥0.2/张 上（与 Nano Banana Pro 同机制）。
func TestSeedreamImageCountDrivesBillingMultiplier(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"猫","n":3}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, map[string]float64{"n": 3}, a.EstimateBilling(c, info))

	// 张数为 1 时不注入倍率（避免无意义的 quota 乘 1 误差）。
	c2, info2, a2 := postVideoCtx(t, `{"model":"Seedream 4.5","prompt":"猫","n":1}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	assert.Nil(t, a2.EstimateBilling(c2, info2))
}

// 张数上限与 ArcReel `_remote_image_count` 的 clamp 一致（1..10）。
func TestSeedreamImageRejectsIllegalRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{"missing prompt", `{"model":"Seedream 5.0 Flash"}`, "invalid_request"},
		{"count above cap", `{"model":"Seedream 5.0 Flash","prompt":"x","n":11}`, "invalid_count"},
		{"count below one", `{"model":"Seedream 5.0 Flash","prompt":"x","n":0}`, "invalid_count"},
		{"non integer count", `{"model":"Seedream 5.0 Flash","prompt":"x","n":"many"}`, "invalid_count"},
		{"bad ratio", `{"model":"Seedream 5.0 Flash","prompt":"x","ratio":"21:10"}`, "invalid_ratio"},
		{"video seconds on image", `{"model":"Seedream 5.0 Flash","prompt":"x","seconds":5}`, "invalid_request"},
		{"resolution on image", `{"model":"Seedream 5.0 Flash","prompt":"x","resolution":"2k"}`, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, info, a := postVideoCtx(t, tc.body)
			taskErr := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr, tc.name)
			assert.Equal(t, tc.code, taskErr.Code)
		})
	}
}

// ──────────────────────────────────────────────────────────────────────────
// 张数上限必须贴合官网真实能力（2026-11-07 真机取证）
//
// 豆包「图像生成」与 dola /chat/create-image 的图片工具栏里**都没有张数控件**：
//   - 豆包：actionbar 只有 model / style，另有「比例 自动」按钮；
//     全页文本扫「张 / 数量 / count」零命中（CDP 9230 实测）；
//   - dola：真机全页扫描「张/数量/count」零命中。
// 官网一次固定出 **4 张**，出图张数只能由上游截断，不能由参数控制。
//
// ⚠️ 为什么不沿用 kindImage 的 1..10：
// 上限放 10 而官网只出 4 张 → 用户付 10 张的钱、只拿到 4 张图，
// 账单与交付错配。所以 Seedream 图片这条链路的张数上限单独收紧到 4。
// Nano Banana Pro（kindImage）不受影响 —— 那是另一条链路、另一个能力集。
// ──────────────────────────────────────────────────────────────────────────
func TestSeedreamImageCountCapMatchesOfficialCapacity(t *testing.T) {
	assert.Equal(t, 4, maxSeedreamImageCount,
		"官网图片固定出 4 张，张数上限必须收敛到 4，否则计费与交付错配")

	// 4 张必须放行。
	c, info, a := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"猫","n":4}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 4, req.Count)

	// 5 张必须拒：官网给不出 5 张，收了钱也交不出来。
	c2, info2, a2 := postVideoCtx(t, `{"model":"Seedream 5.0 Flash","prompt":"猫","n":5}`)
	taskErr := a2.ValidateRequestAndSetAction(c2, info2)
	require.NotNil(t, taskErr, "n=5 超出官网能力，必须在建单前拒绝")
	assert.Equal(t, "invalid_count", taskErr.Code)
}

// dola 侧同构：同一个上限，不能只有豆包收紧。
func TestSeedream45CountCapMatchesOfficialCapacity(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Seedream 4.5","prompt":"猫","n":5}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_count", taskErr.Code)
}

// Nano Banana Pro（kindImage）**不受**本次收紧影响：那是另一条链路，
// 走 ArcReel 的即梦/通用图片能力，张数上限仍是 10。
func TestNanoBananaImageCountCapUnchanged(t *testing.T) {
	assert.Equal(t, 10, maxImageCount, "既有图片链路张数上限不得被 Seedream 收紧牵连")
	assert.NotEqual(t, maxImageCount, maxSeedreamImageCount,
		"两条图片链路的上限必须各自独立")
}

// 两个模型是**独立商品**：同一个公开名不能被两个平台共用，
// 也不能让一个平台的请求静默走到另一个平台。
func TestSeedreamModelsAreNotAliased(t *testing.T) {
	flash, ok := SpecFor(ModelSeedream50Flash)
	require.True(t, ok)
	seedream45, ok := SpecFor(ModelSeedream45)
	require.True(t, ok)

	assert.NotEqual(t, flash.PlatformID, seedream45.PlatformID)
	assert.NotEqual(t, flash.UpstreamModel, seedream45.UpstreamModel)
}
