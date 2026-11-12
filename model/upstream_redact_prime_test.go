package model

// ════════════════════════════════════════════════════════════════════════════
// 上游档位名 prime 的下游过滤契约（2026-11-12）
// ════════════════════════════════════════════════════════════════════════════
//
// 生产真实样本（logs.id=14649，2026-11-12 17:12:17，渠道 17，用户 36，
// model_name=seedance2.5-特惠）：
//
//	status_code=400, {"ok":false,"error":{"code":"PRIME_DURATION_TOO_SHORT",
//	"message":"由于服务器保护机制，暂时不受理低于 10 秒以下的prime任务"}}
//
// 这条 400 的正文会同时出现在三个对客出口：提交响应（respondTaskError）、
// 用户错误日志（formatUserLogs）、任务失败原因（relay.TaskModel2Dto），
// 三者都走 RedactUpstreamText，所以只要一处漏，客户就能看到 prime。
//
// 为什么现有词表拦不住：upstreamModelLiterals 里只有完整模型名
// "wan3.0-video-prime"，而上游报错用的是**裸词** prime 和**错误码前缀**
// PRIME_ —— 两种写法都不构成完整模型名，字面量一个都命中不了。
//
// ⚠️ 范围必须是「仅别名模型」：生产 abilities 表里 wan3.0-video-prime
// 本身也挂在渠道 17 对外可见。客户直接买它时，报错里出现 prime 是**他自己
// 买的模型名**，遮掉反而看不懂。这也是为什么不能把 prime 塞进
// upstreamVendorLiterals（那份表对所有模型全局生效）。
//
// ⚠️ 顺序敏感：完整模型名必须先被整体替换成「上游」，再过滤裸 prime。
// 反过来的话 "wan3.0-video-prime" 会被削成 "wan3.0-video-"，
// 留下 "上游-" 这种碎片。

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// primeDurationTooShortRaw 就是生产库 logs.id=14649 的 content 原值，一个字没改。
// 用它当回归样本：以后上游换文案、换错误码，这份仍然是必须拦住的最小集合。
const primeDurationTooShortRaw = `status_code=400, {"ok":false,"error":{"code":"PRIME_DURATION_TOO_SHORT","message":"由于服务器保护机制，暂时不受理低于 10 秒以下的prime任务"}}`

// TestRedactUpstreamText_生产样本_prime档位名不带给客户 守的是这次的真实故障。
//
// 断言「整段文案小写后不含 prime」而不是逐个字段：客户拿到的是一整段 JSON
// 字符串，code 字段里的 PRIME_ 和 message 里的 prime 一样泄露货源。
func TestRedactUpstreamText_生产样本_prime档位名不带给客户(t *testing.T) {
	got := RedactUpstreamText(primeDurationTooShortRaw, "seedance2.5-特惠")

	assert.NotContains(t, strings.ToLower(got), "prime",
		"上游档位名 prime 不能出现在任何对客文案里（含错误码前缀）")
	// 失败原因本身要留着：客户得知道是「时长不够」，不是让他对着空串猜。
	assert.Contains(t, got, "由于服务器保护机制", "错误原因要保留")
	assert.Contains(t, got, "低于 10 秒以下", "时长门槛要保留，客户靠它改请求")
	assert.Contains(t, got, "status_code=400", "状态码要保留")
	assert.Contains(t, got, "DURATION_TOO_SHORT", "错误码主体要保留，仍是机器可读的")
	// 精确锁定最终文案：只摘档位名，错误码主体、时长门槛、状态码一个不少。
	// 只写 NotContains 的话，把实现改成「一律换成上游」也能通过，
	// 但客户会收到 "上游_DURATION_TOO_SHORT" 这种坏掉的错误码。
	assert.Equal(t,
		`status_code=400, {"ok":false,"error":{"code":"DURATION_TOO_SHORT","message":"由于服务器保护机制，暂时不受理低于 10 秒以下的任务"}}`,
		got)
}

// 上游同一个档位名会以多种写法出现。这里钉死每一种都必须被洗掉，
// 免得以后有人只按「PRIME_」或只按「prime任务」打补丁。
func TestRedactUpstreamText_prime的多种写法都要过滤(t *testing.T) {
	cases := []struct {
		name string
		in   string
		keep string // 洗掉 prime 后必须还在的内容，防一刀切把整句删了
	}{
		{
			name: "中文裸词紧接中文",
			in:   `{"message":"暂时不受理低于 10 秒以下的prime任务"}`,
			keep: "低于 10 秒以下",
		},
		{
			name: "全大写错误码前缀",
			in:   `{"code":"PRIME_DURATION_TOO_SHORT"}`,
			keep: "DURATION_TOO_SHORT",
		},
		{
			name: "小写错误码前缀",
			in:   `{"code":"prime_duration_too_short"}`,
			keep: "duration_too_short",
		},
		{
			name: "大小写混合的裸词",
			in:   `{"message":"Prime task rejected"}`,
			keep: "task rejected",
		},
		{
			name: "空格分隔的英文裸词",
			in:   `{"message":"prime tier is busy"}`,
			keep: "tier is busy",
		},
		{
			name: "完整上游模型名",
			in:   `{"message":"model wan3.0-video-prime not available"}`,
			keep: "not available",
		},
		{
			name: "完整模型名加错误码同时出现",
			in:   `{"code":"PRIME_QUOTA_EXCEEDED","message":"wan3.0-video-prime quota exhausted"}`,
			keep: "QUOTA_EXCEEDED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RedactUpstreamText(tc.in, "seedance2.5-特惠")
			assert.NotContains(t, strings.ToLower(got), "prime", "对客文案里不能残留 prime")
			assert.Contains(t, got, tc.keep, "只洗档位名，错误原因/错误码主体要留着")
		})
	}
}

// 完整模型名必须被**整体**替换，不能先削掉尾缀 prime 留下 "上游-" 碎片。
// 这是对实现顺序的约束：先整体替换，再过滤裸词。
func TestRedactUpstreamText_完整prime模型名不留碎片(t *testing.T) {
	got := RedactUpstreamText(`model wan3.0-video-prime rejected`, "seedance2.5-特惠")

	assert.NotContains(t, strings.ToLower(got), "prime")
	assert.NotContains(t, got, "上游-", "整体替换不能留下半截模型名")
	assert.Contains(t, got, "上游")
	assert.Contains(t, got, "rejected")
}

// 别名模型的兄弟档（活动版）也必须受同一套保护：它和特惠版同货源，
// 上游同样会返回 prime 相关的报错。
func TestRedactUpstreamText_活动版同样过滤prime(t *testing.T) {
	for _, name := range []string{"seedance2.5-活动", "seedance2.5特惠版"} {
		got := RedactUpstreamText(primeDurationTooShortRaw, name)
		assert.NotContains(t, strings.ToLower(got), "prime", "别名 %s 不能漏 prime", name)
	}
}

// 非别名模型：prime 是客户自己买的模型名，必须原样留着。
// 这条守的是「别把过滤做成全局」—— 全局替换会让直接调
// wan3.0-video-prime 的客户看到被改得莫名其妙的报错。
func TestRedactUpstreamText_非别名模型的prime原样保留(t *testing.T) {
	for _, name := range []string{"wan3.0-video-prime", "wan-3.0", "minimax_h3_omni", "gpt-6-luna", ""} {
		got := RedactUpstreamText(primeDurationTooShortRaw, name)
		assert.Contains(t, got, "PRIME_DURATION_TOO_SHORT",
			"模型 %q 不是别名，不该动上游错误码", name)
		assert.Contains(t, got, "prime任务", "模型 %q 不是别名，不该动上游文案", name)
	}
}

// RedactUpstreamIdentity 是另一个公开出口（任务 DTO 链路也用它），
// 必须和 RedactUpstreamText 保持一致，不能一个洗了一个没洗。
func TestRedactUpstreamIdentity_同样过滤prime(t *testing.T) {
	got := RedactUpstreamIdentity(primeDurationTooShortRaw, "seedance2.5-特惠")
	assert.NotContains(t, strings.ToLower(got), "prime")
	assert.Contains(t, got, "低于 10 秒以下")

	// 非别名模型仍然原样
	assert.Equal(t, primeDurationTooShortRaw,
		RedactUpstreamIdentity(primeDurationTooShortRaw, "wan3.0-video-prime"))
}

// 出口 2：错误日志。客户是把整条日志 JSON 摊在浏览器里看的，
// 所以按「序列化后的整条」断言，逐字段断言会漏掉 Content 之外的字段。
func TestFormatUserLogs_prime错误日志整条不含prime(t *testing.T) {
	logs := []*Log{
		{
			Type:        LogTypeError,
			ModelName:   "seedance2.5-特惠",
			ChannelId:   17,
			ChannelName: "有赞 Wan3",
			Content:     primeDurationTooShortRaw,
			Other:       `{"status_code":400,"request_path":"/v1/videos"}`,
		},
	}

	formatUserLogs(logs, 0)

	raw, err := common.Marshal(logs[0])
	require.NoError(t, err)
	body := string(raw)

	assert.NotContains(t, strings.ToLower(body), "prime", "客户侧整条日志不能出现 prime")
	assert.Contains(t, body, "低于 10 秒以下", "失败原因要留着给客户看")
	assert.Contains(t, body, "status_code", "状态码要留着")
}

// 空内容与原样兜底：脱敏不能把空串写成别的，也不能误伤普通英文词。
func TestRedactUpstreamText_prime过滤不误伤普通词(t *testing.T) {
	assert.Equal(t, "", RedactUpstreamText("", "seedance2.5-特惠"))

	for _, ordinary := range []string{
		"primary key", "primer", "primeval forest", "PRIMARY", "validate", "invalid",
	} {
		got := RedactUpstreamText(`{"message":"`+ordinary+`"}`, "seedance2.5-特惠")
		assert.Contains(t, got, ordinary,
			"%q 是普通英文词，不是上游档位名，不能被过滤", ordinary)
	}
}
