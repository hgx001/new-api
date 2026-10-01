package controller

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// videoProxyError returns a standardized OpenAI-style error response.
func videoProxyError(c *gin.Context, status int, errType, message string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": message,
			"type":    errType,
		},
	})
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
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
	case constant.ChannelTypeOpenAI, constant.ChannelTypeSora, constant.ChannelTypeYoukou:
		videoURL = fmt.Sprintf("%s/v1/videos/%s/content", baseURL, task.GetUpstreamTaskID())
		req.Header.Set("Authorization", "Bearer "+channel.Key)
	case constant.ChannelTypeWan3:
		// wan3 的成片是 OSS 直链：轮询时已写入 PrivateData.ResultURL（自带签名，无需
		// 再转发上游 /result 端点——dashscope 官方没有该端点，转发必 404/502）。
		videoURL = task.GetResultURL()
	case constant.ChannelTypeManwu:
		// 漫屋（ArcReel）：Worker 上传到本站托管目录的产物（Gemini 图片 blob、Veo 的
		// data: mp4）在 ArcReel 侧存的是**相对路径**且需要渠道密钥，不是公网地址，
		// adaptor 因此不透传，ResultURL 落成 /v1/videos/{task}/content 代理地址。
		// 这里回查一次 job 拿真实 sourceUrl 再下载；只有 ArcReel 自己的 /api 路径才带
		// 渠道密钥，避免把凭证泄露给 dola 等第三方 CDN。
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

	resp, err := client.Do(req)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to fetch video from %s: %s", videoURL, err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", "Failed to fetch video content")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Upstream returned status %d for %s", resp.StatusCode, videoURL))
		videoProxyError(c, http.StatusBadGateway, "server_error",
			fmt.Sprintf("Upstream service returned status %d", resp.StatusCode))
		return
	}

	for key, values := range resp.Header {
		for _, value := range values {
			c.Writer.Header().Add(key, value)
		}
	}

	c.Writer.Header().Set("Cache-Control", "public, max-age=86400")
	c.Writer.WriteHeader(resp.StatusCode)
	if _, err = io.Copy(c.Writer, resp.Body); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("Failed to stream video content: %s", err.Error()))
	}
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
	if isAbsolute && !isSelfProxy {
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
