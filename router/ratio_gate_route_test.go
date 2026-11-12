package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 后台「可选比例条件」勾选接口必须真的挂在管理端路由组里。
//
// 配置写入、接口逻辑、中间件判定三层各有测试，但注册那一行写错路径
// （比如挂到 /api/video-route/maintenance/ 或漏进 AdminAuth 组）
// 三层测试全都照绿，运营点勾选只会拿到 404。这里把接线锁住。
func TestRatioGateAdminRouteRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	require.NotPanics(t, func() { SetApiRouter(engine) })

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}

	assert.True(t,
		registered[http.MethodPost+" /api/video-route/policy/ratio-gate"],
		"可选比例条件勾选接口未注册")
	// 与豆包链路开关是两个独立动作，新接口不能把老接口挤掉。
	assert.True(t,
		registered[http.MethodPost+" /api/video-route/policy/doubao-link"],
		"豆包链路开关接口不能被新接口顶掉")
	// 勾选只通过 POST 生效，避免有人误加只读入口后以为查询就能改配置。
	assert.False(t,
		registered[http.MethodGet+" /api/video-route/policy/ratio-gate"],
		"不该有 GET 写入口")
}
