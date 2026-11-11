package common

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
)

const (
	// taskRequestParamsPromptLimit 是日志里保留的提示词长度。
	// 长剧本可达上万字，日志只需要「能认出是哪一单」。
	taskRequestParamsPromptLimit = 200
	// taskRequestParamsMediaLimit 与上游 max_input_files 同量级，超出部分不再记录。
	taskRequestParamsMediaLimit = 20
)

// BuildTaskRequestParamsSummary 把任务请求压成「客户实际发了什么」的摘要。
//
// 为什么要有它：日志页只能看到模型/渠道/费用，排查 422/502 时无法回答
// 「这次到底带没带参考视频、时长多少、比例是什么」。task_logs 里的提交快照
// 只覆盖成功建单的请求，错误日志完全没有参数。
//
// 记录口径是**客户原始请求**：本函数必须在条件路由改写 body 之前调用
// （见 middleware.CaptureTaskRequestParams），否则记下来的是内部目标模型。
//
// 只收关键字段，不收完整 body：prompt 截断 200 字、data URI 压成体积摘要，
// 其余原样保留 —— 素材 URL 是复现问题的唯一线索，不能被截断。
func BuildTaskRequestParamsSummary(req *TaskSubmitReq) map[string]any {
	if req == nil {
		return nil
	}
	summary := make(map[string]any, 10)
	if model := strings.TrimSpace(req.Model); model != "" {
		summary["model"] = model
	}
	if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		summary["prompt"] = truncateTaskRequestRunes(prompt, taskRequestParamsPromptLimit)
	}
	if seconds := strings.TrimSpace(req.Seconds); seconds != "" {
		summary["seconds"] = seconds
	}
	if req.Duration > 0 {
		summary["duration"] = req.Duration
	}
	// 分辨率与尺寸优先记顶层字段，缺失时回落到 metadata ——
	// 两条路径都会被适配器读取，只记一条会漏掉真实下发的值。
	if resolution := firstNonEmptyString(req.Resolution, metadataString(req.Metadata, "resolution")); resolution != "" {
		summary["resolution"] = resolution
	}
	if size := firstNonEmptyString(req.Size, metadataString(req.Metadata, "size")); size != "" {
		summary["size"] = size
	}
	// 比例取值优先级与适配器一致：metadata > 顶层。
	if ratio := firstNonEmptyString(
		metadataString(req.Metadata, "ratio"),
		metadataString(req.Metadata, "aspect_ratio"),
		req.Ratio,
		req.AspectRatio,
	); ratio != "" {
		summary["ratio"] = ratio
	}
	if req.Seed != nil {
		summary["seed"] = *req.Seed
	}

	media := collectTaskRequestMedia(req)
	if len(media) > 0 {
		summary["media"] = media
		summary["media_counts"] = taskRequestMediaCounts(media)
	}
	if fields := taskRequestMediaFields(req); len(fields) > 0 {
		summary["media_fields"] = fields
	}
	if len(summary) == 0 {
		return nil
	}
	return summary
}

// taskRequestMediaCounts 按 type 统计素材数量，便于一眼看出「带了几条参考视频」。
func taskRequestMediaCounts(media []map[string]string) map[string]int {
	counts := make(map[string]int, len(media))
	for _, item := range media {
		counts[item["type"]]++
	}
	return counts
}

// taskRequestMediaFields 记录客户用了哪些素材字段（字段名 → 条数）。
//
// 必须按字段记而不是只记合并结果：适配器对 media / images / image 的取舍
// 各不相同，出问题时第一个要回答的就是「他到底把素材放在哪个字段里」。
func taskRequestMediaFields(req *TaskSubmitReq) map[string]int {
	fields := make(map[string]int, 9)
	if len(req.Media) > 0 {
		fields["media"] = len(req.Media)
	}
	if media := metadataTaskMedia(req.Metadata, "media"); len(media) > 0 {
		fields["metadata_media"] = len(media)
	}
	if inputMedia := metadataInputTaskMedia(req.Metadata); len(inputMedia) > 0 {
		fields["metadata_input_media"] = len(inputMedia)
	}
	if len(req.Images) > 0 {
		// input_reference 在 UnmarshalJSON 里被并进 Images，按字段统计时要扣掉，
		// 否则客户只写了 input_reference，日志却显示他也用了 images。
		imageListCount := len(req.Images)
		if strings.TrimSpace(req.InputReference) != "" && imageListCount > 0 {
			imageListCount--
		}
		if imageListCount > 0 {
			fields["images"] = imageListCount
		}
	}
	if strings.TrimSpace(req.Image) != "" {
		fields["image"] = 1
	}
	if len(req.Videos) > 0 {
		fields["videos"] = len(req.Videos)
	}
	if strings.TrimSpace(req.Video) != "" {
		fields["video"] = 1
	}
	if len(req.Audios) > 0 {
		fields["audios"] = len(req.Audios)
	}
	if strings.TrimSpace(req.Audio) != "" {
		fields["audio"] = 1
	}
	if strings.TrimSpace(req.InputReference) != "" {
		fields["input_reference"] = 1
	}
	return fields
}

// collectTaskRequestMedia 合并所有素材容器，顺序与适配器的读取顺序一致。
//
// 只认「客户写了什么」，不做去重：同一 URL 出现两次本身就是排查线索
// （例如参考视频被客户端重复投递）。
func collectTaskRequestMedia(req *TaskSubmitReq) []map[string]string {
	media := make([]map[string]string, 0, 8)
	appendMedia := func(mediaType, rawURL string) {
		url := strings.TrimSpace(rawURL)
		if url == "" {
			return
		}
		mediaType = strings.TrimSpace(mediaType)
		if mediaType == "" {
			mediaType = "unknown"
		}
		media = append(media, map[string]string{
			"type": mediaType,
			"url":  CompactTaskMediaURL(url),
		})
	}

	for _, item := range req.Media {
		appendMedia(item.Type, item.URL)
	}
	for _, item := range metadataTaskMedia(req.Metadata, "media") {
		appendMedia(item.Type, item.URL)
	}
	for _, item := range metadataInputTaskMedia(req.Metadata) {
		appendMedia(item.Type, item.URL)
	}
	// 兼容口字段：归一成与 media 相同的语义类型，统计「参考图/参考视频/参考音频各有几条」
	// 时才不会因为客户端写法不同而分裂成两套计数。
	for _, url := range req.Images {
		appendMedia("reference_image", url)
	}
	appendMedia("reference_image", req.Image)
	for _, url := range req.Videos {
		appendMedia("reference_video", url)
	}
	appendMedia("reference_video", req.Video)
	for _, url := range req.Audios {
		appendMedia("reference_audio", url)
	}
	appendMedia("reference_audio", req.Audio)
	// input_reference 在 UnmarshalJSON 里已经并入 Images，这里不再重复追加，
	// 否则同一张图会被记两次（media_fields 里仍保留 input_reference 的痕迹）。

	if len(media) > taskRequestParamsMediaLimit {
		return media[:taskRequestParamsMediaLimit]
	}
	return media
}

// metadataTaskMedia 读取 metadata.media：[]interface{} 逐项转成 TaskMedia。
func metadataTaskMedia(metadata map[string]interface{}, key string) []TaskMedia {
	if metadata == nil {
		return nil
	}
	raw, ok := metadata[key].([]interface{})
	if !ok {
		return nil
	}
	return toTaskMediaList(raw)
}

// metadataInputTaskMedia 读取 metadata.input.media（有赞/心笔都支持的第二层容器）。
func metadataInputTaskMedia(metadata map[string]interface{}) []TaskMedia {
	if metadata == nil {
		return nil
	}
	input, ok := metadata["input"].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := input["media"].([]interface{})
	if !ok {
		return nil
	}
	return toTaskMediaList(raw)
}

func toTaskMediaList(raw []interface{}) []TaskMedia {
	media := make([]TaskMedia, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		media = append(media, TaskMedia{
			Type: metadataString(entry, "type"),
			URL:  metadataString(entry, "url"),
		})
	}
	return media
}

func metadataString(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func truncateTaskRequestRunes(value string, limit int) string {
	if limit <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…(truncated)"
}

// CompactTaskMediaURL 把内联 base64 媒体压成可读摘要。
//
// data URI 单条可达数 MB，原样落库会把日志行撑爆；压成体积摘要后
// 仍能看出「确实是内联素材」以及格式与大小。
func CompactTaskMediaURL(raw string) string {
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

// SetTaskRequestParams 把摘要挂到 Context，供消费/错误日志落库时读取。
func SetTaskRequestParams(c *gin.Context, params map[string]any) {
	if c == nil || len(params) == 0 {
		return
	}
	c.Set(string(constant.ContextKeyTaskRequestParams), params)
}

// GetTaskRequestParams 读取请求参数摘要；未捕获时返回 nil。
func GetTaskRequestParams(c *gin.Context) map[string]any {
	if c == nil {
		return nil
	}
	value, ok := c.Get(string(constant.ContextKeyTaskRequestParams))
	if !ok {
		return nil
	}
	params, _ := value.(map[string]any)
	return params
}
