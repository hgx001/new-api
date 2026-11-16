package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则八（全量兜底 → cp-seedance-2.5）的管理端测试（2026-11-15）。
//
// 重点锁三件事：
//  1. 状态字段完整（前端要按这两个字段渲染开关与目标模型）；
//  2. 缺 enabled 必须 400 —— 少写一个字段就把应急开关静默关掉，
//     而这种开关「关了」的表现是「请求继续打挂掉的源链路」，无从区分；
//  3. 成功路径只写一次库、只改这一个字段。

func TestDoubaoRouteAdminStateReportsTotalFallbackFields(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠","seedance2.5-活动"],
		"total_fallback_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["total_fallback_enabled"])
	assert.Equal(t, "cp-seedance-2.5", state["total_fallback_target_model"])
}

// 旧生产 option 的形状（没有该字段）：默认关闭，升级当天行为不变。
func TestDoubaoRouteAdminStateTotalFallbackDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"]}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["total_fallback_enabled"])
	assert.Equal(t, "cp-seedance-2.5", state["total_fallback_target_model"])
}

func TestSetTotalFallbackRouteAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/total-fallback", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetTotalFallbackRouteAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestSetTotalFallbackRouteAdminRejectsMalformedBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/total-fallback", strings.NewReader(`not-json`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetTotalFallbackRouteAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

// 成功路径：写一次库、审计一条、状态里立刻是「已开启」。
func TestSetTotalFallbackRouteAdminEnablesSwitch(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"]}`)

	saved := 0
	audits := 0
	var savedPolicy seedanceroute_setting.Policy

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/total-fallback",
		strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setTotalFallbackRouteAdmin(c,
		func(policy seedanceroute_setting.Policy) error {
			saved++
			savedPolicy = policy
			// 模拟落库后的 option 分发：真实现走 model.UpdateOption，
			// 单测环境没有 DB，直接喂给同一份解析入口。
			return seedanceroute_setting.UpdateFromJSONString(mustMarshalPolicy(t, policy))
		},
		func(*gin.Context, string, map[string]interface{}) { audits++ },
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 1, saved, "成功路径只能写一次库")
	assert.Equal(t, 1, audits, "成功路径必须留一条审计")
	assert.True(t, savedPolicy.TotalFallbackEnabled)
	assert.True(t, seedanceroute_setting.IsTotalFallbackEnabled())
}

// 关闭方向同样要能落库（否则「开了关不掉」= 全站请求永久跑 CapCut）。
func TestSetTotalFallbackRouteAdminDisablesSwitch(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"total_fallback_enabled":true}`)
	require.True(t, seedanceroute_setting.IsTotalFallbackEnabled())

	var savedPolicy seedanceroute_setting.Policy
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/total-fallback",
		strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setTotalFallbackRouteAdmin(c,
		func(policy seedanceroute_setting.Policy) error {
			savedPolicy = policy
			return seedanceroute_setting.UpdateFromJSONString(mustMarshalPolicy(t, policy))
		},
		func(*gin.Context, string, map[string]interface{}) {},
	)

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.False(t, savedPolicy.TotalFallbackEnabled)
	assert.False(t, seedanceroute_setting.IsTotalFallbackEnabled())
}

// mustMarshalPolicy 把策略序列化成 option JSON。
//
// 用 common.Marshal（业务代码统一的 JSON 包装），不用 encoding/json ——
// 与 saveRoutePolicy 的写入口径逐字节一致，测试才有意义。
func mustMarshalPolicy(t *testing.T, policy seedanceroute_setting.Policy) string {
	t.Helper()
	raw, err := common.Marshal(policy)
	require.NoError(t, err)
	return string(raw)
}
