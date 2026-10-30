package controller

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// buildTaskSubmitSnapshot 汇总提交时的请求要素，写进 task_logs.payload。
//
// 只收「事后追责需要、且会随请求消失」的信息：
// 提示词、时长/比例/分辨率、参考素材（类型 + 原始 URL）、计费与渠道归属。
// 提示词按 2000 字截断（长剧本任务可达上万字），媒体 URL 保留原文 ——
// 它是复现问题的唯一线索，而 task_logs 只对管理员开放。
func buildTaskSubmitSnapshot(c *gin.Context, info *relaycommon.RelayInfo, quota int) map[string]any {
	snapshot := map[string]any{
		"quota":          quota,
		"channel_id":     info.ChannelId,
		"user_id":        info.UserId,
		"token_id":       info.TokenId,
		"group":          info.TokenGroup,
		"action":         info.Action,
		"origin_model":   info.OriginModelName,
		"upstream_model": info.UpstreamModelName,
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		snapshot["request_error"] = err.Error()
		return snapshot
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		snapshot["prompt"] = truncateRunes(prompt, 2000)
	}
	params := map[string]any{}
	if req.Duration > 0 {
		params["duration"] = req.Duration
	}
	if req.Seconds != "" {
		params["seconds"] = req.Seconds
	}
	if strings.TrimSpace(req.Resolution) != "" {
		params["resolution"] = req.Resolution
	}
	if strings.TrimSpace(req.Size) != "" {
		params["size"] = req.Size
	}
	if req.Metadata != nil {
		// 比例通常走 metadata（OpenAI 风格没有独立字段），单独提出来便于检索。
		if ratio, ok := req.Metadata["ratio"].(string); ok && strings.TrimSpace(ratio) != "" {
			params["ratio"] = ratio
		}
	}
	if req.Seed != nil {
		params["seed"] = *req.Seed
	}
	if len(params) > 0 {
		snapshot["parameters"] = params
	}
	if media := collectRequestMedia(req); len(media) > 0 {
		snapshot["media"] = media
	}
	if req.Metadata != nil {
		snapshot["metadata_keys"] = metadataKeys(req.Metadata)
	}
	return snapshot
}

// collectRequestMedia 把三种入参形态（media / images / image）统一成
// [{type,url}]，与适配器 buildMedia 的优先级保持一致。
func collectRequestMedia(req relaycommon.TaskSubmitReq) []map[string]string {
	media := make([]map[string]string, 0, len(req.Media)+len(req.Images)+1)
	for _, item := range req.Media {
		media = append(media, map[string]string{"type": item.Type, "url": item.URL})
	}
	if len(media) > 0 {
		return media
	}
	for _, url := range req.Images {
		if strings.TrimSpace(url) != "" {
			media = append(media, map[string]string{"type": "image", "url": url})
		}
	}
	if len(media) == 0 && strings.TrimSpace(req.Image) != "" {
		media = append(media, map[string]string{"type": "image", "url": req.Image})
	}
	if len(media) == 0 && strings.TrimSpace(req.InputReference) != "" {
		media = append(media, map[string]string{"type": "input_reference", "url": req.InputReference})
	}
	return media
}

func metadataKeys(metadata map[string]any) []string {
	keys := make([]string, 0, len(metadata))
	for key := range metadata {
		keys = append(keys, key)
	}
	return keys
}

// channelTypeOf 取渠道类型；relayInfo 可能在极早期失败时还没有 ChannelMeta。
func channelTypeOf(info *relaycommon.RelayInfo) int {
	if info == nil || info.ChannelMeta == nil {
		return 0
	}
	return info.ChannelMeta.ChannelType
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…(truncated)"
}

// GetTaskLogs 是管理员查询任务流水的只读接口。
//
//	GET /api/task/logs?task_id=task_xxx
//	GET /api/task/logs?upstream_task_id=5cddc82e-...
//
// 出问题时不必连库：拿到 task id 就能看到提交快照、每次重试的失败原因、
// 状态迁移与后处理阶段。
func GetTaskLogs(c *gin.Context) {
	taskId := strings.TrimSpace(c.Query("task_id"))
	upstreamTaskId := strings.TrimSpace(c.Query("upstream_task_id"))
	if taskId == "" && upstreamTaskId == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "task_id 或 upstream_task_id 必填"})
		return
	}
	logs, err := model.GetTaskLogs(taskId, upstreamTaskId, parseLimit(c.Query("limit")))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "", "data": logs})
}

func parseLimit(raw string) int {
	limit := 0
	for _, ch := range strings.TrimSpace(raw) {
		if ch < '0' || ch > '9' {
			return 0
		}
		limit = limit*10 + int(ch-'0')
		if limit > 500 {
			return 500
		}
	}
	return limit
}
