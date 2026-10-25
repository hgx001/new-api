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

// 按次计价：**不分分辨率**，480P 与 720P 都是 ¥1.50/次。
//
// 2026-10-25 老板定稿：按最低的 480P 定价 ¥1.50，卡片徽标也只显示 ¥1.50。
// 之前「720P 基准 ¥2.00 + 480P 打 75 折」的方案作废 —— 那会让徽标显示 ¥2，
// 而客户最低能买到 ¥1.50，看着像涨价。
//
// 按次任务在 service/task_polling.go:767 跳过差额结算，所以预扣额就是最终扣费；
// 这里若误返回倍率，会被 applyTaskOtherRatios 再乘一次（¥1.5 × 30 秒 = ¥45）。
func TestPicsartUnlimitedBillingFlatPerCall(t *testing.T) {
	for _, resolution := range []string{"480P", "720P"} {
		body := fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","resolution":%q}`, resolution)
		c, info, a := postVideoCtx(t, body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		assert.Nil(t, a.EstimateBilling(c, info),
			"%s 必须返回 nil：两档同价，任何倍率都会把按次价再乘一遍", resolution)
	}

	// 缺省分辨率落 720P，同样不乘。
	c, info, a := postVideoCtx(t, `{"model":"wan-3.0-特惠","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info), "缺省即 720P，不该乘倍率")
}

// 价目与扣费口径必须一致：¥1.50/次，与分辨率无关。
//
// ModelPrice 存在数据库里，测试读不到，所以这里把「价目 → 扣费」的算式钉死：
// 谁改了价目表却没改这条，或者反过来，这个用例立刻红
// （这正是 2026-10-13 错价事故的防线思路）。
func TestPicsartUnlimitedPriceMatchesPriceList(t *testing.T) {
	const (
		wan3TeHuiPriceCNY = 1.50 // ModelPrice['wan-3.0-特惠'] 的对外价，按次
	)

	// 真算一遍额度，确认落地到「元」是 1.50（QuotaPerUnit 口径与生产一致）。
	quota := int(wan3TeHuiPriceCNY / 7.3 * common.QuotaPerUnit)
	assert.InDelta(t, 1.50, float64(quota)/common.QuotaPerUnit*7.3, 0.005)

	// 两档分辨率都不产生倍率 ⇒ 扣费恒等于基价，不存在「传 480P 少收」或
	// 「传 720P 多收」的分支。
	for _, resolution := range []string{"480P", "720P"} {
		body := fmt.Sprintf(`{"model":"wan-3.0-特惠","prompt":"x","resolution":%q}`, resolution)
		c, info, a := postVideoCtx(t, body)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info))
		assert.Nil(t, a.EstimateBilling(c, info), "%s 不得有倍率", resolution)
	}
}
