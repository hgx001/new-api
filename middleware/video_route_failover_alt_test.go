package middleware

// 规则五「备选备胎」（豆包不可用 → CapCut 480p + 超分 720P）的测试。
//
// 判定矩阵与规则五主链路同构，多了三格：
//
//	1. 备选开关关闭 → 仍走班车（升级当天行为必须逐字节不变）；
//	2. 备选开关打开 → 改派 cp-seedance-2.5flash，并钉住 480p 生成；
//	3. 备选接不住（没渠道 / 没比例 / 比例不兼容 / 没配超分 / 时长非档位）
//	   → **回落班车**，而不是把请求留在已挂的源链路上。
//
// ⚠️ 第三条是本文件最重要的断言：备选是「更好的退路」，不是唯一退路。

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

const (
	testCapcutAltModel = "cp-seedance-2.5flash"
	// testCapcutAltChannelModel 与备选目标同名 —— 备选链路的渠道可用性按模型名探测。
	testCapcutAltChannelModel = "cp-seedance-2.5flash"
)

// enableFailoverWithAlt 打开规则五并配置备选备胎。
//
// target 传班车模型（wan3.0-班车）时语义与生产一致：备选接不住就落班车。
func enableFailoverWithAlt(t *testing.T, altEnabled bool, extra map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"failover_enabled":      true,
		"failover_source_model": failoverSourceModel,
		"failover_target_model": failoverBackupModel,
		"failover_alt_enabled":  altEnabled,
	}
	for key, value := range extra {
		payload[key] = value
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// stubSuperResolution 覆盖「超分链路是否可用」的探针。
func stubSuperResolution(t *testing.T, configured bool) {
	t.Helper()
	previous := superResolutionConfiguredProbe
	superResolutionConfiguredProbe = func() bool { return configured }
	t.Cleanup(func() { superResolutionConfiguredProbe = previous })
}

// runAltRoute 跑一遍路由，返回完整记录器（要看屏蔽倍率标记）。
func runAltRoute(t *testing.T, body string) *routeRecorder {
	t.Helper()
	rec := &routeRecorder{}
	engine := newRouteEngine(rec)
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)
	return rec
}

// 备选开关默认关闭时，豆包挂了仍然走班车 —— 与改造前完全一致。
func TestFailoverAltOffKeepsBackupChain(t *testing.T) {
	enableFailoverWithAlt(t, false, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9"}`)
	seen := decodeBody(t, rec.body)
	assert.Equal(t, failoverBackupModel, seen["model"], "备选关闭时必须仍走班车")
	assert.False(t, rec.suppressTaskRatios, "班车链路不需要屏蔽倍率")
}

// 备选打开且接得住：改派 CapCut，钉住 480p，并保留比例/时长/参考图。
func TestFailoverAltRoutesToCapcut480p(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t,
		`{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9","images":["https://cdn.example.com/a.png"]}`)
	seen := decodeBody(t, rec.body)

	assert.Equal(t, testCapcutAltModel, seen["model"])
	assert.Equal(t, "480p", seen["resolution"], "生成档位必须是 480p（成本与超分链路的前提）")
	assert.Equal(t, "16:9", seen["ratio"], "比例必须按 CapCut 顶层字段写")
	assert.Equal(t, "10", seen["seconds"], "时长按 CapCut 契约写成字符串")
	images, ok := seen["images"].([]interface{})
	require.True(t, ok, "参考图必须落在顶层 images")
	require.Len(t, images, 1)
	assert.Equal(t, "https://cdn.example.com/a.png", images[0])
	_, hasMetadata := seen["metadata"]
	assert.False(t, hasMetadata, "CapCut 不读 metadata，带过去会与顶层 ratio 打架")
	assert.Equal(t, failoverSourceModel, rec.originModel, "计费原点必须仍是客户请求的 seedance-2-5")
	assert.True(t, rec.suppressTaskRatios, "一口价模型降级到按秒链路时必须屏蔽倍率（否则超收 N 倍）")
}

// 备选链路没有可用渠道 → 回落班车。
func TestFailoverAltFallsBackWhenCapcutChannelMissing(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: false,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9"}`)
	assert.Equal(t, failoverBackupModel, decodeBody(t, rec.body)["model"])
	assert.False(t, rec.suppressTaskRatios)
}

// 没配超分 → 不能用备选（否则按 720P 的价交付 480P）。
func TestFailoverAltFallsBackWhenSuperResolutionUnconfigured(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, false)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9"}`)
	assert.Equal(t, failoverBackupModel, decodeBody(t, rec.body)["model"],
		"超分没配时必须回落班车，不能静默交付 480P")
}

// 请求没写比例（= 官网自动档）→ CapCut 无自动档，回落班车。
func TestFailoverAltFallsBackWhenRatioMissing(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`)
	assert.Equal(t, failoverBackupModel, decodeBody(t, rec.body)["model"])
}

// 比例是 auto → 同上，回落班车（不能替客户改成 16:9）。
func TestFailoverAltFallsBackWhenRatioAuto(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"auto"}`)
	assert.Equal(t, failoverBackupModel, decodeBody(t, rec.body)["model"])
}

// 时长不在改派白名单（7 秒）→ 备选与班车都拒收，请求原样留在源链路。
//
// 两条退路共用同一份改派白名单（failoverAllowedDurations）：非档位时长
// 在源链路就该报错，改派出去只会把「源链路的明确报错」变成「按某个截断值跑」。
func TestFailoverAltFallsBackWhenDurationNotWhitelisted(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	body := `{"model":"seedance-2-5","seconds":7,"prompt":"a cat","ratio":"16:9"}`
	rec := runAltRoute(t, body)
	assert.Equal(t, body, rec.body, "非档位时长不能被改派（备选与班车同口径）")
	assert.Empty(t, rec.originModel)
}

// 源链路可用时一个字都不改（备选只在故障时参与）。
func TestFailoverAltDoesNothingWhenSourceHealthy(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       true,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9"}`
	rec := runAltRoute(t, body)
	assert.Equal(t, body, rec.body, "源链路可用时请求体必须逐字节不变")
	assert.Empty(t, rec.originModel)
	assert.False(t, rec.suppressTaskRatios)
}

// 促销族 30 秒 → 规则一改派豆包 → 豆包额度用完 → 备选 CapCut（第二段降级）。
//
// ⚠️ 用「额度探针标记不可用」而不是「渠道不可用」构造豆包故障：
// 渠道真被关掉时规则一根本不会把请求改派过去（prepareSecondsRoute 先探目标可用性），
// 那是另一格矩阵。本用例验的是「请求已到豆包、但豆包跑不了」这一格。
func TestFailoverAltAppliesAfterRuleOneRoute(t *testing.T) {
	enableFailoverWithAlt(t, true, map[string]interface{}{
		"source_model":    "seedance2.5-特惠",
		"target_model":    "seedance-2-5",
		"trigger_seconds": 20,
	})
	stubChannelAvailableByModel(t, map[string]bool{
		"seedance2.5-特惠":          true,
		failoverSourceModel:       true, // 渠道还活着，只是额度用完
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	marked := seedanceroute_setting.WithFailoverUnavailable(
		timeNowUnix()+3600, seedanceroute_setting.ReasonQuotaExhausted)
	encoded, err := common.Marshal(marked)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))

	rec := runAltRoute(t,
		`{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat","ratio":"16:9"}`)
	seen := decodeBody(t, rec.body)

	assert.Equal(t, testCapcutAltModel, seen["model"])
	assert.Equal(t, "480p", seen["resolution"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel,
		"计费原点必须是客户最初请求的促销族模型")
	assert.True(t, rec.suppressTaskRatios)
}

// 备选目标配成别的模型名 → 不按 CapCut 契约改写，回落班车。
func TestFailoverAltIgnoresUnknownTargetModel(t *testing.T) {
	enableFailoverWithAlt(t, true, map[string]interface{}{
		"failover_alt_model": "some-other-model",
	})
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
		"some-other-model":        true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"16:9"}`)
	assert.Equal(t, failoverBackupModel, decodeBody(t, rec.body)["model"],
		"备选目标必须是 CapCut 2.5 Flash，其它名字不猜契约")
}

// CapCut 契约：模型名必须是 2.5 Flash 那一档，且写的是 480p（小写字面量）。
func TestFailoverAltBodyUsesCapcutContract(t *testing.T) {
	enableFailoverWithAlt(t, true, nil)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	rec := runAltRoute(t, `{"model":"seedance-2-5","seconds":5,"prompt":"a cat","size":"9:16"}`)
	seen := decodeBody(t, rec.body)
	assert.Equal(t, testCapcutAltModel, seen["model"])
	assert.Equal(t, "9:16", seen["ratio"], "size 形态的比例要归一成 CapCut 的顶层 ratio")
	assert.True(t, strings.EqualFold("480p", seen["resolution"].(string)))
}
