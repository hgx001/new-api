package middleware

// 无条件别名：wan3.0-720p-特惠 → seedance2.5-特惠。
//
// 为什么单独一个中间件、且必须挂在 Seedance25TeHuiRoute **之前**：
//
//	1. 它是「模型名等价」而不是「条件改派」——不看时长/比例/首尾帧，
//	   也不受 seedanceroute 开关影响。开关只该影响优化策略，不该影响
//	   一个已经不再单独供货的模型能不能用。
//	2. 改名之后 seedance2.5-特惠 的规则仍然照常生效（短时长 → 心笔内链、
//	   超时长 → seedance-2-5、首尾帧 → wan3 直连），所以客户拿到的行为
//	   与直接调用 seedance2.5-特惠 完全一致。
//	3. 计费原点保留客户原始请求的 wan3.0-720p-特惠（两者 ModelPrice 相同，
//	   客户账单无变化；日志/任务归属仍能看出这是从旧模型名进来的）。
//
// 只改写**客户原始请求**的模型名：规则二/三把请求内部改派到
// wan3.0-720p-特惠 时，本中间件已经执行完，不会把内部改派再打回特惠链路
// （首尾帧仍然需要 wan3 直连，不能被别名覆盖）。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

// Wan3TehuiAliasRoute 把 wan3.0-720p-特惠 的请求无条件改名为 seedance2.5-特惠。
func Wan3TehuiAliasRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
			c.Next()
			return
		}
		// /remix 的模型由原任务决定，改名会破坏 remix 语义。
		if strings.HasSuffix(c.Request.URL.Path, "/remix") {
			c.Next()
			return
		}
		body, ok := readJSONRequestBody(c)
		if !ok {
			c.Next()
			return
		}
		modelName, _ := body["model"].(string)
		if strings.TrimSpace(modelName) != wan3TehuiModelName {
			c.Next()
			return
		}
		body["model"] = constant.Seedance25TeHuiModel
		if !applyRewrittenBody(c, body, wan3TehuiModelName) {
			c.Next()
			return
		}
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 无条件别名到 %s（计费仍按 %s）",
			wan3TehuiModelName, constant.Seedance25TeHuiModel, wan3TehuiModelName))
		c.Next()
	}
}
