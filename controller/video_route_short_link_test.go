package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 规则六（短时长分流 → 心笔内链）的全局链路开关（2026-11-13）。
//
// 语义与豆包链路开关对齐：一个开关 = 心笔内链通不通。
//   - 开启：≤阈值的短请求改派到 short_target_model；
//   - 关闭：规则六对所有源模型停用，短请求走原链路。
//
// 与豆包开关的差异：心笔内链没有按天重置的日额度，所以没有自动恢复
// 时间、没有接单计数、不强制故障转移 —— 纯手动开关。

// 管理状态必须如实暴露开关与规则六的配置：
// 「开/关」+「改派到谁」+「阈值几秒」，运营按按钮前要能读懂影响范围。
func TestDoubaoRouteAdminStateReportsShortLinkSwitch(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_trigger_seconds":10}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["short_link_enabled"])
	assert.Equal(t, "wan3.0-720p-特惠-内链", state["short_target_model"])
	assert.Equal(t, 10, state["short_trigger_seconds"])
}

// 显式关闭：状态里 short_link_enabled = false。
func TestDoubaoRouteAdminStateReportsShortLinkOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_link_enabled":false}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["short_link_enabled"])
}

// 没配 short_link_enabled 字段（旧生产 option 的形状）：默认开启 ——
// 升级当天行为必须不变。
func TestDoubaoRouteAdminStateShortLinkDefaultsOn(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["short_link_enabled"])
}

// 校验先行：enabled 没传时必须拒绝 —— nil bool 若当 false 处理，
// 一次缺字段的请求就会把心笔分流静默关掉。
func TestSetShortLinkAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/short-link", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetShortLinkAdmin(c)

	// 与 SetDoubaoLinkAdmin 同口径：校验失败 = 400。
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}
