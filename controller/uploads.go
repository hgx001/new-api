package controller

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// POST /v1/uploads：本地参考图上交换公网 URL（2026-10-11）。
//
// 背景：工作台带本地参考图的任务走黑白道（manwu）渠道时，adaptor 的
// assertHTTPURL 只放行公网 http(s) URL，而 new-api 原本没有任何文件端点——
// 任务注定失败。这里补最小闭环：把 multipart 图片薄转发到 ArcReel 暂存图床
// （service token 来自渠道密钥），返回拼好渠道 baseURL 的公网绝对 URL。
// 文件字节不在 new-api 落盘，校验（魔数/体积）由 ArcReel 权威执行并透传其错误。

// maxStagingUploadBytes 与 ArcReel staging 的单文件上限同口径（25MB）：
// 网关提前 413 省一次跨服务器传输，也给用户更快的反馈。
var maxStagingUploadBytes = 25 << 20

// resolveManwuStagingChannel 取用于 staging 转发的 manwu 渠道（baseURL + 密钥）。
// 包级变量是为了测试可替换；生产实现查 DB 里第一台启用的漫屋渠道。
var resolveManwuStagingChannel = func() (*model.Channel, error) {
	channels, err := model.GetChannelsByType(0, 100, false, constant.ChannelTypeManwu)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch != nil && ch.Status == common.ChannelStatusEnabled {
			return ch, nil
		}
	}
	return nil, fmt.Errorf("没有已启用的漫屋渠道")
}

func UploadStagingFile(c *gin.Context) {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "multipart 字段 file 不能为空")
		return
	}
	if fileHeader.Size > int64(maxStagingUploadBytes) {
		videoProxyError(c, http.StatusRequestEntityTooLarge, "invalid_request_error",
			fmt.Sprintf("文件超过 %dMB 上限", maxStagingUploadBytes>>20))
		return
	}

	file, err := fileHeader.Open()
	if err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "无法读取上传文件")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxStagingUploadBytes)+1))
	if err != nil {
		videoProxyError(c, http.StatusBadRequest, "invalid_request_error", "读取上传文件失败")
		return
	}
	if len(data) > maxStagingUploadBytes {
		videoProxyError(c, http.StatusRequestEntityTooLarge, "invalid_request_error",
			fmt.Sprintf("文件超过 %dMB 上限", maxStagingUploadBytes>>20))
		return
	}

	channel, err := resolveManwuStagingChannel()
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("staging upload: resolve manwu channel failed: %s", err.Error()))
		videoProxyError(c, http.StatusServiceUnavailable, "server_error", "暂无可用漫屋渠道，请稍后重试")
		return
	}

	baseURL := channel.GetBaseURL()
	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("staging upload: build http client failed: %s", err.Error()))
		videoProxyError(c, http.StatusInternalServerError, "server_error", "构建上传客户端失败")
		return
	}

	stagingPath, err := manwu.StagingUpload(
		c.Request.Context(), client, baseURL, channel.Key,
		fileHeader.Filename, fileHeader.Header.Get("Content-Type"), data,
	)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("staging upload failed: %s", err.Error()))
		videoProxyError(c, http.StatusBadGateway, "server_error", err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"url":      manwu.StagingURL(baseURL, stagingPath),
		"path":     stagingPath,
		"filename": fileHeader.Filename,
		"size":     len(data),
	})
}
