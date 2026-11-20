package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「万相官网 → 客美 tj」开关的管理端测试（2026-11-19）。
//
// 按钮语义：打开后 wan3.0-video-官网 的请求改派 wan3.0-video-tj；
// 计费仍按客户请求的官网模型；目标没渠道或画幅 adaptive 时放行原请求。

// 管理状态必须暴露开关与源/目标模型：运营按按钮前要能读出「谁 → 谁」。
func TestDoubaoRouteAdminStateReportsWan3GuanwangTj(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_guanwang_tj_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["wan3_guanwang_tj_enabled"])
	assert.Equal(t, "wan3.0-video-官网", state["wan3_guanwang_tj_source_model"])
	assert.Equal(t, "wan3.0-video-tj", state["wan3_guanwang_tj_target_model"])
}

// 旧生产 option 的形状（没有这个字段）：默认关闭。
func TestDoubaoRouteAdminStateWan3GuanwangTjDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["wan3_guanwang_tj_enabled"])
}

// 校验先行：enabled 没传时必须拒绝 —— nil bool 当 false 处理会把正在跑的
// 改派静默关掉。
func TestSetWan3GuanwangTjAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-tj", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetWan3GuanwangTjAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

// 成功路径：策略被保存（带开关）、审计记一条，响应里带回最新状态。
func TestSetWan3GuanwangTjAdminSavesAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-tj", strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	audited := ""
	setWan3GuanwangTjAdmin(c,
		func(policy seedanceroute_setting.Policy) error {
			saved = policy
			return nil
		},
		func(_ *gin.Context, action string, params map[string]interface{}) {
			audited = action
			// 审计里必须能看出改的是哪两个模型，否则事后只能靠猜。
			assert.Equal(t, "wan3.0-video-官网", params["source_model"])
			assert.Equal(t, "wan3.0-video-tj", params["target_model"])
		})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, saved.Wan3GuanwangTJEnabled, "保存的策略必须带上开关")
	assert.Equal(t, "video_route.wan3_guanwang_tj_switch", audited)
	assert.Contains(t, recorder.Body.String(), "wan3_guanwang_tj_enabled")
}

// 关闭也必须能保存（按钮是双向的）。
func TestSetWan3GuanwangTjAdminSavesOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_guanwang_tj_enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-tj", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	setWan3GuanwangTjAdmin(c,
		func(policy seedanceroute_setting.Policy) error { saved = policy; return nil },
		func(*gin.Context, string, map[string]interface{}) {})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.False(t, saved.Wan3GuanwangTJEnabled)
}
