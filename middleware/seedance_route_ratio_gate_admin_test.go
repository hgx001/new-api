package middleware

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/require"
)

// 后台「可选比例条件」勾选的跨层闭环（2026-11-12）。
//
// 三层各自都有测试：配置写入（setting）、接口契约（controller）、
// 闸门判定（seedance_route_test.go 的 TestSeedanceRouteRatioGate*）。
// 但「后台勾一下，真机上的改派行为就跟着变」这条链没被串起来测过 ——
// 中间任何一层把字段名写错（比如写成 ratio_gate），三层测试各自仍绿，
// 功能却完全不生效。所以这里用后台同一个纯函数写配置，再跑真实中间件。

const (
	ratioGateAdminBody16x9 = `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"16:9"},"prompt":"a cat"}`
	ratioGateAdminBody9x16 = `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`
)

// writeRatioGateFromAdmin 模拟后台勾选的完整落库链：
// 用同一个纯函数生成策略 → 序列化 → 走 option 分发链更新内存。
func writeRatioGateFromAdmin(t *testing.T, enabled bool) {
	t.Helper()
	policy, err := seedanceroute_setting.WithSecondsRuleRatioGate(enabled, "")
	require.NoError(t, err)
	raw, err := common.Marshal(policy)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(raw)))
}

func routedToTarget(t *testing.T, body string) bool {
	t.Helper()
	seen, _ := runRoute(t, body)
	var out map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(seen), &out))
	return out["model"] == testTargetModel
}

// 未勾选（旧配置）→ 勾上 → 取消，三个阶段的改派行为必须依次为：
// 不限比例 / 只有 9:16 / 又回到不限比例。
// 管理接口仍能写 seconds_rule_ratio_gate（兼容老前端 / 老脚本），
// 但规则一自 2026-11-20 起不再读它 —— 写了也是无效配置。
//
// 这条用例把「接口照常成功、路由不受影响」钉死：日后如果有人想把闸门
// 复活，必须同时改这里，不可能悄悄恢复生效。
func TestAdminRatioGateToggleControlsReroute(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	require.True(t, routedToTarget(t, ratioGateAdminBody16x9),
		"基线：16:9 无音视频请求应改派")

	writeRatioGateFromAdmin(t, true)
	require.Equal(t, "9:16", seedanceroute_setting.Get().SecondsRuleRatioGate,
		"管理接口仍应把字段写进策略（仅作留档）")
	require.True(t, routedToTarget(t, ratioGateAdminBody16x9),
		"闸门已撤销：勾上之后 16:9 仍要改派")
	require.True(t, routedToTarget(t, ratioGateAdminBody9x16),
		"9:16 当然也照旧改派")

	writeRatioGateFromAdmin(t, false)
	require.Empty(t, seedanceroute_setting.Get().SecondsRuleRatioGate)
	require.True(t, routedToTarget(t, ratioGateAdminBody16x9),
		"取消勾选不影响任何路由")
}
