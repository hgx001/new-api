package middleware

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// 条件路由的「可观测性」测试。
//
// 为什么单独测日志：规则一（30 秒）和规则四（提示词超长）的目标模型是同一条
// 豆包链路，命中后下游看到的现象完全一样。线上排查时唯一的区分手段就是日志。
//
// 曾经踩过的坑：规则四上了日志、规则一没上。结果 30 秒 + 超长提示词的请求
// 只打了「提示词 5728 字符超过 5000」，看日志的人会以为「必须 30 秒并且超 5000
// 才路由」—— 实际是两条互不依赖的 OR 规则，30 秒单独就能触发。
//
// 所以下面钉死三件事：
//  1. 规则一命中必须留痕（含命中的秒数）
//  2. 规则四命中必须留痕（含提示词长度）
//  3. 两条都满足时，日志必须体现「是规则一先接手的」，不能让人误读成 AND

// captureRouteLogs 把 INFO 日志重定向到内存 buffer，返回读取函数。
//
// logger.LogInfo 写的是 gin.DefaultWriter（见 logger.logHelper），
// 所以替换它就能拿到完整日志，不用碰文件。
func captureRouteLogs(t *testing.T) func() string {
	t.Helper()
	previous := gin.DefaultWriter
	buffer := &bytes.Buffer{}
	gin.DefaultWriter = buffer
	t.Cleanup(func() {
		gin.DefaultWriter = previous
	})
	return func() string {
		return buffer.String()
	}
}

// 规则一（时长）命中时必须打日志。
//
// 这条用例在规则一没有日志时会失败 —— 也正是它暴露了「30 秒那单在日志里
// 完全找不到路由记录」的线上问题。
func TestSecondsRouteEmitsLog(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testTargetModel {
		t.Fatalf("前置条件失败：30 秒请求没有改派")
	}

	logs := readLogs()
	if !strings.Contains(logs, "条件路由") {
		t.Fatalf("规则一命中后没有任何条件路由日志，日志内容=%q", logs)
	}
	if !strings.Contains(logs, "30 秒") {
		t.Fatalf("规则一日志没写清命中的秒数，日志内容=%q", logs)
	}
	if !strings.Contains(logs, testSourceModel) || !strings.Contains(logs, testTargetModel) {
		t.Fatalf("规则一日志没写清改派方向（%s → %s），日志内容=%q", testSourceModel, testTargetModel, logs)
	}
}

// 规则四（提示词超长）命中时必须打日志，且带上提示词长度。
func TestPromptRouteEmitsLog(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+1), map[string]interface{}{
		"seconds": 10,
	})
	runRoute(t, body)

	logs := readLogs()
	if !strings.Contains(logs, "条件路由") {
		t.Fatalf("规则四命中后没有条件路由日志，日志内容=%q", logs)
	}
	if !strings.Contains(logs, "提示词") {
		t.Fatalf("规则四日志没写清触发原因（提示词超长），日志内容=%q", logs)
	}
	if !strings.Contains(logs, "5001") {
		t.Fatalf("规则四日志没写清提示词实际长度，日志内容=%q", logs)
	}
}

// 两条规则同时满足时，日志必须让人看出「是规则一先接手的」。
//
// 这是用户误读的原始场景：30 秒 + 5728 字符。规则一排在前面先命中，
// 此时不应该再打「提示词超过 5000」—— 否则看日志的人会以为规则四是
// 「30 秒 + 超长」的 AND 条件。
func TestSecondsRouteLogWinsOverPromptLogWhenBothMatch(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	body := buildPromptBody(t, strings.Repeat("a", promptRouteLimit+228), map[string]interface{}{
		"seconds": 30,
	})
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testTargetModel {
		t.Fatalf("前置条件失败：30 秒 + 超长提示词没有改派")
	}

	logs := readLogs()
	if !strings.Contains(logs, "30 秒") {
		t.Fatalf("两条件同时满足时应由规则一留痕（含秒数），日志内容=%q", logs)
	}
	if strings.Contains(logs, "超过 5000") {
		t.Fatalf("规则一已接手，不该再打规则四的日志（会让人误读成 AND 条件），日志内容=%q", logs)
	}
}

// 规则二（比例）命中时必须留痕：它和规则一/四的目标模型不同，但同样是
// 「按调度偏好省钱」的隐式改派，不打日志客户会以为请求按原模型跑了。
func TestRatioRouteEmitsLog(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)
	readLogs := captureRouteLogs(t)

	body := `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testRatioTargetModel {
		t.Fatalf("前置条件失败：9:16 请求没有改派到 %s", testRatioTargetModel)
	}

	logs := readLogs()
	if !strings.Contains(logs, "条件路由") {
		t.Fatalf("规则二命中后没有条件路由日志，日志内容=%q", logs)
	}
	if !strings.Contains(logs, "9:16") {
		t.Fatalf("规则二日志没写清命中的比例，日志内容=%q", logs)
	}
}

// 规则三（首尾帧）命中时必须留痕：这是四条规则里唯一「能力约束」型的改派
// （不改派就必然失败），排查时最需要一眼看到。
func TestFramesRouteEmitsLog(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)
	readLogs := captureRouteLogs(t)

	body := `{"model":"seedance2.5-特惠","seconds":"10","prompt":"a cat","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testFramesTargetModel {
		t.Fatalf("前置条件失败：首尾帧请求没有改派到 %s", testFramesTargetModel)
	}

	logs := readLogs()
	if !strings.Contains(logs, "条件路由") {
		t.Fatalf("规则三命中后没有条件路由日志，日志内容=%q", logs)
	}
	if !strings.Contains(logs, "首尾帧") {
		t.Fatalf("规则三日志没写清触发原因（首尾帧），日志内容=%q", logs)
	}
}

// 没命中任何规则时不该有路由日志 —— 避免日志噪音掩盖真正的路由事件。
func TestNoRouteLogWhenNoRuleMatches(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)

	if logs := readLogs(); strings.Contains(logs, "条件路由") {
		t.Fatalf("未命中规则却打了条件路由日志，日志内容=%q", logs)
	}
}

// 规则一命中但渠道预检不通过（没有可用渠道）时不该打路由日志：
// 请求实际没改派，打了日志会误导排查。
func TestSecondsRouteNoLogWhenChannelUnavailable(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)
	stubChannelAvailable(t, false)
	readLogs := captureRouteLogs(t)

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] == testTargetModel {
		t.Fatalf("前置条件失败：目标渠道不可用却仍改派了")
	}

	if logs := readLogs(); strings.Contains(logs, "条件路由") {
		t.Fatalf("没有真正改派却打了条件路由日志，日志内容=%q", logs)
	}
}
