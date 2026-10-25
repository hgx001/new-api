package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── Picsart 免费无限档 Wan 3.0（wan-3.0-特惠，2026-10-24 上架） ──
//
// 商品口径（老板 2026-10-24 定稿）：固定 30 秒、分辨率 480P/720P 可调、
// 比例六档可选、按次 ¥3.00/条。链路是**一级建单平台**（platformId=picsart），
// 不再靠「豆包建单 + Worker 路由」那条老路。
//
// 与 MegaAI 渠道的 `wan-3.0` 是两个不同商品（不同渠道、不同上游、不同计价形态），
// 名字只差后缀，不要合并或互相别名化。

// 路由必须落到 picsart 平台 + picsart_video kind，且公开名翻译成官网档位 id。
func TestPicsartUnlimitedRoutesToPicsart(t *testing.T) {
	spec, ok := SpecFor(ModelWan3TeHui)
	require.True(t, ok, "wan-3.0-特惠 必须在路由表里")
	assert.Equal(t, PlatformIDPicsart, spec.PlatformID)
	assert.Equal(t, OutputModeVideo, spec.OutputMode)
	assert.Equal(t, kindPicsartVideo, spec.Kind)
	assert.Equal(t, "wan-3.0-video", spec.UpstreamModel,
		"发往 ArcReel/Worker 的必须是 Picsart 官网档位 id，不是公开名")
}

// 改名前的公开名 `wan-3.0-无限` 必须**仍然可调用**（同样的平台/kind/上游档位），
// 只是不上模型广场（白名单见 model/pricing.go 的 isAllowedPricingModel）。
//
// 为什么要专门锁这条：改名时很容易顺手把 modelSpecs 里的旧条目一起删掉，
// 那会让 24 小时内已按旧名接入的调用直接 400 —— 商务损失不可逆，
// 而多一个 map 条目的成本几乎为零。清理条件写在 ModelWan3TeHuiLegacy 的注释里。
func TestWan3TeHuiLegacyAliasStillRoutes(t *testing.T) {
	legacy, ok := SpecFor(ModelWan3TeHuiLegacy)
	require.True(t, ok, "改名前的公开名必须仍在路由表里（只保可调用，不上广场）")
	fresh, ok := SpecFor(ModelWan3TeHui)
	require.True(t, ok)
	assert.Equal(t, fresh, legacy, "旧名与新名必须是同一条 spec，否则两档口径会漂移")
	assert.Equal(t, "wan-3.0-video", legacy.UpstreamModel)
}

// 固定 30 秒：传 10 秒必须 400，不能静默改成 30（客户会以为拿到 10 秒）。
func TestPicsartUnlimitedRejectsNonThirtySeconds(t *testing.T) {
	for _, body := range []string{
		`{"model":"wan-3.0-特惠","prompt":"x","duration":10}`,
		`{"model":"wan-3.0-特惠","prompt":"x","seconds":15}`,
		`{"model":"wan-3.0-特惠","prompt":"x","duration":"5"}`,
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
		`{"model":"wan-3.0-特惠","prompt":"x","duration":30}`,
		`{"model":"wan-3.0-特惠","prompt":"x","seconds":30}`,
		`{"model":"wan-3.0-特惠","prompt":"x"}`,
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
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","resolution":%q}`, raw))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), raw)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, strings.ToUpper(raw), req.Resolution, raw)
	}

	for _, raw := range []string{"1080P", "4k", "720"} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","resolution":%q}`, raw))
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, raw)
		assert.Equal(t, "invalid_resolution", taskErr.Code, raw)
	}

	// 缺省 720P：官网默认 1080P 不开放，不下发就会出客户没买的档。
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x"}`)
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
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","ratio":%q}`, raw))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), raw)
		req, err := getNormalizedRequest(c)
		require.NoError(t, err)
		assert.Equal(t, want, req.Ratio, raw)
	}

	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x","ratio":"21:9"}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "Wan 3.0 官网没有 21:9，必须拒")
	assert.Equal(t, "invalid_ratio", taskErr.Code)

	// 未指定 → 空串（Worker 保持官网 adaptive 默认档，不硬塞 16:9）。
	c, info, a = postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "", req.Ratio)
}

// 参考视频不收、参考图上限 10。
func TestPicsartUnlimitedReferenceConstraints(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x","videos":["https://a.example/v.mp4"]}`)
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "picsart 路线不吃参考视频")
	assert.Equal(t, "invalid_input_reference", taskErr.Code)

	eleven := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		eleven = append(eleven, fmt.Sprintf(`"https://a.example/%d.png"`, i))
	}
	c, info, a = postVideoCtx(t, fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","images":[%s]}`, strings.Join(eleven, ",")))
	taskErr = a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "超过 10 张必须拒")
	assert.Equal(t, "invalid_input_reference", taskErr.Code)
}

// 建单载荷：platformId=picsart、model 是官网真名、30 秒、分辨率归一。
func TestPicsartUnlimitedBuildsOfficialPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{
		"model":"wan-3.0-特惠","prompt":"海边日落","duration":30,
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

// 分分辨率定价：720P 是基准价（返回 nil = 不乘），480P 返回 size=0.75。
//
// 单模型名承载两档价，靠的就是这条倍率 —— relay_task.go 用
// applyTaskOtherRatios 把预扣额乘一次，而按次任务在
// service/task_polling.go:767 跳过差额结算，所以预扣额就是最终扣费。
// 若这里误返回 nil，480P 会按 720P 的 ¥2.00 收（多收 33%）。
func TestPicsartUnlimitedBillingByResolution(t *testing.T) {
	// 720P = 基准价，不返回倍率（applyTaskOtherRatios 在 multiplier==1 时原样返回）。
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x","resolution":"720P"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info), "720P 是基准价，不该再乘")

	// 缺省分辨率在 ArcReel 侧落 720P，同样按基准价。
	c, info, a = postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info), "缺省即 720P，不该再乘")

	// 480P 打 75 折。
	c, info, a = postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x","resolution":"480p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, map[string]float64{"size": 0.75}, a.EstimateBilling(c, info),
		"480P 必须返回 size=0.75，否则按 720P 价多收 33%")
}

// 倍率与价目的对应关系必须成立：480P ¥1.50 / 720P ¥2.00 → 0.75。
//
// ModelPrice 存在数据库里，测试读不到，所以这里把「价目 → 倍率」的算式钉死：
// 谁改了一边没改另一边，这个用例立刻红（这正是 2026-10-13 错价事故的防线思路）。
func TestPicsartUnlimited480PRatioMatchesPriceList(t *testing.T) {
	const (
		wan3Unlimited480PPriceCNY = 1.50 // ModelPrice 里的 720P 基价 × 本倍率
		wan3Unlimited720PPriceCNY = 2.00 // ModelPrice['wan-3.0-特惠'] 的对外价
	)
	assert.Equal(t, wan3Unlimited480PPriceCNY/wan3Unlimited720PPriceCNY, picsart480PSizeRatio,
		"picsart480PSizeRatio 必须等于 480P 价 / 720P 价；改价时两处要一起改")

	// 真算一遍额度，确认落地到「元」是 1.50 / 2.00（QuotaPerUnit 口径与生产一致）。
	baseQuota := int(wan3Unlimited720PPriceCNY / 7.3 * common.QuotaPerUnit)
	discountedQuota := int(float64(baseQuota) * picsart480PSizeRatio)
	assert.InDelta(t, 2.00, float64(baseQuota)/common.QuotaPerUnit*7.3, 0.005)
	assert.InDelta(t, 1.50, float64(discountedQuota)/common.QuotaPerUnit*7.3, 0.005)
}
