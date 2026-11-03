package seedanceroute_setting

import (
	"testing"
)

// 「按源模型单独开关按时长改派」的测试（2026-11-03）。
//
// 背景：促销族两个成员的经营口径不同 —— 母本 seedance2.5-特惠 按秒 ¥0.20/s，
// 改派豆包是赚差价；活动版 seedance2.5-活动 按次 ¥4.5/次，改派豆包每单倒贴。
// 老板需要能**只停活动版**，而全局暂停（SecondsRulePausedUntil）一停就是两个一起停。
//
// 本文件锁三件事：
//  1. 没配这个字段 = 默认开启（旧 option 升级后行为不变）；
//  2. 显式 false 只影响那个源模型，另一个照常；
//  3. 全部关掉不会把整份策略判成「没配全」—— 否则首尾帧规则会跟着停。

// switchPolicy 是一份「两个源模型、只有一个 30 秒目标」的基础策略。
func switchPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 30,
	}
}

// 没配 seconds_rule_enabled 时默认开启（升级当天行为不变）。
func TestSecondsRuleSwitchDefaultsOn(t *testing.T) {
	applyPolicy(t, switchPolicy())

	policy := Get()
	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		if !policy.secondsRuleEnabledFor(source) {
			t.Fatalf("%s 的改派开关默认应为开启", source)
		}
		if !IsSecondsRuleEnabledFor(source) {
			t.Fatalf("%s 的规则一默认应生效", source)
		}
	}
}

// 显式 false 只关掉配置的那个源模型。
func TestSecondsRuleSwitchDisablesOnlyThatSource(t *testing.T) {
	payload := switchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{
			"seconds_rule_enabled": false,
		},
	}
	applyPolicy(t, payload)

	if IsSecondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("活动版被显式关掉后，规则一不该生效")
	}
	if !IsSecondsRuleEnabledFor("seedance2.5-特惠") {
		t.Fatal("母本不该被活动版的开关带走")
	}
}

// 关闭开关不影响整份策略的「配全」判定，否则首尾帧规则会一起停。
func TestSecondsRuleSwitchOffKeepsPolicyReady(t *testing.T) {
	payload := switchPolicy()
	payload["frames_target_model"] = "wan3.0-720p-特惠"
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{"seconds_rule_enabled": false},
		"seedance2.5-活动": map[string]interface{}{"seconds_rule_enabled": false},
	}
	applyPolicy(t, payload)

	policy := Get()
	if !policy.Enabled {
		t.Fatal("两个源模型的 30 秒改派都关掉后，整份策略被判成未启用（首尾帧规则会跟着停）")
	}
	if !IsFramesRouteEnabled() {
		t.Fatal("首尾帧规则不该因为 30 秒开关被关而失效")
	}
	if IsSecondsRuleEnabledFor("seedance2.5-特惠") || IsSecondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("两个源模型的规则一都该失效")
	}
}

// WithSourceSecondsRuleEnabled 是纯函数：返回副本，不动全局状态。
func TestWithSourceSecondsRuleEnabledIsCopy(t *testing.T) {
	applyPolicy(t, switchPolicy())

	updated, err := WithSourceSecondsRuleEnabled("seedance2.5-活动", false)
	if err != nil {
		t.Fatalf("设置开关失败：%v", err)
	}
	if updated.secondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("返回的副本里开关仍是开启")
	}
	if !IsSecondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("纯函数不该改动全局状态")
	}

	// 再设回 true，副本也要正确反映。
	restored, err := WithSourceSecondsRuleEnabled("seedance2.5-活动", true)
	if err != nil {
		t.Fatalf("恢复开关失败：%v", err)
	}
	if !restored.secondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("开关设回 true 后副本仍为关闭")
	}
}

// 未知模型名必须报错：静默什么都不做会让运营以为已经关掉了。
func TestWithSourceSecondsRuleEnabledRejectsUnknownModel(t *testing.T) {
	applyPolicy(t, switchPolicy())

	if _, err := WithSourceSecondsRuleEnabled("seedance2.5-不存在的模型", false); err == nil {
		t.Fatal("未知源模型应报错")
	}
	if _, err := WithSourceSecondsRuleEnabled("   ", false); err == nil {
		t.Fatal("空模型名应报错")
	}
}

// 覆盖表里的键带空格时也要能命中并沿用原键。
func TestWithSourceSecondsRuleEnabledKeepsExistingKey(t *testing.T) {
	payload := switchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		" seedance2.5-活动 ": map[string]interface{}{"target_model": "seedance-2-5"},
	}
	applyPolicy(t, payload)

	updated, err := WithSourceSecondsRuleEnabled("seedance2.5-活动", false)
	if err != nil {
		t.Fatalf("设置开关失败：%v", err)
	}
	if _, ok := updated.SourceOverrides[" seedance2.5-活动 "]; !ok {
		t.Fatalf("没有沿用配置里已有的键，实际键：%+v", updated.SourceOverrides)
	}
	if updated.secondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("带空格的键没有生效")
	}
}

// Get() 必须深拷贝指针：改副本不能改到全局状态。
func TestClonePolicyCopiesSwitchPointer(t *testing.T) {
	payload := switchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{"seconds_rule_enabled": false},
	}
	applyPolicy(t, payload)

	copied := Get()
	override := copied.SourceOverrides["seedance2.5-活动"]
	*override.SecondsRuleEnabled = true

	if IsSecondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatal("改副本的开关指针污染了全局状态")
	}
}

// 管理后台的状态快照要能读出「哪个模型被单独关了」。
func TestSourceSwitchStates(t *testing.T) {
	payload := switchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{"seconds_rule_enabled": false},
	}
	applyPolicy(t, payload)

	states := SourceSwitchStates()
	if len(states) != 2 {
		t.Fatalf("应返回 2 个源模型，实际 %d 个", len(states))
	}
	byName := map[string]SourceSwitchState{}
	for _, state := range states {
		byName[state.SourceModel] = state
	}
	if byName["seedance2.5-活动"].SecondsRuleEnabled {
		t.Fatal("活动版的开关状态应为关闭")
	}
	if !byName["seedance2.5-特惠"].SecondsRuleEnabled {
		t.Fatal("母本的开关状态应为开启")
	}
	if byName["seedance2.5-活动"].TargetModel != "seedance-2-5" {
		t.Fatalf("目标模型快照不对：%q", byName["seedance2.5-活动"].TargetModel)
	}
}
