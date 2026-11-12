package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 后台「可选比例条件」勾选的接口契约（2026-11-12）。
//
// 勾上 = 只有指定比例的请求才走规则一改派；取消 = 清空闸门、恢复不限比例。
// 这条线此前只能靠 SQL 改 options 表，运营看不到也改不动。
//
// 重点锁死两件事：
//   - 少传 enabled 或比例写错必须 400，且**一次都不能写库**；
//     静默写进一个永远匹配不上的比例值，等于悄悄停掉整条改派链；
//   - 接口返回的管理状态必须回显新闸门，前端才能立刻把勾选画成已勾上。

const ratioGateAdminBaseline = `{
	"enabled": true,
	"source_model": "seedance2.5-特惠",
	"target_model": "seedance-2-5",
	"trigger_seconds": 20,
	"ratio_target_model": "wan3.0-720p-特惠",
	"trigger_ratio": "9:16",
	"frames_target_model": "wan3.0-720p-特惠"
}`

func newRatioGateRequest(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/ratio-gate", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

// ratioGateSaveStub 模拟 model.UpdateOption 的分发链（写库 + 更新内存状态），
// 让测试能覆盖成功路径而不依赖数据库。第三个返回值是写库次数，
// 校验失败时必须一次都没调。
func ratioGateSaveStub(t *testing.T) (func(seedanceroute_setting.Policy) error, *seedanceroute_setting.Policy, *int) {
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
func noopRatioGateAudit(_ *gin.Context, _ string, _ map[string]interface{}) {}

// 少传 enabled 必须拒绝：nil bool 当 false 处理，
// 一次前端字段丢失就会把刚勾上的条件悄悄取消。
func TestSetSecondsRuleRatioGateAdminRejectsMissingEnabled(t *testing.T) {
	withAdminRoutePolicy(t, ratioGateAdminBaseline)
	c, recorder := newRatioGateRequest(t, `{"ratio":"9:16"}`)
	save, _, calls := ratioGateSaveStub(t)

	setSecondsRuleRatioGateAdmin(c, save, noopRatioGateAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "enabled")
	require.Zero(t, *calls, "校验失败不能写库")
}

// 比例写错必须拒绝，且不能写库。
func TestSetSecondsRuleRatioGateAdminRejectsInvalidRatio(t *testing.T) {
	withAdminRoutePolicy(t, ratioGateAdminBaseline)
	c, recorder := newRatioGateRequest(t, `{"enabled":true,"ratio":"这不是比例"}`)
	save, _, calls := ratioGateSaveStub(t)

	setSecondsRuleRatioGateAdmin(c, save, noopRatioGateAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "ratio")
	require.Zero(t, *calls, "非法比例不能写库")
}

func TestSetSecondsRuleRatioGateAdminRejectsMalformedJSON(t *testing.T) {
	withAdminRoutePolicy(t, ratioGateAdminBaseline)
	c, recorder := newRatioGateRequest(t, `{`)
	save, _, calls := ratioGateSaveStub(t)

	setSecondsRuleRatioGateAdmin(c, save, noopRatioGateAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, *calls, "解析失败不能写库")
}

// 勾上：写进 9:16，返回的状态里也要带 9:16（前端据此立即刷新勾选）。
func TestSetSecondsRuleRatioGateAdminEnables(t *testing.T) {
	withAdminRoutePolicy(t, ratioGateAdminBaseline)
	c, recorder := newRatioGateRequest(t, `{"enabled":true}`)
	save, saved, calls := ratioGateSaveStub(t)

	setSecondsRuleRatioGateAdmin(c, save, noopRatioGateAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	require.Equal(t, "9:16", saved.SecondsRuleRatioGate)
	require.Equal(t, "9:16", saved.TriggerRatio, "规则二的触发比例不能被顺手改掉")
	require.Equal(t, "wan3.0-720p-特惠", saved.RatioTargetModel, "其它经营配置不能丢")
	require.Equal(t, "9:16", ratioGateStateFromBody(t, recorder))
}

// 取消：清空闸门，且不校验传进来的比例值（运营关条件不需要先填比例）。
func TestSetSecondsRuleRatioGateAdminDisables(t *testing.T) {
	withAdminRoutePolicy(t, `{
		"enabled": true, "source_model": "seedance2.5-特惠",
		"target_model": "seedance-2-5", "trigger_seconds": 20,
		"seconds_rule_ratio_gate": "9:16"
	}`)
	c, recorder := newRatioGateRequest(t, `{"enabled":false,"ratio":"随便什么都行"}`)
	save, saved, calls := ratioGateSaveStub(t)

	setSecondsRuleRatioGateAdmin(c, save, noopRatioGateAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	require.Empty(t, saved.SecondsRuleRatioGate)
	require.Equal(t, "", ratioGateStateFromBody(t, recorder))
}

// 管理状态本身必须回显闸门：卡片刷新时读的就是这个字段。
func TestDoubaoRouteAdminStateReportsSecondsRuleRatioGate(t *testing.T) {
	withAdminRoutePolicy(t, `{
		"enabled": true, "source_model": "seedance2.5-特惠",
		"target_model": "seedance-2-5", "trigger_seconds": 20,
		"seconds_rule_ratio_gate": "9:16"
	}`)

	state := doubaoRouteAdminState()

	require.Equal(t, "9:16", state["seconds_rule_ratio_gate"])
}

func ratioGateStateFromBody(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()
	var payload struct {
		Data struct {
			SecondsRuleRatioGate string `json:"seconds_rule_ratio_gate"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data.SecondsRuleRatioGate
}
