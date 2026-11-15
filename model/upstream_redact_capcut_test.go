package model

// ════════════════════════════════════════════════════════════════════════════
// CapCut（剪映国际版）渠道名脱敏 —— 2026-11-15
// ════════════════════════════════════════════════════════════════════════════
//
// 事实：生产客户单 seedance2.5-活动 被条件路由改派到 cp-seedance-2.5flash
// （CapCut 链路）后，因 Worker 侧参考音频开关未开而被拒单退款，
// 退款日志（type=6）的 other["reason"] 原文是：
//
//	"capcut 远端暂不支持参考音频，任务未提交（请去掉该素材，或改用万相）"
//
// 这条 reason 会渲染到客户可见的日志页（前端「Async task refund」行）。
// 「capcut」直接点名了改派后的平台，而客户公开能买到的只是 cp-seedance-*
// 档位名 —— 平台名属于渠道证据，与 wan3/youzan/aliyun 同口径，必须遮成「上游」。
//
// 本文件把「CapCut 平台名不外泄」变成可执行断言。

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capcutRefundReason 是生产实测的原文（logs.id=14844 等 6 条 type=6）。
const capcutRefundReason = "capcut 远端暂不支持参考音频，任务未提交（请去掉该素材，或改用万相）"

// 守卫 1：供应商词表必须含 CapCut 的中英文两个写法。
//
// 为什么要两个：Worker 的平台 id 是英文 "capcut"，但中文文案里会出现「剪映」
// （constant/capcut_family.go 与 manwu adaptor 的注释都这么叫）。
// 只遮英文会留下中文这一半 —— 客户照样认得出这是哪个平台。
func TestRedactUpstreamVendor_遮掉CapCut平台名(t *testing.T) {
	for _, literal := range []string{"capcut", "剪映"} {
		assert.False(t, strings.Contains(RedactUpstreamVendor("平台 "+literal+" 报错"), literal),
			"供应商词表漏了 %q —— 上游报错里点名它等于直接报出货源", literal)
	}
}

// 守卫 2：生产原文整句脱敏后不得残留平台名。
//
// 这是最贴近生产的断言：直接拿生产退款文案跑一遍，
// 断言「capcut」与「剪映」都不在结果里，且替换成了中性词「上游」。
func TestRedactUpstreamText_生产退款文案不残留CapCut(t *testing.T) {
	redacted := RedactUpstreamText(capcutRefundReason, "seedance2.5-活动")

	assert.NotContains(t, redacted, "capcut",
		"生产退款文案里的 capcut 必须被遮 —— 客户一看就知道被改派到了剪映国际版")
	assert.NotContains(t, redacted, "剪映",
		"中文写法「剪映」同样要遮")
	assert.Contains(t, redacted, "上游",
		"平台名必须换成中性词「上游」")
	// 客户仍需知道发生了什么 —— 只遮身份词，不该把整句话删空。
	assert.Contains(t, redacted, "参考音频",
		"失败原因本身要保留：客户被退款了，得知道为什么")
}

// 守卫 3：全局生效 —— 跟客户买哪个模型无关。
//
// 口径（2026-10-30 老板定）：有渠道的直接说「上游」。上游报错里点名货源
// 跟客户调的是哪个模型无关 —— 客户调 gpt-4o 时上游报 capcut 的错，
// 照样不该让他看见。
func TestRedactUpstreamText_CapCut脱敏对所有模型生效(t *testing.T) {
	for _, modelName := range []string{"gpt-4o", "seedance2.5-活动", "cp-seedance-2.5flash", "h3-image"} {
		redacted := RedactUpstreamText(capcutRefundReason, modelName)
		assert.NotContains(t, redacted, "capcut",
			"模型 %q 下也必须遮 capcut —— 供应商词是全局口径", modelName)
	}
}

// 守卫 4（反证）：不得误伤公开在售的模型名。
//
// cp-seedance-* 是**客户可以直接调**的公开档位名，不是上游痕迹。
// 如果有人把 "cp-seedance" 加进供应商词表，客户的报错里
// 「model cp-seedance-2.5flash is busy」会被削成「model 上游 is busy」——
// 客户看不到自己买的模型名，报错失去意义。
//
// ���里锁住这个边界：公开名必须原样保留。
func TestRedactUpstreamText_公开的cp模型名不被误伤(t *testing.T) {
	for _, publicName := range []string{
		"cp-seedance-2.5",
		"cp-seedance-2.5flash",
		"cp-seedance-2.0",
		"cp-seedance-2.0fast",
		"cp-seedance-2.0mini",
	} {
		redacted := RedactUpstreamText("model "+publicName+" is busy", publicName)
		assert.Contains(t, redacted, publicName,
			"公开在售的 %q 必须原样保留 —— 它是客户自己买的模型名，不是渠道痕迹", publicName)
	}
}

// 守卫 5：日志页出口 —— other["reason"] 与 content 都必须脱敏。
//
// 这是客户实际看到的那一行（前端 usage-logs 的「Async task refund」）。
// 只测纯函数不够，必须验 formatUserLogs 这个真实出口。
func TestFormatUserLogs_退款日志不暴露CapCut(t *testing.T) {
	logs := []*Log{
		{
			Type:      6, // 退款
			ModelName: "seedance2.5-活动",
			Content:   capcutRefundReason,
			Other: `{"reason":"` + capcutRefundReason + `","route_target_model":"cp-seedance-2.5flash","task_id":"task_x1"}`,
		},
	}
	formatUserLogs(logs, 0)

	require.Len(t, logs, 1)
	assert.NotContains(t, logs[0].Content, "capcut",
		"日志正文里的 capcut 必须被遮")
	assert.NotContains(t, logs[0].Other, "capcut",
		"other.reason 里的 capcut 必须被遮 —— 前端退款行渲染的就是它")
	assert.NotContains(t, logs[0].Other, "route_target_model",
		"route_target_model 是改派痕迹，必须整个删掉")
	assert.Contains(t, logs[0].Other, "参考音频",
		"失败原因要保留，客户得知道为什么被退款")
}
