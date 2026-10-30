package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactUpstreamIdentity_AliasModel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "组合写法整体替换，不留 上游:上游 碎片",
			in:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support the requested output combination"}}`,
			want: `status_code=400, {"error":{"message":"model 上游 does not support the requested output combination"}}`,
		},
		{
			name: "供应商 + 产品线连续命中时收敛成一个中性词",
			in:   `status_code=500, youzan wan3 asset upload failed: HTTP 422`,
			want: `status_code=500, 上游 asset upload failed: HTTP 422`,
		},
		{
			name: "独立的 ali 被替换",
			in:   `status_code=400, ali rejected the request`,
			want: `status_code=400, 上游 rejected the request`,
		},
		{
			name: "validate / invalid 里的 ali 不能被误伤",
			in:   `status_code=400, Failed to validate API key (invalid_request_error)`,
			want: `status_code=400, Failed to validate API key (invalid_request_error)`,
		},
		{
			name: "对外在售的 wan3.0 变体整体替换",
			in:   `status_code=400, model wan3.0-480p not available`,
			want: `status_code=400, model 上游 not available`,
		},
		{
			name: "渠道编号 LK888 被替换",
			in:   `status_code=500, LK888 image task failed: 上游没有返回预期的图像`,
			want: `status_code=500, 上游 image task failed: 上游没有返回预期的图像`,
		},
		{
			name: "腾讯云原文被替换",
			in:   `status_code=429, ... contact Tencent Cloud support to request a higher limit`,
			want: `status_code=429, ... contact 上游 support to request a higher limit`,
		},
		{
			name: "别名模型自身的名字必须原样保留",
			in:   `status_code=500, model seedance2.5-特惠 is busy`,
			want: `status_code=500, model seedance2.5-特惠 is busy`,
		},
		{
			name: "wan2 系列是另外在售的模型，不该被牵连",
			in:   `status_code=500, model wan2.7-r2v is busy`,
			want: `status_code=500, model wan2.7-r2v is busy`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RedactUpstreamIdentity(tc.in, "seedance2.5-特惠"))
		})
	}
}

func TestRedactUpstreamIdentity_历史旧名同样生效(t *testing.T) {
	assert.Equal(t,
		`status_code=400, model 上游 does not support`,
		RedactUpstreamIdentity(`status_code=400, model aliyun:wan-3.0 does not support`, "seedance2.5特惠版"))
}

// 两跳链路模型（wan3.0-720p-官网）必须脱敏，
// 原因比「藏货源」更硬：它的上游直链指向的是**还没做提升的 480P 原片**，
// 用户拿到就能绕过本站直接下载到没提升的成片 —— 商业逻辑被击穿。
func TestRedactUpstreamIdentity_两跳链路模型脱敏(t *testing.T) {
	assert.True(t, IsUpstreamAliasModel("wan3.0-720p-特惠"),
		"两跳链路模型必须在脱敏名单里，否则上游 480P 原片直链会漏给用户")
	// 旧名（2026-10-30 改名前）过渡期仍可调用 → 也必须在名单里，
	// 否则旧名调用方的 data 字段照样漏出上游签名直链。
	assert.True(t, IsUpstreamAliasModel("wan3.0-720p-官网"),
		"旧名过渡期仍可调用，脱敏名单不能漏它")

	raw := `成片地址 https://dashscope-a717.oss-accelerate.aliyuncs.com/1d/54/20261030/xxx.mp4 拉取失败：model wan3.0-video rejected`
	got := RedactUpstreamText(raw, "wan3.0-720p-官网")
	assert.NotContains(t, got, "dashscope", "上游存储域名不能漏")
	assert.NotContains(t, got, "aliyuncs.com", "上游存储域名不能漏")
	assert.NotContains(t, got, "https://", "上游地址整体摘掉")
	assert.Contains(t, got, "拉取失败", "失败原因本身要保留")

	// 上游模型名也要换掉 —— 它直接暴露了「我们卖的是 480P 原片」
	assert.Equal(t,
		`status_code=400, model 上游 not available`,
		RedactUpstreamIdentity(`status_code=400, model wan3.0-video not available`, "wan3.0-720p-官网"))
}

func TestRedactUpstreamIdentity_非别名模型原样返回(t *testing.T) {
	raw := `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`
	for _, modelName := range []string{"wan-3.0", "wan3.0-smart", "gpt-6-luna", "seedance-2.5", ""} {
		assert.Equal(t, raw, RedactUpstreamIdentity(raw, modelName), "模型 %s 的文案不该被改写", modelName)
	}
}

func TestRedactUpstreamIdentity_空内容(t *testing.T) {
	assert.Equal(t, "", RedactUpstreamIdentity("", "seedance2.5-特惠"))
}

// RedactUpstreamText 把「上游地址」和「上游身份字样」一起清掉。
//
// 为什么地址也要清：上游的成片直链（R2 域名 + hailuo-video 货源目录）本身就是
// "真实上游是谁"的证据。交付地址已改走本站 /content 代理，错误文案里再留一个
// 上游地址没有任何用途 —— 留着就等于把货源写在脸上。
func TestRedactUpstreamText_摘掉上游地址并脱敏身份(t *testing.T) {
	raw := `成片地址 https://d4c280ea2c37b44805c584296cb271a0.r2.cloudflarestorage.com/hailuo-video/vid_x.m4v 拉取失败：model aliyun:wan-3.0 rejected`
	got := RedactUpstreamText(raw, "seedance2.5-特惠")
	assert.NotContains(t, got, "r2.cloudflarestorage.com", "上游存储域名不能留在下游文案里")
	assert.NotContains(t, got, "hailuo-video", "上游货源目录不能留在下游文案里")
	assert.NotContains(t, got, "aliyun")
	assert.Contains(t, got, "上游")
	assert.Contains(t, got, "拉取失败", "失败原因本身要保留，只清上游信息")
}

// 非别名模型：地址照样摘、供应商词照样换成「上游」，但**模型名保留** ——
// 客户自己买的模型名出现在报错里是正常的，替换了反而看不懂。
// 口径见 RedactUpstreamText（2026-10-30 老板定：有渠道的直接说「上游」）。
func TestRedactUpstreamText_非别名模型也摘渠道痕迹但保留模型名(t *testing.T) {
	raw := `成片地址 https://cdn.example.com/wan3.0-720p/final.mp4 拉取失败：model aliyun:wan-3.0 rejected`
	got := RedactUpstreamText(raw, "wan-3.0")
	assert.NotContains(t, got, "https://", "地址对所有模型都摘")
	assert.NotContains(t, got, "aliyun", "供应商词对所有模型都换")
	assert.Contains(t, got, "上游")
	assert.Contains(t, got, "wan-3.0", "非别名模型：模型名保留，报错才看得懂")
	assert.Contains(t, got, "拉取失败", "失败原因本身要保留")

	assert.Equal(t, "", RedactUpstreamText("", "seedance2.5-特惠"))
}

// 地址后面的标点不能被一起吞掉 —— 否则文案破相。
//
// 整段替换会把 "错误 (https://x.com/a.mp4)" 变成 "错误 ([地址已隐藏]"：
// 括号不配对，客户看着像系统故障。标点要摘掉地址后原样留下。
func TestRedactUpstreamAddress_保留尾部标点(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"括号包裹", "错误 (https://x.com/a.mp4)", "错误 ([地址已隐藏])"},
		{"句末句号", "详见 https://x.com/a.mp4。", "详见 [地址已隐藏]。"},
		{"逗号结尾", "地址 https://x.com/a.mp4, 请重试", "地址 [地址已隐藏], 请重试"},
		{"问号结尾", "https://x.com/a.mp4?", "[地址已隐藏]?"},
		{"右括号", "(https://x.com/a)", "([地址已隐藏])"},
		{"无标点", "https://x.com/a", "[地址已隐藏]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, RedactUpstreamAddress(c.raw))
		})
	}
}

func TestFormatUserLogs_错误日志对普通用户隐藏渠道名与上游字样(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeError,
			ModelName: "seedance2.5-特惠",
			Content:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3","channel_type":64,"admin_info":{"use_channel":["24"]}}`,
		},
		{
			Type:      LogTypeError,
			ModelName: "wan-3.0",
			Content:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3"}`,
		},
		{
			Type:      LogTypeConsume,
			ModelName: "seedance2.5-特惠",
			Content:   `aliyun:wan-3.0`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3"}`,
		},
	}

	formatUserLogs(logs, 0)

	// 别名模型的错误日志：上游字样被替换、渠道名被清掉
	assert.NotContains(t, logs[0].Content, "aliyun")
	assert.NotContains(t, logs[0].Content, "wan-3.0")
	assert.NotContains(t, logs[0].Other, "channel_name")
	assert.NotContains(t, logs[0].Other, "admin_info")

	// 其它模型：供应商词同样换成「上游」（有渠道的直接说上游），
	// 但上游模型名保留 —— 那是客户自己买的模型族，替换了看不懂。
	assert.NotContains(t, logs[1].Content, "aliyun")
	assert.Contains(t, logs[1].Content, "wan-3.0")
	assert.NotContains(t, logs[1].Other, "channel_name")

	// 非错误日志的正文**也要脱敏**（2026-10-30 老板定：有渠道的一律说「上游」）。
	//
	// 为什么扩到所有类型：真实库里退款日志（type=6）的正文是运营手写的
	// 成本核算备注（"MegaAI 渠道 wan-3.0 30 秒 720p…"），消费日志（type=2）
	// 也出现过带渠道名的备注 —— 只脱敏错误日志会漏。
	assert.NotContains(t, logs[2].Content, "aliyun", "消费日志正文同样要换供应商词")
	assert.Contains(t, logs[2].Content, "上游")
}

// 别名模型的错误日志正文里不能出现上游地址：上游站点域名（R2 域名 / 渠道域名）
// 本身就是货源证据，错误文案里留着等于把货源写在脸上。
func TestFormatUserLogs_别名模型的错误日志不暴露上游地址(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeError,
			ModelName: "seedance2.5-特惠",
			Content:   `status_code=401, upstream https://newapi.megabyai.cc/v1/videos: invalid token`,
		},
	}

	formatUserLogs(logs, 0)

	assert.NotContains(t, logs[0].Content, "newapi.megabyai.cc", "上游站点域名不能给普通用户看")
	assert.NotContains(t, logs[0].Content, "https://", "上游地址整体摘掉")
	assert.Contains(t, logs[0].Content, "invalid token", "错误原因本身要保留")
}

func TestFormatUserLogs_别名模型的消费日志不暴露上游模型名(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeConsume,
			ModelName: "seedance2.5-特惠",
			Content:   "模型倍率 1",
			Other:     `{"is_model_mapped":true,"upstream_model_name":"wan-3.0","channel_id":24}`,
		},
		{
			Type:      LogTypeConsume,
			ModelName: "gpt-5.6-luna",
			Content:   "模型倍率 1",
			Other:     `{"is_model_mapped":true,"upstream_model_name":"gpt-6-luna"}`,
		},
	}

	formatUserLogs(logs, 0)

	// 别名模型：前端的 Model Mapping 面板会把 upstream_model_name 显示给用户，必须抹掉
	assert.NotContains(t, logs[0].Other, "upstream_model_name")
	assert.NotContains(t, logs[0].Other, "is_model_mapped")

	// 其它模型同样抹：upstream_model_name 是上游真实模型名，属于渠道痕迹。
	// 口径见 model.userLogDroppedOtherKeys（2026-10-30 老板定：有渠道的一律过滤）。
	assert.NotContains(t, logs[1].Other, "upstream_model_name")
	assert.NotContains(t, logs[1].Other, "is_model_mapped")

	// 渠道 ID 一律置 0，不给客户看走了哪条渠道。
	assert.Equal(t, 0, logs[0].ChannelId)
}

// 任务类日志（线上实际存在的 type=6：只带 reason / task_id，正文为空）也要抹。
// 2026-10-29 抽查生产库发现这类行确实带着 upstream_model_name=wan-3.0 ——
// 它的抹除逻辑与日志类型无关（IsUpstreamAliasModel 分支不分 type），这里钉死，
// 免得以后有人把该分支挪进 `if Type == LogTypeError` 里，静默放走任务日志。
func TestFormatUserLogs_别名模型的任务日志不暴露上游模型名(t *testing.T) {
	logs := []*Log{
		{
			Type:      6,
			ModelName: "seedance2.5-特惠",
			Content:   "",
			Other:     `{"group_ratio":1,"is_model_mapped":true,"model_price":0.41,"reason":"第 1 个 image 素材：素材无法下载，HTTP 404","task_id":"task_x","upstream_model_name":"wan-3.0"}`,
		},
	}

	formatUserLogs(logs, 0)

	assert.NotContains(t, logs[0].Other, "upstream_model_name")
	assert.NotContains(t, logs[0].Other, "is_model_mapped")
	// other["reason"] 要**保留**（客户得知道为什么失败），但内容要脱敏。
	//
	// 真实库里的 reason 是 "生成结果疑似包含真人面部，未通过内容安全审核…" ——
	// 客户被扣了钱又被退款，一刀切删掉等于让他对着空记录猜。
	assert.Contains(t, logs[0].Other, "素材无法下载", "失败原因本身要保留，客户要看得懂")
}

// other["reason"] 保留但脱敏：失败原因要给客户看懂，上游痕迹要洗掉。
//
// 真实风险：有些 reason 是上游原文转述（"youzan wan3 rejected: 积分不足"），
// 直接保留会把货源写进客户的退款记录里。
func TestFormatUserLogs_reason保留但脱敏(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeRefund,
			ModelName: "seedance2.5-特惠",
			Other:     `{"reason":"youzan wan3 rejected the request: 积分不足 (https://dashscope-result.oss-cn-beijing.aliyuncs.com/x.mp4)","task_id":"task_x"}`,
		},
	}

	formatUserLogs(logs, 0)

	assert.Contains(t, logs[0].Other, "reason", "reason 字段要保留")
	assert.Contains(t, logs[0].Other, "积分不足", "失败原因本身要保留")
	assert.NotContains(t, logs[0].Other, "youzan", "供应商词要换成「上游」")
	assert.NotContains(t, logs[0].Other, "aliyuncs.com", "上游地址要摘掉")
	assert.Contains(t, logs[0].Other, "上游")
}

// 条件路由改派过的任务：日志的 model_name 已经是用户请求的源模型，
// 实际执行的目标模型放在 other["route_target_model"] —— 只给管理员看。
//
// 2026-10-30 老板发现客户日志里出现 wan3.0-720p-特惠（他调的是 seedance2.5-特惠），
// 等于把「被改派」这件事直接告诉客户。修法：写日志时用源模型名 + 目标名进 other，
// 客户侧把 other 里的目标名抹掉。
func TestFormatUserLogs_不暴露条件路由的目标模型(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeConsume,
			ModelName: "seedance2.5-特惠",
			Content:   "操作 generate, 计算参数：seconds: 30.00",
			Other:     `{"group_ratio":1,"is_task":true,"model_price":0.41,"request_path":"/v1/videos","route_target_model":"wan3.0-720p-特惠"}`,
		},
		{
			Type:      LogTypeError,
			ModelName: "seedance2.5-特惠",
			Content:   `status_code=400, {"detail":"Failed to download https://api.example.com/a.png"}`,
			Other:     `{"channel_id":12,"channel_name":"DashScope-Wan3","route_target_model":"wan3.0-720p-特惠","status_code":400}`,
		},
	}

	formatUserLogs(logs, 0)

	for i := range logs {
		assert.NotContains(t, logs[i].Other, "route_target_model",
			"改派目标模型是管理员信息，不能给普通用户看")
		assert.Equal(t, "seedance2.5-特惠", logs[i].ModelName,
			"用户日志里的模型名必须是他请求的源模型")
	}
	assert.NotContains(t, logs[1].Other, "channel_name")
	assert.NotContains(t, logs[1].Content, "api.example.com", "上游地址仍然要摘掉")
}

// 整个日志条目序列化后，一个渠道痕迹都不能剩。
//
// 按「序列化后的字符串」断言：客户是把整条日志 JSON 摊在浏览器里看的，
// 任何一个字段漏出去都算泄露 —— 逐字段断言只能保证你想到的那些字段。
// 口径见 userLogDroppedOtherKeys（2026-10-30 老板定：有渠道的一律过滤）。
func TestFormatUserLogs序列化后不含任何渠道痕迹(t *testing.T) {
	logs := []*Log{
		{
			Type:              LogTypeError,
			ModelName:         "seedance2.5-特惠",
			ChannelId:         12,
			ChannelName:       "DashScope-Wan3",
			UpstreamRequestId: "up-task-abc123",
			Content:           `status_code=400, {"detail":"Failed to download https://api.example.com/a.png"}`,
			Other:             `{"channel_id":12,"channel_name":"DashScope-Wan3","channel_type":12,"upstream_model_name":"wan3.0-video","is_model_mapped":true,"route_target_model":"wan3.0-720p-特惠","status_code":400,"request_path":"/v1/videos"}`,
		},
	}

	formatUserLogs(logs, 0)
	raw, err := common.Marshal(logs[0])
	require.NoError(t, err)
	body := string(raw)

	for _, banned := range []string{
		"\"channel\":12", // 渠道 ID（json tag 就叫 channel）
		"DashScope",
		"aliyun",
		"wan3.0-video",
		"up-task-abc123",
		"route_target_model",
		"api.example.com",
	} {
		assert.NotContains(t, body, banned, "对外日志不能出现渠道痕迹")
	}
	assert.Contains(t, body, "seedance2.5-特惠", "模型名用用户请求的源模型")
	assert.Contains(t, body, "status_code", "错误码要留，客户靠它判断原因")
}
