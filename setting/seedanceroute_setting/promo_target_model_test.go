package seedanceroute_setting

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

// 促销族 CapCut 出口 vs 豆包备胎的 CapCut 出口 —— **2026-11-17 起已统一**。
//
// 口径沿革（两次老板决定，后一次覆盖前一次）：
//
//	2026-11-16 晚：「特惠/活动 → cp-seedance-2.5 满血档做口碑；豆包 seedance-2-5
//	             → cp-seedance-2.5flash 不变」—— 当时两条刻意分叉。
//	2026-11-17：「所有超分去掉，直接返回 720p 原片；特惠活动路由模型全部改
//	            cp-seedance-2.5flash」—— 取消 480p+超分后，两条出口统一到 Flash。
//
// 所以本文件现在锁的是**统一**，不是分叉。保留独立测试文件是为了把这条
// 口径钉在显眼处：将来若有人只改其中一处常量，这里会立刻报出来。

// 促销族的三条 CapCut 出口必须统一到 cp-seedance-2.5flash。
func TestPromoFamilyCapcutTargetIsSeedance25Flash(t *testing.T) {
	assert.Equal(t, constant.Capcut25FlashModel, DefaultEcommerceTargetModel,
		"规则七（电商）的目标必须是 cp-seedance-2.5flash")
	assert.Equal(t, constant.Capcut25FlashModel, DefaultTotalFallbackTargetModel,
		"规则八（全量兜底）的目标必须与规则七同档")
	assert.Equal(t, constant.Capcut25FlashModel, DefaultRefAVTargetModel,
		"规则九（音视频参考）的目标必须与规则七/八同档")
	assert.Equal(t, constant.Capcut25FlashModel, DefaultShortCapcutTargetModel,
		"规则六（短时长 CapCut 档）的目标必须与规则七/八/九同档")
}

// 豆包的降级备胎仍是 cp-seedance-2.5flash（与促销族出口同型号、不同链路：
// 那边按 720p 原生直出，走 middleware/video_route_failover_alt.go）。
func TestDoubaoTargetIsStillSeedance25Flash(t *testing.T) {
	assert.Equal(t, constant.Capcut25FlashModel, DefaultFailoverAltModel,
		"seedance-2-5 的备胎仍是 2.5 Flash：原单已按一口价收过钱的降级，价格优先")
}

// 两组现在**相等**（2026-11-17 起统一），这条守住「统一」这个决定本身。
func TestPromoTargetEqualsDoubaoTarget(t *testing.T) {
	assert.Equal(t, DefaultFailoverAltModel, DefaultEcommerceTargetModel,
		"促销族与豆包的 CapCut 出口统一为 2.5 Flash")
}

// 换档后目标仍在 CapCut 五档内，且是按秒计费商品。
//
// 这条守的是**准入逻辑不用改**：规则六/七/八/九的改写与准入走
// prepareCapcutRoute，它只适配 CapCut 这一族的契约（480p/720p、
// 4–30 秒、CapCut 比例表）。若哪天有人把目标改成族外模型名，
// 准入检查会全部形同虚设 —— 请求被改写到一条没适配过的链路上。
func TestPromoTargetStaysInsideCapcutFamily(t *testing.T) {
	for name, model := range map[string]string{
		"规则六": DefaultShortCapcutTargetModel,
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
