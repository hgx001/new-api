package controller

// 规则七（电商/产品推荐内容 → CapCut 快速档）与规则六第三分支
// （≤10 秒 → CapCut 快速档）的管理端接口。
//
// 为什么单独一个文件：这两个功能是同一批需求（2026-11-14 老板要的
// 「电商内容路由」+「短时长路由到 cp-seedance-2.0fast」），
// 与既有 controller/video_route_maintenance.go 里的规则一~五互不重叠，
// 独立成文件能让「这条规则为什么这么写」的上下文集中在一处。

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// ══════════════════════════════════════════════════════════════════
// 规则七：电商/产品推荐内容 → cp-seedance-2.5flash
// ══════════════════════════════════════════════════════════════════

// ecommerceSwitchRequest 是规则七总开关的请求体。
type ecommerceSwitchRequest struct {
	// Enabled true = 规则七生效；false = 停用。
	//
	// 用指针收：不传（nil）必须报错，不能默认成 false ——
	// 少写一个字段会把「电商流量改派」静默关掉（与 shortYouzanSwitchRequest 同款理由）。
	Enabled *bool `json:"enabled"`
}

// SetEcommerceRouteAdmin 切换规则七的总开关（2026-11-14）。
//
//   - 开启：seedance2.5-特惠 / seedance2.5-活动 的请求，只要提示词命中
//     电商/产品推荐关键词（或勾选的参数条件命中），就改派 cp-seedance-2.5flash；
//   - 关闭：规则七停用，这类请求留在源链路。
//
// 带参考视频的请求**永远不路由**（老板明确要求），与开关无关。
func SetEcommerceRouteAdmin(c *gin.Context) {
	setEcommerceRouteAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setEcommerceRouteAdmin 是唯一实现。
//
// save / audit 显式注入而不是写死：审计日志要落库，controller 单测环境
// 没有 DB；把两个副作用收进参数，成功路径也能被完整测到（校验拒绝时
// 一次都不写库），不必为了测试去起数据库。与 setSecondsRuleRatioGateAdmin 同款做法。
func setEcommerceRouteAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req ecommerceSwitchRequest
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

	policy := seedanceroute_setting.WithEcommerceEnabled(*req.Enabled)
	if err := save(policy); err != nil {
		common.SysLog("update ecommerce route switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.ecommerce_switch", map[string]interface{}{
		"enabled": *req.Enabled,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// ecommerceConditionsRequest 是规则七参数条件的请求体。
type ecommerceConditionsRequest struct {
	// RatioEnabled 勾选「9:16 比例」入口。
	RatioEnabled *bool `json:"ratio_enabled"`
	// SecondsEnabled 勾选「时长超过阈值」入口。
	SecondsEnabled *bool `json:"seconds_enabled"`
	// TriggerSeconds 时长条件的阈值（秒）。**超过**该值命中。
	//
	// 0 = 沿用当前配置（不改阈值）。负数 = 非法（400）。
	TriggerSeconds *int `json:"trigger_seconds"`
}

// SetEcommerceConditionsAdmin 设置规则七的参数条件（后台勾选）。
//
// 语义：命中 = 内容命中（电商关键词）**或** 参数命中（勾选的项）。
// 参数入口是可选项 —— 全部不勾时规则七只按内容命中。
//
// ⚠️ 两个布尔都用指针收：nil = 「没传这个字段」= 不动它。
// 用 bool 的话前端漏传一个字段就会把它静默关掉 —— 那正是「配置项形同虚设」
// 那种最难排查的故障（后台看着勾着的，代码里是关的）。
func SetEcommerceConditionsAdmin(c *gin.Context) {
	setEcommerceConditionsAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setEcommerceConditionsAdmin 是唯一实现（依赖注入理由同 setEcommerceRouteAdmin）。
func setEcommerceConditionsAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req ecommerceConditionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}
	if req.RatioEnabled == nil || req.SecondsEnabled == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "ratio_enabled and seconds_enabled are required",
		})
		return
	}
	if req.TriggerSeconds != nil && *req.TriggerSeconds < 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "trigger_seconds must not be negative",
		})
		return
	}

	policy := seedanceroute_setting.Get()
	// 阈值：传了就用传的，没传就用当前生效值（避免「只改勾选却把阈值重置」）。
	triggerSeconds := policy.EcommerceTriggerSeconds()
	if req.TriggerSeconds != nil && *req.TriggerSeconds > 0 {
		triggerSeconds = *req.TriggerSeconds
	}

	updated, err := seedanceroute_setting.WithEcommerceConditions(
		*req.RatioEnabled, *req.SecondsEnabled, triggerSeconds)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	if err := save(updated); err != nil {
		common.SysLog("update ecommerce route conditions failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.ecommerce_conditions", map[string]interface{}{
		"ratio_enabled":   *req.RatioEnabled,
		"seconds_enabled": *req.SecondsEnabled,
		"trigger_seconds": triggerSeconds,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// ecommerceKeywordsRequest 是规则七关键词表的请求体。
type ecommerceKeywordsRequest struct {
	// Keywords 电商/产品推荐的关键词表。
	//
	// 空数组 = 回落默认表（DefaultEcommerceKeywords），
	// **不是**「关掉内容判定」—— 内容判定是这条规则的主入口，
	// 没有「只留参数条件」的表达方式。
	Keywords []string `json:"keywords"`
}

// SetEcommerceKeywordsAdmin 设置规则七的关键词表。
func SetEcommerceKeywordsAdmin(c *gin.Context) {
	setEcommerceKeywordsAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setEcommerceKeywordsAdmin 是唯一实现（依赖注入理由同 setEcommerceRouteAdmin）。
func setEcommerceKeywordsAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req ecommerceKeywordsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "invalid request body",
		})
		return
	}

	policy := seedanceroute_setting.WithEcommerceKeywords(req.Keywords)
	if err := save(policy); err != nil {
		common.SysLog("update ecommerce route keywords failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.ecommerce_keywords", map[string]interface{}{
		"count": len(policy.EcommercePromptKeywords),
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// ══════════════════════════════════════════════════════════════════
// 规则六第三分支：≤10 秒 → cp-seedance-2.0fast（CapCut 快速档）
// ══════════════════════════════════════════════════════════════════

// shortCapcutSwitchRequest 是规则六第三分支开关的请求体。
type shortCapcutSwitchRequest struct {
	// Enabled true = ≤阈值的短请求改派 cp-seedance-2.0fast；false = 不走这条。
	//
	// 用指针收：不传（nil）必须报错 —— 少写一个字段会把这条分支静默关掉。
	Enabled *bool `json:"enabled"`
}

// SetShortCapcutLinkAdmin 切换规则六第三分支（CapCut 快速档）开关。
//
// ⚠️ **三条短时长分支同时只能有一条生效**：心笔内链 / 有赞标准版 /
// CapCut 快速档。这三条是**同一个时长区间的三个出口** ——
// 同时开着两三条时，同一个短请求会按优先级落到其中一条，
// 但「落到哪一条」必须是**明确的经营意图**，而不是「谁写在配置里靠前」。
//
// 所以这里在**写入口**做互斥：打开本条时自动关掉另外两条。
// 关闭本条时**不动**另外两条 —— 运营关掉一条往往只是想换一条，不是想全停。
//
// 读侧还有一层兜底（见 seedanceroute_setting.ActiveShortRouteTarget）：
// 万一配置被 SQL 直接改过，也不会出现两条同时生效。
func SetShortCapcutLinkAdmin(c *gin.Context) {
	setShortCapcutLinkAdmin(c, saveRoutePolicy, recordManageAudit)
}

// setShortCapcutLinkAdmin 是唯一实现（依赖注入理由同 setEcommerceRouteAdmin）。
func setShortCapcutLinkAdmin(
	c *gin.Context,
	save func(seedanceroute_setting.Policy) error,
	audit func(*gin.Context, string, map[string]interface{}),
) {
	var req shortCapcutSwitchRequest
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

	policy := seedanceroute_setting.WithShortCapcutEnabled(*req.Enabled)
	if *req.Enabled {
		// 互斥：开本条时把另外两条显式关掉。
		//
		// ⚠️ 心笔那条必须写成**显式 false**（不是「不写」）：
		// short_link_enabled 是三态指针，nil = 默认开启 ——
		// 不写它的话，读侧兜底会认为心笔还开着，本条永远轮不到。
		policy = seedanceroute_setting.ApplyShortLinkEnabled(policy, false)
		policy = seedanceroute_setting.ApplyShortYouzanEnabled(policy, false)
	}
	if err := save(policy); err != nil {
		common.SysLog("update short capcut switch failed: " + err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{
			"success": false,
			"message": "failed to save route policy",
		})
		return
	}

	audit(c, "video_route.short_capcut_switch", map[string]interface{}{
		"enabled": *req.Enabled,
	})
	common.ApiSuccess(c, doubaoRouteAdminState())
}

// ══════════════════════════════════════════════════════════════════
// 状态输出
// ══════════════════════════════════════════════════════════════════

// appendEcommerceRouteState 把规则七与 CapCut 分支的状态字段追加到管理端状态里。
//
// 为什么要单独一个函数而不是直接写在 doubaoRouteAdminState 里：
// doubaoRouteAdminState 已经在 200 行上下（规则一~六的状态），
// 再往里塞两条新规则会让它继续膨胀；而这两条规则的状态字段是**一整套**
// （开关 + 条件 + 关键词 + 三路互斥的当前生效方），单独一个函数更好读。
//
// 字段命名与前端 api.ts 的 VideoRoutePolicy 一一对应。
func appendEcommerceRouteState(state gin.H) {
	policy := seedanceroute_setting.Get()

	// 规则七：总开关 + 条件 + 关键词。
	state["ecommerce_enabled"] = seedanceroute_setting.IsEcommerceRouteEnabled()
	// 目标模型：按**第一个源模型**取（多源时各源模型可以有各自的覆盖）。
	// 前端展示的是「默认目标」—— 逐源模型的覆盖在 source_switches 里另有其形。
	state["ecommerce_target_model"] = policy.EcommerceTargetModelFor(policy.PrimarySourceModel())
	state["ecommerce_ratio_condition"] = policy.EcommerceRatioEnabled()
	state["ecommerce_seconds_condition"] = policy.EcommerceSecondsEnabled()
	state["ecommerce_trigger_seconds"] = policy.EcommerceTriggerSeconds()
	state["ecommerce_ratio_condition_default"] = seedanceroute_setting.DefaultEcommerceRatioCondition
	// 关键词表：运营配了就给配的，没配就给默认表的**生效值**
	// （EcommerceKeywords() 会回落默认值）—— 前端要显示的是「现在判定用的那张表」。
	state["ecommerce_keywords"] = policy.EcommerceKeywords()
	state["ecommerce_keywords_default"] = seedanceroute_setting.DefaultEcommerceKeywords

	// 规则六第三分支：CapCut 快速档。
	state["short_capcut_enabled"] = seedanceroute_setting.IsShortCapcutEnabled()
	state["short_capcut_target_model"] = seedanceroute_setting.DefaultShortCapcutTargetModel

	// 三路互斥的**当前生效方**：运营要能看到「现在到底哪条分支在跑」。
	//
	// 空串 = 三条都没开 = 短请求走原链路。
	state["short_active_target"] = seedanceroute_setting.ActiveShortRouteTarget()
	state["short_active_targets"] = seedanceroute_setting.ShortRouteTargets()

	// 三条分支各自的开关状态（前端要能分别显示三个按钮的亮灭）。
	state["short_link_enabled"] = seedanceroute_setting.IsShortLinkEnabled()
	state["short_youzan_enabled"] = seedanceroute_setting.IsShortYouzanEnabled()
}
