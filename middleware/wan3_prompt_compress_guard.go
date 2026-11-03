package middleware

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// 压缩结果的四条硬不变量。
//
// 为什么必须校验而不是「相信模型」：压缩改写的是客户的创作描述，一旦丢东西，
// 客户拿到的成片与提示词不符，而且**任务照样成功、账单照收** —— 这类错误
// 没有任何下游信号能发现，只能在改写的入口挡住。
//
//	1. rune 长度 ≤ 5000     —— 超了上游照拒，等于白跑一次（还白花压缩的钱）
//	2. 引用标记多重集一致   —— @图片2 变成 @图片3 就是换了另一张素材
//	3. 负向尾坠逐字保留     —— 压掉「禁止出现：字幕」就会冒出字幕
//	4. 最小保真 ≥ 40%       —— 防模型过度摘要把六要素压没
//
// 第 2/3 条是「原文有才校验」：非 ArcReel 客户不会带这些标记，
// 硬要求它们存在会把正常客户挡在门外。

// minCompressFidelity 是压缩后长度相对原文的最小比例。
const minCompressFidelity = 0.40

// compressValidationError 是「压缩结果没通过校验」的结构化原因。
//
// 为什么要按类型而不是字符串：重试时要按**原因**决定给模型什么材料 ——
// 只是超长就让它接着删，丢了引用标记则必须把原文还给它（它看不到原文就补不回来）。
// 用普通 error 只能做子串匹配，改一个字就静默退化成错误的指令。
type compressValidationError struct {
	// Kind 见下面的 compressFail* 常量。
	Kind string
	// Detail 是具体描述，同时进日志与重试提示。
	Detail string
}

func (e *compressValidationError) Error() string { return e.Detail }

const (
	compressFailEmpty        = "empty"
	compressFailLength       = "length"
	compressFailFidelity     = "fidelity"
	compressFailMarkers      = "markers"
	compressFailNegativeTail = "negative_tail"
	compressFailMetaPrefix   = "meta_prefix"
)

// citationMarkerPattern 匹配素材引用标记：@图片1 / @图2 / @image3 / @视频1 / @音频1。
//
// 交替顺序把「图片」放在「图」前面，否则 @图片1 会被解析成前缀「图」+ 编号「片1」
// 而匹配失败 —— 那会让最常用的一种标记逃过校验。
var citationMarkerPattern = regexp.MustCompile(`(?i)@(图片|图|image|视频|音频)(\d+)`)

// countRunes 按 Unicode 字符计数。
//
// ⚠️ 不能用 len()：5000 个汉字按字节是 15000，用字节数判断会把「刚好在
// 上限内」的中文提示词误判成超长，直接改写客户的正常请求。
func countRunes(text string) int {
	return utf8.RuneCountInString(strings.TrimSpace(text))
}

// cleanCompressedOutput 清洗模型输出，再进校验。
//
// 模型即使被要求「只输出正文」，仍可能包一层代码围栏或首尾引号。
// 这些符号会被原样当作 prompt 送去生成，必须在入口摘掉。
func cleanCompressedOutput(text string) string {
	out := strings.TrimSpace(text)

	// 代码围栏：```text ... ``` 或 ``` ... ```
	if strings.HasPrefix(out, "```") {
		if newline := strings.Index(out, "\n"); newline >= 0 {
			out = out[newline+1:]
		} else {
			out = strings.TrimPrefix(out, "```")
		}
		out = strings.TrimSuffix(strings.TrimSpace(out), "```")
		out = strings.TrimSpace(out)
	}

	// 首尾成对引号。
	pairs := [][2]string{
		{`"`, `"`},
		{"'", "'"},
		{"“", "”"},
		{"「", "」"},
		{"『", "』"},
	}
	for _, pair := range pairs {
		if strings.HasPrefix(out, pair[0]) && strings.HasSuffix(out, pair[1]) && countRunes(out) > 2 {
			out = strings.TrimSuffix(strings.TrimPrefix(out, pair[0]), pair[1])
			out = strings.TrimSpace(out)
			break
		}
	}
	return out
}

// validateCompressedPrompt 校验四条硬不变量，返回第一条不满足的原因。
//
// 返回 nil 表示可以放行（此时长度已确认 ≤ 5000）。
func validateCompressedPrompt(original, compressed string) error {
	length := countRunes(compressed)
	if length == 0 {
		return &compressValidationError{Kind: compressFailEmpty, Detail: "压缩结果为空"}
	}
	if length > promptRouteLimitRunes {
		return &compressValidationError{
			Kind:   compressFailLength,
			Detail: fmt.Sprintf("压缩后仍为 %d 字符，超过上限 %d", length, promptRouteLimitRunes),
		}
	}

	originalLength := countRunes(original)
	// 保真度：只说「别压太狠」，不做下限绝对值判断（短原文压缩后本来就短）。
	if originalLength > 0 && float64(length) < float64(originalLength)*minCompressFidelity {
		return &compressValidationError{
			Kind:   compressFailFidelity,
			Detail: fmt.Sprintf("压缩后仅剩 %d 字符（原文 %d），低于保真下限", length, originalLength),
		}
	}

	if prefix := metaPrefixPresent(compressed); prefix != "" {
		return &compressValidationError{
			Kind:   compressFailMetaPrefix,
			Detail: fmt.Sprintf("压缩结果带了元话语前缀「%s」", prefix),
		}
	}

	if err := validateCitationMarkers(original, compressed); err != nil {
		return err
	}
	if err := validateNegativeTail(original, compressed); err != nil {
		return err
	}
	return nil
}

// metaPrefixPresent 检查开头是否出现「以下是 / 压缩后 / 改写」这类解释性前缀。
//
// 只看开头 30 个字符：正文中间出现「改写」是正常的创作描述（例如「改写自
// 某段古诗的画面」），不该被判失败。
func metaPrefixPresent(compressed string) string {
	head := []rune(compressed)
	if len(head) > 30 {
		head = head[:30]
	}
	prefixes := []string{"以下是", "压缩后", "改写后", "这是压缩"}
	headText := string(head)
	for _, prefix := range prefixes {
		if strings.Contains(headText, prefix) {
			return prefix
		}
	}
	return ""
}

// validateCitationMarkers 校验引用标记的多重集完全一致。
//
// 用多重集（而不是集合）比对：@图片1 出现两次压成一次，也是丢素材 ——
// 客户上传了两张图、两次都引用同一张，压缩后只提一次，生成结果的构图就变了。
func validateCitationMarkers(original, compressed string) error {
	want := citationMarkerMultiset(original)
	if len(want) == 0 {
		return nil
	}
	got := citationMarkerMultiset(compressed)
	for marker, wantCount := range want {
		if gotCount := got[marker]; gotCount != wantCount {
			return &compressValidationError{
				Kind:   compressFailMarkers,
				Detail: fmt.Sprintf("引用标记 %s 数量由 %d 变成 %d", marker, wantCount, gotCount),
			}
		}
	}
	for marker, gotCount := range got {
		if wantCount, ok := want[marker]; !ok || wantCount != gotCount {
			return &compressValidationError{
				Kind:   compressFailMarkers,
				Detail: fmt.Sprintf("压缩结果多出引用标记 %s（%d 个）", marker, gotCount),
			}
		}
	}
	return nil
}

// citationMarkerMultiset 统计标记出现次数，键统一成小写。
//
// 统一小写是为了让 @Image2 与 @image2 视为同一个标记 —— 它们指向同一张图，
// 大小写差异不构成语义变化，没必要因此判失败。
func citationMarkerMultiset(text string) map[string]int {
	out := make(map[string]int)
	for _, match := range citationMarkerPattern.FindAllStringSubmatch(text, -1) {
		key := strings.ToLower(match[1]) + match[2]
		out[key]++
	}
	return out
}

// validateNegativeTail 校验原文结尾的负向约束句被逐字保留。
//
// 取「最后一次出现的『禁止出现』到结尾」作为需要保留的片段：这类句子是
// 客户用来排除字幕/水印/BGM 的，压掉或改写会让成片多出这些东西，
// 而且客户很难判断是哪一步弄丢的。
func validateNegativeTail(original, compressed string) error {
	tail := negativeTail(original)
	if tail == "" {
		return nil
	}
	if !strings.Contains(compressed, tail) {
		return &compressValidationError{
			Kind:   compressFailNegativeTail,
			Detail: fmt.Sprintf("负向约束句未逐字保留：%s", truncateForLog(tail, 60)),
		}
	}
	return nil
}

// negativeTail 取出原文结尾的负向约束句；没有则返回空串。
func negativeTail(original string) string {
	trimmed := strings.TrimSpace(original)
	index := strings.LastIndex(trimmed, "禁止出现")
	if index < 0 {
		return ""
	}
	tail := strings.TrimSpace(trimmed[index:])
	// 太短的片段（比如只有「禁止出现」四个字）不构成有意义的约束句，
	// 拿它做「必须逐字保留」的要求只会误伤。
	if countRunes(tail) < 6 {
		return ""
	}
	return tail
}
