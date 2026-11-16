package middleware

// 促销族（seedance2.5-特惠 / -活动）**5-30 秒**的端到端路由口径。
//
// 2026-11-15 老板口径：
//
//	· 特惠与活动都支持 5-30 秒；
//	· <10 秒的请求改派 **cp-seedance-2.0**（CapCut 满血档，区间 4-15 秒）；
//	· ≥10 秒的请求留在有赞标准版 wan3.0-video（上游下界 10 秒）。
//
// 这份测试锁住的是**分流分界线**：5-9 秒必须落 CapCut、10 秒必须不落 CapCut。
// 两边错一格就是「5 秒单被上游拒」或「10 秒单白白降级到跨渠道链路」。

import (
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 5-9 秒 → CapCut 满血档（有赞标准版接不住这个区间）。
func TestPromoDuration_BelowTenGoesCapcut20(t *testing.T) {
	for _, seconds := range []int{5, 6, 8, 9} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			enableShortCapcut(t, false, false, true)
			stubChannelAvailableByModel(t, map[string]bool{
				testShortTargetModel:    false,
				testYouzanStandardModel: true,
				testShortCapcutTarget:   true,
			})

			seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":`+strconv.Itoa(seconds)+`,"prompt":"a cat"}`)
			body := decodeBody(t, seen)

			if body["model"] != testShortCapcutTarget {
				t.Fatalf("%d 秒必须改派 CapCut 满血档 %s，实际 model=%v",
					seconds, testShortCapcutTarget, body["model"])
			}
		})
	}
}

// 10 秒及以上 → 不落 CapCut（有赞标准版优先，它是同渠道同 adaptor）。
func TestPromoDuration_TenAndAboveStayOnYouzan(t *testing.T) {
	for _, seconds := range []int{10, 12, 15} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			enableShortCapcut(t, false, true, true)
			stubChannelAvailableByModel(t, map[string]bool{
				testShortTargetModel:    false,
				testYouzanStandardModel: true,
				testShortCapcutTarget:   true,
			})

			seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":`+strconv.Itoa(seconds)+`,"prompt":"a cat"}`)
			body := decodeBody(t, seen)

			if body["model"] == testShortCapcutTarget {
				t.Fatalf("%d 秒不该降级到 CapCut 满血档 —— 有赞标准版能接住", seconds)
			}
		})
	}
}

// 活动版（按次档）同样适用 5-30 秒口径。
//
// ⚠️ 活动版默认关闭规则六（source_overrides 里的 short_rule_enabled=false），
// 那是运营口径（按次档没有短时长成本问题）。这里显式打开，验证**规则本身**
// 对活动版也成立 —— 哪天运营把开关打开，行为已经是对的。
func TestPromoDuration_HuoDongSameBoundary(t *testing.T) {
	enableShortCapcutForHuoDong(t)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-活动","seconds":5,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testShortCapcutTarget {
		t.Fatalf("活动版 5 秒同样应落 CapCut 满血档 %s，实际 model=%v",
			testShortCapcutTarget, body["model"])
	}
}

// enableShortCapcutForHuoDong 给活动版也打开规则六。
//
// 与 enableShortCapcut 的差别：它把活动版加进 source_models，并显式写
// source_overrides[活动版].short_rule_enabled = true（生产配置里它是 false）。
func enableShortCapcutForHuoDong(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{testSourceModel, "seedance2.5-活动"},
		"target_model":          testTargetModel,
		"trigger_seconds":       20,
		"short_target_model":    testShortTargetModel,
		"short_trigger_seconds": 10,
		"short_link_enabled":    false,
		"short_youzan_enabled":  false,
		"short_capcut_enabled":  true,
		"source_overrides": map[string]interface{}{
			"seedance2.5-活动": map[string]interface{}{
				"short_rule_enabled": true,
			},
		},
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// promoPolicy 是「促销族路由配置」的测试参数。
//
// 为什么需要它：现有的几个辅助函数（enableShortCapcut / enableTotalFallback …）
// 各自都会 UpdateFromJSONString 写入一份**完整** policy，后写的覆盖先写的。
// 于是「规则八开着 + 规则六第三分支开着」这种**组合**没法表达 ——
// 而它恰恰是生产上最需要验的那一组。
type promoPolicy struct {
	xinbi    bool // 规则六第一分支：心笔内链
	youzan   bool // 规则六第二分支：有赞标准版
	capcut   bool // 规则六第三分支：CapCut 满血档
	fallback bool // 规则八：全量兜底
	huodong  bool // 把活动版也纳入源模型（并打开它的规则六）
}

// enablePromoPolicy 按给定开关组合写入一份完整 policy。
func enablePromoPolicy(t *testing.T, p promoPolicy) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	sources := []string{testSourceModel}
	overrides := map[string]interface{}{}
	if p.huodong {
		sources = append(sources, "seedance2.5-活动")
		overrides["seedance2.5-活动"] = map[string]interface{}{
			"short_rule_enabled": true,
		}
	}
	payload := map[string]interface{}{
		"enabled":                true,
		"source_models":          sources,
		"target_model":           testTargetModel,
		"trigger_seconds":        20,
		"short_target_model":     testShortTargetModel,
		"short_trigger_seconds":  10,
		"short_link_enabled":     p.xinbi,
		"short_youzan_enabled":   p.youzan,
		"short_capcut_enabled":   p.capcut,
		"total_fallback_enabled": p.fallback,
	}
	if len(overrides) > 0 {
		payload["source_overrides"] = overrides
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 分界常量必须与「有赞标准版下界」一致 —— 两边不是同一个数就会出现
// 「5 秒既不被 CapCut 接、也不被有赞接」的空洞。
func TestPromoDuration_BoundaryConstantsAligned(t *testing.T) {
	if seedanceroute_setting.ShortCapcutMinSeconds != 10 {
		t.Fatalf("CapCut 接管门槛应为 10 秒，实际 %d", seedanceroute_setting.ShortCapcutMinSeconds)
	}
	if seedanceroute_setting.DefaultShortCapcutTargetModel != "cp-seedance-2.0" {
		t.Fatalf("规则六第三分支目标应为满血档 cp-seedance-2.0，实际 %s",
			seedanceroute_setting.DefaultShortCapcutTargetModel)
	}
}

// 规则八开着时，**全部**请求（含 <10 秒）都落 2.5 Flash —— 这是规则八的
// 本意：它是一个「真无条件」的逃生口，打开就全量。
//
// 2026-11-16 老板口径：「勾选了全量无条件后全部到 2.5 Flash 是没问题的，
// 不用管」。早先这里曾断言 <10 秒会跳过规则八去落满血档（那是 2026-11-15
// 的临时口径），现已撤销 —— 任何时长特判都会把规则八变成「看情况的开关」，
// 运营按下按钮时的心智模型被破坏。
//
// ⚠️ 要让 <10 秒落满血档（cp-seedance-2.0），正确做法是**关掉规则八**，
// 而不是给规则八加时长特判。
func TestPromoDuration_TotalFallbackTakesEverything(t *testing.T) {
	// ⚠️ 这里必须用**合并版**配置，不能连续调 enableShortCapcut +
	// enableTotalFallback：两个辅助函数各自都会 UpdateFromJSONString
	// 写入一份**完整** policy，后写的会把先写的整个冲掉
	//（规则八的开关就是这样被冲没的 —— 症状是「规则八开着，
	// 8 秒却落了满血档」，看起来像代码没生效，其实是测试配置被覆盖）。
	enablePromoPolicy(t, promoPolicy{
		capcut:   true,
		fallback: true,
	})
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
		"cp-seedance-2.5flash":  true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != "cp-seedance-2.5flash" {
		t.Fatalf("规则八是无条件全量兜底，8 秒也应落 2.5 Flash，实际 model=%v", body["model"])
	}
}

// 规则八**关闭**时，<10 秒落 CapCut 满血档（这是默认路径）。
//
// 这条与上一条配对：说明「<10 秒走满血档」靠的是**关规则八**，
// 而不是靠规则八内部对 <10 秒开特例。
func TestPromoDuration_ShortGoesCapcutWhenTotalFallbackOff(t *testing.T) {
	enableShortCapcut(t, false, false, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
		"cp-seedance-2.5flash":  true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testShortCapcutTarget {
		t.Fatalf("规则八关闭时 8 秒应落 CapCut 满血档 %s，实际 model=%v", testShortCapcutTarget, body["model"])
	}
}

// 新口径（2026-11-16 老板）：**特惠只有 <10 秒走 CapCut，其余全部走有赞**。
//
// 具体到规则六的三条分支：
//   - 心笔（short_link_enabled）必须**关掉** —— 它是跨渠道链路，
//     与「尽量用有赞」的口径不符；
//   - 有赞标准版（short_youzan_enabled）打开，接住恰好 10 秒；
//   - CapCut 满血档（short_capcut_enabled）打开，接住 <10 秒。
//
// ⚠️ **≥11 秒根本进不了规则六**（阈值是 ≤10），它留在源模型
// `seedance2.5-特惠` 上 —— 而源模型的执行渠道就是有赞（youzanwan3 adaptor），
// 30 秒还会由 adaptor 内部映射到有赞 prime。所以「≥10 秒都走有赞」成立。
//
// 这条守的是**两条分支的分界**同时成立：
//   - <10 秒 → CapCut 满血档（cp-seedance-2.0）；
//   - =10 秒 → 有赞标准版 wan3.0-video；
//   - ≥11 秒 → 源模型（仍是有赞链路）。
//
// 只锁一边的话，另一边的回归（10 秒被 CapCut 抢走 / 8 秒被有赞接走）
// 都不会被发现 —— 而这两个错位分别表现为「跨渠道白跳」与「上游 400」。
func TestPromoDuration_TehuiPrefersYouzanAboveTen(t *testing.T) {
	enablePromoPolicy(t, promoPolicy{youzan: true, capcut: true})
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	// 恰好 10 秒 → 有赞标准版（规则六第二分支）。
	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testYouzanStandardModel {
		t.Fatalf("10 秒必须走有赞标准版 %s，实际 model=%v", testYouzanStandardModel, body["model"])
	}

	// <10 秒 → CapCut 满血档。
	for _, seconds := range []int{5, 6, 8, 9} {
		seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":`+strconv.Itoa(seconds)+`,"prompt":"a cat"}`)
		body := decodeBody(t, seen)
		if body["model"] != testShortCapcutTarget {
			t.Fatalf("%d 秒必须走 CapCut 满血档 %s，实际 model=%v",
				seconds, testShortCapcutTarget, body["model"])
		}
	}
}

// ≥11 秒留在源模型（它就是有赞链路），**不被**改派到 CapCut。
//
// 这条守的是「≥10 秒都走有赞」的另一半：11 秒以上进不了规则六
// （阈值 ≤10），所以必须留在源模型而不是落到 CapCut。
func TestPromoDuration_AboveTriggerStaysOnSource(t *testing.T) {
	enablePromoPolicy(t, promoPolicy{youzan: true, capcut: true})
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	for _, seconds := range []int{11, 12, 15, 20} {
		seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":`+strconv.Itoa(seconds)+`,"prompt":"a cat"}`)
		body := decodeBody(t, seen)
		if body["model"] == testShortCapcutTarget {
			t.Fatalf("%d 秒不该被改派到 CapCut —— 它必须留在有赞源链路", seconds)
		}
		if body["model"] != testSourceModel {
			t.Fatalf("%d 秒必须留在源模型 %s（其执行渠道即有赞），实际 model=%v",
				seconds, testSourceModel, body["model"])
		}
	}
}
