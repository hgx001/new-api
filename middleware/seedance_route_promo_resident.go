package middleware

// ════════════════════════════════════════════════════════════════════════════
// 促销族（seedance2.5-特惠）常驻改派 + 自营短档（2026-11-23 老板口径）
// ════════════════════════════════════════════════════════════════════════════
//
// 这是**复用现有开关**的两段补丁，不新开一套规则引擎：
//
//	① 常驻改派（本文件 tryPromoResidentRoute）
//	   source_overrides[特惠].promo_resident_model = "wan3.0-video-tj"
//	   → 前面所有规则都没命中的请求（带参考视频/音频、带首尾帧、未传时长）
//	     落客美 tj 执行；计费仍按 seedance2.5-特惠（720P ¥0.20/秒）。
//	② 自营短档（本文件 tryPromoShortOwnRoute）
//	   promo_short_own_enabled = true
//	   → ≤15 秒 + 9:16 的请求改派 sd-seedance-2.0fast（漫屋 ch20，自营）。
//
// 另外两段**完全复用**既有规则，只改阈值与文案（见 seedance_route.go）：
//
//	> 15 秒 → seedance-2-5        ← 规则一 + seconds_trigger_seconds=15
//	≤ 15 秒 → seedance-2-0-fast   ← 规则六豆包 Fast + short_trigger_seconds=15
//
// 为什么常驻改派要排在**所有规则之后**：它是「兜底」，不是「优先级」。
// 带参考视频的请求在规则一/六/九那里都会被判「不路由」（目标链路丢素材），
// 最后落到这里才有意义；排在前面会把 30 秒的长单也钉在常驻，白丢成本优化。
//
// 为什么自营短档排在**规则六之前**：规则六豆包 Fast 的目标同样接住 ≤15 秒，
// 先判自营档才能让开关真正生效（否则永远被 Fast 抢走）。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/kemeitv"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/gin-gonic/gin"
)

// promoResidentDeliveryResolution 促销族对客恒定交付 720P。
//
// 源链路（mw-sd2.5-flash）把 480P 请求**升档**到 720P 执行、把 1080P 请求
// 在校验阶段 400 拒掉。改派时必须把「实际交付档」显式写进请求体，
// 否则目标 adaptor 会按客户字面要求降成 480P —— 客户付的是 720P 的钱，
// 拿到 480P 的片（静默降档交付）。
const promoResidentDeliveryResolution = "720P"

// promoResidentSupportedRatios 客美 tj 支持的画幅（取自 kemeitv 常量表）。
//
// 不在表里的写法（21:9 / auto / adaptive）客美会**静默回落 16:9** ——
// 那是替客户钉死画幅，宁可留在源链路（mw 支持 7 档含 21:9 与 auto）。
var promoResidentSupportedRatios = map[string]bool{
	"16:9": true, "9:16": true, "4:3": true, "3:4": true, "1:1": true,
}

// promoResidentDefaultSeconds 源链路未传时长时的缺省（mw-sd2.5-flash 文档 5 秒，
// 与对外下界同值）。客美 tj 的缺省也是 5 秒，显式写下来才不会随任何一边改而漂。
const promoResidentDefaultSeconds = 5

// tryPromoResidentRoute 把「没被其它规则接走」的促销族请求改派到常驻模型。
//
// 返回 true = 已改派（调用方直接 return）。
func tryPromoResidentRoute(
	c *gin.Context,
	body map[string]interface{},
	sourceModel string,
	billingOriginModel string,
) bool {
	// 按次档（seedance2.5-活动）不参与：它是一口价商品，常驻改派会把它乘成天价。
	// 判据走能力组，与计费链同源。
	if constant.IsSeedance25PerCall(sourceModel) {
		return false
	}
	target := seedanceroute_setting.PromoResidentModelFor(sourceModel)
	if target == "" {
		// 没配常驻改派：保持原链路（与加这个字段之前逐字节一致）。
		return false
	}

	// 1080P：源链路本来就会 400（mw 只有 480p/720p），改派过去反而降档交付。
	// 交给源链路 fail-loud，客户拿到的是「该分辨率不支持」而不是「静默变 720P」。
	if sourceResolutionTier(body) == "1080P" {
		logger.LogInfo(c, "条件路由跳过常驻改派：请求指定 1080P，源链路不支持（由其 fail-loud）")
		return false
	}

	// 画幅闸门：客美不认 21:9 / auto / adaptive，会静默回落 16:9。
	if ratio, hasRatio := resolveRequestRatio(body); hasRatio {
		normalized := normalizeRatioText(ratio)
		if !promoResidentSupportedRatios[normalized] {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过常驻改派：画幅 %s 在 %s 的档位表里没有对应项（会回落 16:9），保持源链路",
				ratio, target))
			return false
		}
	}

	// 素材闸门：直接用目标链路的校验器判，不另写一套。
	// 素材上限 / 首尾帧互斥 / 后缀白名单都在它里面；不通过就留在源链路。
	var mediaReq relaycommon.TaskSubmitReq
	if raw, err := common.Marshal(body); err == nil && common.Unmarshal(raw, &mediaReq) == nil {
		if err := kemeitv.ValidateIncomingMedia(mediaReq); err != nil {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过常驻改派：目标链路的素材校验不通过（%v），保持源链路", err))
			return false
		}
	}

	if !channelAvailabilityProbe(c, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过常驻改派：目标 %s 在当前分组下没有可用渠道", target))
		return false
	}

	seconds, hasSeconds := extractRequestSeconds(body)
	if !hasSeconds || seconds <= 0 {
		// 未传时长：用源链路缺省（特惠 = 5 秒）显式落盘，避免目标 adaptor
		// 用自己的缺省值（同样是 5 秒，但显式写下来才不会被将来改动带偏）。
		seconds = promoResidentDefaultSeconds
	}

	rewritten := make(map[string]interface{}, len(body)+3)
	for key, value := range body {
		if key == "model" {
			continue
		}
		rewritten[key] = value
	}
	rewritten["model"] = target
	rewritten["duration"] = seconds
	rewritten["resolution"] = promoResidentDeliveryResolution
	if ratio, hasRatio := resolveRequestRatio(body); hasRatio {
		rewritten["ratio"] = normalizeRatioText(ratio)
	}

	if !applyRewrittenBody(c, rewritten, billingOriginModel) {
		return false
	}
	// 计费按**源模型**口径：客美 tj 的 720P 倍率是 1.81（绝对档位价反推），
	// 不覆盖就会把特惠的 ¥0.20/秒 乘成 ¥0.36/秒。秒数**不覆盖** ——
	// 客美 adaptor 自己会算「生成秒数 + 参考视频秒数」，与 mw 同口径。
	setRouteBillingContext(c, 0, sourceModelSizeRatio(sourceModel, promoResidentDeliveryResolution))

	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：未命中其它规则，改派 %s → %s（常驻链路，%d 秒，计费仍按 %s）",
		sourceModel, target, seconds, sourceModel))
	return true
}

// tryPromoShortOwnRoute 把「≤15 秒 + 9:16」的促销族请求改派到自营 sd- 档。
//
// 返回 true = 已改派（调用方直接 return）。
func tryPromoShortOwnRoute(
	c *gin.Context,
	body map[string]interface{},
	sourceModel string,
	billingOriginModel string,
) bool {
	if !seedanceroute_setting.PromoShortOwnEnabledFor(sourceModel) {
		return false
	}
	// 按次档（活动版）不参与短时长分流：它固定 30 秒一口价。
	if constant.IsSeedance25PerCall(sourceModel) {
		return false
	}

	seconds, ok := extractRequestSeconds(body)
	if !ok {
		// 未传时长不在路由层替客户做「这算短任务」的决定（与规则十同口径）。
		return false
	}
	if seconds < seedanceroute_setting.PromoShortOwnMinSeconds ||
		seconds > seedanceroute_setting.PromoShortOwnMaxSeconds {
		// 越界（<4 或 >15）不路由，不做就近吸附 —— 吸附是静默改档。
		return false
	}

	// 9:16 是硬条件（老板口径「这个模型必须是 9:16 的才路由」）。
	// 没传比例时不路由：源链路缺省是 16:9，改派过去会被钉成 9:16。
	ratio, hasRatio := resolveRequestRatio(body)
	if !hasRatio || normalizeRatioText(ratio) != seedanceroute_setting.PromoShortOwnRatio {
		return false
	}

	// 参考音视频 / 首尾帧：自营档只认参考图（改派过去是静默丢素材）。
	if requestHasReferenceAudioVideo(body) || hasFrameMedia(body) {
		return false
	}
	images, unsupported := collectReferenceImagesInternal(body, false, true)
	if unsupported {
		return false
	}
	if len(images) > seedanceroute_setting.PromoShortOwnMaxImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过自营短档：参考图 %d 张超过该链路上限 %d 张",
			len(images), seedanceroute_setting.PromoShortOwnMaxImages))
		return false
	}

	target := seedanceroute_setting.DefaultPromoShortOwnModel
	if !channelAvailabilityProbe(c, target) {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过自营短档：目标 %s 在当前分组下没有可用渠道", target))
		return false
	}

	// 参考图必须抬到**目标 adaptor 的白名单字段**（manwu 只读顶层
	// input_reference / images 等别名，不读 metadata.media / metadata.input.media）。
	//
	// 2026-11-24 A 机事故：原实现只换 model / resolution / ratio / seconds，
	// 图还留在 ArcReel 发来的 metadata.input.media（有赞 Wan3 形状）里 ——
	// manwu 日志 `source=<none> count=0`，Worker 判「TikTok 图生视频至少需要
	// 1 张参考图」，任务直接失败，客户看到「提供了图片，却显示没有图」。
	//
	// 剥掉全部素材源位置与整块 metadata，与 rewriteBodyForCapcut 同口径：
	// 图统一收到顶层 input_reference，源位置留副本会让读 metadata.media 的
	// 目标同图投两次；比例/分辨率已显式落到顶层，metadata 没有保留价值。
	rewritten := make(map[string]interface{}, len(body)+3)
	for key, value := range body {
		switch key {
		case "model", "metadata":
			continue
		case "media", "images", "image", "input_reference", "reference_images",
			"reference_image", "image_url", "image_urls", "imageUrl", "imageUrls",
			"referenceImage", "referenceImages", "input_images", "inputImages",
			"inputReference":
			continue
		}
		rewritten[key] = value
	}
	rewritten["model"] = target
	if len(images) > 0 {
		rewritten["input_reference"] = images
	}
	rewritten["resolution"] = promoResidentDeliveryResolution
	rewritten["ratio"] = seedanceroute_setting.PromoShortOwnRatio
	rewritten["seconds"] = strconv.Itoa(seconds)

	if !applyRewrittenBody(c, rewritten, billingOriginModel) {
		return false
	}
	// 计费仍按源模型：秒数由路由层钉死（自营 adaptor 不给 seconds，
	// 漏记就会按 1 秒收费）；size 倍率按源模型 720P 口径（特惠 = 1.0）。
	setRouteBillingContext(c, seconds, sourceModelSizeRatio(sourceModel, promoResidentDeliveryResolution))

	_ = strings.TrimSpace(sourceModel)
	logger.LogInfo(c, fmt.Sprintf(
		"条件路由：命中自营短档（%d 秒，%s），改派 %s → %s（计费仍按 %s）",
		seconds, seedanceroute_setting.PromoShortOwnRatio, sourceModel, target, billingOriginModel))
	return true
}
