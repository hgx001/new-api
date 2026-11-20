package middleware

// 已下线模型的显式拦截。
//
// 背景（2026-11-20 运营口径）：seedance2.5-活动 停止售卖，客户手里还留着
// 这个模型名的调用方不在少数。这一类请求**不能静默处理**：
//
//   - 静默改派到替代型号 → 客户以为买的是活动价，实际按特惠链路出片，
//     属于计费口径上的欺骗（同样会破坏「按源模型计费」的台账口径）；
//   - 落到「无可用渠道」→ 客户只看到一句笼统的 503/404，不知道该换谁，
//     会反复重试、报障。
//
// 所以这里给一张「已下线模型 → 替代型号」的表，命中即在选渠道之前返回
// 400 + 明确文案。表的粒度是**逐模型**，不按前缀匹配：模型名是对外契约，
// 前缀规则容易把在售型号（seedance2.5-特惠）误伤。

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
)

// retiredModelReplacements 已下线模型 → 建议替代型号。
//
// 文案与替代型号是运营决定，不是技术推导：改这里的值等于改对客户的说辞，
// 必须与产品/运营口径一致。
var retiredModelReplacements = map[string]string{
	"seedance2.5-活动": "seedance2.5-特惠",
}

// RetiredModelMessage 返回该模型的下线提示文案。
//
// 第二个返回值是「是否已下线」，调用方据此决定放行还是拦截。
func RetiredModelMessage(modelName string) (string, bool) {
	replacement, retired := retiredModelReplacements[strings.TrimSpace(modelName)]
	if !retired {
		return "", false
	}
	return "模型已下线，请使用 " + replacement, true
}

// RejectRetiredModel 在读取 model 之后、任何改写之前拦掉已下线模型的请求。
//
// 必须挂在条件路由（Seedance25TeHuiRoute 等）**之前**：那些中间件会把
// 活动版改写成特惠链路，一旦被改写，这里看到的就是替代型号，
// 「已下线」的提示永远发不出去。
//
// body 用 UnmarshalBodyReusable 读：它会把原始 body 缓存进 context，
// 后续中间件（CaptureTaskRequestParams / Distribute）照常解析同一份内容。
// 解析失败一律放行 —— 参数格式错的请求交给后面的校验器报它该报的错，
// 不能在这里伪装成「模型已下线」。
func RejectRetiredModel() func(c *gin.Context) {
	return func(c *gin.Context) {
		var probe struct {
			Model string `json:"model"`
		}
		if err := common.UnmarshalBodyReusable(c, &probe); err != nil {
			c.Next()
			return
		}
		if message, retired := RetiredModelMessage(probe.Model); retired {
			abortWithOpenAiMessage(c, http.StatusBadRequest, message)
			return
		}
		c.Next()
	}
}
