package middleware

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 豆包链路开关的路由行为（2026-11-11）。
//
// 开关 = 「豆包通不通」这一个事实：
//
//   - 关闭：规则一停用 —— 特惠/活动超时长请求**留在常驻链路**（一个字节不动）；
//     故障转移强制生效 —— seedance-2-5 全部改派 wan3.0-班车；
//   - 开启（或到点自动恢复）：规则一照常把超时长请求改派豆包。
//
// 每条「不路由」的用例都由同文件的「应该路由」用例证明中间件确实挂着。

// linkBasePayload 构造规则一 + 规则五配全的策略 map。
func linkBasePayload() map[string]interface{} {
	return map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":          testTargetModel,
		"trigger_seconds":       20,
		"failover_enabled":      true,
		"failover_source_model": failoverSourceModel,
		"failover_target_model": failoverBackupModel,
	}
}

// applyLinkPolicy 用一份 map 覆盖当前策略，测试结束恢复（与其它策略测试同口径）。
func applyLinkPolicy(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 关闭开关：所有需要路由到豆包的请求都留在常驻链路。
// 故意让所有渠道都可用 —— 不路由只能是开关导致的，不能是渠道缺失的副作用。
func TestDoubaoLinkOffKeepsPromoRequestsOnResidentChannel(t *testing.T) {
	applyLinkPolicy(t, withLinkOff(linkBasePayload(), time.Now().Unix()+3600))
	stubChannelAvailableByModel(t, map[string]bool{
		"seedance2.5-特惠":    true,
		"seedance2.5-活动":    true,
		testTargetModel:     true,
		failoverBackupModel: true,
	})

	// 特惠按秒计费，30 秒超阈值 → 开关关闭必须留在有赞常驻。
	body := `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`
	seen, origin := runFailover(t, body)
	assert.Equal(t, body, seen, "特惠超时长请求必须原样留在常驻有赞链路")
	assert.Empty(t, origin)

	// 活动按次固定 30 秒，同样超阈值 → 留在心笔常驻。
	body = `{"model":"seedance2.5-活动","seconds":30,"prompt":"a cat"}`
	seen, origin = runFailover(t, body)
	assert.Equal(t, body, seen, "活动超时长请求必须原样留在常驻心笔链路")
	assert.Empty(t, origin)
}

// 关闭开关：seedance-2-5 全部改派班车 —— 即使豆包渠道本身可用，
// 证明改派由开关驱动，而不是渠道状态的巧合。
func TestDoubaoLinkOffRoutesDoubaoModelToBackup(t *testing.T) {
	applyLinkPolicy(t, withLinkOff(linkBasePayload(), time.Now().Unix()+3600))
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true, // 豆包渠道可用也照样改派
		failoverBackupModel: true,
		"seedance2.5-特惠":    true,
	})

	body := `{"model":"seedance-2-5","seconds":5,"prompt":"a cat"}`
	seen, origin := runFailover(t, body)
	assertModelRouted(t, seen, failoverBackupModel)
	assert.Equal(t, failoverSourceModel, origin)
}

// 关闭开关：即使规则五总开关被人关了，改派也必须生效 ——
// 豆包链路开关是唯一事实源，不能被别的开关静默挡住。
func TestDoubaoLinkOffBypassesFailoverMasterSwitch(t *testing.T) {
	payload := linkBasePayload()
	payload["failover_enabled"] = false
	applyLinkPolicy(t, withLinkOff(payload, time.Now().Unix()+3600))
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`
	seen, origin := runFailover(t, body)
	assertModelRouted(t, seen, failoverBackupModel)
	assert.Equal(t, failoverSourceModel, origin)
}

// 截止时间已过（凌晨 2 点额度恢复）= 自动恢复开启：
// 规则一照常改派豆包，故障转移不再因开关触发。
func TestDoubaoLinkAutoRecoversRuleOne(t *testing.T) {
	applyLinkPolicy(t, withLinkOff(linkBasePayload(), time.Now().Unix()-1))
	stubChannelAvailableByModel(t, map[string]bool{
		testTargetModel:     true, // == failoverSourceModel，同键只写一次
		failoverBackupModel: true,
	})

	body := `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`
	seen, origin := runFailover(t, body)
	assertModelRouted(t, seen, testTargetModel)
	assert.Equal(t, testSourceModel, origin)
}

// withLinkOff 在策略 map 上附加豆包链路关闭截止时间。
func withLinkOff(payload map[string]interface{}, until int64) map[string]interface{} {
	payload["doubao_link_off_until"] = until
	return payload
}
