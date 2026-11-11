package controller

import (
	"math"
	"net/http"
	"strconv"
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
	// 比例记录（2026-11-10 审查后修正）。
	//
	// ⚠️ 为什么要记**顶层**比例：此前这里只读 metadata.ratio，
	// 客户端把比例放在请求体顶层时（OpenAI 风格）快照里**看不到比例** ——
	// 排查任务 task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q 时，只能绕道查上游
	// 有赞回执才发现实收 ratio=adaptive。
	//
	// ⚠️ 为什么要用 relaycommon.ResolveTaskRatio 统一算：快照必须回答的是
	// 「**实际下发给上游的比例是什么**」，而不是「客户端在哪些字段里写了什么」。
	// 首轮修复把顶层与 metadata 记成两个独立键（ratio / aspect_ratio），
	// 排查时要自己推断谁优先 —— 而这正是各适配器口径曾经漂移的地方
	// （megabyai 是顶层优先，youzanwan3 是 metadata 优先）。
	// 现在用与适配器**同一个**解析器算，快照里的 ratio 就是上游收到的 ratio。
	//
	// 记三个东西：
	//   ratio         —— 最终生效的比例（与上游实收一致）
	//   ratio_source  —— 生效比例来自哪个字段（一眼区分「没传」与「被降级」）
	//   ratio_raw     —— 客户端原始写法（顶层 / metadata 都保留，便于核对）
	ratioResult := relaycommon.ResolveTaskRatio(req, relaycommon.TaskRatioOptions{
		// 快照是全渠道通用的，用**并集**档位表 —— 各渠道自己的档位表是子集，
		// 少一档就会把该渠道合法的比例误判成「非法」。
		Candidates:   snapshotRatioCandidates(),
		DefaultRatio: "",
		SizeToRatio:  ratioFromSnapshotSize,
	})
	if ratioResult.Ratio != "" {
		params["ratio"] = ratioResult.Ratio
		params["ratio_source"] = string(ratioResult.Source)
	}
	if ratioResult.Rejected != "" {
		// 客户端传了但不被支持的比例 —— 单独记，别混进 ratio，
		// 否则会把「非法值」误当成「生效值」。
		params["ratio_invalid"] = ratioResult.Rejected
	}
	// 原始写法：顶层与 metadata 都留，便于核对「顶层与 metadata 不一致」。
	if raw := strings.TrimSpace(req.Ratio); raw != "" {
		params["ratio_top_level"] = raw
	}
	if raw := strings.TrimSpace(req.AspectRatio); raw != "" {
		params["aspect_ratio_top_level"] = raw
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["ratio"].(string); ok && strings.TrimSpace(raw) != "" {
			params["ratio_metadata"] = strings.TrimSpace(raw)
		}
		if raw, ok := req.Metadata["aspect_ratio"].(string); ok && strings.TrimSpace(raw) != "" {
			params["aspect_ratio_metadata"] = strings.TrimSpace(raw)
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
// 具体实现收敛在 relaycommon.CompactTaskMediaURL（日志详情与提交快照共用一套口径）。
func compactMediaURL(raw string) string {
	return relaycommon.CompactTaskMediaURL(raw)
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

// ratioFromSnapshotSize 把 size 尺寸推成比例档位，供提交快照使用。
//
// 快照是**全渠道通用**的记录，不该带某个渠道的档位表，所以这里只做
// 「尺寸 → 最接近的常见比例」的映射，且差距超过 0.25 就判定为推不出
// （返回 false）—— 宁可快照里不记比例，也不要记一个编造的比例。
//
// 各渠道适配器自己算比例时有各自的合法档位表（心笔只认 6 档、
// 视频生成车站还多一档 21:9），快照记的是「客户端意图」，不是
// 「渠道最终下发的值」—— 后者在各渠道的 task_logs 里另有记录。
func ratioFromSnapshotSize(size string) (string, bool) {
	size = strings.TrimSpace(strings.ToLower(size))
	if size == "" {
		return "", false
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", false
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return "", false
	}
	candidates := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
		"4:3":  4.0 / 3.0,
		"3:4":  3.0 / 4.0,
		"1:1":  1.0,
	}
	ratio := float64(width) / float64(height)
	best := ""
	bestDiff := math.MaxFloat64
	for name, value := range candidates {
		if diff := math.Abs(ratio - value); diff < bestDiff {
			bestDiff = diff
			best = name
		}
	}
	if bestDiff > 0.25 {
		return "", false
	}
	return best, true
}

// snapshotRatioCandidates 是提交快照用的**并集**比例档位表。
//
// 各渠道的合法档位不一样（心笔 6 档、视频生成车站多一档 21:9、
// 有赞按模型分 smart / r2v 两套）。快照要能认出「这些渠道都认的比例」，
// 否则会把某个渠道合法的比例误标成非法。
//
// 这里取站内已知的全部档位的并集；出现未知写法时按「非法」记录到
// ratio_invalid —— 那正是排查时最该看到的信号。
func snapshotRatioCandidates() relaycommon.TaskRatioCandidates {
	return relaycommon.TaskRatioCandidates{
		"adaptive": true,
		"16:9":     true,
		"9:16":     true,
		"4:3":      true,
		"3:4":      true,
		"1:1":      true,
		"21:9":     true,
	}
}
