package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuotaQueryRouteRegistersWithoutConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	// 生产由 SetRouter 把三组路由注册到同一个 engine：
	// dashboard 已有 /v1/dashboard/billing/{subscription,usage}，
	// relay 新增 /v1/dashboard/billing/quota，必须能共存。
	require.NotPanics(t, func() {
		SetDashboardRouter(engine)
		SetRelayRouter(engine)
		SetVideoRouter(engine)
	})

	// 新增路由已注册：不带 key 会被 TokenAuth 拦成 401，而不是 404
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/dashboard/billing/quota", nil))
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)

	// 同前缀的既有路由未被新注册破坏
	usageRecorder := httptest.NewRecorder()
	engine.ServeHTTP(usageRecorder, httptest.NewRequest(http.MethodGet, "/v1/dashboard/billing/usage", nil))
	assert.Equal(t, http.StatusUnauthorized, usageRecorder.Code)
}
