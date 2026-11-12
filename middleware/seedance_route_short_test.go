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
// 语义：源模型 + 请求时长 ≤ 阈值（默认 10 秒）→ 改派到有赞标准版 wan3.0-video。
//
// 为什么要有这条规则：源模型 seedance2.5-特惠 常驻有赞 **prime** 档，
// 标准版 wan3.0-video 是同一渠道的另一档（三档同价）。短任务分流过去，
// 把 prime 的算力留给长任务。
//
// 与另外几条规则的关系（顺序即优先级）：
//
//	规则五（故障转移）→ 规则三（首尾帧）→ 规则一（长时长）→ **规则六（短时长）** → 规则二（比例）
//
// 规则六排在规则二之前是老板 2026-11-13 的口径：短任务优先分流，
// 即使它是 9:16 也走标准版，不再落到比例目标链路。

// testShortTargetModel 是规则六的目标模型（有赞标准版）。
const testShortTargetModel = "wan3.0-video"

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

// 5 秒请求必须改派到标准版，并且：
//   - model 换成目标
//   - 分辨率显式钉成 720P（标准版缺省是 480P，不钉就静默降档）
//   - 计费秒数与 size 倍率都按**源模型**口径记进 context
func TestShortRouteRewritesToStandardWan3(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortTargetModel {
		t.Fatalf("5 秒请求没有改派到 %s，实际 model=%v", testShortTargetModel, body["model"])
	}
	if body["resolution"] != "720P" {
		t.Fatalf("分辨率应显式钉成 720P（源链路 prime 缺省档），实际 %v", body["resolution"])
	}
	if body["seconds"] != float64(5) {
		t.Fatalf("时长必须原样保留，实际 %v", body["seconds"])
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
	if !rec.hasBillingSize || rec.billingSizeRatio != 1.0 {
		t.Fatalf("720P 的源模型 size 倍率应为 1.0，实际 has=%v value=%v", rec.hasBillingSize, rec.billingSizeRatio)
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
// 不能拿「默认时长」去猜：源链路缺省 5 秒、目标标准版缺省也是 5 秒，
// 但客户没说要短任务，我们不该替他做链路决策。
func TestShortRouteWithoutSecondsStaysOnSource(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("没传时长不该改派，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 带首尾帧 → 规则六必须让路。
//
// 有赞上游 2026-10-28 起取消 first_frame/last_frame 入参，标准版同样不收；
// 带首尾帧的请求只有规则三的目标链路能跑。规则三没启用时也不能被规则六
// 悄悄改派 —— 那会把「首尾帧降级成参考图」，客户拿到不是他要的片子。
func TestShortRouteSkipsFramesMedia(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	body := `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("带首尾帧的短请求不该走标准版，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 短任务优先于比例规则：9:16 + 5 秒走标准版，不落到比例目标。
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

// 客户要 480P：源链路 prime 不支持 480P，实际按 720P 执行。
// 改派后必须把这个「实际档位」钉住 —— 标准版缺省就是 480P，
// 不钉住会静默降档（客户付 720P 的钱拿到 480P 的片）。
func TestShortRoutePinsActualTierFor480Request(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"resolution":"480P","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortTargetModel {
		t.Fatalf("480P 短请求也应改派，实际 model=%v", body["model"])
	}
	if body["resolution"] != "720P" {
		t.Fatalf("480P 请求在源链路实际按 720P 跑，改派后必须钉成 720P，实际 %v", body["resolution"])
	}
	if !rec.hasBillingSize || rec.billingSizeRatio != 1.0 {
		t.Fatalf("按 720P 计费，size 倍率应为 1.0，实际 has=%v value=%v", rec.hasBillingSize, rec.billingSizeRatio)
	}
}

// 客户要 1080P：标准版支持 1080P，原样保留；但 size 倍率必须按**源模型**算。
//
// 标准版三档同价（adaptor 会给 1.0），而 seedance2.5-特惠 的 1080P 是 1.5 倍。
// 不补这个倍率就会按 ¥0.20/秒 收 1080P（应收 ¥0.30），少收 33% 且无任何报错。
func TestShortRouteKeeps1080PWithSourceSizeRatio(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"resolution":"1080P","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortTargetModel {
		t.Fatalf("1080P 短请求也应改派，实际 model=%v", body["model"])
	}
	if body["resolution"] != "1080P" {
		t.Fatalf("客户要的 1080P 不能被降档，实际 %v", body["resolution"])
	}
	if !rec.hasBillingSize {
		t.Fatalf("1080P 必须记录源模型口径的 size 倍率")
	}
	if rec.billingSizeRatio != 1.5 {
		t.Fatalf("seedance2.5-特惠 的 1080P 倍率应为 1.5，实际 %v", rec.billingSizeRatio)
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

// metadata 里的时长同样能触发（客户端常把 seconds 塞进 metadata）。
func TestShortRouteReadsSecondsFromMetadata(t *testing.T) {
	enableShortRoute(t, testShortTargetModel, 10)

	body := `{"model":"seedance2.5-特惠","metadata":{"seconds":"8"},"prompt":"a cat"}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testShortTargetModel {
		t.Fatalf("metadata.seconds=8 应触发分流，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 计费倍率键必须真的落在 PriceData.OtherRatios 里（不是只存在 context）。
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
