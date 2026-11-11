package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
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

// ─────────────────────────────────────────────────────────────
// 按源模型的比例改派开关（ratio_rule_enabled，2026-11-11）
//
// 背景：老板要求「只取消 seedance2.5-特惠 → 心笔 的 9:16 改派，
// 别的模型路由到心笔的不变」。公共 ratio_target_model 一改就是两个源模型
// 一起改，所以必须给规则二加一个与 SecondsRuleEnabled 同形的三态开关。
// ─────────────────────────────────────────────────────────────

// ratioSwitchPolicy 是本节测试共用的配置：促销族两个成员 + 时长规则 + 比例规则，
// 母本 seedance2.5-特惠 的比例改派被单独关掉。
func ratioSwitchPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":       "seedance-2-5",
		"trigger_seconds":    30,
		"ratio_target_model": "wan3.0-720p-特惠-内链",
		"trigger_ratio":      "9:16",
		"source_overrides": map[string]interface{}{
			"seedance2.5-特惠": map[string]interface{}{
				"ratio_rule_enabled": false,
			},
		},
	}
}

// 只关母本的比例改派：母本不生效、活动版照旧、规则一不受牵连。
func TestSourceOverrideRatioRuleSwitch(t *testing.T) {
	applyPolicy(t, ratioSwitchPolicy())

	require.False(t, IsRatioRouteEnabledFor("seedance2.5-特惠"),
		"母本配了 ratio_rule_enabled=false，比例改派必须不生效")
	require.True(t, IsRatioRouteEnabledFor("seedance2.5-活动"),
		"只关母本不能把活动版的比例改派一起关掉")
	require.True(t, IsSecondsRuleEnabledFor("seedance2.5-特惠"),
		"比例开关不能牵连规则一（时长改派）")
	require.True(t, IsAnyRouteEnabled(),
		"时长/活动版比例都还在跑，中间件不能被整个跳过")
}

// 没配 ratio_rule_enabled 的源模型必须默认开启 —— 三态指针 nil = 开。
//
// 这是升级安全的硬要求：生产 options 里没有这个字段，
// 解析后是 nil，若被当成「关掉」，升级当天就静默停掉全站 9:16 改派。
func TestRatioRuleSwitchAbsentMeansEnabled(t *testing.T) {
	payload := ratioSwitchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{
			"target_model": "seedance-2-5",
		},
	}
	applyPolicy(t, payload)

	require.True(t, IsRatioRouteEnabledFor("seedance2.5-特惠"),
		"覆盖里没写 ratio_rule_enabled（nil）必须按默认开启处理")
	require.True(t, IsRatioRouteEnabledFor("seedance2.5-活动"))
}

// 两个源模型的比例改派都被关掉时，校验不能把整份策略打回 Enabled=false。
//
// 这是 anyRuleReady 的回归：它必须看「配全没配全」（configured），
// 不能看「开关开没开」。若那里也看开关，只配了比例规则的策略会被判成
// 「一条都没配全」而 Enabled=false —— 将来重新打开开关时路由已死，且无报错。
func TestRatioRuleSwitchOffDoesNotDisablePolicy(t *testing.T) {
	payload := ratioSwitchPolicy()
	payload["source_overrides"] = map[string]interface{}{
		"seedance2.5-特惠": map[string]interface{}{"ratio_rule_enabled": false},
		"seedance2.5-活动": map[string]interface{}{"ratio_rule_enabled": false},
	}
	// 只留比例规则：时长/首尾帧/故障转移全不配。
	delete(payload, "target_model")
	delete(payload, "trigger_seconds")
	applyPolicy(t, payload)

	policy := Get()
	require.True(t, policy.Enabled,
		"规则只是被开关关掉、没有配错 —— anyRuleReady 不能把它打回 false")
	require.False(t, IsRatioRouteEnabledFor("seedance2.5-特惠"))
	require.False(t, IsRatioRouteEnabledFor("seedance2.5-活动"),
		"两个源模型的按源门控都关掉后，规则二对谁都不该生效")
}

// Get() 的深拷贝必须连新开关的指针一起拷 —— 改副本不能污染全局状态。
func TestGetReturnsDeepCopyOfRatioRuleSwitch(t *testing.T) {
	applyPolicy(t, ratioSwitchPolicy())

	copied := Get()
	require.NotNil(t, copied.SourceOverrides["seedance2.5-特惠"].RatioRuleEnabled)
	*copied.SourceOverrides["seedance2.5-特惠"].RatioRuleEnabled = true

	require.False(t, IsRatioRouteEnabledFor("seedance2.5-特惠"),
		"改副本的开关指针把全局状态也改了 —— clonePolicy 没做深拷贝")
}

// ─────────────────────────────────────────────────────────────
// 按源模型的「改派到豆包后是否继续降级到班车」开关
// （failover_after_doubao_enabled，2026-11-11）
//
// 背景：活动版常驻心笔、豆包只是兜底；老板要求活动版**不要**再落到班车，
// 且只影响活动版 —— 母本的「促销族 → 豆包 → 班车」降级链必须保持不变。
// 公共 failover 配置一改就是所有源模型一起改，所以必须按源模型独立开关。
// ─────────────────────────────────────────────────────────────

// failoverAfterDoubaoPolicy 是本节测试共用的配置：
// 促销族两个成员 + 规则一 + 规则五，活动版关闭「改派后降级」。
func failoverAfterDoubaoPolicy() map[string]interface{} {
	return map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{"seedance2.5-特惠", "seedance2.5-活动"},
		"target_model":          "seedance-2-5",
		"trigger_seconds":       20,
		"failover_enabled":      true,
		"failover_source_model": "seedance-2-5",
		"failover_target_model": "wan3.0-班车",
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"failover_after_doubao_enabled": false,
			},
		},
	}
}

// 只关活动版的「改派后降级」：活动版不降级、母本照旧。
func TestFailoverAfterDoubaoSwitchOffPerSource(t *testing.T) {
	applyPolicy(t, failoverAfterDoubaoPolicy())

	require.False(t, IsFailoverAfterDoubaoEnabledFor("seedance2.5-活动"),
		"活动版配了 failover_after_doubao_enabled=false，改派到豆包后不能再降级到班车")
	require.True(t, IsFailoverAfterDoubaoEnabledFor("seedance2.5-特惠"),
		"只关活动版不能把母本的降级链一起关掉")
}

// 没配这个开关的源模型必须默认开启 —— 三态指针 nil = 开。
//
// 升级安全：生产 options 里没有这个字段，解析后是 nil；
// 若被当成「关掉」，升级当天母本的「豆包 → 班车」降级会静默失效。
func TestFailoverAfterDoubaoSwitchAbsentMeansEnabled(t *testing.T) {
	payload := failoverAfterDoubaoPolicy()
	delete(payload, "source_overrides")
	applyPolicy(t, payload)

	require.True(t, IsFailoverAfterDoubaoEnabledFor("seedance2.5-活动"),
		"覆盖里没写 failover_after_doubao_enabled（nil）必须按默认开启处理")
	require.True(t, IsFailoverAfterDoubaoEnabledFor("seedance2.5-特惠"))
}

// Get() 的深拷贝必须连这个新开关的指针一起拷。
func TestGetReturnsDeepCopyOfFailoverAfterDoubaoSwitch(t *testing.T) {
	applyPolicy(t, failoverAfterDoubaoPolicy())

	copied := Get()
	require.NotNil(t, copied.SourceOverrides["seedance2.5-活动"].FailoverAfterDoubaoEnabled)
	*copied.SourceOverrides["seedance2.5-活动"].FailoverAfterDoubaoEnabled = true

	require.False(t, IsFailoverAfterDoubaoEnabledFor("seedance2.5-活动"),
		"改副本的开关指针把全局状态也改了 —— clonePolicy 没做深拷贝")
}
