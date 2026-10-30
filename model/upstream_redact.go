package model

import (
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
	"seedance2.5-特惠": true, // 上游是 wan3.0-video-prime（有赞渠道，2026-10-29 起）
	"seedance2.5特惠版": true, // 同一模型的历史旧名
	// 两跳链路（2026-10-30 加）：上游是 wan3.0-video 的 480P 原片。
	//
	// 为什么它比别的模型更该脱敏：出口 2 的 data 字段里挂着**上游 OSS 签名直链**
	// （带 OSSAccessKeyId + Signature，24 小时内有效），而那条直链指向的是
	// **还没做提升的 480P 原片** —— 用户能绕过本站直接下载到没提升的成片，
	// 商业逻辑直接被击穿。遮掉它是「交付正确性」问题，不只是「藏货源」问题。
	"wan3.0-720p-官网": true,
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

// upstreamURLPattern 用于识别文案里的地址。
// 上游成片直链（R2 域名 + hailuo-video 货源目录）本身就是"真实上游是谁"的证据，
// 别名模型的下游文案里要**整个摘掉**（见 RedactUpstreamText）。
var upstreamURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+`)

// upstreamURLPlaceholder 是被摘掉的上游地址的占位符。
const upstreamURLPlaceholder = "[地址已隐藏]"

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
	return redactUpstreamIdentityText(content)
}

// RedactUpstreamText 把下游可见文案里的「上游地址」与「上游身份字样」一起清掉。
//
// 为什么地址也要清：上游的成片直链（R2 域名 + hailuo-video 货源目录）本身就是
// "真实上游是谁"的证据。交付地址已经改走本站 /content 代理，错误文案里再留一个
// 上游地址没有任何用途 —— 留着就等于把货源写在脸上。
//
// 只对"对外别名模型"生效（见 upstreamAliasModels），其它模型原样返回。
func RedactUpstreamText(content string, modelName string) string {
	if content == "" || !IsUpstreamAliasModel(modelName) {
		return content
	}
	// 先摘地址：脱敏字样会把 URL 里的 wan3.0 换成「上游」，地址反正要删，
	// 不必再费劲保护 URL 区段。
	text := upstreamURLPattern.ReplaceAllString(content, upstreamURLPlaceholder)
	return RedactUpstreamIdentity(text, modelName)
}

func redactUpstreamIdentityText(text string) string {
	for _, token := range upstreamIdentityTokens {
		text = token.re.ReplaceAllString(text, upstreamNeutralWord)
	}
	for _, re := range upstreamIdentityPatterns {
		text = re.ReplaceAllString(text, upstreamNeutralWord)
	}
	// 连续替换可能产生 "上游 上游"，收敛成一个
	text = strings.ReplaceAll(text, upstreamNeutralWord+" "+upstreamNeutralWord, upstreamNeutralWord)
	return text
}
