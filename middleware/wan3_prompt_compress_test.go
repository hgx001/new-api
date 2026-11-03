package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/setting/promptcompress_setting"
	"github.com/gin-gonic/gin"
)

// 提示词压缩中间件的测试。
//
// 核心断言：**要么压缩成功且只有 prompt 被换掉，要么请求体逐字节不变**，
// 压不下来就 400。绝不允许「部分改写」或「截断提交」—— 那两种形态都会让
// 客户拿到与提示词不符的成片，而账单照收。
//
// ⚠️ 所有用例都用 fake 压缩器（compressModelCall），不真调 LLM：
// 真实调用既慢又不确定，且会污染渠道账目。

// compressStep 是一次 fake 调用的结果。
type compressStep struct {
	out string
	err error
}

// setupPromptCompress 打开压缩配置，测试结束恢复原状并复位熔断器。
func setupPromptCompress(t *testing.T, overrides map[string]interface{}) {
	t.Helper()
	// 400 的对客文案走 i18n；单测进程里没有 main() 的初始化，这里补一次。
	// Init 内部有 sync.Once，重复调用无副作用。
	if err := i18n.Init(); err != nil {
		t.Fatalf("init i18n: %v", err)
	}
	previous := promptcompress_setting.Get()

	payload := map[string]interface{}{
		"enabled":         true,
		"model":           "MiniMax-M3.1",
		"endpoint":        "http://127.0.0.1:3000/v1/chat/completions",
		"token":           "test-token",
		"target_runes":    4600,
		"timeout_seconds": 5,
		"max_attempts":    2,
	}
	for key, value := range overrides {
		payload[key] = value
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal compress config: %v", err)
	}
	if err := promptcompress_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update compress config: %v", err)
	}
	resetCompressorHealth()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = promptcompress_setting.UpdateFromJSONString(string(restored))
		resetCompressorHealth()
	})
}

// resetCompressorHealth 把熔断器清回初始状态（用例之间互不影响）。
func resetCompressorHealth() {
	compressorHealth.mu.Lock()
	defer compressorHealth.mu.Unlock()
	compressorHealth.consecutiveFailures = 0
	compressorHealth.openUntil = time.Time{}
}

// stubCompressSteps 注入一串 fake 结果，返回「实际被调用了几次」的计数器。
func stubCompressSteps(t *testing.T, steps ...compressStep) *int {
	t.Helper()
	previous := compressModelCall
	calls := 0
	compressModelCall = func(_ context.Context, _ promptcompress_setting.Config, _ []compressMessage) (string, error) {
		index := calls
		calls++
		if index >= len(steps) {
			return "", errors.New("fake compressor: 没有更多脚本步骤")
		}
		return steps[index].out, steps[index].err
	}
	t.Cleanup(func() { compressModelCall = previous })
	return &calls
}

// compressRecorder 记录下游 handler 看到的请求体。
type compressRecorder struct {
	body string
	// from / to 是中间件记下的「压缩前后字符数」（未压缩时为 0）。
	from int
	to   int
}

// newCompressEngine 建一个挂上压缩中间件的引擎。
//
// originModel 非空时，先由一个前置中间件写入「改派前的模型名」——
// 模拟条件路由已经跑过（这是判断该不该压的关键输入）。
//
// ⚠️ Use 必须在路由注册之前：gin 在注册路由那一刻就快照了中间件链。
func newCompressEngine(rec *compressRecorder, originModel string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if originModel != "" {
		engine.Use(func(c *gin.Context) {
			c.Set(routeOriginModelKey, originModel)
			c.Next()
		})
	}
	engine.Use(Wan3PromptCompress())
	engine.POST("/v1/video/generations", func(c *gin.Context) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.String(500, "read body storage: %v", err)
			return
		}
		raw, err := storage.Bytes()
		if err != nil {
			c.String(500, "read body bytes: %v", err)
			return
		}
		rec.body = string(raw)
		rec.from = common.GetContextKeyInt(c, constant.ContextKeyPromptCompressFrom)
		rec.to = common.GetContextKeyInt(c, constant.ContextKeyPromptCompressTo)
		c.String(200, "ok")
	})
	engine.POST("/v1/videos/:video_id/remix", func(c *gin.Context) {
		storage, _ := common.GetBodyStorage(c)
		raw, _ := storage.Bytes()
		rec.body = string(raw)
		c.String(200, "ok")
	})
	return engine
}

// compressResult 是一次请求的结果。
type compressResult struct {
	status int
	body   string
	from   int
	to     int
}

// runCompress 把请求灌进引擎。
func runCompress(t *testing.T, originModel, path, body string) compressResult {
	t.Helper()
	rec := &compressRecorder{}
	engine := newCompressEngine(rec, originModel)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return compressResult{status: response.Code, body: rec.body, from: rec.from, to: rec.to}
}

// buildCompressBody 造请求体。
func buildCompressBody(t *testing.T, model, prompt string, extra map[string]interface{}) string {
	t.Helper()
	body := map[string]interface{}{
		"model":  model,
		"prompt": prompt,
	}
	for key, value := range extra {
		body[key] = value
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(encoded)
}

// decodeCompressBody 解出下游看到的请求体。
func decodeCompressBody(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	if raw == "" {
		t.Fatal("下游 handler 没有读到请求体")
	}
	var out map[string]interface{}
	if err := common.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	return out
}

// 恰好 5000 字符不压缩：上游收得下，压了等于白白改写客户文案。
func TestPromptCompressSkipsPromptAtLimit(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t)

	prompt := strings.Repeat("字", promptRouteLimitRunes)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if *calls != 0 {
		t.Fatalf("恰好 5000 字符不该调压缩模型，实际调了 %d 次", *calls)
	}
	if got := decodeCompressBody(t, result.body)["prompt"]; got != prompt {
		t.Fatalf("prompt 被改了（%d → %d 字符）", countRunes(prompt), countRunes(got.(string)))
	}
}

// 5001 字符触发压缩，且只换 prompt，模型/时长/素材保持不变。
func TestPromptCompressRewritesOnlyPrompt(t *testing.T) {
	setupPromptCompress(t, nil)
	compressed := strings.Repeat("压", 3000)
	stubCompressSteps(t, compressStep{out: compressed})

	prompt := strings.Repeat("字", promptRouteLimitRunes+1)
	body := buildCompressBody(t, testSourceModel, prompt, map[string]interface{}{
		"seconds": 10,
		"metadata": map[string]interface{}{
			"ratio": "16:9",
			"media": []interface{}{map[string]interface{}{"type": "reference_image", "url": "https://example.com/a.png"}},
		},
	})
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	out := decodeCompressBody(t, result.body)
	if out["prompt"] != compressed {
		t.Fatalf("prompt 未被替换为压缩结果")
	}
	if out["model"] != testSourceModel {
		t.Fatalf("model = %v, want %s（压缩不能改模型）", out["model"], testSourceModel)
	}
	if out["seconds"] != float64(10) {
		t.Fatalf("seconds = %v, want 10（压缩不能改时长）", out["seconds"])
	}
	metadata, _ := out["metadata"].(map[string]interface{})
	if metadata["ratio"] != "16:9" {
		t.Fatalf("metadata.ratio 被改动了：%v", metadata["ratio"])
	}
	if media, _ := metadata["media"].([]interface{}); len(media) != 1 {
		t.Fatalf("参考图丢了：%v", metadata["media"])
	}
	// 任务侧要能回答「这条压过没有」：原长度与压缩后长度都要记下来。
	if result.from != promptRouteLimitRunes+1 || result.to != countRunes(compressed) {
		t.Fatalf("压缩长度没记对：from=%d to=%d", result.from, result.to)
	}
}

// 白名单之外的模型一律不压（wan3 一族按 wan3 卖，超长让客户精简）。
func TestPromptCompressSkipsNonFamilyModels(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t)

	longPrompt := strings.Repeat("字", promptRouteLimitRunes+100)
	for _, model := range []string{"wan3.0-video", "wan3.0-720p-特惠", testTargetModel} {
		result := runCompress(t, "", "/v1/video/generations",
			buildCompressBody(t, model, longPrompt, nil))
		if result.status != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", model, result.status)
		}
	}
	if *calls != 0 {
		t.Fatalf("非白名单模型不该触发压缩，实际调了 %d 次", *calls)
	}
}

// 豆包已经接走（body.model 已是 seedance-2-5）→ 不压。
func TestPromptCompressSkipsWhenDoubaoAlreadyRouted(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t)

	longPrompt := strings.Repeat("字", promptRouteLimitRunes+100)
	body := buildCompressBody(t, testTargetModel, longPrompt, nil)
	result := runCompress(t, testSourceModel, "/v1/video/generations", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if *calls != 0 {
		t.Fatalf("豆包已接走的请求不该再压缩，实际调了 %d 次", *calls)
	}
}

// 被规则三改派到 wan3.0-720p-特惠的请求仍要压缩：
// 那条链路同样 5000 上限，客户买的是特惠，得替他兜住。
func TestPromptCompressRunsAfterFramesReroute(t *testing.T) {
	setupPromptCompress(t, nil)
	stubCompressSteps(t, compressStep{out: strings.Repeat("压", 3000)})

	longPrompt := strings.Repeat("字", promptRouteLimitRunes+100)
	body := buildCompressBody(t, "wan3.0-720p-特惠", longPrompt, nil)
	result := runCompress(t, testSourceModel, "/v1/video/generations", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if countRunes(decodeCompressBody(t, result.body)["prompt"].(string)) >= promptRouteLimitRunes {
		t.Fatalf("首尾帧改派后的超长提示词没有被压缩")
	}
}

// 丢引用标记 → 校验失败 → 重试一次 → 仍失败 → 400（绝不截断提交）。
func TestPromptCompressRejectsWhenCitationMarkerLost(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t,
		compressStep{out: "@图片1 一只橘猫在窗边打盹，阳光缓慢移动，镜头轻微推进，" + strings.Repeat("细节", 1400)},
		compressStep{out: "@图片1 一只橘猫在窗边打盹，阳光缓慢移动，镜头轻微推进，" + strings.Repeat("细节", 1400)},
	)

	prompt := "@图片1 @图片2 一只橘猫在窗边打盹，" + strings.Repeat("细节", 2600)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400（丢了 @图片2 不能放行）", result.status)
	}
	if result.body != "" {
		t.Fatalf("400 时下游 handler 不该被执行，却读到了请求体：%s", truncateForLog(result.body, 80))
	}
	if *calls != 2 {
		t.Fatalf("校验失败后应重试一次（共 2 次调用），实际 %d 次", *calls)
	}
}

// 模型把结果包在代码围栏里 → 清洗后应当通过。
func TestPromptCompressCleansCodeFence(t *testing.T) {
	setupPromptCompress(t, nil)
	inner := strings.Repeat("压", 3000)
	stubCompressSteps(t, compressStep{out: "```text\n" + inner + "\n```"})

	prompt := strings.Repeat("字", promptRouteLimitRunes+1)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if got := decodeCompressBody(t, result.body)["prompt"]; got != inner {
		t.Fatalf("代码围栏没被清洗：%q", truncateForLog(fmt.Sprint(got), 60))
	}
}

// /remix 放行：改写 prompt 会破坏 remix 的原任务语义。
func TestPromptCompressSkipsRemix(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t)

	longPrompt := strings.Repeat("字", promptRouteLimitRunes+100)
	body := buildCompressBody(t, testSourceModel, longPrompt, nil)
	result := runCompress(t, "", "/v1/videos/abc123/remix", body)

	if result.status != http.StatusOK {
		t.Fatalf("status = %d, want 200", result.status)
	}
	if *calls != 0 {
		t.Fatalf("remix 不该触发压缩，实际调了 %d 次", *calls)
	}
}

// 压缩模型两次都网络失败 → 400，不建任务、不扣费。
func TestPromptCompressReturns400WhenModelUnavailable(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t,
		compressStep{err: &compressCallError{Message: "connection reset", Retryable: true}},
		compressStep{err: &compressCallError{Message: "connection reset", Retryable: true}},
	)

	body := buildCompressBody(t, testSourceModel, strings.Repeat("字", promptRouteLimitRunes+1), nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", result.status)
	}
	if *calls != 2 {
		t.Fatalf("可重试的失败应重试一次（共 2 次），实际 %d 次", *calls)
	}
}

// 4xx（令牌错/模型不在白名单）不重试：重试只是白等一轮。
func TestPromptCompressDoesNotRetryClientErrors(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t,
		compressStep{err: &compressCallError{Status: http.StatusUnauthorized, Message: "invalid token"}},
	)

	body := buildCompressBody(t, testSourceModel, strings.Repeat("字", promptRouteLimitRunes+1), nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", result.status)
	}
	if *calls != 1 {
		t.Fatalf("4xx 不该重试，实际调用 %d 次", *calls)
	}
}

// 没配内部令牌：不发起注定 401 的调用，直接 400 并在日志里点名。
func TestPromptCompressRejectsWithoutToken(t *testing.T) {
	setupPromptCompress(t, map[string]interface{}{"token": ""})
	calls := stubCompressSteps(t)

	body := buildCompressBody(t, testSourceModel, strings.Repeat("字", promptRouteLimitRunes+1), nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", result.status)
	}
	if *calls != 0 {
		t.Fatalf("没配令牌不该调用压缩模型，实际 %d 次", *calls)
	}
}

// 熔断：连续 5 次失败后静默 5 分钟，冷却结束自动恢复。
func TestPromptCompressorBreakerOpensAndRecovers(t *testing.T) {
	resetCompressorHealth()
	t.Cleanup(resetCompressorHealth)

	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	previous := compressorNow
	compressorNow = func() time.Time { return now }
	t.Cleanup(func() { compressorNow = previous })

	if compressorBreakerOpen() {
		t.Fatal("初始状态不该熔断")
	}
	for i := 0; i < compressorBreakerThreshold-1; i++ {
		noteCompressorResult(false)
	}
	if compressorBreakerOpen() {
		t.Fatal("未到阈值不该熔断")
	}
	noteCompressorResult(false)
	if !compressorBreakerOpen() {
		t.Fatal("连续失败达到阈值后应熔断")
	}

	// 冷却期内仍然熔断。
	now = now.Add(compressorBreakerCooldown - time.Second)
	if !compressorBreakerOpen() {
		t.Fatal("冷却期内应保持熔断")
	}
	// 过冷却点自动恢复。
	now = now.Add(2 * time.Second)
	if compressorBreakerOpen() {
		t.Fatal("冷却结束后应自动恢复")
	}
}

// 校验不过**不计入熔断**：一个压不动的提示词不能把整条压缩链路熔断。
func TestPromptCompressorBreakerIgnoresValidationFailures(t *testing.T) {
	setupPromptCompress(t, nil)
	// 每次请求都会压两轮，两轮都丢 @图片2 → 校验失败。
	bad := "@图片1 一只橘猫在窗边打盹，" + strings.Repeat("细节", 1500)
	stubCompressSteps(t,
		compressStep{out: bad}, compressStep{out: bad},
		compressStep{out: bad}, compressStep{out: bad},
		compressStep{out: bad}, compressStep{out: bad},
	)

	prompt := "@图片1 @图片2 一只橘猫在窗边打盹，" + strings.Repeat("细节", 2600)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	for i := 0; i < 3; i++ {
		if result := runCompress(t, "", "/v1/video/generations", body); result.status != http.StatusBadRequest {
			t.Fatalf("第 %d 次：status = %d, want 400", i+1, result.status)
		}
	}

	if compressorBreakerOpen() {
		t.Fatal("校验失败不该触发熔断（那是输入的问题，不是压缩器挂了）")
	}
}

// 调用层连续失败（网络/5xx）才熔断：之后的长提示词请求不再等 25 秒。
func TestPromptCompressorBreakerTripsOnCallFailures(t *testing.T) {
	setupPromptCompress(t, nil)
	errStep := compressStep{err: &compressCallError{Message: "connection reset", Retryable: true}}
	stubCompressSteps(t, errStep, errStep, errStep, errStep, errStep, errStep)

	body := buildCompressBody(t, testSourceModel, strings.Repeat("字", promptRouteLimitRunes+1), nil)
	for i := 0; i < 3; i++ {
		runCompress(t, "", "/v1/video/generations", body)
	}

	if !compressorBreakerOpen() {
		t.Fatal("连续 6 次调用失败后应熔断")
	}
}

// stubCompressStepsCapture 与 stubCompressSteps 相同，但额外记录每次调用发给模型的
// messages —— 用来断言「重试时到底给了模型什么材料」，这是 fake 返回值本身看不出来的。
func stubCompressStepsCapture(t *testing.T, steps ...compressStep) (*int, *[][]compressMessage) {
	t.Helper()
	previous := compressModelCall
	calls := 0
	var captured [][]compressMessage
	compressModelCall = func(_ context.Context, _ promptcompress_setting.Config, messages []compressMessage) (string, error) {
		index := calls
		calls++
		captured = append(captured, messages)
		if index >= len(steps) {
			return "", errors.New("fake compressor: 没有更多脚本步骤")
		}
		return steps[index].out, steps[index].err
	}
	t.Cleanup(func() { compressModelCall = previous })
	return &calls, &captured
}

// 内容被改坏（丢了引用标记）时的重试必须**把原文还给模型**。
//
// 只喂上一版的话，模型看不到原文，根本无从知道丢了 @图片2 —— 而当时的指令却是
// 「继续删减」，只会越删越远。这条用例锁死这个行为。
func TestPromptCompressRetryHandsOriginalBackWhenContentBroken(t *testing.T) {
	setupPromptCompress(t, nil)
	bad := "@图片1 一只橘猫在窗边打盹，" + strings.Repeat("细节", 1500)
	calls, captured := stubCompressStepsCapture(t, compressStep{out: bad}, compressStep{out: bad})

	prompt := "@图片1 @图片2 一只橘猫在窗边打盹，" + strings.Repeat("细节", 2600)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	runCompress(t, "", "/v1/video/generations", body)

	if *calls != 2 || len(*captured) != 2 {
		t.Fatalf("应重试一次（共 2 次调用），实际 %d 次", *calls)
	}
	retry := (*captured)[1]
	last := retry[len(retry)-1]
	if !strings.Contains(last.Content, "<original>") {
		t.Fatalf("重试没把原文给模型，它无法补回丢失的标记：%s", truncateForLog(last.Content, 120))
	}
	if !strings.Contains(last.Content, "图片2") {
		t.Fatalf("重试提示里没点名丢了哪个标记：%s", truncateForLog(last.Content, 120))
	}
	if strings.Contains(last.Content, "超出上限 0 字符") {
		t.Fatalf("内容类失败不该伪装成超长，会诱导模型继续删：%s", truncateForLog(last.Content, 120))
	}
}

// 只是超长时的重试仍走「在上一版基础上继续删」：不需要原文，收敛更快。
func TestPromptCompressRetryStaysIncrementalWhenOnlyTooLong(t *testing.T) {
	setupPromptCompress(t, nil)
	tooLong := strings.Repeat("字", promptRouteLimitRunes+300)
	_, captured := stubCompressStepsCapture(t, compressStep{out: tooLong})

	prompt := strings.Repeat("字", promptRouteLimitRunes+1)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	runCompress(t, "", "/v1/video/generations", body)

	if len(*captured) != 2 {
		t.Fatalf("应重试一次，实际 %d 次", len(*captured))
	}
	last := (*captured)[1][1]
	if !strings.Contains(last.Content, "<previous>") {
		t.Fatalf("超长重试应基于上一版增量删减：%s", truncateForLog(last.Content, 120))
	}
	if !strings.Contains(last.Content, "超出上限") {
		t.Fatalf("超长重试应告知超了多少：%s", truncateForLog(last.Content, 120))
	}
}

// 超过「可压缩上界」的原文必然过不了保真下限：不许调模型，直接 400。
//
// 否则每次都是一次注定失败的双调用 —— 客户拿不到片，平台白付两次 LLM 费用。
func TestPromptCompressRejectsPromptTooLongToCompress(t *testing.T) {
	setupPromptCompress(t, nil)
	calls := stubCompressSteps(t)

	prompt := strings.Repeat("字", maxCompressibleRunes+1)
	body := buildCompressBody(t, testSourceModel, prompt, nil)
	result := runCompress(t, "", "/v1/video/generations", body)

	if result.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", result.status)
	}
	if *calls != 0 {
		t.Fatalf("注定压不下来的请求不该调模型，实际调了 %d 次", *calls)
	}
}

// maxCompressibleRunes 必须等于 5000/保真下限，否则会放出「必然失败」的请求。
func TestMaxCompressibleRunesMatchesFidelityFloor(t *testing.T) {
	want := int(float64(promptRouteLimitRunes) / minCompressFidelity)
	if maxCompressibleRunes != want {
		t.Fatalf("maxCompressibleRunes = %d, want %d（5000 / %.2f）",
			maxCompressibleRunes, want, minCompressFidelity)
	}
}

// 输出被 max_tokens 截断时必须判失败：半截提示词的长度、标记都可能通过校验，
// 放过去就是把缺了后半段的创作描述交给上游，而任务成功、账单照收。
func TestPromptCompressTreatsTruncatedOutputAsFailure(t *testing.T) {
	cases := []struct {
		name         string
		finishReason string
		wantErr      bool
	}{
		{name: "正常结束", finishReason: "stop", wantErr: false},
		{name: "被截断", finishReason: "length", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"压缩后的提示词"},"finish_reason":"` +
					tc.finishReason + `"}]}`))
			}))
			defer server.Close()

			cfg := promptcompress_setting.Config{
				Model:          "MiniMax-M3.1",
				Endpoint:       server.URL,
				Token:          "test-token",
				TimeoutSeconds: 5,
			}
			content, err := requestPromptCompression(context.Background(), cfg,
				[]compressMessage{{Role: "user", Content: "x"}})

			if !tc.wantErr {
				if err != nil {
					t.Fatalf("正常结束不该报错：%v", err)
				}
				if content != "压缩后的提示词" {
					t.Fatalf("content = %q", content)
				}
				return
			}
			var callErr *compressCallError
			if !errors.As(err, &callErr) {
				t.Fatalf("err = %v, want *compressCallError", err)
			}
			if !callErr.Retryable {
				t.Fatal("截断应判为可重试（换一次更克制的删法）")
			}
			if !strings.Contains(callErr.Message, "truncated") {
				t.Fatalf("日志要能看出是截断：%s", callErr.Message)
			}
		})
	}
}
