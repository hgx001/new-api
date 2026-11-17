package middleware

// wan3.0-720p-特惠 → seedance2.5-特惠 的无条件别名。
//
// 与另外几条「条件改派」的区别：
//   - 条件改派看请求特征（时长/比例/首尾帧）或链路可用性；
//   - 这里是**模型名等价**：客户请求 wan3.0-720p-特惠，就当作请求 seedance2.5-特惠，
//     不看任何参数、也不受 seedanceroute 开关影响。
//
// 别名之后 seedance2.5-特惠 的规则仍然照常生效（短时长 → 心笔内链、
// 超时长 → seedance-2-5、首尾帧 → wan3）：客户拿到的行为与直接调用
// seedance2.5-特惠 完全一致，只是账单/日志仍记客户原始请求的模型。

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disableAllRoutes 把条件路由全部关掉，验证「别名不受开关影响」。
func disableAllRoutes(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(`{"enabled":false}`))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// runAlias 按「别名 → 条件路由」的真实顺序跑一遍请求。
func runAlias(t *testing.T, path, contentType, body string) *routeRecorder {
	t.Helper()
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3TehuiAliasRoute(), Seedance25TeHuiRoute())
	request := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return rec
}

// 无条件：路由开关全关也要改写。
func TestWan3TehuiAliasRewritesUnconditionally(t *testing.T) {
	disableAllRoutes(t)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-720p-特惠","prompt":"a cat"}`)
	assert.Equal(t, constant.Seedance25TeHuiModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, wan3TehuiModelName, rec.originModel, "计费原点必须是客户原始请求的模型")
}

// 其它模型不能被动到。
func TestWan3TehuiAliasLeavesOtherModelsUntouched(t *testing.T) {
	disableAllRoutes(t)

	original := `{"model":"seedance2.5-特惠","prompt":"a cat"}`
	rec := runAlias(t, "/v1/video/generations", "application/json", original)
	assert.Equal(t, original, rec.body)
	assert.Empty(t, rec.originModel)
}

// 非 JSON 请求不解析、不改写。
func TestWan3TehuiAliasSkipsNonJSON(t *testing.T) {
	disableAllRoutes(t)

	original := `model=wan3.0-720p-特惠`
	rec := runAlias(t, "/v1/video/generations", "text/plain", original)
	assert.Equal(t, original, rec.body)
	assert.Empty(t, rec.originModel)
}

// /remix 基于已有任务，模型与参数由原任务决定，不能改名。
func TestWan3TehuiAliasSkipsRemix(t *testing.T) {
	disableAllRoutes(t)

	original := `{"model":"wan3.0-720p-特惠","prompt":"a cat"}`
	rec := runAlias(t, "/v1/videos/task_abc/remix", "application/json", original)
	assert.Equal(t, original, rec.body)
	assert.Empty(t, rec.originModel)
}

// 别名之后仍按 seedance2.5-特惠 的规则分流，但计费原点不能被覆盖。
func TestWan3TehuiAliasKeepsOriginAfterShortRule(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-720p-特惠","seconds":5,"prompt":"a cat"}`)
	assert.Equal(t, testShortTargetModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, wan3TehuiModelName, rec.originModel,
		"条件规则改派后，计费原点必须仍是 wan3.0-720p-特惠")
}

// 长时长同样：规则一改派到 seedance-2-5，计费原点保持客户原始模型。
func TestWan3TehuiAliasKeepsOriginAfterSecondsRule(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-720p-特惠","seconds":30,"prompt":"a cat"}`)
	assert.Equal(t, testTargetModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, wan3TehuiModelName, rec.originModel,
		"规则一改派后，计费原点必须仍是 wan3.0-720p-特惠")
}

// ════════════════════════════════════════════════════════════════════════════
// 特惠档比例闸门（老板口径：只做 9:16）
//
// ⚠️ 闸门必须挂在**别名中间件**（Wan3TehuiAliasRoute）而不是 wan3 adaptor：
// 生产上 wan3.0-720p-特惠 是无条件别名 → seedance2.5-特惠，请求根本不会
// 进入 wan3 adaptor（先被改名，再被规则八兜底到 cp-seedance-2.5flash）。
// 闸门放在 adaptor 里等于形同虚设 —— 2026-11-17 生产实测：16:9 请求照样
// 200 建单并落到 channel 20。
// ════════════════════════════════════════════════════════════════════════════

// runAliasRaw 不对响应码做断言，用于验证 400 拒单的场景。
func runAliasRaw(t *testing.T, body string) (*httptest.ResponseRecorder, *routeRecorder) {
	t.Helper()
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3TehuiAliasRoute(), Seedance25TeHuiRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response, rec
}

// 非 9:16 必须被 400 明确拒单（而不是静默改成 9:16 交付）。
func TestWan3TehuiAliasRejectsNonPortraitRatio(t *testing.T) {
	disableAllRoutes(t)

	for _, ratio := range []string{"16:9", "1:1", "4:3", "3:4", "21:9", "adaptive"} {
		response, rec := runAliasRaw(t,
			`{"model":"wan3.0-720p-特惠","prompt":"a cat","ratio":"`+ratio+`"}`)
		assert.Equal(t, 400, response.Code,
			"比例 %s 必须被 400 拒单，实际响应 %d：%s", ratio, response.Code, response.Body.String())
		assert.Contains(t, response.Body.String(), "9:16",
			"拒单文案必须说明只支持 9:16")
		assert.Empty(t, rec.body, "被拒的请求不能进入下游链路")
	}
}

// 9:16 必须放行，且照常改名成 seedance2.5-特惠。
func TestWan3TehuiAliasAllowsPortraitRatio(t *testing.T) {
	disableAllRoutes(t)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-720p-特惠","prompt":"a cat","ratio":"9:16"}`)
	assert.Equal(t, constant.Seedance25TeHuiModel, decodeBody(t, rec.body)["model"])
}

// metadata.ratio 与 aspect_ratio 写法同样受闸门约束（不能只堵顶层字段）。
func TestWan3TehuiAliasRatioGateCoversMetadataFields(t *testing.T) {
	disableAllRoutes(t)

	cases := []string{
		`{"model":"wan3.0-720p-特惠","prompt":"a cat","metadata":{"ratio":"16:9"}}`,
		`{"model":"wan3.0-720p-特惠","prompt":"a cat","metadata":{"aspect_ratio":"16:9"}}`,
		`{"model":"wan3.0-720p-特惠","prompt":"a cat","aspect_ratio":"16:9"}`,
		`{"model":"wan3.0-720p-特惠","prompt":"a cat","size":"1920x1080"}`,
	}
	for _, body := range cases {
		response, _ := runAliasRaw(t, body)
		assert.Equal(t, 400, response.Code,
			"请求 %s 必须被 400 拒单，实际 %d：%s", body, response.Code, response.Body.String())
	}
}

// 客户**没传**比例时必须放行：不能凭空拒单（默认画幅由链路自己定）。
func TestWan3TehuiAliasRatioGateAllowsMissingRatio(t *testing.T) {
	disableAllRoutes(t)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-720p-特惠","prompt":"a cat"}`)
	assert.Equal(t, constant.Seedance25TeHuiModel, decodeBody(t, rec.body)["model"])
}

// 闸门只针对特惠档：其它模型（seedance2.5-特惠 等）不受影响。
func TestWan3TehuiAliasRatioGateDoesNotAffectOtherModels(t *testing.T) {
	disableAllRoutes(t)

	rec := runAlias(t, "/v1/video/generations", "application/json",
		`{"model":"seedance2.5-特惠","prompt":"a cat","ratio":"16:9"}`)
	assert.Contains(t, rec.body, `"ratio":"16:9"`)
}
