package minimax

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 语音识别走 MiniMax 自己的 /v1/speech_to_text，不是 OpenAI 的 /v1/audio/transcriptions。
func TestGetRequestURLRoutesTranscriptionToSpeechToText(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:   constant.RelayModeAudioTranscription,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.minimax.chat"},
	}

	url, err := GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.chat/v1/speech_to_text", url)
}

// TTS 的既有出口不能被这次改动碰到。
func TestGetRequestURLKeepsSpeechOnT2A(t *testing.T) {
	info := &relaycommon.RelayInfo{
		RelayMode:   constant.RelayModeAudioSpeech,
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://api.minimax.chat"},
	}

	url, err := GetRequestURL(info)
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.chat/v1/t2a_v2", url)
}

// OpenAI 允许 response_format=text，MiniMax 没有这个取值，必须归到 json，
// 否则上游按自己的默认值返回，客户拿到的东西与声明不符。
func TestNormalizeAsrResponseFormat(t *testing.T) {
	cases := map[string]string{
		"":             "json",
		"json":         "json",
		"text":         "json",
		"verbose_json": "verbose_json",
		"srt":          "srt",
		"VTT":          "vtt",
	}
	for input, want := range cases {
		assert.Equal(t, want, normalizeAsrResponseFormat(input), "input=%q", input)
	}
}

func newAsrTestContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", nil)
	return c, w
}

func newAsrRelayInfo(estimateTokens int) *relaycommon.RelayInfo {
	info := &relaycommon.RelayInfo{RelayMode: constant.RelayModeAudioTranscription}
	info.Request = &dto.AudioRequest{}
	info.SetEstimatePromptTokens(estimateTokens)
	return info
}

// verbose_json 要带说话人分离，字段名按 OpenAI 的 segments/start/end/text，
// 多出来的 speaker 不影响客户端解析。
func TestHandleAsrResponseVerboseJSON(t *testing.T) {
	payload := `{"text":"你好。","duration":12.744,"n_speakers":2,"segments":[{"id":0,"start":0.1,"end":1.66,"speaker":"S1","text":"你好。"}],"trace_id":"t1"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(payload)),
	}
	c, rec := newAsrTestContext(t)
	// 12.744 秒 → 1 分钟口径 1000 token
	info := newAsrRelayInfo(1000)
	info.Request = &dto.AudioRequest{ResponseFormat: "verbose_json"}

	usage, err := handleAsrResponse(c, resp, info, "verbose_json")
	require.NoError(t, err)
	require.NotNil(t, usage)
	assert.Equal(t, 1000, usage.PromptTokens)

	body := rec.Body.String()
	assert.Contains(t, body, `"text":"你好。"`)
	assert.Contains(t, body, `"duration":12.744`)
	assert.Contains(t, body, `"speaker":"S1"`)
	assert.Contains(t, body, `"n_speakers":2`)
	// 上游的 trace_id 属于内部信息，不往客户侧带。
	assert.NotContains(t, body, "trace_id")
}

// json 形态只回 text + duration，与 OpenAI 的 json 响应一致。
func TestHandleAsrResponseJSONOmitsSegments(t *testing.T) {
	payload := `{"text":"hello","duration":3.5,"segments":[{"id":0,"start":0,"end":1,"text":"hi"}]}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(payload)),
	}
	c, rec := newAsrTestContext(t)

	_, err := handleAsrResponse(c, resp, newAsrRelayInfo(1000), "json")
	require.NoError(t, err)

	body := rec.Body.String()
	assert.Contains(t, body, `"text":"hello"`)
	assert.NotContains(t, body, "segments")
}

// srt / vtt 由上游直接返回纯文本，原样透传。
func TestHandleAsrResponsePassesThroughSubtitle(t *testing.T) {
	subtitle := "1\n00:00:00,000 --> 00:00:01,000\n你好\n"
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(bytes.NewBufferString(subtitle)),
	}
	c, rec := newAsrTestContext(t)

	usage, err := handleAsrResponse(c, resp, newAsrRelayInfo(500), "srt")
	require.NoError(t, err)
	assert.Equal(t, 500, usage.PromptTokens)
	assert.Equal(t, subtitle, rec.Body.String())
}

// 响应体异常时必须报错，不能把空文本当成功回给客户（那里已经收过钱了）。
func TestHandleAsrResponseRejectsUpstreamError(t *testing.T) {
	payload := `{"base_resp":{"status_code":1002,"status_msg":"rate limit"}}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(payload)),
	}
	c, _ := newAsrTestContext(t)

	_, err := handleAsrResponse(c, resp, newAsrRelayInfo(1000), "json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

// 计费沿用转录模式的时长口径：1 小时 = 60000 token，与 model ratio 相乘即得价格。
func TestAsrUsageUsesDurationTokens(t *testing.T) {
	usage := asrUsageFromDuration(newAsrRelayInfo(60000))
	assert.Equal(t, 60000, usage.PromptTokens)
	assert.Equal(t, 60000, usage.TotalTokens)
	assert.Zero(t, usage.CompletionTokens)
	// 走文本结算分支（音频明细为空），因此不能被 PostAudioConsumeQuota 的音频倍率再乘一次。
	assert.Zero(t, usage.PromptTokensDetails.AudioTokens)
}

// OpenAI 客户端常传 alloy 这类预置音色，MiniMax 不认识会回 empty field，
// 必须落默认音色；客户自己克隆的 voice_id 则要原样透传。
func TestNormalizeVoiceID(t *testing.T) {
	assert.Equal(t, "male-qn-qingse", normalizeVoiceID(""))
	assert.Equal(t, "male-qn-qingse", normalizeVoiceID("alloy"))
	assert.Equal(t, "male-qn-qingse", normalizeVoiceID("Nova"))
	assert.Equal(t, "my-cloned-voice-01", normalizeVoiceID("my-cloned-voice-01"))
}

// output_format 只接受 hex/url：把 OpenAI 的 mp3 透传上去，上游直接 400。
func TestNormalizeAudioFormat(t *testing.T) {
	assert.Equal(t, "mp3", normalizeAudioFormat(""))
	assert.Equal(t, "mp3", normalizeAudioFormat("mp3"))
	assert.Equal(t, "mp3", normalizeAudioFormat("opus"))
	assert.Equal(t, "wav", normalizeAudioFormat("wav"))
	assert.Equal(t, "flac", normalizeAudioFormat("flac"))
	assert.Equal(t, "pcm", normalizeAudioFormat("pcm"))
}
