package middleware

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

// 「提示词超长 × 时长」的回归测试（2026-11-11）。
//
// 原规则四会按提示词长度把请求改派到豆包，并按「没传时长 → 5 秒」补一个
// 默认档；规则四删除后，这些请求**一律留在源模型**，由后面的压缩中间件处理。
//
// 提示词长度不再参与路由判定，所以各种时长组合（合法 / 非法 / 缺省）都
// 应该得到同一个结果：请求体逐字节不变。

// promptRouteTestPrompt 是一段真实长度的超长提示词（6169 字，与生产
// tasks 1045/1046 同量级）。
func promptRouteTestPrompt() string {
	return strings.Repeat("画面细节", 1542) + "，" // 4×1542 + 1 = 6169，> 5000
}

func TestLongPromptNeverReroutesWhateverTheDuration(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "显式 2 秒",
			body: fmt.Sprintf(`{"model":%q,"seconds":2,"prompt":%q}`, testSourceModel, promptRouteTestPrompt()),
		},
		{
			name: "显式 12 秒（旧非档位值）",
			body: fmt.Sprintf(`{"model":%q,"seconds":12,"prompt":%q}`, testSourceModel, promptRouteTestPrompt()),
		},
		{
			name: "显式 0 秒",
			body: fmt.Sprintf(`{"model":%q,"seconds":0,"prompt":%q}`, testSourceModel, promptRouteTestPrompt()),
		},
		{
			name: "不传时长",
			body: fmt.Sprintf(`{"model":%q,"prompt":%q}`, testSourceModel, promptRouteTestPrompt()),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableRoute(t, testSourceModel, testTargetModel, 20)
			rewritten, origin := runRoute(t, tc.body)

			if origin != "" {
				t.Fatalf("不该改派却记下了来源模型 %q", origin)
			}
			if rewritten != tc.body {
				t.Fatalf("超长提示词不应改写请求体:\n got %s\nwant %s", rewritten, tc.body)
			}
			var parsed map[string]interface{}
			if err := common.Unmarshal([]byte(rewritten), &parsed); err != nil {
				t.Fatalf("unmarshal rewritten body: %v", err)
			}
			if model, _ := parsed["model"].(string); model != testSourceModel {
				t.Fatalf("模型被改成了 %q，超长提示词不该触发改派", model)
			}
			if p, _ := parsed["prompt"].(string); p != promptRouteTestPrompt() {
				t.Fatal("提示词必须逐字不变（路由层不碰 prompt）")
			}
		})
	}
}

// 活动版是固定 30 秒档：非 30 秒会被路由层的 fail-loud 门控提前放行，
// 长提示词同样不该把它改派到任何链路。
func TestLongPromptDoesNotRerouteHuoDongWithWrongDuration(t *testing.T) {
	enableRoute(t, constant.Seedance25HuoDongModel, testTargetModel, 20)

	body := fmt.Sprintf(`{"model":%q,"seconds":10,"prompt":%q}`,
		constant.Seedance25HuoDongModel, promptRouteTestPrompt())
	rewritten, origin := runRoute(t, body)

	if origin != "" || rewritten != body {
		t.Fatalf("活动版非 30 秒的超长提示词不该改派（origin=%q）", origin)
	}
}
