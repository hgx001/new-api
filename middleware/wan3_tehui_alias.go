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
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
)

// wan3TehuiAllowedRatio 是特惠档**唯一**支持的画幅（老板 2026-11-17 拍板）。
//
// 为什么锁死 9:16：特惠档的交付链路是「480P 生成 → 云端超分 → 交付 720P」，
// 超分输出按**长边/短边**给尺寸 —— 同模型名不同比例会拿到完全不同的像素：
// 9:16 的 480P 原片（540×960）→ 720×1280；16:9 的原片（854×480）→ 1280×720。
// 对外却只有一个「720P」标签，客户无法分辨自己拿到的是竖屏还是横屏。
// 生产实况也印证了这个档位的真实场景就是竖屏带货（渠道 12 的历史 21/21 单全是 9:16）。
const wan3TehuiAllowedRatio = "9:16"

// resolveTehuiRatioGateRatio 提取请求里的画幅比例，供特惠档闸门判定。
//
// 与 resolveRequestRatio 的区别：后者只认**具体比例**（"9:16"），
// adaptive / auto 这类档位名会被当成「没传」。但对闸门来说，
// adaptive 是**客户显式传了一个非 9:16 的画幅诉求**，必须拒单 ——
// 否则 adaptive 会被下游解析成 16:9 或自适应画幅，等于绕过了闸门。
//
// 返回值语义与 resolveRequestRatio 一致：hasRatio=false 表示客户没传比例。
func resolveTehuiRatioGateRatio(body map[string]interface{}) (string, bool) {
	if ratio, ok := resolveRequestRatio(body); ok {
		return ratio, true
	}
	// 没解析出具体比例，但可能传了 adaptive / auto 这类档位名。
	for _, key := range []string{"ratio", "aspect_ratio"} {
		if value, ok := body[key].(string); ok {
			if isAdaptiveRatioText(value) {
				return strings.ToLower(strings.TrimSpace(value)), true
			}
		}
	}
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := metadata[key].(string); ok {
				if isAdaptiveRatioText(value) {
					return strings.ToLower(strings.TrimSpace(value)), true
				}
			}
		}
	}
	return "", false
}

// isAdaptiveRatioText 判断是不是 adaptive / auto 这类「自适应」档位名。
func isAdaptiveRatioText(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "adaptive", "auto":
		return true
	default:
		return false
	}
}

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
		// ── 特惠档的比例闸门（2026-11-17 老板口径：只做 9:16）──────────────
		//
		// ⚠️ 闸门必须在这里（别名中间件），**不能**放在 relay 的 wan3 adaptor：
		// 生产上 wan3.0-720p-特惠 是无条件别名 → seedance2.5-特惠，
		// 请求根本不会进入 wan3 adaptor（先被改名，再被规则八兜底到
		// cp-seedance-2.5flash）。2026-11-17 实测：闸门放在 adaptor 里时，
		// 16:9 请求照样 200 建单并落到 channel 20 —— 形同虚设。
		//
		// 为什么拒单而不是静默改比例：客户传 16:9 是有明确诉求的，
		// 静默改成 9:16 属于「钱按 16:9 付、片是 9:16」的静默降级。
		// 宁可 400 说清楚，也不要交付错画幅。
		//
		// 客户**没传**比例时放行：默认画幅交给链路自己定，不凭空拒单。
		if ratio, hasRatio := resolveTehuiRatioGateRatio(body); hasRatio && ratio != wan3TehuiAllowedRatio {
			logger.LogWarn(c, fmt.Sprintf(
				"条件路由：%s 只支持 %s 画幅，当前请求 %s，已拒单",
				wan3TehuiModelName, wan3TehuiAllowedRatio, ratio))
			c.JSON(http.StatusBadRequest, gin.H{
				"code":    "invalid_parameter",
				"message": fmt.Sprintf("%s 只支持 %s 画幅，当前请求为 %s；请改传 %s",
					wan3TehuiModelName, wan3TehuiAllowedRatio, ratio, wan3TehuiAllowedRatio),
				"data":    nil,
			})
			c.Abort()
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
