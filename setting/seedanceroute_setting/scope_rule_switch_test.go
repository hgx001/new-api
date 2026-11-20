package seedanceroute_setting

// 「按源模型单独停用规则三（首尾帧）/ 规则九（音视频）」的测试（2026-11-20）。
//
// 背景：促销族母本的常驻上游换成 mw-sd2.5-flash 后，它**自己就支持**
// 参考音视频与首尾帧 —— 规则三/九原本是给「源链路接不住」补位的，对它
// 不再需要：继续改派会把请求从 mw 拉走，违背「特惠全部路由到 mw」的口径。
//
// 但这两条规则对**别的**源模型仍然有效（wan3.0-video 直连依旧只收参考图、
// 不收首尾帧），所以不能关全局开关 —— 必须能只关一个源模型。
//
// 本文件锁三件事：
//  1. 没配 = 默认开启（旧 option 升级后行为不变）；
//  2. 显式 false 只影响那个源模型，另一个照常；
//  3. 全局开关关掉时，按源开关开着的也不生效（两层与的关系）。

import "testing"

// scopeSwitchPolicy 是一份「两个源模型 + 首尾帧目标 + 音视频开启」的基础策略。
func scopeSwitchPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":                 true,
		"source_models":           []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":            "seedance-2-5",
		"frames_target_model":     "wan3.0-video-官网",
		"ref_av_capcut_enabled":   true,
		"ecommerce_target_model":  "cp-seedance-2.5flash",
		"ratio_target_model":      "wan3.0-720p-特惠",
		"short_target_model":      "wan3.0-720p-特惠",
		"failover_source_model":   "seedance-2-5",
		"failover_target_model":   "wan3.0-班车",
		"trigger_seconds":         20,
		"short_trigger_seconds":   10,
		"ecommerce_prompt_keywords": []string{"产品"},
	}
}

// 没配两个新字段时默认开启 —— 升级当天行为与改动前逐字节一致。
func TestFramesAndRefAVDefaultOn(t *testing.T) {
	applyPolicy(t, scopeSwitchPolicy())

	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		if !IsFramesRouteEnabledFor(source) {
			t.Fatalf("%s 的规则三默认应生效", source)
		}
		if !IsRefAVRouteEnabledFor(source) {
			t.Fatalf("%s 的规则九默认应生效", source)
		}
	}
}

// 显式 false 只关掉配置的那个源模型。
func TestFramesAndRefAVDisableOnlyThatSource(t *testing.T) {
	payload := scopeSwitchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{
			"frames_rule_enabled":  false,
			"ref_av_rule_enabled":  false,
		},
	}
	applyPolicy(t, payload)

	if IsFramesRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("特惠的规则三应被单独关掉")
	}
	if IsRefAVRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("特惠的规则九应被单独关掉")
	}
	// 另一个源模型照常 —— 配置是按源模型生效的，不是全局。
	if !IsFramesRouteEnabledFor("seedance2.5-活动") {
		t.Fatal("活动版的规则三不该被连带关掉")
	}
	if !IsRefAVRouteEnabledFor("seedance2.5-活动") {
		t.Fatal("活动版的规则九不该被连带关掉")
	}
}

// 全局开关关掉时，按源开关开着也不生效（两层与的关系）。
func TestRefAVGlobalOffOverridesSourceSwitch(t *testing.T) {
	payload := scopeSwitchPolicy()
	payload["ref_av_capcut_enabled"] = false
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{"ref_av_rule_enabled": true},
	}
	applyPolicy(t, payload)

	if IsRefAVRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("全局开关关闭时，按源开关不能单独把规则九打开")
	}
}

// 副本深拷贝：改副本的指针不能影响全局配置。
func TestScopeSwitchesAreDeepCopied(t *testing.T) {
	payload := scopeSwitchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{
			"frames_rule_enabled": false,
			"ref_av_rule_enabled": false,
		},
	}
	applyPolicy(t, payload)

	copied := Get()
	on := true
	value := copied.SourceOverrides["seedance2.5-特惠"]
	value.FramesRuleEnabled = &on
	value.RefAVRuleEnabled = &on
	copied.SourceOverrides["seedance2.5-特惠"] = value

	if IsFramesRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("改副本不该打开全局的规则三")
	}
	if IsRefAVRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("改副本不该打开全局的规则九")
	}
}
