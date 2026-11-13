package seedanceroute_setting

import (
	"encoding/json"
	"testing"
)

// 「短时长分流（规则六）链路开关」的测试（2026-11-13）。
//
// 背景：规则六把 ≤10 秒的请求改派到心笔内链（wan3.0-720p-特惠-内链），
// 是为了绕开上游 prime 的 PRIME_DURATION_TOO_SHORT。运营需要一个与豆包
// 链路开关同款的开关：**打开 = 路由到心笔；关闭 = 不路由、请求走原链路**。
//
// 本文件锁五件事：
//  1. 没配 short_link_enabled = 默认开启（旧 option 升级后行为不变 ——
//     生产当前正在分流，加 bool 零值 false 会静默停掉线上分流）；
//  2. 显式 false 关掉**所有源模型**的规则六（与按源模型的
//     short_rule_enabled 不同，这是全局闸门）；
//  3. 关掉开关不影响整份策略的「配全」判定（否则其它规则会跟着停）；
//  4. WithShortLinkEnabled 是纯函数 + Get() 深拷贝指针；
//  5. 开关关闭时 IsAnyRouteEnabled 只依赖规则六的配置会被短路 ——
//     中间件整体放行，请求走原链路。
func shortLinkPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":             true,
		"source_models":       []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"short_target_model":  "wan3.0-720p-特惠-内链",
		"short_trigger_seconds": 10,
	}
}

// 没配 short_link_enabled 时默认开启（升级当天行为不变）。
func TestShortLinkDefaultsOn(t *testing.T) {
	applyPolicy(t, shortLinkPolicy())

	if !IsShortLinkEnabled() {
		t.Fatal("没配 short_link_enabled 时开关应默认开启")
	}
	if IsShortLinkOff() {
		t.Fatal("没配 short_link_enabled 时 IsShortLinkOff 不应为 true")
	}
	if !IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("开关默认开启时规则六应生效")
	}
}

// 显式 false 关掉所有源模型的规则六。
func TestShortLinkOffDisablesRuleForAllSources(t *testing.T) {
	payload := shortLinkPolicy()
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	if IsShortLinkEnabled() {
		t.Fatal("显式 false 后开关应为关闭")
	}
	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		if IsShortRouteEnabledFor(source) {
			t.Fatalf("%s 的规则六在链路开关关闭后不该生效", source)
		}
	}
}

// 开关关掉只影响规则六，不把整份策略判成「没配全」。
func TestShortLinkOffKeepsPolicyReady(t *testing.T) {
	payload := shortLinkPolicy()
	payload["target_model"] = "seedance-2-5"
	payload["trigger_seconds"] = 20
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	policy := Get()
	if !policy.Enabled {
		t.Fatal("规则六链路开关关闭后，整份策略不该被判成未启用")
	}
	if !IsAnyRouteEnabled() {
		t.Fatal("规则一仍配置着，IsAnyRouteEnabled 不应为 false")
	}
	if IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("规则六应在开关关闭后失效")
	}
}

// 只配了规则六、开关关闭：中间件整体放行（IsAnyRouteEnabled = false），
// 请求走原链路 —— 这正是「关闭就不路由 走原链路」的语义。
func TestShortLinkOffAloneShortCircuitsAnyRoute(t *testing.T) {
	payload := shortLinkPolicy()
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	if IsAnyRouteEnabled() {
		t.Fatal("只配规则六且开关关闭时，IsAnyRouteEnabled 应为 false（中间件放行走原链路）")
	}
}

// 开关重新打开后规则六恢复生效。
func TestShortLinkOnRestoresRule(t *testing.T) {
	payload := shortLinkPolicy()
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	if IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("前置条件失败：开关关闭时规则六应失效")
	}

	policy := Get()
	enabled := true
	policy.ShortLinkEnabled = &enabled
	encoded, err := json.Marshal(policy)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	if !IsShortLinkEnabled() {
		t.Fatal("开关设回 true 后应恢复开启")
	}
	if !IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("开关设回 true 后规则六应恢复生效")
	}
}

// WithShortLinkEnabled 是纯函数：返回副本，不动全局状态。
func TestWithShortLinkEnabledIsCopy(t *testing.T) {
	applyPolicy(t, shortLinkPolicy())

	updated := WithShortLinkEnabled(false)
	if updated.IsShortLinkOn() {
		t.Fatal("返回的副本里开关仍是开启")
	}
	if !IsShortLinkEnabled() {
		t.Fatal("纯函数不该改动全局状态")
	}

	restored := WithShortLinkEnabled(true)
	if !restored.IsShortLinkOn() {
		t.Fatal("开关设回 true 后副本仍为关闭")
	}
}

// Get() 必须深拷贝开关指针：改副本不能改到全局状态。
func TestClonePolicyCopiesShortLinkPointer(t *testing.T) {
	payload := shortLinkPolicy()
	payload["short_link_enabled"] = false
	applyPolicy(t, payload)

	copied := Get()
	enabled := true
	copied.ShortLinkEnabled = &enabled

	if IsShortLinkEnabled() {
		t.Fatal("改副本的开关指针污染了全局状态")
	}
}

// 全局链路开关与按源模型的 short_rule_enabled 是两层独立闸门，
// 任何一层关掉规则六都不生效。
func TestShortLinkAndPerSourceSwitchAreIndependentGates(t *testing.T) {
	// 链路开关开着，按源开关关 → 不生效。
	payload := shortLinkPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{"short_rule_enabled": false},
	}
	applyPolicy(t, payload)
	if IsShortRouteEnabledFor("seedance2.5-特惠") {
		t.Fatal("按源开关关闭时规则六不该生效")
	}
	if !IsShortRouteEnabledFor("seedance2.5-活动") {
		t.Fatal("另一个源模型不该被带走")
	}

	// 链路开关关掉 → 全部不生效（即使按源开关都没写 false）。
	payload2 := shortLinkPolicy()
	payload2["short_link_enabled"] = false
	applyPolicy(t, payload2)
	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		if IsShortRouteEnabledFor(source) {
			t.Fatalf("%s 的规则六在全局链路开关关闭后不该生效", source)
		}
	}
}
