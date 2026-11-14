package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 规则六「有赞标准版兜底」开关的管理端测试（2026-11-14）。
//
// 按钮语义：打开后 ≤阈值的短请求在**心笔接不住时**改派 wan3.0-video；
// 心笔打开且可用时仍然心笔优先；两个都关闭走原链路。

// 管理状态必须暴露有赞兜底开关与目标模型，运营按按钮前要能读懂影响范围。
func TestDoubaoRouteAdminStateReportsShortYouzanSwitch(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_youzan_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["short_youzan_enabled"])
	assert.Equal(t, "wan3.0-video", state["short_youzan_target_model"])
}

// 没配 short_youzan_enabled（旧生产 option 的形状）：默认关闭，升级当天行为不变。
func TestDoubaoRouteAdminStateShortYouzanDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["short_youzan_enabled"])
}

// 校验先行：enabled 没传时必须拒绝 —— nil bool 若当 false 处理，
// 一次缺字段的请求就会把兜底静默关掉。
func TestSetShortYouzanLinkAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/short-youzan-link", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetShortYouzanLinkAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}
