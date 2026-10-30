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
		`","trigger_ratio":"` + p.TriggerRatio +
		`","seconds_rule_paused_until":` + itoa64(p.SecondsRulePausedUntil) + `}`
}

// itoa64 把 int64 转成十进制字符串（不引 strconv，与 itoa 保持一致的手写风格）。
func itoa64(v int64) string {
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

// ─────────────────────────────────────────────────────────────
// 规则一的「带到期时间的暂停」
// ─────────────────────────────────────────────────────────────

// stubNow 把 nowUnix 固定成给定时间，测试结束自动还原。
//
// 不 stub 就没法测「到期自动恢复」—— 总不能真的等一天。
func stubNow(t *testing.T, unix int64) {
	t.Helper()
	previous := nowUnix
	nowUnix = func() int64 { return unix }
	t.Cleanup(func() { nowUnix = previous })
}

// bothRulesOn 把两条规则都配全，作为暂停测试的基线。
func bothRulesOn(t *testing.T) {
	t.Helper()
	if err := UpdateFromJSONString(`{"enabled":true,"source_model":"seedance2.5-特惠",` +
		`"target_model":"seedance-2-5","trigger_seconds":30,` +
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`); err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestSecondsRulePauseDisablesOnlySecondsRule(t *testing.T) {
	restore(t)
	stubNow(t, 1000)
	bothRulesOn(t)

	if err := UpdateFromJSONString(MarshalJSONStringOf(WithSecondsRulePausedUntil(2000))); err != nil {
		t.Fatalf("update: %v", err)
	}

	if IsEnabled() {
		t.Fatal("IsEnabled() = true, want false（规则一已暂停）")
	}
	if !IsRatioRouteEnabled() {
		t.Fatal("IsRatioRouteEnabled() = false, want true（暂停不该波及比例规则）")
	}
	if !IsAnyRouteEnabled() {
		t.Fatal("IsAnyRouteEnabled() = false, want true（比例规则仍在，中间件不能早退）")
	}
	if !IsSecondsRulePaused() {
		t.Fatal("IsSecondsRulePaused() = false, want true")
	}
	// 暂停期间 Enabled 必须保持 true —— 否则恢复时要靠人记得把整个开关打开。
	if !Get().Enabled {
		t.Fatal("Enabled = false, want true（暂停不应改写总开关）")
	}
}

func TestSecondsRulePauseExpiresAutomatically(t *testing.T) {
	restore(t)
	bothRulesOn(t)
	const until int64 = 2000

	if err := UpdateFromJSONString(MarshalJSONStringOf(WithSecondsRulePausedUntil(until))); err != nil {
		t.Fatalf("update: %v", err)
	}

	// 截止前 1 秒：仍在暂停。
	stubNow(t, until-1)
	if !IsSecondsRulePaused() || IsEnabled() {
		t.Fatalf("1s before deadline: paused=%v enabled=%v, want paused=true enabled=false",
			IsSecondsRulePaused(), IsEnabled())
	}

	// 截止那一秒：已经恢复。用 `>` 而不是 `>=` 的判定就体现在这里。
	stubNow(t, until)
	if IsSecondsRulePaused() {
		t.Fatal("at deadline: still paused, want resumed")
	}
	if !IsEnabled() {
		t.Fatal("at deadline: IsEnabled() = false, want true（到期应自动恢复）")
	}

	// 截止之后：保持恢复。
	stubNow(t, until+86400)
	if IsSecondsRulePaused() || !IsEnabled() {
		t.Fatal("after deadline: should stay resumed")
	}
}

func TestSecondsRulePauseWithOnlyRatioRuleKeepsAnyRouteOff(t *testing.T) {
	restore(t)
	stubNow(t, 1000)
	// 只配了比例规则：暂停截止时间非零也不该让 IsAnyRouteEnabled 变 true，
	// 否则中间件会为了一份「没有任何规则可跑」的配置反复解析 body。
	if err := UpdateFromJSONString(`{"enabled":true,"source_model":"a",` +
		`"ratio_target_model":"b","trigger_ratio":"9:16",` +
		`"seconds_rule_paused_until":9999999999}`); err != nil {
		t.Fatalf("update: %v", err)
	}
	if IsEnabled() {
		t.Fatal("IsEnabled() = true, want false（时长规则没配全）")
	}
	if !IsRatioRouteEnabled() || !IsAnyRouteEnabled() {
		t.Fatal("比例规则应仍然生效")
	}
}

func TestWithSecondsRulePausedUntilDoesNotMutateCurrent(t *testing.T) {
	restore(t)
	stubNow(t, 1000)
	bothRulesOn(t)
	before := Get()

	patched := WithSecondsRulePausedUntil(5000)
	if patched.SecondsRulePausedUntil != 5000 {
		t.Fatalf("patched.SecondsRulePausedUntil = %d, want 5000", patched.SecondsRulePausedUntil)
	}
	if patched.UpdatedAt != 1000 {
		t.Fatalf("patched.UpdatedAt = %d, want 1000（应写入当前时间）", patched.UpdatedAt)
	}
	if Get().SecondsRulePausedUntil != 0 {
		t.Fatal("WithSecondsRulePausedUntil 改动了全局状态，它必须是纯函数")
	}
	if before.TargetModel != patched.TargetModel || before.TriggerRatio != patched.TriggerRatio {
		t.Fatalf("patched 丢字段: before=%+v patched=%+v", before, patched)
	}
}

func TestMarshalRoundTripKeepsPauseField(t *testing.T) {
	restore(t)
	stubNow(t, 1000)
	bothRulesOn(t)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithSecondsRulePausedUntil(7777))); err != nil {
		t.Fatalf("update: %v", err)
	}
	encoded := MarshalJSONString()
	if err := UpdateFromJSONString(encoded); err != nil {
		t.Fatalf("round trip failed: %v (encoded=%s)", err, encoded)
	}
	if got := Get().SecondsRulePausedUntil; got != 7777 {
		t.Fatalf("round trip lost pause field: %d (encoded=%s)", got, encoded)
	}
	if got := SecondsRulePausedUntil(); got != 7777 {
		t.Fatalf("SecondsRulePausedUntil() = %d, want 7777", got)
	}
}

func TestResumeClearsPause(t *testing.T) {
	restore(t)
	stubNow(t, 1000)
	bothRulesOn(t)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithSecondsRulePausedUntil(2000))); err != nil {
		t.Fatalf("update: %v", err)
	}
	if !IsSecondsRulePaused() {
		t.Fatal("setup failed: should be paused")
	}
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithSecondsRulePausedUntil(0))); err != nil {
		t.Fatalf("update: %v", err)
	}
	if IsSecondsRulePaused() {
		t.Fatal("IsSecondsRulePaused() = true, want false after resume")
	}
	if !IsEnabled() {
		t.Fatal("IsEnabled() = false, want true after resume")
	}
}
