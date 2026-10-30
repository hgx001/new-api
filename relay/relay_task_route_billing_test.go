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

// 条件路由改派后的计费口径测试。
//
// 背景（老板 2026-10-30 定稿）：**客户请求哪个模型，就按哪个模型收费**。
// 改派只是内部调度，不改变对客价格。所以：
//   - 取价用 RelayInfo.BillingModelName()（= 源模型）
//   - 30 秒改派到豆包时，源模型是按秒的、目标 adaptor 是按次的不给 seconds，
//     必须由中间件记下的秒数补上 —— 否则 30 秒只按 1 秒收钱
//
// 这条测试盯的正是「补秒数」这一步：算错的后果是少收 29 秒的钱，
// 而且没有任何报错，只能靠对账发现。

const (
	// 源模型 seedance2.5-特惠：¥0.20/秒 → ModelPrice 0.027397260273972605
	routeSourceBaseQuota = 13698
	// 目标模型 seedance-2-5：¥3.00/次 → ModelPrice 0.410958904109589
	routeTargetFlatQuota = 205479
)

func newRouteBillingCtx(seconds float64) *gin.Context {
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	if seconds > 0 {
		common.SetContextKey(ctx, constant.ContextKeyRouteBillingSeconds, seconds)
	}
	return ctx
}

func newRouteBillingInfo(originModel, routeOriginModel string, ratios map[string]float64) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:      &relaycommon.ChannelMeta{},
		OriginModelName:  originModel,
		RouteOriginModel: routeOriginModel,
		PriceData: types.PriceData{
			Quota:       routeSourceBaseQuota,
			OtherRatios: ratios,
		},
	}
}

// 30 秒改派到豆包：必须按源模型的按秒价算，¥0.20 × 30 = ¥6.00。
func TestApplyRouteBillingSeconds_ThirtySecondsBillsSourceModel(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", "seedance2.5-特惠", nil)
	// 目标 adaptor（manwu）对按次模型返回 nil，所以 OtherRatios 是空的。
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	require.Equal(t, 30.0, info.PriceData.OtherRatios["seconds"], "必须补上源模型的秒数倍率")

	quota := applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
	assert.Equal(t, routeSourceBaseQuota*30, quota, "30 秒 = 13698 × 30 = 410940（¥6.00）")
	// 反向断言：绝不能落到目标模型的按次价。
	assert.NotEqual(t, routeTargetFlatQuota, quota, "不能按豆包的按次价 ¥3.00 收")
}

// adaptor 已经给了 seconds（比例规则 → wan3）时不覆盖它的判断。
func TestApplyRouteBillingSeconds_KeepsAdaptorSeconds(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", "seedance2.5-特惠",
		map[string]float64{"seconds": 15})
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	assert.Equal(t, 15.0, info.PriceData.OtherRatios["seconds"], "adaptor 的秒数优先")
}

// 未改派时不做任何事：非路由请求零影响。
func TestApplyRouteBillingSeconds_NoopWhenNotRouted(t *testing.T) {
	info := newRouteBillingInfo("seedance-2-5", "", nil)
	applyRouteBillingSeconds(newRouteBillingCtx(30), info)

	assert.Empty(t, info.PriceData.OtherRatios, "未改派不该补任何倍率")
}

// 中间件没记秒数（比如规则二命中）时保持原样，不瞎猜。
func TestApplyRouteBillingSeconds_NoopWithoutContextSeconds(t *testing.T) {
	info := newRouteBillingInfo("wan3.0-720p-特惠", "seedance2.5-特惠", nil)
	applyRouteBillingSeconds(newRouteBillingCtx(0), info)

	assert.Empty(t, info.PriceData.OtherRatios, "没有秒数就不补，交给 adaptor")
}

// nil info 不能 panic（重试路径上 info 可能尚未填充）。
func TestApplyRouteBillingSeconds_NilInfo(t *testing.T) {
	assert.NotPanics(t, func() {
		applyRouteBillingSeconds(newRouteBillingCtx(30), nil)
	})
}
