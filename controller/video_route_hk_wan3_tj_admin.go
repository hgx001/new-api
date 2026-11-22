package controller

// 「hk-wan3 → 客美 tj」手动改派开关的管理端接口（2026-11-22）。
//
// 与「官网 → 客美 tj」（video_route_wan3_guanwang_tj_admin.go）同构：
// 都是万相商品的手动换货按钮，只是源模型不同（hk-wan3，渠道 17 有赞）。
// 独立成文件是为了让这条链路的上下文集中在近处。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// wan3HkWan3TjSwitchRequest 是「hk-wan3 → 客美 tj」开关的请求体。
type wan3HkWan3TjSwitchRequest struct {
	// Enabled true = 不带参考视频的 hk-wan3 请求改派客美 tj；false = 留在 hk-wan3。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false —— 少写一个字段
	// 会把一条正在跑的真实流量改派静默关掉。
	Enabled *bool `json:"enabled"`
}

// SetWan3HkWan3TjAdmin 切换「hk-wan3 → 客美 tj」改派开关。
//
//   - 开启：hk-wan3 的请求（不带参考视频）改派 wan3.0-video-tj（渠道 32 客美），
//     计费仍按客户请求的 hk-wan3；
//   - 关闭（默认）：请求留在 hk-wan3，行为与加这个开关之前逐字节一致。
//
// 带参考视频、auto/adaptive 画幅、时长越界、素材过不了客美校验、
// 目标模型没有可用渠道时，中间件会**放行原请求** —— 开关只表达「想换货」。
func SetWan3HkWan3TjAdmin(c *gin.Context) {
	setWan3HkWan3TjAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setWan3HkWan3TjAdmin 是唯一实现（依赖注入理由同 setWan3GuanwangTjAdmin）。
func setWan3HkWan3TjAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req wan3HkWan3TjSwitchRequest
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

	policy := seedanceroute_setting.WithWan3HkWan3TjEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update wan3 hk wan3 tj switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.wan3_hk_wan3_tj_switch", map[string]interface{}{
		"enabled":      *req.Enabled,
		"source_model": seedanceroute_setting.Wan3HkModel,
		"target_model": seedanceroute_setting.Wan3TjTargetModel,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// appendWan3HkWan3TjState 把开关状态追加进管理端状态快照。
func appendWan3HkWan3TjState(state gin.H) {
	state["wan3_hk_wan3_tj_enabled"] = seedanceroute_setting.IsWan3HkWan3TjEnabled()
	state["wan3_hk_wan3_tj_source_model"] = seedanceroute_setting.Wan3HkModel
	state["wan3_hk_wan3_tj_target_model"] = seedanceroute_setting.Wan3TjTargetModel
}
