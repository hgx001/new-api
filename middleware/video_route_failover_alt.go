package middleware

// 规则五的**备选备胎**：豆包链路不可用时改派 CapCut Seedance 2.5 Flash，
// 按 480p 生成、交付前超分到 720P（2026-11-14 加）。
//
// ───────────────────────── 为什么需要第二个备胎 ─────────────────────────
//
//	规则五原本只有一条退路：wan3.0-班车（同上游的「视频生成车站」队列）。
//	豆包额度耗尽时，班车能接住，但排队时长不可控；CapCut 是**另一家上游**
//	（我们自己的账号池），豆包挂掉时它是一条完全独立的路。
//
//	所以运营需要一个开关来选择「豆包挂了以后走哪条」：
//	  打开：cp-seedance-2.5flash（480p 生成 + 超分交付 720P）
//	  关闭：wan3.0-班车（现状，默认）
//
// ───────────────────────── 为什么必须单独一套改写 ─────────────────────────
//
//	班车与 CapCut 的入参契约完全不同，不能共用 rewriteBodyForFailover：
//
//	          班车（yuanmeng）              CapCut（manwu）
//	  比例    metadata.ratio（含 adaptive）  顶层 ratio（**没有 auto**，6 档）
//	  分辨率  顶层 resolution 三档           顶层 resolution（只卖 480p/720p）
//	  素材    media[].type=reference_image  顶层 images[]（或 media，但只收图片）
//	  时长    字符串 seconds                 字符串 seconds（4–30 秒）
//
//	按班车契约改写后发给 CapCut，比例会被当非法值 400（metadata.ratio 它读，
//	但 adaptive 不在白名单），素材字段也要重排 —— 「改派过去跑不起来」
//	正是规则五要防的最坏结果。
//
// ───────────────────────── 交付档位：480p 生成 → 720P 交付 ─────────────────────────
//
//	CapCut 的 480p 与 720p 是两个价（720p 是 480p 的 2 倍，见
//	manwu.capcut720PRatios），而客户买的是 seedance-2-5 的一口价（¥3.50/次）。
//	生成 480p 再把成片超分到 720P 交付，成本最低且交付档位与对外承诺一致。
//
//	超分复用规则六心笔内链那套腾讯 MPS 链路（relay/channel/task/wan3 的
//	SubmitSuperResolution）—— 超分与上游是谁无关，只要有一条可抓取的 480p 成片。
//
//	⚠️ MPS 没配就**不能用这条备选链路**：那会「按 720P 的价交付 480P 的片」，
//	是静默降质（任务成功、账单照收、客户拿到低一档的成片）。
//	这种情况回落班车 —— 至少班车交付的是它承诺的档位。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

// capcutAltGenerationResolution 是备选链路向上游请求的生成档位（小写，CapCut 口径）。
//
// 与交付档位分开：交付恒 720P（超分后），生成档位只影响成本。
const capcutAltGenerationResolution = "480p"

// capcutAltMinSeconds / capcutAltMaxSeconds 是 CapCut 2.5 Flash 的时长区间。
//
// 与 relay/channel/task/manwu 的 capcutMinDuration / capcutMaxDuration25 同口径。
// 规则五本来就有自己的改派白名单（failoverAllowedDurations），这里是**目标链路**
// 的硬边界：目标接不住的时长不改派，否则等于把能跑的请求改成跑不了的。
const (
	capcutAltMinSeconds = 4
	capcutAltMaxSeconds = 30
)

// capcutAltRatios 是 CapCut 支持的比例档位。
//
// ⚠️ **没有 auto/adaptive**：官网面板没有「自动」档，CapCut adaptor 的
// 白名单就是这 6 个（见 manwu.allowedRatios）。豆包请求的「自动」档
// （不传比例 / auto）在 CapCut 侧无法表达 —— 这种请求不改派，回落班车
// （班车有 adaptive，语义能对上）。
var capcutAltRatios = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// superResolutionConfiguredProbe 判定「超分链路是否可用」。
//
// 抽成变量是为了可测：真实实现读全局配置，单测替换它就能构造
// 「MPS 没配」的世界（那是必须回落班车的分支）。
var superResolutionConfiguredProbe = tencentmps_setting.IsConfigured

// failoverAltTarget 返回备选备胎的目标模型名；第二个返回值 = 当前是否启用。
//
// 只认 CapCut 2.5 Flash 一个名字：它的改写契约与别的模型都不同，
// 配成别的名字时本规则不生效（由调用方回落班车），不做猜测。
func failoverAltTarget(c *gin.Context) (string, bool) {
	if !seedanceroute_setting.IsFailoverAltEnabled() {
		return "", false
	}
	target := seedanceroute_setting.FailoverAltTargetModel()
	if !isCapcutAltTarget(target) {
		if strings.TrimSpace(target) != "" {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过故障转移备选：备选目标 %q 不是 CapCut 2.5 Flash，按班车处理", target))
		}
		return "", false
	}
	return strings.TrimSpace(target), true
}

// isCapcutAltTarget 判断目标模型是不是备选链路支持的那一档。
func isCapcutAltTarget(target string) bool {
	return strings.TrimSpace(target) == seedanceroute_setting.DefaultFailoverAltModel
}

// tryFailoverAltRoute 尝试把请求改派到 CapCut 备选备胎。
//
// 返回 true = 已改派（计费口径仍是 billingOriginModel，客户无感）。
// 返回 false = 这条备选接不住，调用方**必须继续走班车** ——
// 备选是「更好的退路」，不是「唯一退路」，接不住时不能把请求留在已挂的源链路上。
//
// body 既可以是客户原始请求，也可以是规则一改派到豆包后的扁平形态：
// 两者用规则五同一套读取口径（failoverSourceImages/Ratio/Seconds），
// 因为改写后图片在 input_reference、比例在顶层 ratio、时长在顶层 seconds，
// 都命中那套优先级列表的第一个非空字段。
func tryFailoverAltRoute(
	c *gin.Context,
	body map[string]interface{},
	sourceModel string,
	billingOriginModel string,
) bool {
	target, enabled := failoverAltTarget(c)
	if !enabled {
		return false
	}
	// 超分没配 → 交付不出 720P，不能用这条链路（否则是静默降质）。
	if !superResolutionConfiguredProbe() {
		logger.LogInfo(c, "条件路由跳过故障转移备选：超分链路未配置，回落班车（避免按 720P 价交付 480P）")
		return false
	}
	// 源与备选落在同一批渠道时改名无路由收益（与班车那条同一判定）。
	if failoverSharedChannelChain(c, sourceModel, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：源 %s 与备选 %s 解析到同一批渠道，改名无路由收益",
			sourceModel, target))
		return false
	}

	newBody, ok := prepareCapcutAltRoute(c, body, target)
	if !ok {
		// 具体原因已由 prepareCapcutAltRoute 打进日志。
		return false
	}
	if !applyRewrittenBody(c, newBody, billingOriginModel) {
		return false
	}
	// 计费：客户买的是 seedance-2-5 的一口价，而 CapCut adaptor 会按秒给倍率 ——
	// 不标记就会 ¥3.50×N（见 constant.ContextKeyRouteSuppressTaskRatios）。
	common.SetContextKey(c, constant.ContextKeyRouteSuppressTaskRatios, true)
	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：豆包链路不可用，改派 %s → %s（480p 生成 + 超分 720P 交付，计费仍按 %s）",
		sourceModel, target, billingOriginModel))
	return true
}

// prepareCapcutAltRoute 校验并组装改派到 CapCut 的请求体。
//
// 任何一项不满足都返回 false 并留下可读原因 —— 运营要能回答
// 「为什么豆包挂了、开关也开着，这一单还是走了班车」。
func prepareCapcutAltRoute(
	c *gin.Context,
	body map[string]interface{},
	target string,
) (map[string]interface{}, bool) {
	// 备选链路自己没有可用渠道 → 不能改派。
	if !channelAvailabilityProbe(c, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：%s 在当前分组下没有可用渠道", target))
		return nil, false
	}

	// 提示词：与班车同口径（5–20000 **字节**，CapCut 侧也是同一量级上限）。
	prompt, _ := body["prompt"].(string)
	promptLength := len(strings.TrimSpace(prompt))
	if promptLength < failoverMinPromptLength || promptLength > failoverMaxPromptLength {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：提示词 %d 字节超出范围 %d-%d 字节",
			promptLength, failoverMinPromptLength, failoverMaxPromptLength))
		return nil, false
	}

	// 时长：先过规则五的改派白名单（限制改派面），再过 CapCut 的硬边界。
	seconds, secondsOK := failoverSourceSeconds(body)
	if !secondsOK {
		logger.LogInfo(c, "条件路由跳过故障转移备选：时长不是合法整数，源链路会直接报错")
		return nil, false
	}
	if !failoverAllowedDurations[seconds] {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：时长 %d 秒不在允许的档位内", seconds))
		return nil, false
	}
	if seconds < capcutAltMinSeconds || seconds > capcutAltMaxSeconds {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：时长 %d 秒超出 %s 的范围 %d-%d 秒",
			seconds, target, capcutAltMinSeconds, capcutAltMaxSeconds))
		return nil, false
	}

	// 参考图：规则五只搬豆包真正生效的那一批（第一个有值的字段）。
	images, imageErr := failoverSourceImages(body)
	if imageErr != nil {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：参考图解析失败（%v）", imageErr))
		return nil, false
	}
	if len(images) > failoverMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：参考图 %d 张超过上限 %d 张",
			len(images), failoverMaxReferenceImages))
		return nil, false
	}

	// 比例：CapCut **没有自动档**，所以「没传比例」与「auto」都不能改派 ——
	// 强行换成 16:9 等于替客户改了画幅。
	ratio, ratioOK := failoverSourceRatio(body)
	if !ratioOK {
		logger.LogInfo(c, "条件路由跳过故障转移备选：请求未指定比例（CapCut 无自动档），回落班车")
		return nil, false
	}
	if !capcutAltRatios[ratio] {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过故障转移备选：比例 %q 不在 CapCut 支持的档位内（无自动档），回落班车", ratio))
		return nil, false
	}

	return buildCapcutAltBody(target, prompt, seconds, images, ratio), true
}

// buildCapcutAltBody 组装 CapCut 契约的请求体。
//
// 只带 CapCut 真正会读的字段：
//
//	model / prompt / seconds（字符串）/ resolution（恒 480p）/ ratio / images
//
// 其余（metadata、media、size、aspect_ratio、audio/video 系列）一律**不带**：
//   - metadata：豆包链路根本不读它（客户塞进去不生效），带过去 CapCut 的
//     resolveRatio 反而会优先读 metadata.ratio，与这里的顶层 ratio 打架；
//   - media：规则五已把参考图归一成 images，再带一份 media 会重复计数。
func buildCapcutAltBody(
	target string,
	prompt string,
	seconds int,
	images []string,
	ratio string,
) map[string]interface{} {
	out := map[string]interface{}{
		"model":      target,
		"prompt":     prompt,
		"seconds":    strconv.Itoa(seconds),
		"resolution": capcutAltGenerationResolution,
		"ratio":      ratio,
	}
	if len(images) > 0 {
		out["images"] = images
	}
	return out
}
