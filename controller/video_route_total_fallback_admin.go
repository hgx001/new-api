package controller

// 规则八（全量兜底 → cp-seedance-2.5）的管理端接口。
//
// 为什么单独一个文件：这条规则解决的问题与前七条**不是一类** ——
// 前七条是「怎么调度更省 / 更适配」，这条是「源链路彻底不可用时的逃生口」。
// 它的使用者是在救火，界面必须一眼看懂「现在全站请求都跑在 CapCut 上」，
// 而不是埋在电商卡/短时长卡的某个勾选里。
//
// 独立成文件还能让「为什么它不依赖规则总开关」这类反直觉设计
// 与实现集中在一处（理由见 setting/seedanceroute_setting/total_fallback.go）。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// ══════════════════════════════════════════════════════════════════
// 规则八：全量兜底 → cp-seedance-2.5
// ══════════════════════════════════════════════════════════════════

// totalFallbackSwitchRequest 是规则八开关的请求体。
type totalFallbackSwitchRequest struct {
	// Enabled true = seedance2.5-特惠 / seedance2.5-活动 的所有请求
	// 无条件改派 cp-seedance-2.5；false = 规则八不参与调度。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false ——
	// 少写一个字段就把应急开关静默关掉，而这种开关「关了」的表现是
	// 「请求继续打到挂掉的源链路」，与「开关没生效」完全无法区分。
	Enabled *bool `json:"enabled"`
}

// SetTotalFallbackRouteAdmin 切换规则八（全量兜底）开关。
//
// 语义：打开后**不看任何请求条件**（时长/比例/提示词/首尾帧），
// 这两个模型名收到的请求全部改派 cp-seedance-2.5。
//
// ⚠️ 唯一例外是目标链路的**物理能力边界**（1080P / 2–3 秒 /
// 带参考视频音频 / 自适应比例带参考图）—— 那几类改派过去必 400，
// 会留在原链路并打日志（理由见 setting 包 total_fallback.go 包头注释）。
func SetTotalFallbackRouteAdmin(c *gin.Context) {
	setTotalFallbackRouteAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setTotalFallbackRouteAdmin 是唯一实现。
//
// save / audit 显式注入而不是写死：审计日志要落库，controller 单测环境
// 没有 DB；把两个副作用收进参数，成功路径也能被完整测到（校验拒绝时
// 一次都不写库），与 setEcommerceRouteAdmin 同款做法。
func setTotalFallbackRouteAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req totalFallbackSwitchRequest
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

	policy := seedanceroute_setting.WithTotalFallbackEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update total fallback route switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.total_fallback_switch", map[string]interface{}{
		"enabled":      *req.Enabled,
		"target_model": seedanceroute_setting.TotalFallbackTargetModel(),
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// appendTotalFallbackState 把规则八的状态字段追加到管理端状态里。
//
// 字段命名与前端 api.ts 的 VideoRoutePolicy 一一对应。
//
// 字段分两类：开关 + **只读的分档口径**。后者没有阈值输入框、没有关键词、
// 没有条件勾选 —— 前端只拿它们渲染说明文字，运营点击不会改变这些值。
// 2026-11-18 起目标按秒数分档，前端必须知道短档目标/秒数阈值/参考图上限，
// 否则界面上那句「全部到 2.5flash」就是错的。
func appendTotalFallbackState(state gin.H) {
	// 用 IsTotalFallbackEnabled（= 开关打开**且**配了源模型）而不是直接读
	// policy.TotalFallbackEnabled：界面显示的必须是「实际会不会生效」。
	// 只读字段的话，「开着但一个源模型都没配」会显示成已启用，
	// 而中间件一个请求都不会改派 —— 那种状态没有任何报错。
	state["total_fallback_enabled"] = seedanceroute_setting.IsTotalFallbackEnabled()
	state["total_fallback_target_model"] = seedanceroute_setting.TotalFallbackTargetModel()
	// 短档口径（2026-11-18）：前端描述从后端拿，不在 TS 里另写一份「15」「9」。
	state["total_fallback_short_target_model"] = seedanceroute_setting.DefaultTotalFallbackShortTargetModel
	state["total_fallback_short_max_seconds"] = seedanceroute_setting.TotalFallbackShortMaxSeconds
	state["total_fallback_short_max_reference_images"] = seedanceroute_setting.TotalFallbackShortMaxReferenceImages
}
