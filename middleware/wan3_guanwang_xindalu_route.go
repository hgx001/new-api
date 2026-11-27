package middleware

// 万相官网 → wan3-新大陆 的手动改派（2026-11-27 用户需求）。
//
//	客户请求  wan3.0-video-官网   渠道 12 DashScope-Wan3（type 60）
//	内部改名为 wan3-新大陆        渠道 34 二春 Erchun（type 65，上游 Wan3.0 新线路）
//
// 与 wan3_guanwang_tj_route.go 同构，差异只有目标链路与一条素材约束：
//
//   - 目标支持 480P/720P/1080P 三档（与官网一致），adaptive 画幅也支持，
//     所以不需要像客美那样为「adaptive 回落 16:9」而放弃改派；
//   - 目标对参考视频按「生成秒数 + 参考视频实测秒数」计费，而官网不读素材、
//     只按生成秒数收 —— 为保证改派不改变对客账单，带参考视频的请求留在官网。
//
// 计费口径与客美改派完全一致：客户请求哪个模型就按哪个模型收费，这里只写
// routeOriginModel，并把计费档位倍率覆盖成官网那张表（1 / 2.25 / 4）。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/dashscope"
	"github.com/QuantumNous/new-api/relay/channel/task/erchun"
	"github.com/QuantumNous/new-api/relay/channel/task/wan3"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// Wan3GuanwangXindaluRoute 把 wan3.0-video-官网 的请求改派到 wan3-新大陆。
//
// 必须挂在 Distribute() 之前、CaptureTaskRequestParams() 之后，
// 理由与客美改派逐条相同（见 wan3_guanwang_tj_route.go 包头注释）。
func Wan3GuanwangXindaluRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsWan3GuanwangXindaluEnabled() {
			c.Next()
			return
		}
		// 历史 option 里两个开关可能都是 true（写入时会互斥，但旧数据不保证）：
		// 此时让更早存在、正在承载流量的客美开关优先，避免升级瞬间静默换货。
		if seedanceroute_setting.IsWan3GuanwangTjEnabled() {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
			c.Next()
			return
		}
		// /remix 的模型由原任务决定，改名会破坏 remix 语义。
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
		sourceModel := strings.TrimSpace(modelName)
		if sourceModel != seedanceroute_setting.Wan3GuanwangModel {
			c.Next()
			return
		}

		// 已被前置规则改派到官网的请求不二次改派（计费原点会被覆盖）。
		if origin := RouteOriginModel(c); origin != "" {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过官网→新大陆改派：请求已由前置规则改派到官网（计费原点 %s），不二次改派", origin))
			c.Next()
			return
		}

		targetModel := seedanceroute_setting.Wan3XindaluTargetModel

		// 带参考视频 / 素材过不了目标链路校验 → 留在官网。
		// 两条都是为了避免「静默改价」或「静默变 400」。
		var mediaReq relaycommon.TaskSubmitReq
		if raw, err := common.Marshal(body); err == nil && common.Unmarshal(raw, &mediaReq) == nil {
			if hasReferenceVideoRequest(mediaReq) {
				logger.LogInfo(c, "条件路由跳过官网→新大陆改派：请求带参考视频（新大陆会把它并入计费），保持官网链路")
				c.Next()
				return
			}
			if err := erchun.ValidateIncomingMedia(mediaReq); err != nil {
				logger.LogInfo(c, fmt.Sprintf(
					"条件路由跳过官网→新大陆改派：目标链路的素材校验不通过（%v），保持官网链路", err))
				c.Next()
				return
			}
		}

		// 目标模型没有可用渠道 → 不路由，让请求自然落回官网。
		if !channelAvailabilityProbe(c, targetModel) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过官网→新大陆改派：目标 %s 在当前分组下没有可用渠道", targetModel))
			c.Next()
			return
		}

		rewritten, requested := wan3GuanwangXindaluRewriteBody(body)
		if !applyRewrittenBody(c, rewritten, sourceModel) {
			c.Next()
			return
		}
		// 计费仍按官网价：覆盖目标 adaptor 给的分辨率倍率（1 / 1.739 / 3.623）
		// 为官网那张表（1 / 2.25 / 4，¥0.20 / ¥0.45 / ¥0.80 每秒）。
		common.SetContextKey(c, constant.ContextKeyRouteBillingSizeRatio,
			wan3.OfficialBillingSizeRatio(requested.resolution))

		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 改派到 %s（渠道 34 二春；%s 计费，%d 秒，%s）",
			sourceModel, targetModel, sourceModel, requested.duration, requested.resolution))
		c.Next()
	}
}

// wan3GuanwangXindaluRewriteBody 返回改写后的请求体与按源渠道口径解析的规格。
//
// 只改 model 并写入显式 duration：新大陆与官网的时长区间一致（2–30 秒），
// 但显式写入可避免目标 adaptor 对「顶层 seconds」与「duration」的读取差异。
func wan3GuanwangXindaluRewriteBody(body map[string]interface{}) (map[string]interface{}, wan3GuanwangRequested) {
	var req relaycommon.TaskSubmitReq
	var requested wan3GuanwangRequested
	if raw, err := common.Marshal(body); err == nil && common.Unmarshal(raw, &req) == nil {
		requested.duration = dashscope.RequestDuration(req)
		requested.resolution = wan3.ParseResolution(req)
	} else {
		requested.duration = 5
		requested.resolution = "480P"
	}

	rewritten := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		rewritten[key] = value
	}
	rewritten["model"] = seedanceroute_setting.Wan3XindaluTargetModel
	rewritten["duration"] = requested.duration
	return rewritten, requested
}
