package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"

	"github.com/gin-gonic/gin"
)

// seedance2.5-活动 的参考视频改派：带参考视频时走有赞 prime（seedance2.5-特惠）。
//
// 为什么需要改派：
//
//	活动版常驻链路是心笔，心笔把参考视频秒数并入上游计费
//	（480P ¥0.135/秒 ×（生成秒数 + 参考视频秒数）），而活动版是按次 ¥4.5/条。
//	30 秒生成 + 15 秒参考视频会到 ¥6.08，直接倒挂。
//	有赞 prime 是参考视频请求的既定链路（¥0.15/秒、原生 720P），
//	且活动版与特惠同属 seedance2.5 促销族，请求形态完全一致。
//
// 为什么只改派 2–15 秒：
//
//	有赞对有参考视频的任务硬限制生成 ≤15 秒
//	（youzanwan3.MaxGenerationSecondsWithVideo），16–30 秒的请求留在心笔，
//	不能为了省钱把客户打成 400；不传时长时按次档默认 30 秒，同样不改派。
//
// 计费原点保持 seedance2.5-活动：客户仍按 ¥4.5/条结算，
// 改派只影响上游成本，不影响对客价格与账单上的模型名。
const (
	huoDongRefVideoSourceModel = constant.Seedance25HuoDongModel
	huoDongRefVideoTargetModel = constant.Seedance25TeHuiModel
	// huoDongRefVideoMinSeconds 与按次档的下限一致：更短的请求由源链路
	// 按自己的规则拒绝，路由层不改写它的校验口径。
	huoDongRefVideoMinSeconds = 2
)

// Seedance25HuoDongRefVideoRoute 把活动版的参考视频请求改派到有赞 prime。
//
// 顺序要求：必须挂在 Seedance25TeHuiRoute 与 Wan3TehuiXinbiRoute 之后、
// Distribute 之前。规则一（>20 秒改派豆包）先跑，跑过之后模型已不是活动版，
// 本规则自然不介入；本规则改写成特惠后也不能再被特惠分流接手，
// 否则参考视频请求会被再次改写回心笔。
func Seedance25HuoDongRefVideoRoute() gin.HandlerFunc {
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
		if strings.TrimSpace(modelName) != huoDongRefVideoSourceModel {
			c.Next()
			return
		}
		if !hasReferenceVideoForRoute(body) {
			c.Next()
			return
		}

		seconds, ok := huoDongRefVideoSeconds(body)
		if !ok {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频改派：生成时长不在有赞可受理区间 %d-%d 秒",
				huoDongRefVideoMinSeconds, youzanwan3.MaxGenerationSecondsWithVideo))
			c.Next()
			return
		}

		// 目标模型没有可用渠道 → 不改派。改写后重试只会用同一个模型名重选渠道，
		// 不会退回活动版。
		if !channelAvailabilityProbe(c, huoDongRefVideoTargetModel) {
			logger.LogInfo(c, "条件路由跳过参考视频改派：目标模型在当前分组下没有可用渠道")
			c.Next()
			return
		}

		newBody := rewriteBodyForYouzanPrime(body)
		if !applyRewrittenBodyKeepingOrigin(c, newBody, huoDongRefVideoSourceModel) {
			c.Next()
			return
		}
		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 带参考视频改派到 %s（%d 秒），对客模型与计费口径不变",
			huoDongRefVideoSourceModel, huoDongRefVideoTargetModel, seconds))
		c.Next()
	}
}

// huoDongRefVideoSeconds 返回可用于改派的生成秒数。
//
// 未传时长时按次档默认 30 秒，超过有赞上限，因此一并返回 false。
func huoDongRefVideoSeconds(body map[string]interface{}) (int, bool) {
	seconds, ok := extractRequestSeconds(body)
	if !ok {
		return 0, false
	}
	if seconds < huoDongRefVideoMinSeconds {
		return 0, false
	}
	if seconds > youzanwan3.MaxGenerationSecondsWithVideo {
		return 0, false
	}
	return seconds, true
}

// hasReferenceVideoForRoute 判断请求里是否带参考视频。
//
// 三种素材容器都要看：顶层 media、metadata.media、metadata.input.media；
// 兼容口字段 video / videos 也算。只看其中一个会漏判，
// 漏判的后果是参考视频请求继续走心笔、成本可能倒挂。
func hasReferenceVideoForRoute(body map[string]interface{}) bool {
	if strings.TrimSpace(stringValue(body["video"])) != "" {
		return true
	}
	if len(stringListValue(body["videos"])) > 0 {
		return true
	}

	containers := make([][]interface{}, 0, 3)
	if media, ok := body["media"].([]interface{}); ok {
		containers = append(containers, media)
	}
	if metadata, ok := body["metadata"].(map[string]interface{}); ok {
		if media, ok := metadata["media"].([]interface{}); ok {
			containers = append(containers, media)
		}
		if input, ok := metadata["input"].(map[string]interface{}); ok {
			if media, ok := input["media"].([]interface{}); ok {
				containers = append(containers, media)
			}
		}
	}
	for _, media := range containers {
		for _, item := range media {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if strings.TrimSpace(stringValue(entry["type"])) == "reference_video" {
				return true
			}
		}
	}
	return false
}

// rewriteBodyForYouzanPrime 改写请求体：
//
//	model  → seedance2.5-特惠（有赞 prime）
//	分辨率 → 固定 720P（活动版的交付档位，不允许改派后升档）
//	顶层 video/videos → 并入有赞 adaptor 实际读取的 media 数组
//
// 其余字段（prompt / duration|seconds / metadata.ratio / media / images ...）原样保留。
func rewriteBodyForYouzanPrime(body map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		switch key {
		case "model", "metadata", "resolution", "size":
			continue
		}
		out[key] = value
	}
	out["model"] = huoDongRefVideoTargetModel

	metadata := map[string]interface{}{}
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			if key == "resolution" || key == "size" {
				continue
			}
			metadata[key] = value
		}
	}
	// 顶层 size 可能是像素尺寸（丢掉），也可能写成比例（9:16）。
	// 摘掉 size 前必须把比例搬进 metadata.ratio，否则客户选的画幅会变成 adaptive。
	if _, exists := metadata["ratio"]; !exists {
		if ratio, ok := resolveRequestRatio(body); ok {
			metadata["ratio"] = ratio
		} else if size, ok := body["size"].(string); ok {
			// resolveRequestRatio 只从像素尺寸推断比例，认不出 "9:16" 这种写法。
			if ratio := normalizeRatioText(size); ratio != "" {
				metadata["ratio"] = ratio
			}
		}
	}
	metadata["resolution"] = constant.Seedance25PerCallFixedResolution
	out["metadata"] = metadata

	mergeTopLevelVideos(out, metadata, body)
	return out
}

// mergeTopLevelVideos 把兼容口顶层 video / videos 并入 media。
//
// 有赞 adaptor 不读这两个字段，直接改名会让参考视频被静默丢掉
// （任务照建、照扣费、成片没有参考素材）。合并目标必须与 adaptor 的读取顺序
// 一致：顶层 media 优先，其次 metadata.media，再次 metadata.input.media；
// 三处都没有才新建顶层 media。
func mergeTopLevelVideos(out map[string]interface{}, metadata map[string]interface{}, body map[string]interface{}) {
	urls := topLevelVideoURLs(body)
	if len(urls) == 0 {
		return
	}
	delete(out, "video")
	delete(out, "videos")

	if media, ok := body["media"].([]interface{}); ok && len(media) > 0 {
		out["media"] = appendReferenceVideos(media, urls)
		return
	}
	if media, ok := metadata["media"].([]interface{}); ok && len(media) > 0 {
		metadata["media"] = appendReferenceVideos(media, urls)
		return
	}
	if input, ok := metadata["input"].(map[string]interface{}); ok {
		if media, ok := input["media"].([]interface{}); ok && len(media) > 0 {
			input["media"] = appendReferenceVideos(media, urls)
			return
		}
	}
	media := make([]interface{}, 0, len(urls))
	out["media"] = appendReferenceVideos(media, urls)
}

// stringValue 把 JSON 值当字符串读；非字符串一律视为空。
func stringValue(value interface{}) string {
	typed, _ := value.(string)
	return typed
}

func topLevelVideoURLs(body map[string]interface{}) []string {
	urls := make([]string, 0, 2)
	if single := strings.TrimSpace(stringValue(body["video"])); single != "" {
		urls = append(urls, single)
	}
	urls = append(urls, stringListValue(body["videos"])...)
	return urls
}

func appendReferenceVideos(media []interface{}, urls []string) []interface{} {
	for _, url := range urls {
		if mediaHasReferenceVideoURL(media, url) {
			continue
		}
		media = append(media, map[string]interface{}{
			"type": "reference_video",
			"url":  url,
		})
	}
	return media
}

func mediaHasReferenceVideoURL(media []interface{}, url string) bool {
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if strings.TrimSpace(stringValue(entry["type"])) != "reference_video" {
			continue
		}
		if strings.TrimSpace(stringValue(entry["url"])) == url {
			return true
		}
	}
	return false
}
