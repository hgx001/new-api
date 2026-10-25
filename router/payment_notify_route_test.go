package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaymentNotifyRoutesRegistered 支付回调路由必须全部在册。
//
// 少注册一条的后果是静默的：网关按注册的地址回调，拿到 404 就持续重试，
// 用户那边钱已经扣了，订单永远 pending，没有任何告警。
func TestPaymentNotifyRoutesRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() { SetApiRouter(engine) })

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	for _, want := range []string{
		// VPN 年费套餐（官方直连 + 易支付两条通道）
		"POST /api/user/vpn/alipay/notify",
		"GET /api/user/vpn/alipay/notify",
		"POST /api/user/vpn/epay/notify",
		"GET /api/user/vpn/epay/notify",
		// 账户充值
		"POST /api/user/alipay/notify",
		"GET /api/user/alipay/notify",
		"POST /api/user/epay/notify",
		"GET /api/user/epay/notify",
		// 订阅
		"POST /api/subscription/epay/notify",
		"GET /api/subscription/epay/notify",
		// 其它网关
		"POST /api/stripe/webhook",
		"POST /api/creem/webhook",
		"POST /api/waffo/webhook",
		"POST /api/waffo-pancake/webhook/:env",
	} {
		assert.True(t, registered[want], "支付回调路由缺失：%s", want)
	}
}

// TestVpnNotifyRoutesReachableAnonymously VPN 回调由支付网关直连，不带会话/token：
// 请求必须能到达 handler（通道未开通时由 handler 自己回 fail），
// 不能被鉴权中间件拦成 401/403，更不能 404。
func TestVpnNotifyRoutesReachableAnonymously(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() { SetApiRouter(engine) })

	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/user/vpn/alipay/notify"},
		{http.MethodGet, "/api/user/vpn/alipay/notify"},
		{http.MethodPost, "/api/user/vpn/epay/notify"},
		{http.MethodGet, "/api/user/vpn/epay/notify"},
	} {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))

		assert.NotEqual(t, http.StatusNotFound, recorder.Code, "%s %s 未注册", tc.method, tc.path)
		assert.NotEqual(t, http.StatusUnauthorized, recorder.Code, "%s %s 被鉴权拦截，网关回调永远进不来", tc.method, tc.path)
		assert.NotEqual(t, http.StatusForbidden, recorder.Code, "%s %s 被鉴权拦截，网关回调永远进不来", tc.method, tc.path)
		assert.Equal(t, "fail", recorder.Body.String(), "%s %s 通道未开通时应由 handler 明确回 fail", tc.method, tc.path)
	}
}
