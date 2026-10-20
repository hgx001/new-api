package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestPaymentNotifyRateLimitBlocksFlood 支付回调端点是匿名接口，必须有独立限流。
func TestPaymentNotifyRateLimitBlocksFlood(t *testing.T) {
	oldRedisEnabled := common.RedisEnabled
	oldEnable := common.PaymentNotifyRateLimitEnable
	oldNum := common.PaymentNotifyRateLimitNum
	oldDuration := common.PaymentNotifyRateLimitDuration
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.PaymentNotifyRateLimitEnable = oldEnable
		common.PaymentNotifyRateLimitNum = oldNum
		common.PaymentNotifyRateLimitDuration = oldDuration
	})

	common.RedisEnabled = false
	common.PaymentNotifyRateLimitEnable = true
	common.PaymentNotifyRateLimitNum = 3
	common.PaymentNotifyRateLimitDuration = 60

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/user/alipay/notify", PaymentNotifyRateLimit(), func(c *gin.Context) {
		c.String(http.StatusOK, "success")
	})

	newRequest := func() *http.Request {
		request := httptest.NewRequest(http.MethodPost, "/api/user/alipay/notify", nil)
		request.RemoteAddr = "203.0.113.77:4321"
		return request
	}

	for i := 0; i < 3; i++ {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, newRequest())
		require.Equal(t, http.StatusOK, recorder.Code, "第 %d 次请求应在阈值内放行", i+1)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, newRequest())
	require.Equal(t, http.StatusTooManyRequests, recorder.Code, "超过阈值后必须限流，避免匿名回调端点被刷")
}

// TestPaymentNotifyRateLimitDefaultIsGenerous 支付回调的默认阈值必须比
// CriticalRateLimit 宽松得多。
//
// CriticalRateLimit 是 20 次 / 20 分钟，直接套到支付回调上会在业务高峰
// （同一批来源 IP 短时间集中到达）把真实付款的通知挡掉 → 钱扣了但订单不入账。
func TestPaymentNotifyRateLimitDefaultIsGenerous(t *testing.T) {
	require.True(t, common.PaymentNotifyRateLimitNum >= 100,
		"支付回调限流阈值过低会误伤真实付款，当前 %d", common.PaymentNotifyRateLimitNum)
	require.GreaterOrEqual(t, common.PaymentNotifyRateLimitDuration, int64(60),
		"支付回调限流窗口过短会误伤真实付款，当前 %d", common.PaymentNotifyRateLimitDuration)
	require.Greater(t, common.PaymentNotifyRateLimitNum, common.CriticalRateLimitNum,
		"支付回调限流阈值必须高于 CriticalRateLimit")
}
