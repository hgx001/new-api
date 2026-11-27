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

// 「万相官网 → wan3-新大陆」开关的管理端测试（2026-11-27）。

func TestDoubaoRouteAdminStateReportsWan3GuanwangXindalu(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_guanwang_xindalu_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["wan3_guanwang_xindalu_enabled"])
	assert.Equal(t, "wan3.0-video-官网", state["wan3_guanwang_xindalu_source_model"])
	assert.Equal(t, "wan3-新大陆", state["wan3_guanwang_xindalu_target_model"])
}

func TestDoubaoRouteAdminStateWan3GuanwangXindaluDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["wan3_guanwang_xindalu_enabled"])
}

func TestSetWan3GuanwangXindaluAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-xindalu", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetWan3GuanwangXindaluAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

// 成功路径：保存的策略必须打开新大陆并**自动关掉客美**，审计带前后模型名。
func TestSetWan3GuanwangXindaluAdminSavesAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_guanwang_tj_enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-xindalu", strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	audited := ""
	setWan3GuanwangXindaluAdmin(c,
		func(policy seedanceroute_setting.Policy) error {
			saved = policy
			return nil
		},
		func(_ *gin.Context, action string, params map[string]interface{}) {
			audited = action
			assert.Equal(t, "wan3.0-video-官网", params["source_model"])
			assert.Equal(t, "wan3-新大陆", params["target_model"])
		})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.True(t, saved.Wan3GuanwangXindaluEnabled, "保存的策略必须带上开关")
	assert.False(t, saved.Wan3GuanwangTJEnabled, "打开新大陆必须同时关掉客美，避免双改派")
	assert.Equal(t, "video_route.wan3_guanwang_xindalu_switch", audited)
	assert.Contains(t, recorder.Body.String(), "wan3_guanwang_xindalu_enabled")
}

func TestSetWan3GuanwangXindaluAdminSavesOff(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_guanwang_xindalu_enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-guanwang-xindalu", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	setWan3GuanwangXindaluAdmin(c,
		func(policy seedanceroute_setting.Policy) error { saved = policy; return nil },
		func(*gin.Context, string, map[string]interface{}) {})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.False(t, saved.Wan3GuanwangXindaluEnabled)
}
