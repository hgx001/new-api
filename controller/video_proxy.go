package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	taskwan3 "github.com/QuantumNous/new-api/relay/channel/task/wan3"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyFetchTimeout 是「从上游回源 + 转发给客户端」整段的总超时（2026-10-25）。
//
// ⚠️ 这个 ctx 同时覆盖 client.Do(req) 和后面的 io.Copy(c.Writer, resp.Body)，
// 也就是「从上游下载」与「写给客户端」**共用同一个 deadline**。
// 旧值 60 秒对 30 秒 720P 成片（27~30MB）远远不够，生产实测（2026-10-25）：
//   - new-api → ArcReel 走公网域名回源只有 244~337 KB/s（同机内网直连是 17~22 MB/s），
//     60 秒只拉到 19~20MB → io.Copy 报 `context deadline exceeded`；
//   - 客户端侧同理：用户带宽 1.3Mbps 时 60 秒只能收 ~10MB，
//     用户看到的就是「拉到 10M 就断了，一直显示生成中」。
//
// 后果是上游（ArcReel）已结算扣费、下游拿不到完整产物 → 用户收不到成片、new-api 无法结算。
// 30MB 在 1Mbps 下约需 240 秒，留足余量取 600 秒；慢客户端占用连接可接受，
// 真正的兜底在网关/nginx 层。
const videoProxyFetchTimeout = 600 * time.Second

// videoProxyRelayRedirectEnv 是「跨主机重定向透传」的开关，设为 0/false/off 可关闭。
//
// 留这个开关是为了**不用重新构建镜像就能回滚**：万一某个客户端不跟随 302，
// 改 docker 环境变量重启即可回到「本机代理转发」的旧行为。
const videoProxyRelayRedirectEnv = "VIDEO_PROXY_RELAY_REDIRECT"

// videoProxyRelayRedirectEnabled 判断是否透传跨主机重定向，默认开启。
func videoProxyRelayRedirectEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(videoProxyRelayRedirectEnv))) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}

// videoProxyRedirectClient 返回一个「跨主机重定向不跟随」的客户端副本。
//
// 背景（2026-10-25）：ArcReel 托管产物在写入 300 秒后对 /output 返回
// 302 → COS/CDN 直链（arcmedia.heibaidao.cn）。若本服务继续跟随，视频字节
// 会先下到本机、再原样吐给客户端，而本机公网出口只有 3Mbps：
//
//	29MB 实测 87.51s（331 KB/s）—— 正好是这个上限算出来的
//
// 而客户端直连 CDN 只要 1.55s（边缘命中）/ 11.5s（回源 COS）。所以把跨主机
// 302 原样透传，让字节根本不经过本机，同时把 3Mbps 让给 API 本身。
//
// 为什么只透传「跨主机」：
//   - net/http 在跨 host 重定向时会剥掉 Authorization 等敏感头，也就是说
//     跟随它本来就只能取到公开资源（需要凭证的上游会 401）。透传不降低能力。
//   - 同 host 重定向通常靠请求头里的凭证才能取到内容（Gemini 的
//     x-goog-api-key、OpenAI 的 Bearer），透传给客户端必然 401，必须继续跟随。
//
// 返回副本而不是改原 client：client 由 service.GetHttpClientWithProxy 提供，
// 可能是共享实例，就地改 CheckRedirect 会影响其它调用方。
func videoProxyRedirectClient(base *http.Client) *http.Client {
	clone := *base
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return http.ErrUseLastResponse
		}
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return http.ErrUseLastResponse
		}
		return nil
	}
	return &clone
}

// isRelayableRedirect 判断这个状态码是不是可以透传给客户端。
func isRelayableRedirect(status int) bool {
	switch status {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
}

// applyVideoProxyRangeHeaders 把客户端的 Range / If-Range 透传给回源请求。
//
// 成片动辄几十 MB，客户端下载中途断开后不应从 0 重来：Range 透传后上游
// （OSS/COS/CDN）回 206，客户端按自己的偏移续传即可。客户端没带就不发，
// 避免凭空给上游塞空头。
func applyVideoProxyRangeHeaders(req *http.Request, clientHeader http.Header) {
	if rng := clientHeader.Get("Range"); rng != "" {
		req.Header.Set("Range", rng)
	}
	if ifRange := clientHeader.Get("If-Range"); ifRange != "" {
		req.Header.Set("If-Range", ifRange)
	}
}

// isVideoProxyRelayableStatus 判断回源状态能否原样转给客户端。
//
// 200 是完整文件；206 / 416 是断点续传的协议响应，必须透传，
// 否则会被下面的错误分支包成 502，客户端永远无法续传。
func isVideoProxyRelayableStatus(status int) bool {
	switch status {
	case http.StatusOK, http.StatusPartialContent, http.StatusRequestedRangeNotSatisfiable:
		return true
	default:
		return false
	}
}

// writeVideoProxyContent 把回源响应原样写回客户端。
//
// 206 / 416 是分片响应，绝不能打 public 长缓存：少量中间缓存会把半截
// 文件当完整产物回给后续请求。只有完整 200 才允许缓存一天。
func writeVideoProxyContent(w gin.ResponseWriter, resp *http.Response) error {
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if resp.StatusCode == http.StatusOK {
		w.Header().Set("Cache-Control", "public, max-age=86400")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

func VideoProxy(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "task_id is required")
		return
	}

	userID := c.GetInt("id")
	task, exists, err := model.GetByTaskId(userID, taskID)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to query task")
		return
	}
	if !exists || task == nil {
		videoProxyError(c, http.StatusNotFound, "invalid_request_error", "Task not found")
		return
	}

	if task.Status != model.TaskStatusSuccess {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("Task is not completed yet, current status: %s", task.Status))
		return
	}

	channel, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to get channel for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to retrieve channel information")
		return
	}
	baseURL := channel.GetBaseURL()
	if baseURL == "" {
		baseURL = "https://api.openai.com"
	}

	var videoURL string
	proxy := channel.GetSetting().Proxy
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create proxy client for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy client")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), videoProxyFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "", nil)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to create request: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	switch channel.Type {
	case constant.ChannelTypeGemini:
		apiKey := task.PrivateData.Key
		if apiKey == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Missing stored API key for Gemini task %s", taskID))
			videoProxyError(c, http.StatusInternalServerError, "server_error", "API key not stored for task")
			return
		}
		videoURL, err = getGeminiVideoURL(channel, task, apiKey)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Gemini video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Gemini video URL")
			return
		}
		req.Header.Set("x-goog-api-key", apiKey)
	case constant.ChannelTypeVertexAi:
		videoURL, err = getVertexVideoURL(channel, task)
		if err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve Vertex video URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve Vertex video URL")
			return
		}
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora, constant.ChannelTypeYoukou,
		constant.ChannelTypeYouzanWan3:
		// 有赞 Wan3（63）：2026-11-15 上游切换后 completed 响应不带成片地址，
		// 成片改由 /v1/videos/{upstream_task_id}/content 提供、必须带渠道密钥
		// （adaptor ParseTaskResult 因此留空 Url，轮询落成站内代理地址）。
		// 与 OpenAI/Sora 同形，直接复用该分支。
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	case constant.ChannelTypeWan3:
		// wan3 的成片有两种来源：
		//
		// 1. 官网直链（OSS 签名 URL）：轮询时已写入 PrivateData.ResultURL
		//    （自带签名，无需再转发上游 /result 端点 —— dashscope 官方没有该端点，
		//    转发必 404/502）。
		// 2. 成片提升后的产物（在腾讯云 COS 里）：ResultURL 是一个
		//    cos-proxy://bucket/region/path 标记，需要现签临时直链。
		videoURL = task.GetResultURL()
		if bucket, region, objectPath, ok := taskwan3.ParseCOSProxyMarker(videoURL); ok {
			signed, perr := taskwan3.PresignCOSURL(c.Request.Context(), bucket, region, objectPath)
			if perr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf(
					"Failed to presign COS url for task %s: %s", taskID, perr.Error()))
				videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
				return
			}
			// 302 直连 COS：字节不经过本机 3Mbps 出口。
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Location", signed)
			c.Writer.WriteHeader(http.StatusFound)
			return
		}
	case constant.ChannelTypeXinbi:
		// 心笔 Model Router 的成片有两种来源，与 wan3 同构：
		//
		// 1. 成片提升后的产物（在腾讯云 COS 里）：ResultURL 是
		//    cos-proxy://bucket/region/path 标记，需要现签临时直链。
		//    本渠道默认走这条路（480P → 提升 720P 是商品定义）。
		// 2. 没配 MPS 或提升失败时回落到上游原片：ResultURL 是上游的稳定
		//    下载直链（token 在 URL 里，3 天有效），直接 302 过去即可，
		//    不要带渠道密钥 —— 那是给第三方 CDN 的。
		//
		// ⚠️ 漏了 cos-proxy 这一支的后果实测过：下游拿到
		//    「unsupported protocol: cos-proxy」403，成片完全取不到。
		videoURL = task.GetResultURL()
		if bucket, region, objectPath, ok := taskwan3.ParseCOSProxyMarker(videoURL); ok {
			signed, perr := taskwan3.PresignCOSURL(c.Request.Context(), bucket, region, objectPath)
			if perr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf(
					"Failed to presign COS url for task %s: %s", taskID, perr.Error()))
				videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
				return
			}
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Location", signed)
			c.Writer.WriteHeader(http.StatusFound)
			return
		}
	case constant.ChannelTypeErchun:
		// 二春：成片在 /v1/tasks/{task_id}/content，必须带渠道密钥才能取。
		// adaptor 因此不吐上游地址（ParseTaskResult 留空 Url，由轮询落成站内代理路径）。
		// delivery=cdn 时上游可能 302 到签名 CDN 地址：这里的 client 是默认策略，
		// Go 的 net/http 在**跨 host** 重定向时会自动剥掉 Authorization / Cookie，
		// 所以 Bearer 不会跟着跳到第三方 CDN，无需额外处理。
		videoURL = fmt.Sprintf("%s/v1/tasks/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	case constant.ChannelTypeSanyuan:
		// 三元巡狩：成片在 /v1/video/file/{event_id}，必须带渠道密钥才能取。
		// adaptor 不吐上游地址（ParseTaskResult 留空 Url，由轮询落成站内代理路径），
		// 这里按渠道密钥回源。上游偶发 302 到第三方 CDN 时 Authorization 会被
		// net/http 跨 host 剥离，与二春同口径，无需额外处理。
		videoURL = fmt.Sprintf("%s/v1/video/file/%s", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	case constant.ChannelTypeManwu:
		// 漫屋（ArcReel）：Worker 上传到本站托管目录的产物（Gemini 图片 blob、Veo 的
		// data: mp4）在 ArcReel 侧存的是**相对路径**且需要渠道密钥，不是公网地址，
		// adaptor 因此不透传，ResultURL 落成 /v1/videos/{task}/content 代理地址。
		// 这里回查一次 job 拿真实 sourceUrl 再下载；只有 ArcReel 自己的 /api 路径才带
		// 渠道密钥，避免把凭证泄露给 dola 等第三方 CDN。
		//
		// ── cos-proxy:// 占位符（2026-11-16 补）──
		//
		// CapCut 链路开了「交付前超分」后（2026-11-14/15 起，规则六 CapCut 分支 /
		// 规则七 / 规则八 / 规则九改派到 cp-seedance-* 的促销族任务），MPS 超分成功
		// 时 PollPostProcess 会把 ResultURL 改写成 cos-proxy://bucket/region/path
		// —— 客户按 720P 买的片，真正的 720P 产物在那个对象里。
		//
		// ⚠️ 这个分支漏判 cos-proxy:// 的后果（2026-11-16 真机实测）：
		// resolveManwuResultURL 见到非 http 开头的 stored 就回查上游 job、拿
		// sourceUrl 回源 —— 那是 480p **原片**。任务照常 SUCCESS、账单按 720P 收，
		// 客户拿到的却是超分前的原片（md5 与原片完全一致）——
		// 正是 capcut_upscale.go 里反复强调要避免的「按 720P 价交付 480P」。
		//
		// 修法与 wan3 / 心笔 / 车站三个分支同构：先认占位符、现签临时直链 302。
		// 认不出占位符（普通 manwu 任务）再走原有回查逻辑，行为不变。
		if serveCOSProxyRedirectIfMarked(c, taskID, task.GetResultURL()) {
			return
		}
		resolved, needsAuth, rerr := resolveManwuResultURL(baseURL, channel.Key, channel.GetSetting().Proxy, task)
		if rerr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve manwu result URL for task %s: %s", taskID, rerr.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to resolve result URL")
			return
		}
		videoURL = resolved
		if needsAuth {
			req.Header.Set("Authorization", "Bearer "+channel.Key)
		}
	case constant.ChannelTypeYuanmeng:
		// 视频生成车站（排队通道）：成片来源有两种。
		//
		// 1. **转存后**（主路径）：ResultURL 是 cos-proxy://bucket/region/path 标记，
		//    需要现签临时直链 —— 与 wan3/心笔同构。
		//    转存是因为上游直链只有约 24 小时有效，站点不留副本；
		//    不转存的话用户隔天取片必然 404，而任务在库里是「成功」状态。
		// 2. **转存失败回落**：ResultURL 是上游的 videoUrl 直链（短期有效），
		//    直接回源即可 —— 但**不能带渠道密钥**，那是上游的 X-Token。
		//
		// ⚠️ 上游成片直链是公网可访问的链接，回源时不需要任何凭证。
		//    这里刻意不设 Authorization：把 X-Token 发给第三方存储是凭证泄露。
		videoURL = task.GetResultURL()
		if bucket, region, objectPath, ok := taskwan3.ParseCOSProxyMarker(videoURL); ok {
			signed, perr := taskwan3.PresignCOSURL(c.Request.Context(), bucket, region, objectPath)
			if perr != nil {
				logger.LogError(c.Request.Context(), fmt.Sprintf(
					"Failed to presign COS url for task %s: %s", taskID, perr.Error()))
				videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
				return
			}
			// 302 直连 COS：字节不经过本机 3Mbps 出口。
			c.Writer.Header().Set("Cache-Control", "no-cache")
			c.Writer.Header().Set("Location", signed)
			c.Writer.WriteHeader(http.StatusFound)
			return
		}
	case constant.ChannelTypeKemeitv:
		// 客美 Kemeitv：**没有** /content 端点 —— 成片地址在
		// GET /v1/video/generations/{id} 的 data[0].url 里，且是**带签名的临时直链**。
		//
		// ⚠️ 为什么必须回查而不是用库里存的地址：
		//	签名直链有有效期，任务完成到客户来取片可能隔很久，
		//	库里存的那份早就过期 → 404。这里每次取片都实时回上游要最新地址。
		//
		// 回查要带渠道密钥（上游鉴权是 Bearer），所以 needsAuth=true。
		resolved, rerr := resolveKemeitvResultURL(baseURL, channel.Key, channel.GetSetting().Proxy, task)
		if rerr != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to resolve kemeitv result URL for task %s: %s", taskID, rerr.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
			return
		}
		videoURL = resolved
	default:
		// Video URL is stored in PrivateData.ResultURL (fallback to FailReason for old data)
		videoURL = task.GetResultURL()
	}

	videoURL = strings.TrimSpace(videoURL)
	if videoURL == "" {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL is empty for task %s", taskID))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}

	if strings.HasPrefix(videoURL, "data:") {
		if err := writeVideoDataURL(c, videoURL); err != nil {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to decode video data URL for task %s: %s", taskID, err.Error()))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		}
		return
	}

	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(videoURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Video URL blocked for task %s: %v", taskID, err))
		videoProxyError(c, http.StatusForbidden, "server_error", fmt.Sprintf("request blocked: %v", err))
		return
	}

	req.URL, err = url.Parse(videoURL)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to parse URL %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "Failed to create proxy request")
		return
	}

	// 断点续传：客户端带 Range/If-Range 时透传给上游，由上游回 206。
	applyVideoProxyRangeHeaders(req, c.Request.Header)

	fetchClient := client
	if videoProxyRelayRedirectEnabled() {
		fetchClient = videoProxyRedirectClient(client)
	}

	resp, err := fetchClient.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	// 跨主机重定向：透传给客户端，让字节直连 CDN，不经过本机 3Mbps 出口。
	// 能走到这里的 3xx 一定是「跨主机」的（同主机的已被 CheckRedirect 跟随掉）。
	if isRelayableRedirect(resp.StatusCode) {
		location := resp.Header.Get("Location")
		if location == "" {
			logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d without Location for %s", resp.StatusCode, videoURL))
			videoProxyError(c, http.StatusBadGateway, "server_error", "Upstream redirect missing Location")
			return
		}
		// 302 绝不能被客户端长缓存：CDN 直链带 ?v=<mtime_ns> 版本参数，
		// 一旦被缓存住就会一直拿到旧版本（ArcReel 侧 2026-09-23 踩过同类坑）。
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Location", location)
		c.Writer.WriteHeader(resp.StatusCode)
		return
	}

	if !isVideoProxyRelayableStatus(resp.StatusCode) {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	if err = writeVideoProxyContent(c.Writer, resp); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
}

// serveCOSProxyRedirectIfMarked 认领 cos-proxy://bucket/region/path 占位符：
// 命中就现签 COS 临时直链并 302，返回 true（响应已写出，调用方直接 return）。
//
// 适用渠道：wan3 / 心笔 / 车站（yuanmeng）/ 漫屋（manwu）。四条链路的
// 「产物落 COS + 取片时实时签名」是同一套协议（见 wan3/relocate.go 与
// capcut_upscale.go），任何分支漏判占位符，客户拿到的都会是**超分/转存前**
// 的原始产物 —— 2026-11-16 在 manwu 分支实测：客户按 720P 买的 CapCut 超分
// 任务，/content 交付的却是 480p 原片（md5 与原片完全一致）。
//
// 返回 false = 不是占位符，调用方继续走各自渠道的回源逻辑；
// 预签名失败时写 502 并返回 true（与三个既有分支同一口径，不回落 ——
// 回落会把降级产物当成交付，比显式失败更难被发现）。
func serveCOSProxyRedirectIfMarked(c *gin.Context, taskID, stored string) bool {
	bucket, region, objectPath, ok := taskwan3.ParseCOSProxyMarker(stored)
	if !ok {
		return false
	}
	signed, err := taskwan3.PresignCOSURL(c.Request.Context(), bucket, region, objectPath)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf(
			"Failed to presign COS url for task %s: %s", taskID, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return true
	}
	// 302 直连 COS：字节不经过本机 3Mbps 出口。
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Location", signed)
	c.Writer.WriteHeader(http.StatusFound)
	return true
}

// resolveManwuResultURL 解析漫屋任务的真实结果地址，返回（地址, 是否需要渠道密钥）。
//
// 两条路径：
//  1. ResultURL 已是 ArcReel 以外的公网 CDN 直链（dola / tiktok）→ 直接用，不带凭证。
//  2. ResultURL 是本服务的 /v1/videos/{task}/content 代理地址（即 adaptor 拒透传
//     相对路径结果后的落库值）→ 回查 ArcReel job 拿 sourceUrl；ArcReel 托管产物是
//     相对路径 /api/v1/...，拼上渠道 baseURL 后属 ArcReel 自有资源，需要渠道密钥。
//
// 绝不能无条件带上渠道密钥：ResultURL 若指向第三方 CDN，凭证会随请求发出去。
func resolveManwuResultURL(baseURL, key, proxy string, task *model.Task) (string, bool, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://arcreel.heibaidao.cn"
	}

	stored := strings.TrimSpace(task.GetResultURL())
	lowered := strings.ToLower(stored)
	isAbsolute := strings.HasPrefix(lowered, "http://") || strings.HasPrefix(lowered, "https://")
	// 自代理识别用**路径形状**而不是 host 比对：ServerAddress 可能在任务落库后被改过
	// （域名迁移/换端口），那时按 host 判会把代理地址当成上游直链，导致本站
	// /content 代理自我递归或返回 401。路径形状是 BuildProxyURL 的稳定契约。
	isSelfProxy := isAbsolute && isSelfProxyPath(stored)
	// ⚠️ ArcReel 自有托管路径即使被拼成**绝对** URL 也必须回查 job 带渠道密钥下载,
	// 不能当成"公网直链"透传 —— 客户端没有 service token, 拿到就是 401。
	// 2026-10-08 实测(langdu 生产 gemini-web-video 任务): ResultURL 落成了
	// https://arcreel.../api/v1/remote-generation/jobs/...mp4, 走到这里被透传,
	// 客户端直连 401、/content 代理同样透传后把 401 包成 502, 成片拿不到。
	isOwnedAsset := manwu.IsOwnedAssetPath(stored)
	if isAbsolute && !isSelfProxy && !isOwnedAsset {
		// 公网直链（dola CDN 等）：直接透传，不泄露渠道密钥。
		return stored, false, nil
	}

	// 回查 job 拿 sourceUrl。
	jobURL := fmt.Sprintf("%s/api/v1/remote-generation/jobs/%s", baseURL, url.PathEscape(task.GetUpstreamTaskID()))
	req, err := http.NewRequest(http.MethodGet, jobURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return "", false, fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", false, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("arcreel job query returned status %d: %s", resp.StatusCode, truncateForError(body))
	}

	var job struct {
		Status       string `json:"status"`
		SourceURL    string `json:"sourceUrl"`
		Alt          string `json:"source_url"`
		ResultPrompt string `json:"resultPrompt"`
		Error        string `json:"error"`
	}
	if err := common.Unmarshal(body, &job); err != nil {
		return "", false, fmt.Errorf("unmarshal arcreel job: %w", err)
	}
	source := strings.TrimSpace(job.SourceURL)
	if source == "" {
		source = strings.TrimSpace(job.Alt)
	}
	if source == "" {
		// 文本类任务（即梦视频反解）本来就没有媒体产物：这里要说清楚，否则客户端
		// 只会看到一个无意义的 502，然后以为是下载失败。
		if strings.TrimSpace(job.ResultPrompt) != "" {
			return "", false, fmt.Errorf("该任务没有媒体产物（文本结果请读 metadata.prompt）")
		}
		return "", false, fmt.Errorf("arcreel job has no sourceUrl (status=%s, error=%s)", job.Status, job.Error)
	}
	if !strings.HasPrefix(strings.ToLower(source), "http://") &&
		!strings.HasPrefix(strings.ToLower(source), "https://") {
		source = baseURL + "/" + strings.TrimLeft(source, "/")
	}
	return source, true, nil
}

// resolveKemeitvResultURL 回查客美 Kemeitv 的任务详情，取最新的成片直链。
//
// 为什么每次取片都回查，而不是直接用库里存的 ResultURL：
//
//	客美的上游成片地址是**带签名的临时直链**（上游存储，签名有有效期）。
//	任务完成到客户来取片可能隔很久，库里存的那份签名早就过期 → 404。
//	实时回查能拿到上游当前有效的地址。
//
// 回查要带渠道密钥（上游是 Bearer 鉴权），所以返回的 needsAuth=true ——
// 但注意：取到的是**上游存储的公网直链**，带 Authorization 回源本身没问题
// （同 host），只是不能把它透传给客户端。
func resolveKemeitvResultURL(baseURL, key, proxy string, task *model.Task) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://ai.kemeitv.cn"
	}

	queryURL := fmt.Sprintf("%s/v1/video/generations/%s", baseURL, url.PathEscape(task.GetUpstreamTaskID()))
	req, err := http.NewRequest(http.MethodGet, queryURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return "", fmt.Errorf("new proxy http client failed: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("kemeitv task query returned status %d: %s", resp.StatusCode, truncateForError(body))
	}

	var detail struct {
		Status string `json:"status"`
		Data   []struct {
			URL string `json:"url"`
		} `json:"data"`
		URL   string `json:"url"`
		Error *struct {
			Message string `json:"message"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if err := common.Unmarshal(body, &detail); err != nil {
		return "", fmt.Errorf("unmarshal kemeitv task detail: %w", err)
	}

	for _, item := range detail.Data {
		if u := strings.TrimSpace(item.URL); u != "" {
			return u, nil
		}
	}
	if u := strings.TrimSpace(detail.URL); u != "" {
		return u, nil
	}

	// 没有成片地址：区分「上游还在生成」与「上游明确失败」，
	// 否则客户端只会看到一个笼统的 502，定位不到原因。
	if detail.Error != nil && strings.TrimSpace(detail.Error.Message) != "" {
		return "", fmt.Errorf("kemeitv task failed (status=%s): %s", detail.Status, detail.Error.Message)
	}
	return "", fmt.Errorf("kemeitv task has no result url yet (status=%s)", detail.Status)
}

// isSelfProxyPath 判断地址是否指向本服务的 /v1/videos/{task_id}/content 代理。
func isSelfProxyPath(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil {
		return false
	}
	path := strings.TrimRight(parsed.Path, "/")
	return strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/content")
}

func truncateForError(body []byte) string {
	const limit = 200
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "..."
}

func writeVideoDataURL(c *gin.Context, dataURL string) error {
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid data url")
	}

	header := parts[0]
	payload := parts[1]
	if !strings.HasPrefix(header, "data:") || !strings.Contains(header, ";base64") {
		return fmt.Errorf("unsupported data url")
	}

	mimeType := strings.TrimPrefix(header, "data:")
	mimeType = strings.TrimSuffix(mimeType, ";base64")
	if mimeType == "" {
		mimeType = "video/mp4"
	}

	videoBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		videoBytes, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return err
		}
	}

	c.Writer.Header().Set("Content-Type", mimeType)
	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(http.StatusOK)
	_, err = c.Writer.Write(videoBytes)
	return err
}
