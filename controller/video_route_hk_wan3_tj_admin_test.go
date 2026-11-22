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

// 「hk-wan3 → 客美 tj」开关的管理端测试（2026-11-22）。
//
// 按钮语义：打开后不带参考视频的 hk-wan3 请求改派 wan3.0-video-tj；
// 计费仍按客户请求的 hk-wan3；带参考视频的请求不改派。

func TestDoubaoRouteAdminStateReportsWan3HkWan3Tj(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_hk_wan3_tj_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["wan3_hk_wan3_tj_enabled"])
	assert.Equal(t, "hk-wan3", state["wan3_hk_wan3_tj_source_model"])
	assert.Equal(t, "wan3.0-video-tj", state["wan3_hk_wan3_tj_target_model"])
}

func TestDoubaoRouteAdminStateWan3HkWan3TjDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["wan3_hk_wan3_tj_enabled"])
}

func TestSetWan3HkWan3TjAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-hk-wan3-tj", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetWan3HkWan3TjAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

func TestSetWan3HkWan3TjAdminSavesAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-hk-wan3-tj", strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	audited := ""
	setWan3HkWan3TjAdmin(c,
		func(policy seedanceroute_setting.Policy) error {
			saved = policy
			return nil
		},
		func(_ *gin.Context, action string, params map[string]interface{}) {
			audited = action
			assert.Equal(t, "hk-wan3", params["source_model"])
			assert.Equal(t, "wan3.0-video-tj", params["target_model"])
		})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, saved.Wan3HkWan3TjEnabled, "保存的策略必须带上开关")
	assert.Equal(t, "video_route.wan3_hk_wan3_tj_switch", audited)
	assert.Contains(t, recorder.Body.String(), "wan3_hk_wan3_tj_enabled")
}

func TestSetWan3HkWan3TjAdminSavesOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_hk_wan3_tj_enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-hk-wan3-tj", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	setWan3HkWan3TjAdmin(c,
		func(policy seedanceroute_setting.Policy) error { saved = policy; return nil },
		func(*gin.Context, string, map[string]interface{}) {})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.False(t, saved.Wan3HkWan3TjEnabled)
}
