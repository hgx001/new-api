package middleware

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// 规则四（提示词超长）的测试。
//
// 背景：源链路（MegaAI 的 wan-3.0）对提示词有 5000 字符上限，超了必失败；
// 目标链路（豆包官网 Seedance 2.5）能吃下更长的提示词。所以「提示词超长」
// 和「30 秒」「9:16」「首尾帧」一样，是一条独立的改派条件。
//
// 与规则一的关键差别：**规则一强制把时长改成 30 秒**（它的触发条件就是 30 秒），
// 规则四必须**保留客户原本要的时长** —— 客户要 10 秒，改派后还是 10 秒。
// 豆包只认 5/10/15/30 四档，其它时长不路由（宁可不路由，也不悄悄改客户参数）。

const promptRouteLimit = 5000

// buildPromptBody 造一个带指定提示词长度的请求体。
//
// 用 json.Marshal 而不是拼字符串：5000 个字符里可能有需要转义的字符，
// 手工拼接会把「转义后长度」和「原始长度」搞混。
func buildPromptBody(t *testing.T, prompt string, extra map[string]interface{}) string {
	t.Helper()
	body := map[string]interface{}{
		"model":  testSourceModel,
		"prompt": prompt,
	}
	for key, value := range extra {
		body[key] = value
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(encoded)
}

// TestPromptRouteTriggersWhenPromptExceedsLimit 是规则四的主用例：
// 5001 个字符必须改派到豆包，且来源模型被记下来（计费/日志都靠它）。
func TestPromptRouteTriggersWhenPromptExceedsLimit(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	seen, originModel := runRoute(t, body)

	out := decodeBody(t, seen)
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s（超长提示词没被改派）", out["model"], testTargetModel)
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

// 老板的原话是「大于 5000」—— 正好 5000 不触发。
// 边界写错的后果是双向的：写成 >= 会让一条合法请求白白改派，
// 写成 > 5001 会让真正的超长请求漏过去。
func TestPromptRouteNotTriggerAtExactLimit(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit), map[string]interface{}{
		"seconds": 10,
	})
	seen, originModel := runRoute(t, body)

	if seen != body {
		t.Fatalf("正好 5000 字符不该改派，body 却被改了:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
	}
}

func TestPromptRouteNotTriggerForShortPrompt(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, "a cat walking in the rain", map[string]interface{}{
		"seconds": 10,
	})
	seen, _ := runRoute(t, body)

	if seen != body {
		t.Fatalf("短提示词不该改派，body 却被改了:\n got %s\nwant %s", seen, body)
	}
}

// 按**字符**（rune）算，不按字节算。
//
// 5000 个汉字按字节是 15000，按字符是 5000 —— 用 len() 判断会让
// 「刚好在限制内」的中文提示词被误判成超长。反过来，如果上游真按字节卡，
// 这个用例会保护我们不去猜，而是保持当前口径（字符）。
func TestPromptRouteCountsChineseCharactersAsOneRune(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	// 正好 5000 个汉字：字节数 15000，但字符数 5000 → 不该改派。
	atLimit := buildPromptBody(t, strings.Repeat("汉", promptRouteLimit), map[string]interface{}{
		"seconds": 10,
	})
	if seen, _ := runRoute(t, atLimit); seen != atLimit {
		t.Fatalf("5000 个汉字按字符算正好在限制内，不该改派")
	}

	// 5001 个汉字 → 改派。
	overLimit := buildPromptBody(t, strings.Repeat("汉", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	seen, _ := runRoute(t, overLimit)
	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("5001 个汉字应改派到 %s，实际 model = %v", testTargetModel, got)
	}
}

// 规则四必须**保留客户要的时长**。
//
// 复用规则一的改写逻辑会把 seconds 强行写成 30 —— 客户要 10 秒，
// 最后拿到 30 秒的片并按 30 秒付钱，属于错交付。
func TestPromptRouteKeepsRequestedDuration(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	seen, _ := runRoute(t, body)

	out := decodeBody(t, seen)
	if out["seconds"] != "10" {
		t.Fatalf("seconds = %v, want \"10\"（规则四不能把时长改成 30）", out["seconds"])
	}
}

// 豆包只认 5/10/15/30 四档。12 秒的请求路由过去会被上游 400，
// 所以这种请求不路由 —— 留在源链路，至少还有可能成功。
func TestPromptRouteSkipsUnsupportedDuration(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 12,
	})
	seen, _ := runRoute(t, body)

	if seen != body {
		t.Fatalf("12 秒不在豆包档位内，不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 没传时长时按源链路默认 5 秒算，5 秒是豆包支持的档位 → 可以改派。
func TestPromptRouteDefaultsToFiveSecondsWhenDurationMissing(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), nil)
	seen, _ := runRoute(t, body)

	out := decodeBody(t, seen)
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s", out["model"], testTargetModel)
	}
	if out["seconds"] != "5" {
		t.Fatalf("seconds = %v, want \"5\"（缺省按源链路默认时长）", out["seconds"])
	}
}

// 目标链路额度用完时，规则一会被自动暂停。规则四打的是同一条链路、
// 同一份额度，必须一起停 —— 否则暂停期间超长提示词照样打过去、照样失败。
func TestPromptRouteRespectsPausedWindow(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	pauseSecondsRule(t, time.Now().Unix()+3600)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	seen, _ := runRoute(t, body)

	if seen != body {
		t.Fatalf("暂停期内不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 规则四（提示词超长）命中时，分辨率必须跟着透传（豆包三档全支持）。
func TestPromptRouteForwardsExplicitResolution(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds":    10,
		"resolution": "720p",
	})
	seen, _ := runRoute(t, body)

	out := decodeBody(t, seen)
	if out["model"] != testTargetModel {
		t.Fatalf("超长提示词未改派（model=%v）", out["model"])
	}
	if out["resolution"] != "720p" {
		t.Fatalf("resolution = %v, want 720p", out["resolution"])
	}
}

// 带参考视频/音频 → 不路由（目标链路只吃参考图）。
func TestPromptRouteSkipsReferenceVideo(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
		"media": []map[string]string{
			{"type": "reference_video", "url": "https://example.com/ref.mp4"},
		},
	})
	seen, _ := runRoute(t, body)

	if seen != body {
		t.Fatalf("带参考视频不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 首尾帧优先于长提示词：首尾帧只有 wan3 能跑，豆包根本不支持。
// 两条规则同时命中时必须走 wan3，否则带首尾帧的请求会直接失败。
func TestPromptRouteKeepsFramesRulePriority(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testRatioTargetModel)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
		"media": []map[string]string{
			{"type": "first_frame", "url": "https://example.com/first.png"},
		},
	})
	seen, _ := runRoute(t, body)

	if got := decodeBody(t, seen)["model"]; got != testRatioTargetModel {
		t.Fatalf("首尾帧 + 长提示词应走 %s（首尾帧规则优先），实际 model = %v", testRatioTargetModel, got)
	}
}

// 改派后计费仍按源模型（按秒 ¥0.20/秒）算，而目标 adaptor 是按次的、
// 不返回 seconds。不补这个秒数，10 秒的请求只按 1 秒收钱 —— 少收 9 秒。
func TestPromptRouteRecordsBillingSeconds(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	rec := runRouteFull(t, body)

	if !rec.hasBillingSec {
		t.Fatalf("规则四命中后没有记录计费秒数（key 缺失）")
	}
	if rec.billingSeconds != 10 {
		t.Fatalf("计费秒数 = %v, want 10", rec.billingSeconds)
	}
}

// 规则四与规则一互相独立：15 秒不是 30 秒，规则一不命中，
// 但提示词超长，规则四必须接手。
func TestPromptRouteIndependentOfSecondsTrigger(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 15,
	})
	seen, _ := runRoute(t, body)

	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("15 秒 + 超长提示词应改派，实际 model = %v", got)
	}
}

// 非源模型的请求一个字节都不能动 —— 规则四只认 SourceModel。
func TestPromptRouteIgnoresOtherModels(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := fmt.Sprintf(`{"model":"seedance-2.5","seconds":10,"prompt":%q}`,
		strings.Repeat("a", promptRouteLimit+1))
	seen, _ := runRoute(t, body)

	if seen != body {
		t.Fatalf("非源模型不该被改派:\n got %s\nwant %s", seen, body)
	}
}
