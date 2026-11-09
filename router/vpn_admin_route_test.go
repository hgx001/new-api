package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVpnAdminOrdersRouteRegistered 管理端订单视图必须独立于用户侧接口注册。
// 两条都是 GET：若误把管理端接口挂到 /api/user/vpn/orders，会把「我的订单」
// 顶掉或让管理员数据走用户鉴权；这里锁住三条路径同时在册。
func TestVpnAdminOrdersRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() { SetApiRouter(engine) })

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	assert.True(t, registered[http.MethodGet+" /api/vpn/admin/orders"], "管理端 VPN 订单接口未注册")
	assert.True(t, registered[http.MethodGet+" /api/user/vpn/orders"], "用户侧我的订单接口不能被挤掉")
	assert.True(t, registered[http.MethodGet+" /api/user/vpn/key"], "用户侧密钥接口不能被挤掉")
	assert.False(t, registered[http.MethodPost+" /api/vpn/admin/orders"],
		"管理端订单视图必须只读，不能出现写路由")
}
