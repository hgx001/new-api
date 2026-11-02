package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// 生产实测：用**生产库 option 的真实 JSON** 验证活动版的路由能力。
//
// 背景（2026-11-02 上线实测）：seedance2.5-活动 上架后，
// 需要确认它真的能命中条件路由，而不是只有 abilities 行能调用。
// 本文件把生产 options.Seedance25TeHuiRoute30s 的真实内容原样喂进来，
// 逐条断言四条规则的源模型判定。
//
// ⚠️ 这份 JSON 是**生产实测值**，改动生产 option 时必须同步改这里。
const prodRouteOptionJSON = `{
  "enabled": true,
  "updated_at": 1793515704,
  "source_model": "seedance2.5-特惠",
  "target_model": "seedance-2-5",
  "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
  "trigger_ratio": "9:16",
  "trigger_seconds": 30,
  "ratio_target_model": "wan3.0-720p-特惠",
  "frames_target_model": "wan3.0-720p-特惠"
}`

func TestProdOption_活动版命中源模型判定(t *testing.T) {
	require.NoError(t, UpdateFromJSONString(prodRouteOptionJSON))
	p := Get()

	// 总开关与核心字段必须全部就位
	require.True(t, p.Enabled, "生产 option 里 enabled 必须为 true，否则四条规则全停")
	require.Equal(t, 30, p.TriggerSeconds, "规则一触发时长必须是 30 秒")
	require.Equal(t, "seedance-2-5", p.TargetModel, "规则一目标模型")
	require.Equal(t, "9:16", p.TriggerRatio, "规则二触发比例")
	require.Equal(t, "wan3.0-720p-特惠", p.RatioTargetModel, "规则二目标模型")
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

// 四条规则的「配置就位」判定 —— 每条规则都有独立的启用条件，
// 少配一个字段该条规则就静默不生效（不报错）。
func TestProdOption_四条规则各自就位(t *testing.T) {
	require.NoError(t, UpdateFromJSONString(prodRouteOptionJSON))
	p := Get()

	// 规则一：秒数规则 —— 配全了（Enabled + 源模型 + 目标 + 触发时长）
	require.True(t, secondsRuleReady(p),
		"规则一必须就位：30 秒请求要改派到漫屋，否则活动版零毛利")

	// 规则二：比例规则 —— 有目标模型 + 触发比例
	require.NotEmpty(t, p.RatioTargetModel, "规则二必须配目标模型")
	require.NotEmpty(t, p.TriggerRatio, "规则二必须配触发比例")

	// 规则三：首尾帧规则 —— 有目标模型
	require.NotEmpty(t, p.FramesTargetModel, "规则三必须配目标模型")

	// 规则四：无独立开关，跟随 Enabled
	require.True(t, p.Enabled, "规则四跟随总开关")
}
