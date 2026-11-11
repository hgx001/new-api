package middleware

import (
	"encoding/json"
	"strings"
	"testing"
)

// 「提示词超长不再触发条件路由」的回归测试（2026-11-11）。
//
// 原规则四（提示词 > 5000 字符 → 豆包）已删除：它几乎总被规则一抢先，
// 且超长提示词本就该交给后面的 Wan3PromptCompress 压缩（压不下来明确 400）。
//
// 这里钉死三件事，防止有人把隐式改派又加回来：
//  1. 超长提示词**本身**不触发任何改派，请求体逐字节不变；
//  2. 规则一（时长）与规则二（比例）仍照常生效，不受提示词长度影响；
//  3. 长提示词 + 30 秒走的是规则一，不是「超长」改派。

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

// 超长提示词 + 短时长：不该有任何改派，body 一个字节都不能动。
//
// 这条是「规则四已删除」的主断言：以前它会改派到 testTargetModel。
func TestLongPromptDoesNotTriggerRoute(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	seen, originModel := runRoute(t, body)

	if seen != body {
		t.Fatalf("超长提示词不该触发改派，body 却被改了:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty（没改派就不该记计费原点）", originModel)
	}
	if got := decodeBody(t, seen)["model"]; got != testSourceModel {
		t.Fatalf("model = %v, want %s", got, testSourceModel)
	}
}

// 中文按字符算 6001 字（旧规则同样会命中），也必须留在源模型。
func TestLongChinesePromptDoesNotTriggerRoute(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("汉", promptRouteLimit+1000), map[string]interface{}{
		"seconds": 12,
	})
	seen, originModel := runRoute(t, body)

	if seen != body || originModel != "" {
		t.Fatalf("中文超长提示词不该触发改派（origin=%q）", originModel)
	}
}

// 长提示词 + 30 秒：仍然由规则一改派到豆包。
// 证明「删规则四」没有把正常的时长改派一起删掉。
func TestLongPromptStillHitsSecondsRule(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 30,
	})
	seen, originModel := runRoute(t, body)

	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("30 秒请求仍应改派到 %s，实际 model = %v", testTargetModel, got)
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

// 长提示词 + 9:16：仍然由规则二改派（目标模型由配置决定）。
func TestLongPromptStillHitsRatioRule(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds":  10,
		"metadata": map[string]interface{}{"ratio": "9:16"},
	})
	seen, _ := runRoute(t, body)

	if got := decodeBody(t, seen)["model"]; got != testRatioTargetModel {
		t.Fatalf("9:16 请求仍应改派到 %s，实际 model = %v", testRatioTargetModel, got)
	}
}

// 长提示词 + 首尾帧：仍然由规则三（能力约束）优先改派。
func TestLongPromptStillHitsFramesRule(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
		"media": []map[string]string{
			{"type": "first_frame", "url": "https://example.com/first.png"},
		},
	})
	seen, _ := runRoute(t, body)

	if got := decodeBody(t, seen)["model"]; got != testFramesTargetModel {
		t.Fatalf("首尾帧 + 长提示词应走 %s，实际 model = %v", testFramesTargetModel, got)
	}
}

// 没传时长的超长提示词也不改派（旧规则会按 5 秒档改派到豆包）。
func TestLongPromptWithoutDurationDoesNotTriggerRoute(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), nil)
	seen, originModel := runRoute(t, body)

	if seen != body || originModel != "" {
		t.Fatalf("未传时长的超长提示词不该触发改派（origin=%q）", originModel)
	}
}
