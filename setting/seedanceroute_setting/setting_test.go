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
func MarshalJSONStringOf(p Policy) string {
	return `{"enabled":` + boolStr(p.Enabled) +
		`,"source_model":"` + p.SourceModel +
		`","target_model":"` + p.TargetModel +
		`","trigger_seconds":` + itoa(p.TriggerSeconds) + `}`
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
