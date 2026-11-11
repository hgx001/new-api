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
//
// 改写原则（与源链路逐条对齐）：
//
//	源链路（心笔兼容口）与有赞的素材契约不同——有赞只认顶层 media，且只收
//	reference_image / reference_video / reference_audio 三种 type。
//	所以本规则必须把三类 media 容器、顶层 video/videos/audio/audios、legacy
//	images 全部**合并归一**再投递；任何搬不动的字段一律放弃改派（放行原请求），
//	绝不带着少掉的素材或改掉的语义建单。
const (
	huoDongRefVideoSourceModel = constant.Seedance25HuoDongModel
	huoDongRefVideoTargetModel = constant.Seedance25TeHuiModel
	// huoDongRefVideoMinSeconds 与按次档的下限一致：更短的请求由源链路
	// 按自己的规则拒绝，路由层不改写它的校验口径。
	huoDongRefVideoMinSeconds = 2

	youzanReferenceImage = "reference_image"
	youzanReferenceVideo = "reference_video"
	youzanReferenceAudio = "reference_audio"
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

		if !huoDongRefVideoCompatible(body) {
			logger.LogInfo(c, "条件路由跳过参考视频改派：请求带源链路专属语义（显式非 720P / seed / input_reference）")
			c.Next()
			return
		}
		media, ok := collectYouzanMedia(body)
		if !ok {
			logger.LogInfo(c, "条件路由跳过参考视频改派：请求带无法安全搬运的素材类型")
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

		newBody := rewriteBodyForYouzanPrime(body, media)
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

// huoDongRefVideoCompatible 判断请求里有没有「源链路专属」的字段。
//
// 这些字段在源链路的语义与被改派后的链路不一致，改了就是静默改档/改语义：
//   - 显式非 720P：源链路（心笔按次档）明确 400；改派后强制 720P 等于
//     「客户要 1080P，我们悄悄给 720P」。
//   - seed：源链路 400（上游 parameterSchema 拒绝）；有赞会直接忽略。
//   - input_reference：源链路忽略；有赞会当成参考图，凭空多一张素材。
func huoDongRefVideoCompatible(body map[string]interface{}) bool {
	if raw := strings.TrimSpace(stringValue(body["resolution"])); raw != "" &&
		!strings.EqualFold(raw, constant.Seedance25PerCallFixedResolution) {
		return false
	}
	if body["seed"] != nil {
		return false
	}
	if strings.TrimSpace(stringValue(body["input_reference"])) != "" {
		return false
	}
	return true
}

// hasReferenceVideoForRoute 判断请求里是否带参考视频。
//
// 三种素材容器都要看：顶层 media、metadata.media、metadata.input.media；
// 兼容口字段 video / videos 也算，media 里的旧写法（type=video）同样算。
// 只看其中一个会漏判，漏判的后果是参考视频请求继续走心笔、成本可能倒挂。
func hasReferenceVideoForRoute(body map[string]interface{}) bool {
	for _, container := range mediaContainers(body) {
		for _, item := range container {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if strings.TrimSpace(stringValue(entry["url"])) == "" {
				continue
			}
			if mediaType, ok := youzanMediaType(stringValue(entry["type"])); ok && mediaType == youzanReferenceVideo {
				return true
			}
		}
	}
	return len(topLevelVideoURLs(body)) > 0
}

// mediaContainers 按两份 adaptor 共同的读取顺序返回三类素材容器。
func mediaContainers(body map[string]interface{}) [][]interface{} {
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
	return containers
}

// youzanMediaType 把源链路的素材类型归一成有赞只认的三种。
//
// 判定口径与心笔 adaptor 的 classifyMedia 一致（子串匹配、image 优先于 video）：
// 同一个 type 在两条链路必须落到同一类素材，否则改派会改变素材语义。
// 首尾帧与未知类型返回 false —— 源链路对这些请求本来就会报错，
// 改派只会用有赞的报错替换掉源链路那条，没有收益。
func youzanMediaType(raw string) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(normalized, "first_frame"), strings.Contains(normalized, "last_frame"):
		return "", false
	case normalized == "" || strings.Contains(normalized, "image"):
		return youzanReferenceImage, true
	case strings.Contains(normalized, "video"):
		return youzanReferenceVideo, true
	case strings.Contains(normalized, "audio"):
		return youzanReferenceAudio, true
	}
	return "", false
}

// collectYouzanMedia 把所有素材容器合并成有赞实际读取的顶层 media 数组。
//
// 顺序按源链路的读取顺序排列（同类型内的相对顺序不能变，prompt 里的
// @图片N / @视频N / @音频N 依赖它）：
//
//	image/images → video/videos → audio/audios → media → metadata.media → metadata.input.media
//
// 返回 ok=false 表示存在搬不动的素材（未知类型 / 首尾帧 / 非对象条目），
// 调用方必须放弃改派 —— 宁可留在源链路上跑，也不能丢素材。
func collectYouzanMedia(body map[string]interface{}) ([]interface{}, bool) {
	media := make([]interface{}, 0, 4)
	for _, url := range legacyImageURLs(body) {
		media = append(media, youzanMediaEntry(youzanReferenceImage, url))
	}
	for _, url := range topLevelVideoURLs(body) {
		media = append(media, youzanMediaEntry(youzanReferenceVideo, url))
	}
	for _, url := range topLevelAudioURLs(body) {
		media = append(media, youzanMediaEntry(youzanReferenceAudio, url))
	}
	for _, container := range mediaContainers(body) {
		for _, item := range container {
			entry, ok := item.(map[string]interface{})
			if !ok {
				return nil, false
			}
			url := strings.TrimSpace(stringValue(entry["url"]))
			if url == "" {
				// 源链路忽略空 URL 条目，有赞会直接 400 —— 跟着源链路丢掉。
				continue
			}
			mediaType, ok := youzanMediaType(stringValue(entry["type"]))
			if !ok {
				return nil, false
			}
			media = append(media, youzanMediaEntry(mediaType, url))
		}
	}
	return dedupeMedia(media), true
}

func youzanMediaEntry(mediaType, url string) map[string]interface{} {
	return map[string]interface{}{"type": mediaType, "url": url}
}

// dedupeMedia 按 type+url 去重（保留首次出现的位置）：
// 同一素材同时出现在顶层字段与 media 里时不能重复投递。
func dedupeMedia(media []interface{}) []interface{} {
	seen := make(map[string]bool, len(media))
	out := make([]interface{}, 0, len(media))
	for _, item := range media {
		entry, _ := item.(map[string]interface{})
		key := stringValue(entry["type"]) + "\x00" + stringValue(entry["url"])
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

// rewriteBodyForYouzanPrime 改写请求体：
//
//	model  → seedance2.5-特惠（有赞 prime）
//	分辨率 → 固定 720P（活动版的交付档位，不允许改派后升档）
//	素材   → 全部并入顶层 media（有赞只读这一处）
//
// 其余字段（prompt / duration|seconds / metadata.ratio ...）原样保留。
func rewriteBodyForYouzanPrime(body map[string]interface{}, media []interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		switch key {
		// 素材字段已合并进 media，其余字段搬进新 body 后必须消失：
		// 留在原处会被有赞 adaptor 再读一遍（legacy images）或静默忽略。
		case "model", "metadata", "resolution", "size", "media",
			"image", "images", "input_reference",
			"video", "videos", "audio", "audios":
			continue
		}
		out[key] = value
	}
	out["model"] = huoDongRefVideoTargetModel
	out["media"] = media

	metadata := map[string]interface{}{}
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			switch key {
			case "resolution", "size", "media":
				continue
			}
			metadata[key] = value
		}
	}
	if input, ok := metadata["input"].(map[string]interface{}); ok {
		copied := make(map[string]interface{}, len(input))
		for key, value := range input {
			if key != "media" {
				copied[key] = value
			}
		}
		if len(copied) == 0 {
			delete(metadata, "input")
		} else {
			metadata["input"] = copied
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
	return out
}

// stringValue 把 JSON 值当字符串读；非字符串一律视为空。
func stringValue(value interface{}) string {
	typed, _ := value.(string)
	return typed
}

func legacyImageURLs(body map[string]interface{}) []string {
	urls := make([]string, 0, 2)
	if single := strings.TrimSpace(stringValue(body["image"])); single != "" {
		urls = append(urls, single)
	}
	urls = append(urls, stringListValue(body["images"])...)
	return urls
}

func topLevelVideoURLs(body map[string]interface{}) []string {
	urls := make([]string, 0, 2)
	if single := strings.TrimSpace(stringValue(body["video"])); single != "" {
		urls = append(urls, single)
	}
	urls = append(urls, stringListValue(body["videos"])...)
	return urls
}

func topLevelAudioURLs(body map[string]interface{}) []string {
	urls := make([]string, 0, 2)
	if single := strings.TrimSpace(stringValue(body["audio"])); single != "" {
		urls = append(urls, single)
	}
	urls = append(urls, stringListValue(body["audios"])...)
	return urls
}
