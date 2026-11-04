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
//
// ⚠️ 这是所有脱敏逻辑的**总开关**：IsUpstreamAliasModel 返回 false 时，
// RedactUpstreamIdentity 与 redactAliasTaskUpstream 都直接 return —— 不报错、
// 不告警，上游模型名和上游回执原样漏给下游。所以从这里删一行等于关掉一条防线的开关。
// 改动前先看 model/channel_hide_guard_test.go 与
// relay/seedance25_tehui_channel_hide_test.go，那里锁住了这份名单。
var upstreamAliasModels = map[string]bool{
	// 上游是渠道 17「有赞 Wan3」(type 63) 的 wan3.0-video-prime（2026-10-29 起）。
	// 生产 abilities 表里这个别名与 wan3.0-video-prime 同挂渠道 17。
	"seedance2.5-特惠": true,
	"seedance2.5特惠版": true, // 同一模型的历史旧名
	// seedance2.5-活动（2026-11-02 加）：seedance2.5 促销族的**按次档**，
	// 与母本 seedance2.5-特惠 是同一个商品的两种售价形态。
	//
	// ⚠️ 2026-11-03 起上游**变了**：活动版的常驻链路从渠道 17「有赞 Wan3」的
	// wan3.0-video-prime 换成渠道 25「心笔 Model Router」的 wan3.0-video
	// （兜底路由才走豆包 seedance-2-5）。
	//
	// ⚠️ 必须与母本享受**完全相同**的保护级别：漏了它，
	// redactAliasTaskUpstream 会直接 return —— 上游回执（任务号 + 成本 +
	// 原片直链）原样进对外 DTO。而这是促销档，下单的人最多、最需要藏货源。
	//
	// ⚠️ 走心笔链路的额外风险：成片还要经交付处理才对外，出口里一旦带出
	// **未经交付处理的 480P 源片直链**，用户既能看穿货源、也能绕过本站拿走源片。
	"seedance2.5-活动": true,
	// 两跳链路（2026-10-30 加）：上游是 wan3.0-video 的 480P 原片。
	//
	// 为什么它比别的模型更该脱敏：出口 2 的 data 字段里挂着**上游 OSS 签名直链**
	// （带 OSSAccessKeyId + Signature，24 小时内有效），而那条直链指向的是
	// **还没做提升的 480P 原片** —— 用户能绕过本站直接下载到没提升的成片，
	// 商业逻辑直接被击穿。遮掉它是「交付正确性」问题，不只是「藏货源」问题。
	//
	// ⚠️ 新旧两个名字**都要在名单里**（旧名 2026-10-30 改名前的公开名，
	// 过渡期保留可调用）。漏了旧名 → 旧名调用方的 data 字段仍会漏出上游直链。
	"wan3.0-720p-特惠": true,
	"wan3.0-720p-官网": true,
	// 按次档（2026-11-01 加）：上游是心笔 Model Router 的 wan3.0-video 源片，
	// 成片还要走交付处理才对外。与特惠同一套风险：出口里一旦带出上游模型名或
	// **未经交付处理的源片直链**，用户既能看穿货源、也能绕过本站拿走源片。
	"wan3.0-720p-按次": true,
	// wan3.0-排队（2026-11-04 加）：上游是「视频生成车站」排队通道的万相 wan3，
	// 底层是攒批发车的车队模式。
	//
	// 为什么必须脱敏：对外名「wan3.0-排队」里的 wan3 就是上游模型名本身，
	// 而上游回执（task.Data）里还有上游任务号 T130、上游成片直链域名与路径。
	// 不脱敏的话，用户一眼就能反推货源是万相，也能直接看到车站在上游的目录结构。
	//
	// ⚠️ 另有「交付正确性」风险：上游直链只有约 24 小时有效，本站已把成片
	// 转存到 COS；若脱敏失效导致 data 字段漏出上游直链，用户会绕过本站
	// 直接去抓那条**会过期的**地址，过期后拿到的是一个打不开的链接，
	// 却以为是自己拿到了成片。
	"wan3.0-排队": true,
}

// upstreamNeutralWord 是替换后的中性词：既点明"来自上游"，又不暴露是谁。
const upstreamNeutralWord = "上游"

// upstreamVendorLiterals 是**渠道 / 供应商身份词**：所有模型都要替换成「上游」。
//
// 口径（2026-10-30 老板定）：有渠道的，直接说成「上游」就行。
// 这类词出现就等于点名了货源（"youzan wan3 rejected the request"、
// "model aliyun:wan-3.0 does not support"），跟客户买的是哪个模型无关 ——
// 客户调 gpt-4o 时上游报 aliyun 的错误，照样不该让他看见。
var upstreamVendorLiterals = []string{
	// 阿里 / 万相
	"阿里云", "阿里", "万相", "aliyun",
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
	// 视频生成车站（2026-11-04 加）：上游站点域名与品牌词。
	// 出现在上游回执/报错里的这些字样等于直接报出货源。
	"视频生成车站", "yuanmengstudio", "yuanmeng",
}

// upstreamModelLiterals 是**上游模型名**：只对别名模型替换。
//
// 为什么不全局替换：非别名模型（客户直接调 wan3.0-720p-特惠）的报错里
// 提到自己买的模型名是正常的，替换成「上游」反而看不懂。只有别名模型
// 才需要连模型名一起抹 —— 那是他本不该知道的另一条链路的名字。
//
// 顺序敏感：长词必须排在前面，保证 "wan3.0-video-prime-1080p" 这类组合词
// 被整体替换，而不是先命中短词、留下 "上游-prime-1080p" 这种碎片。
var upstreamModelLiterals = []string{
	// 组合写法（供应商部分由 upstreamVendorLiterals 负责）
	"aliyun:wan-3.0",
	"aliyun:wan3.0",
	"aliyun wan3",
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
	// 条件路由的目标模型名（2026-10-30 加）。
	//
	// seedance-2-5 本身是我们公开售卖的模型，不暴露货源；但它出现在
	// seedance2.5-特惠 的报错文案里，等于把「你调 A、实际跑 B」的改派
	// 结果告诉客户（规则一：30 秒 → seedance-2-5）。
	"seedance-2-5",
}

// upstreamVendorPatterns 是字面量覆盖不到的供应商写法。
var upstreamVendorPatterns = []*regexp.Regexp{
	// 独立的 "ali"：不能裸匹配，否则 validate / invalid 会被误伤
	regexp.MustCompile(`(?i)\bali\b`),
	// LK888 这类渠道编号
	regexp.MustCompile(`(?i)\bLK[0-9]+\b`),
}

// upstreamModelPatterns 是字面量覆盖不到的上游模型名写法。
var upstreamModelPatterns = []*regexp.Regexp{
	// wan3 / wan 3 / wan-3 的其它写法
	regexp.MustCompile(`(?i)\bwan[\s\-_]?3(\.0)?\b`),
}

// upstreamURLPattern 用于识别文案里的地址。
// 上游成片直链（R2 域名 + hailuo-video 货源目录）本身就是"真实上游是谁"的证据，
// 下游文案里要**整个摘掉**（见 RedactUpstreamAddress）。
var upstreamURLPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+`)

// upstreamURLTrailingPunctuation 是地址末尾**不属于地址本身**的标点。
//
// 地址常常被写在括号里或句末：
//
//	"错误 (https://x.com/a.mp4)"、"详见 https://x.com/a.mp4。"
//
// 整段替换成 "[地址已隐藏]" 会把括号/句号一起吞掉，剩下 "([地址已隐藏]" ——
// 括号不配对、文案破相，客户看着像系统故障。所以这些标点要**摘掉地址后原样留下**。
const upstreamURLTrailingPunctuation = ".,;:!?)]}>\"'，。；：！？、）】》」』"

var upstreamURLPunctuationPattern = regexp.MustCompile(`[` + regexp.QuoteMeta(upstreamURLTrailingPunctuation) + `]+$`)

// upstreamURLPlaceholder 是被摘掉的上游地址的占位符。
const upstreamURLPlaceholder = "[地址已隐藏]"

type identityToken struct {
	literal string
	re      *regexp.Regexp
}

var (
	upstreamVendorTokens = buildIdentityTokens(upstreamVendorLiterals)
	upstreamModelTokens  = buildIdentityTokens(upstreamModelLiterals)
)

// buildIdentityTokens 按字面长度倒序排好，保证同一个起点上先命中更长的词。
func buildIdentityTokens(source []string) []identityToken {
	literals := append([]string(nil), source...)
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

// RedactUpstreamVendor 把渠道/供应商身份词换成「上游」，**所有模型都生效**。
//
// 这是老板 2026-10-30 定的口径：有渠道的，直接说成「上游」就行。
// 上游报错里点名货源（youzan / aliyun / dashscope / 漫屋 …）跟客户买的是
// 哪个模型无关，一律换成「上游」。
func RedactUpstreamVendor(content string) string {
	if content == "" {
		return content
	}
	return redactWithTokens(content, upstreamVendorTokens, upstreamVendorPatterns)
}

// RedactUpstreamIdentity 把文案里暴露上游身份的字样替换成中性词。
//
// 只对"对外别名模型"生效（见 upstreamAliasModels）：别名模型连**上游模型名**
// 也要抹（wan3.0 / seedance-2-5），因为那是他本不该知道的另一条链路的名字。
// 其它模型原样返回 —— 但调用方通常还会再走一遍 RedactUpstreamVendor，
// 供应商词照样会被替换。
func RedactUpstreamIdentity(content string, modelName string) string {
	if content == "" || !IsUpstreamAliasModel(modelName) {
		return content
	}
	text := RedactUpstreamVendor(content)
	return redactWithTokens(text, upstreamModelTokens, upstreamModelPatterns)
}

// RedactUpstreamAddress 把文案里的地址整个摘掉，**不分模型**。
//
// 为什么地址要单独提出来：地址是最硬的渠道证据 —— 客户拿到就能直接访问上游，
// 甚至绕过我们。地址没有任何「客户需要留着」的理由，所以对所有模型一律摘。
//
// 尾部标点保留：地址后面的 ")" / "。" 不属于地址，摘地址时一并吞掉会让文案破相
// （"([地址已隐藏]" 括号不配对）。
func RedactUpstreamAddress(content string) string {
	if content == "" {
		return content
	}
	return upstreamURLPattern.ReplaceAllStringFunc(content, func(raw string) string {
		// 把地址末尾的标点摘出来，替换后原样接回去
		trimmed := upstreamURLPunctuationPattern.FindString(raw)
		if trimmed == "" {
			return upstreamURLPlaceholder
		}
		return upstreamURLPlaceholder + trimmed
	})
}

// RedactUpstreamText 是下游文案脱敏的统一入口：
//
//	① 摘地址       —— 所有模型（地址是最硬的渠道证据）
//	② 换供应商词   —— 所有模型（老板口径：有渠道的直接说「上游」）
//	③ 换上游模型名 —— 仅别名模型（非别名模型提到自己买的名字是正常的）
//
// 顺序不能颠倒：先摘地址，脱敏字样会把 URL 里的 wan3.0 换成「上游」，
// 地址反正要删，不必再费劲保护 URL 区段。
func RedactUpstreamText(content string, modelName string) string {
	if content == "" {
		return content
	}
	text := RedactUpstreamAddress(content)
	text = RedactUpstreamVendor(text)
	// 非别名模型到这里就结束：供应商词已换成「上游」，地址已摘，
	// 剩下的模型名是他自己买的，留着才看得懂。
	if !IsUpstreamAliasModel(modelName) {
		return text
	}
	return redactWithTokens(text, upstreamModelTokens, upstreamModelPatterns)
}

func redactWithTokens(text string, tokens []identityToken, patterns []*regexp.Regexp) string {
	for _, token := range tokens {
		text = token.re.ReplaceAllString(text, upstreamNeutralWord)
	}
	for _, re := range patterns {
		text = re.ReplaceAllString(text, upstreamNeutralWord)
	}
	// 连续替换可能产生 "上游 上游"、"上游:上游" 这类碎片，收敛成一个
	text = strings.ReplaceAll(text, upstreamNeutralWord+" "+upstreamNeutralWord, upstreamNeutralWord)
	text = strings.ReplaceAll(text, upstreamNeutralWord+":"+upstreamNeutralWord, upstreamNeutralWord)
	return text
}
