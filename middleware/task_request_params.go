package middleware

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// CaptureTaskRequestParams 在条件路由改写请求体之前，把客户原始请求参数摘要挂到 Context。
//
// 为什么需要前置捕获：Seedance25TeHuiRoute / Wan3TehuiXinbiRoute 会把 body 的
// model 换成内部目标模型（甚至改写 seconds / resolution）。事后再从 body 反推，
// 记下来的就是「客户从没请求过的参数」，排查 422/502 时反而误导。
//
// 失败一律放行：解析不出来就不记参数，绝不挡客户请求。
// 读取方是消费日志（service.LogTaskConsumption）与错误日志（controller.processChannelError）。
func CaptureTaskRequestParams() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request == nil || c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.Next()
			return
		}
		raw, err := storage.Bytes()
		if err != nil || len(raw) == 0 {
			c.Next()
			return
		}
		var req relaycommon.TaskSubmitReq
		if err := common.Unmarshal(raw, &req); err != nil {
			c.Next()
			return
		}
		relaycommon.SetTaskRequestParams(c, relaycommon.BuildTaskRequestParamsSummary(&req))
		c.Next()
	}
}
