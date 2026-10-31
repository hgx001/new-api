package middleware

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/wan3"
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
// **计费与展示都以它为优先**（老板 2026-10-30 定稿）：客户请求哪个模型，
// 就按哪个模型的价格收。改派是我们为了省上游成本做的内部调度，
// 不能让客户看到「我买 A，账单按 B 算」，也不能让对客价格随调度策略漂移。
//
// 计费链取值入口是 RelayInfo.BillingModelName()：
//
//	relay/helper/price.go  ratio_setting.GetModelPrice(info.BillingModelName(), true)
//
// 所以 30 秒改派到豆包后，客户付的仍是 seedance2.5-特惠 的 ¥0.20/秒 × 30 = ¥6.00，
// 而不是豆包的按次 ¥3.00。差额是内部调度收益，不体现在客户账单上。
//
// ⚠️ 但光有模型名不够：源模型按秒计价，而目标 adaptor（manwu）对按次模型
// 返回 nil 倍率，没有 seconds 就会把 30 秒算成 1 秒。所以规则一命中时
// 还要把秒数记进 ContextKeyRouteBillingSeconds（见 relay_task.go 第 5.5 步）。
//
// properties.origin_model_name 记「实际执行的模型」是自洽且诚实的：
// 它回答的是「这条任务在哪个链路上跑」，与「按谁收费」是两个问题。
const routeOriginModelKey = string(constant.ContextKeyRouteOriginModel)

// RouteOriginModel 返回改派前的原始模型名；没发生改派时返回空串。
func RouteOriginModel(c *gin.Context) string {
	return c.GetString(routeOriginModelKey)
}

// Seedance25TeHuiRoute 在满足条件时把请求改派到另一个对外模型。
//
// 两条规则，**时长规则优先**（老板 2026-10-30 拍板「30 秒优先，豆包仍接 9:16」）：
//
//	规则一（时长）：源模型 + 30 秒 → seedance-2-5（豆包，按次 ¥3.00）
//	规则二（比例）：源模型 + 9:16（非 30 秒）→ wan3.0-720p-特惠（按秒 ¥0.20）
//
// 改派**只决定内部走哪条链路，不改变对客价格**：两条规则的计费都按源模型
// （seedance2.5-特惠，¥0.20/秒）算，客户请求什么模型就按什么模型收。
// 所以 30 秒这一单客户付 ¥6.00（0.20×30），内部走豆包 ¥3.00/次的成本 ——
// 差价是调度收益，不进客户账单。
//
// 源/目标/触发值全部从 seedanceroute_setting 读，改配置不用改代码。
//
// 不路由的情形（**都不是错误**，一律放行原请求）：
//   - 开关关闭（默认）或两条规则都没配全
//   - 模型名不匹配
//   - 时长规则：时长不等于触发值（含不传时长 → 默认 5 秒）
//   - 时长规则：带了参考视频 / 参考音频（豆包链路不支持，见 hasUnsupportedMedia）
//   - 时长规则：用户明确要了分辨率（豆包链路没有分辨率控件）
//   - **目标模型在当前分组下没有可用渠道**（见 hasAvailableChannel）
//   - 比例规则：用户明确要 1080P（目标模型交付 720P，做不到）
//   - 请求不是 JSON，或 body 解析/转换失败
func Seedance25TeHuiRoute() func(c *gin.Context) {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsAnyRouteEnabled() {
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
		body, ok := readJSONRequestBody(c)
		if !ok {
			c.Next()
			return
		}
		modelName, _ := body["model"].(string)
		if strings.TrimSpace(modelName) != policy.SourceModel {
			c.Next()
			return
		}

		// ── 规则一（时长）优先 ──
		if seedanceroute_setting.IsEnabled() {
			if seconds, hasSeconds := extractRequestSeconds(body); hasSeconds && seconds == policy.TriggerSeconds {
				if newBody, routed := prepareDoubaoRoute(c, body, policy); routed {
					if applyRewrittenBody(c, newBody, policy.SourceModel) {
						// 计费按源模型（按秒）算，而目标 adaptor 是按次的、不给 seconds。
						// 把判定命中的秒数记下来，交给 relay_task.go 补进 OtherRatios。
						common.SetContextKey(c, constant.ContextKeyRouteBillingSeconds, float64(seconds))
						c.Next()
						return
					}
				}
			}
		}

		// ── 规则二（比例）：规则一没命中才走这里 ──
		if seedanceroute_setting.IsRatioRouteEnabled() {
			if matchesTriggerRatio(body, policy.TriggerRatio) {
				if newBody, routed := prepareWan3Route(c, body, policy); routed {
					if applyRewrittenBody(c, newBody, policy.SourceModel) {
						c.Next()
						return
					}
				}
			}
		}

		c.Next()
	}
}

// readJSONRequestBody 读出请求体并解析成 map。
//
// 失败返回 false，调用方放行原请求 —— 解析不了就不该猜，更不该改写。
func readJSONRequestBody(c *gin.Context) (map[string]interface{}, bool) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, false
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, false
	}
	var body map[string]interface{}
	if err := common.Unmarshal(raw, &body); err != nil {
		return nil, false
	}
	return body, true
}

// applyRewrittenBody 把改写后的 body 换回 context 与 Request.Body。
//
// 失败返回 false（调用方放行原请求）。此时原 body 仍在 storage 里，
// 但 storage 的读位置可能已被 Bytes() 消费 —— GetBodyStorage 返回的
// storage 支持随机读，Bytes() 不改变语义；真正保险的是 apply 成功后才算路由成功。
func applyRewrittenBody(c *gin.Context, body map[string]interface{}, originModel string) bool {
	newRaw, err := common.Marshal(body)
	if err != nil {
		return false
	}
	newStorage, err := common.CreateBodyStorage(newRaw)
	if err != nil {
		return false
	}
	c.Set(common.KeyBodyStorage, newStorage)
	c.Request.Body = io.NopCloser(newStorage)
	c.Request.ContentLength = int64(len(newRaw))
	c.Set(routeOriginModelKey, originModel)
	return true
}

// prepareDoubaoRoute 组装「改派到豆包链路」的请求体。
//
// 第二个返回值 false = 放弃路由（放行原请求）。
func prepareDoubaoRoute(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy) (map[string]interface{}, bool) {
	// 目标链路（豆包官网）只支持参考图。带参考视频/音频时不路由 ——
	// 走源模型继续跑，而不是报错打断客户（老板 2026-10-30 定的口径）。
	if hasUnsupportedMedia(body) {
		return nil, false
	}

	// 用户明确要了分辨率 → 不路由。目标链路没有分辨率控件，
	// 路由过去只能给一份分辨率不对的片（或者被漫屋 400 掉）。
	// 宁可走源链路按用户要的分辨率跑。
	if hasExplicitResolution(body) {
		return nil, false
	}

	// 目标模型没有可用渠道 → 不路由。
	//
	// 为什么必须有这一步：中间件改写了 body 的 model 字段，之后的重试
	// （shouldRetryTaskRelay）只会**用同一个模型名重选渠道**，不会退回源模型。
	// 而 seedance-2-5 在生产只有渠道 20 一个渠道 —— 豆包账号全挂时
	// Distribute 直接返回 503「当前分组下没有可用渠道」，客户撞错误。
	// 预检之后这种请求会自然落回源链路（有赞），客户完全无感。
	if !channelAvailabilityProbe(c, policy.TargetModel) {
		return nil, false
	}

	return rewriteBodyForDoubao(body, policy)
}

// prepareWan3Route 组装「改派到 wan3.0-720p-特惠」的请求体。
//
// 与豆包规则的关键差别：wan3 渠道的入参契约与源链路**几乎一致**
// （buildWan3Media 的素材优先级同样是 顶层 media > metadata.media > images），
// 所以**不需要**转换参考图格式，也不需要摘掉 resolution / size。
func prepareWan3Route(c *gin.Context, body map[string]interface{}, policy seedanceroute_setting.Policy) (map[string]interface{}, bool) {
	// 目标模型固定交付 720P（480P 生成 + 提升）。用户明确要 1080P 时做不到，
	// 路由过去等于降级交付 —— 宁可不路由，交回源链路按用户要的跑。
	if wants1080P(body) {
		return nil, false
	}

	if !channelAvailabilityProbe(c, policy.RatioTargetModel) {
		return nil, false
	}

	// 参考视频挡板：wan3 比源链路严格，超限会在提交阶段 400。
	//
	// 两边的限制并不相同（2026-10-31 实测更正，此前以为源链路不限时长）：
	//	· 源链路 youzanwan3：单条 ≤15 秒（上游原文「参考素材时长过长：当前为
	//	  31.609 秒，最长允许 15 秒」），但**不限制「参考+生成」总量**；
	//	· 目标链路 wan3：单条 ≤15 秒、总量 ≤15 秒，且「输入视频总时长 + 输出
	//	  视频时长 ≤30 秒」。
	//
	// 所以这里的价值集中在后两条：单条超 15 秒时两边都会拒，改不改派都失败，
	// 但「参考+生成 >30 秒」只有 wan3 拒 —— 挡掉它，请求落回源链路还能跑成功。
	// 生产实例：2026-10-31 01:10，logs id=9379，参考视频 32.39 秒。
	//
	// 代价：带参考视频的 9:16 请求会多一次视频下载。moov 在头部时几十 KB 即可
	// 拿到 mvhd，但在尾部时要下全文件（探测上限 64MB）—— 所以 wan3 包给单条
	// 10 秒、整组 30 秒的超时预算，超时按「预检不通过」处理，交回源链路。
	// 不带参考视频的请求（绝大多数）不触发，零开销。
	if outOfRange, reason := wan3ReferenceVideosOutOfRange(body); outOfRange {
		logger.LogInfo(c, "条件路由跳过 wan3，交回源链路："+reason)
		return nil, false
	}

	return rewriteBodyForWan3(body, policy), true
}

// wan3ReferenceVideosOutOfRange 判断请求里的参考视频是否超出 wan3 的硬限制。
//
// 返回 true 表示**不能改派**（交回源链路），reason 是给日志看的原因。
//
// 三个失败都按「不改派」处理 —— 与整个中间件的失败原则一致：
// 任何一步不确定就放弃路由，绝不挡客户。这里尤其如此：
// 探测失败意味着无法判断会不会超限，而改派过去一旦超限就是一次必然的 400。
func wan3ReferenceVideosOutOfRange(body map[string]interface{}) (bool, string) {
	urls := collectReferenceVideos(body)
	if len(urls) == 0 {
		return false, ""
	}
	_, realSeconds, err := wan3.ProbeReferenceVideos(urls)
	if err != nil {
		// 单条超 15 秒也走这里：probeReferenceVideoSeconds 内部已校验单条上限，
		// 超限时返回的是 "exceeds the 15s per-video limit" 错误，不是网络错误。
		// 所以文案用中性表述，具体原因由 err 带出 —— 写成「探测失败」会让运维
		// 以为是下载不通，实际多半是视频本身超限。
		return true, "参考视频预检不通过：" + err.Error()
	}
	if realSeconds > float64(wan3.MaxReferenceVideoTotalSeconds)+routeDurationEpsilon {
		return true, fmt.Sprintf("参考视频合计 %.2f 秒超过 %d 秒上限",
			realSeconds, wan3.MaxReferenceVideoTotalSeconds)
	}
	generation := routeGenerationSeconds(body)
	if realSeconds+float64(generation) > float64(wan3.MaxTotalDurationWithVideo)+routeDurationEpsilon {
		return true, fmt.Sprintf("参考视频 %.2f 秒 + 生成 %d 秒超过 %d 秒上限",
			realSeconds, generation, wan3.MaxTotalDurationWithVideo)
	}
	return false, ""
}

// routeDurationEpsilon 是浮点比较容差，与 wan3 包内部的 durationEpsilon 同值。
//
// mvhd 的 duration/timescale 除法会带出 15.0000000001 这种尾巴，
// 严格大于会把「刚好 15 秒」误判成超限。
const routeDurationEpsilon = 1e-6

// routeGenerationSeconds 取请求的**生成时长**，口径与 wan3 的 resolveDuration 一致。
//
// 必须钳制：不传时长时上游默认 5 秒（不是 0，也不是 30）。用 0 会让
// 「参考视频 15 秒 + 默认 5 秒 = 20 ≤ 30」这类本该放行的请求被误挡；
// 用 30 会让所有带参考视频的请求都被挡。
func routeGenerationSeconds(body map[string]interface{}) int {
	const (
		routeMinDuration     = 2
		routeMaxDuration     = 30
		routeDefaultDuration = 5
	)
	seconds, ok := extractRequestSeconds(body)
	if !ok || seconds <= 0 {
		return routeDefaultDuration
	}
	if seconds < routeMinDuration {
		return routeMinDuration
	}
	if seconds > routeMaxDuration {
		return routeMaxDuration
	}
	return seconds
}

// collectReferenceVideos 收集请求里的参考视频 URL 列表。
//
// ⚠️ 必须与 wan3 的 buildWan3Media **逐条对齐**，否则挡板会判错方向。
// buildWan3Media 的优先级是：
//
//	顶层 media 非空      → 只看顶层（哪怕里面全是图片）
//	否则 metadata.media 非空 → 只看 metadata
//	否则 legacy images / image / input_reference，按地址扩展名推断类型
//
// 写成「顶层 media 里有视频才看顶层」是错的：顶层 media 只有图片、
// metadata.media 里挂着视频时，实际提交给 wan3 的只有那些图片（视频根本不发），
// 我们却会去探测 metadata 里的视频，把一条本来合规的请求挡掉。
//
// 反过来漏掉 legacy 分支同样危险：客户用 images:["a.mp4"] 传视频时，
// buildWan3Media 会把它推断成 reference_video 发给上游，这里却看不见它 ——
// 挡板失效，超长视频照样被改派到 wan3 撞 400（就是这次要修的那个故障）。
func collectReferenceVideos(body map[string]interface{}) []string {
	if media, ok := body["media"].([]interface{}); ok && len(media) > 0 {
		return referenceVideosFromMedia(media)
	}
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		if media, ok := metadata["media"].([]interface{}); ok && len(media) > 0 {
			return referenceVideosFromMedia(media)
		}
	}
	return legacyReferenceVideos(body)
}

// legacyReferenceVideos 按 buildWan3Media 的 legacy 规则挑出视频地址：
// images 优先，其次 image，再次 input_reference；类型由地址推断。
func legacyReferenceVideos(body map[string]interface{}) []string {
	legacy := stringListValue(body["images"])
	if len(legacy) == 0 {
		legacy = stringListValue(body["image"])
	}
	if len(legacy) == 0 {
		legacy = stringListValue(body["input_reference"])
	}
	var videos []string
	for _, url := range legacy {
		if isLegacyVideoURL(url) {
			videos = append(videos, url)
		}
	}
	return videos
}

// stringListValue 把字符串或字符串数组统一成 []string（丢掉空串）。
func stringListValue(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return []string{trimmed}
		}
	case []interface{}:
		var out []string
		for _, item := range typed {
			out = append(out, stringListValue(item)...)
		}
		return out
	}
	return nil
}

// isLegacyVideoURL 与 wan3 的 inferLegacyMediaType 同口径：
// 只认 data:video/ 前缀与 .mp4 / .mov 后缀（忽略查询串）。
func isLegacyVideoURL(url string) bool {
	lower := strings.ToLower(strings.TrimSpace(url))
	if strings.HasPrefix(lower, "data:video/") {
		return true
	}
	path := strings.Split(lower, "?")[0]
	return strings.HasSuffix(path, ".mp4") || strings.HasSuffix(path, ".mov")
}

// referenceVideosFromMedia 从一个 media 数组里挑出 reference_video 的地址。
func referenceVideosFromMedia(value interface{}) []string {
	media, ok := value.([]interface{})
	if !ok {
		return nil
	}
	var videos []string
	for _, item := range media {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mediaType, _ := entry["type"].(string)
		if strings.TrimSpace(mediaType) != "reference_video" {
			continue
		}
		if url, ok := entry["url"].(string); ok && strings.TrimSpace(url) != "" {
			videos = append(videos, strings.TrimSpace(url))
		}
	}
	return videos
}

// channelAvailabilityProbe 判断「目标模型在当前分组下是否还有可用渠道」。
//
// 声明成变量而不是直接调 hasAvailableChannel，**只为测试可替换**：
// 真实实现要读渠道缓存（group2model2channels）或查数据库，
// 单测里没有这些环境，硬造一个渠道缓存会把测试和全局状态耦合在一起。
// 生产代码路径永远走默认值 hasAvailableChannel，没有任何分支。
var channelAvailabilityProbe = hasAvailableChannel

// hasAvailableChannel 预检「该模型在当前分组下是否还有可用渠道」。
//
// 用 GetRandomSatisfiedChannel 而不是真的选中渠道：它只读缓存
// （group2model2channels）并按权重挑一个，不占用负载计数，可以安全地当探针用。
// 返回 nil 且无错误 = 该模型没有可用渠道。
//
// ⚠️ 传 requestPath 是故意的：渠道支持哪些路径也是可用性的一部分
// （filterChannelsByRequestPath），只按模型名判断会高估可用性。
func hasAvailableChannel(c *gin.Context, modelName string) bool {
	if strings.TrimSpace(modelName) == "" {
		return false
	}
	group := common.GetContextKeyString(c, constant.ContextKeyUsingGroup)
	if group == "" {
		group = "default"
	}
	channel, err := model.GetRandomSatisfiedChannel(group, modelName, 0, c.Request.URL.Path)
	if err != nil {
		return false
	}
	return channel != nil
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
//
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
//     - 顶层 resolution 透传下去 → 漫屋 rejectFields 显式 400
//     （"does not support resolution"）→ **客户 30 秒任务直接失败**；
//     - 顶层 size 透传下去 → 漫屋把 size 当**比例**读
//     （firstNonEmpty(Size, Ratio, AspectRatio)），"720P" 不在
//     allowedDoubaoRatios 里 → 同样 400。
//     所以顶层 resolution / size 必须**摘掉**。
//     而 metadata.resolution 表达的是「用户明确要 720P/1080P」——
//     目标链路做不到这个事，**不能假装做到了**：带分辨率的请求一律不路由，
//     交回源链路按用户要的分辨率跑（宁可不路由，也不给一份分辨率不对的片）。
func rewriteBodyForDoubao(body map[string]interface{}, policy seedanceroute_setting.Policy) (map[string]interface{}, bool) {
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

// rewriteBodyForWan3 组装改派到 wan3.0-720p-特惠 的请求体。
//
// 与豆包规则的差别：wan3 渠道的入参契约和源链路（有赞 Wan3）**同源**，
// 所以只做最小改动 —— 换模型名 + 把比例落到 metadata.ratio，
// 其余字段（media / resolution / size / prompt / seconds）原样保留。
//
// 为什么素材不用转换：relay/channel/task/wan3 的 buildWan3Media 优先级是
// 顶层 media > metadata.media > images/image/input_reference，
// 与 youzanwan3 的 buildMedia **完全一致**，原样传过去不会丢素材。
// （这与豆包规则相反 —— 豆包只认顶层 input_reference，必须转换。）
//
// 为什么分辨率不用摘：wan3 的 resolveGenerationResolution 对
// wan3.0-720p-特惠 强制 480P 生成（再提升到 720P），用户传 480P/720P
// 都不会报错也不会多花钱；1080P 已在 prepareWan3Route 里挡掉。
func rewriteBodyForWan3(body map[string]interface{}, policy seedanceroute_setting.Policy) map[string]interface{} {
	out := make(map[string]interface{}, len(body)+1)
	for key, value := range body {
		if key == "model" {
			continue
		}
		out[key] = value
	}
	out["model"] = policy.RatioTargetModel

	// 比例统一落到 metadata.ratio：wan3 的 resolveRatio 读 metadata.ratio /
	// metadata.aspect_ratio。统一写 ratio 是因为判定时可能命中的是
	// 顶层字段或 size 推断，而源链路只认 metadata 里的值。
	metadata := make(map[string]interface{})
	if existing, ok := body["metadata"].(map[string]interface{}); ok {
		for key, value := range existing {
			metadata[key] = value
		}
	}
	metadata["ratio"] = policy.TriggerRatio
	out["metadata"] = metadata
	return out
}

// matchesTriggerRatio 判断请求里的比例是否命中触发值。
func matchesTriggerRatio(body map[string]interface{}, trigger string) bool {
	want := normalizeRatioText(trigger)
	if want == "" {
		return false
	}
	got, ok := resolveRequestRatio(body)
	if !ok {
		return false
	}
	return got == want
}

// resolveRequestRatio 从请求里推断比例。
//
// 读取顺序与源链路一致：metadata.ratio / metadata.aspect_ratio 优先，
// 其次顶层同名字段，最后从 size（"720x1280" 这类）推断。
//
// 注意源链路 youzanwan3 只认 metadata 里的比例，顶层会被 Go 解码器静默丢弃；
// 这里多读顶层是**为了判定**（客户端真传了 9:16 就该算命中），
// 而不是为了让顶层值生效 —— 改写时统一写进 metadata.ratio。
func resolveRequestRatio(body map[string]interface{}) (string, bool) {
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := metadata[key].(string); ok {
				if ratio := normalizeRatioText(value); ratio != "" {
					return ratio, true
				}
			}
		}
	}
	for _, key := range []string{"ratio", "aspect_ratio"} {
		if value, ok := body[key].(string); ok {
			if ratio := normalizeRatioText(value); ratio != "" {
				return ratio, true
			}
		}
	}
	if value, ok := body["size"].(string); ok {
		if ratio, inferred := ratioFromSizeText(value); inferred {
			return ratio, true
		}
	}
	if metadata != nil {
		if value, ok := metadata["size"].(string); ok {
			if ratio, inferred := ratioFromSizeText(value); inferred {
				return ratio, true
			}
		}
	}
	return "", false
}

// normalizeRatioText 把 "9:16" / "9：16" / "9/16" 这类写法归一成 "9:16"。
//
// **不认** adaptive / auto 这类档位名（返回空）—— 它们不是具体比例，
// 不该被当成触发比例命中。
func normalizeRatioText(value string) string {
	text := strings.TrimSpace(value)
	if text == "" {
		return ""
	}
	text = strings.ReplaceAll(text, "：", ":")
	text = strings.ReplaceAll(text, "/", ":")
	parts := strings.Split(text, ":")
	if len(parts) != 2 {
		return ""
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ""
	}
	return strconv.Itoa(w) + ":" + strconv.Itoa(h)
}

// ratioFromSizeText 把 "1280x720" / "1280*720" 这类尺寸映射到最接近的比例。
//
// 误差超过 0.25 就认为不是常见比例（返回 false），避免把任意尺寸硬塞进某一档。
// 与 relay/channel/task/wan3 的 ratioFromSize 同口径。
func ratioFromSizeText(value string) (string, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), "*", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	candidates := []struct {
		name  string
		value float64
	}{
		{"16:9", 16.0 / 9.0},
		{"9:16", 9.0 / 16.0},
		{"4:3", 4.0 / 3.0},
		{"3:4", 3.0 / 4.0},
		{"1:1", 1.0},
	}
	actual := float64(w) / float64(h)
	best := ""
	bestDiff := math.MaxFloat64
	for _, candidate := range candidates {
		if diff := math.Abs(actual - candidate.value); diff < bestDiff {
			bestDiff = diff
			best = candidate.name
		}
	}
	if bestDiff > 0.25 {
		return "", false
	}
	return best, true
}

// wants1080P 判断用户是否**明确要求了 1080P**。
//
// 规则二的目标模型 wan3.0-720p-特惠 固定交付 720P（480P 生成 + 提升），
// 用户要 1080P 时路由过去就是降级交付 → 不路由，交回源链路按用户要的跑。
//
// 480P / 720P 不挡：480P 需求被 720P 成片满足（升级），720P 需求正好命中。
func wants1080P(body map[string]interface{}) bool {
	for _, key := range []string{"resolution", "size"} {
		if explicitResolutionTier(body[key]) == "1080P" {
			return true
		}
	}
	metadata, ok := body["metadata"].(map[string]interface{})
	if !ok {
		return false
	}
	for _, key := range []string{"resolution", "size"} {
		if explicitResolutionTier(metadata[key]) == "1080P" {
			return true
		}
	}
	return false
}

// explicitResolutionTier 把一个分辨率取值归一成 480P / 720P / 1080P。
//
// 支持档位字面量（"720P"）与像素尺寸（"1280x720"）。无法解析返回空串。
// 尺寸按**短边**分档，与 relay/channel/task/wan3 的 resolutionFromSize 同口径。
func explicitResolutionTier(value interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	switch strings.ToUpper(strings.TrimSpace(text)) {
	case "480", "480P":
		return "480P"
	case "720", "720P":
		return "720P"
	case "1080", "1080P":
		return "1080P"
	}
	normalized := strings.ReplaceAll(strings.TrimSpace(text), "*", "x")
	parts := strings.Split(normalized, "x")
	if len(parts) != 2 {
		return ""
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return ""
	}
	short := w
	if h < short {
		short = h
	}
	switch {
	case short <= 600:
		return "480P"
	case short <= 900:
		return "720P"
	default:
		return "1080P"
	}
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
