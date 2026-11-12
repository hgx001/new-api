package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	// 远期截止时间（当前时间 + 24 小时）→ 开关处于关闭状态。
	//
	// ⚠️ 不能写死时间戳：这条用例原来写死 1794477600（2026-11-12 18:00），
	// 到点之后开关按设计自动恢复成「开」，测试就变成**永久红** ——
	// 与代码无关的失败会掩盖真正的回归。用相对时间才能永远表达「还没到点」。
	offUntil := time.Now().Add(24 * time.Hour).Unix()
	withAdminRoutePolicy(t, fmt.Sprintf(`{"enabled":true,"doubao_link_off_until":%d}`, offUntil))

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["doubao_link_off"])
	assert.Equal(t, offUntil, state["doubao_link_off_until"])
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

// 自动恢复后（until 已过期）：doubao_link_off 已回到 false，恢复时间文案必须
// 一并收起 —— api.ts 约定该字段「仅关闭时返回」，过期时间还挂着就是自相矛盾
// 的状态（off=false 却带着"关到几点"）。
func TestDoubaoRouteAdminStateHidesExpiredUntilText(t *testing.T) {
	withAdminRoutePolicy(t, fmt.Sprintf(
		`{"enabled":true,"doubao_link_off_until":%d}`, time.Now().Unix()-60))

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["doubao_link_off"])
	_, present := state["doubao_link_off_until_text"]
	assert.False(t, present, "已自动恢复的状态不该再返回恢复时间文案")
}
