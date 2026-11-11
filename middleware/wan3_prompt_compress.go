package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/promptcompress_setting"
	"github.com/gin-gonic/gin"
)

// 提示词超长压缩：wan3 上游限 5000 字符，超了就拒单。
//
// 为什么排在条件路由**之后**：顺序本身就是「豆包优先」的实现。
// 规则一（时长超过阈值）会把请求改派到豆包 —— 豆包吃 20000 字符，不需要压缩。
// 只有豆包**没接走**（没超阈值 / 目标渠道不可用 / 带参考视频）的请求才会走到这里。
// 2026-11-11 删除原规则四（提示词超长改派豆包）后，**所有**留在非豆包链路上的
// 超长提示词都归这里处理：能压就压、压不下来明确 400，不再悄悄换链路。
//
// 为什么白名单只有促销族两个模型：wan3 一族的提示词上限都是 5000，但
// `wan3.0-video` 等模型在广场上就是按 wan3 卖的，超长应该让客户精简；
// `seedance2.5-特惠` / `-活动` 是我们自己包装的便宜档，需要兜住长提示词。
//
// 失败原则：**压不下来就明确报错**（400 prompt_too_long），绝不截断提交、
// 绝不静默改派。截断提交等于交付了半截提示词的片，且账单照收 ——
// 客户根本发现不了问题出在哪。

// compressInternalHeader 是内部压缩调用的标记头。
//
// 压缩走本站 /v1/chat/completions，与视频路由不同组，天然不会递归；
// 这个头是双保险 —— 将来若有人把压缩端点误配到视频路由上，
// 中间件看到这个头会直接放行，不会自己调自己。
const compressInternalHeader = "X-Wan3-Compress-Internal"

// maxCompressibleRunes 是「压缩还有意义」的长度上界。
//
// = promptRouteLimitRunes / minCompressFidelity（5000 / 0.4 = 12500）。
// 超过它的原文，压到标称上限后仍然违反保真下限 —— 无论模型多努力都过不了校验，
// 唯一的结果是白烧两次调用再回 400。
//
// ⚠️ 两个常量必须同步改：写死成 12500 而不是用浮点常量表达式，是因为
// const 不支持 float64→int 的转换；有单测守着这个等式（见
// TestMaxCompressibleRunesMatchesFidelityFloor）。
const maxCompressibleRunes = promptRouteLimitRunes * 5 / 2

// Wan3PromptCompress 在条件路由之后压缩超长提示词。
func Wan3PromptCompress() gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := promptcompress_setting.Get()
		if !cfg.Enabled {
			c.Next()
			return
		}
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		// /remix 基于已有任务再生成，模型与参数由原任务决定，
		// 改写 prompt 会破坏 remix 的原任务语义。
		if strings.HasSuffix(c.Request.URL.Path, "/remix") {
			c.Next()
			return
		}
		if c.GetHeader(compressInternalHeader) != "" {
			c.Next()
			return
		}

		body, ok := readJSONRequestBody(c)
		if !ok {
			c.Next()
			return
		}

		// ① 客户**请求**的模型（改派前的名字）。
		//
		// 必须用 RouteOriginModel：规则三（首尾帧）会把特惠改派到
		// wan3.0-720p-特惠，那条链路同样 5000 上限，客户买的是特惠，也该压。
		// 只看 body 里的 model 会漏掉这一类。
		requested := strings.TrimSpace(RouteOriginModel(c))
		effective, _ := body["model"].(string)
		if requested == "" {
			requested = strings.TrimSpace(effective)
		}
		if !constant.IsSeedance25Family(requested) {
			c.Next()
			return
		}

		// ② 豆包已经接走了 → 不压。
		//
		// 规则一成功改派时 body.model 已经是 seedance-2-5；没改派则仍是
		// 特惠/活动（或被规则二/三改到别的目标模型）。所以「body 里还是不是豆包」
		// 就是「豆包有没有接走」的现成信号，不需要再查渠道。
		if isDoubaoTarget(effective) {
			c.Next()
			return
		}

		// ③ 不超就不管（> 不是 >=：恰好 5000 上游是收的）。
		prompt, isString := body["prompt"].(string)
		if !isString {
			c.Next()
			return
		}
		length := countRunes(prompt)
		if length <= promptRouteLimitRunes {
			c.Next()
			return
		}

		// ④ 长到「压也压不下来」就不压了。
		//
		// 保真下限（minCompressFidelity）把可达长度锁死在原文的 40% 以上：
		// 原文超过 12500 字符时，即使压到 5000 也必然卡在保真校验 ——
		// 结果一定是 400，但已经白调了两次模型（还白花两次 LLM 的钱）。
		// 直接拒，客户立刻知道要精简，运营也不会看到「调了模型但仍失败」的日志噪音。
		if length > maxCompressibleRunes {
			logger.LogInfo(c, fmt.Sprintf(
				"提示词 %d 字符，超过可压缩上限 %d（保真下限决定），直接拒绝",
				length, maxCompressibleRunes))
			rejectPromptTooLong(c)
			return
		}

		// 配置不完整（没配内部令牌）或压缩器正在熔断：
		// 不再尝试，直接按「压不下来」处理 —— 让客户拿到明确的「提示词过长」，
		// 而不是等上游回一个他看不懂的拒绝。
		if !cfg.Usable() {
			logger.LogInfo(c, "提示词压缩未配置内部令牌，长提示词直接拒绝")
			rejectPromptTooLong(c)
			return
		}
		if compressorBreakerOpen() {
			logger.LogInfo(c, "提示词压缩器熔断中（连续失败），本次直接拒绝")
			rejectPromptTooLong(c)
			return
		}

		origin := RouteOriginModel(c)
		startedAt := time.Now()
		compressed, attempts, err := compressPromptWithRetry(c, cfg, prompt)
		if err != nil {
			logger.LogError(c, fmt.Sprintf(
				"提示词压缩失败：模型=%s 生效模型=%s 原长度=%d 尝试=%d 耗时=%s 原因=%s",
				requested, effective, length, attempts, time.Since(startedAt), err.Error()))
			rejectPromptTooLong(c)
			return
		}

		// 改写只动 prompt：模型、计费、时长、素材一律不变。
		rewritten := copyBodyWithPrompt(body, compressed)
		if !applyRewrittenBody(c, rewritten, origin) {
			// 改写失败等于回到了「未压缩」的状态，交给上游按原样处理。
			logger.LogError(c, "提示词压缩结果无法写回请求体，放行原请求")
			c.Next()
			return
		}

		finalLength := countRunes(compressed)
		// 记下长度变化：任务落库时写进管理员可见的 PrivateData，
		// 支持侧凭 task_id 就能回答「这条请求被压过没有」。
		common.SetContextKey(c, constant.ContextKeyPromptCompressFrom, length)
		common.SetContextKey(c, constant.ContextKeyPromptCompressTo, finalLength)
		if cfg.ExposeHeader {
			c.Header("X-Wan3-Prompt-Compressed",
				fmt.Sprintf("%d->%d", length, finalLength))
		}
		logger.LogInfo(c, fmt.Sprintf(
			"提示词压缩：%s %d → %d 字符（尝试 %d 次，耗时 %s）",
			requested, length, finalLength, attempts, time.Since(startedAt)))
		c.Next()
	}
}

// compressPromptWithRetry 调压缩模型并校验，最多 cfg.MaxAttempts 次。
//
// 第二次带「上一版错在哪」的反馈：按失败原因决定给模型什么材料
// （只是超长就接着删，丢了标记就必须把原文还给它），详见 compressRetryPrompt。
//
// 返回值 attempts 是**实际调用模型的次数**，用于日志与测试断言。
func compressPromptWithRetry(c *gin.Context, cfg promptcompress_setting.Config, original string) (string, int, error) {
	var previous string
	var lastErr error
	attempts := 0

	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		messages := []compressMessage{{Role: "system", Content: compressSystemPrompt(cfg.TargetRunes)}}
		if lastErr != nil {
			messages = append(messages, compressMessage{
				Role:    "user",
				Content: compressRetryPrompt(original, previous, lastErr, promptRouteLimitRunes, cfg.TargetRunes),
			})
		} else {
			messages = append(messages, compressMessage{
				Role:    "user",
				Content: compressUserPrompt(original, cfg.TargetRunes),
			})
		}

		attempts++
		output, err := compressModelCall(c.Request.Context(), cfg, messages)
		if err != nil {
			noteCompressorResult(false)
			lastErr = err
			// 4xx（令牌错、模型不在白名单）重试只是白等一轮，直接收工。
			var callErr *compressCallError
			if errors.As(err, &callErr) && !callErr.Retryable {
				return "", attempts, err
			}
			continue
		}

		cleaned := cleanCompressedOutput(output)
		if err := validateCompressedPrompt(original, cleaned); err != nil {
			// ⚠️ 这里**不计入熔断**：校验不过说明这条提示词压不动，
			// 是「输入的问题」而不是「压缩器挂了」。若把它也算失败，
			// 一个极端提示词重试几次就能把整个压缩链路熔断 5 分钟，
			// 让其它本来能压的请求一起被拒 —— 单个坏输入不该有这种杀伤力。
			lastErr = err
			previous = cleaned
			continue
		}

		noteCompressorResult(true)
		return cleaned, attempts, nil
	}

	if lastErr == nil {
		lastErr = errors.New("压缩未产生结果")
	}
	return "", attempts, lastErr
}

// copyBodyWithPrompt 返回一份「只换掉 prompt」的请求体副本。
//
// 为什么要复制：原 body 还要在「写回失败 → 放行原请求」的分支里用，
// 就地改会让那条分支也把 prompt 换掉，等于静默压缩。
func copyBodyWithPrompt(body map[string]interface{}, prompt string) map[string]interface{} {
	out := make(map[string]interface{}, len(body))
	for key, value := range body {
		out[key] = value
	}
	out["prompt"] = prompt
	return out
}

// rejectPromptTooLong 用明确的 400 结束请求。
//
// 走 400 而不是 500：这是**客户输入超出上游能力**，不是服务故障。
// 也天然不建任务、不扣费 —— 预扣费发生在渠道选定之后。
func rejectPromptTooLong(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": gin.H{
			"message": i18n.T(c, i18n.MsgRelayPromptTooLong),
			"type":    "prompt_too_long",
			"param":   "prompt",
			"code":    "prompt_too_long",
		},
	})
	c.Abort()
}

// 压缩器健康状态：连续失败到阈值就静默一段时间。
//
// 为什么需要：压缩是**兜底**，它挂了不能让每个长提示词请求都白等 25 秒 × 2。
// 熔断期间直接返回 400，客户立刻知道要精简提示词，运营看日志也能发现
// 「压缩器挂了」—— 而不是让请求一个个超时。
//
// ⚠️ 只统计**调用层失败**（网络错误 / 5xx / 空结果），不统计校验不过：
// 校验不过说明那条提示词压不动，是输入的问题，不该让它把整条链路熔断。
const (
	compressorBreakerThreshold = 5
	compressorBreakerCooldown  = 5 * time.Minute
)

var compressorHealth = struct {
	mu                  sync.Mutex
	consecutiveFailures int
	openUntil           time.Time
}{}

// compressorNow 是当前时间，**只为单测可替换**（测熔断不能真等 5 分钟）。
var compressorNow = time.Now

// compressorBreakerOpen 报告压缩器是否处于熔断期。
//
// 冷却结束后自动复位计数并放行 —— 不需要有人回来手动恢复，
// 与条件路由的「到期自动恢复」同一套思路。
func compressorBreakerOpen() bool {
	compressorHealth.mu.Lock()
	defer compressorHealth.mu.Unlock()

	if compressorHealth.openUntil.IsZero() {
		return false
	}
	if compressorNow().Before(compressorHealth.openUntil) {
		return true
	}
	compressorHealth.openUntil = time.Time{}
	compressorHealth.consecutiveFailures = 0
	return false
}

// noteCompressorResult 记录一次成功/失败，维护熔断窗口。
func noteCompressorResult(ok bool) {
	compressorHealth.mu.Lock()
	defer compressorHealth.mu.Unlock()

	if ok {
		compressorHealth.consecutiveFailures = 0
		compressorHealth.openUntil = time.Time{}
		return
	}
	compressorHealth.consecutiveFailures++
	if compressorHealth.consecutiveFailures >= compressorBreakerThreshold {
		compressorHealth.openUntil = compressorNow().Add(compressorBreakerCooldown)
	}
}
