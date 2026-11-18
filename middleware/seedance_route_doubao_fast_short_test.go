package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/stretchr/testify/require"
)

// 规则十：seedance-2-5 的 ≤15 秒请求 → seedance-2-0-fast（2026-11-18）。
//
// 与规则六（促销族短时长）分开守：
//  1. 15 秒（含边界）改派，计费仍按 seedance-2-5 一口价（屏蔽目标倍率）；
//  2. 16 秒不改派，留在 seedance-2-5；
//  3. 4 秒改派（目标链路硬下界），3 秒不改派；
//  4. 未显式传时长不改派；
//  5. 带参考视频不改派（豆包 Fast 只收参考图）；
//  6. 开关关闭时不改派；
//  7. 阈值可配（配 10 时 11 秒不改派）。

const testDoubaoFastShortModel = "seedance-2-0-fast"

// enableDoubaoFastShort 只打开规则十，其它规则保持关闭。
//
// 故意不给 source_model / source_models：这条规则的源模型是常量
// seedance-2-5，不依赖促销族的源清单 —— 同时验证「只开这一条，
// 中间件也不会被 IsAnyRouteEnabled 提前短路」。
func enableDoubaoFastShort(t *testing.T, extra map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":                   true,
		"doubao_fast_short_enabled": true,
	}
	for key, value := range extra {
		payload[key] = value
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	require.True(t, seedanceroute_setting.IsDoubaoFastShortEnabled(),
		"前置：规则十开关必须生效（策略被判成没配全时会被整体关掉）")
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 15 秒是边界，必须改派；计费保留一口价口径（不写秒数/倍率，改为屏蔽）。
func TestDoubaoFastShortRoutesAtFifteenSeconds(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":15,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testDoubaoFastShortModel, body["model"], "15 秒（含）应改派豆包 Fast")
	require.Equal(t, "seedance-2-5", rec.originModel, "计费原点必须是客户请求的 seedance-2-5")
	require.True(t, rec.suppressTaskRatios, "一口价商品必须屏蔽目标 adaptor 的按秒倍率")
	require.False(t, rec.hasBillingSec, "一口价不写计费秒数（写了会乘歪成 ¥3.50×N）")
	require.False(t, rec.hasBillingSize, "一口价不写 size 倍率")
}

// 16 秒超出上界，留在 seedance-2-5。
func TestDoubaoFastShortSkipsAboveFifteenSeconds(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":16,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, "seedance-2-5", body["model"], "16 秒不属于短时长，不能改派")
	require.False(t, rec.suppressTaskRatios)
}

// 4 秒是目标链路（seedance-2-0-fast）的硬下界，必须改派。
func TestDoubaoFastShortRoutesAtMinSeconds(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":4,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, testDoubaoFastShortModel, body["model"], "4 秒应改派豆包 Fast")
}

// 3 秒低于目标链路下界，改派过去会被上游拒，必须留在源链路。
func TestDoubaoFastShortSkipsBelowMinSeconds(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":3,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, "seedance-2-5", body["model"], "3 秒目标链路接不住，不能改派")
}

// 没传时长：不替客户按缺省 5 秒做「短任务」判断。
func TestDoubaoFastShortSkipsWithoutExplicitSeconds(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, "seedance-2-5", body["model"], "未显式传时长时不改派")
}

// 带参考视频：豆包 Fast 只收参考图，改写过去会丢素材。
func TestDoubaoFastShortSkipsReferenceVideo(t *testing.T) {
	enableDoubaoFastShort(t, nil)

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":12,"media":[{"type":"reference_video","url":"https://cdn.example/v.mp4"}],"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, "seedance-2-5", body["model"], "带参考视频的请求不能改派豆包 Fast")
}

// 开关关闭（默认）：即使时长在区间内也不改派。
func TestDoubaoFastShortDisabledByDefault(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	// 只打开总开关，不打开规则十。
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(
		`{"enabled":true,"total_fallback_enabled":false,"failover_enabled":false,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":20}`))

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, "seedance-2-5", body["model"], "规则十关闭时不得改派")
}

// 阈值可配：配 10 秒时 11 秒不命中、10 秒命中。
func TestDoubaoFastShortTriggerIsConfigurable(t *testing.T) {
	enableDoubaoFastShort(t, map[string]interface{}{
		"doubao_fast_short_trigger_seconds": 10,
	})

	rec := runRouteFull(t, `{"model":"seedance-2-5","seconds":11,"prompt":"a cat"}`)
	require.Equal(t, "seedance-2-5", decodeBody(t, rec.body)["model"], "11 秒超过配置的 10 秒上界")

	rec = runRouteFull(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`)
	require.Equal(t, testDoubaoFastShortModel, decodeBody(t, rec.body)["model"], "10 秒命中配置的上界")
}
