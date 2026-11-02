package controller

import (
	"fmt"
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
		// 按类型汇总，便于用一条 LIKE 就回答「这次到底带没带图」，
		// 不必再解析整个 media 数组。
		if summary := mediaTypeSummary(media); len(summary) > 0 {
			snapshot["media_types"] = summary
		}
	}
	// 请求里**出现过**哪些媒体字段。必须单独记：适配器按 media > images >
	// image > input_reference 取第一个非空的，其余会被静默忽略。只记生效的那份，
	// 事后就无法区分「客户端没传图」和「传了但被 media 挡住」—— 2026-11-02 排查
	// 任务 1194 时就卡在这里。
	if fields := requestMediaFields(req); len(fields) > 0 {
		snapshot["media_fields"] = fields
	}
	if req.Metadata != nil {
		snapshot["metadata_keys"] = metadataKeys(req.Metadata)
	}
	return snapshot
}

// mediaTypeSummary 统计各媒体类型的数量，例如 {"image":3,"reference_audio":2}。
func mediaTypeSummary(media []map[string]string) map[string]int {
	summary := make(map[string]int, len(media))
	for _, item := range media {
		summary[item["type"]]++
	}
	return summary
}

// requestMediaFields 记录请求实际带了哪些媒体字段（只记字段名与条数）。
func requestMediaFields(req relaycommon.TaskSubmitReq) map[string]int {
	fields := make(map[string]int, 4)
	if len(req.Media) > 0 {
		fields["media"] = len(req.Media)
	}
	if len(req.Images) > 0 {
		fields["images"] = len(req.Images)
	}
	if strings.TrimSpace(req.Image) != "" {
		fields["image"] = 1
	}
	if strings.TrimSpace(req.InputReference) != "" {
		fields["input_reference"] = 1
	}
	return fields
}

// collectRequestMedia 把 media 与 legacy 字段（images / image / input_reference）
// **合并**成 [{type,url}]，与适配器 buildMedia 的口径一致。
//
// 为什么是合并而不是「media 优先」：适配器现在会把两边并起来一起送上游，
// 快照若只记 media，事后看到的就是「只有音频、没有图」，与上游实际收到的不符 ——
// 排障时会被自己的日志误导（2026-11-02 就踩过）。
func collectRequestMedia(req relaycommon.TaskSubmitReq) []map[string]string {
	media := make([]map[string]string, 0, len(req.Media)+len(req.Images)+1)
	for _, item := range req.Media {
		media = append(media, map[string]string{"type": item.Type, "url": compactMediaURL(item.URL)})
	}
	// legacy 三选一，与适配器一致：同一张图不重复计入。
	legacy := req.Images
	if len(legacy) == 0 && strings.TrimSpace(req.Image) != "" {
		legacy = []string{req.Image}
	}
	if len(legacy) == 0 && strings.TrimSpace(req.InputReference) != "" {
		legacy = []string{req.InputReference}
	}
	for _, url := range legacy {
		if trimmed := strings.TrimSpace(url); trimmed != "" {
			media = append(media, map[string]string{"type": "image", "url": compactMediaURL(trimmed)})
		}
	}
	return media
}

// compactMediaURL 把内联 base64 媒体压成可读摘要。
//
// 为什么必须压：部分客户端直接传 `data:image/png;base64,...`，单条可达数 MB。
// 原样落库有两个后果 —— 一是把表撑爆，二是快照被 16KB 上限截断成**非法 JSON**
// （实测 381 条提交快照里 18 条如此，`payload::jsonb` 直接报错）。
//
// 压成摘要反而更好用：既能看出「确实是内联图」，又能看到体积与格式，
// 还保留了「这张图根本没上过 CDN」这个关键信息。
func compactMediaURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return trimmed
	}
	comma := strings.IndexByte(trimmed, ',')
	if comma < 0 {
		return "data:<malformed>"
	}
	header := trimmed[:comma]
	payload := trimmed[comma+1:]
	if strings.HasSuffix(strings.ToLower(header), ";base64") {
		// base64 每 4 字符还原 3 字节，不必解码整段。
		return fmt.Sprintf("%s,<%d bytes>", header, len(payload)*3/4)
	}
	return fmt.Sprintf("%s,<%d chars>", header, len(payload))
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
