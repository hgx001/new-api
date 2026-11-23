package middleware

// 促销族常驻改派 + 自营短档（2026-11-23 老板口径）。
//
// 本文件锁六件事：
//  1. 带参考视频的请求**留在常驻**（客美 tj），计费原点仍是 seedance2.5-特惠；
//  2. 常驻改派把交付档钉成 720P（源链路对客恒定 720P），并把档位倍率覆盖成
//     源模型口径（否则客美 1.81 倍会把 ¥0.20/秒 乘成 ¥0.36/秒）；
//  3. 1080P / 21:9 / 目标无渠道 → 不改派（静默降档、静默改画幅都比「留在原链路」糟）；
//  4. >15 秒 → seedance-2-5（规则一 + seconds_trigger_seconds=15）；
//  5. ≤15 秒 → seedance-2-0-fast；开关打开且 9:16 → sd-seedance-2.0fast；
//  6. 自营档的边界（非 9:16 / 参考图 >4 张 / 带参考音视频 / 越界时长）→ 回落豆包 Fast。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyPromoPolicy 写一份「与生产同形」的策略：母本特惠走规则一/六 + 常驻改派，
// 活动版不在本文件用例范围内。
func applyPromoPolicy(t *testing.T, shortOwnEnabled bool, extraOverride map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()

	override := map[string]interface{}{
		"seconds_rule_enabled":    true,
		"seconds_trigger_seconds": 15,
		"short_rule_enabled":      true,
		"promo_resident_model":    seedanceroute_setting.DefaultPromoResidentModel,
		// 规则九（音视频 → CapCut）必须关掉：它在常驻之前判，开着会把
		// 带参考视频的请求截走，与「带参考视频留在常驻」直接冲突。
		"ref_av_rule_enabled":    false,
		"frames_rule_enabled":    false,
		"ratio_rule_enabled":     false,
		"ecommerce_rule_enabled": false,
	}
	for key, value := range extraOverride {
		override[key] = value
	}

	payload := map[string]interface{}{
		"enabled":                 true,
		"source_model":            "seedance2.5-特惠",
		"source_models":           []string{"seedance2.5-特惠"},
		"target_model":            "seedance-2-5",
		"trigger_seconds":         20,
		"short_trigger_seconds":   15,
		"short_fast_enabled":      true,
		"promo_short_own_enabled": shortOwnEnabled,
		"source_overrides": map[string]interface{}{
			"seedance2.5-特惠": override,
		},
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// TestPromoResidentRefVideoStaysOnResident 带参考视频 → 留常驻 tj。
func TestPromoResidentRefVideoStaysOnResident(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9","videos":["https://cdn.example.com/ref.mp4"]}`)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoResidentModel, decoded["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点必须是源模型")
	assert.Equal(t, float64(10), decoded["duration"], "时长要显式落成 duration（客美不读顶层 seconds）")
	assert.Equal(t, "720P", decoded["resolution"], "源链路对客恒定 720P，不能降成 480P")
	assert.Equal(t, "16:9", decoded["ratio"], "画幅原样保留")

	require.True(t, rec.hasBillingSize, "必须覆盖档位倍率")
	assert.InDelta(t, 1.0, rec.billingSizeRatio, 1e-9,
		"特惠 720P 倍率是 1.0（¥0.20/秒），不能被客美的 1.81 覆盖")
	assert.False(t, rec.hasBillingSec, "秒数交给客美 adaptor 算（生成 + 参考视频），不在这里钉死")
}

// TestPromoResidentSkips1080P 1080P 源链路本来会 400，不能改成静默 720P 交付。
func TestPromoResidentSkips1080P(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","resolution":"1080P","videos":["https://cdn.example.com/ref.mp4"]}`
	rec := runRouteFull(t, body)

	assert.Equal(t, body, rec.body, "1080P 请求必须原样放行，由源链路 fail-loud")
	assert.Empty(t, rec.originModel)
}

// TestPromoResidentSkipsUnsupportedRatio 21:9 客美会静默回落 16:9 → 不改派。
func TestPromoResidentSkipsUnsupportedRatio(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"21:9","videos":["https://cdn.example.com/ref.mp4"]}`
	rec := runRouteFull(t, body)

	assert.Equal(t, body, rec.body, "客美没有 21:9，改派过去等于替客户钉死画幅")
}

// TestPromoResidentNoChannelKeepsSource 目标没渠道时不改写（改写后重试不会退回源模型）。
func TestPromoResidentNoChannelKeepsSource(t *testing.T) {
	applyPromoPolicy(t, false, nil)
	stubChannelAvailable(t, false)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","videos":["https://cdn.example.com/ref.mp4"]}`
	got, origin := runRouteRaw(t, body)

	assert.Equal(t, body, got, "目标无渠道时必须原样放行")
	assert.Empty(t, origin)
}

// TestPromoShortOwnRoutes916ToOwn 开关打开 + 9:16 + ≤15 秒 → 自营 sd- 档。
func TestPromoShortOwnRoutes916ToOwn(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16"}`)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decoded["model"])
	assert.Equal(t, "9:16", decoded["ratio"])
	assert.Equal(t, "720P", decoded["resolution"], "自营档固定 720P")
	assert.Equal(t, "10", decoded["seconds"], "秒数必须显式落盘（自营 adaptor 不给 seconds）")
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点仍是源模型")

	require.True(t, rec.hasBillingSec, "必须记源模型口径的秒数，否则按 1 秒收费")
	assert.InDelta(t, 10.0, rec.billingSeconds, 1e-9)
	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 1.0, rec.billingSizeRatio, 1e-9)
}

// TestPromoShortOwnOffFallsToDoubaoFast 开关关着 → 同一个请求走豆包 Fast。
func TestPromoShortOwnOffFallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel)
}

// TestPromoShortOwnNon916FallsToDoubaoFast 非 9:16 不走自营档（老板口径）。
func TestPromoShortOwnNon916FallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"16:9 改派到固定 9:16 的自营档 = 静默改画幅")
}

// TestPromoShortOwnAccepts9Images 9 张参考图是自营档的上限（官网 Video 档口径），必须路由成功。
func TestPromoShortOwnAccepts9Images(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	images := make([]string, 0, 9)
	for i := 0; i < 9; i++ {
		images = append(images, fmt.Sprintf(`"https://a/%d.png"`, i))
	}
	body := fmt.Sprintf(`{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16","images":[%s]}`, strings.Join(images, ","))
	rec := runRouteFull(t, body)

	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decodeBody(t, rec.body)["model"],
		"9 张参考图在自营档上限内（2026-11-22 官网 Video 档真机取证），必须走自营档")
}

// TestPromoShortOwnTooManyImagesFallsToDoubaoFast 参考图 >9 张时自营档接不住。
func TestPromoShortOwnTooManyImagesFallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	images := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		images = append(images, fmt.Sprintf(`"https://a/%d.png"`, i))
	}
	body := fmt.Sprintf(`{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16","images":[%s]}`, strings.Join(images, ","))
	rec := runRouteFull(t, body)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"10 张参考图超过自营档上限 9 张，必须回落豆包 Fast")
}

// TestPromoLongSecondsGoesDoubao25 >15 秒 → seedance-2-5（规则一 + 下界 15）。
func TestPromoLongSecondsGoesDoubao25(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"20","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-5", decodeBody(t, rec.body)["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel)
	require.True(t, rec.hasBillingSec)
	assert.InDelta(t, 20.0, rec.billingSeconds, 1e-9, "计费秒数按客户请求的 20 秒")
}

// TestPromoExactly15SecondsGoesShort 15 秒（含）算短档 —— 老板口径「>15 才算长」。
func TestPromoExactly15SecondsGoesShort(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"15 秒必须走短档：老板口径是「大于 15 秒」才路由到 seedance-2-5")
}
