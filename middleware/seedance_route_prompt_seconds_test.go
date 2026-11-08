package middleware

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// 规则四（提示词超长）与「时长档位」的交叉行为。
//
// 为什么单独一组用例：规则四的触发条件只写了「提示词 > 5000」，很容易被理解成
// 「任何超长提示词都会被改派到豆包」。实际还要过一道 doubaoDurationTiers
// （只认 5/10/15/30）—— 客户显式要 2 秒时，规则四**放弃改派**，请求留在
// 源链路（有赞 wan3），照样撞上 5000 上限。这条差异直接决定「测什么」：
// 要复现源链路的超长拒绝，必须显式传 2 秒；不传时长会被当成 5 秒改派走。

// promptRouteTestPrompt 是一段真实长度的超长提示词（6171 字，与生产 tasks 1045/1046 同量级）。
func promptRouteTestPrompt() string {
	return strings.Repeat("画面细节", 1542) + "，" // 4×1542 + 1 = 6169，> 5000
}

// 显式 2 秒：不在豆包档位内 → 不改派，请求留在源链路。
//
// 这正是「2 秒 + 超长提示词」在改造前的真实表现：改派这道兜底用不上，
// 请求带着 6000+ 字的提示词打到有赞，由上游按 5000 上限拒单。
func TestPromptRouteSkipsSecondsOutsideDoubaoTiers(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	prompt := promptRouteTestPrompt()
	body := fmt.Sprintf(`{"model":%q,"seconds":2,"prompt":%q}`, testSourceModel, prompt)
	rewritten, origin := runRoute(t, body)

	if origin != "" {
		t.Fatalf("2 秒不该被改派，却记下了来源模型 %q", origin)
	}
	var parsed map[string]interface{}
	if err := common.Unmarshal([]byte(rewritten), &parsed); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	if model, _ := parsed["model"].(string); model != testSourceModel {
		t.Fatalf("模型被改成了 %q，2 秒不在豆包档位（5/10/15/30）内，不该改派", model)
	}
	if p, _ := parsed["prompt"].(string); p != prompt {
		t.Fatal("未改派时提示词必须逐字不变")
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
