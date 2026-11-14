package controller

// 规则五备选备胎（CapCut 2.5 Flash）管理接口的契约测试（2026-11-14）。
//
// 要锁死的三件事：
//
//  1. 缺字段 / 坏 JSON 必须 400 且**一次都不写库**。
//     备选开关是「豆包挂了走哪条」的唯一控制点，一次误写就会把线上退路
//     从班车换成 CapCut（或反过来），而出问题的时候正是豆包已经挂了的时候。
//  2. 开关与模型名互不覆盖。历史上一堆 With* 系列是「从全局快照 clone」，
//     连着调两个就会丢前一个改动 —— 这条线用 ApplyFailoverAltModel 避开，
//     测试必须证明它确实没被覆盖。
//  3. 状态快照要能回答「开关开了但为什么没生效」：effective 只有在
//     开关 + 模型名 + 超分配置 + 渠道可用 四项全满足时才为 true，
//     且 note 必须指出缺的是哪一项。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

func newFailoverAltRequest(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/video-route/policy/failover/alt", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

// failoverAltSaveStub 模拟 model.UpdateOption 的分发链（写库 + 更新内存状态），
// 让测试能覆盖成功路径而不依赖数据库。第三个返回值是写库次数。
func failoverAltSaveStub(t *testing.T) (func(seedanceroute_setting.Policy) error, *seedanceroute_setting.Policy, *int) {
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

func noopFailoverAltAudit(_ *gin.Context, _ string, _ map[string]interface{}) {}

// stubFailoverAltProbes 把「渠道可用」「超分已配」两个外部事实替换成常量。
//
// 这两个探针在生产分别读渠道表与 MPS 配置；单测没有 DB，真实实现会让
// 状态里的 available/upscaleReady 恒为 false，那样就测不出 effective 的组合逻辑。
func stubFailoverAltProbes(t *testing.T, channelAvailable bool) {
	t.Helper()
	previousProbe := failoverChannelProbe
	failoverChannelProbe = func(*gin.Context, string) bool { return channelAvailable }
	t.Cleanup(func() { failoverChannelProbe = previousProbe })
}

// stubSuperResolutionConfigured 临时把腾讯 MPS 配置改成「配好了 / 没配」。
func stubSuperResolutionConfigured(t *testing.T, configured bool) {
	t.Helper()
	previous := tencentmps_setting.MarshalJSONString()
	raw := `{"enabled":false}`
	if configured {
		raw = `{
			"enabled": true,
			"secret_id": "sid", "secret_key": "skey",
			"region": "ap-guangzhou", "bucket": "b-1250000000",
			"output_dir": "upscale/"
		}`
	}
	require.NoError(t, tencentmps_setting.UpdateFromJSONString(raw))
	t.Cleanup(func() { _ = tencentmps_setting.UpdateFromJSONString(previous) })
}

// 缺 enabled 必须拒绝：nil 当 false 处理会悄悄把退路切回班车。
func TestSetFailoverAltAdminRejectsMissingEnabled(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"failover_source_model":"seedance-2-5"}`)
	c, recorder := newFailoverAltRequest(t, `{"model":"cp-seedance-2.5flash"}`)
	save, _, calls := failoverAltSaveStub(t)

	setFailoverAltAdmin(c, save, noopFailoverAltAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "enabled")
	require.Zero(t, *calls, "校验失败不能写库")
}

func TestSetFailoverAltAdminRejectsMalformedJSON(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true}`)
	c, recorder := newFailoverAltRequest(t, `{`)
	save, _, calls := failoverAltSaveStub(t)

	setFailoverAltAdmin(c, save, noopFailoverAltAudit)

	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Zero(t, *calls, "解析失败不能写库")
}

// 打开：开关落库，返回的状态里也是打开，且**不动**其它经营配置。
func TestSetFailoverAltAdminEnables(t *testing.T) {
	stubFailoverAltProbes(t, true)
	withAdminRoutePolicy(t, `{
		"enabled": true,
		"source_model": "seedance2.5-特惠",
		"target_model": "seedance-2-5",
		"trigger_seconds": 9,
		"failover_enabled": true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车"
	}`)
	c, recorder := newFailoverAltRequest(t, `{"enabled":true}`)
	save, saved, calls := failoverAltSaveStub(t)

	setFailoverAltAdmin(c, save, noopFailoverAltAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	require.True(t, saved.FailoverAltEnabled)
	require.Equal(t, "", saved.FailoverAltModel, "没传 model 就不该写入模型名")
	require.Equal(t, "wan3.0-班车", saved.FailoverTargetModel, "班车这条退路不能被动")
	require.True(t, saved.FailoverEnabled)
	require.Equal(t, 9, saved.TriggerSeconds)

	state := failoverAltStateFromBody(t, recorder)
	require.Equal(t, true, state["failover_alt_enabled"])
	require.Equal(t, seedanceroute_setting.DefaultFailoverAltModel, state["failover_alt_target_model"])
}

// 关掉：开关落库为 false，且**不碰**已配好的模型名。
//
// 「关开关顺手清空模型名」会让下次打开时落回默认值 —— 如果运营改成过别的名字，
// 他的配置就被悄悄改掉了。
func TestSetFailoverAltAdminDisablesKeepsConfiguredModel(t *testing.T) {
	stubFailoverAltProbes(t, false)
	withAdminRoutePolicy(t, `{
		"failover_alt_enabled": true,
		"failover_alt_model": "cp-seedance-2.5flash"
	}`)
	c, recorder := newFailoverAltRequest(t, `{"enabled":false}`)
	save, saved, calls := failoverAltSaveStub(t)

	setFailoverAltAdmin(c, save, noopFailoverAltAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	require.False(t, saved.FailoverAltEnabled)
	require.Equal(t, "cp-seedance-2.5flash", saved.FailoverAltModel, "关开关不能清模型名")

	state := failoverAltStateFromBody(t, recorder)
	require.Equal(t, false, state["failover_alt_enabled"])
	require.Equal(t, false, state["failover_alt_effective"])
}

// 同时传开关与模型名：两个改动都要落库（这是 Apply 版本存在的理由）。
func TestSetFailoverAltAdminAppliesSwitchAndModelTogether(t *testing.T) {
	stubFailoverAltProbes(t, true)
	withAdminRoutePolicy(t, `{"failover_enabled": true}`)
	c, recorder := newFailoverAltRequest(t, `{"enabled":true,"model":"  cp-seedance-2.5flash  "}`)
	save, saved, calls := failoverAltSaveStub(t)

	setFailoverAltAdmin(c, save, noopFailoverAltAudit)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, 1, *calls)
	require.True(t, saved.FailoverAltEnabled, "开关不能被后一次 model 改动覆盖回旧值")
	require.Equal(t, "cp-seedance-2.5flash", saved.FailoverAltModel, "模型名要去空白")
	require.True(t, saved.FailoverEnabled, "规则五主开关不受影响")
}

// 超分没配：开关能开，但 effective 必须是 false，note 要指向超分。
//
// 这种情况如果 effective 报 true，运营会以为豆包挂了就走 CapCut，
// 实际请求会静默走班车 —— 而他看界面「已打开」，根本不会去查。
func TestFailoverAltAdminStateReportsUpscaleMissing(t *testing.T) {
	stubFailoverAltProbes(t, true)
	stubSuperResolutionConfigured(t, false)
	withAdminRoutePolicy(t, `{"failover_alt_enabled": true}`)
	c, _ := newFailoverAltRequest(t, "")

	state := failoverAltAdminState(c)

	require.Equal(t, true, state["failover_alt_enabled"])
	require.Equal(t, false, state["failover_alt_upscale_configured"])
	require.Equal(t, false, state["failover_alt_effective"])
	require.Contains(t, state["failover_alt_note"], "超分")
}

// 渠道没了：同样是「开着但没生效」，note 要指向渠道。
func TestFailoverAltAdminStateReportsChannelMissing(t *testing.T) {
	stubFailoverAltProbes(t, false)
	stubSuperResolutionConfigured(t, true)
	withAdminRoutePolicy(t, `{"failover_alt_enabled": true}`)
	c, _ := newFailoverAltRequest(t, "")

	state := failoverAltAdminState(c)

	require.Equal(t, false, state["failover_alt_target_available"])
	require.Equal(t, false, state["failover_alt_effective"])
	require.Contains(t, state["failover_alt_note"], "渠道")
}

// 四项全满足：effective=true，note 要写清「480p 生成 + 超分 720P」，
// 这是运营对客承诺的交付档位，不能只写一句「已打开」。
func TestFailoverAltAdminStateEffectiveWhenAllReady(t *testing.T) {
	stubFailoverAltProbes(t, true)
	stubSuperResolutionConfigured(t, true)
	withAdminRoutePolicy(t, `{"failover_alt_enabled": true}`)
	c, _ := newFailoverAltRequest(t, "")

	state := failoverAltAdminState(c)

	require.Equal(t, true, state["failover_alt_target_supported"])
	require.Equal(t, true, state["failover_alt_target_available"])
	require.Equal(t, true, state["failover_alt_upscale_configured"])
	require.Equal(t, true, state["failover_alt_effective"])
	require.Contains(t, state["failover_alt_note"], "480p")
	require.Contains(t, state["failover_alt_note"], "720P")
}

// 配成别的模型名：本链路不生效（middleware 按名字判契约），不能假装能跑。
func TestFailoverAltAdminStateReportsUnsupportedModel(t *testing.T) {
	stubFailoverAltProbes(t, true)
	stubSuperResolutionConfigured(t, true)
	withAdminRoutePolicy(t, `{
		"failover_alt_enabled": true,
		"failover_alt_model": "cp-seedance-2.0fast"
	}`)
	c, _ := newFailoverAltRequest(t, "")

	state := failoverAltAdminState(c)

	require.Equal(t, "cp-seedance-2.0fast", state["failover_alt_model"])
	require.Equal(t, false, state["failover_alt_target_supported"])
	require.Equal(t, false, state["failover_alt_effective"])
	require.Contains(t, state["failover_alt_note"], "不是 cp-seedance-2.5flash")
}

// 备选状态必须合并进规则五那份快照：前端只拉一次 /policy/failover
// 就要能画出「班车 / CapCut / 都不走」三选一。
func TestFailoverAdminStateIncludesAltFields(t *testing.T) {
	stubFailoverAltProbes(t, true)
	stubSuperResolutionConfigured(t, true)
	withAdminRoutePolicy(t, `{
		"enabled": true,
		"failover_enabled": true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车",
		"failover_alt_enabled": true
	}`)
	c, _ := newFailoverAltRequest(t, "")

	state := failoverAdminState(c)

	require.Equal(t, true, state["failover_enabled"])
	require.Equal(t, true, state["failover_alt_enabled"])
	require.Equal(t, true, state["failover_alt_effective"])
	require.Equal(t, seedanceroute_setting.DefaultFailoverAltModel, state["failover_alt_target_model"])
}

func failoverAltStateFromBody(t *testing.T, recorder *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var payload struct {
		Data map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	return payload.Data
}
