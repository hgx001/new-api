package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newModelEchoInfo 构造一个用于验证 model 回显的 RelayInfo。
// mapped=true 时模拟渠道 model_mapping 生效（如 opus-5.5 → gpt-6-luna）。
func newModelEchoInfo(originModel, upstreamModel string, mapped bool) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: upstreamModel,
			IsModelMapped:     mapped,
		},
		OriginModelName: originModel,
		RelayFormat:     types.RelayFormatOpenAI,
	}
}

func newModelEchoTestContext(body string, contentType string) (*gin.Context, *httptest.ResponseRecorder, *http.Response) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{"Content-Type": []string{contentType}},
	}
	return c, recorder, resp
}

func TestResponseModelNameOnlyForMappedModels(t *testing.T) {
	require.Equal(t, "opus-5.5", responseModelName(newModelEchoInfo("opus-5.5", "gpt-6-luna", true)),
		"映射生效时必须回显用户请求的原始名")
	require.Equal(t, "", responseModelName(newModelEchoInfo("gpt-6-luna", "gpt-6-luna", false)),
		"未映射时不得改写，保持原文透传")
	require.Equal(t, "", responseModelName(nil))
	require.Equal(t, "", responseModelName(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{IsModelMapped: true},
	}), "缺少原始名时不能把 model 置空")
}

func TestRewriteJSONModelFieldReplacesTopLevelModelOnly(t *testing.T) {
	in := `{"id":"chatcmpl-1","model":"gpt-6-luna","created":1710000000,` +
		`"nested":{"ratio":0.064212000001,"keep":"<tag>"},"choices":[]}`

	out, ok := rewriteJSONModelField(in, "opus-5.5")
	require.True(t, ok)
	require.Contains(t, out, `"model":"opus-5.5"`)
	require.NotContains(t, out, "gpt-6-luna")

	// 其它字段按原始字节保留：数字精度不被重排，字符串转义风格不变
	require.Contains(t, out, `"ratio":0.064212000001`)
	require.Contains(t, out, `"keep":"<tag>"`)
	require.Contains(t, out, `"created":1710000000`)
	require.Contains(t, out, `"id":"chatcmpl-1"`)
}

func TestRewriteJSONModelFieldReturnsFalseWithoutModel(t *testing.T) {
	_, ok := rewriteJSONModelField(`{"id":"x"}`, "opus-5.5")
	require.False(t, ok, "没有 model 字段时不应改写")

	_, ok = rewriteJSONModelField(`not-json`, "opus-5.5")
	require.False(t, ok, "非 JSON 数据必须原样保留，不能因改写失败丢内容")
}

func TestRewriteResponsesStreamModelFieldReplacesNestedModel(t *testing.T) {
	in := `{"type":"response.completed","response":{"id":"resp_1","model":"gpt-6-luna",` +
		`"usage":{"input_tokens":2,"output_tokens":3}}}`

	out, ok := rewriteResponsesStreamModelField(in, "opus-5.5")
	require.True(t, ok)
	require.Contains(t, out, `"model":"opus-5.5"`)
	require.NotContains(t, out, "gpt-6-luna")
	require.Contains(t, out, `"type":"response.completed"`)
	require.Contains(t, out, `"input_tokens":2`)

	_, ok = rewriteResponsesStreamModelField(`{"type":"response.output_text.delta","delta":"hi"}`, "opus-5.5")
	require.False(t, ok, "不含 response 对象的事件应保持原样")
}

func TestOpenaiHandlerEchoesOriginModelNameWhenMapped(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"id":"chatcmpl-1","object":"chat.completion","created":1710000000,` +
		`"model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},` +
		`"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}}`

	c, recorder, resp := newModelEchoTestContext(body, "application/json")
	usage, apiErr := OpenaiHandler(c, newModelEchoInfo("opus-5.5", "gpt-6-luna", true), resp)
	require.Nil(t, apiErr)
	require.Equal(t, 10, usage.PromptTokens)

	got := recorder.Body.String()
	require.Contains(t, got, `"model":"opus-5.5"`)
	require.NotContains(t, got, "gpt-6-luna", "响应体不得泄露上游模型名")
	require.Contains(t, got, `"content":"hi"`, "改写 model 不能影响正文内容")
}

func TestOpenaiHandlerKeepsUpstreamModelWhenNotMapped(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"id":"chatcmpl-2","object":"chat.completion","created":1710000000,` +
		`"model":"gpt-6-luna","choices":[{"index":0,"message":{"role":"assistant","content":"hi"},` +
		`"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1,"total_tokens":5}}`

	c, recorder, resp := newModelEchoTestContext(body, "application/json")
	_, apiErr := OpenaiHandler(c, newModelEchoInfo("gpt-6-luna", "gpt-6-luna", false), resp)
	require.Nil(t, apiErr)

	require.Contains(t, recorder.Body.String(), `"model":"gpt-6-luna"`,
		"未映射模型的行为必须完全不变")
}

func TestOaiResponsesHandlerEchoesOriginModelNameWhenMapped(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	body := `{"id":"resp_1","object":"response","created_at":1710000000,"status":"completed",` +
		`"model":"gpt-6-luna","output":[],"usage":{"input_tokens":2,"output_tokens":3,"total_tokens":5}}`

	c, recorder, resp := newModelEchoTestContext(body, "application/json")
	usage, apiErr := OaiResponsesHandler(c, newModelEchoInfo("opus-5.5", "gpt-6-luna", true), resp)
	require.Nil(t, apiErr)
	require.Equal(t, 2, usage.PromptTokens)

	got := recorder.Body.String()
	require.Contains(t, got, `"model":"opus-5.5"`)
	require.NotContains(t, got, "gpt-6-luna")
}

func TestOaiStreamHandlerEchoesOriginModelNameWhenMapped(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })

	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"role":"assistant","content":""},"finish_reason":null}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","created":1710000000,"model":"gpt-6-luna","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	c, recorder, resp := newModelEchoTestContext(body, "text/event-stream")
	info := newModelEchoInfo("opus-5.5", "gpt-6-luna", true)
	info.IsStream = true
	info.ShouldIncludeUsage = true
	info.DisablePing = true

	usage, apiErr := OaiStreamHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.NotNil(t, usage)
	require.Equal(t, 5, usage.PromptTokens)

	got := recorder.Body.String()
	require.Contains(t, got, `"model":"opus-5.5"`)
	require.NotContains(t, got, "gpt-6-luna", "流式每一段 chunk 都不得泄露上游模型名")
	require.Contains(t, got, `"content":"hi"`)
	require.Contains(t, got, "data: [DONE]")
}
