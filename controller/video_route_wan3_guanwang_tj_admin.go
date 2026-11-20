package controller

// 「万相官网 → 客美 tj」手动改派开关的管理端接口（2026-11-19）。
//
// 为什么单独一个文件：这条改派与 video_route_ecommerce_admin.go 里的规则七/八
// 没有任何关系 —— 源模型是 wan3.0-video-官网（万相官网渠道），
// 那批规则的源模型是促销族 seedance2.5-*，两者共用的只有「策略怎么落库」这套壳。
// 独立成文件能让「这条为什么这么写」的上下文集中在一处。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// wan3GuanwangTjSwitchRequest 是「官网 → 客美 tj」开关的请求体。
type wan3GuanwangTjSwitchRequest struct {
	// Enabled true = 官网模型的请求改派客美 tj；false = 留在官网。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false —— 少写一个字段
	// 会把一条正在跑的真实流量改派静默关掉（与 ecommerceSwitchRequest 同款理由）。
	Enabled *bool `json:"enabled"`
}

// SetWan3GuanwangTjAdmin 切换「万相官网 → 客美 tj」改派开关。
//
//   - 开启：wan3.0-video-官网 的请求改派 wan3.0-video-tj（渠道 32 客美 Kemeitv），
//     计费仍按客户请求的官网模型；
//   - 关闭（默认）：请求留在官网渠道，行为与加这个开关之前逐字节一致。
//
// 目标模型没有可用渠道、或请求画幅是 adaptive 时，中间件会**放行原请求**
// 留在官网链路 —— 开关只表达「想换货」，不保证每条请求都能换（见
// middleware/wan3_guanwang_tj_route.go）。
func SetWan3GuanwangTjAdmin(c *gin.Context) {
	setWan3GuanwangTjAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setWan3GuanwangTjAdmin 是唯一实现（依赖注入理由同 setEcommerceRouteAdmin：
// 审计与落库都是副作用，注入进来才能把成功路径完整测到）。
func setWan3GuanwangTjAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req wan3GuanwangTjSwitchRequest
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

	policy := seedanceroute_setting.WithWan3GuanwangTjEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update wan3 guanwang tj switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.wan3_guanwang_tj_switch", map[string]interface{}{
		"enabled":      *req.Enabled,
		"source_model": seedanceroute_setting.Wan3GuanwangModel,
		"target_model": seedanceroute_setting.Wan3TjTargetModel,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// appendWan3GuanwangTjState 把开关状态追加进管理端状态快照。
//
// 三个字段一起给：运营要能读出「开/关」「谁 → 谁」——
// 只给一个布尔值，卡片上就是一个没有主语的开关（与规则七/八同口径）。
func appendWan3GuanwangTjState(state gin.H) {
	state["wan3_guanwang_tj_enabled"] = seedanceroute_setting.IsWan3GuanwangTjEnabled()
	state["wan3_guanwang_tj_source_model"] = seedanceroute_setting.Wan3GuanwangModel
	state["wan3_guanwang_tj_target_model"] = seedanceroute_setting.Wan3TjTargetModel
}
