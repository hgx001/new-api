package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 按次档在**改派链路**里的倍率隔离测试。
//
// 背景（2026-11-02 上架 seedance2.5-活动 时查出来的三个真实超收路径）：
//
//	seedance2.5-活动 是**按次**档（¥4.5/次、固定 30 秒），母本 seedance2.5-特惠
//	是**按秒**档（¥0.20/秒）。两者共用同一条路由规则。
//
//	活动版被改派后，目标 adaptor 是**别的渠道**的适配器 —— 它不知道源模型是按次档，
//	会按自己的习惯返回倍率。三条路径都会把 ¥4.5 乘成别的数：
//
//	① 初始倍率（relay_task.go 的 EstimateBilling）
//	   规则二/三改派到 wan3 链路，wan3 adaptor 自己返回：
//	       {"seconds": 30, "size": 1.0}
//	   → applyTaskOtherRatios 会把 ¥4.5 乘 30 = **¥135**。
//	   （这是 applyRouteBillingSeconds 拦不住的那种：它只在 adaptor **没给** seconds
//	   时补秒数，不会删除 adaptor 自己给的。）
//
//	② 提交后重算（relay_task.go 的 AdjustBillingOnSubmit + recalcQuotaFromRatios）
//	   recalcQuotaFromRatios 会先**除掉**原倍率还原基础额度，再乘新倍率。
//	   若 OtherRatios 里残留 seconds=30 而 Quota 根本没乘过（按次档），
//	   baseQuota 会被算成 308219/30 = 10273 —— 额度被莫名砍到三分之一。
//
//	③ 轮询阶段差额结算（service/task_polling.go）
//	   PerCallBilling 判定与计费处不同步时，完成态会再改一次额度。
//
// 这个文件盯的是「按次档不参与任何倍率计算」这条不变量。

// ① 初始倍率：adaptor 自己返回 seconds/size 时，按次档必须**原样拒绝**。
//
// 金额换算：¥4.5/次 → 0.6164383561643836 USD × 500000 = 308219.178… → 308219。
func TestPerCallBilling_RejectsAdaptorEstimatedRatios(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25HuoDongModel, nil)
	info.PriceData.Quota = 308219
	info.PriceData.UsePrice = true

	// 模拟 wan3 adaptor 的 EstimateBilling 返回值（规则二/三改派后的真实形态）。
	mergeEstimatedRatios(info, map[string]float64{"seconds": 30, "size": 1.0})

	assert.Empty(t, info.PriceData.OtherRatios,
		"按次档不能接受 adaptor 返回的任何倍率（seconds/size）")
	assert.Equal(t, 308219, info.PriceData.Quota,
		"按次档的预扣额度必须等于 ModelPrice 本身，不能是 308219×30")
	assert.NotEqual(t, 308219*30, info.PriceData.Quota, "不能收 ¥135（30 倍）")
}

// ① 反向断言：母本（按秒档）**必须**照常接受 adaptor 的倍率 ——
// 误伤母本的后果是少收 29 秒的钱（¥0.20 而不是 ¥6.00），同样是无声的钱差。
func TestPerSecondBilling_StillAppliesAdaptorEstimatedRatios(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25TeHuiModel, nil)
	info.PriceData.Quota = 13698

	mergeEstimatedRatios(info, map[string]float64{"seconds": 30, "size": 1.0})
	require.Equal(t, 30.0, info.PriceData.OtherRatios["seconds"], "母本必须照常接受秒数倍率")
	require.Equal(t, 1.0, info.PriceData.OtherRatios["size"], "母本必须照常接受分辨率倍率")

	applyBillingRatios(info)
	assert.Equal(t, 13698*30, info.PriceData.Quota,
		"母本 30 秒 = 13698 × 30 = 410940（¥6.00），行为不能变")
}

// ② 提交后重算：adaptor 返回倍率时，按次档不得重算额度。
//
// 这条路径最阴险：recalcQuotaFromRatios 会**除掉**原倍率还原基础额度。
// 按次档的 Quota 本来就没乘过倍率（308219），OtherRatios 里若残留 seconds=30，
// 基础额度会被算成 308219/30 = 10273 —— 客户被少收三分之二的钱，
// 而账单上写着 ¥4.5、实际只收了 ¥1.50。
func TestPerCallBilling_SkipsSubmitTimeRecalculation(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25HuoDongModel, nil)
	info.PriceData.Quota = 308219

	quota := resolveSubmitQuota(info, map[string]float64{"seconds": 30})

	assert.Equal(t, 308219, quota,
		"按次档的额度必须是 ¥4.5/次 的 308219，不能被提交后的倍率重算改写")
	assert.NotEqual(t, 10273, quota,
		"不能因为残留的 seconds=30 把基础额度除成 308219/30（少收三分之二）")
	assert.Empty(t, info.PriceData.OtherRatios,
		"按次档不能接受提交后返回的倍率")
}

// ② 反向断言：母本（按秒档）的提交后重算必须照常工作。
//
// ⚠️ 测试前提：recalcQuotaFromRatios 的语义是「Quota 已经乘过倍率」——
// 它会先除掉原倍率还原基础额度，再乘新倍率。所以这里的 Quota 必须是**预扣后**的额度
// （13698 × 30 = 410940），不是基础额度。
func TestPerSecondBilling_StillRecalculatesOnSubmit(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25TeHuiModel, nil)
	info.PriceData.Quota = 13698 * 30
	info.PriceData.OtherRatios = map[string]float64{"seconds": 30}

	// 上游实际跑了 15 秒 → 按 15 秒重算。
	quota := resolveSubmitQuota(info, map[string]float64{"seconds": 15})

	assert.Equal(t, 13698*15, quota,
		"母本的提交后重算必须照常生效（13698 × 15）")
	assert.Equal(t, 15.0, info.PriceData.OtherRatios["seconds"],
		"母本的 OtherRatios 必须被更新成提交后的倍率")
}

// 判据必须走**计费口径**（源模型），不是实际执行的模型。
//
// 场景：客户请求 seedance2.5-活动（按次），改派到 wan3.0-720p-特惠 执行。
// 如果判据误用了 info.OriginModelName（执行模型 = 按秒的 wan3），
// 会「碰巧」得到正确结果 —— 但那是**偶然**对，逻辑是错的：
// 万一将来有个按秒模型被改派到按次模型，按秒档就会漏补秒数（少收钱）。
func TestPerCallBilling_DecisionUsesBillingModelOnly(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", constant.Seedance25HuoDongModel, nil)
	require.True(t, isPerCallBilling(info),
		"判据只看源模型（计费口径），执行模型是按秒也不能改变结论")

	// 反过来：源模型按秒、执行模型按次 —— 结论必须是按秒。
	info2 := newRouteBillingInfo("seedance-2-5", constant.Seedance25TeHuiModel, nil)
	require.False(t, isPerCallBilling(info2),
		"源模型是按秒档时，即使执行模型是按次的也不能判成按次（否则少收钱）")
}

// TASK_PRICE_PATCH 环境变量命中的模型（站内既有的按次先例，如 sora）
// 同样必须被完整隔离 —— 判据不能只认能力组。
func TestPerCallBilling_CoversTaskPricePatchModels(t *testing.T) {
	previous := constant.TaskPricePatches
	constant.TaskPricePatches = []string{"sora-2"}
	t.Cleanup(func() { constant.TaskPricePatches = previous })

	info := newRouteBillingInfo("seedance-2-5", "sora-2", nil)
	info.PriceData.Quota = 308219

	mergeEstimatedRatios(info, map[string]float64{"seconds": 30})
	applyBillingRatios(info)
	quota := resolveSubmitQuota(info, map[string]float64{"seconds": 30})

	assert.Equal(t, 308219, quota,
		"TASK_PRICE_PATCH 命中的模型也是按次档，不能被倍率改写")
}

// 族外的普通按秒模型行为不变（能力组与 TASK_PRICE_PATCH 都不命中）。
func TestPerCallBilling_DoesNotAffectRegularModels(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", "wan3.0-video", nil)
	info.PriceData.Quota = 13698

	mergeEstimatedRatios(info, map[string]float64{"seconds": 5})
	applyBillingRatios(info)

	require.Equal(t, 5.0, info.PriceData.OtherRatios["seconds"], "普通按秒模型照常接受倍率")
	assert.Equal(t, 13698*5, info.PriceData.Quota, "普通按秒模型的计费行为不变")
}

// nil info 不能 panic（重试路径上 info 可能尚未填充）。
func TestPerCallBilling_NilInfo(t *testing.T) {
	assert.NotPanics(t, func() {
		require.False(t, isPerCallBilling(nil))
		mergeEstimatedRatios(nil, map[string]float64{"seconds": 30})
		applyBillingRatios(nil)
		resolveSubmitQuota(nil, map[string]float64{"seconds": 30})
	})
}

// 按次档必须**清空已存在的倍率**，不只是「不新增」。
//
// 这条守卫盯的是 remix 路径：ResolveOriginTask 会从原任务的 BillingContext
// 里取出 OtherRatios 预设进 info.PriceData（relay_task.go 的 ResolveOriginTask）。
// 那份倍率是**按秒档**留下来的（seconds/size）—— 对按次档来说，它必须被清掉：
//   - 不清 → applyBillingRatios 把 ¥4.5 乘上 seconds=30 = ¥135（超收 30 倍）；
//   - 不清 → recalcQuotaFromRatios 会把基础额度除成 308219/30（少收三分之二）。
//
// ⚠️ 这也是 M26 变异暴露的鉴别力缺口：只在「不新增倍率」这一层拦截时，
// 预置倍率仍会漏过去。必须单独锁死「按次档的 OtherRatios 为空」。
func TestPerCallBilling_ClearsPreexistingRatios(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel,
		map[string]float64{"seconds": 30, "size": 1.5})
	info.PriceData.Quota = 308219

	mergeEstimatedRatios(info, nil)
	applyBillingRatios(info)

	assert.Empty(t, info.PriceData.OtherRatios,
		"按次档必须清空残留倍率（remix 路径会预设按秒档的 OtherRatios）")
	assert.Equal(t, 308219, info.PriceData.Quota,
		"清空倍率后额度必须仍是 ¥4.5/次 的 308219")
}

// 母本的预置倍率必须保留（remix 按秒档要照常按秒数计费）。
func TestPerSecondBilling_KeepsPreexistingRatios(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25TeHuiModel,
		map[string]float64{"seconds": 30})
	info.PriceData.Quota = 13698

	mergeEstimatedRatios(info, nil)
	applyBillingRatios(info)

	require.Equal(t, 30.0, info.PriceData.OtherRatios["seconds"],
		"母本的预置倍率必须保留")
	assert.Equal(t, 13698*30, info.PriceData.Quota, "母本照常按秒数计费")
}

// applyBillingRatios 自身的隔离必须独立成立 —— 不能只靠「合并阶段没产生倍率」。
//
// 这条守卫盯的是**倍率从别的路径进来**的情形：
//   - applyRouteBillingSeconds 按改派补的秒数；
//   - 将来新增的任何倍率来源。
//
// ⚠️ M26 变异暴露的鉴别力缺口：只在「合并阶段拦截」时，把 applyBillingRatios 的
// 隔离去掉，测试仍全绿 —— 说明「应用倍率」这一步本身没有守卫。
// 实际后果：¥4.5 被乘上 seconds=30 = **¥135**（超收 30 倍），且没有任何报错。
func TestPerCallBilling_ApplyStepRejectsRatiosIndependently(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel, nil)
	info.PriceData.Quota = 308219
	info.PriceData.UsePrice = true

	// 模拟「倍率已经进来了」的状态（例如改派补秒数之后、应用倍率之前）。
	info.PriceData.AddOtherRatio("seconds", 30)
	info.PriceData.AddOtherRatio("size", 1.0)

	applyBillingRatios(info)

	assert.Equal(t, 308219, info.PriceData.Quota,
		"按次档的应用倍率这一步必须独立拦住：¥4.5 不能被乘成 ¥135")
	assert.NotEqual(t, 308219*30, info.PriceData.Quota, "不能收 ¥135（30 倍）")
}

// 母本（按秒档）的应用倍率步骤必须照常生效 ——
// 这是 M26 的**反向守卫**：没有它，一个「把所有模型都当按次」的错误实现也能让
// 上面那条测试变绿。
func TestPerSecondBilling_ApplyStepStillMultiplies(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", constant.Seedance25TeHuiModel, nil)
	info.PriceData.Quota = 13698
	info.PriceData.AddOtherRatio("seconds", 30)

	applyBillingRatios(info)

	assert.Equal(t, 13698*30, info.PriceData.Quota,
		"母本的应用倍率步骤必须照常乘（13698 × 30 = ¥6.00）")
}

// 活动版与母本的**对称性**守卫：能力相同，只有计费口径不同。
// 两个档位必须各自走到正确的分支，不能被合并成同一种行为。
func TestPerCallBilling_MotherAndHuoDongTakeDifferentPaths(t *testing.T) {
	perCall := newRouteBillingInfo("seedance-2-5", constant.Seedance25HuoDongModel, nil)
	perCall.PriceData.Quota = 308219
	perSecond := newRouteBillingInfo("seedance-2-5", constant.Seedance25TeHuiModel, nil)
	perSecond.PriceData.Quota = 13698

	mergeEstimatedRatios(perCall, map[string]float64{"seconds": 30})
	mergeEstimatedRatios(perSecond, map[string]float64{"seconds": 30})
	applyBillingRatios(perCall)
	applyBillingRatios(perSecond)

	assert.Equal(t, 308219, perCall.PriceData.Quota, "活动版按次：¥4.5/次")
	assert.Equal(t, 13698*30, perSecond.PriceData.Quota, "母本按秒：¥0.20×30 = ¥6.00")
}
