package seedanceroute_setting

import (
	"testing"
)

func restore(t *testing.T) {
	t.Helper()
	previous := Get()
	t.Cleanup(func() {
		_ = UpdateFromJSONString(MarshalJSONStringOf(previous))
	})
}

// MarshalJSONStringOf 是给测试用的序列化助手（生产用 MarshalJSONString）。
//
// 必须包含**全部**字段：restore(t) 用它恢复原状，漏字段会导致
// 测试之间互相污染（比如比例规则被悄悄清掉，后续用例却「通过」）。
func MarshalJSONStringOf(p Policy) string {
	return `{"enabled":` + boolStr(p.Enabled) +
		`,"source_model":"` + p.SourceModel +
		`","target_model":"` + p.TargetModel +
		`","trigger_seconds":` + itoa(p.TriggerSeconds) +
		`,"ratio_target_model":"` + p.RatioTargetModel +
		`","trigger_ratio":"` + p.TriggerRatio + `"}`
}

func boolStr(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	negative := v < 0
	if negative {
		v = -v
	}
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	if negative {
		return "-" + digits
	}
	return digits
}

func TestDefaultPolicyIsDisabled(t *testing.T) {
	restore(t)
	_ = UpdateFromJSONString(`{"enabled":true,"source_model":"a","target_model":"b","trigger_seconds":30}`)

	// 模拟「option 从未写过」：用一份不带任何字段的 JSON 覆盖。
	if err := UpdateFromJSONString(`{}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	policy := Get()
	if policy.Enabled {
		t.Fatal("Enabled = true, want false for empty payload")
	}
	if policy.SourceModel != "" || policy.TargetModel != "" || policy.TriggerSeconds != 0 {
		t.Fatalf("policy = %+v, want zero-value policy", policy)
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() = true, want false")
	}
}

func TestUpdateEnablesWithCompletePolicy(t *testing.T) {
	restore(t)
	if err := UpdateFromJSONString(
		`{"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":30}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !IsEnabled() {
		t.Fatal("IsEnabled() = false, want true")
	}
	policy := Get()
	if policy.SourceModel != "seedance2.5-特惠" || policy.TargetModel != "seedance-2-5" || policy.TriggerSeconds != 30 {
		t.Fatalf("policy = %+v, unexpected values", policy)
	}
}

func TestUpdateDisablesWhenFieldsMissing(t *testing.T) {
	restore(t)
	cases := []struct {
		name string
		json string
	}{
		{"missing source", `{"enabled":true,"target_model":"b","trigger_seconds":30}`},
		{"missing target", `{"enabled":true,"source_model":"a","trigger_seconds":30}`},
		{"missing trigger", `{"enabled":true,"source_model":"a","target_model":"b"}`},
		{"zero trigger", `{"enabled":true,"source_model":"a","target_model":"b","trigger_seconds":0}`},
		{"negative trigger", `{"enabled":true,"source_model":"a","target_model":"b","trigger_seconds":-1}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := UpdateFromJSONString(tc.json); err != nil {
				t.Fatalf("update: %v", err)
			}
			if IsEnabled() {
				t.Fatalf("IsEnabled() = true, want false for %s", tc.json)
			}
			if Get().Enabled {
				t.Fatalf("Enabled = true, want false for %s", tc.json)
			}
		})
	}
}

func TestUpdateRejectsInvalidJSON(t *testing.T) {
	restore(t)
	before := Get()
	if err := UpdateFromJSONString(`{invalid`); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	after := Get()
	if after.Enabled != before.Enabled || after.SourceModel != before.SourceModel ||
		after.TargetModel != before.TargetModel || after.TriggerSeconds != before.TriggerSeconds {
		t.Fatalf("policy changed on invalid JSON: before=%+v after=%+v", before, after)
	}
}

func TestMarshalRoundTrip(t *testing.T) {
	restore(t)
	if err := UpdateFromJSONString(
		`{"enabled":true,"source_model":"src","target_model":"dst","trigger_seconds":15,"updated_at":1700000000}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	encoded := MarshalJSONString()
	if err := UpdateFromJSONString(encoded); err != nil {
		t.Fatalf("round trip failed: %v (encoded=%s)", err, encoded)
	}
	policy := Get()
	if !policy.Enabled || policy.SourceModel != "src" || policy.TargetModel != "dst" ||
		policy.TriggerSeconds != 15 || policy.UpdatedAt != 1700000000 {
		t.Fatalf("round trip mismatch: %+v", policy)
	}
}

// ─────────────────────────────────────────────────────────────
// 规则二（比例）的配置校验
// ─────────────────────────────────────────────────────────────

func TestLegacyPolicyKeepsRatioRuleOff(t *testing.T) {
	restore(t)
	// 生产上现存的 option JSON 就长这样（没有 ratio_* 字段）。
	// 解析后比例规则必须不生效，否则等于没批准就上线一条新路由。
	if err := UpdateFromJSONString(
		`{"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":30}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !IsEnabled() {
		t.Fatal("IsEnabled() = false, want true")
	}
	if IsRatioRouteEnabled() {
		t.Fatal("IsRatioRouteEnabled() = true, want false for legacy policy")
	}
	if !IsAnyRouteEnabled() {
		t.Fatal("IsAnyRouteEnabled() = false, want true")
	}
}

func TestRatioOnlyPolicyIsUsable(t *testing.T) {
	restore(t)
	if err := UpdateFromJSONString(
		`{"enabled":true,"source_model":"seedance2.5-特惠","ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() = true, want false（时长规则没配）")
	}
	if !IsRatioRouteEnabled() {
		t.Fatal("IsRatioRouteEnabled() = false, want true")
	}
	if !IsAnyRouteEnabled() {
		t.Fatal("IsAnyRouteEnabled() = false, want true")
	}
}

func TestBothRulesCoexist(t *testing.T) {
	restore(t)
	if err := UpdateFromJSONString(`{"enabled":true,"source_model":"seedance2.5-特惠",` +
		`"target_model":"seedance-2-5","trigger_seconds":30,` +
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !IsEnabled() || !IsRatioRouteEnabled() || !IsAnyRouteEnabled() {
		t.Fatalf("all rules should be on: IsEnabled=%v IsRatioRouteEnabled=%v IsAnyRouteEnabled=%v",
			IsEnabled(), IsRatioRouteEnabled(), IsAnyRouteEnabled())
	}
	policy := Get()
	if policy.RatioTargetModel != "wan3.0-720p-特惠" || policy.TriggerRatio != "9:16" {
		t.Fatalf("policy = %+v, ratio fields not persisted", policy)
	}
}

func TestRatioRuleIncompleteDisablesEverything(t *testing.T) {
	restore(t)
	cases := []struct {
		name string
		json string
	}{
		{"ratio target missing", `{"enabled":true,"source_model":"a","trigger_ratio":"9:16"}`},
		{"trigger ratio missing", `{"enabled":true,"source_model":"a","ratio_target_model":"b"}`},
		{"source missing", `{"enabled":true,"ratio_target_model":"b","trigger_ratio":"9:16"}`},
		{"both incomplete", `{"enabled":true,"source_model":"a","target_model":"b","ratio_target_model":"c"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := UpdateFromJSONString(tc.json); err != nil {
				t.Fatalf("update: %v", err)
			}
			if IsAnyRouteEnabled() {
				t.Fatalf("IsAnyRouteEnabled() = true, want false for %s", tc.json)
			}
			if Get().Enabled {
				t.Fatalf("Enabled = true, want false for %s", tc.json)
			}
		})
	}
}

func TestMarshalRoundTripKeepsRatioFields(t *testing.T) {
	restore(t)
	if err := UpdateFromJSONString(`{"enabled":true,"source_model":"src",` +
		`"target_model":"dst","trigger_seconds":30,` +
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	encoded := MarshalJSONString()
	if err := UpdateFromJSONString(encoded); err != nil {
		t.Fatalf("round trip failed: %v (encoded=%s)", err, encoded)
	}
	policy := Get()
	if policy.RatioTargetModel != "wan3.0-720p-特惠" || policy.TriggerRatio != "9:16" {
		t.Fatalf("round trip lost ratio fields: %+v (encoded=%s)", policy, encoded)
	}
}
