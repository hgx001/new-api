package relay

// ════════════════════════════════════════════════════════════════════════════
// seedance2.5-活动 的「渠道隐藏」契约 —— 改代码前请先读完这段
// ════════════════════════════════════════════════════════════════════════════
//
// 事实：seedance2.5-活动 与母本 seedance2.5-特惠 是**同一个商品的两种售价形态**，
// 上游同样是渠道 17「有赞 Wan3」(type 63) 的 wan3.0-video-prime。
//
// 所以它必须享有与母本**完全相同**的渠道隐藏保护 ——
// 这是 C4「保留母本全部属性」在脱敏层的直接推论。
//
// ⚠️ 这里最容易被漏掉的原因：新模型的脱敏代码路径没人跑过。
// 名单里没有它 → IsUpstreamAliasModel 返回 false →
//   · RedactUpstreamIdentity 直接 return → 报错文案里的上游模型名原样漏出；
//   · redactAliasTaskUpstream 直接 return → 上游回执（任务号 + 成本 + 原片直链）
//     原样进对外 DTO。
//
// 而活动版是**促销档**，恰恰是最多人下单、最需要保护货源的一个。
//
// 本文件把「活动版与母本享有同等保护」变成可执行断言。

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance25HuoDong 是被保护的别名模型（促销档）。
const seedance25HuoDong = constant.Seedance25HuoDongModel

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：别名白名单
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25HuoDong_必须在别名白名单里 守的是**所有脱敏的总开关**。
//
// 与母本的守卫（TestSeedance25TeHui_必须在别名白名单里）成对 ——
// 两个模型是同一个商品，保护级别必须一致。
func TestSeedance25HuoDong_必须在别名白名单里(t *testing.T) {
	assert.True(t, model.IsUpstreamAliasModel(seedance25HuoDong),
		"模型 %q 必须留在 upstreamAliasModels 里 —— 删掉它，"+
			"redactAliasTaskUpstream 会直接 return，上游模型名与上游回执全部漏给下游", seedance25HuoDong)

	// 母本也必须仍在（回归守卫：加新模型时不能把旧的弄掉）。
	assert.True(t, model.IsUpstreamAliasModel(seedance25TeHui),
		"母本 %q 必须仍在别名白名单里", seedance25TeHui)

	// 反证：非别名模型不该被误判成别名。
	for _, name := range []string{"gpt-4o", "wan3.0-video", "seedance-2-5", "seedance2.5-活动版"} {
		assert.False(t, model.IsUpstreamAliasModel(name),
			"模型 %q 不是别名，不该进白名单 —— 会把客户自己买的模型名也打散", name)
	}
}

// 活动版的报错文案必须与母本享受**同等**的脱敏强度。
//
// 这是「能力一致」在脱敏层的可执行定义：同样的上游错误文案，
// 母本脱敏后不该剩上游痕迹，活动版也不该剩。
func TestSeedance25HuoDong_报错脱敏强度与母本一致(t *testing.T) {
	raw := "youzan wan3 rejected: model " + upstreamRealModel +
		" not available, see https://dashscope-result.oss-cn-beijing.aliyuncs.com/raw.mp4"

	tehui := model.RedactUpstreamIdentity(raw, seedance25TeHui)
	huodong := model.RedactUpstreamIdentity(raw, seedance25HuoDong)

	for _, banned := range []string{upstreamRealModel, "wan3", "youzan", "aliyuncs.com"} {
		assert.NotContains(t, tehui, banned,
			"母本的脱敏不该留下 %q —— 否则测试本身就失去意义", banned)
		assert.NotContains(t, huodong, banned,
			"活动版必须享受与母本同等的脱敏：不该留下 %q", banned)
	}

	assert.Contains(t, huodong, "上游",
		"活动版的报错里上游字样必须被换成中性词「上游」")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：任务 DTO 全字段审计（最坏情况）
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25HuoDong_任务DTO不暴露渠道 是**最坏情况**审计：
// 把库里能填的渠道字段全部填满，然后断言对外 DTO 一个都不剩。
//
// 与母本的 TestSeedance25TeHui_任务DTO不暴露渠道 是同一套断言，
// 只是换模型名 —— 两个模型是同一个商品，暴露面必须一样。
func TestSeedance25HuoDong_任务DTO不暴露渠道(t *testing.T) {
	task := &model.Task{
		ID:        4243,
		TaskID:    "task_seedance_huodong_probe",
		ChannelId: upstreamChannelID,
		Platform:  constant.TaskPlatform(upstreamChannelType),
		Status:    model.TaskStatusFailure,
		Properties: model.Properties{
			UpstreamModelName: upstreamRealModel,
			OriginModelName:   seedance25HuoDong,
		},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "wz_83d06520-ed98-4621-9e40",
			ResultURL:      upstreamVideoURL,
		},
		FailReason: "youzan wan3 rejected the request: model " + upstreamRealModel + " not available",
		Data:       json.RawMessage(upstreamRawReceiptJS),
	}

	out := TaskModel2Dto(task)
	require.NotNil(t, out)

	assert.Equal(t, 0, out.ChannelId, "channel_id 必须置 0")
	assert.Equal(t, "上游", out.Platform, "platform 必须换成「上游」")
	assert.Nil(t, out.Data, "data 是上游完整回执，必须清空")

	props, ok := out.Properties.(model.Properties)
	require.True(t, ok, "properties 结构不能变")
	assert.Equal(t, seedance25HuoDong, props.UpstreamModelName,
		"upstream_model_name 必须换回对外别名")

	raw, err := common.Marshal(out)
	require.NoError(t, err)
	body := string(raw)

	for _, banned := range []struct {
		needle string
		why    string
	}{
		{`"channel_id":17`, "渠道编号不能给下游"},
		{`"platform":"63"`, "渠道类型编号一查就知道是「有赞 Wan3」"},
		{upstreamChannelName, "渠道名直接点名货源"},
		{upstreamRealModel, "上游真实模型名（wan3.0-video-prime）"},
		{"wan3", "wan3 字样本身"},
		{"youzan", "供应商名"},
		{"billing_amount", "上游成本：给了就能算出我们赚多少"},
		{"sourceUrl", "上游原片直链"},
		{"aliyuncs.com", "上游存储域名"},
		{"r2.cloudflarestorage.com", "上游存储域名"},
		{"hailuo-video", "上游货源目录"},
		{"wz_83d06520", "上游任务号：能拿去上游后台查"},
	} {
		assert.NotContains(t, body, banned.needle,
			"对外响应体不能出现渠道痕迹 —— %s", banned.why)
	}

	// 该留的必须留：模型名用客户请求的源模型（活动版自己）。
	assert.Contains(t, body, seedance25HuoDong, "模型名用客户请求的源模型")
	assert.Contains(t, body, "task_seedance_huodong_probe", "任务号要留，客户靠它查任务")
	assert.Contains(t, body, "上游", "对客户统一说「上游」")
}

// TestSeedance25HuoDong_条件路由改派后仍隐藏渠道 守的是最隐蔽的一条路径。
//
// 活动版同样会被条件路由改派（30 秒 → seedance-2-5、9:16 → wan3.0-720p-特惠）。
// 改派后 task 上记的是**目标**模型与**目标**渠道 —— 脱敏必须按
// **用户请求的源模型**判断，否则目标模型不在别名白名单里 → 直接 return →
// data / fail_reason / result_url / channel_id 全部原样漏出。
func TestSeedance25HuoDong_条件路由改派后仍隐藏渠道(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_huodong_routed",
		ChannelId: 12,
		Platform:  constant.TaskPlatform("12"),
		Status:    model.TaskStatusFailure,
		Properties: model.Properties{
			UpstreamModelName: "wan3.0-video",
			OriginModelName:   "wan3.0-720p-特惠",
		},
		PrivateData: model.TaskPrivateData{
			RouteOriginModel: seedance25HuoDong,
			ResultURL:        upstreamVideoURL,
		},
		FailReason: "wan3.0-720p-特惠 rejected: wan3 upstream busy",
		Data:       json.RawMessage(upstreamRawReceiptJS),
	}

	out := TaskModel2Dto(task)
	raw, err := common.Marshal(out)
	require.NoError(t, err)
	body := string(raw)

	assert.Contains(t, body, seedance25HuoDong,
		"对外 model 必须是用户请求的源模型")
	assert.NotContains(t, body, "wan3.0-720p-特惠",
		"改派目标模型名不能出现")
	assert.NotContains(t, body, `"channel_id":12`, "改派后的渠道同样不能暴露")

	assert.Nil(t, out.Data, "改派后 data 同样要清")
	assert.Equal(t, 0, out.ChannelId, "改派后 channel_id 同样要置 0")
	assert.Equal(t, "上游", out.Platform, "改派后 platform 同样要换成「上游」")
}
