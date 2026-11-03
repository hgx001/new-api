package middleware

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/promptcompress_setting"
)

// 提示词压缩的内部调用（走本站 /v1/chat/completions）。
//
// 为什么不直接调上游渠道：压缩要经过正常的中转链（选渠道、计费、失败重试），
// 自己拼上游请求等于把渠道适配逻辑复制一份，渠道一换就漂。
// 走本站接口还有一个好处：消耗记在独立令牌下，能单独查账，不进客户账单。

// compressMessage 是一条 chat 消息。
type compressMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// compressChatRequest 是内部 chat completions 请求体。
type compressChatRequest struct {
	Model       string            `json:"model"`
	Messages    []compressMessage `json:"messages"`
	Temperature float64           `json:"temperature"`
	MaxTokens   int               `json:"max_tokens"`
	Stream      bool              `json:"stream"`
}

// compressChatResponse 只解析用得到的字段。
type compressChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		// FinishReason 必须是 length 之外的取值。
		//
		// 为什么非看它不可：输出一旦被 max_tokens 截断，末尾会在半句话处结束 ——
		// 此时长度可能仍 ≤ 5000、@图片 标记还在（它们通常在前半段），
		// 四条不变量全部通过，半截提示词就被当作合格结果提交给上游。
		// 客户拿到的成片缺了后半段描述，而任务成功、账单照收，没有任何下游信号能发现。
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

// compressCallError 是内部调用失败。
//
// 为什么要结构体而不是普通 error：调用方要区分「值得重试」和「重试也没用」——
// 4xx（令牌错、模型不在白名单）重试只是白等一轮，而网络抖动/5xx 重试一次
// 通常就好了。两次都失败时，日志里要能看出是哪一类。
type compressCallError struct {
	Status    int
	Message   string
	Retryable bool
}

func (e *compressCallError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("prompt compress call failed: status=%d %s", e.Status, e.Message)
	}
	return "prompt compress call failed: " + e.Message
}

// compressModelCall 是「调一次压缩模型」的可替换入口。
//
// 声明成变量**只为单测可注入**：压缩链路的正确性（触发条件、四条不变量、
// 重试与 400）不应该建立在「真的调一次 LLM」上 —— 那样测试既慢又不确定。
// 生产代码路径永远走默认实现 requestPromptCompression。
var compressModelCall = requestPromptCompression

// requestPromptCompression 调一次内部的 chat completions，返回模型输出原文。
//
// 不在这里做重试与清洗：重试需要「上一版结果」做反馈，清洗属于校验环节，
// 放在调用方（middleware/wan3_prompt_compress.go）才能一眼看出顺序。
func requestPromptCompression(ctx context.Context, cfg promptcompress_setting.Config, messages []compressMessage) (string, error) {
	payload := compressChatRequest{
		Model:       cfg.Model,
		Messages:    messages,
		Temperature: compressTemperature,
		MaxTokens:   compressMaxTokens,
		Stream:      false,
	}
	raw, err := common.Marshal(payload)
	if err != nil {
		return "", &compressCallError{Message: "marshal request: " + err.Error()}
	}

	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cfg.Endpoint, strings.NewReader(string(raw)))
	if err != nil {
		return "", &compressCallError{Message: "build request: " + err.Error()}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(cfg.Token))
	// 双保险：即使将来把压缩端点误配到视频路由上，也不会绕回自己。
	req.Header.Set("X-Wan3-Compress-Internal", "1")

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", &compressCallError{Message: err.Error(), Retryable: true}
	}
	defer func() { _ = resp.Body.Close() }()

	// 响应体只读有限长度：异常时上游可能回一整页 HTML，全读进内存没有意义。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", &compressCallError{
			Status:    resp.StatusCode,
			Message:   truncateForLog(string(body), 200),
			Retryable: resp.StatusCode >= 500,
		}
	}

	var parsed compressChatResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return "", &compressCallError{Message: "unmarshal response: " + err.Error(), Retryable: true}
	}
	if len(parsed.Choices) == 0 {
		return "", &compressCallError{Message: "empty choices", Retryable: true}
	}
	content := parsed.Choices[0].Message.Content
	if strings.TrimSpace(content) == "" {
		return "", &compressCallError{Message: "empty content", Retryable: true}
	}
	// 被 max_tokens 截断的输出是**半截提示词**，绝不能当结果用（见 FinishReason 注释）。
	// 判为可重试：重试时提示里会写「上一版被截断」，模型会更克制地删。
	if reason := parsed.Choices[0].FinishReason; reason == "length" || reason == "max_tokens" {
		return "", &compressCallError{
			Message:   "output truncated (finish_reason=" + reason + ")",
			Retryable: true,
		}
	}
	return content, nil
}

// 采样参数：压缩要稳定，不要发挥。
const (
	compressTemperature = 0.2
	// 4600 个中文字大约 1.4 倍 token，8000 留足余量。
	compressMaxTokens = 8000
)

// compressSystemPrompt 是压缩器的 system 消息。
//
// 设计要点（每一条都是踩过的坑）：
//   - 用「删减优先级」而不是「请概括」：概括会重写句式、丢镜头顺序、把 @图片2 挤没；
//     删减是保守操作，改得少、可控；
//   - 引用标记 / 数值 / 对白 / 负向尾坠单列成硬规则：这四类最容易被当成冗余删掉，
//     而它们恰恰决定成片对不对（改号等于换图、删数值等于换镜头）；
//   - 指令里不写 5000：写真实阈值模型会贴着边写，一超就要重试；
//   - 禁止 markdown / 列表 / 解释：输出会原样当作 prompt 送上游，符号会污染画面描述。
func compressSystemPrompt(target int) string {
	return fmt.Sprintf(`你是视频生成提示词的压缩器。任务只有一个：在不改变创作意图的前提下，把给定提示词压到长度上限以内。你不是编剧，也不是润色助手——不许新增任何画面信息。

【硬性规则】违反任意一条即视为失败
1. 输出必须 ≤ %d 个字符（按 Unicode 字符计，标点与换行都算）。这是硬要求，不是建议。
2. 引用标记原样保留：形如 @图片1、@图2、@image3、@视频1、@音频1。编号、数量、先后顺序都不能变，也不能新增。它们绑定客户上传的素材，改号等于换图。
3. 结尾的负向约束句（形如「禁止出现：……」）逐字保留，不得改写、合并、移位或删除。
4. 对白保留原文与说话人，不得改写台词，不得把两个人的话并成一段。
5. 六要素一项都不能丢：主体、动作、环境、运镜、光影与氛围、整体风格。
6. 数值不得模糊化：时长秒数、镜头焦段、光圈、色温、帧率、画幅比例、数量，全部原样保留。
7. 保持原文结构与顺序：若原文用【】小标题分段，输出用同样的小标题、同样的先后。
8. 不翻译、不改语言、不解释。不写「以下是压缩后的提示词」，不加标题、序号、引号、代码块、项目符号。

【删减优先级】从高到低，删够即停
1. 同义反复与重复修饰，例如「非常非常震撼的画面」；
2. 无信息量的铺垫与元话语，例如「我想让你生成一段」「整体来说」「请注意」；
3. 画面上不可见的内容：抽象形容词堆叠、心理活动、主观抒情；
4. 可以合并成一个短句的并列从句。

【绝对不删】引用标记、数值、对白、负向约束句、六要素中的任何一项。

只输出压缩后的提示词正文。`, target)
}

// compressUserPrompt 是首次压缩的 user 消息。
func compressUserPrompt(original string, target int) string {
	return fmt.Sprintf("目标长度：≤ %d 字符\n原文长度：%d 字符\n\n<prompt>\n%s\n</prompt>",
		target, countRunes(original), original)
}

// compressRetryPrompt 是「上一版没过」时的 user 消息。
//
// 分两条路，因为两种失败需要的材料不同：
//
//   - **只是超长**：在上一版基础上继续删。收敛快，也不会把已保住的内容重写丢；
//   - **其它原因**（丢了引用标记 / 负向尾坠丢了 / 压得太狠 / 输出被截断）：
//     只给上一版，模型**补不回来** —— 它看不到原文，不知道少了什么。
//     所以这条必须把原文一起给它，并点名上次错在哪。
//
// 早期实现只有第一种（无条件说「超出上限 N 字符」）。结果丢标记时喂给模型的
// 反馈是「超出上限 0 字符」，指令却是「继续删减」—— 越删越远，必然 400。
func compressRetryPrompt(original, previous string, cause error, limit, target int) string {
	if previous == "" || !isLengthOnlyFailure(cause) {
		return fmt.Sprintf(`上一版未通过校验：%s。

这次请**从原文重新压缩**（上一版的长度 %d 字符并不算超，问题在内容被改坏）。逐条对照原文，确保：
1. 每个 @图片N / @图N / @imageN / @视频N / @音频N 标记按原编号、原数量保留，不增不减不改号；
2. 结尾的负向约束句（形如「禁止出现：……」）逐字保留；
3. 六要素（主体、动作、环境、运镜、光影氛围、整体风格）一项都不丢；
4. 数值（秒数、焦段、光圈、色温、帧率、画幅比例、数量）原样保留；
5. 最终 ≤ %d 个字符。

<original>
%s
</original>

只输出压缩后的提示词正文。`,
			retryCauseText(cause), countRunes(previous), target, original)
	}

	return fmt.Sprintf(`上一版 %d 字符，超出上限 %d 字符。

在上一版基础上继续删减，只删「同义反复、无信息量的铺垫、抽象堆叠、可合并的并列从句」，不得删引用标记、数值、对白、负向约束句与六要素。目标 ≤ %d 字符。

<previous>
%s
</previous>

只输出新版本。`,
		countRunes(previous), max(0, countRunes(previous)-limit), target, previous)
}

// isLengthOnlyFailure 判断这次失败是不是「仅仅超长」。
func isLengthOnlyFailure(cause error) bool {
	var validation *compressValidationError
	return errors.As(cause, &validation) && validation.Kind == compressFailLength
}

// retryCauseText 把失败原因翻成给模型看的一句话。
//
// 调用层的错误（截断、网络）不能原样丢给模型 ——
// 「output truncated (finish_reason=length)」只会干扰它的判断。
func retryCauseText(cause error) string {
	var validation *compressValidationError
	if errors.As(cause, &validation) {
		return validation.Error()
	}
	var callErr *compressCallError
	if errors.As(cause, &callErr) {
		return "上一次输出被截断或未完整返回，后半段内容缺失"
	}
	return cause.Error()
}

// truncateForLog 截断日志内容，避免把整页 HTML / 超长报错打进日志。
//
// 按**字符**截断而不是字节：中文日志按字节切会在字符中间断开，
// 产生非法 UTF-8，日志检索里显示成乱码。
func truncateForLog(text string, limit int) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "..."
}
