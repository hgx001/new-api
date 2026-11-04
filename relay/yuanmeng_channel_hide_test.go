package relay

// ════════════════════════════════════════════════════════════════════════════
// wan3.0-排队（视频生成车站排队通道）的「渠道隐藏」契约
// ════════════════════════════════════════════════════════════════════════════
//
// 事实：对外模型名是「wan3.0-排队」，上游是「视频生成车站」排队通道的万相 wan3，
// 底层是攒批发车的车队模式（每条固定扣 1 次，成片直链约 24 小时过期）。
//
// 为什么它必须脱敏：
//  1. 对外名里的 "wan3" 就是上游模型名本身 —— 不脱敏等于直接报出货源是万相；
//  2. 上游回执（task.Data）里有上游任务号（T130）、上游成片直链域名与路径；
//  3. 站点名「视频生成车站 / yuanmengstudio」出现在回执里同样等于报出货源。
//
// ⚠️ 漏掉它的后果与其它别名模型完全一致：
//   IsUpstreamAliasModel 返回 false →
//     · RedactUpstreamIdentity 直接 return → 报错文案里的上游模型名原样漏出；
//     · redactAliasTaskUpstream 直接 return → 上游回执（任务号 + 成本 + 直链）
//       原样进对外 DTO，用户能直接绕过本站去抓那条会过期的直链。
//
// 本文件把「排队档享有渠道隐藏保护」变成可执行断言。

import (
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
)

// yuanmengPaiDuiModel 是被保护的别名模型（排队档）。
const yuanmengPaiDuiModel = "wan3.0-排队"

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：别名白名单
// ────────────────────────────────────────────────────────────────────────────

// TestYuanmengPaiDui_必须在别名白名单里 守的是**所有脱敏的总开关**。
func TestYuanmengPaiDui_必须在别名白名单里(t *testing.T) {
	assert.True(t, model.IsUpstreamAliasModel(yuanmengPaiDuiModel),
		"模型 %q 必须留在 upstreamAliasModels 里 —— 删掉它，"+
			"redactAliasTaskUpstream 会直接 return，上游模型名与上游回执全部漏给下游", yuanmengPaiDuiModel)

	// 既有别名模型也必须仍在（回归守卫：加新模型时不能把旧的弄掉）。
	for _, name := range []string{
		"seedance2.5-特惠", "seedance2.5-活动",
		"wan3.0-720p-特惠", "wan3.0-720p-官网", "wan3.0-720p-按次",
	} {
		assert.True(t, model.IsUpstreamAliasModel(name),
			"既有别名模型 %q 必须仍在别名白名单里", name)
	}

	// 反证：非别名模型不该被误判成别名。
	for _, name := range []string{"gpt-4o", "wan3.0-video", "seedance-2-5"} {
		assert.False(t, model.IsUpstreamAliasModel(name),
			"模型 %q 不是别名，不该进白名单 —— 会把客户自己买的模型名也打散", name)
	}
}

// 上游回执与报错文案里不能出现货源痕迹。
//
// ⚠️ 用 RedactUpstreamText（完整入口）而不是 RedactUpstreamIdentity：
// 完整入口才会先「摘地址」，只调 Identity 的话 URL 会留在文案里。
func TestYuanmengPaiDui_回执与报错脱敏(t *testing.T) {
	raw := "yuanmengstudio task T130 failed: model wan3.0-video not available, " +
		"see https://yuanmengstudio.com/vr/api/pub/abc.mp4"

	redacted := model.RedactUpstreamText(raw, yuanmengPaiDuiModel)

	assert.NotContains(t, redacted, "yuanmengstudio", "上游站点名必须被脱敏")
	assert.NotContains(t, redacted, "wan3.0-video", "上游模型名必须被脱敏")
	assert.NotContains(t, redacted, "http", "上游地址必须被摘掉")
}

// 供应商词对**所有模型**生效（老板口径：有渠道的直接说「上游」）。
func TestYuanmengPaiDui_站点名对所有模型生效(t *testing.T) {
	raw := "视频生成车站 队列已满"

	redacted := model.RedactUpstreamVendor(raw)

	assert.NotContains(t, redacted, "视频生成车站", "上游品牌词必须被换成中性词")
	assert.Contains(t, redacted, "上游")
}

// 对外模型名在**文案里**会被抹掉 wan3 前缀 —— 这与既有别名模型完全一致。
//
// 口径（与 wan3.0-720p-特惠 / wan3.0-720p-按次 同款）：别名模型连上游
// 模型名一起抹，因为对外名的前缀本身就是上游模型名。我们的对外名
// 「wan3.0-排队」里的 wan3 就是上游模型名，留着等于报出货源。
//
// 真正给客户看的模型名在**模型名字段**里（properties / model），
// 那儿是原样保留的，不受这段文案脱敏影响。
//
// ⚠️ 这条断言同时是**回归守卫**：如果哪天有人把「wan3.0」从
// upstreamModelLiterals 里删掉（理由可能是「客户自己买的名字别乱动」），
// 上游模型名就会重新漏出 —— 那才是真的破坏货源保护。
func TestYuanmengPaiDui_文案里的上游模型名前缀被抹(t *testing.T) {
	raw := "模型 wan3.0-排队 当前排队人数较多"

	redacted := model.RedactUpstreamText(raw, yuanmengPaiDuiModel)

	assert.NotContains(t, redacted, "wan3", "文案里的上游模型名前缀必须被抹掉")
	assert.Contains(t, redacted, "上游", "必须换成中性词「上游」")
	assert.Contains(t, redacted, "排队", "对外档位语义必须保留")
}

// 对照组：既有别名模型也是同样口径 —— 证明排队档没有特殊待遇差异。
func TestYuanmengPaiDui_与既有别名模型口径一致(t *testing.T) {
	for _, name := range []string{"wan3.0-720p-特惠", "wan3.0-720p-按次", "wan3.0-排队"} {
		redacted := model.RedactUpstreamText("模型 "+name+" 处理中", name)
		assert.Contains(t, redacted, "上游", "模型 %q 的文案脱敏口径必须一致", name)
	}
}
