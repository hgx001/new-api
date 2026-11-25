package controller

// 促销族「满血档」（≤15 秒 → 豆包标准版 seedance-2-0）的管理接口。
//
//	POST /api/video-route/policy/promo-short-full   {"enabled": true}
//
// 与「自营短档」是**三选一**关系（2026-11-25 老板口径）：
//
//	满血档 / 自营短档 / 都不勾 → 豆包 Fast（seedance-2-0-fast）
//
// 互斥在设置层（WithPromoShortFullEnabled / WithPromoShortOwnEnabled）保证：
// 勾一个自动关另一个，关一个不动另一个 —— 运营关掉一条往往只是想换一条。

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// promoShortFullSwitchRequest 是「启停满血档」的请求体。
type promoShortFullSwitchRequest struct {
	// Enabled 是否启用满血档。
	//
	// 用指针收：缺字段必须与「明确要关」区分开 —— 后者会让 ≤15 秒的请求
	// 悄悄回到豆包 Fast（两条链路的成本与出片效果不同），不能靠零值猜。
	Enabled *bool `json:"enabled"`
}

// SetPromoShortFullAdmin 启停促销族的满血档分支。
func SetPromoShortFullAdmin(c *gin.Context) {
	setPromoShortFullAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setPromoShortFullAdmin 是带注入的实体，供单测替换落库与审计。
func setPromoShortFullAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req promoShortFullSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid request body"})
		return
	}
	if req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "enabled is required"})
		return
	}

	policy := seedanceroute_setting.WithPromoShortFullEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update promo short full switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.promo_short_full_switch", map[string]interface{}{
		"enabled": *req.Enabled,
	})
	state := promoShortTiersAdminState()
	if c != nil {
		// 可用性只在写入回包里探一次（读快照不带渠道探针，与其它卡片一致）。
		state["promo_short_full_available"] = failoverChannelProbe(c, seedanceroute_setting.DefaultPromoShortFullModel)
	}
	common.ApiSuccess(c, state)
}

// promoShortFullAdminState 是满血档的状态片段（由 promoShortTiersAdminState 合并）。
func promoShortFullAdminState() gin.H {
	enabled := seedanceroute_setting.IsPromoShortFullEnabled()
	target := seedanceroute_setting.DefaultPromoShortFullModel

	note := ""
	if enabled {
		note = fmt.Sprintf("打开：≤%d 秒（不带参考音视频/首尾帧、720P 或未指定）的请求改派 %s（固定 720p）",
			seedanceroute_setting.PromoShortFullMaxSeconds, target)
	} else {
		note = "关闭：≤15 秒的请求走自营短档（若勾选）或豆包 Fast（seedance-2-0-fast）"
	}

	return gin.H{
		"promo_short_full_enabled":     enabled,
		"promo_short_full_model":       target,
		"promo_short_full_min_seconds": seedanceroute_setting.PromoShortFullMinSeconds,
		"promo_short_full_max_seconds": seedanceroute_setting.PromoShortFullMaxSeconds,
		"promo_short_full_note":        note,
	}
}

// appendPromoShortFullState 把满血档的状态合并进策略快照。
//
// 单独一个函数的理由同自营短档：字段独立、改动面小，
// 摊在 doubaoRouteAdminState 里会淹没核心状态。
func appendPromoShortFullState(state gin.H) {
	for key, value := range promoShortFullAdminState() {
		state[key] = value
	}
}

// promoShortTiersAdminState 合并「自营短档 + 满血档」的状态。
//
// 两个开关三选一互斥：打开一个会关掉另一个，所以任一接口的写入回包都必须
// 同时带上两边的字段 —— 只回自己那一半，旁边那张卡的开关会停在旧状态，
// 运营看到两个都亮着（实际只有一个生效），正是最该避免的静默失效。
func promoShortTiersAdminState() gin.H {
	state := promoShortOwnAdminState()
	for key, value := range promoShortFullAdminState() {
		state[key] = value
	}
	return state
}
