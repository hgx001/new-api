package middleware

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// 规则四（提示词超长）与「时长」的交叉行为。
//
// 2026-11-09 用户口径：豆包 2.5 支持任意正整数秒，改派时**原样保留**客户秒数，
// 不再有四档白名单挡板。规则四只对非法秒数（<=0）放弃改派。

// promptRouteTestPrompt 是一段真实长度的超长提示词（6171 字，与生产 tasks 1045/1046 同量级）。
func promptRouteTestPrompt() string {
	return strings.Repeat("画面细节", 1542) + "，" // 4×1542 + 1 = 6169，> 5000
}

// 显式 2 秒：原样透传改派到豆包，不得被改成 5/15/30。
func TestPromptRouteKeepsExplicitSecondsWhenRerouting(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	prompt := promptRouteTestPrompt()
	body := fmt.Sprintf(`{"model":%q,"seconds":2,"prompt":%q}`, testSourceModel, prompt)
	rewritten, origin := runRoute(t, body)

	if origin != testSourceModel {
		t.Fatalf("来源模型 = %q, want %q（超长改派必须留下原始模型名用于计费）", origin, testSourceModel)
	}
	var parsed map[string]interface{}
	if err := common.Unmarshal([]byte(rewritten), &parsed); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	if model, _ := parsed["model"].(string); model != testTargetModel {
		t.Fatalf("模型 = %q, want %q", model, testTargetModel)
	}
	if seconds, _ := parsed["seconds"].(string); seconds != "2" {
		t.Fatalf("seconds = %q, want \"2\"（必须原样透传，禁止吸附档位）", seconds)
	}
	if p, _ := parsed["prompt"].(string); p != prompt {
		t.Fatal("改派时提示词必须逐字不变")
	}
}

// 显式 0/负数：非法秒数，规则四放弃改派，请求留在源链路。
func TestPromptRouteSkipsNonPositiveSeconds(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	prompt := promptRouteTestPrompt()
	body := fmt.Sprintf(`{"model":%q,"seconds":0,"prompt":%q}`, testSourceModel, prompt)
	rewritten, origin := runRoute(t, body)

	if origin != "" {
		t.Fatalf("0 秒不该被改派，却记下了来源模型 %q", origin)
	}
	var parsed map[string]interface{}
	if err := common.Unmarshal([]byte(rewritten), &parsed); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	if model, _ := parsed["model"].(string); model != testSourceModel {
		t.Fatalf("模型被改成了 %q，0 秒不是合法时长，不该改派", model)
	}
}

// 不传时长：源链路默认 5 秒，正好在豆包档位内 → 被改派到豆包。
//
// 生产实例：tasks id=1045/1046，6171 字、未传时长，落到渠道 20（豆包），
// 因「渠道今日额度已用完」失败。所以「想测源链路的超长拒绝」就**不能省略时长**。
func TestPromptRouteReroutesLongPromptWhenSecondsOmitted(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	prompt := promptRouteTestPrompt()
	body := fmt.Sprintf(`{"model":%q,"prompt":%q}`, testSourceModel, prompt)
	rewritten, origin := runRoute(t, body)

	if origin != testSourceModel {
		t.Fatalf("来源模型 = %q, want %q（超长改派必须留下原始模型名用于计费）", origin, testSourceModel)
	}
	var parsed map[string]interface{}
	if err := common.Unmarshal([]byte(rewritten), &parsed); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	if model, _ := parsed["model"].(string); model != testTargetModel {
		t.Fatalf("模型 = %q, want %q（不传时长按 5 秒，在档位内应改派）", model, testTargetModel)
	}
}
