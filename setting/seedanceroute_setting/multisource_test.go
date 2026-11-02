package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 路由规则的**多源化**（2026-11-02，seedance2.5-活动 上架）。
//
// 背景：条件路由原本只认**一个**源模型名（SourceModel = seedance2.5-特惠）。
// 活动版 seedance2.5-活动 是同一个商品的促销档，必须**继承同一套路由规则** ——
// 尤其 30 秒规则：活动版走有赞的成本是 ¥4.50、卖 ¥4.50（零毛利），
// 不路由就是白干；路由到豆包后成本 ≈ ¥0，毛利 ¥4.50。
//
// 所以 SourceModel 要能表达**多个**源模型。
//
// ⚠️ 向后兼容是硬要求：生产 options 里存的是 `"source_model": "seedance2.5-特惠"`，
// 旧字段必须继续生效，不能因为加了个新字段就把现有路由弄失效。

// IsSourceModel 判定「这个模型名是不是本策略的源模型」。
//
// 三条要求：
//  1. 老配置（只配 source_model）必须继续命中；
//  2. 新配置（配 source_models 数组）能命中多个；
//  3. 两个字段都配时，**并集**生效（不能是「后者覆盖前者」）。
func TestPolicyIsSourceModel(t *testing.T) {
	// 老配置：只有 source_model
	legacy := Policy{Enabled: true, SourceModel: constant.Seedance25TeHuiModel}
	require.True(t, legacy.IsSourceModel(constant.Seedance25TeHuiModel),
		"老配置只有 source_model，必须继续命中（向后兼容是硬要求）")
	require.False(t, legacy.IsSourceModel(constant.Seedance25HuoDongModel),
		"老配置不该命中别的模型")

	// 新配置：source_models 数组
	multi := Policy{Enabled: true, SourceModels: []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	}}
	require.True(t, multi.IsSourceModel(constant.Seedance25TeHuiModel))
	require.True(t, multi.IsSourceModel(constant.Seedance25HuoDongModel),
		"新配置必须能命中活动版")
	require.False(t, multi.IsSourceModel("seedance-2-5"),
		"不在列表里的模型不得命中")

	// 两个字段都配：取并集
	both := Policy{
		Enabled:     true,
		SourceModel: constant.Seedance25TeHuiModel,
		SourceModels: []string{
			constant.Seedance25HuoDongModel,
		},
	}
	require.True(t, both.IsSourceModel(constant.Seedance25TeHuiModel),
		"两个字段都配时 source_model 必须仍然生效")
	require.True(t, both.IsSourceModel(constant.Seedance25HuoDongModel),
		"两个字段都配时 source_models 必须也生效（并集，不是覆盖）")
}

// 匹配必须容忍首尾空格 —— 生产上真出现过「 seedance2.5-特惠」。
func TestPolicyIsSourceModelTrimsSpace(t *testing.T) {
	p := Policy{Enabled: true, SourceModels: []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	}}
	require.True(t, p.IsSourceModel(" "+constant.Seedance25HuoDongModel+" "),
		"模型名带首尾空格时必须仍然命中")
	require.True(t, p.IsSourceModel(" "+constant.Seedance25TeHuiModel),
		"单值字段也要容忍首尾空格")
}

// 配置里写的模型名本身带空格时，匹配也必须容忍（不能只容忍请求侧）。
func TestPolicyIsSourceModelTrimsConfiguredValue(t *testing.T) {
	p := Policy{Enabled: true, SourceModels: []string{" " + constant.Seedance25HuoDongModel + " "}}
	require.True(t, p.IsSourceModel(constant.Seedance25HuoDongModel),
		"配置里的模型名带空格时也必须能匹配上")
}

// 规则「配全」的判定必须把多源算进去 ——
// 否则只配了 source_models（没配 source_model）的策略会被判成「没配全」→ 路由被静默关掉。
//
// 这是最容易踩的坑：改配置的人按新格式配了 source_models，
// 结果 UpdateFromJSONString 判定 Enabled=false 把它整条关掉，
// 而且**没有任何报错** —— 路由静默失效，30 秒请求全走有赞（活动版零毛利）。
func TestMultiSourcePolicyPassesReadyChecks(t *testing.T) {
	onlyMulti := Policy{
		Enabled:        true,
		SourceModels:   []string{constant.Seedance25TeHuiModel, constant.Seedance25HuoDongModel},
		TargetModel:    "seedance-2-5",
		TriggerSeconds: 30,
	}
	require.True(t, secondsRuleReady(onlyMulti),
		"只配 source_models 也必须算「规则一配全」，否则路由会被静默关掉")
	require.True(t, ratioRuleReady(onlyMulti) == false,
		"只配了规则一字段时，规则二不该被判定为配全（比例目标模型为空）")
	require.True(t, framesRuleReady(onlyMulti) == false,
		"只配了规则一字段时，规则三不该被判定为配全（首尾帧目标模型为空）")

	// 规则二/规则三同理：源模型名的多源化不能让它们误判。
	ratioMulti := Policy{
		Enabled:          true,
		SourceModels:     []string{constant.Seedance25HuoDongModel},
		RatioTargetModel: "wan3.0-720p-特惠",
		TriggerRatio:     "9:16",
	}
	require.True(t, ratioRuleReady(ratioMulti),
		"多源配置下规则二也必须算配全")

	framesMulti := Policy{
		Enabled:           true,
		SourceModels:      []string{constant.Seedance25HuoDongModel},
		FramesTargetModel: "wan3.0-720p-特惠",
	}
	require.True(t, framesRuleReady(framesMulti),
		"多源配置下规则三也必须算配全")
}

// UpdateFromJSONString 必须接受 source_models 并保留它（写回 option 时不能丢）。
func TestUpdateFromJSONStringKeepsSourceModels(t *testing.T) {
	previous := Get()
	t.Cleanup(func() {
		restored, _ := marshalPolicy(previous)
		_ = UpdateFromJSONString(restored)
	})

	payload := `{"enabled":true,"source_models":["seedance2.5-特惠","seedance2.5-活动"],` +
		`"target_model":"seedance-2-5","trigger_seconds":30}`
	require.NoError(t, UpdateFromJSONString(payload))

	got := Get()
	assert.True(t, got.Enabled, "多源配置必须能开启路由")
	assert.Len(t, got.SourceModels, 2, "source_models 必须被保留")
	assert.True(t, got.IsSourceModel(constant.Seedance25TeHuiModel))
	assert.True(t, got.IsSourceModel(constant.Seedance25HuoDongModel),
		"活动版必须被多源策略命中")
}

// 序列化往返不能丢字段 —— 运维改配置时走的是「读 JSON → 改 → 写回」，
// 少一个字段就等于把多源降级回单源，而且无感知。
func TestMarshalRoundTripKeepsSourceModels(t *testing.T) {
	previous := Get()
	t.Cleanup(func() {
		restored, _ := marshalPolicy(previous)
		_ = UpdateFromJSONString(restored)
	})

	payload := `{"enabled":true,"source_model":"seedance2.5-特惠",` +
		`"source_models":["seedance2.5-特惠","seedance2.5-活动"],` +
		`"target_model":"seedance-2-5","trigger_seconds":30}`
	require.NoError(t, UpdateFromJSONString(payload))

	encoded := MarshalJSONString()
	assert.Contains(t, encoded, "source_models",
		"序列化结果必须包含 source_models，否则运维改一次配置就丢多源")
	assert.Contains(t, encoded, "seedance2.5-活动",
		"序列化结果必须包含活动版")

	require.NoError(t, UpdateFromJSONString(encoded))
	assert.True(t, Get().IsSourceModel(constant.Seedance25HuoDongModel),
		"往返后仍必须命中活动版")
}

// marshalPolicy 是测试辅助：把策略序列化成 JSON 字符串。
func marshalPolicy(p Policy) (string, error) {
	encoded, err := common.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
