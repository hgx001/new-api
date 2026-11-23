package controller

// 促销族「自营短档」（≤15 秒 + 9:16 → sd-seedance-2.0fast）的管理接口。
//
//	POST /api/video-route/policy/promo-short-own   {"enabled": true}
//
// 为什么要单独一个开关、而不是复用规则六的四条分支：
//
//	规则六的四条分支各绑死一个目标（心笔内链 / 有赞标准版 / CapCut 满血 /
//	豆包 Fast），其中豆包 Fast 正是本分支关掉后的落点。把自营档塞进那四条里
//	只能二选一，而老板要的是「能随手切回来对比出片效果」，所以做成并列开关：
//	打开 → 9:16 的短请求走自营 sd-；关闭 → 回到豆包 Fast。
//
// 与规则六的互斥关系：本开关**不**自动关闭豆包 Fast —— 两者是「优先/兜底」
// 关系（自营档先判，接不住时豆包 Fast 自然接手），不是互斥的四选一。

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// promoShortOwnSwitchRequest 是「启停自营短档」的请求体。
type promoShortOwnSwitchRequest struct {
	// Enabled 是否启用自营短档。
	//
	// 用指针收：缺字段必须与「明确要关」区分开 —— 后者会让 9:16 的短请求
	// 悄悄回到豆包 Fast（两条链路的成本与出片效果不同），不能靠零值猜。
	Enabled *bool `json:"enabled"`
}

// SetPromoShortOwnAdmin 启停促销族的自营短档分支。
func SetPromoShortOwnAdmin(c *gin.Context) {
	setPromoShortOwnAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setPromoShortOwnAdmin 是带注入的实体，供单测替换落库与审计。
func setPromoShortOwnAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req promoShortOwnSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	if req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "enabled is required"})
		return
	}

	policy := seedanceroute_setting.WithPromoShortOwnEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update promo short own switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.promo_short_own_switch", map[string]interface{}{
		"enabled": *req.Enabled,
	})
	state := promoShortOwnAdminState()
	if c != nil {
		// 可用性只在写入回包里探一次（读快照不带渠道探针，与其它卡片一致）。
		state["promo_short_own_available"] = failoverChannelProbe(c, seedanceroute_setting.DefaultPromoShortOwnModel)
	}
	common.ApiSuccess(c, state)
}

// promoShortOwnAdminState 是自营短档的状态片段（由 doubaoRouteAdminState 合并）。
//
// 字段回答两个问题：开关打开了吗、打开后接哪一段请求。
func promoShortOwnAdminState() gin.H {
	enabled := seedanceroute_setting.IsPromoShortOwnEnabled()
	target := seedanceroute_setting.DefaultPromoShortOwnModel
	ratio := seedanceroute_setting.PromoShortOwnRatio

	note := ""
	if enabled {
		note = fmt.Sprintf("打开：≤15 秒且 %s（不带参考音视频/首尾帧、参考图 ≤%d 张）的请求改派 %s", ratio, seedanceroute_setting.PromoShortOwnMaxImages, target)
	} else {
		note = "关闭：≤15 秒且 " + ratio + " 的请求走豆包 Fast（seedance-2-0-fast）"
	}

	return gin.H{
		"promo_short_own_enabled":    enabled,
		"promo_short_own_model":      target,
		"promo_short_own_ratio":      ratio,
		"promo_short_own_max_images": seedanceroute_setting.PromoShortOwnMaxImages,
		"promo_short_own_note":       note,
	}
}

// appendPromoShortOwnState 把自营短档的状态合并进策略快照。
//
// 单独一个函数的理由同规则七/八/万相那几条：字段独立、改动面小，
// 摊在 doubaoRouteAdminState 里会淹没「规则一/五」那几行核心状态。
func appendPromoShortOwnState(state gin.H) {
	for key, value := range promoShortOwnAdminState() {
		state[key] = value
	}
}
