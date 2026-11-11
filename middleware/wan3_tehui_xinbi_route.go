package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/logger"

	"github.com/gin-gonic/gin"
)

// wan3.0-720p-特惠 的分流：能力允许时走心笔（480P 生成 + 交付 720P，成本更低），
// 心笔吃不住的请求留在原 Wan3 官网链路。
//
// 为什么需要分流而不是直接换渠道：
//
//	特惠现在的 Wan3 链路支持首尾帧、参考视频（5 段）、参考音频（5 段）、
//	seed、2–30 秒任意时长；心笔兼容口只收参考图（≤10 张）、不支持 seed，
//	且只交付 720P。直接换渠道会把「带首尾帧的任务」变成建单失败或素材被丢。
//	规则三（首尾帧改派）的目标正是特惠 —— 换渠道必须保住这条能力。
//
// 计费原点：改写只动请求体，RouteOriginModel 保持客户请求的模型
// （直接调用 = 特惠；规则二/三改派过 = 促销族）。账单、任务列表、
// OpenAI Video 响应仍显示客户买的那个模型。
const (
	// wan3TehuiModelName 是触发分流的对外模型名。
	wan3TehuiModelName = "wan3.0-720p-特惠"
	// xinbiTehuiTargetModel 是心笔渠道的内部目标模型名，
	// 必须与 relay/channel/task/xinbiapi.ModelNameTehuiRoute 一致（有测试守着）。
	xinbiTehuiTargetModel = "wan3.0-720p-特惠-内链"
	// tehuiXinbiDefaultSeconds / Min / Max 与 Wan3 特惠链路的时长口径一致。
	tehuiXinbiDefaultSeconds = 5
	tehuiXinbiMinSeconds     = 2
	tehuiXinbiMaxSeconds     = 30
)

// xinbiRatioSet 是心笔上游 parameterSchema 声明的比例枚举。
// 与 Wan3 的 adaptive/16:9/9:16/1:1/4:3/3:4 同口径；不在这里的比例（如 21:9）
// 分流过去会被上游 400，必须留在原链路。
var xinbiRatioSet = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
}

// Wan3TehuiXinbiRoute 把 wan3.0-720p-特惠 的可兼容请求分流到心笔。
//
// 顺序要求：必须挂在 Seedance25TeHuiRoute 之后（规则二/三会先把它改写成特惠），
// 且早于 Distribute（渠道是读 body 的 model 字段选定的）。
func Wan3TehuiXinbiRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		// remix 基于原任务再生成，参数由原任务决定，改写会破坏其语义。
		if strings.HasSuffix(c.Request.URL.Path, "/remix") {
			c.Next()
			return
		}

		body, ok := readJSONRequestBody(c)
		if !ok {
			c.Next()
			return
		}
		modelName, _ := body["model"].(string)
		if strings.TrimSpace(modelName) != wan3TehuiModelName {
			c.Next()
			return
		}

		newBody, seconds, routed := prepareTehuiXinbiRoute(c, body, xinbiTehuiTargetModel)
		if !routed {
			c.Next()
			return
		}
		if !applyRewrittenBodyKeepingOrigin(c, newBody, wan3TehuiModelName) {
			c.Next()
			return
		}
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 分流到内链（%d 秒），对客模型与计费口径不变",
			wan3TehuiModelName, seconds))
		c.Next()
	}
}

// isXinbiTehuiTarget 判断目标模型是不是心笔的 480P 按秒内链。
//
// 2026-11-11 起规则二（比例）也可以直接指向这条链路 —— 命中后必须走
// prepareTehuiXinbiRoute 的改写，不能用 wan3 官网那套（会静默丢参考图）。
func isXinbiTehuiTarget(modelName string) bool {
	return strings.TrimSpace(modelName) == xinbiTehuiTargetModel
}

// prepareTehuiXinbiRoute 判断能否分流并组装请求体。
//
// targetModel 由调用方给：Wan3TehuiXinbiRoute 用内链常量，
// 条件路由的规则二用配置里的 ratio_target_model —— 两者当前是同一个名字，
// 但分开传能让「配置改成别的名字」立刻生效，而不是悄悄仍用常量。
//
// 第二个返回值是归一后的计费/生成秒数（仅用于日志）。
func prepareTehuiXinbiRoute(c *gin.Context, body map[string]interface{}, targetModel string) (map[string]interface{}, int, bool) {
	if !tehuiXinbiCompatible(c, body, targetModel) {
		return nil, 0, false
	}
	seconds, ok := tehuiXinbiSeconds(body)
	if !ok {
		return nil, 0, false
	}
	return rewriteBodyForXinbiPerSecond(body, targetModel, seconds), seconds, true
}

// tehuiXinbiCompatible 是分流的能力/参数边界。
//
// 任何一条不满足都**留在原链路**：宁可多付官网的钱，也不能让客户拿到
// 建单失败或素材被丢的结果。targetModel 同时用于渠道可用性预检。
func tehuiXinbiCompatible(c *gin.Context, body map[string]interface{}, targetModel string) bool {
	// 参考视频 / 参考音频（含顶层 audio/audios）：兼容口没有对应字段。
	if hasUnsupportedMedia(body) {
		logger.LogInfo(c, "条件路由跳过特惠分流：请求带参考视频/参考音频")
		return false
	}
	// 首尾帧：兼容口只收参考图。
	if hasFrameMedia(body) {
		logger.LogInfo(c, "条件路由跳过特惠分流：请求带首尾帧")
		return false
	}
	// seed：上游 parameterSchema 是 additionalProperties:false，带 seed 会被拒。
	if raw, ok := body["seed"]; ok && failoverHasValue(raw) {
		logger.LogInfo(c, "条件路由跳过特惠分流：请求带 seed")
		return false
	}
	// 分辨率**不参与**分流判断。
	//
	// 特惠只有一个交付档位（720P）：原 Wan3 链路对它的处理是
	// resolveGenerationResolution() 里无条件返回 480P，客户端传的
	// 480P / 720P / 1080P 全部被忽略，成片统一提升到 720P。
	// 心笔这边 generationResolution 同样锁 480P、交付同样锁 720P，
	// 两条链路产物一致，所以「传了 1080P」不该把请求赶回贵链路。
	//
	// 参数本身仍要剥掉（见 rewriteBodyForXinbiPerSecond）：
	// 心笔的顶层 size 会被当成比例误读。
	// 比例：上游只认固定枚举，别的比例分流过去必被 400。
	if ratio, ok := resolveRequestRatio(body); ok && !xinbiRatioSet[ratio] {
		logger.LogInfo(c, fmt.Sprintf("条件路由跳过特惠分流：比例 %s 不在目标链路枚举内", ratio))
		return false
	}

	images, hasUnsupported := collectReferenceImages(body)
	if hasUnsupported {
		logger.LogInfo(c, "条件路由跳过特惠分流：metadata.media 里有非图片素材")
		return false
	}
	if len(images) > xinbiMaxReferenceImages {
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由跳过特惠分流：参考图 %d 张超过目标链路上限 %d 张",
			len(images), xinbiMaxReferenceImages))
		return false
	}
	// 参考图必须是 https 直链（data: 内联允许），http 会被上游拒。
	for _, image := range images {
		if strings.HasPrefix(image, "data:") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(image), "https://") {
			logger.LogInfo(c, "条件路由跳过特惠分流：参考图不是 https 直链")
			return false
		}
	}

	// 目标模型没有可用渠道 → 不分流。改写后重试只会用同一个模型名重选渠道，
	// 不会退回特惠。
	if !channelAvailabilityProbe(c, targetModel) {
		logger.LogInfo(c, "条件路由跳过特惠分流：目标模型在当前分组下没有可用渠道")
		return false
	}
	return true
}

// tehuiXinbiSeconds 复刻 Wan3 的时长归一：duration 优先、再 seconds、
// 再默认 5 秒，最后夹到 [2,30]。
//
// 第二个返回值 false 表示「字段有值但不是整数」（如 7.5）—— 这种请求
// 留在原链路，让源 adaptor 按它自己的规则报错，不在分流层替它决定。
func tehuiXinbiSeconds(body map[string]interface{}) (int, bool) {
	if raw, ok := body["duration"]; ok && failoverHasValue(raw) {
		seconds, integral := bodyInteger(raw)
		if !integral {
			return 0, false
		}
		if seconds > 0 {
			return clampTehuiXinbiSeconds(seconds), true
		}
	}
	if raw, ok := body["seconds"]; ok && failoverHasValue(raw) {
		seconds, integral := bodyInteger(raw)
		if !integral {
			return 0, false
		}
		if seconds > 0 {
			return clampTehuiXinbiSeconds(seconds), true
		}
	}
	return tehuiXinbiDefaultSeconds, true
}

func clampTehuiXinbiSeconds(seconds int) int {
	if seconds < tehuiXinbiMinSeconds {
		return tehuiXinbiMinSeconds
	}
	if seconds > tehuiXinbiMaxSeconds {
		return tehuiXinbiMaxSeconds
	}
	return seconds
}

// bodyInteger 把一个 JSON 数值/字符串解析成整数；小数一律判为非整数。
func bodyInteger(raw interface{}) (int, bool) {
	switch typed := raw.(type) {
	case float64:
		if typed != float64(int(typed)) {
			return 0, false
		}
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			return 0, false
		}
		return int(value), true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		value, err := strconv.Atoi(trimmed)
		if err != nil {
			return 0, false
		}
		return value, true
	default:
		return 0, false
	}
}

// rewriteBodyForXinbiPerSecond 按心笔兼容口的契约重写请求体。
//
//	接受：model / prompt / duration(seconds) / metadata{ratio}
//	素材：统一收到顶层 reference_images[]（图片直链）
//
// 顶层 media / metadata.media 必须删掉：留着会被 adaptor 再收一遍，
// 同一张参考图投递两次。resolution / size 也删掉 —— 心笔只认 metadata.ratio，
// 顶层像素尺寸会被当成比例误读。
func rewriteBodyForXinbiPerSecond(body map[string]interface{}, target string, seconds int) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		switch key {
		case "model", "metadata", "duration", "seconds", "resolution", "size",
			"images", "image", "input_reference", "reference_images", "media":
			continue
		}
		out[key] = value
	}
	out["model"] = target
	out["seconds"] = strconv.Itoa(seconds)

	images, _ := collectReferenceImages(body)
	if len(images) > 0 {
		out["reference_images"] = images
	}

	metadata := map[string]interface{}{}
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			switch key {
			case "media", "resolution", "size", "duration", "seconds", "ratio", "aspect_ratio":
				continue
			}
			metadata[key] = value
		}
	}
	if ratio, ok := resolveRequestRatio(body); ok {
		metadata["ratio"] = ratio
	}
	out["metadata"] = metadata

	return out
}

// applyRewrittenBodyKeepingOrigin 换回请求体，但**不覆盖**已有的计费原点。
//
// 规则二/三先改写时已经把 RouteOriginModel 记成客户最初请求的促销族模型；
// 分流层再写一次特惠，会让客户账单上的价格跟着调度策略漂移。
func applyRewrittenBodyKeepingOrigin(c *gin.Context, body map[string]interface{}, fallbackOrigin string) bool {
	origin := trimSpace(c.GetString(routeOriginModelKey))
	if origin == "" {
		origin = fallbackOrigin
	}
	return applyRewrittenBody(c, body, origin)
}
