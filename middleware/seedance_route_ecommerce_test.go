package middleware

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 规则七（电商/产品推荐内容 → **有赞优先**，接不住才 CapCut）的中间件测试。
//
// 老板口径（2026-11-16，覆盖 11-14/15 的「一律 CapCut」）：
//   - seedance2.5-特惠 / seedance2.5-活动 识别到电商类/产品推荐提示词 →
//     **留在常驻有赞链路**（10–29 秒标准版；30 秒由 adaptor 逐条映射 prime）；
//   - 只有有赞物理接不住的请求才改派 cp-seedance-2.5：
//     时长 <10 秒（标准版下界，prime 又只接 30 秒）或带参考视频/音频
//     （2026-11-15 上游切换后标准版与 prime 都只收 reference_image）；
//   - 9:16 与「时长大于 N 秒」是**并列的触发入口**，命中后同样走「有赞优先」；
//   - 带首尾帧的电商请求规则七不接管，交给规则三的直连目标。
//
// 改派到 CapCut 时与规则八/九共用准入逻辑，
// 规则六（≤10 秒 → cp-seedance-2.0）也有一组独立用例（见 TestShortCapcut* 系列）。

const (
	testEcommerceTarget = "cp-seedance-2.5flash"
	// 2026-11-15 老板口径：<10 秒改派 CapCut **满血档** cp-seedance-2.0
	// （不是快速档 2.0fast）。与 setting 层的 DefaultShortCapcutTargetModel 同源。
	testShortCapcutTarget = "cp-seedance-2.5flash"
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
	withSecondsRuleOff(payload, testSourceModel, testSourceModelAlt)
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

// 提示词命中电商关键词、时长 10 秒（有赞标准版能接）→ **留在有赞**，不改派。
//
// 这是 2026-11-16 口径的核心：电商命中 = 「优先用常驻有赞」，不是「一定改派」。
func TestEcommerceStaysOnYouzanForKeywordPrompt(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"双11 好物推荐 种草文案"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testSourceModel {
		t.Fatalf("10 秒电商请求应留在有赞源模型 %s，实际 model=%v", testSourceModel, body["model"])
	}
	if rec.originModel != "" && rec.originModel != testSourceModel {
		t.Fatalf("留在有赞时计费原点不得被改写成 %s", rec.originModel)
	}
	// 留在有赞时不做任何 CapCut 改写 —— 出现 resolution 就说明被误改派了。
	if _, ok := body["resolution"]; ok {
		t.Fatalf("留在有赞时不得注入 resolution（那是 CapCut 契约的改写），实际 %#v", body["resolution"])
	}
}

// 大小写不敏感：英文电商词（sale / discount）也要命中。
//
// 这里用 6 秒：有赞标准版接不住（<10 秒）→ 改派 CapCut，正好用「改派发生了」
// 反证「关键词命中判定生效了」。
func TestEcommerceMatchesKeywordsCaseInsensitively(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"Black Friday SALE promotion"}`)
	body := decodeBody(t, seen)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("英文电商词应命中，且 6 秒有赞接不住 → 应改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
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

// 「9:16 比例」勾选后，9:16 的请求即使不是电商内容也归规则七管。
//
// 这里用 6 秒：有赞接不住（<10 秒）→ 改派 CapCut，用「改派发生了」反证
// 参数入口生效；10 秒以上的 9:16 请求则留在有赞（见
// TestEcommerceBeatsRatioRuleOnVerticalRequests）。
func TestEcommerceRoutesOnRatioConditionWhenChecked(t *testing.T) {
	enableEcommerce(t, true, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testEcommerceTarget {
		t.Fatalf("勾选 9:16 条件后 6 秒请求应改派 %s，实际 model=%v", testEcommerceTarget, body["model"])
	}
	if body["ratio"] != "9:16" {
		t.Fatalf("比例必须原样落到顶层 ratio，实际 %v", body["ratio"])
	}
}

// 「9:16 比例」**没勾选**时，9:16 请求不该命中参数入口（配置项形同虚设是最难查的故障）。
//
// 同样用 6 秒做对照：若参数入口被误判命中，这条会改派 CapCut 而不是留在源模型。
func TestEcommerceRatioConditionOffDoesNotRoute(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("未勾选比例条件时不该改派，实际 model=%v", body["model"])
	}
}

// 「时长超过阈值」勾选后，超阈值的请求归规则七管 —— 命中后**留在有赞**，
// 而不是像旧口径那样改派 CapCut。
//
// 关键回归点：**桶口与规则一重叠时规则七必须赢**。25 秒电商请求如果先被
// 规则一改派豆包按次，运营勾的「时长大于 20 秒」就成了一份把电商流量
// 从有赞赶走的配置 —— 与老板 2026-11-16 口径正好相反。
func TestEcommerceSecondsConditionKeepsLongRequestsOnYouzan(t *testing.T) {
	enableEcommerce(t, false, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testSourceModel {
		t.Fatalf("25 秒命中电商时长条件后必须留在有赞 %s（不能被规则一改派 %s），实际 model=%v",
			testSourceModel, testTargetModel, body["model"])
	}
}

// 对照：不勾「时长超过阈值」时，25 秒请求照旧被规则一改派。
//
// 这条证明上一条确实是被规则七接住的，而不是规则一本来就不生效 ——
// 没有这组对照，「留在源模型」的断言可能因为规则一被误关而假通过。
func TestEcommerceSecondsConditionOffFallsToDurationRule(t *testing.T) {
	enableEcommerce(t, false, false, 20)
	// enableEcommerce 默认关着规则一（它开着会盖住规则七）。本条是「没勾
	// 电商时长条件时照旧走规则一」的对照组，所以把它显式打开。
	mustEnableSecondsRule(t, testSourceModel)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testTargetModel {
		t.Fatalf("未勾电商时长条件时 25 秒请求应照旧走规则一 %s，实际 model=%v",
			testTargetModel, body["model"])
	}
}

// 规则七必须排在规则二（比例）之前且命中后短路：9:16 的电商请求留在有赞，
// 不被比例规则改派到比例目标链路。
func TestEcommerceBeatsRatioRuleOnVerticalRequests(t *testing.T) {
	enableEcommerceWithRatioRule(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"metadata":{"ratio":"9:16"},"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)
	if body["model"] != testSourceModel {
		t.Fatalf("9:16 电商请求应留在有赞 %s，实际 model=%v", testSourceModel, body["model"])
	}
}

// 对照：非电商的 9:16 请求照旧被规则二改派（证明上一条不是规则二失效）。
func TestEcommerceRatioRuleAppliesWhenNotEcommerce(t *testing.T) {
	enableEcommerceWithRatioRule(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if body["model"] != testRatioTargetModel {
		t.Fatalf("非电商 9:16 请求应照旧走规则二 %s，实际 model=%v", testRatioTargetModel, body["model"])
	}
}

// enableEcommerceWithRatioRule 同时打开规则七与规则二（比例），用来验证优先级。
//
// 两个 helper（enableEcommerce / enableBothRules）会互相覆盖策略，所以这里
// 直接构造一份合并后的策略 —— 与 enableEcommerceWithShortCapcut 同一做法。
func enableEcommerceWithRatioRule(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{testSourceModel, testSourceModelAlt},
		"target_model":       testTargetModel,
		"trigger_seconds":    20,
		"ratio_target_model": testRatioTargetModel,
		"trigger_ratio":      "9:16",
		"ecommerce_enabled":  true,
	}
	withSecondsRuleOff(payload, testSourceModel, testSourceModelAlt)
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

// 8 秒的电商请求：有赞标准版接不住（下界 10 秒）、prime 只接 30 秒 →
// 改派 CapCut 2.5 Flash。
//
// 规则六的短时长 CapCut 分支同时开着也不影响结论：两条路径目标一致，
// 但规则七先判（顺序在规则六之前），日志里留下的是「电商 + 有赞接不住」。
func TestEcommerceShortRequestGoesCapcutNotShortRoute(t *testing.T) {
	// 开规则七（内容入口）+ 规则六的 CapCut 分支。
	// 两次配置要合并成一份策略，所以这里直接构造一份完整策略。
	enableEcommerceWithShortCapcut(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"好物推荐 种草文案"}`)
	body := decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("8 秒的电商请求应改派 %s（有赞接不住 <10 秒），实际 model=%v",
			testEcommerceTarget, body["model"])
	}
}

// 10–20 秒、非 9:16 的电商请求：**留在有赞**（内容命中不等于改派）。
func TestEcommerceMidDurationStaysOnYouzan(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"resolution":"720P","prompt":"双11 好物推荐 种草文案"}`)
	body := decodeBody(t, seen)
	if body["model"] != testSourceModel {
		t.Fatalf("15 秒电商请求应留在有赞 %s，实际 model=%v", testSourceModel, body["model"])
	}
}

// 电商请求带首尾帧时规则七既不短路也不改派：有赞 2026-10-28 起不收首尾帧，
// CapCut 也没有首尾帧语义 —— 只有规则三的直连目标能跑，交给它。
func TestEcommerceWithFramesDoesNotRouteToCapcut(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"好物推荐 种草文案","media":[{"type":"first_frame","url":"https://example.com/a.jpg"}]}`)
	body := decodeBody(t, seen)
	if body["model"] == testEcommerceTarget {
		t.Fatalf("带首尾帧的电商请求不该被规则七改派到 %s", testEcommerceTarget)
	}
}

// enableEcommerceWithShortCapcut 同时打开规则七与规则六的 CapCut 快速档分支。
//
// 两个 helper（enableEcommerce / enableShortCapcut）各自会写一份完整策略，
// 先后调用会互相覆盖，所以这里直接构造一份合并后的策略。
func enableEcommerceWithShortCapcut(t *testing.T) {
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
		"short_capcut_enabled":        true,
		"ecommerce_enabled":           true,
		"ecommerce_ratio_condition":   false,
		"ecommerce_seconds_condition": false,
		"ecommerce_trigger_seconds":   20,
	}
	withSecondsRuleOff(payload, testSourceModel, testSourceModelAlt)
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

// 带参考视频的请求**改派并透传**（2026-11-15 决策变更）。
//
// 旧口径是「一律不路由」（老板 2026-10-30 拍板，理由是视频参考的成片质量与
// 素材一致性没验证过）。2026-11-15 放开：CapCut 链路已具备视频参考能力，
// 继续挡只会把客户留在源链路上白等。
//
// ⚠️ 透传的关键**不是「不挡」**，而是把素材抬到顶层 `videos`：
// 改派后 `rewriteBodyForCapcut` 会整块丢掉 metadata 与 media（防止同一素材
// 投递两次），漏抬 = 客户按「带参考视频」下单、拿到没有参考视频的成片。
func TestEcommerceRoutesReferenceVideo(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	const want = "https://example.com/a.mp4"
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
			if body["model"] != testEcommerceTarget {
				t.Fatalf("带参考视频的请求应改派到 %s，实际 model=%v", testEcommerceTarget, body["model"])
			}
			if got := stringListValue(body["videos"]); len(got) != 1 || got[0] != want {
				t.Fatalf("参考视频必须抬到顶层 videos=%q，实际 %#v", want, body["videos"])
			}
			if _, ok := body["media"]; ok {
				t.Fatalf("media 必须摘掉（否则同一视频投两次），实际 %#v", body["media"])
			}
			if _, ok := body["metadata"]; ok {
				t.Fatalf("metadata 必须摘掉（否则同一视频投两次），实际 %#v", body["metadata"])
			}
		})
	}
}

// 带参考音频的请求**改派并透传**（2026-11-15 决策变更）。
//
// 旧口径「不路由」的理由是 CapCut 静默丢弃音频 —— 那个前提已经不存在：
// new-api 的 capcut 分支现在会把音频下发到 Worker，Worker 也会上传到素材槽。
// 现在丢掉音频的是「不路由」这个动作本身（留在源链路反而可能不支持）。
func TestEcommerceRoutesReferenceAudio(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	const want = "https://example.com/a.mp3"
	cases := []struct {
		name string
		body string
	}{
		{"顶层 audios", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","audios":["https://example.com/a.mp3"]}`},
		{"顶层 audio 单值", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","audio":"https://example.com/a.mp3"}`},
		{"media 数组", `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, _ := runRoute(t, tc.body)
			body := decodeBody(t, seen)
			if body["model"] != testEcommerceTarget {
				t.Fatalf("带参考音频的请求应改派到 %s，实际 model=%v", testEcommerceTarget, body["model"])
			}
			if got := stringListValue(body["audios"]); len(got) != 1 || got[0] != want {
				t.Fatalf("参考音频必须抬到顶层 audios=%q，实际 %#v", want, body["audios"])
			}
			// 单数键不能留副本：manwu 的 resolveCapcutReferenceMaterials 同时读
			// audios 与 audio，两个都留着 = 同一条音频投两次。
			if got := stringListValue(body["audio"]); len(got) != 0 {
				t.Fatalf("audio 单数键必须被摘掉（否则同一音频投两次），实际 %#v", body["audio"])
			}
		})
	}
}

// 图 + 视频 + 音频同时出现：三类各归各的顶层字段，不交叉、不重复。
//
// 这是改动风险最高的一条 —— 三条收集路径（图片 / 视频 / 音频）如果各读一遍
// media，很容易把同一条素材塞进两个顶层字段，上游就会看到「图里有 mp4」。
//
// ⚠️ body 里必须带 ratio：带参考图且未指定比例时准入会挡（CapCut 没有
// adaptive 档，替客户挑画幅 = 替客户改需求，见 prepareCapcutRoute 的比例段）。
func TestEcommerceRoutesMixedReferenceMedia(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	const body = `{"model":"seedance2.5-特惠","seconds":10,"prompt":"好物推荐","ratio":"9:16","media":[` +
		`{"type":"reference_image","url":"https://example.com/a.png"},` +
		`{"type":"reference_video","url":"https://example.com/b.mp4"},` +
		`{"type":"reference_audio","url":"https://example.com/c.mp3"}]}`

	seen, _ := runRoute(t, body)
	decoded := decodeBody(t, seen)

	if decoded["model"] != testEcommerceTarget {
		t.Fatalf("混合素材应改派到 %s，实际 model=%v", testEcommerceTarget, decoded["model"])
	}
	images := stringListValue(decoded["input_reference"])
	if len(images) != 1 || images[0] != "https://example.com/a.png" {
		t.Fatalf("input_reference 只能有图片，实际 %#v", decoded["input_reference"])
	}
	videos := stringListValue(decoded["videos"])
	if len(videos) != 1 || videos[0] != "https://example.com/b.mp4" {
		t.Fatalf("videos 只能有视频，实际 %#v", decoded["videos"])
	}
	audios := stringListValue(decoded["audios"])
	if len(audios) != 1 || audios[0] != "https://example.com/c.mp3" {
		t.Fatalf("audios 只能有音频，实际 %#v", decoded["audios"])
	}
}

// 1080P 不交给 CapCut：它只卖 480p/720p，改派过去必被上游 400。
//
// 用 6 秒做构造（本来该因 <10 秒改派）：1080P 被准入挡住 → 留在有赞。
// 有赞标准版两档齐全，留源链路是 fail-loud，不是降档。
func TestEcommerceDoesNotRoute1080P(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐","resolution":"1080P"}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("1080P 请求不该改派 CapCut，实际 model=%v", body["model"])
	}
}

// 时长两侧都接不住：有赞下界 10 秒、CapCut 下界 4 秒 —— 2–3 秒留在源链路
// fail-loud（报「时长越界」），不伪装改派。
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

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐","ratio":"16:9",
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

// 生成档位必须显式钉 720p 原生直出（2026-11-17 老板口径）：
// 超分画质不达标，取消「480p 生成 + 超分 720P」，直接向上游买 720p。
//
// ⚠️ 这里**不能**留空让 CapCut 用自己的缺省值：虽然它的缺省恰好也是 480p，
// 但「靠巧合对上」在改一次上游缺省后就会变成静默降档交付。
func TestEcommercePinsGenerationResolutionTo720p(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	// 6 秒：有赞接不住才会改派 CapCut，720p 钉档只对改派路径有意义。
	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐"}`)
	body := decodeBody(t, seen)

	if body["resolution"] != "720p" {
		t.Fatalf("生成档位必须钉 720p 原生直出（不再超分），实际 %v", body["resolution"])
	}
}

// 超分配置与否不再影响促销族改派的生成档（已彻底解耦）。
// 无论探针返回什么，都必须按 720p 原生生成。
func TestEcommerceResolutionIgnoresSuperResolutionConfig(t *testing.T) {
	for _, configured := range []bool{true, false} {
		enableEcommerce(t, true, true, 20)

		seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐"}`)
		body := decodeBody(t, seen)

		if body["resolution"] != "720p" {
			t.Fatalf("超分配置=%v 时都应钉 720p 原生直出，实际 %v", configured, body["resolution"])
		}
	}
}

// data URI 参考图不交给 CapCut：它只收公网 http(s) 直链。
//
// 用 6 秒做构造（本来该因 <10 秒改派）：素材形态不合格 → 留在有赞
// （有赞收 data URI），而不是打过去吃一段 base64 回显的 400。
func TestEcommerceDoesNotRouteDataUriReferenceImage(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐",
		"images":["data:image/png;base64,iVBORw0KGgo="]}`)
	body := decodeBody(t, seen)

	if body["model"] != testSourceModel {
		t.Fatalf("data URI 参考图不该改派 CapCut，实际 model=%v", body["model"])
	}
}

// 目标没有可用渠道 → 不路由（请求自然落回源链路）。
//
// 用 6 秒：这条请求本来会因有赞接不住而改派，靠渠道预检退回源链路。
func TestEcommerceSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableEcommerce(t, true, true, 20)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget: false,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"好物推荐"}`)
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

	// 只有内容命中（非 9:16）→ 归规则七管。用 6 秒让「归它管」可观察
	// （有赞接不住 <10 秒 → 改派 CapCut）。
	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"metadata":{"ratio":"16:9"},"prompt":"产品推荐 卖点"}`)
	body := decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("内容命中即归规则七管（与参数条件是或关系），实际 model=%v", body["model"])
	}

	// 只有参数命中（9:16、非电商）→ 同样归规则七管。
	seen, _ = runRoute(t, `{"model":"seedance2.5-特惠","seconds":6,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body = decodeBody(t, seen)
	if body["model"] != testEcommerceTarget {
		t.Fatalf("参数命中即归规则七管，实际 model=%v", body["model"])
	}
}

// 活动版（第二个源模型）同样生效 —— 规则七的源模型口径是「族内两个都算」。
func TestEcommerceAppliesToBothSourceModels(t *testing.T) {
	enableEcommerce(t, true, true, 20)

	for _, source := range []string{"seedance2.5-特惠", "seedance2.5-活动"} {
		seen, _ := runRoute(t, `{"model":"`+source+`","seconds":6,"prompt":"好物推荐"}`)
		body := decodeBody(t, seen)
		if body["model"] != testEcommerceTarget {
			t.Fatalf("源模型 %s 应改派 %s，实际 model=%v", source, testEcommerceTarget, body["model"])
		}
	}
}

// ── 2026-11-16 新口径的核心用例 ──────────────────────────────────

// 有赞能接的标准时长区间（10–29 秒）：全部留在有赞，一个都不改派。
//
// 这是新口径的主路径 —— 旧口径下这里每一条都会被改派到 CapCut。
func TestEcommerceStaysOnYouzanAcrossStandardDurations(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	for _, seconds := range []int{10, 11, 15, 20, 25, 29} {
		seen, _ := runRoute(t, fmt.Sprintf(
			`{"model":"seedance2.5-特惠","seconds":%d,"prompt":"好物推荐 种草文案"}`, seconds))
		body := decodeBody(t, seen)
		if body["model"] != testSourceModel {
			t.Fatalf("%d 秒电商请求应留在有赞 %s，实际 model=%v",
				seconds, testSourceModel, body["model"])
		}
	}
}

// 30 秒：路由层**不动**模型名，交给 adaptor 的 upstreamModelForRequest
// 逐条映射到 prime（仅 720P）。路由层改模型名会丢掉计费原点与逐秒判定。
func TestEcommerceThirtySecondsStaysOnYouzanForAdaptorPrimeMapping(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"好物推荐"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testSourceModel {
		t.Fatalf("30 秒电商请求应留在源模型 %s（prime 映射由 adaptor 做），实际 model=%v",
			testSourceModel, body["model"])
	}
	// 留在源模型时中间件不写 RouteOriginModel（只有改派才需要记计费原点）——
	// 计费本来就走客户请求的模型名，这里断言「没被写成别的模型」即可。
	if rec.originModel != "" && rec.originModel != testSourceModel {
		t.Fatalf("计费原点不得被改成 %s（留在有赞时就是源模型）", rec.originModel)
	}
}

// 客户没传时长：不判「有赞接不住」（源链路缺省 10 秒 = 标准版下界，能接）。
func TestEcommerceMissingSecondsStaysOnYouzan(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","prompt":"好物推荐"}`)
	body := decodeBody(t, seen)
	if body["model"] != testSourceModel {
		t.Fatalf("未传时长的电商请求应留在有赞 %s，实际 model=%v", testSourceModel, body["model"])
	}
}

// <10 秒：有赞标准版（下界 10 秒）与 prime（只接 30 秒）都接不住 → CapCut。
func TestEcommerceBelowTenSecondsGoesCapcut(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	for _, seconds := range []int{5, 6, 8, 9} {
		seen, _ := runRoute(t, fmt.Sprintf(
			`{"model":"seedance2.5-特惠","seconds":%d,"prompt":"好物推荐"}`, seconds))
		body := decodeBody(t, seen)
		if body["model"] != testEcommerceTarget {
			t.Fatalf("%d 秒电商请求有赞接不住，应改派 %s，实际 model=%v",
				seconds, testEcommerceTarget, body["model"])
		}
	}
}

// 带参考音视频：与时长无关，一律不走有赞（标准版与 prime 都只收参考图）→ CapCut。
func TestEcommerceAudioVideoGoesCapcutRegardlessOfDuration(t *testing.T) {
	enableEcommerce(t, false, false, 20)

	cases := []struct{ name, body string }{
		{"20 秒 + 顶层 audios", `{"model":"seedance2.5-特惠","seconds":20,"prompt":"好物推荐","audios":["https://example.com/a.mp3"]}`},
		{"30 秒 + 顶层 videos", `{"model":"seedance2.5-特惠","seconds":30,"prompt":"好物推荐","videos":["https://example.com/a.mp4"]}`},
		{"12 秒 + media 音频", `{"model":"seedance2.5-特惠","seconds":12,"prompt":"好物推荐","ratio":"16:9","media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, _ := runRoute(t, tc.body)
			body := decodeBody(t, seen)
			if body["model"] != testEcommerceTarget {
				t.Fatalf("带参考音视频的电商请求必须改派 %s（有赞只收参考图），实际 model=%v",
					testEcommerceTarget, body["model"])
			}
		})
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
	withSecondsRuleOff(payload, testSourceModel)
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
	// 2026-11-15 起标准版最低 10 秒：恰 10 秒仍由有赞标准版优先；
	// <10 秒的请求会跳过它落 CapCut（见 TestShortCapcutTakesOverBelowTenSeconds）。
	enableShortCapcut(t, true, true, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("有赞标准版优先于 CapCut，实际 model=%v", body["model"])
	}
}

// <10 秒的短请求标准版接不住（2026-11-15 上游切换后最低 10 秒）→ 落 CapCut 快速档。
func TestShortCapcutTakesOverBelowTenSeconds(t *testing.T) {
	enableShortCapcut(t, true, true, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
		testShortCapcutTarget:   true,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testShortCapcutTarget {
		t.Fatalf("<10 秒的短请求应落 CapCut 满血档 %s，实际 model=%v", testShortCapcutTarget, body["model"])
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
		t.Fatalf("心笔与有赞都接不住时应落 CapCut 满血档 %s，实际 model=%v", testShortCapcutTarget, body["model"])
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
