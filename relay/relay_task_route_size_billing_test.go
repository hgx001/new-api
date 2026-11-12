package relay

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 条件路由改派后的 **size 倍率** 口径测试（规则六：短时长 → 有赞标准版）。
//
// 与 seconds 那条补丁是同一个问题的两面：
//
//	计费按源模型（seedance2.5-特惠）算 —— 720P ¥0.20/秒、1080P ¥0.30/秒；
//	而改派目标 wan3.0-video 三档同价，它的 adaptor 会给 size = 1.0。
//
// 不覆盖就会按 ¥0.20/秒 收 1080P（应收 ¥0.30），少收 33%，且没有任何报错。
//
// 为什么必须**覆盖**而不是相乘：adaptor 给的 1.0 是「目标模型的档位差价」，
// 与源模型的档位差价是同一维度的东西，不是两个可叠加的倍率。
// 相乘看似无害（×1.0 等于没变），但一旦目标 adaptor 将来给出非 1 的倍率，
// 相乘会把两套定价叠成四不像 —— 覆盖是唯一自洽的语义。

func newRouteBillingSizeCtx(ratio float64) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	if ratio > 0 {
		common.SetContextKey(ctx, constant.ContextKeyRouteBillingSizeRatio, ratio)
	}
	return ctx
}

// 1080P 改派到标准版：size 必须被覆盖成源模型的 1.5 倍。
func TestApplyRouteBillingSizeRatio_OverridesTargetFlatRatio(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-video", "seedance2.5-特惠", map[string]float64{
		"seconds": 5,
		"size":    1.0, // 标准版三档同价，adaptor 给的就是 1.0
	})
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.5), info)

	require.Equal(t, 1.5, info.PriceData.OtherRatios["size"],
		"1080P 必须按源模型的 1.5 倍收，不能被目标的 1.0 冲掉")

	quota := applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
	assert.Equal(t, routeSourceBaseQuota*5*3/2, quota,
		"5 秒 1080P = 13698 × 5 × 1.5 = 102735（¥0.30/秒）")
}

// 720P 改派：倍率是 1.0，写进去等于没变 —— 但必须写，语义上是「按源模型口径」。
func TestApplyRouteBillingSizeRatio_BaseTierStaysOne(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-video", "seedance2.5-特惠", map[string]float64{
		"seconds": 5,
		"size":    1.0,
	})
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.0), info)

	assert.Equal(t, 1.0, info.PriceData.OtherRatios["size"])
}

// 按次档（seedance2.5-活动，¥4.5/次）不接收任何倍率。
//
// 覆盖成 1.5 会把一口价乘歪；虽然按次档的 EstimateBilling 已返回 nil、
// mergeEstimatedRatios 也会清空倍率，但这条补丁在它之后执行 ——
// 不在这里拦住就会把倍率重新塞回去。
func TestApplyRouteBillingSizeRatio_SkipsPerCallSourceModel(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-video", constant.Seedance25HuoDongModel, map[string]float64{
		"size": 1.0,
	})
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.5), info)

	assert.Equal(t, 1.0, info.PriceData.OtherRatios["size"],
		"按次档的一口价不能被 size 倍率乘歪")
}

// 没发生改派（RouteOriginModel 为空）时零影响。
func TestApplyRouteBillingSizeRatio_NoopWithoutRoute(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-video", "", map[string]float64{"size": 1.0})
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.5), info)

	assert.Equal(t, 1.0, info.PriceData.OtherRatios["size"],
		"未改派的请求不能被动到")
}

// 改派了但中间件没记倍率（例如规则一改派，只记秒数）时不覆盖目标自己的档位差价。
func TestApplyRouteBillingSizeRatio_NoopWithoutContextValue(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", "seedance2.5-特惠", map[string]float64{
		"seconds": 30,
		"size":    1.2,
	})
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(0), info)

	assert.Equal(t, 1.2, info.PriceData.OtherRatios["size"],
		"没有记倍率时应保留目标 adaptor 的判断")
}

// info 为 nil 时不能 panic（防御性：调用点在 relay 主链路上）。
func TestApplyRouteBillingSizeRatio_NilInfoIsSafe(t *testing.T) {
	assert.NotPanics(t, func() {
		applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.5), nil)
	})
}

// 守卫：OtherRatios 的键必须是 "size"，改名字会让 applyTaskOtherRatios 静默漏乘。
func TestApplyRouteBillingSizeRatio_UsesSizeKey(t *testing.T) {
	info := &relaycommon.RelayInfo{
		ChannelMeta:      &relaycommon.ChannelMeta{},
		OriginModelName:  "wan3.0-video",
		RouteOriginModel: "seedance2.5-特惠",
		PriceData:        types.PriceData{Quota: routeSourceBaseQuota},
	}
	applyRouteBillingSizeRatio(newRouteBillingSizeCtx(1.5), info)

	_, ok := info.PriceData.OtherRatios["size"]
	require.True(t, ok, "倍率键必须是 size（与 adaptor 的 EstimateBilling 同键）")
}
