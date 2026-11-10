package common

import (
	"fmt"
	"strings"
)

// 全站统一的画幅比例解析。
//
// 2026-11-10 审查背景：首轮「顶层比例被丢弃」的修复只覆盖了 youzanwan3 与
// megabyai 两个适配器，但根因是**全站性**的 —— 生产在售的另外 4 个适配器
// （wan3 / xinbiapi / yuanmeng / hailuo）的 resolveRatio 同样只读 metadata，
// 客户端把比例放在请求体顶层时一样被静默丢弃。
//
// 把解析逻辑收到一个包里做唯一入口，各适配器调它，避免：
//  1. 同样的漏网之鱼再发生（改了 A 包忘了 B 包）；
//  2. 各包各写一遍导致口径漂移（megabyai 与 youzanwan3 之前就分别是
//     「顶层优先」与「metadata 优先」两种口径并存）。

// 2026-11-10 审查发现的第二个缺口：
//
// 首轮修复只补了 youzanwan3（type=63）与 megabyai（复用给 youkou）两个适配器，
// 但生产在售的另外 4 个适配器 —— wan3(60) / xinbiapi(68) / yuanmeng(69) /
// hailuo(35) —— 的 resolveRatio **仍然只读 metadata**，客户端把比例放在
// 请求体顶层时同样被静默丢弃。
//
// 这是首轮修复的漏网之鱼：根因（顶层字段被解码器丢弃 + 适配器只读 metadata）
// 是**全站性**的，只在一个包修等于留了四个同样的坑。
//
// 这里先立一个**全站统一**的解析入口，作为唯一事实来源，
// 各个适配器都调它，避免各写一遍导致口径再次漂移（megabyai 与
// youzanwan3 之前就是「顶层优先」与「metadata 优先」两种口径并存）。

// TaskRatioSource 描述比例是从哪里取到的，用于日志与快照。
type TaskRatioSource string

const (
	TaskRatioSourceNone            TaskRatioSource = ""
	TaskRatioSourceMetadataRatio   TaskRatioSource = "metadata.ratio"
	TaskRatioSourceMetadataAspect  TaskRatioSource = "metadata.aspect_ratio"
	TaskRatioSourceTopLevelRatio   TaskRatioSource = "ratio"
	TaskRatioSourceTopLevelAspect  TaskRatioSource = "aspect_ratio"
	TaskRatioSourceSize            TaskRatioSource = "size"
	TaskRatioSourceDefault         TaskRatioSource = "default"
	TaskRatioSourceInvalidRejected TaskRatioSource = "invalid"
)

// TaskRatioCandidates 是各适配器声明的「我支持哪些比例档位」。
// 空 map 视为不限制（调用方自行校验）。
type TaskRatioCandidates map[string]bool

// TaskRatioResult 是统一比例解析的结果。
type TaskRatioResult struct {
	// Ratio 是最终下发给上游的比例。空串表示「不指定，由上游决定」。
	Ratio string
	// Source 说明 Ratio 是从哪个来源取到的 —— 排查「比例不对」时，
	// 第一眼就能分清是「客户端没传」还是「传了但被降级」。
	Source TaskRatioSource
	// Rejected 非空表示客户端传了但**不被支持**的比例值。
	// 各适配器可以据此报错（心笔/视频生成车站口径）或静默降级（有赞口径）。
	Rejected string
}

// TaskRatioOptions 是统一比例解析的可选项。
type TaskRatioOptions struct {
	// Candidates 声明本渠道支持的档位。nil 表示不限制。
	Candidates TaskRatioCandidates
	// DefaultRatio 是所有来源都没命中时的取值（通常是 adaptive 或 16:9）。
	DefaultRatio string
	// SizeToRatio 把 size 尺寸/比例写法转成档位。返回 false 表示推不出。
	SizeToRatio func(string) (string, bool)
	// Normalize 在比对档位前归一化客户端写法（如 "16/9"、"16：9" → "16:9"）。
	// nil 表示不归一化。
	Normalize func(string) string
	// RejectInvalid 决定遇到不支持的比例时是否**立即报错**。
	//
	//	true  —— 心笔 / 视频生成车站：档位是硬约束（上游会直接拒单），
	//	         提前报错能给客户可读原因，而不是建单后失败再退款。
	//	false —— 有赞 / DashScope 等：比例是画幅参数不是计费硬参数，
	//	         继续往下一个来源找，全都没命中就落默认值，不打断请求。
	RejectInvalid bool
	// SizeFirst 让 size 的**原始比例写法**优先于顶层 ratio / aspect_ratio。
	//
	// 默认 false（全站口径：顶层字段优先于 size）。设为 true 的唯一场景是
	// manwu（type=66）：它的 size 字段在官网语义里**就是比例**
	// （Worker 侧 picsart/doubao params.ts 把 size 直接映射为比例档位），
	// 且「size 优先」是该渠道已上线并有测试保护的既定契约 ——
	// 客户端同时传 size="1:1" 与 ratio="9:16" 时，官网以 size 为准。
	//
	// 为什么用显式选项而不是各包各写一遍：口径差异必须是**声明式的**，
	// 否则又回到"改 A 忘 B / 口径悄悄漂移"的老路。
	SizeFirst bool
	// AllowEmptyRatio 表示「客户端没指定比例」是合法状态，此时 Ratio 返回空串
	// 而不是默认值。
	//
	// 用于官网有「自动 / 不指定」档位的场景（manwu 的豆包视频、豆包图片、
	// Picsart）：补一个 16:9 等于把「用户没选」静默改写成一次参数变更。
	AllowEmptyRatio bool
}

// ResolveTaskRatio 是全站统一的画幅比例解析入口。
//
// 优先级（唯一口径，所有适配器都必须按这个顺序）：
//
//	metadata.ratio → metadata.aspect_ratio → 顶层 ratio → 顶层 aspect_ratio
//	→ size 尺寸推断 → 默认值
//
// 为什么 metadata 优先于顶层：metadata 是站内**已验证**的传法（生产上
// 已有大量 9:16 走这条路），顶层是后来为了兼容 OpenAI 风格补的。
// 让已验证的路径保持最高优先级，行为变更最小。
//
// 关于 size 推断：只在显式比例都没给时才用，且**推不出就不猜**
// （返回 default 由调用方决定是自适应还是报错），绝不编造比例。
//
// 关于非法比例：记录到 Rejected。RejectInvalid=true 时立即返回（由调用方
// 报错）；false 时继续往下一个来源找，全都没命中就落默认值 —— 不打断请求。
func ResolveTaskRatio(req TaskSubmitReq, opts TaskRatioOptions) TaskRatioResult {
	normalize := opts.Normalize
	if normalize == nil {
		normalize = func(value string) string { return value }
	}

	allowed := func(value string) bool {
		value = trimSpace(value)
		if value == "" {
			return false
		}
		if opts.Candidates == nil {
			return true
		}
		return opts.Candidates[value]
	}

	// 收集所有候选来源，按优先级排列。
	//
	// ⚠️ size 有**两档**参与：size 的原始值（"16:9"、"4/3" 这类直接写比例的
	// 写法）与 size 的转换结果（"1280x720" → "16:9"）。原始值优先 ——
	// 客户端明写了比例时，不该被尺寸推断覆盖。
	type candidate struct {
		raw    string
		source TaskRatioSource
	}
	candidates := make([]candidate, 0, 6)

	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			raw, ok := req.Metadata[key]
			if !ok || raw == nil {
				continue
			}
			// metadata 里的比例不一定是字符串（部分客户端会传数字）。
			// 统一转成字符串再走同一套归一化与档位比对。
			text, isString := raw.(string)
			if !isString {
				text = fmt.Sprintf("%v", raw)
			}
			candidates = append(candidates, candidate{raw: text, source: metadataSource(key)})
		}
	}
	for _, key := range []string{"ratio", "aspect_ratio"} {
		candidates = append(candidates, candidate{
			raw:    topLevelRatio(req, key),
			source: topLevelSource(key),
		})
	}
	// size 原始值：客户端把比例直接塞进 size 的写法。
	//
	// ⚠️ 只有「看起来像比例」的 size 才参与（"16:9"、"4/3"、"16：9"）——
	// 像素尺寸（"1280x720"）**不是**比例写法，不能当比例原样下发，
	// 否则上游会收到 "1920x1080" 这种荒谬的 ratio 值。
	//
	// ⚠️ SizeFirst 时 size 要排在顶层字段**之前**（manwu 的既定契约：
	// 官网以 size 为准），否则就要把候选重新排一遍。
	sizeCandidate := candidate{}
	if rawSize := trimSpace(req.Size); rawSize != "" && looksLikeRatio(rawSize) {
		sizeCandidate = candidate{raw: rawSize, source: TaskRatioSourceSize}
		if opts.SizeFirst {
			candidates = append([]candidate{sizeCandidate}, candidates...)
			sizeCandidate = candidate{}
		}
	}
	if sizeCandidate.raw != "" {
		candidates = append(candidates, sizeCandidate)
	}

	rejected := ""
	for _, c := range candidates {
		value := trimSpace(c.raw)
		if value == "" {
			continue
		}
		normalized := trimSpace(normalize(value))
		if normalized == "" {
			// 归一化后为空（如 "abc"）—— 视为非法。
			if rejected == "" {
				rejected = value
			}
			if opts.RejectInvalid {
				return TaskRatioResult{
					Ratio:    fallbackRatio(opts, req.Size),
					Source:   TaskRatioSourceInvalidRejected,
					Rejected: rejected,
				}
			}
			continue
		}
		if allowed(normalized) {
			return TaskRatioResult{Ratio: normalized, Source: c.source}
		}
		// size 不是「比例字段」—— 它写成 "1280x720" 是像素尺寸，
		// 交给下面的 SizeToRatio 去推断，不算「客户端传了非法比例」。
		if c.source == TaskRatioSourceSize {
			if opts.RejectInvalid {
				// 但 size 写了「像比例的东西」（"5:2"）却被判非法时，
				// 渠道口径要求报错（视频生成车站）—— 不能静默降级。
				return TaskRatioResult{
					Ratio:    fallbackRatio(opts, req.Size),
					Source:   TaskRatioSourceInvalidRejected,
					Rejected: value,
				}
			}
			continue
		}
		if rejected == "" {
			rejected = value
		}
		if opts.RejectInvalid {
			return TaskRatioResult{
				Ratio:    fallbackRatio(opts, req.Size),
				Source:   TaskRatioSourceInvalidRejected,
				Rejected: rejected,
			}
		}
	}

	// size 尺寸推断（显式比例都没给/都不认时）。
	//
	// ⚠️ 这里处理的是 "1280x720" 这类**像素尺寸** —— 它不是比例写法，
	// 上面的候选循环会把它判为非法，所以要在这里单独兜底转换。
	if opts.SizeToRatio != nil {
		if value, ok := opts.SizeToRatio(trimSpace(req.Size)); ok {
			normalized := trimSpace(normalize(value))
			if normalized != "" && allowed(normalized) {
				return TaskRatioResult{Ratio: normalized, Source: TaskRatioSourceSize}
			}
		}
	}

	// 默认值。
	if rejected != "" {
		// 客户端传了但不被支持 —— 记下来（调用方可以据此报错或记日志），
		// 但比例本身落默认值。
		return TaskRatioResult{
			Ratio:    opts.DefaultRatio,
			Source:   TaskRatioSourceInvalidRejected,
			Rejected: rejected,
		}
	}
	if opts.AllowEmptyRatio {
		// 「没指定」是合法状态 —— 不补默认值，让上游用它自己的「自动」档。
		return TaskRatioResult{Ratio: "", Source: TaskRatioSourceDefault}
	}
	return TaskRatioResult{Ratio: opts.DefaultRatio, Source: TaskRatioSourceDefault}
}

func fallbackRatio(opts TaskRatioOptions, size string) string {
	if opts.SizeToRatio != nil {
		if value, ok := opts.SizeToRatio(trimSpace(size)); ok {
			normalized := trimSpace(value)
			if opts.Normalize != nil {
				normalized = trimSpace(opts.Normalize(normalized))
			}
			if normalized != "" && (opts.Candidates == nil || opts.Candidates[normalized]) {
				return normalized
			}
		}
	}
	return opts.DefaultRatio
}
func metadataSource(key string) TaskRatioSource {
	if key == "aspect_ratio" {
		return TaskRatioSourceMetadataAspect
	}
	return TaskRatioSourceMetadataRatio
}

func topLevelSource(key string) TaskRatioSource {
	if key == "aspect_ratio" {
		return TaskRatioSourceTopLevelAspect
	}
	return TaskRatioSourceTopLevelRatio
}

func topLevelRatio(req TaskSubmitReq, key string) string {
	if key == "aspect_ratio" {
		return req.AspectRatio
	}
	return req.Ratio
}

// looksLikeRatio 判断一个字符串「看起来像比例写法」。
//
// 判据：含 ':' 或 '/'，且不是像素尺寸。
//
//	"16:9"    → 是
//	"4/3"     → 是
//	"16：9"   → 是（全角冒号）
//	"1280x720" → 否（像素尺寸）
//	"720P"    → 否（分辨率写法）
func looksLikeRatio(value string) bool {
	value = trimSpace(value)
	if value == "" {
		return false
	}
	if strings.ContainsAny(value, ":/") {
		return true
	}
	return false
}

func trimSpace(value string) string {
	// 避免为一个字符串引入 strconv 之外的处理；这里保持简单。
	start := 0
	end := len(value)
	for start < end && isSpace(value[start]) {
		start++
	}
	for end > start && isSpace(value[end-1]) {
		end--
	}
	return value[start:end]
}

func isSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}
