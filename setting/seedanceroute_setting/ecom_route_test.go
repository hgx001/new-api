package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则七（电商/产品推荐内容 → CapCut 快速档）的配置层测试（2026-11-14）。
//
// 老板口径：
//   - seedance2.5-特惠 / seedance2.5-活动 识别到电商类或产品推荐提示词 → 改派
//     cp-seedance-2.5flash；
//   - 带参考视频的请求不路由（中间件层的事，这里不涉及）；
//   - 9:16 也路由过去（独立勾选）；
//   - 还可勾选「时长大于 N 秒」作为额外触发入口。
//
// 本文件锁六件事：
//  1. 没配 ecommerce_enabled = 默认关闭（升级当天线上流量逐字节不变）；
//  2. 关键词判定是**大小写不敏感的子串匹配**，空表 = 不命中；
//  3. 内容入口与参数入口是「或」关系 —— 这条在中间件层有测试，
//     这里锁的是「勾选状态能被读出来」；
//  4. 三路短时长分支的互斥辅助函数（Apply*）能一次改三个字段；
//  5. 只配规则七 / 只配 CapCut 分支也算「策略可用」，中间件不被短路；
//  6. 参数条件的非法阈值被拒绝（而不是写进去当死配置）。

// ecommercePolicy 是规则七的最小可用配置。
func ecommercePolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 20,
	}
}

// 没配 ecommerce_enabled 时默认关闭。
func TestEcommerceDefaultsOff(t *testing.T) {
	applyPolicy(t, ecommercePolicy())

	assert.False(t, IsEcommerceRouteEnabled(), "没配 ecommerce_enabled 时规则七应默认关闭")
	assert.False(t, EcommerceEnabledFor("seedance2.5-特惠"), "默认关闭时不该对源模型生效")
	assert.False(t, EcommerceEnabledFor("seedance2.5-活动"), "默认关闭时不该对源模型生效")
}

// 打开后对两个源模型都生效。
func TestEcommerceToggleEnablesForBothSources(t *testing.T) {
	payload := ecommercePolicy()
	payload["ecommerce_enabled"] = true
	applyPolicy(t, payload)

	assert.True(t, IsEcommerceRouteEnabled(), "显式 true 后规则七应开启")
	assert.True(t, EcommerceEnabledFor("seedance2.5-特惠"), "母本应生效")
	assert.True(t, EcommerceEnabledFor("seedance2.5-活动"), "活动版应生效")
}

// 按源模型的专属开关能单独关掉一个源模型。
//
// 三态指针的语义：没配 = 默认开启（向后兼容），显式 false = 该源模型不走。
func TestEcommercePerSourceOverride(t *testing.T) {
	payload := ecommercePolicy()
	payload["ecommerce_enabled"] = true
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{"ecommerce_rule_enabled": false},
	}
	applyPolicy(t, payload)

	assert.True(t, EcommerceEnabledFor("seedance2.5-特惠"), "母本不该被活动版的开关影响")
	assert.False(t, EcommerceEnabledFor("seedance2.5-活动"), "显式关掉后活动版不该生效")
}

// 没配 source_overrides 时按源模型的开关默认开启（升级当天行为不变）。
func TestEcommercePerSourceOverrideDefaultsOn(t *testing.T) {
	payload := ecommercePolicy()
	payload["ecommerce_enabled"] = true
	applyPolicy(t, payload)

	assert.True(t, EcommerceEnabledFor("seedance2.5-特惠"))
	assert.True(t, EcommerceEnabledFor("seedance2.5-活动"))
}

// 关键词判定：大小写不敏感的子串匹配。
func TestMatchesEcommercePromptIsCaseInsensitiveSubstring(t *testing.T) {
	keywords := []string{"好物", "SALE", "折扣"}

	assert.True(t, MatchesEcommercePrompt("双11 好物推荐", keywords), "中文子串应命中")
	assert.True(t, MatchesEcommercePrompt("Black Friday SALE promotion", keywords), "英文大写应命中")
	assert.True(t, MatchesEcommercePrompt("black friday sale promotion", keywords), "英文小写应命中")
	assert.True(t, MatchesEcommercePrompt("限时折扣活动中", keywords), "中文子串应命中")
	assert.False(t, MatchesEcommercePrompt("a cat playing piano", keywords), "无关提示词不该命中")
}

// 空关键词表 = 不命中（不因为「表是空的」就把整条规则判死）。
func TestMatchesEcommercePromptEmptyKeywordsNeverMatches(t *testing.T) {
	assert.False(t, MatchesEcommercePrompt("好物推荐", nil))
	assert.False(t, MatchesEcommercePrompt("好物推荐", []string{"", "  "}))
}

// 空提示词 = 不命中。
func TestMatchesEcommercePromptEmptyPromptNeverMatches(t *testing.T) {
	assert.False(t, MatchesEcommercePrompt("", []string{"好物"}))
	assert.False(t, MatchesEcommercePrompt("   ", []string{"好物"}))
}

// 关键词表为空时回落默认表；配了就用配的。
func TestEcommerceKeywordsFallbackToDefault(t *testing.T) {
	applyPolicy(t, ecommercePolicy())

	policy := Get()
	assert.Equal(t, DefaultEcommerceKeywords, policy.EcommerceKeywords(),
		"没配关键词表时应回落默认表")

	withKeywords := WithEcommerceKeywords([]string{"仅这一个词"})
	assert.Equal(t, []string{"仅这一个词"}, withKeywords.EcommerceKeywords(),
		"配了关键词表就该用配的")
}

// 关键词表去空白、去重、丢空串。
func TestWithEcommerceKeywordsNormalizes(t *testing.T) {
	policy := WithEcommerceKeywords([]string{" 好物 ", "好物", "", "  ", "折扣"})

	assert.Equal(t, []string{"好物", "折扣"}, policy.EcommercePromptKeywords)
}

// 传空列表 = 回落默认表（不是「关掉内容判定」）。
func TestWithEcommerceKeywordsEmptyFallsBack(t *testing.T) {
	policy := WithEcommerceKeywords(nil)

	assert.Empty(t, policy.EcommercePromptKeywords)
	assert.Equal(t, DefaultEcommerceKeywords, policy.EcommerceKeywords(),
		"空表应回落默认表")
}

// 目标模型：源模型专属覆盖 > 公共字段 > 默认值。
func TestEcommerceTargetModelPrecedence(t *testing.T) {
	payload := ecommercePolicy()
	payload["ecommerce_enabled"] = true
	payload["ecommerce_target_model"] = "公共目标"
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{"ecommerce_target_model": "专属目标"},
	}
	applyPolicy(t, payload)

	policy := Get()
	assert.Equal(t, "公共目标", policy.EcommerceTargetModelFor("seedance2.5-特惠"))
	assert.Equal(t, "专属目标", policy.EcommerceTargetModelFor("seedance2.5-活动"))
}

// 没配目标模型时用默认值 cp-seedance-2.5flash。
func TestEcommerceTargetModelDefaults(t *testing.T) {
	applyPolicy(t, ecommercePolicy())

	assert.Equal(t, DefaultEcommerceTargetModel, Get().EcommerceTargetModelFor("seedance2.5-特惠"))
	assert.Equal(t, "cp-seedance-2.5flash", DefaultEcommerceTargetModel)
}

// 时长阈值：0 = 没配 = 用默认 20 秒。
func TestEcommerceTriggerSecondsFallback(t *testing.T) {
	applyPolicy(t, ecommercePolicy())

	assert.Equal(t, DefaultEcommerceTriggerSeconds, Get().EcommerceTriggerSeconds())
	assert.Equal(t, 20, DefaultEcommerceTriggerSeconds)

	policy, err := WithEcommerceConditions(false, true, 15)
	require.NoError(t, err)
	assert.Equal(t, 15, policy.EcommerceTriggerSeconds())
}

// 勾选状态必须能读出来（前端要按它渲染勾选框）。
func TestEcommerceConditionFlags(t *testing.T) {
	policy, err := WithEcommerceConditions(true, false, 20)
	require.NoError(t, err)

	assert.True(t, policy.EcommerceRatioEnabled(), "9:16 勾选状态应能被读出")
	assert.False(t, policy.EcommerceSecondsEnabled(), "时长勾选状态应能被读出")
}

// 非法阈值（<= 0）必须被拒绝 —— 一个永远匹配不上的阈值会让所有超长请求都不改派，
// 而后台勾选看起来仍是勾着的。
func TestWithEcommerceConditionsRejectsInvalidThreshold(t *testing.T) {
	_, err := WithEcommerceConditions(false, true, 0)

	require.Error(t, err, "阈值非法时必须报错而不是写进去")
}

// 条件写入只动三个字段，不碰其它经营配置。
func TestWithEcommerceConditionsKeepsOtherFields(t *testing.T) {
	payload := ecommercePolicy()
	payload["ecommerce_enabled"] = true
	payload["short_target_model"] = "wan3.0-720p-特惠-内链"
	applyPolicy(t, payload)

	before := Get()
	policy, err := WithEcommerceConditions(true, true, 25)
	require.NoError(t, err)

	assert.True(t, policy.EcommerceEnabled, "总开关不能被条件写入冲掉")
	assert.Equal(t, before.ShortTargetModel, policy.ShortTargetModel, "短时长目标不能被冲掉")
	assert.Equal(t, before.TargetModel, policy.TargetModel, "规则一目标不能被冲掉")
	assert.Equal(t, before.TriggerSeconds, policy.TriggerSeconds, "规则一阈值不能被冲掉")
}

// ══════════════════════════════════════════════════════════════════
// 规则六第三分支：三路互斥
// ══════════════════════════════════════════════════════════════════

// 没配 short_capcut_enabled 时默认关闭。
func TestShortCapcutDefaultsOff(t *testing.T) {
	payload := ecommercePolicy()
	payload["short_target_model"] = "wan3.0-720p-特惠-内链"
	payload["short_trigger_seconds"] = 10
	applyPolicy(t, payload)

	assert.False(t, IsShortCapcutEnabled(), "没配 short_capcut_enabled 时应默认关闭")
	assert.False(t, IsShortCapcutEnabledFor("seedance2.5-特惠"), "默认关闭时不该生效")
}

// 三路同时只能有一条生效：读侧按「心笔 > 有赞 > CapCut」取第一条。
//
// 这条锁的是**不变量**：无论配置被 SQL 改成什么样，至多一条分支生效。
func TestShortRouteTargetsAtMostOneActive(t *testing.T) {
	payload := ecommercePolicy()
	payload["short_target_model"] = "wan3.0-720p-特惠-内链"
	payload["short_trigger_seconds"] = 10
	payload["short_link_enabled"] = false
	payload["short_youzan_enabled"] = false
	payload["short_capcut_enabled"] = true
	applyPolicy(t, payload)

	assert.Equal(t, []string{DefaultShortCapcutTargetModel}, ShortRouteTargets())
	assert.Equal(t, DefaultShortCapcutTargetModel, ActiveShortRouteTarget())

	// 三条全开：仍然只有第一条生效。
	payload["short_link_enabled"] = true
	payload["short_youzan_enabled"] = true
	applyPolicy(t, payload)

	assert.Len(t, ShortRouteTargets(), 3, "配置上三条都开着时列表应反映现状")
	assert.Equal(t, "wan3.0-720p-特惠-内链", ActiveShortRouteTarget(),
		"读侧兜底必须取优先级最高的那条")
}

// Apply* 辅助函数：基于**给定策略**改字段，能一次改三个。
//
// 这组函数存在的原因是 WithShortLinkEnabled / WithShortYouzanEnabled
// 从**全局状态**取基底 —— 分三次调用会互相覆盖前一次的结果。
func TestApplyShortHelpersMutateGivenPolicy(t *testing.T) {
	base := Policy{
		Enabled:            true,
		SourceModels:       []string{"seedance2.5-特惠"},
		ShortTargetModel:   "wan3.0-720p-特惠-内链",
		ShortCapcutEnabled: true,
	}

	policy := ApplyShortLinkEnabled(base, false)
	policy = ApplyShortYouzanEnabled(policy, false)
	policy = ApplyShortCapcutEnabled(policy, true)

	assert.NotNil(t, policy.ShortLinkEnabled, "心笔开关必须被显式写成 false（不能留 nil）")
	assert.False(t, *policy.ShortLinkEnabled, "心笔开关应为 false")
	assert.False(t, policy.ShortYouzanEnabled, "有赞开关应为 false")
	assert.True(t, policy.ShortCapcutEnabled, "CapCut 开关应为 true")
}

// ApplyShortLinkEnabled 必须写**显式 false** 而不是留 nil。
//
// 理由：short_link_enabled 是三态指针，nil = 默认开启 ——
// 留 nil 的话读侧兜底会认为心笔还开着，CapCut 分支永远轮不到。
func TestApplyShortLinkEnabledWritesExplicitFalse(t *testing.T) {
	base := Policy{Enabled: true, SourceModels: []string{"seedance2.5-特惠"}}

	policy := ApplyShortLinkEnabled(base, false)

	require.NotNil(t, policy.ShortLinkEnabled)
	assert.False(t, *policy.ShortLinkEnabled)
}

// Apply* 不修改传入的策略（纯函数语义）。
func TestApplyShortHelpersDoNotMutateInput(t *testing.T) {
	base := Policy{Enabled: true, SourceModels: []string{"seedance2.5-特惠"}}

	policy := ApplyShortCapcutEnabled(base, true)

	assert.False(t, base.ShortCapcutEnabled, "传入的策略不能被就地修改")
	assert.True(t, policy.ShortCapcutEnabled)
}
