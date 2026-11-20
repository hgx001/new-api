package middleware

// 万相官网 → 客美 tj 的手动改派（2026-11-19 用户需求）。
//
//	客户请求  wan3.0-video-官网   渠道 12 DashScope-Wan3（type 60）
//	内部改名为 wan3.0-video-tj    渠道 32 客美 Kemeitv（type 71）
//
// 为什么单独一个中间件、不塞进 Seedance25TeHuiRoute：
//
//	那个中间件按**促销族源模型**（seedance2.5-特惠 / -活动）做早退，源模型
//	不匹配就直接放行。把万相塞进它的源模型清单，会顺带激活规则一~八
//	（时长/比例/电商/全量兜底……）—— 那些规则的改写只适配了有赞与 CapCut
//	两条契约，套到万相身上会把「官网 10 秒的片子」改成别的档位。
//
// 开关语义（见 setting/seedanceroute_setting/wan3_guanwang_tj.go）：
//
//	打开 = 所有 wan3.0-video-官网 的请求改派客美 tj；
//	关闭 = 完全不介入，走官网渠道，行为与加这个开关之前逐字节一致。
//
// 为什么不看 Policy.Enabled（规则总开关）：两者是不同商品的不同开关，
// 连坐只会制造「按钮亮着、实际没生效」的静默故障。
//
// ── 计费口径（老板 2026-10-30 定稿的通用规则）──
//
// 改派不改变对客价格：客户请求哪个模型，就按哪个模型收费。所以这里
// 只写 routeOriginModel（applyRewrittenBody 负责），价格与日志都按
// wan3.0-video-官网 走。（官网 480P ¥0.20/秒 vs 客美 tj 成本 ¥0.138/秒，
// 差额是本站毛利 —— 不是「打折卖」，客户账单完全不变。）
//
// ── 三处**必须**改写而不是只改模型名的地方 ──
//
//  1. 档位倍率：官网 480P/720P/1080P 的倍率是 1 / 1.955 / 3.148，客美是
//     1 / 1.81 / 4。不覆盖就会把官网 1080P 按 4 倍收（¥0.80/秒，官网口径
//     ¥0.63/秒，多收 27%）。→ 写 ContextKeyRouteBillingSizeRatio。
//  2. 时长：官网 adaptor 会**钳制**越界时长（1 → 2 秒、60 → 30 秒）并且
//     认顶层 `seconds`；客美是直接 400，且 resolveDuration **不读**顶层
//     seconds。→ 显式写入 duration（本渠道口径算出来的秒数）。
//  3. 画幅 adaptive：官网支持 adaptive（交给上游自适应），客美的档位表里
//     没有它，解析不到会回落 16:9 —— 等于替客户钉死了一个画幅。
//     → 带 adaptive 的请求**不改派**，留在官网。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/dashscope"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// Wan3GuanwangTjRoute 把 wan3.0-video-官网 的请求改派到 wan3.0-video-tj。
//
// 必须挂在 Distribute() **之前**：渠道是按 body 的 model 选的，
// 改名之后选渠道、挑 adaptor、算价格全都自动跟着走。
// 也必须挂在 CaptureTaskRequestParams() **之后**：日志详情要展示客户原始参数。
func Wan3GuanwangTjRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsWan3GuanwangTjEnabled() {
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

		// 已经被前面的规则改派到官网的请求（当前是规则三「首尾帧」→
		// frames_target_model = wan3.0-video-官网）：**不二次改派**。
		//
		// 理由：
		//  1. 计费原点已被那条规则写成**客户真正请求的模型**（如 seedance2.5-特惠），
		//     本中间件再改一次名会把原点覆盖成官网 —— 账单口径静默变化；
		//  2. 客户请求的本就不是官网模型，换货开关不该推翻别的规则已经做出的结论
		//     （否则一条请求被两层规则来回翻，排查时无法还原到底谁出的手）。
		if origin := RouteOriginModel(c); origin != "" {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过官网→客美改派：请求已由前置规则改派到官网（计费原点 %s），不二次改派", origin))
			c.Next()
			return
		}

		targetModel := seedanceroute_setting.Wan3TjTargetModel

		// 画幅 adaptive：客美没有这一档，解析不到会回落 16:9。
		// 静默改画幅比「留在官网」更糟 —— 客户付的是官网价，拿到的却是被钉死的比例。
		if ratio, hasRatio := resolveTehuiRatioGateRatio(body); hasRatio && isAdaptiveRatioText(ratio) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过官网→客美改派：请求画幅 %s 在客美档位表里没有对应项（会回落 16:9），保持官网链路",
				ratio))
			c.Next()
			return
		}

		// 目标模型没有可用渠道 → 不路由。
		//
		// 改写之后再失败只会报「tj 没有可用渠道」，客户看到的是「你买的官网模型
		// 不存在」；预检能让请求自然落回官网链路，客户完全无感。
		if !channelAvailabilityProbe(c, targetModel) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过官网→客美改派：目标 %s 在当前分组下没有可用渠道", targetModel))
			c.Next()
			return
		}

		// 时长按**本渠道口径**钉死（钳制 + 认顶层 seconds），见包头注释第 2 条。
		rewritten, requested := wan3GuanwangRewriteBody(body)

		if !applyRewrittenBody(c, rewritten, sourceModel) {
			c.Next()
			return
		}
		// 档位倍率按源模型口径覆盖目标 adaptor 给的那一份（见包头注释第 1 条）。
		common.SetContextKey(c, constant.ContextKeyRouteBillingSizeRatio,
			dashscope.BillingSizeRatioForResolution(requested.resolution))

		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 改派到 %s（渠道 32 客美；%s 计费，%d 秒）",
			sourceModel, targetModel, sourceModel, requested.duration))
		c.Next()
	}
}

// wan3GuanwangRequested 是「按源渠道口径解析出来的请求规格」。
type wan3GuanwangRequested struct {
	// duration 是钳制后的时长（秒）。
	duration int
	// resolution 是请求声明的分辨率档位；空串 = 没声明（计费按默认 480P）。
	resolution string
}

// wan3GuanwangRewriteBody 返回改写后的请求体，以及按源渠道口径解析出的规格。
//
// 只做两件事：
//   - model 换成目标模型；
//   - 写入显式的 duration（客美只认 duration，且不做钳制）。
//
// 其余字段（prompt / resolution / ratio / images / videos / audios / metadata）
// 原样保留：两个渠道的入参契约同源，客美 adaptor 自己会再解析一遍。
func wan3GuanwangRewriteBody(body map[string]interface{}) (map[string]interface{}, wan3GuanwangRequested) {
	// 用与真实请求链路同一套解码（TaskSubmitReq 有自定义 UnmarshalJSON，
	// 负责 duration/seconds 的数字与字符串两种写法），再交给 dashscope 的
	// 导出函数算钳制后的秒数与分辨率档 —— 与官网 adaptor 逐字节一致。
	var req relaycommon.TaskSubmitReq
	var requested wan3GuanwangRequested
	if raw, err := common.Marshal(body); err == nil && common.Unmarshal(raw, &req) == nil {
		requested.duration = dashscope.RequestDuration(req)
		requested.resolution = dashscope.ResolutionFromRequest(req)
	} else {
		// 解析失败就退回缺省时长（与官网 adaptor 的缺省一致），不因此放弃路由 ——
		// 客户请求体本身没坏（readJSONRequestBody 已经成功），坏的只是规格解析。
		requested.duration = 5
	}

	rewritten := make(map[string]interface{}, len(body)+2)
	for key, value := range body {
		rewritten[key] = value
	}
	rewritten["model"] = seedanceroute_setting.Wan3TjTargetModel
	rewritten["duration"] = requested.duration
	return rewritten, requested
}
