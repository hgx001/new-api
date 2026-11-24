package middleware

// hk-wan3 → 客美 tj 的手动改派（2026-11-22 用户需求）。
//
//	客户请求  hk-wan3             渠道 17 有赞（type 17）
//	内部改名为 wan3.0-video-tj    渠道 32 客美 Kemeitv（type 71）
//
// 开关语义（见 setting/seedanceroute_setting/wan3_hk_wan3_tj.go）：
//
//	打开 = 不带参考视频的 hk-wan3 请求改派客美 tj；
//	关闭（默认）= 完全不介入，走 hk-wan3，行为与加这个开关之前逐字节一致。
//
// ── 只改派「不带参考视频」的请求 ──
//
// 客美自 2026-11-20 起把参考视频秒数并入计费（生成秒 + 参考视频秒），
// hk-wan3 不收这部分钱。带参考视频的请求改派过去会让同一个模型名的账单变高，
// 与「改派不改变对客价格」的口径冲突 —— 这类请求留在 hk-wan3。
//
// ── 三处必须改写的地方 ──
//
//  1. 时长：hk-wan3 认顶层 `duration` 与字符串 `seconds`、缺省 5 秒、区间 4–30；
//     客美只认 `duration`、缺省 5 秒、区间 **5–30**（2026-11-24 起下限收到 5 秒，
//     因为上游对 <5 秒仍按 5 秒出片计费）。→ 按 hk-wan3 口径算出秒数显式写入
//     duration；低于客美下限（4 秒）或越界则不拦截而是不改派（让 hk-wan3 原样处理）。
//  2. 档位倍率：hk-wan3 是 480P/720P/1080P = 1 / 0.25÷0.18 / 0.48÷0.18，
//     客美是 1 / 2 / 4。不覆盖就按客美倍率收费（720P 多收 44%、1080P 多收 50%）。
//     → 写 ContextKeyRouteBillingSizeRatio。
//  3. 画幅 auto / adaptive：客美档位表里没有，解析不到会回落 16:9 ——
//     等于替客户钉死一个画幅。带这两个值的请求不改派。

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/kemeitv"
	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// hk-wan3 自己的缺省时长（上游文档 5 秒；可路由区间取
// [kemeitv.MinDurationSeconds, kemeitv.MaxDurationSeconds]，即客美的 5–30 秒）。
const hkDefaultDuration = 5

// HkWan3TjRoute 把 hk-wan3 的请求改派到 wan3.0-video-tj。
//
// 必须挂在 Distribute() 之前（渠道按 body 的 model 选）与
// CaptureTaskRequestParams() 之后（日志要展示客户原始参数）。
func HkWan3TjRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsWan3HkWan3TjEnabled() {
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
		if sourceModel != seedanceroute_setting.Wan3HkModel {
			c.Next()
			return
		}

		// 已被前置规则改派过的请求不二次改派：计费原点会被覆盖，账单口径静默变化。
		if origin := RouteOriginModel(c); origin != "" {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过 hk-wan3→客美改派：请求已由前置规则改派（计费原点 %s），不二次改派", origin))
			c.Next()
			return
		}

		targetModel := seedanceroute_setting.Wan3TjTargetModel

		// 画幅 auto / adaptive：客美没有这一档，解析不到会回落 16:9。
		if ratio, hasRatio := resolveTehuiRatioGateRatio(body); hasRatio && isAdaptiveRatioText(ratio) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过 hk-wan3→客美改派：请求画幅 %s 在客美档位表里没有对应项（会回落 16:9），保持 hk-wan3 链路",
				ratio))
			c.Next()
			return
		}

		var mediaReq relaycommon.TaskSubmitReq
		var duration int
		if raw, err := common.Marshal(body); err == nil && common.Unmarshal(raw, &mediaReq) == nil {
			// 带参考视频：客美会把参考视频秒数并入计费，hk-wan3 不收 —— 不改派。
			if hasReferenceVideoRequest(mediaReq) {
				logger.LogInfo(c, "条件路由跳过 hk-wan3→客美改派：请求带参考视频（客美会把它并入计费），保持 hk-wan3 链路")
				c.Next()
				return
			}
			// 素材过不了客美的校验（数量上限 / 首尾帧互斥 / 后缀白名单 …）：
			// hk-wan3 可能照常出片，改派过去则是硬 400 —— 不把能跑的请求变成报错。
			if err := kemeitv.ValidateIncomingMedia(mediaReq); err != nil {
				logger.LogInfo(c, fmt.Sprintf(
					"条件路由跳过 hk-wan3→客美改派：目标链路的素材校验不通过（%v），保持 hk-wan3 链路", err))
				c.Next()
				return
			}
			duration = hkRequestDuration(mediaReq)
		} else {
			duration = hkDefaultDuration
		}

		// 客美下限 5 秒（2026-11-24 起）：hk-wan3 的 4 秒请求改派过去会被本地 400，
		// 低于下限就留在 hk-wan3；超出 hk-wan3 自己的 30 秒上限同样不接。
		if duration < kemeitv.MinDurationSeconds || duration > kemeitv.MaxDurationSeconds {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过 hk-wan3→客美改派：时长 %d 秒不在客美 %d–%d 秒区间（应留在 hk-wan3）",
				duration, kemeitv.MinDurationSeconds, kemeitv.MaxDurationSeconds))
			c.Next()
			return
		}

		// 目标模型没有可用渠道 → 不路由，让请求自然落回 hk-wan3。
		if !channelAvailabilityProbe(c, targetModel) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过 hk-wan3→客美改派：目标 %s 在当前分组下没有可用渠道", targetModel))
			c.Next()
			return
		}

		rewritten := make(map[string]interface{}, len(body)+2)
		for key, value := range body {
			rewritten[key] = value
		}
		rewritten["model"] = targetModel
		rewritten["duration"] = duration

		if !applyRewrittenBody(c, rewritten, sourceModel) {
			c.Next()
			return
		}

		// 档位倍率按源模型（hk-wan3）口径覆盖：客美 adaptor 给的是它自己的 1/2/4。
		resolution, err := kemeitv.RequestResolution(mediaReq)
		if err != nil || resolution == "" {
			resolution = "720P"
		}
		common.SetContextKey(c, constant.ContextKeyRouteBillingSizeRatio,
			youzanwan3.RouteBillingSizeRatio(sourceModel, resolution))

		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 改派到 %s（渠道 32 客美；%s 计费，%d 秒）",
			sourceModel, targetModel, sourceModel, duration))
		c.Next()
	}
}

// hkRequestDuration 按 hk-wan3 口径解析时长：duration > seconds > 缺省 5 秒。
//
// 与 youzanwan3 adaptor 的 resolveDurationForModel 同源口径（那个函数未导出），
// 只提供本路由需要的 hk-wan3 分支。
func hkRequestDuration(req relaycommon.TaskSubmitReq) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(strings.TrimSpace(req.Seconds))
	}
	if duration <= 0 {
		return hkDefaultDuration
	}
	return duration
}
