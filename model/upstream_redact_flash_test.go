package model

// ════════════════════════════════════════════════════════════════════════════
// 上游档位名 flash 的下游过滤契约（2026-11-22）
// ════════════════════════════════════════════════════════════════════════════
//
// 生产真实样本（new 机 logs.id=15168/15169，2026-11-22，渠道 17，
// model_name=seedance2.5-特惠，客户带参考视频）：
//
//	status_code=400, {"error":{"code":"","message":"HooksAI 的 Seedance 2.5 Flash
//	不支持视频参考，请移除参考视频后提交 (request id: 202611221153444657…)"}}
//
// 两个泄漏点，一次修完：
//  1. **HooksAI** —— 分销商品牌词，属渠道证据（同 youzan / aliyun 口径）→「上游」；
//  2. **Flash**  —— 上游档位名。客户买的是 seedance2.5-特惠（对外名没有 flash），
//     文案却点名 Flash，等于把「你调 A、实际跑 Flash」告诉客户。
//
// 这三条对客出口都走 RedactUpstreamText：提交响应（respondTaskError）、
// 用户错误日志（formatUserLogs）、任务失败原因（relay.TaskModel2Dto）。
//
// ⚠️ 范围判据不是「别名模型」，而是「客户买的模型名里有没有 flash」：
//   - 买 cp-seedance-2.5flash / el-gemini-1.2flash / Seedream 5.0 Flash 的客户
//     → 原样保留 flash（那是他自己买的东西的名字，删掉反而看不懂）；
//   - 买 seedance2.5-特惠 / seedance-2-5 / el-seedance-2.5 的客户 → 摘掉。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// flashLeakRaw 是生产库 logs.content 的真实原文（只截短了 request id）。
const flashLeakRaw = `status_code=400, {"error":{"code":"","message":"HooksAI 的 Seedance 2.5 Flash 不支持视频参考，请移除参考视频后提交 (request id: 202611221153444657002228268d9d6Bey6efqJ)","type":"new_api_error"}}`

// TestRedactUpstreamText_生产样本_flash档位名与分销商不带给客户 守的是这次的真实故障。
func TestRedactUpstreamText_生产样本_flash档位名与分销商不带给客户(t *testing.T) {
	got := RedactUpstreamText(flashLeakRaw, "seedance2.5-特惠")

	assert.NotContains(t, strings.ToLower(got), "flash",
		"客户买的不是 flash 档，文案里不能出现 flash（等于暴露真实执行的档位）")
	assert.NotContains(t, strings.ToLower(got), "hooksai",
		"分销商品牌词属渠道证据，必须换成「上游」")

	// 可操作信息一个字都不能少：客户得知道「带参考视频不行、要移除后重提」。
	assert.Contains(t, got, "不支持视频参考", "拒绝原因要保留")
	assert.Contains(t, got, "请移除参考视频后提交", "客户靠这句话改请求")
	assert.Contains(t, got, "status_code=400", "状态码要保留")
	assert.Contains(t, got, "request id", "上游回执号要保留（客服排查用）")

	// 精确文案：只摘品牌词与档位名，句子结构不动。
	assert.Equal(t,
		`status_code=400, {"error":{"code":"","message":"上游 的 Seedance 2.5 不支持视频参考，请移除参考视频后提交 (request id: 202611221153444657002228268d9d6Bey6efqJ)","type":"new_api_error"}}`,
		got)
}

// TestRedactUpstreamText_客户自己买的就是flash档_flash字样必须留着
// 反向用例：cp-seedance-2.5flash / el-gemini-1.2flash / Seedream 5.0 Flash 的
// 客户看到自己买的名字被改掉，会把报错当成「系统给我换了模型」。
func TestRedactUpstreamText_客户自己买的就是flash档_flash字样必须留着(t *testing.T) {
	cases := []struct {
		model string
		text  string
		want  string
	}{
		{
			model: "cp-seedance-2.5flash",
			text:  "seedance_2.5_flash 参考图数量超限",
			want:  "seedance_2.5_flash 参考图数量超限",
		},
		{
			model: "el-gemini-1.2flash",
			text:  "gemini-omni-1.2-flash 不支持参考音频",
			want:  "gemini-omni-1.2-flash 不支持参考音频",
		},
		{
			model: "Seedream 5.0 Flash",
			text:  "Seedream 5.0 Flash 不支持该比例",
			want:  "Seedream 5.0 Flash 不支持该比例",
		},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, RedactUpstreamText(tc.text, tc.model), "model=%s", tc.model)
	}
}

// TestRedactUpstreamFlashWord_写法矩阵 锁定各种 flash 写法都收敛到 2.5。
func TestRedactUpstreamFlashWord_写法矩阵(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Seedance 2.5 Flash 不支持视频参考", "Seedance 2.5 不支持视频参考"},
		{"seedance-2.5flash 不支持视频参考", "seedance-2.5 不支持视频参考"},
		{"seedance_2.5_flash 不支持视频参考", "seedance_2.5 不支持视频参考"},
		{"2.5flash 不支持视频参考", "2.5 不支持视频参考"},
		{"Seedance Flash 不支持视频参考", "Seedance 不支持视频参考"},
		// 不该被误伤：fl 开头的其它词没有后置词边界
		{"flashy 文案不算档位名", "flashy 文案不算档位名"},
		{"flashback 也不算", "flashback 也不算"},
		// 中文紧贴也要能摘
		{"Seedance 2.5 Flash不支持视频参考", "Seedance 2.5不支持视频参考"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, redactUpstreamFlashWord(tc.in, "seedance2.5-特惠"), "in=%q", tc.in)
	}
}

// TestRedactUpstreamText_非别名模型也摘flash 客户买 seedance-2-5 被改派到 flash 档时同样要摘。
func TestRedactUpstreamText_非别名模型也摘flash(t *testing.T) {
	got := RedactUpstreamText("上游模型 Seedance 2.5 Flash 不支持该比例", "seedance-2-5")
	assert.NotContains(t, strings.ToLower(got), "flash")
	assert.Contains(t, got, "Seedance 2.5")
}

// TestRedactUpstreamIdentity_也走flash过滤 另一条入口（RedactUpstreamIdentity）
// 不能漏：它在部分链路单独被调用。
func TestRedactUpstreamIdentity_也走flash过滤(t *testing.T) {
	got := RedactUpstreamIdentity(flashLeakRaw, "seedance2.5-特惠")
	assert.NotContains(t, strings.ToLower(got), "flash")
	assert.NotContains(t, strings.ToLower(got), "hooksai")
}
