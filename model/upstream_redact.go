package model

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// upstreamAliasModels 列出「对外用别名售卖、上游却另有其模型/供应商」的对外模型名。
//
// 为什么只针对这几个模型：这类模型对外展示的是别名（例如 seedance2.5-特惠），
// 一旦任何下游可见的字段里出现上游或供应商的字样（wan3 / aliyun / 有赞 …），
// 用户就能直接反推出真实货源，别名就白起了。其它模型本来就是原样对外，
// 遮它们的字样只会让报错更难读，还可能把用户自己买的东西的名字也打散。
var upstreamAliasModels = map[string]bool{
	"seedance2.5-特惠": true, // 上游是 wan-3.0（第三方渠道）
	"seedance2.5特惠版": true, // 同一模型的历史旧名
}

// upstreamNeutralWord 是替换后的中性词：既点明"来自上游"，又不暴露是谁。
const upstreamNeutralWord = "上游"

// upstreamIdentityLiterals 需要从下游可见文案里抹掉的上游/供应商字样。
//
// 顺序敏感：长词必须排在前面，保证 "aliyun:wan-3.0"、"wan3.0-video-prime-1080p"
// 这类组合词被整体替换，而不是先命中短词、留下 "上游:上游" 这种碎片。
var upstreamIdentityLiterals = []string{
	// 组合写法
	"aliyun:wan-3.0",
	"aliyun:wan3.0",
	"aliyun wan3",
	// 阿里 / 万相
	"阿里云", "阿里", "万相", "aliyun",
	// wan3 系列：我们对外卖的 wan3.0-* 变体 + 上游的 wan-3.0
	"wan3.0-video-prime-1080p",
	"wan3.0-1080p-慢速",
	"wan3.0-video-官网",
	"wan3.0-video-prime",
	"wan3.0-video-smart",
	"wan3.0-video-480p",
	"wan3.0-smart",
	"wan3.0-480p",
	"wan3.0-video",
	"wan3.0",
	"wan-3.0",
	"wan3",
	// 其它上游供应商
	"腾讯云", "腾讯", "tencent cloud", "tencent",
	"有赞", "youzan",
	"豆包", "doubao",
	"火山", "volcengine",
	"dashscope", "百炼",
	"漫屋", "manwu", "arcreel",
	"三元", "巡狩",
	"二春", "erchun",
	"youkou",
	"sensenova", "商汤",
	"megabyai", "megaai",
	"dola",
	"gpt-pool", "sub2api", "coding plan",
	"autodl",
}

// upstreamIdentityPatterns 是字面量覆盖不到、但仍可能暴露上游的写法。
var upstreamIdentityPatterns = []*regexp.Regexp{
	// 独立的 "ali"：不能裸匹配，否则 validate / invalid 会被误伤
	regexp.MustCompile(`(?i)\bali\b`),
	// LK888 这类渠道编号
	regexp.MustCompile(`(?i)\bLK[0-9]+\b`),
	// wan3 / wan 3 / wan-3 的其它写法
	regexp.MustCompile(`(?i)\bwan[\s\-_]?3(\.0)?\b`),
}

// upstreamURLPattern 用于标记「不能动」的 URL 区段。
// 交付链接（成片直链）就在里面：改一个字链接就废了，所以只遮 URL 之外的文案。
var upstreamURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+`)

type identityToken struct {
	literal string
	re      *regexp.Regexp
}

// upstreamIdentityTokens 是按字面长度倒序排好的字面量表，
// 保证同一个起点上先命中更长的词。
var upstreamIdentityTokens = buildIdentityTokens()

func buildIdentityTokens() []identityToken {
	literals := append([]string(nil), upstreamIdentityLiterals...)
	sort.SliceStable(literals, func(i, j int) bool {
		return len(literals[i]) > len(literals[j])
	})
	tokens := make([]identityToken, 0, len(literals))
	for _, literal := range literals {
		tokens = append(tokens, identityToken{
			literal: literal,
			re:      regexp.MustCompile(`(?i)` + regexp.QuoteMeta(literal)),
		})
	}
	return tokens
}

// IsUpstreamAliasModel 判断对外模型名是不是「上游别名模型」。
// 只有别名模型需要向下游隐藏真实上游。
func IsUpstreamAliasModel(modelName string) bool {
	return upstreamAliasModels[modelName]
}

// RedactUpstreamIdentity 把文案里暴露上游/供应商身份的字样替换成中性词。
//
// 只对"对外别名模型"生效（见 upstreamAliasModels），
// 其它模型原样返回：它们的名字本来就是对外公开的，遮掉只会让报错更难读。
func RedactUpstreamIdentity(content string, modelName string) string {
	if content == "" || !IsUpstreamAliasModel(modelName) {
		return content
	}
	return redactUpstreamIdentityText(content, false)
}

// RedactUpstreamIdentityKeepURL 与 RedactUpstreamIdentity 相同，但 URL 区段内的字样不动。
// 用于可能夹带交付链接的字段（成片地址被改一个字链接就废了）。
func RedactUpstreamIdentityKeepURL(content string, modelName string) string {
	if content == "" || !IsUpstreamAliasModel(modelName) {
		return content
	}
	return redactUpstreamIdentityText(content, true)
}

// RedactUpstreamIdentityJSON 对 JSON 文本做脱敏（URL 区段不动）。
// 直接在 JSON 文本上替换，替换词不含引号/反斜杠，替换后仍是合法 JSON。
func RedactUpstreamIdentityJSON(raw []byte, modelName string) []byte {
	if len(raw) == 0 || !IsUpstreamAliasModel(modelName) {
		return raw
	}
	redacted := redactUpstreamIdentityText(string(raw), true)
	if !json.Valid([]byte(redacted)) {
		// 兜底：脱敏后不是合法 JSON 就原样返回，绝不把下游的响应体搞坏
		return raw
	}
	return []byte(redacted)
}

func redactUpstreamIdentityText(text string, protectURLs bool) string {
	var protected [][2]int
	if protectURLs {
		for _, loc := range upstreamURLPattern.FindAllStringIndex(text, -1) {
			protected = append(protected, [2]int{loc[0], loc[1]})
		}
	}
	for _, token := range upstreamIdentityTokens {
		text = replaceAllSkippingRanges(text, token.re, protected)
	}
	for _, re := range upstreamIdentityPatterns {
		text = replaceAllSkippingRanges(text, re, protected)
	}
	// 连续替换可能产生 "上游 上游"，收敛成一个
	text = strings.ReplaceAll(text, upstreamNeutralWord+" "+upstreamNeutralWord, upstreamNeutralWord)
	return text
}

// replaceAllSkippingRanges 把 re 命中的片段替换成中性词，但跳过落在受保护区段内的命中。
func replaceAllSkippingRanges(text string, re *regexp.Regexp, protected [][2]int) string {
	matches := re.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var sb strings.Builder
	pos := 0
	for _, m := range matches {
		if overlapsAny(m[0], m[1], protected) {
			continue
		}
		sb.WriteString(text[pos:m[0]])
		sb.WriteString(upstreamNeutralWord)
		pos = m[1]
	}
	sb.WriteString(text[pos:])
	return sb.String()
}

func overlapsAny(start, end int, ranges [][2]int) bool {
	for _, r := range ranges {
		if start < r[1] && r[0] < end {
			return true
		}
	}
	return false
}
