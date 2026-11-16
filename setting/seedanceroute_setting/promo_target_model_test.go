package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

// 促销族的 CapCut 出口 vs 豆包备胎的 CapCut 出口 —— **两条链目标不同**。
//
// 2026-11-16 晚老板口径（同日两条，必须同时成立）：
//
//	「特惠和活动这两个模型会路由到的 cp-seedance-2.5flash 替换成
//	 cp-seedance-2.5，这个保证质量做口碑」
//	「seedance-2-5 这个模型的路由还是不变，还是路由到 cp-seedance-2.5flash」
//
// 所以本文件锁的是**分叉**，不是统一：
//
//	促销族（特惠 / 活动）：规则七 / 八 / 九 → cp-seedance-2.5（满血档）
//	豆包 seedance-2-5：    规则五备选备胎   → cp-seedance-2.5flash（Flash 档）
//
// ⚠️ 为什么值得单独一个测试文件：这两组常量在代码里长得几乎一样
//（名字只差后缀），将来「统一目标模型」时极容易一起改掉。改错的后果
//不是报错，而是**静默变贵或静默降质** —— 一边是促销族按口碑卖却交付
//便宜档，另一边是自营降级单按 Flash 的价收却付满血档的成本。

// 促销族的三条 CapCut 出口必须是 cp-seedance-2.5（满血档）。
func TestPromoFamilyCapcutTargetIsSeedance25(t *testing.T) {
	assert.Equal(t, constant.Capcut25Model, DefaultEcommerceTargetModel,
		"规则七（电商）的目标必须是 cp-seedance-2.5：特惠/活动是对客口碑商品")
	assert.Equal(t, constant.Capcut25Model, DefaultTotalFallbackTargetModel,
		"规则八（全量兜底）的目标必须与规则七同档，否则兜底一开就静默降质")
	assert.Equal(t, constant.Capcut25Model, DefaultRefAVTargetModel,
		"规则九（音视频参考）的目标必须与规则七/八同档")
}

// 豆包的降级备胎必须仍是 cp-seedance-2.5flash —— 老板明确要求「不变」。
func TestDoubaoTargetIsStillSeedance25Flash(t *testing.T) {
	assert.Equal(t, constant.Capcut25FlashModel, DefaultFailoverAltModel,
		"seedance-2-5 的备胎仍是 2.5 Flash：那是「原单已按一口价收过钱」的降级，"+
			"价格优先，不能跟着促销族升档")
}

// 两组必须**不相等** —— 这条是上面两条的反向守卫。
//
// 单看上面两条断言时，如果有人把四个常量都改成同一个值，
// 前两条会全绿（只要值恰好是 2.5 或都改成 Flash 就不会），
// 只有这一条能把「统一改」这件事拦下来。
func TestPromoTargetDiffersFromDoubaoTarget(t *testing.T) {
	assert.NotEqual(t, DefaultFailoverAltModel, DefaultEcommerceTargetModel,
		"促销族与豆包的 CapCut 出口必须分属两个档位，不能合并")
}

// 换档后目标仍在 CapCut 五档内，且是按秒计费商品。
//
// 这条守的是**准入逻辑不用改**：规则七/八/九的改写与准入走
// prepareCapcutRoute，它只适配 CapCut 这一族的契约（480p/720p、
// 4–30 秒、CapCut 比例表）。若哪天有人把目标改成族外模型名，
// 准入检查会全部形同虚设 —— 请求被改写到一条没适配过的链路上。
func TestPromoTargetStaysInsideCapcutFamily(t *testing.T) {
	for name, model := range map[string]string{
		"规则七": DefaultEcommerceTargetModel,
		"规则八": DefaultTotalFallbackTargetModel,
		"规则九": DefaultRefAVTargetModel,
	} {
		assert.True(t, constant.IsCapcutFamily(model),
			"%s 的目标 %q 必须属于 CapCut 五档（prepareCapcutRoute 只适配这一族）", name, model)
		assert.True(t, constant.IsCapcutPerSecond(model),
			"%s 的目标 %q 必须是按秒计费档（改派后的 size 倍率靠这一口径覆盖）", name, model)
	}
}
