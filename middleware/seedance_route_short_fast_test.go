package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/stretchr/testify/require"
)

// 规则六第四分支：短时长 / 非 720P → 豆包 Fast（2026-11-18）。
//
// 背景：特惠母本的常驻上游换成 SJ-wan3-prime（仅 15–30 秒、仅 720P）。
// 为了让对外仍是 5–30 秒，≤14 秒的 720P 与非 720P 档位必须改派
// seedance-2-0-fast（豆包官网，4–30 秒、480P/720P/1080P）。
//
// 本文件守四件事：
//  1. 10 秒 720P → 改派 Fast，计费按源模型（10 秒 × 1.0）；
//  2. 15 秒 720P → **不**改派（留给 SJ 的 15–30 秒区间）；
//  3. 1080P 的 20 秒 → 改派 Fast（SJ 只支持 720P），size 倍率按源模型 1.5；
//  4. 带参考视频 → 不改派（豆包官网只收参考图，传过去会被静默忽略）。

const testShortFastModel = "seedance-2-0-fast"

// enableShortFast 配置规则六：只开豆包 Fast 分支，阈值 14 秒。
func enableShortFast(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_model":          testSourceModel,
		"short_trigger_seconds": seedanceroute_setting.ShortFastMaxSeconds,
		"short_link_enabled":    false,
		"short_youzan_enabled":  false,
		"short_fast_enabled":    true,
		"short_capcut_enabled":  false,
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 10 秒 720P → 改派豆包 Fast，计费按源模型 10 秒 × 1.0。
func TestShortFastRoutesUnderThreshold(t *testing.T) {
	enableShortFast(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testShortFastModel, body["model"], "10 秒 720P 应改派豆包 Fast")
	require.Equal(t, testSourceModel, rec.originModel, "计费原点必须是源模型")
	require.True(t, rec.hasBillingSec, "必须记录计费秒数")
	require.Equal(t, float64(10), rec.billingSeconds)
	require.True(t, rec.hasBillingSize, "720P 也要记 size 倍率（源模型口径）")
	require.Equal(t, 1.0, rec.billingSizeRatio)
}

// 15 秒 720P → 不改派，留给常驻 SJ-wan3-prime。
func TestShortFastSkipsAtFifteenSeconds(t *testing.T) {
	enableShortFast(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testSourceModel, body["model"], "15 秒属于 SJ 区间，不能改派 Fast")
}

// 1080P 的 20 秒 → 改派 Fast（SJ 只支持 720P），倍率按源模型 1.5。
func TestShortFastRoutesNon720PAnyDuration(t *testing.T) {
	enableShortFast(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":20,"resolution":"1080P","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testShortFastModel, body["model"], "1080P 任意时长都应改派 Fast")
	require.True(t, rec.hasBillingSec)
	require.Equal(t, float64(20), rec.billingSeconds)
	require.True(t, rec.hasBillingSize)
	require.Equal(t, 1.5, rec.billingSizeRatio, "1080P 按源模型倍率 1.5 计费")
}

// 带参考视频 → 不改派（豆包官网只收参考图）。
func TestShortFastSkipsReferenceVideo(t *testing.T) {
	enableShortFast(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":8,"media":[{"type":"reference_video","url":"https://cdn.example/v.mp4"}],"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	// 规则九（音视频参考 → CapCut）优先级在规则六之前，会先把这类请求接走；
	// 无论落到规则九还是留在源链路，都**不能**是豆包 Fast（官网只收参考图）。
	require.NotEqual(t, testShortFastModel, body["model"], "带参考视频的请求不能改派豆包 Fast")
}

// Fast 与有赞分支同时开：有赞优先（读侧优先级兜底）。
func TestShortFastLosesToYouzanWhenBothOn(t *testing.T) {
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_model":          testSourceModel,
		"short_trigger_seconds": seedanceroute_setting.ShortFastMaxSeconds,
		"short_link_enabled":    false,
		"short_youzan_enabled":  true,
		"short_fast_enabled":    true,
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testYouzanStandardModel, body["model"],
		"有赞分支优先于 Fast（两者互斥，读侧按优先级兜底）")
}
