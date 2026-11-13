package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 豆包链路的「接单上限」（2026-11-13 老板定）。
//
// 管理状态必须把「上限 / 已接 / 剩余 / 为什么关」如实暴露给前端合并卡片：
// 运营要能读出「这批额度还剩几单」，不能让人猜。

// limitSaveStub 模拟 model.UpdateOption 的分发链（写库 + 更新内存状态），
// 让测试能覆盖成功路径而不依赖数据库。与 ratioGateSaveStub 同口径。
func limitSaveStub(t *testing.T) func(seedanceroute_setting.Policy) error {
	t.Helper()
	return func(policy seedanceroute_setting.Policy) error {
		raw, err := common.Marshal(policy)
		if err != nil {
			return err
		}
		return seedanceroute_setting.UpdateFromJSONString(string(raw))
	}
}

// 审计日志会写库，测试环境没有 DB；注入成空实现。
func noopLimitAudit(_ *gin.Context, _ string, _ map[string]interface{}) {}

// 状态里必须带上限、已接、剩余三个数。
func TestDoubaoRouteAdminStateReportsLinkLimit(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_max_tasks":3,"doubao_link_accepted_tasks":1}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, 3, state["doubao_link_max_tasks"], "必须给出上限")
	assert.Equal(t, 1, state["doubao_link_accepted_tasks"], "必须给出已接单数")
	assert.Equal(t, 2, state["doubao_link_remaining"], "必须给出剩余可接单数")
}

// 没配上限（默认状态）：上限与剩余都是 0，语义是「不限」。
func TestDoubaoRouteAdminStateLinkLimitUnlimitedByDefault(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, 0, state["doubao_link_max_tasks"])
	assert.Equal(t, 0, state["doubao_link_remaining"])
	assert.Equal(t, "", state["doubao_link_off_reason"], "开着时没有关闭原因")
}

// 接满后：链路关闭、原因是「限额」，且**没有**自动恢复时间文案。
//
// 自动恢复文案是给「人工关闭」用的承诺（「关到凌晨 2 点」）。
// 限额关闭没有恢复时间 —— 额度要等人手动再发。
func TestDoubaoRouteAdminStateLinkLimitReached(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_max_tasks":2,"doubao_link_accepted_tasks":2}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["doubao_link_off"], "接满后链路必须关闭")
	assert.Equal(t, seedanceroute_setting.DoubaoLinkOffReasonLimit, state["doubao_link_off_reason"])
	assert.Equal(t, 0, state["doubao_link_remaining"])
	_, present := state["doubao_link_off_until_text"]
	assert.False(t, present, "限额关闭没有自动恢复时间，不该出现恢复文案")
}

// 设置上限的接口：合法值要写进策略。
func TestSetDoubaoLinkMaxTasksAdminSavesLimit(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{"max_tasks":3}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 3, seedanceroute_setting.DoubaoLinkMaxTasks())
}

// 校验先行：max_tasks 必须是**非负整数**。
//
// 负数会让「剩余 = 上限 - 已接」算成比上限还多，运营会以为额度变多了。
// 0 = 不限（合法）。
func TestSetDoubaoLinkMaxTasksAdminRejectsNegative(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{"max_tasks":-1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkMaxTasks(), "非法请求不许改配置")
}

// 缺字段必须报错，不能默认成 0（那等于悄悄取消限额）。
func TestSetDoubaoLinkMaxTasksAdminRejectsMissingField(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_max_tasks":3}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, 3, seedanceroute_setting.DoubaoLinkMaxTasks(), "缺字段不许改配置")
}

// 设置上限会把计数清零：运营「设成 3」= 「给一批新的 3 单额度」。
//
// 不清零的话，上一轮已接 3 单、重新设成 3 单后剩余是 0 —— 一单都接不到，
// 运营会以为设置没生效。
func TestSetDoubaoLinkMaxTasksAdminResetsCounter(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_max_tasks":3,"doubao_link_accepted_tasks":2}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{"max_tasks":3}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 3, seedanceroute_setting.DoubaoLinkMaxTasks())
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkAcceptedTasks(), "新一批额度，计数归零")
	assert.False(t, seedanceroute_setting.IsDoubaoLinkOff(), "新额度给了，链路必须能接单")
}

// 设置上限为 0 = 不限：链路必须恢复接单（不能再停在关闭态）。
func TestSetDoubaoLinkMaxTasksAdminZeroMeansUnlimited(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"doubao_link_max_tasks":2,"doubao_link_accepted_tasks":2}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{"max_tasks":0}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, 0, seedanceroute_setting.DoubaoLinkMaxTasks())
	assert.False(t, seedanceroute_setting.IsDoubaoLinkOff(), "不再限额就该恢复接单")
}

// 上限设置后，返回的状态要能直接渲染（前端一次请求拿到全部事实）。
func TestSetDoubaoLinkMaxTasksAdminReturnsFullState(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost,
		"/api/video-route/policy/doubao-link-max-tasks", strings.NewReader(`{"max_tasks":3}`))
	c.Request.Header.Set("Content-Type", "application/json")

	setDoubaoLinkMaxTasksAdmin(c, limitSaveStub(t), noopLimitAudit)

	body := recorder.Body.String()
	assert.Contains(t, body, fmt.Sprintf("%q", "doubao_link_max_tasks"))
	assert.Contains(t, body, fmt.Sprintf("%q", "doubao_link_remaining"))
}
