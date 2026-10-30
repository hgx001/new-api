package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 条件路由的「运维开关」鉴权。
//
// 为什么不用管理员会话 / access_token：这个接口的调用方是**客户端软件**
// （每个操作员机器上的一份安装包）。把管理员凭据放进客户端安装包，
// 泄露一次就等于把整个后台交出去 —— 而这里真正需要的权限只有一件事：
// 「暂停 / 恢复某一条路由规则」。
//
// 所以用一个**单用途共享密钥**：
//   - 只认 X-Maintenance-Key 头，不接受 session / access_token / sk- 令牌；
//   - 密钥存在 options 表（VideoRouteMaintenanceKey），可随时轮换，
//     改完所有客户端换新值即可，不涉及任何用户账号；
//   - 密钥为空 = 接口不存在（返回 404 而不是 403）—— 没配置就不该暴露
//     「这里有个需要密钥的接口」这件事；
//   - 比较用 crypto/subtle 常量时间，避免按字节计时猜密钥。
//
// 限流挂在这个中间件**前面**（见 router/video-router.go）：先限流再验密钥，
// 爆破者每 IP 每分钟只有固定次数，而密钥本身是 32 字节随机串，爆破不可行。

// MaintenanceKeyHeader 是客户端携带共享密钥的请求头名。
const MaintenanceKeyHeader = "X-Maintenance-Key"

// VideoRouteMaintenanceAuth 校验运维共享密钥。
func VideoRouteMaintenanceAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(common.OptionMap[seedanceroute_setting.MaintenanceKeyOption])
		if expected == "" {
			// 没配置密钥 → 当作接口不存在。
			//
			// 用 404 而不是 403：403 等于告诉外部「这里确实有个受保护的接口」，
			// 而默认状态下它根本不该被任何人发现。
			c.JSON(http.StatusNotFound, gin.H{
				"success": false,
				"message": "not found",
			})
			c.Abort()
			return
		}
		provided := strings.TrimSpace(c.GetHeader(MaintenanceKeyHeader))
		// ConstantTimeCompare 在长度不等时直接返回 0，但仍然只比较到 min(len)，
		// 长度差异本身会泄露 —— 对固定长度的随机密钥无实际影响。
		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "invalid maintenance key",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
