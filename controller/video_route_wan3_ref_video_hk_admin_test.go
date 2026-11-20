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

// 「带参考视频 → hk-wan3」开关的管理端测试（2026-11-20）。

// 管理状态要能读出：有效值、目标模型、源模型清单，以及「有没有被显式配过」。
func TestDoubaoRouteAdminStateReportsWan3RefVideoHk(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","wan3_ref_video_hk_enabled":false}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["wan3_ref_video_hk_enabled"])
	assert.Equal(t, true, state["wan3_ref_video_hk_configured"])
	assert.Equal(t, "hk-wan3", state["wan3_ref_video_hk_target_model"])
	assert.Equal(t, []string{
		"wan3.0-video-官网",
		"wan3.0-video-tj",
		"wan3.0-video-prime-tj",
	}, state["wan3_ref_video_hk_source_models"])
}

// 旧生产 option 的形状（没有这个字段）：有效值 = 开启，但 configured = false。
func TestDoubaoRouteAdminStateWan3RefVideoHkDefaultsOn(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["wan3_ref_video_hk_enabled"])
	assert.Equal(t, false, state["wan3_ref_video_hk_configured"])
}

// enabled 没传时必须拒绝。
func TestSetWan3RefVideoHkAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-ref-video-hk", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetWan3RefVideoHkAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

// 成功路径：策略被保存、审计记一条源模型清单与目标模型。
func TestSetWan3RefVideoHkAdminSavesAndAudits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5"}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/wan3-ref-video-hk", strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")

	var saved seedanceroute_setting.Policy
	audited := ""
	setWan3RefVideoHkAdmin(c,
		func(policy seedanceroute_setting.Policy) error { saved = policy; return nil },
		func(_ *gin.Context, action string, params map[string]interface{}) {
			audited = action
			assert.Equal(t, "hk-wan3", params["target_model"])
		})

	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotNil(t, saved.Wan3RefVideoHkEnabled)
	assert.False(t, *saved.Wan3RefVideoHkEnabled)
	assert.Equal(t, "video_route.wan3_ref_video_hk_switch", audited)
	assert.Contains(t, recorder.Body.String(), "wan3_ref_video_hk_enabled")
}
