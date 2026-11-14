package seedanceroute_setting

import "testing"

// 「有赞标准版兜底」开关的测试（2026-11-14）。
//
// 背景：规则六原来只有心笔内链一个目标。心笔不可用（余额耗尽 / AutoBan）时
// 短请求会回落到源链路 prime，撞上 PRIME_DURATION_TOO_SHORT —— 客户直接失败。
// 老板要一个独立的按钮：打开后 ≤阈值请求改派有赞标准版 wan3.0-video；
//
//	两个按钮同时打开 → 心笔优先，心笔接不住才落有赞标准版；
//	两个都关闭       → 不路由，走原链路。
//
// 本文件锁五件事：
//  1. 没配 short_youzan_enabled = 默认关闭（新行为不能影响升级当天的线上流量）；
//  2. 打开后只对「规则六本身对它生效」的源模型生效（活动版被
//     short_rule_enabled=false 排除，不能因为有赞按钮被带进来）；
//  3. 与心笔开关（short_link_enabled）互相独立：关掉心笔不影响有赞兜底；
//  4. WithShortYouzanEnabled 是纯函数，不改全局状态；
//  5. 只配有赞兜底也算「策略可用」，中间件不能被 IsAnyRouteEnabled 短路。
func shortYouzanPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"short_target_model":    "wan3.0-720p-特惠-内链",
		"short_trigger_seconds": 10,
	}
}

// 没配 short_youzan_enabled 时默认关闭：升级当天行为保持不变。
func TestShortYouzanDefaultsOff(t *testing.T) {
	applyPolicy(t, shortYouzanPolicy())

	if IsShortYouzanEnabled() {
		t.Fatal("没配 short_youzan_enabled 时开关应默认关闭")
	}
	if IsShortYouzanEnabledFor("seedance2.5-特惠") {
		t.Fatal("默认关闭时有赞标准版兜底不该生效")
	}
}

// 打开开关后对源模型生效。
func TestShortYouzanToggleEnablesForSource(t *testing.T) {
	payload := shortYouzanPolicy()
	payload["short_youzan_enabled"] = true
	applyPolicy(t, payload)

	if !IsShortYouzanEnabled() {
		t.Fatal("显式 true 后开关应开启")
	}
	if !IsShortYouzanEnabledFor("seedance2.5-特惠") {
		t.Fatal("开关打开后 seedance2.5-特惠 的有赞兜底应生效")
	}
}

// 按源模型的 short_rule_enabled 仍然是上层闸门：
// 活动版的规则六本来就被关掉，不能被新按钮带进有赞标准版链路。
func TestShortYouzanRespectsPerSourceShortRuleOff(t *testing.T) {
	payload := shortYouzanPolicy()
	payload["short_youzan_enabled"] = true
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-活动": map[string]interface{}{"short_rule_enabled": false},
	}
	applyPolicy(t, payload)

	if IsShortYouzanEnabledFor("seedance2.5-活动") {
		t.Fatal("活动版的规则六已关闭，有赞兜底不该生效")
	}
	if !IsShortYouzanEnabledFor("seedance2.5-特惠") {
		t.Fatal("特惠的有赞兜底不该被活动版的覆盖带走")
	}
}

// 与心笔开关互相独立：关掉心笔，有赞兜底照常可用。
func TestShortYouzanIsIndependentFromXinbiSwitch(t *testing.T) {
	payload := shortYouzanPolicy()
	payload["short_youzan_enabled"] = true
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	if IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("心笔开关关闭后，规则六的心笔路径不该生效")
	}
	if !IsShortYouzanEnabledFor("seedance2.5-特惠") {
		t.Fatal("有赞兜底不该被心笔开关带走")
	}
	if !IsAnyRouteEnabled() {
		t.Fatal("有赞兜底可用时，整条策略应保持启用")
	}
}

// WithShortYouzanEnabled 是纯函数：返回副本，不动全局状态。
func TestWithShortYouzanEnabledIsCopy(t *testing.T) {
	applyPolicy(t, shortYouzanPolicy())

	updated := WithShortYouzanEnabled(true)
	if !updated.ShortYouzanEnabled {
		t.Fatal("返回的副本里开关应为开启")
	}
	if IsShortYouzanEnabled() {
		t.Fatal("纯函数不该改动全局状态")
	}

	disabled := WithShortYouzanEnabled(false)
	if disabled.ShortYouzanEnabled {
		t.Fatal("开关设回 false 后副本仍为开启")
	}
}

// 只配有赞兜底（没有心笔目标、没有其它规则）也要算策略可用 ——
// 否则运营只打开有赞按钮时，中间件会被 IsAnyRouteEnabled 短路，按钮看起来没反应。
func TestShortYouzanAloneKeepsAnyRouteEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":              true,
		"source_model":         "seedance2.5-特惠",
		"short_youzan_enabled": true,
	})

	if !IsAnyRouteEnabled() {
		t.Fatal("只配有赞兜底时，IsAnyRouteEnabled 不应为 false")
	}
	if !IsShortYouzanEnabledFor("seedance2.5-特惠") {
		t.Fatal("只配有赞兜底时，该源模型的有赞兜底应生效")
	}
}
