package controller

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// POST /v1/files：签发 COS 直传预签名（2026-11-14）。
//
// 与已有的 POST /v1/uploads（薄转发 staging 图床）的区别：
//
//	/v1/uploads   字节**经过**网关 → 走老机 3Mbps 出口，大文件慢；
//	/v1/files     字节**不经过**网关 → 客户端直接 PUT 到对象存储，
//	              拿回境内 CDN URL 建单，跨境变境内。
//
// 客户素材在境外服务器时（实测回源仅 67~79 KB/s），这条链路是唯一能支持
// 大素材的正解：ArcReel 侧 60s 硬总预算决定了走代下载的素材不能超过 4MB。
//
// 只签发许可，不落任何字节：文件由客户端直接 PUT 到 COS。
// 密钥只存在 ArcReel 侧，网关不持有。

// cosPresignMaxFilenameLen 文件名长度上限。
//
// 文件名只用来看扩展名，长度限制是防止有人拿 1MB 的字符串当文件名打过来。
const cosPresignMaxFilenameLen = 255

// cosPresignAllowedExtensions 与 ArcReel 侧白名单同口径。
//
// 这里是**提前**拦截：ArcReel 也会拒，但网关先拒能给客户端更快的反馈，
// 且不必消耗一次跨服务器往返。两侧必须同步放宽/收紧。
var cosPresignAllowedExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".mp4":  "video/mp4",
	".mov":  "video/quicktime",
	".webm": "video/webm",
}

// resolveCosPresignChannel 取用于预签发的 manwu 渠道（baseURL + 密钥）。
//
// 与 resolveManwuStagingChannel 同源：建单与预签名必须走同一台漫屋，
// 否则预签名签出的对象 URL 未必是建单那侧能读到的。
var resolveCosPresignChannel = func() (*model.Channel, error) {
	channels, err := model.GetChannelsByType(0, 100, false, constant.ChannelTypeManwu)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch != nil && ch.Status == common.ChannelStatusEnabled {
			return model.GetChannelById(ch.Id, true)
		}
	}
	return nil, fmt.Errorf("没有已启用的漫屋渠道")
}

type cosPresignRequest struct {
	Filename string `json:"filename"`
}

// PresignCosUploadFile 处理 POST /v1/files。
func PresignCosUploadFile(c *gin.Context) {
	var req cosPresignRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "请求体必须是 JSON 且含 filename 字段")
		return
	}

	filename := strings.TrimSpace(req.Filename)
	if filename == "" {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "filename 不能为空")
		return
	}
	if len(filename) > cosPresignMaxFilenameLen {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			fmt.Sprintf("filename 超过 %d 字符上限", cosPresignMaxFilenameLen))
		return
	}

	// 只取扩展名，文件名其余部分（基名、目录）由服务端重新生成。
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	contentType, ok := cosPresignAllowedExtensions[ext]
	if !ok {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error",
			"仅支持 png/jpg/webp/mp4/mov/webm 素材，请先转换为常见格式")
		return
	}

	channel, err := resolveCosPresignChannel()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("cos presign: resolve manwu channel failed: %s", err.Error()))
		videoProxyError(c, http.StatusServiceUnavailable, "server_error", "暂无可用漫屋渠道，请稍后重试")
		return
	}

	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("cos presign: build http client failed: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "构建请求客户端失败")
		return
	}

	result, err := manwu.PresignCosUpload(c.Request.Context(), client, channel.GetBaseURL(), channel.Key, ext)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("cos presign failed: %s", err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":         result.URL,
		"object_url":  result.ObjectURL,
		"key":         result.Key,
		"expires_in":  result.ExpiresIn,
		"max_bytes":   result.MaxBytes,
		"content_type": contentType,
	})
}
