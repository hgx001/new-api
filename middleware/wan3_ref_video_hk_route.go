package middleware

// 带参考视频的万相请求 → hk-wan3（2026-11-20 用户需求）。
//
//	客户请求  wan3.0-video-官网 / wan3.0-video-tj / wan3.0-video-prime-tj
//	且带 reference_video
//	内部改名为 hk-wan3（渠道 17 有赞 WAN3 官渠）
//
// 为什么要这条路由（三条链路对参考视频的处置完全不同）：
//
//	官网   DashScope 适配器只下发 prompt/resolution/ratio/duration，
//	       参考图/视频/音频**整段丢弃** —— 客户传了素材却拿不到带素材的片；
//	tj     客美 2026-11-20 起参考视频秒数**并入计费**（多收钱）；
//	hk-wan3 有赞官渠**不收参考视频的钱**，且真的收 reference_video。
//
// 所以带参考视频的请求走 hk-wan3：素材真正生效，客户也不为参考视频多付钱。
//
// ── 与其它中间件的顺序 ──
//
// 排在最前（在任何「万相官网站→客美 tj」与促销族规则之前）：
// 它判的是**客户原始请求**的特征（带不带参考视频），先定出口就不再让别人改写。
// 已被前置规则改派过的请求（RouteOriginModel 非空）一律放行。
//
// ── 计费口径 ──
//
// 仍按客户请求的源模型算（applyRewrittenBody 写 routeOriginModel）：
//   · 秒数：目标 adaptor（youzanwan3）对 hk-wan3 **只取生成秒数**，
//     不并入参考视频秒数 —— 这正是「参考视频不收费」；
//   · 档位倍率：用**源模型**的口径覆盖（官网 1080P 是 3.148 倍、
//     tj 是 4 倍、hk-wan3 自己只有 2.667 倍），见包头下文的常量表。
//
// ── 准入（任何一条不满足就留在原链路）──
//
//  1. 生成时长 4–15 秒：hk-wan3 下界 4 秒（更低必 400），且带参考视频时
//     生成时长上限 15 秒（上游硬约束）；
//  2. 画幅不能是 adaptive：hk-wan3 档位表里没有它，会回落 16:9 —— 等于替客户
//     钉死了一个画幅；
//  3. 不能带首尾帧：hk-wan3 的首尾帧与普通参考素材互斥（必 400）；
//  4. 素材数量在 hk-wan3 上限内：图 ≤10 / 视频 ≤5 / 音频 ≤5 / 合计 ≤20；
//  5. 目标模型在当前分组有可用渠道。
//
// ⚠️ 参考视频**总时长 ≤15 秒**这条闸门这里不判：解析 mvhd 要真的抓素材头，
// 放在路由层会给每个请求加一次网络往返。它由 hk-wan3 的 validate 在预扣费之前
// 以中文可操作文案 400（"参考视频总时长 N 秒超过 15 秒上限，请裁剪后重试"），
// 不扣费、不建单。改派过去才发现，比「留在官网把素材丢掉」仍然更好。

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/dashscope"
	"github.com/QuantumNous/new-api/relay/channel/task/kemeitv"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// hkRefVideoSourceProfile 是「源模型 → 解析口径」的一格。
type hkRefVideoSourceProfile struct {
	// duration 返回**源渠道口径**的生成秒数。
	duration func(relaycommon.TaskSubmitReq) (int, error)
	// resolution 返回**源渠道口径**的分辨率档位。
	resolution func(relaycommon.TaskSubmitReq) (string, error)
	// sizeRatio 返回该源模型档位相对 480P 的计费倍率。
	sizeRatio func(model, resolution string) float64
}

// hkRefVideoSourceProfiles 按源模型选解析口径。
//
// 为什么必须分开：官网（DashScope）认顶层 seconds 并会钳制越界时长，
// 客美（tj）不认 seconds 且越界直接报错；两者的档位倍率也不同
// （官网 1 / 1.955 / 3.148，tj 1 / 1.811 / 4 —— 720P 还是特价档）。
// 用一份表把「这个模型按谁的规矩算」写清楚，比在函数里 if-else 安全。
var hkRefVideoSourceProfiles = map[string]hkRefVideoSourceProfile{
	seedanceroute_setting.Wan3GuanwangModel: {
		duration: func(req relaycommon.TaskSubmitReq) (int, error) {
			// 官网 adaptor 会钳制（1 → 2 秒、60 → 30 秒），这里取同一份实现。
			return dashscope.RequestDuration(req), nil
		},
		resolution: func(req relaycommon.TaskSubmitReq) (string, error) {
			// 空串 = 客户没声明，计费按 480P 走（与 dashscope adaptor 一致）。
			return dashscope.ResolutionFromRequest(req), nil
		},
		sizeRatio: func(_ string, resolution string) float64 {
			return dashscope.BillingSizeRatioForResolution(resolution)
		},
	},
	seedanceroute_setting.Wan3TjModel: {
		duration:   kemeitv.RequestDuration,
		resolution: kemeitv.RequestResolution,
		sizeRatio:  kemeitv.BillingSizeRatioForResolution,
	},
	seedanceroute_setting.Wan3PrimeTjModel: {
		duration:   kemeitv.RequestDuration,
		resolution: kemeitv.RequestResolution,
		sizeRatio:  kemeitv.BillingSizeRatioForResolution,
	},
}

// Wan3RefVideoHkRoute 把带参考视频的万相请求改派到 hk-wan3。
//
// 必须挂在 Distribute() 之前（渠道按 body 的 model 选），
// 也必须挂在 CaptureTaskRequestParams() 之后（日志要展示客户原始参数）。
func Wan3RefVideoHkRoute() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !seedanceroute_setting.IsWan3RefVideoHkEnabled() {
			c.Next()
			return
		}
		if !strings.HasPrefix(c.Request.Header.Get("Content-Type"), "application/json") {
			c.Next()
			return
		}
		// /remix 的模型与参数由原任务决定，改名会破坏 remix 语义。
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
		profile, known := hkRefVideoSourceProfiles[sourceModel]
		if !known {
			c.Next()
			return
		}
		// 已被前置规则改派过的请求不二次改派（否则会覆盖它写好的计费原点）。
		if origin := RouteOriginModel(c); origin != "" {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频→hk-wan3：请求已由前置规则改派（计费原点 %s），不二次改派", origin))
			c.Next()
			return
		}

		var req relaycommon.TaskSubmitReq
		if raw, err := common.Marshal(body); err != nil || common.Unmarshal(raw, &req) != nil {
			c.Next()
			return
		}
		// 触发条件：带参考视频（media 里 type=reference_video，或顶层 videos/video）。
		if !hasReferenceVideoRequest(req) {
			c.Next()
			return
		}

		duration, err := profile.duration(req)
		if err != nil {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频→hk-wan3：%s 的时长解析失败（%v），保持原链路", sourceModel, err))
			c.Next()
			return
		}
		if duration < seedanceroute_setting.Wan3RefVideoHkMinSeconds ||
			duration > seedanceroute_setting.Wan3RefVideoHkMaxSeconds {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频→hk-wan3：生成时长 %d 秒不在 hk-wan3 的可路由区间 %d–%d 秒",
				duration, seedanceroute_setting.Wan3RefVideoHkMinSeconds, seedanceroute_setting.Wan3RefVideoHkMaxSeconds))
			c.Next()
			return
		}
		// 画幅 adaptive：hk-wan3 没有这一档，会回落 16:9（静默改画幅）。
		if ratio, hasRatio := resolveTehuiRatioGateRatio(body); hasRatio && isAdaptiveRatioText(ratio) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频→hk-wan3：画幅 %s 在 hk-wan3 档位表里没有对应项，保持原链路", ratio))
			c.Next()
			return
		}
		// 首尾帧与参考素材在 hk-wan3 上互斥（必 400）。
		if hasFrameMedia(body) {
			logger.LogInfo(c, "条件路由跳过参考视频→hk-wan3：请求带首尾帧，hk-wan3 与参考素材互斥")
			c.Next()
			return
		}
		if reason := hkRefVideoMediaOutOfLimits(req); reason != "" {
			logger.LogInfo(c, "条件路由跳过参考视频→hk-wan3："+reason)
			c.Next()
			return
		}

		targetModel := seedanceroute_setting.Wan3RefVideoHkTargetModel
		if !channelAvailabilityProbe(c, targetModel) {
			logger.LogInfo(c, fmt.Sprintf(
				"条件路由跳过参考视频→hk-wan3：目标 %s 在当前分组下没有可用渠道", targetModel))
			c.Next()
			return
		}

		resolution, _ := profile.resolution(req)
		rewritten := make(map[string]interface{}, len(body)+2)
		for key, value := range body {
			rewritten[key] = value
		}
		rewritten["model"] = targetModel
		// 显式写 duration：hk-wan3 只认 duration（不读顶层 seconds），且下界是 4 秒。
		rewritten["duration"] = duration

		if !applyRewrittenBody(c, rewritten, sourceModel) {
			c.Next()
			return
		}
		common.SetContextKey(c, constant.ContextKeyRouteBillingSizeRatio,
			profile.sizeRatio(sourceModel, resolution))

		logger.LogInfo(c, fmt.Sprintf(
			"条件路由：%s 带参考视频改派到 %s（%d 秒；参考视频不并入计费，仍按 %s 计费）",
			sourceModel, targetModel, duration, sourceModel))
		c.Next()
	}
}

// hasReferenceVideoRequest 判断请求里有没有参考视频。
//
// 覆盖三处写法（与 youzanwan3/kemeitv 的 buildMedia 同口径）：
//   - media 数组里 type=reference_video 的条目；
//   - 顶层 videos 数组 / video 单个字段；
//   - metadata 下的同名写法（部分客户端塞 metadata）。
func hasReferenceVideoRequest(req relaycommon.TaskSubmitReq) bool {
	for _, item := range req.Media {
		if strings.TrimSpace(item.Type) == "reference_video" {
			return true
		}
	}
	if len(req.Videos) > 0 || strings.TrimSpace(req.Video) != "" {
		return true
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["videos"]; ok && raw != nil {
			return true
		}
		if raw, ok := req.Metadata["video"]; ok && raw != nil {
			return true
		}
	}
	return false
}

// hkRefVideoMediaOutOfLimits 检查素材数量是否超出 hk-wan3 的上限，超了返回原因。
//
// 上限（上游文档 2026-11-19）：图 10 / 视频 5 / 音频 5 / 合计 20。
// 超限的请求留在原链路 —— 改派过去就是一次必然的 400。
func hkRefVideoMediaOutOfLimits(req relaycommon.TaskSubmitReq) string {
	images, videos, audios := len(req.Images), len(req.Videos), len(req.Audios)
	if strings.TrimSpace(req.Image) != "" && images == 0 {
		images = 1
	}
	if strings.TrimSpace(req.Video) != "" && videos == 0 {
		videos = 1
	}
	if strings.TrimSpace(req.Audio) != "" && audios == 0 {
		audios = 1
	}
	for _, item := range req.Media {
		switch strings.TrimSpace(item.Type) {
		case "reference_image", "first_frame", "last_frame":
			images++
		case "reference_video":
			videos++
		case "reference_audio":
			audios++
		}
	}
	if images > 10 || videos > 5 || audios > 5 || images+videos+audios > 20 {
		return fmt.Sprintf(
			"素材数量超出 hk-wan3 上限（图 %d/10、视频 %d/5、音频 %d/5、合计 %d/20），保持原链路",
			images, videos, audios, images+videos+audios)
	}
	return ""
}
