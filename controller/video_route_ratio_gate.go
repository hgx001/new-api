package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 后台「可选比例条件」勾选接口：POST /api/video-route/policy/ratio-gate
//
//   - {"enabled": true}              → 闸门 = 9:16（只有 9:16 且时长超阈值才改派）
//   - {"enabled": true, "ratio": ...} → 闸门 = 指定比例（归一后写入）
//   - {"enabled": false}             → 清空闸门（不限比例，恢复旧行为）
//
// 为什么单独一个接口而不是塞进 doubao-link：豆包链路开关表达的是
// 「这条路通不通」，勾选表达的是「哪些请求有资格走这条路」，
// 两件事的恢复语义也不同（开关到凌晨 2 点自动弹回，勾选不会）。
// 共用一个接口会让「关一下开关」顺带把条件抹掉。

// secondsRuleRatioGateRequest 是勾选请求体。
type secondsRuleRatioGateRequest struct {
	// Enabled 是否启用比例条件。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false —— 少写一个字段
	// 会把刚勾上的条件静默取消（与 sourceSwitchRequest 同款理由）。
	Enabled *bool `json:"enabled"`
	// Ratio 可选的比例值（如 "9:16"）。为空用默认值 9:16；
	// enabled=false 时忽略本字段。
	Ratio string `json:"ratio"`
}

// SetSecondsRuleRatioGateAdmin 设置规则一的「可选比例条件」（登录态）。
func SetSecondsRuleRatioGateAdmin(c *gin.Context) {
	setSecondsRuleRatioGateAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setSecondsRuleRatioGateAdmin 是唯一实现。
//
// save / audit 显式注入而不是写死：审计日志要落库，controller 单测环境
// 没有 DB；把两个副作用收进参数，成功路径也能被完整测到（校验拒绝时
// 一次都不写库、成功时状态回显），不必为了测试去起数据库。
func setSecondsRuleRatioGateAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req secondsRuleRatioGateRequest
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

	policy, err := seedanceroute_setting.WithSecondsRuleRatioGate(*req.Enabled, req.Ratio)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := save(policy); err != nil {
		common.SysLog("update seconds rule ratio gate failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.ratio_gate", map[string]interface{}{
		"enabled": *req.Enabled,
		"ratio":   policy.SecondsRuleRatioGate,
	})
	// 返回完整管理状态：前端拿它直接替换本地 policy，勾选框立即反映新值，
	// 不需要再发一次 GET。
	common.ApiSuccess(c, doubaoRouteAdminState())
}
