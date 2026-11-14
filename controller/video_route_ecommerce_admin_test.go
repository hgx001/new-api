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

// 规则七（电商/产品推荐内容 → CapCut 快速档）与规则六第三分支
// （≤10 秒 → CapCut 快速档）的管理端测试（2026-11-14）。
//
// 重点锁三件事：
//  1. 状态字段完整（前端要按这些字段渲染按钮与勾选框）；
//  2. 缺字段必须 400 —— 少写一个字段就把「电商流量改派」静默关掉
//     是最难查的故障类型；
//  3. **三条短时长分支的互斥写入口**：开一条自动关另两条，
//     且心笔那条必须写成**显式 false**（nil = 默认开启，会让 CapCut 永远轮不到）。

// 管理状态必须暴露规则七的全部字段。
func TestDoubaoRouteAdminStateReportsEcommerceFields(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"ecommerce_enabled":true,"ecommerce_ratio_condition":true,
		"ecommerce_seconds_condition":true,"ecommerce_trigger_seconds":25}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["ecommerce_enabled"])
	assert.Equal(t, "cp-seedance-2.5flash", state["ecommerce_target_model"])
	assert.Equal(t, true, state["ecommerce_ratio_condition"])
	assert.Equal(t, true, state["ecommerce_seconds_condition"])
	assert.Equal(t, 25, state["ecommerce_trigger_seconds"])
	assert.Equal(t, "9:16", state["ecommerce_ratio_condition_default"])
	assert.NotEmpty(t, state["ecommerce_keywords"], "关键词表必须有默认值可供展示")
	assert.NotEmpty(t, state["ecommerce_keywords_default"])
}

// 没配 ecommerce_enabled（旧生产 option 的形状）：默认关闭，升级当天行为不变。
func TestDoubaoRouteAdminStateEcommerceDefaultsOff(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"]}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["ecommerce_enabled"])
	assert.Equal(t, false, state["ecommerce_ratio_condition"])
	assert.Equal(t, false, state["ecommerce_seconds_condition"])
	assert.Equal(t, false, state["short_capcut_enabled"])
}

// 管理状态必须暴露三条短时长分支各自的开关与当前生效方。
func TestDoubaoRouteAdminStateReportsShortCapcutFields(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_trigger_seconds":10,
		"short_link_enabled":false,"short_youzan_enabled":false,"short_capcut_enabled":true}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["short_capcut_enabled"])
	assert.Equal(t, "cp-seedance-2.0fast", state["short_capcut_target_model"])
	assert.Equal(t, "cp-seedance-2.0fast", state["short_active_target"])
	assert.Equal(t, []string{"cp-seedance-2.0fast"}, state["short_active_targets"])
}

// 校验先行：enabled 没传时必须拒绝。
func TestSetEcommerceRouteAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetEcommerceRouteAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

// 参数条件：两个布尔都必须传 —— 漏传一个就把它静默关掉。
func TestSetEcommerceConditionsAdminRejectsMissingFlags(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce/conditions",
		strings.NewReader(`{"ratio_enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetEcommerceConditionsAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "seconds_enabled")
}

// 负数阈值必须被拒绝。
func TestSetEcommerceConditionsAdminRejectsNegativeThreshold(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce/conditions",
		strings.NewReader(`{"ratio_enabled":true,"seconds_enabled":true,"trigger_seconds":-1}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetEcommerceConditionsAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
}

// CapCut 分支开关：enabled 没传时必须拒绝。
func TestSetShortCapcutLinkAdminRejectsMissingEnabled(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/short-capcut-link", strings.NewReader(`{}`))
	c.Request.Header.Set("Content-Type", "application/json")

	SetShortCapcutLinkAdmin(c)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Contains(t, recorder.Body.String(), `"success":false`)
	assert.Contains(t, recorder.Body.String(), "enabled")
}

// ecommerceSaveStub 模拟 model.UpdateOption 的分发链（写库 + 更新内存状态），
// 让测试能覆盖成功路径而不依赖数据库。第三个返回值是写库次数，
// 校验失败时必须一次都没调。
//
// 与 ratioGateSaveStub 同款做法（见 controller/video_route_ratio_gate_test.go）。
func ecommerceSaveStub(t *testing.T) (func(seedanceroute_setting.Policy) error, *seedanceroute_setting.Policy, *int) {
	t.Helper()
	saved := new(seedanceroute_setting.Policy)
	calls := new(int)
	return func(policy seedanceroute_setting.Policy) error {
		*calls = *calls + 1
		*saved = policy
		raw, err := common.Marshal(policy)
		if err != nil {
			return err
		}
		return seedanceroute_setting.UpdateFromJSONString(string(raw))
	}, saved, calls
}

// 审计日志会写库，测试环境没有 DB；成功路径要能跑通就得把它注入成空实现。
func noopEcommerceAudit(_ *gin.Context, _ string, _ map[string]interface{}) {}

// ══════════════════════════════════════════════════════════════════
// 三路互斥：写入口保证「开一条自动关另两条」
// ══════════════════════════════════════════════════════════════════

// 打开 CapCut 分支时，心笔与有赞必须被显式关掉。
//
// ⚠️ 心笔那条必须写成**显式 false**（不是「不写」）：
// short_link_enabled 是三态指针，nil = 默认开启 ——
// 留 nil 的话读侧兜底会认为心笔还开着，CapCut 永远轮不到。
func TestSetShortCapcutLinkAdminTurnsOffOtherBranches(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_trigger_seconds":10,
		"short_link_enabled":true,"short_youzan_enabled":true}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/short-capcut-link",
		strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, saved, calls := ecommerceSaveStub(t)

	setShortCapcutLinkAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	assert.True(t, saved.ShortCapcutEnabled, "CapCut 分支应被打开")
	assert.False(t, saved.ShortYouzanEnabled, "有赞标准版应被自动关掉")
	require.NotNil(t, saved.ShortLinkEnabled, "心笔开关必须被写成显式值")
	assert.False(t, *saved.ShortLinkEnabled, "心笔内链应被自动关掉")

	assert.Equal(t, "cp-seedance-2.0fast", seedanceroute_setting.ActiveShortRouteTarget(),
		"当前生效方必须是 CapCut 快速档")
}

// 关闭 CapCut 分支时**不动**另外两条 —— 运营关掉一条往往只是想换一条。
func TestSetShortCapcutLinkAdminKeepsOtherBranchesWhenDisabled(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"short_target_model":"wan3.0-720p-特惠-内链","short_trigger_seconds":10,
		"short_link_enabled":true,"short_youzan_enabled":false,"short_capcut_enabled":true}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/short-capcut-link",
		strings.NewReader(`{"enabled":false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, saved, calls := ecommerceSaveStub(t)

	setShortCapcutLinkAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	assert.False(t, saved.ShortCapcutEnabled, "CapCut 分支应被关闭")
	require.NotNil(t, saved.ShortLinkEnabled)
	assert.True(t, *saved.ShortLinkEnabled, "关掉 CapCut 不该顺带关掉心笔")
}

// 打开规则七总开关：只改规则七，不碰其它经营配置。
func TestSetEcommerceRouteAdminEnables(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","trigger_seconds":20,
		"short_target_model":"wan3.0-720p-特惠-内链","short_trigger_seconds":10}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce",
		strings.NewReader(`{"enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, saved, calls := ecommerceSaveStub(t)

	setEcommerceRouteAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	assert.True(t, saved.EcommerceEnabled, "规则七应被打开")
	assert.Equal(t, "seedance-2-5", saved.TargetModel, "规则一目标不能被冲掉")
	assert.Equal(t, "wan3.0-720p-特惠-内链", saved.ShortTargetModel, "短时长目标不能被冲掉")
	assert.Equal(t, 20, saved.TriggerSeconds, "规则一阈值不能被冲掉")
}

// 勾选参数条件：两个布尔与阈值都要落进去。
func TestSetEcommerceConditionsAdminApplies(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5","trigger_seconds":20,"ecommerce_enabled":true}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce/conditions",
		strings.NewReader(`{"ratio_enabled":true,"seconds_enabled":true,"trigger_seconds":25}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, saved, calls := ecommerceSaveStub(t)

	setEcommerceConditionsAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	assert.True(t, saved.EcommerceRatioCondition)
	assert.True(t, saved.EcommerceSecondsCondition)
	assert.Equal(t, 25, saved.EcommerceTriggerSecondsValue)
	assert.True(t, saved.EcommerceEnabled, "总开关不能被条件写入冲掉")
}

// 校验失败时**一次都不能写库** —— 静默写进一个永远匹配不上的阈值
// 等于悄悄停掉整条规则。
func TestSetEcommerceConditionsAdminDoesNotWriteOnInvalid(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"ecommerce_enabled":true}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce/conditions",
		strings.NewReader(`{"ratio_enabled":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, _, calls := ecommerceSaveStub(t)

	setEcommerceConditionsAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, *calls, "校验失败不能写库")
}

// 关键词表写入：去重与去空白。
func TestSetEcommerceKeywordsAdminNormalizes(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_models":["seedance2.5-特惠"],
		"ecommerce_enabled":true}`)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ecommerce/keywords",
		strings.NewReader(`{"keywords":[" 好物 ","好物","","折扣"]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	save, saved, calls := ecommerceSaveStub(t)

	setEcommerceKeywordsAdmin(c, save, noopEcommerceAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	assert.Equal(t, []string{"好物", "折扣"}, saved.EcommercePromptKeywords)
}
