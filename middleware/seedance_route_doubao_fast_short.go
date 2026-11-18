package middleware

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/gin-gonic/gin"
)

// 规则十：seedance-2-5 短时长（≤15 秒，含）→ 豆包 Fast。
//
// 为什么单独成规则、不并进规则六：
//
//	规则六的入口是 policy.IsSourceModel(modelName)，源模型来自 source_models
//	（促销族）。seedance-2-5 是规则一的**目标**，把它加进 source_models 会
//	连带激活规则一（目标即自己）、规则八（全量兜底）、规则七（电商）等 ——
//	一开全开，且 30 秒的促销族改派会被再改派一次。
//
//	所以本规则在 IsSourceModel 早退**之前**单独判定，只认一个源模型名。
//
// 为什么排在规则五（故障转移）之后：
//
//	规则五看的是「seedance-2-5 的源链路还能不能用」。源链路（豆包 ch20）
//	不可用时，规则五会先把请求改派到班车/备选；此时再改派 Fast 只会落到
//	同一个不可用渠道。所以故障转移优先，能出片优先于省成本。
//
// 为什么不复用规则六的 short_fast 开关：
//
//	两者的源模型与时长上界都不同（促销族 14 秒 / seedance-2-5 15 秒），
//	共用一个开关会让「给 seedance-2-5 打开」顺带把促销族也切走。
func tryDoubaoFastShortRoute(
	c *gin.Context,
	body map[string]interface{},
	billingOriginModel string,
) bool {
	modelName, _ := body["model"].(string)
	modelName = strings.TrimSpace(modelName)
	if !seedanceroute_setting.IsDoubaoFastShortEnabledFor(modelName) {
		return false
	}

	seconds, ok := extractRequestSeconds(body)
	if !ok {
		// 没传时长 = 按源链路缺省值（豆包 2.5 的 doubaoDefaultDuration = 5 秒）。
		// 但「缺省 5 秒」是链路实现细节，不是客户表达的时长 —— 不在路由层
		// 替客户做「这单算短任务」的决定，留在 seedance-2-5 上执行。
		logger.LogInfo(c, "条件路由跳过 seedance-2-5 短时长：请求未显式指定时长")
		return false
	}

	trigger := seedanceroute_setting.DoubaoFastShortTriggerSeconds()
	if seconds < seedanceroute_setting.DoubaoFastMinSeconds || seconds > trigger {
		// 正常流量（>15 秒，或 <4 秒目标链路接不住），不打日志避免刷屏。
		return false
	}

	target := seedanceroute_setting.DoubaoFastShortTarget()
	newBody, routed := prepareShortFastRoute(c, body, target, seconds)
	if !routed {
		return false
	}

	origin := strings.TrimSpace(billingOriginModel)
	if origin == "" {
		origin = modelName
	}
	if !applyRewrittenBody(c, newBody, origin) {
		return false
	}

	// 计费：客户买的是 seedance-2-5 的一口价（¥3.50/次），而目标 Fast 的
	// adaptor 会按秒返回倍率。不屏蔽就会 ¥3.50 × 15 = ¥52.50（超收 15 倍）。
	// 与规则五的 CapCut 备选同一口径，见 constant.ContextKeyRouteSuppressTaskRatios。
	common.SetContextKey(c, constant.ContextKeyRouteSuppressTaskRatios, true)

	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：命中 seedance-2-5 短时长 %d 秒（≤ %d 秒），改派 %s → %s（豆包 Fast，计费仍按 %s 一口价）",
		seconds, trigger, modelName, target, origin))
	return true
}
