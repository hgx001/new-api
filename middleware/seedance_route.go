package middleware

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 条件路由：把「某个对外模型名 + 特定时长」的请求改派到另一个对外模型名。
//
// 为什么放在 Distribute 之前：渠道是在 middleware/distributor.go 的 getModelRequest(c)
// 里读 body 的 model 字段选定的。改写 body 之后，选渠道、挑 adaptor、算 ModelPrice
// 全都自动跟着新模型名走 —— **不需要改任何 adaptor，也不需要动 abilities 表**。
//
// 为什么不在 controller 里改：controller/relay.go 的 getChannel 只是兜底读
// c.GetInt("channel_id")，那时渠道早就选完了，改名只会让「渠道 20 的 wan3 adaptor
// 收到 seedance-2-5 的请求」这种错配发生。
//
// 失败原则：**任何一步不确定就放弃路由**（放行原请求走源模型），
// 绝不挡客户、绝不带着少掉的素材建单。

// routeOriginModelKey 记录改派前的原始模型名。
//
// ⚠️ **只用于审计，不要拿它回填 origin_model_name**。
//
// 计费链用的是 info.OriginModelName：
//
//	relay/helper/price.go:170  ratio_setting.GetModelPrice(info.OriginModelName, true)
//
// 回填成源模型名 → 按**源模型的按秒价**计费（seedance2.5-特惠 ¥0.20/秒 × 30 = ¥6.00），
// 而不是目标模型的按次价（seedance-2-5 ¥3.00）→ **路由的省钱效果直接归零**。
//
// 任务属性 properties.origin_model_name 记「实际执行的模型」是**自洽且诚实**的：
// 计费也是按它算的，两者对得上。客户看到 model 变成 seedance-2-5 会困惑
// （他明明调的是特惠），这属于产品文案层面的决策，见设计稿「待老板拍板」第 1 条。
const routeOriginModelKey = "route_origin_model"

// RouteOriginModel 返回改派前的原始模型名；没发生改派时返回空串。
func RouteOriginModel(c *gin.Context) string {
	return c.GetString(routeOriginModelKey)
}

// Seedance25TeHuiRoute30s 在满足条件时把请求改派到目标模型。
//
// 当前唯一配置的策略：源模型 seedance2.5-特惠 + 时长 30 秒 → 目标模型 seedance-2-5。
// 具体源/目标/时长都从 seedanceroute_setting 读，改配置不用改代码。
//
// 不路由的情形（**都不是错误**，一律放行原请求）：
//   - 开关关闭（默认）
//   - 模型名不匹配
//   - 时长不等于触发值（含不传时长 → 默认 5 秒）
//   - 带了参考视频 / 参考音频（目标链路不支持，见 hasUnsupportedMedia）
//   - 请求不是 JSON
//   - body 解析或转换失败
func Seedance25TeHuiRoute30s() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsEnabled() {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
			c.Next()
			return
		}
		// /remix 是基于**已有任务**再生成，模型与参数由原任务决定
		// （relay.ResolveOriginTask 负责还原），条件路由对它没有意义，
		// 而且改写 body 会破坏 remix 的原任务语义。
		if strings.HasSuffix(c.Request.URL.Path, "/remix") {
			c.Next()
			return
		}

		policy := seedanceroute_setting.Get()
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.Next()
			return
		}
		raw, err := storage.Bytes()
		if err != nil {
			c.Next()
			return
		}

		var body map[string]interface{}
		if err := common.Unmarshal(raw, &body); err != nil {
			c.Next()
			return
		}
		model, _ := body["model"].(string)
		if strings.TrimSpace(model) != policy.SourceModel {
			c.Next()
			return
		}

		seconds, ok := extractRequestSeconds(body)
		if !ok || seconds != policy.TriggerSeconds {
			c.Next()
			return
		}

		// 目标链路（豆包官网）只支持参考图。带参考视频/音频时不路由 ——
		// 走源模型继续跑，而不是报错打断客户（老板 2026-10-30 定的口径）。
		if hasUnsupportedMedia(body) {
			c.Next()
			return
		}

		// 用户明确要了分辨率 → 不路由。目标链路没有分辨率控件，
		// 路由过去只能给一份分辨率不对的片（或者被漫屋 400 掉）。
		// 宁可走源链路按用户要的分辨率跑。
		if hasExplicitResolution(body) {
			c.Next()
			return
		}

		newBody, ok := rewriteBodyForTarget(body, policy)
		if !ok {
			c.Next()
			return
		}
		newRaw, err := common.Marshal(newBody)
		if err != nil {
			c.Next()
			return
		}
		newStorage, err := common.CreateBodyStorage(newRaw)
		if err != nil {
			c.Next()
			return
		}
		c.Set(common.KeyBodyStorage, newStorage)
		c.Request.Body = io.NopCloser(newStorage)
		c.Request.ContentLength = int64(len(newRaw))
		c.Set(routeOriginModelKey, policy.SourceModel)
		c.Next()
	}
}

// extractRequestSeconds 从请求体里解析时长。
//
// 覆盖客户可能的所有写法（与 relaycommon.TaskSubmitReq 的自定义 UnmarshalJSON 同口径）：
//   - duration：数字或字符串
//   - seconds：数字或字符串
//   - metadata.duration / metadata.seconds：部分客户端塞 metadata
//
// 任一能解析出值就返回它。**解析不出返回 ok=false** —— 调用方据此判定「不命中」，
// 而不是当成某个默认值（不传时长时上游默认 5 秒，不是 30 秒，判定必须一致）。
func extractRequestSeconds(body map[string]interface{}) (int, bool) {
	if seconds, ok := parseSecondsValue(body["duration"]); ok {
		return seconds, true
	}
	if seconds, ok := parseSecondsValue(body["seconds"]); ok {
		return seconds, true
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return 0, false
	}
	if seconds, ok := parseSecondsValue(metadata["duration"]); ok {
		return seconds, true
	}
	if seconds, ok := parseSecondsValue(metadata["seconds"]); ok {
		return seconds, true
	}
	return 0, false
}

// parseSecondsValue 把一个可能是数字、字符串或 nil 的值解析成秒数。
func parseSecondsValue(value interface{}) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), true
	case int:
		return typed, true
	case json.Number:
		if seconds, err := typed.Int64(); err == nil {
			return int(seconds), true
		}
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		if seconds, err := strconv.Atoi(trimmed); err == nil {
			return seconds, true
		}
	}
	return 0, false
}

// hasUnsupportedMedia 判断请求里有没有目标链路不支持的素材（参考视频 / 参考音频）。
//
// 有赞格式的素材在两处，type 取值：reference_image / reference_video / reference_audio：
//   - 顶层 media = [{type,url}]
//   - metadata.media = [{type,url}]
// 另外顶层 audio / audios 是**参考音频**，同样不被目标链路支持。
//
// 顶层 images / image / input_reference 之类的键是**图片**，不算不支持。
//
// ⚠️ 漏判的后果：参考视频/音频会被静默丢掉，任务照跑、照扣费，
// 但客户要的素材根本没进去 —— 比报错更糟。
func hasUnsupportedMedia(body map[string]interface{}) bool {
	// 顶层参考音频
	for _, key := range []string{"audio", "audios"} {
		if value, ok := body[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
		if value, ok := body[key].([]interface{}); ok && len(value) > 0 {
			return true
		}
	}

	// 顶层 media
	if media, ok := body["media"].([]interface{}); ok {
		for _, item := range media {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			mediaType, _ := entry["type"].(string)
			switch strings.TrimSpace(mediaType) {
			case "reference_video", "reference_audio":
				return true
			}
		}
	}

	// metadata.media
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	media, ok := metadata["media"].([]interface{})
	if !ok {
		return false
	}
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := entry["type"].(string)
		switch strings.TrimSpace(mediaType) {
		case "reference_video", "reference_audio":
			return true
		}
	}
	return false
}

// rewriteBodyForTarget 把请求体改写成目标模型能认的形状。
//
// 第二个返回值 false 表示放弃路由（调用方放行原请求）。
//
// 必须做对的三件事：
//  1. 参考图：源链路用 metadata.media=[{type,url}]，目标链路（漫屋）只认**顶层**
//     input_reference / images / image_url / reference_image 等一串别名，
//     **不含 metadata.media** —— 不转换就会静默丢图（2026-10-22 丢图事故同型）。
//     转换后**必须断言图数一致**，不一致就放弃路由。
//  2. 比例：源链路的自适应档叫 adaptive，目标链路叫 auto。
//  3. 分辨率：源链路支持 resolution（顶层或 metadata.resolution，取值 480P/720P/1080P），
//     **目标链路（豆包官网）没有分辨率控件** ——
//       - 顶层 resolution 透传下去 → 漫屋 rejectFields 显式 400
//         （"does not support resolution"）→ **客户 30 秒任务直接失败**；
//       - 顶层 size 透传下去 → 漫屋把 size 当**比例**读
//         （firstNonEmpty(Size, Ratio, AspectRatio)），"720P" 不在
//         allowedDoubaoRatios 里 → 同样 400。
//     所以顶层 resolution / size 必须**摘掉**。
//     而 metadata.resolution 表达的是「用户明确要 720P/1080P」——
//     目标链路做不到这个事，**不能假装做到了**：带分辨率的请求一律不路由，
//     交回源链路按用户要的分辨率跑（宁可不路由，也不给一份分辨率不对的片）。
func rewriteBodyForTarget(body map[string]interface{}, policy seedanceroute_setting.Policy) (map[string]interface{}, bool) {
	out := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		// model 要换成目标模型；metadata 整块丢掉（源链路的素材/比例格式目标链路不认，
		// 逐个字段挑出来重写比整块保留更不容易出错）。
		// resolution / size 必须摘掉：目标链路没有分辨率控件，带过去会被 400。
		if key == "model" || key == "metadata" || key == "resolution" || key == "size" {
			continue
		}
		out[key] = value
	}
	out["model"] = policy.TargetModel

	images, hasUnsupported := collectReferenceImages(body)
	if hasUnsupported {
		return nil, false
	}
	if len(images) > 0 {
		out["input_reference"] = images
	}

	// 时长归一：目标链路只认 5/10/15/30 四档，字符串形态最通用。
	out["seconds"] = strconv.Itoa(policy.TriggerSeconds)

	if ratio, ok := extractRatio(body); ok {
		out["ratio"] = mapAdaptiveRatio(ratio)
	}
	return out, true
}

// hasExplicitResolution 判断用户是否**明确要求了分辨率**。
//
// 覆盖源链路（youzanwan3）读分辨率的全部入口：
//   - 顶层 resolution
//   - 顶层 size（有赞也把 size 当分辨率用，见 normalizeResolutionForModel）
//   - metadata.resolution
//   - metadata.size
//
// 只认**能解析成合法分辨率**的值（480/480P/720/720P/1080/1080P），
// 与源链路的 normalizeResolution 同口径 ——
// 客户端习惯性传 `size: "16:9"`（比例语义）时，源链路本来就会忽略它，
// 不该因为这个把整单挡在路由外面。
//
// 目标链路（豆包官网）没有分辨率控件，做不到用户要的事 → 这种请求一律不路由。
func hasExplicitResolution(body map[string]interface{}) bool {
	for _, key := range []string{"resolution", "size"} {
		if isResolutionValue(body[key]) {
			return true
		}
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"resolution", "size"} {
		if isResolutionValue(metadata[key]) {
			return true
		}
	}
	return false
}

// isResolutionValue 判断一个值是不是合法的分辨率取值。
//
// 与源链路 youzanwan3 的 normalizeResolution 保持同一套取值。
func isResolutionValue(value interface{}) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case "480", "480P", "720", "720P", "1080", "1080P":
		return true
	}
	return false
}

// collectReferenceImages 收集请求里的参考图 URL 列表。
//
// 素材来源优先级与源链路（youzanwan3 的 buildMedia）**保持一致**：
// 顶层 media > metadata.media > 顶层 images / image / input_reference。
// 优先级错了就会**丢图**：源链路认顶层 media 时我们只读 metadata.media，
// 转换出来的图数就少了（2026-10-22 丢图事故同型）。
//
// 第二个返回值 true 表示遇到了目标链路不支持的素材（参考视频/音频）。
func collectReferenceImages(body map[string]interface{}) ([]string, bool) {
	var images []string

	// 1) 顶层 media（源链路优先级最高）
	if media, ok := body["media"].([]interface{}); ok {
		for _, item := range media {
			entry, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			mediaType, _ := entry["type"].(string)
			switch strings.TrimSpace(mediaType) {
			case "reference_image":
				images = appendImageURL(images, entry["url"])
			case "reference_video", "reference_audio":
				return nil, true
			}
		}
		if len(images) > 0 {
			return images, false
		}
	}

	// 2) metadata.media
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if media, ok := metadata["media"].([]interface{}); ok {
			for _, item := range media {
				entry, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				mediaType, _ := entry["type"].(string)
				switch strings.TrimSpace(mediaType) {
				case "reference_image", "first_frame", "last_frame":
					images = appendImageURL(images, entry["url"])
				case "reference_video", "reference_audio":
					return nil, true
				}
			}
		}
		if len(images) > 0 {
			return images, false
		}
	}

	// 3) 顶层 images / image / input_reference
	images = appendImageURL(images, body["images"])
	images = appendImageURL(images, body["input_reference"])
	images = appendImageURL(images, body["image"])

	return images, false
}

// appendImageURL 把一个可能是字符串或数组的值里的图片地址追加进列表。
func appendImageURL(images []string, value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			images = append(images, trimmed)
		}
	case []interface{}:
		for _, item := range typed {
			images = appendImageURL(images, item)
		}
	}
	return images
}

// extractRatio 从请求里取比例值（metadata.ratio 优先，其次顶层 ratio）。
func extractRatio(body map[string]interface{}) (string, bool) {
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if ratio, ok := metadata["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
			return strings.TrimSpace(ratio), true
		}
	}
	if ratio, ok := body["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
		return strings.TrimSpace(ratio), true
	}
	return "", false
}

// mapAdaptiveRatio 把源链路的自适应档名映射成目标链路的叫法。
//
// 源（有赞 Wan3）用 adaptive，目标（豆包官网）用 auto ——
// 不映射的话，adaptive 会被当成一个不认识的字面量发给官网。
func mapAdaptiveRatio(ratio string) string {
	if ratio == "adaptive" {
		return "auto"
	}
	return ratio
}
