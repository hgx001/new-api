package controller

// 规则五的**备选备胎**（豆包不可用 → CapCut 2.5 Flash，480p 生成 + 超分 720P）
// 的管理接口。
//
//	POST /api/video-route/policy/failover/alt   启停备选备胎（可顺带改目标模型）
//
// 查询状态不单开接口：备选的状态挂在 GET /policy/failover 的快照里
// （failover_alt_* 字段）。理由是运营的决策是「豆包挂了以后走哪条」，
// 班车与备选是同一问题的两个答案，分成两个接口会让界面必须同时拉两次
// 才能画出完整的三选一（班车 / CapCut / 都不走）。
//
// ─────────────────── 为什么这个开关与规则五主开关分开 ───────────────────
//
//	规则五主开关 = 「源链路挂了要不要改派」。
//	这个开关     = 「改派到哪一条」（班车 ←→ CapCut）。
//
//	合成一个的话，运营想换退路就必须先把故障转移关掉再开 —— 中间有几秒钟
//	豆包挂了请求会直接失败。两个开关各自独立，切换退路是原子的。

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

// failoverAltSwitchRequest 是「启停备选备胎」的请求体。
type failoverAltSwitchRequest struct {
	// Enabled 是否启用备选备胎（CapCut 2.5 Flash）。
	//
	// 用指针收：缺字段必须与「明确要关」区分开 —— 后者会让线上的退路
	// 悄悄从 CapCut 变回班车（出片时长差一个数量级），不能靠零值猜。
	Enabled *bool `json:"enabled"`

	// Model 备选目标模型名（可选）。不传 = 保持现状；传空串 = 回到默认
	// （cp-seedance-2.5flash）。当前只认默认这一档，别的名字不会生效
	//（middleware 侧按名字判契约，配成别的名字回落班车）。
	Model *string `json:"model"`
}

// SetFailoverAltAdmin 启停规则五的备选备胎（并可选地改目标模型）。
//
// 允许「超分未配置」时打开开关：运营可能先配开关、再配 MPS。
// 但状态快照会明确回报 failover_alt_effective=false，
// 界面据此提示「开关已开但不会生效」——比直接拒绝更能反映真实状态，
// 也避免运营以为「按钮打不开 = 功能坏了」。
func SetFailoverAltAdmin(c *gin.Context) {
	setFailoverAltAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setFailoverAltAdmin 是带注入的实体，供单测替换落库与审计
// （与 setSecondsRuleRatioGateAdmin / setDoubaoLinkMaxTasksAdmin 同一写法）。
func setFailoverAltAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req failoverAltSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
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

	policy := seedanceroute_setting.WithFailoverAltEnabled(*req.Enabled)
	if req.Model != nil {
		// 必须用 Apply 版本：With* 系列都是「从全局快照 clone 再改」，
		// 直接叠加会把上面刚设好的开关值覆盖回旧值。
		policy = seedanceroute_setting.ApplyFailoverAltModel(policy, *req.Model)
	}

	if err := save(policy); err != nil {
		common.SysLog("update failover alt switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	auditParams := map[string]interface{}{
		"enabled": *req.Enabled,
	}
	if req.Model != nil {
		auditParams["model"] = *req.Model
	}
	audit(c, "video_route.failover_alt_switch", auditParams)
	common.ApiSuccess(c, failoverAdminState(c))
}

// failoverAltAdminState 是备选备胎的状态片段（由 failoverAdminState 合并）。
//
// 字段分三层，对应运营要回答的三个问题：
//
//	failover_alt_enabled             开关打开了吗
//	failover_alt_target_available    CapCut 那条路上有渠道吗
//	failover_alt_upscale_configured  超分链路配好了吗（没配就只能走班车）
//	failover_alt_effective           三个条件都满足 → 豆包挂了真的会走 CapCut
//
// 「开关开着但没生效」是最难排查的情况：日志里只有「跳过故障转移备选」，
// 界面必须能直接告诉运营是**哪一项**不满足，否则他只会反复点开关。
func failoverAltAdminState(c *gin.Context) gin.H {
	configured := strings.TrimSpace(seedanceroute_setting.Get().FailoverAltModel)
	target := seedanceroute_setting.FailoverAltTargetModel()
	enabled := seedanceroute_setting.IsFailoverAltEnabled()

	targetSupported := target == seedanceroute_setting.DefaultFailoverAltModel
	available := false
	if target != "" && c != nil {
		// 与源链路 / 班车共用同一个探针（见 failoverChannelProbe）。
		available = failoverChannelProbe(c, target)
	}
	upscaleReady := tencentmps_setting.IsConfigured()

	note := ""
	switch {
	case !enabled:
		note = "关闭：豆包不可用时仍走班车"
	case !targetSupported:
		note = "备选目标模型不是 " + seedanceroute_setting.DefaultFailoverAltModel + "，本链路不生效，仍走班车"
	case !upscaleReady:
		note = "超分链路未配置：按 720P 交付不了，仍走班车（去「腾讯 MPS 超分」页配置）"
	case !available:
		note = "CapCut 在当前分组下没有可用渠道，仍走班车"
	default:
		note = "打开：豆包不可用时改派 " + target + "（480p 生成 + 超分 720P 交付）"
	}

	return gin.H{
		"failover_alt_enabled":            enabled,
		"failover_alt_model":              configured,
		"failover_alt_target_model":       target,
		"failover_alt_target_supported":   targetSupported,
		"failover_alt_target_available":   available,
		"failover_alt_upscale_configured": upscaleReady,
		"failover_alt_effective":          enabled && targetSupported && upscaleReady && available,
		"failover_alt_note":               note,
	}
}
