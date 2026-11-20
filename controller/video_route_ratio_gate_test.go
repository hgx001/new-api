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

// 后台「可选比例条件」写入口的接口契约（2026-11-12 建，2026-11-20 起失效）。
//
// ⚠️ 2026-11-20 规则一改成「不带参考音视频就全部改派」后，这个闸门不再参与
// 任何判定（middleware.secondsRatioGatePasses 已删除）。接口保留只为兼容
// 老脚本：**写得进去、但没有任何效果**，管理状态也不再回显它。
//
// 因此这里锁死的不再是「改派收窄」，而是三件与兼容有关的事：
//   - 少传 enabled 或比例写错仍必须 400，且一次都不写库（别让老脚本静默写脏值）；
//   - 合法请求仍能落库（历史配置链路还在，回退口径时不用重写）；
//   - 管理状态**不再下发**这两个字段 —— 继续下发会让下一个人以为 9:16 还生效。

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

// 勾上：仍能写进 9:16（兼容老脚本），但管理状态不再回显 —— 字段已下线。
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
	require.Empty(t, ratioGateStateFromBody(t, recorder),
		"闸门已下线，管理状态不应再回显（前端也没有读它的地方了）")
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
}

// 管理状态必须**不再**下发闸门：配置里还留着 9:16，也不能让前端看到，
// 否则下一个人会以为这条条件还在起作用（2026-11-20 口径变更）。
func TestDoubaoRouteAdminStateOmitsSecondsRuleRatioGate(t *testing.T) {
	withAdminRoutePolicy(t, `{
		"enabled": true, "source_model": "seedance2.5-特惠",
		"target_model": "seedance-2-5", "trigger_seconds": 20,
		"seconds_rule_ratio_gate": "9:16"
	}`)

	state := doubaoRouteAdminState()

	_, hasGate := state["seconds_rule_ratio_gate"]
	_, hasDefault := state["seconds_rule_ratio_gate_default"]
	require.False(t, hasGate, "闸门字段已下线，不应继续下发")
	require.False(t, hasDefault, "闸门默认值同样不再下发")
}

// trigger_seconds 仍必须下发：它虽不再参与阈值比较，但仍是
// 「规则一配全了没有」的哨兵（secondsRuleConfiguredFor 要求 > 0）。
func TestDoubaoRouteAdminStateStillReportsTriggerSeconds(t *testing.T) {
	withAdminRoutePolicy(t, ratioGateAdminBaseline)

	state := doubaoRouteAdminState()

	require.Equal(t, 20, state["trigger_seconds"])
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
