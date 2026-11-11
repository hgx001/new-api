package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 豆包链路开关（2026-11-11）：一个开关表达「豆包通不通」。
//
//   - 关闭：规则一停用（特惠/活动留常驻）+ 故障转移强制生效（seedance-2-5 → 班车），
//     截止到下一个北京时间凌晨 2 点自动恢复；
//   - 开启：立即恢复。
//
// 管理状态必须把开关与恢复时间如实暴露给前端合并卡片：
// 「关到几点自动恢复」是对运营的承诺，不能让人猜。

func TestDoubaoRouteAdminStateReportsDoubaoLinkSwitch(t *testing.T) {
	// 远期截止时间（2026-11-12 之后）→ 开关处于关闭状态。
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_off_until":1794477600}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["doubao_link_off"])
	assert.Equal(t, int64(1794477600), state["doubao_link_off_until"])
	assert.NotEmpty(t, state["doubao_link_off_until_text"], "关闭时必须给出自动恢复时间文案")
}

func TestDoubaoRouteAdminStateLinkOnByDefault(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["doubao_link_off"])
	assert.Equal(t, int64(0), state["doubao_link_off_until"])
	_, present := state["doubao_link_off_until_text"]
	assert.False(t, present, "开启状态下不该出现恢复时间文案")
}

// 校验先行：enabled 没传时必须拒绝 —— nil bool 若当 false 处理，
// 一次缺字段的请求就会把豆包链路静默关掉。
func TestSetDoubaoLinkAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/doubao-link", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetDoubaoLinkAdmin(c)

	// 与 SetSourceSecondsRuleAdmin 同口径：校验失败 = 400。
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}
