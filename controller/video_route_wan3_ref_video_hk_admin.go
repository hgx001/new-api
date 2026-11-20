package controller

// 「带参考视频的万相请求 → hk-wan3」开关的管理端接口（2026-11-20）。
//
// 为什么单独一个文件：这条路由与 wan3-guanwang-tj（官网换货）以及促销族那批
// 规则都不同族 —— 它的触发条件是**请求带 reference_video**，源模型是万相族
// 三条链路，目标是 hk-wan3（有赞 WAN3 官渠，参考视频不计费）。
// 独立成文件能让「这条为什么这么写」的上下文集中在一处。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// wan3RefVideoHkSwitchRequest 是「参考视频 → hk-wan3」开关的请求体。
type wan3RefVideoHkSwitchRequest struct {
	// Enabled true = 带参考视频的万相请求改派 hk-wan3；false = 留在原链路。
	//
	// 用指针收：不传（nil）必须报错。这个开关**默认是开的**，少写一个字段
	// 若被当成 false，会静默把一条正在跑的链路纠正关掉。
	Enabled *bool `json:"enabled"`
}

// SetWan3RefVideoHkAdmin 切换「参考视频 → hk-wan3」开关。
//
//   - 开启（默认）：wan3.0-video-官网 / -tj / -prime-tj 上带 reference_video
//     且生成时长 4–15 秒的请求改派 hk-wan3，按源模型计费、参考视频不并入；
//   - 关闭：这类请求留在原链路（官网丢素材、tj 多收参考视频秒数 —— 即现状）。
//
// 准入不满足（时长越界 / adaptive 画幅 / 带首尾帧 / 素材超限 / 目标无渠道）
// 的请求即使开关打开也留在原链路（见 middleware/wan3_ref_video_hk_route.go）。
func SetWan3RefVideoHkAdmin(c *gin.Context) {
	setWan3RefVideoHkAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setWan3RefVideoHkAdmin 是唯一实现（依赖注入理由同 setEcommerceRouteAdmin）。
func setWan3RefVideoHkAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req wan3RefVideoHkSwitchRequest
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

	policy := seedanceroute_setting.WithWan3RefVideoHkEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update wan3 ref video hk switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.wan3_ref_video_hk_switch", map[string]interface{}{
		"enabled":       *req.Enabled,
		"source_models": seedanceroute_setting.Wan3FamilySourceModels,
		"target_model":  seedanceroute_setting.Wan3RefVideoHkTargetModel,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// appendWan3RefVideoHkState 把开关状态追加进管理端状态快照。
//
// 为什么给到「默认值」这个字段：卡片的开关状态来自有效值（nil = 开），
// 而运营需要知道「我把它关掉」与「从没人配过」是两回事 ——
// 前者写进 option，后者靠缺省。两者当前行为一样，但含义不同。
func appendWan3RefVideoHkState(state gin.H) {
	policy := seedanceroute_setting.Get()
	state["wan3_ref_video_hk_enabled"] = seedanceroute_setting.IsWan3RefVideoHkEnabled()
	state["wan3_ref_video_hk_configured"] = policy.Wan3RefVideoHkEnabled != nil
	state["wan3_ref_video_hk_target_model"] = seedanceroute_setting.Wan3RefVideoHkTargetModel
	state["wan3_ref_video_hk_source_models"] = seedanceroute_setting.Wan3FamilySourceModels
	// 可路由时段的两个边界：由后端给值，前端不硬编码（两处各写一份必然漂）。
	state["wan3_ref_video_hk_min_seconds"] = seedanceroute_setting.Wan3RefVideoHkMinSeconds
	state["wan3_ref_video_hk_max_seconds"] = seedanceroute_setting.Wan3RefVideoHkMaxSeconds
}
