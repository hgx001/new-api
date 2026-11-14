package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 规则七（电商/产品推荐内容 → CapCut 快速档 cp-seedance-2.5flash）的中间件测试。
//
// 老板口径（2026-11-14）：
//   - seedance2.5-特惠 / seedance2.5-活动 识别到电商类或产品推荐提示词 → 改派
//     cp-seedance-2.5flash；
//   - **带参考视频的请求不路由**；
//   - 9:16 也路由过去（独立开关，运营可勾选）；
//   - 还可勾选「时长大于 N 秒」作为额外触发入口。
//
// 与规则六第三分支（≤10 秒 → cp-seedance-2.0fast）共用准入逻辑，
// 那边也有独立的一组用例（见 TestShortCapcut* 系列）。

const (
	testEcommerceTarget   = "cp-seedance-2.5flash"
	testShortCapcutTarget = "cp-seedance-2.0fast"
	// testSourceModelAlt 是促销族的活动版（按次固定档）。
	//
	// 必须与母本一起写进 source_models：中间件入口的 IsSourceModel 早退
	// 只认配置里列出的名字，漏配的源模型根本走不到规则七 ——
	// 那时「规则七对活动版不生效」看起来像规则实现的问题，其实是配置。
	testSourceModelAlt = "seedance2.5-活动"
)

// enableEcommerce 配置规则七，测试结束恢复原状。
func enableEcommerce(t *testing.T, ratioEnabled, secondsEnabled bool, triggerSeconds int) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":                     true,
		"source_models":               []string{testSourceModel, testSourceModelAlt},
		"target_model":                testTargetModel,
		"trigger_seconds":             20,
		"short_target_model":          testShortTargetModel,
		"short_trigger_seconds":       10,
		"short_link_enabled":          false,
		"short_youzan_enabled":        false,
		"ecommerce_enabled":           true,
		"ecommerce_ratio_condition":   ratioEnabled,
		"ecommerce_seconds_condition": secondsEnabled,
		"ecommerce_trigger_seconds":   triggerSeconds,
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

// 提示词命中电商关键词 → 改派 CapCut 2.5 Flash，且计费原点不变。
func TestEcommerceRoutesOnKeywordPrompt(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"双11 好物推荐 种草文案"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("电商提示词应改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
	}
	if rec.originModel != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, rec.originModel)
	}
	if !rec.hasBillingSec || rec.billingSeconds != 10 {
		t.Fatalf("计费秒数应为 10，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
	if !rec.hasBillingSize || rec.billingSizeRatio != 1.0 {
		t.Fatalf("720P 应按源模型倍率 1.0 计费，实际 has=%v value=%v", rec.hasBillingSize, rec.billingSizeRatio)
	}
}

// 大小写不敏感：英文电商词（sale / discount）也要命中。
func TestEcommerceMatchesKeywordsCaseInsensitively(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"Black Friday SALE promotion"}`)
	body := decodeBody(t, seen)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("英文电商词应命中并改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
	}
}

// 非电商提示词 → 不路由（规则七不生效，请求留在源链路）。
func TestEcommerceSkipsNonEcommercePrompt(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat playing piano"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("非电商提示词不该改派，实际 model=%v", body["model"])
	}
}

// 总开关关闭 → 即使命中关键词也不路由。
func TestEcommerceDisabledDoesNotRoute(t *testing.T) {
	enableEcommerce(t, false, false, 20)
	policy := seedanceroute_setting.WithEcommerceEnabled(false)
	encoded, _ := common.Marshal(policy)
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("总开关关闭时不该改派，实际 model=%v", body["model"])
	}
}

// 「9:16 比例」勾选后，9:16 的请求即使不是电商内容也要改派。
func TestEcommerceRoutesOnRatioConditionWhenChecked(t *testing.T) {
	enableEcommerce(t, true, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("勾选 9:16 条件后应改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
	}
	if body["ratio"] != "9:16" {
		t.Fatalf("比例必须原样落到顶层 ratio，实际 %v", body["ratio"])
	}
}

// 「9:16 比例」**没勾选**时，9:16 请求不该命中参数入口（配置项形同虚设是最难查的故障）。
func TestEcommerceRatioConditionOffDoesNotRoute(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("未勾选比例条件时不该改派，实际 model=%v", body["model"])
	}
}

// 「时长超过阈值」勾选后，超阈值请求改派；不超的不改派。
//
// 关键回归点：**桶口与规则一重叠时规则七必须赢**。
// 规则七排在规则一之前（见 middleware/seedance_route.go 的顺序说明）——
// 一条 25 秒的电商请求如果先被规则一改派豆包按次档，运营勾选的
// 「时长大于 20 秒」就成了一份永远轮不到的**死配置**：
// 后台看着是勾着的，代码里永远到不了。这正是最难查的那类故障。
func TestEcommerceRoutesOnSecondsConditionWhenChecked(t *testing.T) {
	enableEcommerce(t, false, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("时长 %d 秒超过阈值 20 秒应改派 %s（且优先于规则一的豆包按次档），实际 model=%v",
			25, testEcommerceTarget, body["model"])
	}

	seen, _ = runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
	body = decodeBody(t, seen)
	if body["model"] != testSourceModel {
		t.Fatalf("时长 15 秒未超阈值不该改派，实际 model=%v", body["model"])
	}
}

// 规则七必须排在规则一**之前** —— 否则「时长大于 N 秒」这个入口是死配置。
//
// 回归意义（2026-11-14 修掉的真实缺陷）：
// 规则七原先排在规则一之后，而规则一的阈值也是 20 秒。运营在后台勾上
// 「时长大于 20 秒」后，一条 25 秒的电商请求会**先**被规则一接走改派豆包，
// 规则七永远轮不到 —— 后台看着勾着的，代码里是死配置。
//
// 这条用例把「25 秒 + 阈值 20 + 命中时长条件」钉死到 cp-seedance-2.5flash，
// 且**不依赖提示词**（用「a cat」这种不含电商词的）—— 纯参数入口也要生效。
func TestEcommerceBeatsSecondsRuleOnLongRequests(t *testing.T) {
	enableEcommerce(t, false, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("25 秒 + 阈值 20 应命中规则七（%s），实际 model=%v", testEcommerceTarget, body["model"])
	}
}

// ⚠️ 带参考视频的请求**一律不路由**（老板明确要求）。
//
// 三种形态都要挡：media 数组 / metadata.media / 顶层 videos。
func TestEcommerceDoesNotRouteReferenceVideo(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	cases := []struct {
		name string
		body string
	}{
		{"media 数组", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`},
		{"metadata.media", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","metadata":{"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}}`},
		{"顶层 videos", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","videos":["https://example.com/a.mp4"]}`},
		{"顶层 video", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","video":"https://example.com/a.mp4"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, _ := runRoute(t, tc.body)
			body := decodeBody(t, seen)
			if body["model"] != testSourceModel {
				t.Fatalf("带参考视频的请求不该改派，实际 model=%v", body["model"])
			}
		})
	}
}

// 带参考音频也不路由：CapCut 会静默丢音频 = 错交付。
func TestEcommerceDoesNotRouteReferenceAudio(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","audios":["https://example.com/a.mp3"]}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("带参考音频的请求不该改派，实际 model=%v", body["model"])
	}
}

// 1080P 不路由：CapCut 只卖 480p/720p，改派过去必被上游 400。
func TestEcommerceDoesNotRoute1080P(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","resolution":"1080P"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("1080P 请求不该改派，实际 model=%v", body["model"])
	}
}

// 时长下界：CapCut 最少 4 秒，2–3 秒的请求改派过去必被拒。
func TestEcommerceDoesNotRouteBelowCapcutMinDuration(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":3,"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("3 秒请求不该改派（低于该链路下限 4 秒），实际 model=%v", body["model"])
	}
}

// 参考图必须抬到顶层 input_reference，且原位置要摘掉（否则同图投两次）。
//
// ⚠️ 请求必须**带比例**：CapCut 没有自适应档，而带参考图时「不传比例」
// 意味着源链路按那张图的画幅出片 —— 我们推不出它是什么比例，替他挑一档
// 就是替他改需求，所以带图且无比例的请求本来就会被挡在准入之外
// （见 prepareEcommerceRoute 的比例分支）。
func TestEcommerceLiftsReferenceImagesToInputReference(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","ratio":"16:9",
		"metadata":{"media":[{"type":"reference_image","url":"https://example.com/a.png"}]}}`)
	body := decodeBody(t, seen)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("应改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
	}
	images, ok := body["input_reference"].([]interface{})
	if !ok || len(images) != 1 || images[0] != "https://example.com/a.png" {
		t.Fatalf("参考图必须抬到顶层 input_reference，实际 %v", body["input_reference"])
	}
	if _, exists := body["metadata"]; exists {
		t.Fatalf("metadata 必须整块摘掉（否则同一张图会被投两次），实际仍在 body 里")
	}
}

// 分辨率必须显式钉 720p：CapCut 缺省是 480p，不钉就是静默降档。
func TestEcommercePinsResolutionTo720p(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)

	if body["resolution"] != "720p" {
		t.Fatalf("分辨率必须钉 720p（源链路 prime 缺省交付 720P），实际 %v", body["resolution"])
	}
}

// data URI 参考图不路由：CapCut 只收公网 http(s) 直链。
func TestEcommerceDoesNotRouteDataUriReferenceImage(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐",
		"images":["data:image/png;base64,iVBORw0KGgo="]}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("data URI 参考图不该改派，实际 model=%v", body["model"])
	}
}

// 目标没有可用渠道 → 不路由（请求自然落回源链路）。
func TestEcommerceSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableEcommerce(t, true, true, 20)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget: false,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("目标无渠道时不该改派，实际 model=%v", body["model"])
	}
}

// 时长条件与内容条件是「或」关系：命中任一即改派。
//
// 回归意义：用「且」实现会让「电商内容但不是 9:16」的流量整个漏掉 ——
// 那正是老板明确要的那批流量。
func TestEcommerceContentOrParameterSemantics(t *testing.T) {
	enableEcommerce(t, true, false, 20)

	// 只有内容命中（非 9:16）→ 仍要改派。
	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"metadata":{"ratio":"16:9"},"prompt":"产品推荐 卖点"}`)
	body := decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("内容命中即应改派（与参数条件是或关系），实际 model=%v", body["model"])
	}

	// 只有参数命中（9:16、非电商）→ 也要改派。
	seen, _ = runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body = decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("参数命中即应改派，实际 model=%v", body["model"])
	}
}

// 活动版（第二个源模型）同样生效 —— 规则七的源模型口径是「族内两个都算」。
func TestEcommerceAppliesToBothSourceModels(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		seen, _ := runRoute(t, `{"model":"`+source+`","seconds":10,"prompt":"好物推荐"}`)
		body := decodeBody(t, seen)
		if body["model"] != testEcommerceTarget {
			t.Fatalf("源模型 %s 应改派 %s，实际 model=%v", source, testEcommerceTarget, body["model"])
		}
	}
}

// ══════════════════════════════════════════════════════════════════
// 规则六第三分支：≤10 秒 → cp-seedance-2.0fast
// ══════════════════════════════════════════════════════════════════

// enableShortCapcut 配置规则六的三条分支，测试结束恢复原状。
//
// xinbi / youzan / capcut 三个开关 —— 正常情况下写入口保证只有一条开着，
// 这里显式给出三个维度，便于构造「只开 CapCut」这种世界。
func enableShortCapcut(t *testing.T, xinbi, youzan, capcut bool) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{testSourceModel},
		"target_model":          testTargetModel,
		"trigger_seconds":       20,
		"short_target_model":    testShortTargetModel,
		"short_trigger_seconds": 10,
		"short_link_enabled":    xinbi,
		"short_youzan_enabled":  youzan,
		"short_capcut_enabled":  capcut,
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

// 只开 CapCut 快速档：短请求改派 cp-seedance-2.0fast。
func TestShortCapcutRoutesWhenOnlyCapcutEnabled(t *testing.T) {
	enableShortCapcut(t, false, false, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortCapcutTarget {
		t.Fatalf("只开 CapCut 时短请求应改派 %s，实际 model=%v", testShortCapcutTarget, body["model"])
	}
	if rec.originModel != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, rec.originModel)
	}
	if !rec.hasBillingSec || rec.billingSeconds != 8 {
		t.Fatalf("计费秒数应为 8，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
	if !rec.hasBillingSize || rec.billingSizeRatio != 1.0 {
		t.Fatalf("720P 应按源模型倍率 1.0 计费，实际 has=%v value=%v", rec.hasBillingSize, rec.billingSizeRatio)
	}
}

// 三条都开时按优先级取第一条（心笔），不会两条同时生效。
func TestShortCapcutReadSidePrefersXinbi(t *testing.T) {
	enableShortCapcut(t, true, true, true)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testShortTargetModel {
		t.Fatalf("三路都开时必须心笔优先，实际 model=%v", body["model"])
	}
}

// 心笔与有赞都开、心笔无渠道 → 落有赞标准版（不是 CapCut）。
func TestShortCapcutPrefersYouzanOverCapcut(t *testing.T) {
	enableShortCapcut(t, true, true, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("有赞标准版优先于 CapCut，实际 model=%v", body["model"])
	}
}

// 心笔与有赞都接不住 → 落 CapCut 快速档。
func TestShortCapcutFallsBackWhenOthersUnavailable(t *testing.T) {
	enableShortCapcut(t, true, true, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: false,
		testShortCapcutTarget:   true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testShortCapcutTarget {
		t.Fatalf("心笔与有赞都接不住时应落 CapCut 快速档 %s，实际 model=%v", testShortCapcutTarget, body["model"])
	}
}

// 超过 10 秒的请求不走短时长分支（规则六的区间是 ≤10）。
func TestShortCapcutDoesNotRouteLongRequests(t *testing.T) {
	enableShortCapcut(t, false, false, true)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("15 秒不该走短时长分支，实际 model=%v", body["model"])
	}
}

// 2.0 Fast 的时长上界是 15 秒；规则六只接 ≤10 秒，天然落在里面。
// 这里锁住「上界常量与上游一致」—— 上游一改就会红。
func TestCapcutDurationBoundsMatchUpstream(t *testing.T) {
	if capcutMinDuration != 4 {
		t.Fatalf("CapCut 时长下界应为 4 秒，实际 %d", capcutMinDuration)
	}
	if capcutMaxDuration25 != 30 {
		t.Fatalf("CapCut 2.5 系列时长上界应为 30 秒，实际 %d", capcutMaxDuration25)
	}
	if capcutMaxDurationFast != 15 {
		t.Fatalf("CapCut 2.0 系列时长上界应为 15 秒，实际 %d", capcutMaxDurationFast)
	}
}
