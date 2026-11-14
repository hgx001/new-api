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
