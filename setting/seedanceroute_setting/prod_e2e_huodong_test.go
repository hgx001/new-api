package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// 生产路由配置的基线快照（2026-11-11 形态）。
//
// 背景：seedance2.5-活动 上架后，需要确认它真的能命中条件路由，
// 而不是只有 abilities 行能调用。本文件把生产
// options.Seedance25TeHuiRoute30s 的**稳定字段**原样喂进来，逐条断言各规则。
//
// 2026-11-11 两处变更：
//   - 删除规则四（提示词 >5000 改派豆包），超长提示词改由压缩中间件处理；
//   - 规则二 9:16 从 wan3.0-720p-特惠 改为直连心笔 480P 内链
//     wan3.0-720p-特惠-内链（ch25）。
//
// ⚠️ 夹具里**不放** seconds_rule_paused_until / updated_at 这类随时间漂移的
// 字段：它们是运维状态而非配置，放进来会让断言随当前时间变化而假红。
// 改动生产 option 的稳定字段时必须同步改这里。
const prodRouteOptionJSON = `{
  "enabled": true,
  "source_model": "seedance2.5-特惠",
  "target_model": "seedance-2-5",
  "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
  "trigger_ratio": "9:16",
  "trigger_seconds": 20,
  "ratio_target_model": "wan3.0-720p-特惠-内链",
  "frames_target_model": "wan3.0-720p-特惠",
  "failover_source_model": "seedance-2-5",
  "failover_target_model": "wan3.0-班车",
  "failover_enabled": true,
  "source_overrides": {"seedance2.5-活动": {"target_model": "seedance-2-5"}}
}`

func TestProdOption_活动版命中源模型判定(t *testing.T) {
	require.NoError(t, UpdateFromJSONString(prodRouteOptionJSON))
	p := Get()

	// 总开关与核心字段必须全部就位
	require.True(t, p.Enabled, "生产 option 里 enabled 必须为 true，否则条件路由全停")
	require.Equal(t, 20, p.TriggerSeconds, "规则一触发时长必须是 20 秒（严格大于）")
	require.Equal(t, "seedance-2-5", p.TargetModel, "规则一目标模型")
	require.Equal(t, "9:16", p.TriggerRatio, "规则二触发比例")
	require.Equal(t, "wan3.0-720p-特惠-内链", p.RatioTargetModel, "规则二目标模型：心笔 480P 内链")
	require.Equal(t, "wan3.0-720p-特惠", p.FramesTargetModel, "规则三目标模型")

	// 母本与活动版都必须命中（并集口径）
	require.True(t, p.IsSourceModel(constant.Seedance25TeHuiModel),
		"母本必须命中（向后兼容硬要求）")
	require.True(t, p.IsSourceModel(constant.Seedance25HuoDongModel),
		"活动版必须命中 —— 它是促销档，必须共享母本的整套路由规则")

	// 非源模型不得命中
	require.False(t, p.IsSourceModel("seedance-2-5"))
	require.False(t, p.IsSourceModel("wan3.0-720p-特惠"))
	require.False(t, p.IsSourceModel(""))
}

// 各条规则的「配置就位」判定 —— 每条规则都有独立的启用条件，
// 少配一个字段该条规则就静默不生效（不报错）。
func TestProdOption_各条规则各自就位(t *testing.T) {
	require.NoError(t, UpdateFromJSONString(prodRouteOptionJSON))
	p := Get()

	// 规则一：秒数规则 —— 配全了（Enabled + 源模型 + 目标 + 触发时长）
	require.True(t, secondsRuleReady(p),
		"规则一必须就位：超过 20 秒的请求要改派到豆包")

	// 规则二：比例规则 —— 有目标模型 + 触发比例
	require.NotEmpty(t, p.RatioTargetModel, "规则二必须配目标模型")
	require.NotEmpty(t, p.TriggerRatio, "规则二必须配触发比例")
	require.True(t, ratioRuleReady(p), "规则二必须就位：9:16 要直连心笔内链")

	// 规则三：首尾帧规则 —— 有目标模型
	require.NotEmpty(t, p.FramesTargetModel, "规则三必须配目标模型")
	require.True(t, framesRuleReady(p), "规则三必须就位")

	// 规则五：故障转移 —— 独立源/目标 + 独立开关
	require.True(t, failoverRuleReady(p), "规则五必须就位：豆包挂了要能降级到班车")
}
