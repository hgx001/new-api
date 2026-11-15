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
