package model

import (
	"strings"
)

// modelTagRule 是「模型名 → 媒体标签」的一条匹配规则。
//
// 语义与 vendorRule 一致：pattern 命中子串、prefix 要求前缀，**有序**、
// 先命中先算。tag 为空字符串表示「这是文本/对话模型，不该挂媒体标签」。
type modelTagRule struct {
	pattern string
	prefix  string
	tag     string
}

// defaultModelTagRules 是模型标签规则表。
//
// 标签只描述**产出形态**（video / image），不描述能力细节 —— 后者（推理、
// 工具、上下文长度）是逐个模型的属性，写在规则表里会立刻过期。
//
// 视频/图片模型必须带标签：模型广场的「模型标签」筛选与卡片角标都靠它，
// 漏标等于这个模型在筛选里搜不到（生产曾有 17 个视频/图片模型无标签）。
//
// ⚠️ 文本模型的 tag 为空字符串，含义是「不应挂 image/video」。
//
//	同步时只会摘掉 image/video 这类媒体标签，管理员写的其它标签
//	（推理、工具、1M…）一律保留。
var defaultModelTagRules = []modelTagRule{
	// ── 图片 ──
	// 放在 "gpt-6"/"gpt-5" 之前：gpt-image-* 是出图模型，不是对话模型。
	{pattern: "gpt-image", tag: "image"},
	{pattern: "nano banana", tag: "image"},
	{pattern: "seedream", tag: "image"},
	{pattern: "h3-image", tag: "image"},

	// ── 视频 ──
	// "seedance" 一条盖住 seedance-* / cp-seedance-* / el-seedance-* /
	// seedance2.5-特惠·活动；"wan3" 一条盖住 wan3.0-* 与 hk-wan3。
	{pattern: "seedance", tag: "video"},
	{pattern: "jimeng", tag: "video"},
	{pattern: "wan3", tag: "video"},
	{pattern: "wan-3.0", tag: "video"},
	{pattern: "wan2.7", tag: "video"},
	{pattern: "minimax_h3", tag: "video"},

	// ── 文本/对话模型：不带媒体标签 ──
	// "gemini-web-video" 必须在 "gemini" 之前：它是视频生成，不是对话。
	{pattern: "gemini-web-video", tag: "video"},
	{pattern: "gpt-6", tag: ""},
	{pattern: "gpt-5", tag: ""},
	{pattern: "claude", tag: ""},
	{pattern: "kimi", tag: ""},
	{pattern: "glm-", tag: ""},
	{pattern: "deepseek", tag: ""},
	{pattern: "qwen", tag: ""},
	{pattern: "minimax-m", tag: ""},
	{pattern: "gemini", tag: ""},
}

// mediaTagValues 是「媒体标签」取值集合：只这两种算媒体形态。
var mediaTagValues = map[string]bool{"image": true, "video": true}

// classifyModelTag 判定模型该挂什么标签。
//
// 返回值：
//   - ("image"|"video", false)：该挂这个媒体标签；
//   - ("", true)：文本/对话模型，不该挂媒体标签；
//   - ("", false)：判定不了（未知模型），调用方不要动它的标签。
func classifyModelTag(modelName string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(modelName))
	if lower == "" {
		return "", false
	}
	for _, rule := range defaultModelTagRules {
		if rule.prefix != "" {
			if strings.HasPrefix(lower, rule.prefix) {
				return rule.tag, rule.tag == ""
			}
			continue
		}
		if rule.pattern != "" && strings.Contains(lower, rule.pattern) {
			return rule.tag, rule.tag == ""
		}
	}
	return "", false
}

// splitTags 把逗号/分号/竖线/空白分隔的标签串拆成切片。
func splitTags(tags string) []string {
	fields := strings.FieldsFunc(tags, func(r rune) bool {
		switch r {
		case ',', ';', '|', ' ', '\t', '\n', '\r':
			return true
		}
		return false
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}

// mergeTags 把标签按「先媒体后其它」的顺序拼回去，保证同一组标签的
// 序列化结果稳定（否则每次同步可能把顺序打乱，前端 diff 会看到噪声）。
func mergeTags(tags []string) string {
	media := make([]string, 0, 2)
	others := make([]string, 0, len(tags))
	for _, t := range tags {
		if mediaTagValues[strings.ToLower(t)] {
			media = append(media, t)
			continue
		}
		others = append(others, t)
	}
	return strings.Join(append(media, others...), ",")
}

// applyModelTag 按规则算出该模型应有的标签串。
//
// 返回值：
//   - (newTags, true)：应改成 newTags；
//   - (old, false)：保持原样（判定不了，或已经一致）。
func applyModelTag(current string, modelName string) (string, bool) {
	want, isText := classifyModelTag(modelName)
	if want == "" && !isText {
		return current, false // 判定不了，不动
	}
	currentTags := splitTags(current)
	if want == "" {
		// 文本模型：摘掉媒体标签，其它标签保留
		kept := make([]string, 0, len(currentTags))
		for _, t := range currentTags {
			if !mediaTagValues[strings.ToLower(t)] {
				kept = append(kept, t)
			}
		}
		if len(kept) == len(currentTags) {
			return current, false
		}
		return mergeTags(kept), true
	}
	// 媒体模型：媒体标签必须且只能是 want
	hasWant := false
	others := make([]string, 0, len(currentTags))
	for _, t := range currentTags {
		lower := strings.ToLower(t)
		if mediaTagValues[lower] {
			if lower == want && !hasWant {
				hasWant = true
			}
			continue
		}
		others = append(others, t)
	}
	if hasWant && len(others) == len(currentTags)-1 {
		return current, false // 已正确
	}
	return mergeTags(append([]string{want}, others...)), true
}
