package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// 「按源模型分别配置目标模型」的测试（2026-11-03）。
//
// 背景：seedance2.5 促销族的两个成员需要不同的兜底链路：
//
//	母本 seedance2.5-特惠（按秒 ¥0.20/秒）→ 30 秒走豆包 seedance-2-5
//	活动版 seedance2.5-活动（按次 ¥4.5/次）→ 30 秒走心笔 wan3.0-720p-按次
//
// source_models 是共用列表，四条规则共用一套目标模型 —— 只改公共字段
// 必然同时改到另一个成员。所以加了 source_overrides 这一层。
//
// 本文件锁：
//  1. 覆盖生效、未覆盖的字段回落公共值；
//  2. 没有覆盖时行为与改造前逐字节一致；
//  3. Get() 返回深拷贝（改副本不会污染全局状态）。

// applyPolicy 用一份 map 覆盖当前策略，测试结束恢复。
func applyPolicy(t *testing.T, payload map[string]interface{}) {
	t.Helper()
	previous := Get()
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = UpdateFromJSONString(string(restored))
	})
}

// 源模型专属覆盖必须生效。
func TestSourceOverrideTargetModel(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 30,
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"target_model": "wan3.0-720p-按次",
			},
		},
	})

	policy := Get()
	if got := policy.TargetModelFor("seedance2.5-活动"); got != "wan3.0-720p-按次" {
		t.Fatalf("活动版的目标模型 = %q, want wan3.0-720p-按次", got)
	}
	if got := policy.TargetModelFor("seedance2.5-特惠"); got != "seedance-2-5" {
		t.Fatalf("母本的目标模型 = %q, want seedance-2-5（不能被活动版的覆盖带走）", got)
	}
}

// 覆盖只写了一个字段时，没写的字段必须**回落公共值**。
//
// 这是三级回落语义的核心：只给活动版换时长兜底，不应该把比例/首尾帧
// 规则一起关掉 —— 那是配置外的副作用。
func TestSourceOverrideFallsBackToCommonFields(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":             true,
		"source_models":       []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":        "seedance-2-5",
		"trigger_seconds":     30,
		"ratio_target_model":  "wan3.0-720p-特惠",
		"trigger_ratio":       "9:16",
		"frames_target_model": "wan3.0-720p-特惠",
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"target_model": "wan3.0-720p-按次",
			},
		},
	})

	policy := Get()
	if got := policy.TargetModelFor("seedance2.5-活动"); got != "wan3.0-720p-按次" {
		t.Fatalf("活动版的时长目标 = %q, want wan3.0-720p-按次", got)
	}
	if got := policy.RatioTargetModelFor("seedance2.5-活动"); got != "wan3.0-720p-特惠" {
		t.Fatalf("活动版的比例目标 = %q, want wan3.0-720p-特惠（没覆盖就该回落公共值）", got)
	}
	if got := policy.FramesTargetModelFor("seedance2.5-活动"); got != "wan3.0-720p-特惠" {
		t.Fatalf("活动版的首尾帧目标 = %q, want wan3.0-720p-特惠（没覆盖就该回落公共值）", got)
	}
}

// 没有覆盖时，行为必须与改造前**逐字节一致**。
//
// 这是硬要求：生产 options 里没有任何 source_overrides，
// 升级代码不能让现有路由的任何一个字节发生变化。
func TestNoOverrideKeepsLegacyBehavior(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":       "seedance-2-5",
		"trigger_seconds":    30,
		"ratio_target_model": "wan3.0-720p-特惠",
		"trigger_ratio":      "9:16",
	})

	policy := Get()
	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		if got := policy.TargetModelFor(source); got != "seedance-2-5" {
			t.Fatalf("%s 的目标模型 = %q, want seedance-2-5", source, got)
		}
		if got := policy.RatioTargetModelFor(source); got != "wan3.0-720p-特惠" {
			t.Fatalf("%s 的比例目标 = %q, want wan3.0-720p-特惠", source, got)
		}
	}
}

// 公共字段全空、每个源模型各有目标时，策略仍必须算「配全」。
//
// 这是加 source_overrides 后最容易出的错：只看公共字段会误判成
// 「一条都没配全」→ Enabled 被强制置 false → 路由整个失效。
func TestPolicyWithOnlySourceOverridesIsEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"trigger_seconds": 30,
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"target_model": "wan3.0-720p-按次",
			},
		},
	})

	policy := Get()
	if !policy.Enabled {
		t.Fatalf("Enabled 被置成了 false —— 公共字段为空但有源模型专属覆盖时不该关掉")
	}
	if !IsAnyRouteEnabled() {
		t.Fatalf("IsAnyRouteEnabled() = false，路由会整个跳过")
	}
	if !IsSecondsRuleEnabledFor("seedance2.5-活动") {
		t.Fatalf("活动版的规则一不生效 —— 覆盖配了却没被认出来")
	}
}

// Get() 必须返回深拷贝：改副本不能污染全局状态。
//
// SourceOverrides 是 map，浅拷贝会让调用方改一个键就改到全局配置 ——
// 这是典型的「某个请求把别人的路由改坏了」故障源。
func TestGetReturnsDeepCopyOfSourceOverrides(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-活动"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 30,
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"target_model": "wan3.0-720p-按次",
			},
		},
	})

	copied := Get()
	copied.SourceOverrides["seedance2.5-活动"] = SourceOverride{TargetModel: "被改坏了"}
	copied.SourceOverrides["凭空多出来的"] = SourceOverride{TargetModel: "x"}

	fresh := Get()
	if got := fresh.TargetModelFor("seedance2.5-活动"); got != "wan3.0-720p-按次" {
		t.Fatalf("改副本把全局状态改了：目标模型 = %q", got)
	}
	if _, exists := fresh.SourceOverrides["凭空多出来的"]; exists {
		t.Fatalf("改副本往全局状态里塞了新键")
	}
}

// 源模型名两侧有空格时，覆盖必须仍然生效。
//
// 生产上真出现过「 seedance2.5-特惠」这种请求（model/log.go 记着同一个事故），
// 配置侧和请求侧都要 Trim。
func TestSourceOverrideTrimsWhitespace(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{"seedance2.5-活动"},
		"target_model":    "seedance-2-5",
		"trigger_seconds": 30,
		"source_overrides": map[string]interface{}{
			" seedance2.5-活动 ": map[string]interface{}{
				"target_model": "wan3.0-720p-按次",
			},
		},
	})

	policy := Get()
	if got := policy.TargetModelFor(" seedance2.5-活动 "); got != "wan3.0-720p-按次" {
		t.Fatalf("带空格的源模型名没命中覆盖，目标模型 = %q", got)
	}
}
