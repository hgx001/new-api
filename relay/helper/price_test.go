package helper

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelPriceHelperTieredUsesPreloadedRequestInput(t *testing.T) {
	gin.SetMode(gin.TestMode)

	saved := map[string]string{}
	require.NoError(t, config.GlobalConfig.SaveToDB(func(key, value string) error {
		saved[key] = value
		return nil
	}))
	t.Cleanup(func() {
		require.NoError(t, config.GlobalConfig.LoadFromDB(saved))
	})

	require.NoError(t, config.GlobalConfig.LoadFromDB(map[string]string{
		"billing_setting.billing_mode": `{"tiered-test-model":"tiered_expr"}`,
		"billing_setting.billing_expr": `{"tiered-test-model":"param(\"stream\") == true ? tier(\"stream\", p * 3) : tier(\"base\", p * 2)"}`,
	}))

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/api/channel/test/1", nil)
	req.Body = nil
	req.ContentLength = 0
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req
	ctx.Set("group", "default")

	info := &relaycommon.RelayInfo{
		OriginModelName: "tiered-test-model",
		UserGroup:       "default",
		UsingGroup:      "default",
		RequestHeaders:  map[string]string{"Content-Type": "application/json"},
		BillingRequestInput: &billingexpr.RequestInput{
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"stream":true}`),
		},
	}

	priceData, err := ModelPriceHelper(ctx, info, 1000, &types.TokenCountMeta{})
	require.NoError(t, err)
	require.Equal(t, 1500, priceData.QuotaToPreConsume)
	require.NotNil(t, info.TieredBillingSnapshot)
	require.Equal(t, "stream", info.TieredBillingSnapshot.EstimatedTier)
	require.Equal(t, billing_setting.BillingModeTieredExpr, info.TieredBillingSnapshot.BillingMode)
	require.Equal(t, common.QuotaPerUnit, info.TieredBillingSnapshot.QuotaPerUnit)
}

// 条件路由改派后，取价必须按**客户请求的源模型**（BillingModelName），
// 而不是实际执行的目标模型。
//
// 这是「客户请求哪个模型就按哪个模型收费」的落点：30 秒改派到豆包后，
// 客户付的仍是 seedance2.5-特惠 的按秒价，不是豆包的按次价。
func TestModelPriceHelperPerCallBillsRouteOriginModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 备份并恢复价格表，避免污染同包其它用例。
	previous := ratio_setting.GetModelPriceCopy()
	t.Cleanup(func() {
		raw, err := common.Marshal(previous)
		require.NoError(t, err)
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(raw)))
	})

	const (
		sourcePrice = 0.027397260273972605 // 源模型：¥0.20/秒
		targetPrice = 0.410958904109589    // 目标模型：¥3.00/次
	)
	raw, err := common.Marshal(map[string]float64{
		"route-src-model": sourcePrice,
		"route-dst-model": targetPrice,
	})
	require.NoError(t, err)
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(raw)))

	newCtx := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		ctx.Set("group", "default")
		return ctx
	}
	newInfo := func() *relaycommon.RelayInfo {
		return &relaycommon.RelayInfo{
			OriginModelName:  "route-dst-model", // 实际执行的目标模型
			RouteOriginModel: "route-src-model", // 客户请求的源模型
			UserGroup:        "default",
			UsingGroup:       "default",
		}
	}

	// 改派过 → 按源模型价
	routed := newInfo()
	priceData, err := ModelPriceHelperPerCall(newCtx(), routed)
	require.NoError(t, err)
	require.InDelta(t, sourcePrice, priceData.ModelPrice, 1e-18, "改派后必须按源模型取价")
	require.True(t, priceData.UsePrice)
	require.Equal(t,
		int(sourcePrice*float64(common.QuotaPerUnit)*priceData.GroupRatioInfo.GroupRatio),
		priceData.Quota)

	// 未改派 → 按实际执行模型价（行为与改动前一致）
	plain := newInfo()
	plain.RouteOriginModel = ""
	priceData, err = ModelPriceHelperPerCall(newCtx(), plain)
	require.NoError(t, err)
	require.InDelta(t, targetPrice, priceData.ModelPrice, 1e-18)
	require.Equal(t,
		int(targetPrice*float64(common.QuotaPerUnit)*priceData.GroupRatioInfo.GroupRatio),
		priceData.Quota)
}
