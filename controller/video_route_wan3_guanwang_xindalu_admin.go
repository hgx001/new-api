package controller

// 「万相官网 → wan3-新大陆」手动改派开关的管理端接口（2026-11-27）。
//
// 与 video_route_wan3_guanwang_tj_admin.go 同构：同一商品（wan3.0-video-官网）
// 的另一个换货目标。两个开关互斥，写入任一为 true 时另一个自动关闭。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// wan3GuanwangXindaluSwitchRequest 是「官网 → 新大陆」开关的请求体。
type wan3GuanwangXindaluSwitchRequest struct {
	// Enabled true = 官网模型的请求改派 wan3-新大陆；false = 留在官网。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false —— 少写一个字段
	// 会把一条正在跑的真实流量改派静默关掉。
	Enabled *bool `json:"enabled"`
}

// SetWan3GuanwangXindaluAdmin 切换「万相官网 → wan3-新大陆」改派开关。
func SetWan3GuanwangXindaluAdmin(c *gin.Context) {
	setWan3GuanwangXindaluAdmin(c, saveRoutePolicy, recordManageAudit)
}

func setWan3GuanwangXindaluAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req wan3GuanwangXindaluSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	if req.Enabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "enabled is required",
		})
		return
	}

	policy := seedanceroute_setting.WithWan3GuanwangXindaluEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update wan3 guanwang xindalu switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.wan3_guanwang_xindalu_switch", map[string]interface{}{
		"enabled":      *req.Enabled,
		"source_model": seedanceroute_setting.Wan3GuanwangModel,
		"target_model": seedanceroute_setting.Wan3XindaluTargetModel,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// appendWan3GuanwangXindaluState 把开关状态追加进管理端状态快照。
func appendWan3GuanwangXindaluState(state gin.H) {
	state["wan3_guanwang_xindalu_enabled"] = seedanceroute_setting.IsWan3GuanwangXindaluEnabled()
	state["wan3_guanwang_xindalu_source_model"] = seedanceroute_setting.Wan3GuanwangModel
	state["wan3_guanwang_xindalu_target_model"] = seedanceroute_setting.Wan3XindaluTargetModel
}
