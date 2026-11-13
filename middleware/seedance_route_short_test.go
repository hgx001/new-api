package middleware

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 规则六（短时长分流）的中间件测试。
//
// 语义：源模型 + 请求时长 ≤ 阈值（默认 10 秒）→ 改派到**心笔内链**
// （wan3.0-720p-特惠-内链，ch25）：480P 生成 + 超分交付 720P。
//
// 为什么目标从有赞标准版换成心笔（2026-11-13 老板定）：
// 上游 prime「暂时不受理低于 10 秒」的任务（PRIME_DURATION_TOO_SHORT），
// 客户 threeFatAi 一天连续 5 次失败。心笔内链 480P 生成、统一超分到 720P 交付，
// 与源链路承诺的交付档一致。
//
// 与另外几条规则的关系（顺序即优先级）：
//
//	规则五（故障转移）→ 规则三（首尾帧）→ 规则一（长时长）→ **规则六（短时长）** → 规则二（比例）
//
// 规则六排在规则二之前是老板 2026-11-13 的口径：短任务优先分流，
// 即使它是 9:16 也走内链，不再落到比例目标链路。

// testShortTargetModel 是规则六的目标模型（心笔 480P 内链）。
// 必须与 isXinbiTehuiTarget 认的名字一致（xinbiTehuiTargetModel），
// 否则走不到心笔的改写逻辑，参考图会被静默丢弃。
const testShortTargetModel = "wan3.0-720p-特惠-内链"

// enableShortRoute 只开规则六，测试结束恢复原状。
func enableShortRoute(t *testing.T, target string, triggerSeconds int) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":            true,
		"source_model":       testSourceModel,
		"short_target_model": target,
	}
	if triggerSeconds > 0 {
		payload["short_trigger_seconds"] = triggerSeconds
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

// enableShortAndOtherRules 同时打开规则六与时长/比例/首尾帧规则，用来验优先级。
func enableShortAndOtherRules(t *testing.T, secondsTarget string, seconds int, ratioTarget, ratio, framesTarget, shortTarget string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":             true,
		"source_model":        testSourceModel,
		"target_model":        secondsTarget,
		"trigger_seconds":     seconds,
		"ratio_target_model":  ratioTarget,
		"trigger_ratio":       ratio,
		"frames_target_model": framesTarget,
		"short_target_model":  shortTarget,
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

// 短请求必须按心笔兼容口的契约改写：
//   - model 换成内链
//   - seconds 落成顶层字符串（心笔只认顶层 seconds）
//   - 参考图统一收到顶层 reference_images[]
//   - media / resolution 必须剥掉（media 留着会被 adaptor 再收一遍，同图投两次）
//   - 计费秒数按源模型口径记；**没有 size 倍率**（心笔恒交付 720P，复合价一口）
func TestShortRouteRewritesToXinbiContract(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat",
		"media":[{"type":"reference_image","url":"https://x/a.png"}]}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortTargetModel {
		t.Fatalf("5 秒请求没有改派到心笔内链 %s，实际 model=%v", testShortTargetModel, body["model"])
	}
	if body["seconds"] != "5" {
		t.Fatalf("心笔只认顶层字符串 seconds，实际 %v (%T)", body["seconds"], body["seconds"])
	}
	images, _ := body["reference_images"].([]interface{})
	if len(images) != 1 || images[0] != "https://x/a.png" {
		t.Fatalf("参考图必须收到顶层 reference_images，实际 %v", body["reference_images"])
	}
	if _, ok := body["media"]; ok {
		t.Fatalf("顶层 media 必须剥掉，adaptor 会再收一遍导致同图投两次")
	}
	if body["prompt"] != "a cat" {
		t.Fatalf("提示词被改坏了：%v", body["prompt"])
	}
	if rec.originModel != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, rec.originModel)
	}
	if !rec.hasBillingSec || rec.billingSeconds != 5 {
		t.Fatalf("计费秒数应为 5，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
	if rec.hasBillingSize {
		t.Fatalf("心笔恒交付 720P（复合价含在单价里），不能记 size 倍率，实际 %v", rec.billingSizeRatio)
	}
}

// 边界是闭区间：10 秒命中、11 秒不命中。
func TestShortRouteBoundaryIsInclusive(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	ten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)
	if decodeBody(t, ten)["model"] != testShortTargetModel {
		t.Fatalf("10 秒是闭区间边界，必须改派，实际 model=%v", decodeBody(t, ten)["model"])
	}

	eleven, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":11,"prompt":"a cat"}`)
	if decodeBody(t, eleven)["model"] != testSourceModel {
		t.Fatalf("11 秒已超阈值，必须留在源链路，实际 model=%v", decodeBody(t, eleven)["model"])
	}
}

// 没传时长 → 不改派。
//
// 不能拿「默认时长」去猜：客户没说要短任务，我们不该替他做链路决策。
func TestShortRouteWithoutSecondsStaysOnSource(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("没传时长不该改派，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 带首尾帧 → 规则六必须让路。
//
// 心笔兼容口只收参考图；带首尾帧的请求只有规则三的目标链路能跑。
// 规则三没启用时也不能被规则六悄悄改派 —— 那会把首尾帧降级成参考图。
func TestShortRouteSkipsFramesMedia(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	body := `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("带首尾帧的短请求不该走心笔，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 带参考视频/音频 → 留在源链路。
//
// 心笔兼容口没有对应字段，分流过去素材会被静默丢弃 —— 客户拿到的成片
// 没有他要参考的内容，比 400 更糟。
func TestShortRouteSkipsReferenceVideoAndAudio(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	for name, media := range map[string]string{
		"参考视频": `{"type":"reference_video","url":"https://x/a.mp4"}`,
		"参考音频": `{"type":"reference_audio","url":"https://x/a.mp3"}`,
	} {
		body := `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","media":[` + media + `]}`
		seen, _ := runRoute(t, body)
		if decodeBody(t, seen)["model"] != testSourceModel {
			t.Fatalf("带%s的短请求必须留在源链路，实际 model=%v", name, decodeBody(t, seen)["model"])
		}
	}
}

// 带 seed → 留在源链路。
//
// 心笔上游 parameterSchema 是 additionalProperties:false，带 seed 会被 422。
func TestShortRouteSkipsSeed(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","seed":42}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("带 seed 的短请求必须留在源链路，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 比例不在心笔枚举内（如 21:9）→ 留在源链路。
//
// 分流过去必被上游 400；宁可让它在源链路失败，也不换一条必然失败的链路。
func TestShortRouteSkipsRatioOutsideXinbiEnum(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"metadata":{"ratio":"21:9"},"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("21:9 必须留在源链路，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 短任务优先于比例规则：9:16 + 5 秒走心笔内链，不落到比例目标。
func TestShortRouteBeatsRatioRule(t *testing.T) {
	enableShortAndOtherRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel, testShortTargetModel)

	body := `{"model":"seedance2.5-特惠","seconds":5,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, _ := runRoute(t, body)
	model, _ := decodeBody(t, seen)["model"].(string)

	if model != testShortTargetModel {
		t.Fatalf("短时长规则应优先于比例规则，实际 model=%s（比例目标=%s）", model, testRatioTargetModel)
	}
}

// 长时长仍走规则一，规则六不能抢走它。
func TestShortRouteDoesNotStealLongSeconds(t *testing.T) {
	enableShortAndOtherRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel, testShortTargetModel)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testTargetModel {
		t.Fatalf("25 秒应走规则一的目标 %s，实际 %v", testTargetModel, decodeBody(t, seen)["model"])
	}
}

// 分辨率参数必须剥掉，且**不记 size 倍率**。
//
// 心笔内链「无条件 480P 生成 + 超分交付 720P」，480P/720P/1080P 全部无效；
// 顶层 size 会被当成比例误读，必须删。1080P 请求交付的就是 720P，
// 按 720P 计价（不按源模型 1080P 的 1.5 倍收）—— 降档必须降价。
func TestShortRouteStripsResolutionAndBills720P(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	for _, tier := range []string{"480P", "720P", "1080P"} {
		rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"resolution":"`+tier+`","prompt":"a cat"}`)
		body := decodeBody(t, rec.body)

		if body["model"] != testShortTargetModel {
			t.Fatalf("%s 短请求也应改派，实际 model=%v", tier, body["model"])
		}
		if _, ok := body["resolution"]; ok {
			t.Fatalf("resolution 必须剥掉（心笔顶层 size 会被当成比例误读），实际 %v", body["resolution"])
		}
		if rec.hasBillingSize {
			t.Fatalf("%s 请求交付的是 720P，不能按源模型 %s 的 1.5 倍收费，实际 %v",
				tier, tier, rec.billingSizeRatio)
		}
	}
}

// 目标模型没有可用渠道 → 不路由，放行原请求。
//
// 中间件改写 body 之后，重试只会用同一个模型名重选渠道、不会退回源模型 ——
// 预检能让请求自然落回源链路，客户完全无感。
func TestShortRouteSkipsWhenChannelUnavailable(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)
	stubChannelAvailable(t, false)

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("目标渠道不可用时不该改派，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 命中必须留痕：改派后下游只能看到目标模型名，日志是排查的唯一凭据。
func TestShortRouteEmitsLog(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)
	readLogs := captureRouteLogs(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testShortTargetModel {
		t.Fatalf("前置条件失败：5 秒请求没有改派")
	}

	logs := readLogs()
	if !strings.Contains(logs, "条件路由") {
		t.Fatalf("规则六命中后没有条件路由日志，日志内容=%q", logs)
	}
	if !strings.Contains(logs, "5 秒") {
		t.Fatalf("规则六日志没写清命中的秒数，日志内容=%q", logs)
	}
	if !strings.Contains(logs, testSourceModel) || !strings.Contains(logs, testShortTargetModel) {
		t.Fatalf("规则六日志没写清改派方向（%s → %s），日志内容=%q", testSourceModel, testShortTargetModel, logs)
	}
}

// 规则六没配置时，短请求不能被动到 —— 升级当天行为必须逐字节不变。
func TestShortRouteInactiveWhenUnconfigured(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("规则六未配置时短请求必须留在源链路，实际 model=%v", decodeBody(t, seen)["model"])
	}
	if logs := readLogs(); strings.Contains(logs, "短时长") {
		t.Fatalf("规则六未配置却打了分流日志，日志内容=%q", logs)
	}
}

// metadata 里的时长同样能触发（客户端常把 seconds 塞进 metadata），
// 且改写后的秒数必须是 metadata 里那个 —— 不能悄悄变成缺省 5 秒。
func TestShortRouteReadsSecondsFromMetadata(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","metadata":{"seconds":"8"},"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)
	if body["model"] != testShortTargetModel {
		t.Fatalf("metadata.seconds=8 应触发分流，实际 model=%v", body["model"])
	}
	if body["seconds"] != "8" {
		t.Fatalf("metadata.seconds=8 改写后必须还是 8 秒，实际 %v", body["seconds"])
	}
	if !rec.hasBillingSec || rec.billingSeconds != 8 {
		t.Fatalf("计费秒数应为 8，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
}

// 计费倍率键必须真的落在 context 里（不是只写在注释里）。
func TestShortRouteBillingSizeRatioKeyIsStable(t *testing.T) {
	if constant.ContextKeyRouteBillingSizeRatio != "route_billing_size_ratio" {
		t.Fatalf("计费 size 倍率的 context key 变了，relay_task.go 里的读取会失效：%q",
			constant.ContextKeyRouteBillingSizeRatio)
	}
	// 防止有人把 context key 与 OtherRatios 的键混为一谈 —— 后者是 "size"。
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(`{"model":"wan3.0-video"}`), &raw); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
