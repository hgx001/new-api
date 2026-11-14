package controller

// 规则五（故障转移）的管理接口。
//
// 与本文件同目录的 video_route_maintenance.go 管的是规则一~四（暂停/恢复/
// 按源模型开关）。规则五是**另一件事**，接口也分开，理由有三条：
//
//  1. 语义不同。规则一~四的开关是「要不要把请求改派到更便宜的链路」，
//     规则五的开关是「源链路挂了要不要改派到备胎」。两者混在一个开关里，
//     运营关掉其中一个会连带另一个。
//  2. 触发条件不同。规则一~四由**请求特征**触发，规则五由**渠道状态**触发。
//     后者还需要一个独立的「人工标记源链路不可用」的动作 —— 那是规则一~四
//     没有的第四种操作。
//  3. 计费口径不同。规则五的改派**不改对客价格**（客户请求什么就按什么收），
//     这点必须在界面上说清楚，否则运营会以为改派会改变客户账单。
//
// 接口清单：
//
//	GET  /api/video-route/policy/failover              查询规则五状态
//	POST /api/video-route/policy/failover/switch       启停规则五
//	POST /api/video-route/policy/failover/models       配置源/目标模型
//	POST /api/video-route/policy/failover/unavailable  人工标记源链路不可用
//	POST /api/video-route/policy/failover/available    人工解除标记
//
// 「人工标记」与「自动标记」必须分开：自动标记来自额度探针（上游回一句
// 「额度已用完」），源链路一旦成功出片就自动解除；人工标记是运营判断
// 「上游今天一直挂着」，**不会**被一次成功自动解除。若不区分，运营的判断
// 会被一次偶发成功悄悄推翻，而且他根本不知道发生过这件事。

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// maxFailoverUnavailableDuration 单次人工标记的上限。
//
// 与规则一的暂停上限同口径（30 天）：防手滑把毫秒当秒传，
// 结果是源链路被标记成不可用几十年 —— 期间所有请求都跑在备胎链路上
// （慢 18 倍、成片规格不同），且没有任何报错。
const maxFailoverUnavailableDuration = 30 * 24 * time.Hour

// failoverChannelProbe 判定「某个模型在当前分组下有没有可用渠道」。
//
// 声明成变量只为测试可替换 —— 真实实现要查渠道表（model.GetRandomSatisfiedChannel），
// 单测没有 DB 环境，直接调会 nil panic。与 middleware 的
// channelAvailabilityProbe / channelIDsProbe 同一套做法：生产路径永远走默认值。
//
// 三个调用点（源链路 / 班车 / 备选 CapCut）共用同一个探针，避免
// 「有没有渠道」在不同接口里出现两套口径。
var failoverChannelProbe = middleware.HasAvailableChannel

// failoverSwitchRequest 是「启停规则五」的请求体。
type failoverSwitchRequest struct {
	// Enabled 是否启用故障转移。用指针收：不传 = 不改（缺字段与「明确要关」
	// 后果完全不同，前者会让运营以为自己动过配置）。
	Enabled *bool `json:"enabled"`
}

// SetFailoverSwitchAdmin 启停规则五（故障转移）。
func SetFailoverSwitchAdmin(c *gin.Context) {
	var req failoverSwitchRequest
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

	policy, err := seedanceroute_setting.WithFailoverEnabled(*req.Enabled)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := saveRoutePolicy(policy); err != nil {
		common.SysLog("update failover switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.failover_switch", map[string]interface{}{
		"enabled": *req.Enabled,
	})
	common.ApiSuccess(c, failoverAdminState(c))
}

// failoverModelsRequest 是「配置规则五源/目标模型」的请求体。
type failoverModelsRequest struct {
	// SourceModel 源模型名（会触发故障转移的那个），如 seedance-2-5。
	SourceModel string `json:"source_model"`
	// TargetModel 备胎模型名（改派给谁），如 wan3.0-班车。
	TargetModel string `json:"target_model"`
}

// SetFailoverModelsAdmin 配置规则五的源/目标模型。
//
// 为什么不与启停合并成一个接口：模型名是**一次性配置**（很少改），
// 开关是**日常操作**（上游一挂就关、恢复就开）。合成一个接口的话，
// 运营只想关开关也必须把两个模型名再填一遍 —— 填错就把故障转移指到了
// 别的模型上，而那正是最危险的误操作。
func SetFailoverModelsAdmin(c *gin.Context) {
	var req failoverModelsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}

	policy, err := seedanceroute_setting.WithFailoverModels(req.SourceModel, req.TargetModel)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := saveRoutePolicy(policy); err != nil {
		common.SysLog("update failover models failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.failover_models", map[string]interface{}{
		"source_model": req.SourceModel,
		"target_model": req.TargetModel,
	})
	common.ApiSuccess(c, failoverAdminState(c))
}

// failoverUnavailableRequest 是「人工标记源链路不可用」的请求体。
type failoverUnavailableRequest struct {
	// Until 标记截止时间（Unix 秒）。0 或缺省 = 北京时间凌晨 2 点
	//（上游日额度的真实重置点，对应「今天额度用完了」这个最常见的场景）。
	Until int64 `json:"until"`
	// Reason 原因文案（给运营/客户看）。缺省用「上游额度已用完」。
	Reason string `json:"reason"`
}

// MarkFailoverSourceUnavailableAdmin 人工把源链路标记为不可用。
//
// 为什么需要人工标记：上游有时不回报「额度用完」这句话，只是直接失败
// （或失败文案是别的样子，额度探针认不出来）。这时只能由人点一下按钮。
//
// 幂等：重复调用只是把截止时间往后推，不会出错。
func MarkFailoverSourceUnavailableAdmin(c *gin.Context) {
	var req failoverUnavailableRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}

	until, err := parseFailoverUntil(req.Until, time.Now())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		reason = seedanceroute_setting.ReasonQuotaExhausted
	}

	// 用 WithManualFailoverUnavailable 而不是 WithFailoverUnavailable：
	// 要把来源标成 manual，额度探针才不会替管理员解除这个标记。
	if err := saveRoutePolicy(seedanceroute_setting.WithManualFailoverUnavailable(until, reason)); err != nil {
		common.SysLog("mark failover source unavailable failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.failover_unavailable", map[string]interface{}{
		"until":  until,
		"reason": reason,
	})
	common.ApiSuccess(c, failoverAdminState(c))
}

// ClearFailoverSourceUnavailableAdmin 人工解除「源链路不可用」标记。
//
// 只清标记，**不动开关**：解除标记 = 「源链路好了，可以照常打过去」；
// 关掉故障转移 = 「即使源链路坏了也不改派」。两者后果完全不同。
func ClearFailoverSourceUnavailableAdmin(c *gin.Context) {
	if err := saveRoutePolicy(seedanceroute_setting.WithManualFailoverUnavailable(0, "")); err != nil {
		common.SysLog("clear failover unavailable failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	recordManageAudit(c, "video_route.failover_available", map[string]interface{}{})
	common.ApiSuccess(c, failoverAdminState(c))
}

// parseFailoverUntil 解析人工标记的截止时间。
//
// 与规则一的 parsePauseUntil 同口径：
//   - 0/缺省 = 北京时间凌晨 2 点（上游日额度的真实重置点）；
//   - 过去的时间戳必须拒绝（静默接受会得到「看起来成功但完全没标记」的结果）；
//   - 超过 30 天必须拒绝（把毫秒当秒传会把源链路标记成不可用几十年）。
func parseFailoverUntil(until int64, now time.Time) (int64, error) {
	if until <= 0 {
		until = nextBeijingQuotaReset(now).Unix()
	}
	if until <= now.Unix() {
		return 0, errors.New("until must be in the future")
	}
	if time.Duration(until-now.Unix())*time.Second > maxFailoverUnavailableDuration {
		return 0, errors.New("until is too far in the future (max 30 days)")
	}
	return until, nil
}

// GetFailoverPolicyAdmin 查询规则五（故障转移）的完整状态。
func GetFailoverPolicyAdmin(c *gin.Context) {
	common.ApiSuccess(c, failoverAdminState(c))
}

// failoverAdminState 是规则五的状态快照。
//
// 字段设计围绕一个核心区分：**「规则五开着吗」与「源链路现在好不好使」
// 是两件事**，界面必须分开呈现。
//
//	failover_enabled            规则五的开关（要不要改派）
//	failover_source_available   源链路当前有没有渠道（静态可用性）
//	failover_unavailable        源链路当前是否被判定为不可用（含动态标记）
//	failover_unavailable_source 不可用标记的来源：auto / manual / ""
//
// 「源链路不可用」= 静态不可用（没渠道了）**或** 动态标记（额度探针/人工）。
// 静态不可用不需要状态位 —— 渠道一恢复就自动恢复。
//
// 之所以要单独给出 failover_source_available：运营需要区分
// 「渠道被人关了」（去渠道页开回来）与「额度用完了」（等凌晨 2 点重置）。
// 只给一个「不可用」的话，两种原因要靠猜。
func failoverAdminState(c *gin.Context) gin.H {
	policy := seedanceroute_setting.Get()
	sourceModel := strings.TrimSpace(policy.FailoverSourceModel)
	targetModel := strings.TrimSpace(policy.FailoverTargetModel)

	sourceAvailable := false
	if sourceModel != "" {
		sourceAvailable = failoverChannelProbe(c, sourceModel)
	}
	targetAvailable := false
	if targetModel != "" {
		targetAvailable = failoverChannelProbe(c, targetModel)
	}

	until := seedanceroute_setting.FailoverUnavailableUntil()
	marked := seedanceroute_setting.FailoverUnavailable(time.Now().Unix())
	reason := seedanceroute_setting.FailoverReason()
	markSource := seedanceroute_setting.FailoverUnavailableSource()

	// 「源链路不可用」的最终结论 = 静态（没渠道）或动态（被标记过且未过期）。
	unavailable := !sourceAvailable || marked
	unavailableReason := reason
	if !sourceAvailable {
		// 静态原因优先显示：渠道被关是更根本的事实，运营要先去看渠道。
		unavailableReason = "源链路在当前分组下没有启用中的渠道"
	} else if marked && unavailableReason == "" {
		unavailableReason = "上游额度已用完"
	}

	state := gin.H{
		"failover_enabled":                seedanceroute_setting.IsFailoverRouteEnabled(),
		"failover_configured":             seedanceroute_setting.FailoverConfigured(),
		"failover_source_model":           sourceModel,
		"failover_target_model":           targetModel,
		"failover_source_available":       sourceAvailable,
		"failover_target_available":       targetAvailable,
		"failover_unavailable":            unavailable,
		"failover_unavailable_reason":     unavailableReason,
		"failover_unavailable_until":      until,
		"failover_unavailable_source":     markSource,
		"failover_unavailable_until_text": "",
	}
	if until > 0 {
		state["failover_unavailable_until_text"] = time.Unix(until, 0).In(beijingZone).Format("2006-01-02 15:04:05")
	}

	// 备选备胎（CapCut 2.5 Flash）挂在同一份快照里，见
	// video_route_failover_alt_admin.go 的说明（班车与备选是同一问题的两个答案）。
	for key, value := range failoverAltAdminState(c) {
		state[key] = value
	}
	return state
}
