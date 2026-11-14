package seedanceroute_setting

import "testing"

// 「规则五备选备胎」开关的测试（2026-11-14）。
//
// 语义：豆包链路（seedance-2-5）不可用/被关闭时，
//
//	打开 → 改派 cp-seedance-2.5flash（480p 生成 + 超分 720P 交付）；
//	关闭（默认）→ 仍走 failover_target_model（wan3.0-班车）。
//
// 本文件锁四件事：
//  1. 没配 failover_alt_enabled 时默认关闭 —— 升级当天线上流量逐字节不变；
//  2. 目标模型没配时回落到 cp-seedance-2.5flash（不是「没有目标」）；
//  3. With* 是纯函数，不写全局状态；
//  4. 序列化 → 反序列化后两个字段都还在（生产走 options JSON）。
func failoverAltPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":               true,
		"failover_enabled":      true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车",
	}
}

// 没配 failover_alt_enabled 时默认关闭：豆包挂了仍然走班车。
func TestFailoverAltDefaultsOff(t *testing.T) {
	applyPolicy(t, failoverAltPolicy())

	if IsFailoverAltEnabled() {
		t.Fatal("没配 failover_alt_enabled 时开关应默认关闭（走班车）")
	}
	if got := FailoverAltTargetModel(); got != DefaultFailoverAltModel {
		t.Fatalf("没配 failover_alt_model 时应回落到默认目标 %q，实际 %q", DefaultFailoverAltModel, got)
	}
}

// 显式打开后开关为 true，且默认目标就是 CapCut 2.5 Flash。
func TestFailoverAltExplicitOn(t *testing.T) {
	payload := failoverAltPolicy()
	payload["failover_alt_enabled"] = true
	applyPolicy(t, payload)

	if !IsFailoverAltEnabled() {
		t.Fatal("显式 true 后备选备胎开关应开启")
	}
	if got := FailoverAltTargetModel(); got != "cp-seedance-2.5flash" {
		t.Fatalf("默认目标应为 cp-seedance-2.5flash，实际 %q", got)
	}
}

// 目标模型可配置；空白字符串视为「没配」→ 回落默认值。
func TestFailoverAltTargetModelFallsBackOnBlank(t *testing.T) {
	payload := failoverAltPolicy()
	payload["failover_alt_enabled"] = true
	payload["failover_alt_model"] = "  "
	applyPolicy(t, payload)

	if got := FailoverAltTargetModel(); got != DefaultFailoverAltModel {
		t.Fatalf("空白目标应回落到默认值 %q，实际 %q", DefaultFailoverAltModel, got)
	}
}

// WithFailoverAltEnabled / WithFailoverAltModel 是纯函数：不落库、不改全局。
func TestFailoverAltWithFunctionsArePure(t *testing.T) {
	payload := failoverAltPolicy()
	payload["failover_alt_enabled"] = false
	applyPolicy(t, payload)

	enabled := WithFailoverAltEnabled(true)
	if !enabled.FailoverAltEnabled {
		t.Fatal("WithFailoverAltEnabled(true) 应返回开关打开的副本")
	}
	if IsFailoverAltEnabled() {
		t.Fatal("WithFailoverAltEnabled 不应修改全局策略")
	}

	custom := WithFailoverAltModel("  cp-seedance-2.5flash  ")
	if custom.FailoverAltModel != "cp-seedance-2.5flash" {
		t.Fatalf("模型名应去空白保存，实际 %q", custom.FailoverAltModel)
	}
	if got := current.FailoverAltModel; got != "" {
		t.Fatalf("WithFailoverAltModel 不应修改全局策略，实际 %q", got)
	}
}

// 两个字段必须能进出 options JSON（生产配置走这条路径）。
func TestFailoverAltSurvivesJSONRoundTrip(t *testing.T) {
	payload := failoverAltPolicy()
	payload["failover_alt_enabled"] = true
	payload["failover_alt_model"] = "cp-seedance-2.5flash"
	applyPolicy(t, payload)

	serialized := MarshalJSONString()
	applyPolicy(t, map[string]interface{}{"enabled": false})
	if err := UpdateFromJSONString(serialized); err != nil {
		t.Fatalf("回灌失败: %v", err)
	}
	if !IsFailoverAltEnabled() {
		t.Fatal("JSON 往返后备选开关应仍为开启")
	}
	if got := FailoverAltTargetModel(); got != "cp-seedance-2.5flash" {
		t.Fatalf("JSON 往返后目标模型应保留，实际 %q", got)
	}
}

// ApplyFailoverAltModel 必须在**已有改动**的策略副本上继续改，
// 而不是从全局快照重来。
//
// 这是接口层最容易踩的坑：With* 系列每个都 clonePolicy(current)，
// 连着调两个（先开开关、再写模型名）会把前一个改动悄悄丢掉 ——
// 结果是运营点了开关，界面回显「已开启」，实际上开关没落库。
func TestApplyFailoverAltModelKeepsPreviousChanges(t *testing.T) {
	applyPolicy(t, failoverAltPolicy())

	policy := WithFailoverAltEnabled(true)
	policy = ApplyFailoverAltModel(policy, "cp-seedance-2.5flash")

	if !policy.FailoverAltEnabled {
		t.Fatal("ApplyFailoverAltModel 不能覆盖掉刚设好的开关")
	}
	if policy.FailoverAltModel != "cp-seedance-2.5flash" {
		t.Fatalf("模型名应写进同一个副本，实际 %q", policy.FailoverAltModel)
	}
	if IsFailoverAltEnabled() {
		t.Fatal("ApplyFailoverAltModel 同样不应修改全局策略")
	}
}

// 传空串 = 回到默认值，而不是「关闭」。两者混在一起会让
// 「清空模型名」顺手把退路从 CapCut 切回班车。
func TestApplyFailoverAltModelEmptyFallsBackToDefault(t *testing.T) {
	payload := failoverAltPolicy()
	payload["failover_alt_enabled"] = true
	payload["failover_alt_model"] = "cp-seedance-2.0fast"
	applyPolicy(t, payload)

	policy := ApplyFailoverAltModel(Get(), "")
	if policy.FailoverAltModel != "" {
		t.Fatalf("空的模型名应原样存为空（读取时才回落默认值），实际 %q", policy.FailoverAltModel)
	}
	if !policy.FailoverAltEnabled {
		t.Fatal("清空模型名不应把开关一起关掉")
	}

	// 落库后再读：空配置必须回落默认值，否则「清空」会变成「本链路不生效」。
	cleared := failoverAltPolicy()
	cleared["failover_alt_enabled"] = true
	cleared["failover_alt_model"] = policy.FailoverAltModel
	applyPolicy(t, cleared)

	if got := FailoverAltTargetModel(); got != DefaultFailoverAltModel {
		t.Fatalf("空模型名应回落默认值，实际 %q", got)
	}
	if !IsFailoverAltEnabled() {
		t.Fatal("清空模型名不应关掉开关")
	}
}
