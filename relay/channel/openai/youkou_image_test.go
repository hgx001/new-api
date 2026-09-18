package openai

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Upstream model ids (production channel #19 ModelMapping):
// TT Image 2 = mdl_bfb3f6112aa85bd4a309e842c33c7448,
// TT Image 2.5 = mdl_5230f896b94966ce7463817af855bbdd.
func TestResolveYoukouImageModel(t *testing.T) {
	require.Equal(t, "mdl_bfb3f6112aa85bd4a309e842c33c7448", resolveYoukouImageModel("gpt-image-2"))
	require.Equal(t, "mdl_5230f896b94966ce7463817af855bbdd", resolveYoukouImageModel("gpt-image-2.5"))
	// Already-mapped ids and unknown aliases pass through untouched.
	require.Equal(t, "mdl_bfb3f6112aa85bd4a309e842c33c7448", resolveYoukouImageModel("mdl_bfb3f6112aa85bd4a309e842c33c7448"))
	require.Equal(t, "gpt-image-1", resolveYoukouImageModel("gpt-image-1"))
	require.Equal(t, "", resolveYoukouImageModel(""))
}

func TestDetectYoukouImageTask(t *testing.T) {
	taskID, statusURL, ok := detectYoukouImageTask([]byte(`{"object":"generation.task","status":"queued","id":"task_11787","status_url":"/v1/tasks/task_11787"}`))
	require.True(t, ok)
	require.Equal(t, "task_11787", taskID)
	require.Equal(t, "/v1/tasks/task_11787", statusURL)

	_, _, ok = detectYoukouImageTask([]byte(`{"created":1700000000,"data":[{"url":"https://example/x.png"}]}`))
	require.False(t, ok)

	_, _, ok = detectYoukouImageTask([]byte(`not json`))
	require.False(t, ok)
}

func TestJoinUpstreamURL(t *testing.T) {
	require.Equal(t, "https://api.erchun.youkou.cc/v1/tasks/abc", joinUpstreamURL("https://api.erchun.youkou.cc", "/v1/tasks/abc"))
	require.Equal(t, "https://api.erchun.youkou.cc/v1/tasks/abc", joinUpstreamURL("https://api.erchun.youkou.cc/", "v1/tasks/abc"))
	require.Equal(t, "https://media.youkou.cc/x.png", joinUpstreamURL("https://api.erchun.youkou.cc", "https://media.youkou.cc/x.png"))
	require.Equal(t, "", joinUpstreamURL("https://api.erchun.youkou.cc", ""))
}

func youkouImageTestInfo(baseURL string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		RelayMode: relayconstant.RelayModeImagesGenerations,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeOpenAI,
			ChannelBaseUrl: baseURL,
			ApiKey:         "test-key",
		},
		OriginModelName: "gpt-image-2",
		Request:         &dto.ImageRequest{Model: "gpt-image-2", Prompt: "a cat"},
	}
}

// The submit payload for Youkou image channels must be exactly
// {model: mdl_*, prompt} so the upstream model gate accepts it.
func TestConvertYoukouImageRequestMinimal(t *testing.T) {
	a := &Adaptor{}
	info := youkouImageTestInfo("https://api.erchun.youkou.cc")
	converted, err := a.ConvertImageRequest(nil, info, dto.ImageRequest{Model: "gpt-image-2", Prompt: "a cat"})
	require.NoError(t, err)
	raw, err := common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"mdl_bfb3f6112aa85bd4a309e842c33c7448","prompt":"a cat"}`, string(raw))

	// An already-mapped mdl id passes through.
	converted, err = a.ConvertImageRequest(nil, info, dto.ImageRequest{Model: "mdl_5230f896b94966ce7463817af855bbdd", Prompt: "hi"})
	require.NoError(t, err)
	raw, err = common.Marshal(converted)
	require.NoError(t, err)
	require.JSONEq(t, `{"model":"mdl_5230f896b94966ce7463817af855bbdd","prompt":"hi"}`, string(raw))

	// Non-Youkou channels keep the standard OpenAI request untouched.
	plain := youkouImageTestInfo("https://api.openai.com")
	converted, err = a.ConvertImageRequest(nil, plain, dto.ImageRequest{Model: "gpt-image-2", Prompt: "a cat"})
	require.NoError(t, err)
	_, isImageRequest := converted.(dto.ImageRequest)
	require.True(t, isImageRequest)
}

// A 202 Accepted from a Youkou image upstream must reach DoResponse as 200:
// ImageHelper only forwards 200 (plus 201 for Replicate), while the
// task-style submit answers 200 or 202 depending on upstream behavior.
func TestNormalizeYoukouAcceptedStatus(t *testing.T) {
	newResp := func(status int) *http.Response {
		return &http.Response{StatusCode: status, Header: make(http.Header)}
	}

	youkou := youkouImageTestInfo("https://api.erchun.youkou.cc")
	resp := newResp(http.StatusAccepted)
	normalizeYoukouAcceptedStatus(resp, youkou)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Sync 200 responses are untouched.
	resp = newResp(http.StatusOK)
	normalizeYoukouAcceptedStatus(resp, youkou)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Non-Youkou channels keep 202 as-is.
	plain := youkouImageTestInfo("https://api.openai.com")
	resp = newResp(http.StatusAccepted)
	normalizeYoukouAcceptedStatus(resp, plain)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Non-image relay modes keep 202 as-is.
	chat := youkouImageTestInfo("https://api.erchun.youkou.cc")
	chat.RelayMode = relayconstant.RelayModeChatCompletions
	resp = newResp(http.StatusAccepted)
	normalizeYoukouAcceptedStatus(resp, chat)
	require.Equal(t, http.StatusAccepted, resp.StatusCode)

	// Nil inputs are safe.
	normalizeYoukouAcceptedStatus(nil, youkou)
	normalizeYoukouAcceptedStatus(newResp(http.StatusAccepted), nil)
}

// Submits must carry an auto Idempotency-Key when the caller did not supply
// one; a caller-supplied key is preserved for safe retries.
func TestSetupRequestHeaderImageIdempotencyKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}
	newHeader := func() http.Header {
		h := make(http.Header)
		h.Set("Authorization", "Bearer test-key")
		return h
	}
	info := youkouImageTestInfo("https://api.erchun.youkou.cc")

	a := &Adaptor{}
	h1 := newHeader()
	require.NoError(t, a.SetupRequestHeader(newCtx(), &h1, info))
	require.Len(t, h1.Get("Idempotency-Key"), 36)

	h2 := newHeader()
	require.NoError(t, a.SetupRequestHeader(newCtx(), &h2, info))
	require.NotEqual(t, h1.Get("Idempotency-Key"), h2.Get("Idempotency-Key"))

	h3 := newHeader()
	h3.Set("Idempotency-Key", "keep-me")
	require.NoError(t, a.SetupRequestHeader(newCtx(), &h3, info))
	require.Equal(t, "keep-me", h3.Get("Idempotency-Key"))

	// Non-image modes never gain the header.
	chatInfo := youkouImageTestInfo("https://api.erchun.youkou.cc")
	chatInfo.RelayMode = relayconstant.RelayModeChatCompletions
	h4 := newHeader()
	require.NoError(t, a.SetupRequestHeader(newCtx(), &h4, chatInfo))
	require.Empty(t, h4.Get("Idempotency-Key"))
}

// fakeYoukouImageServer emulates the upstream: queued -> completed with
// content_url, and content_url 302-redirects to a media PNG.
func fakeYoukouImageServer(t *testing.T, polls *atomic.Int64, mode string) *httptest.Server {
	t.Helper()
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02}
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/tasks/img1", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		if mode == "queued-forever" || polls.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"img1","status":"queued"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"img1","status":"completed","content_url":"/v1/tasks/img1/content"}`))
	})
	mux.HandleFunc("/v1/tasks/img1/content", func(w http.ResponseWriter, r *http.Request) {
		if mode == "inline-bytes" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(png)
			return
		}
		http.Redirect(w, r, srv.URL+"/media/x.png", http.StatusFound)
	})
	mux.HandleFunc("/media/x.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(png)
	})
	srv = httptest.NewServer(mux)
	return srv
}

func shrinkYoukouPollTempo(t *testing.T) {
	t.Helper()
	oldInterval, oldTimeout := youkouImagePollInterval, youkouImageMaxPollTimeout
	youkouImagePollInterval = 5 * time.Millisecond
	youkouImageMaxPollTimeout = 280 * time.Second
	t.Cleanup(func() {
		youkouImagePollInterval = oldInterval
		youkouImageMaxPollTimeout = oldTimeout
	})
}

func youkouFlowCtx() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"gpt-image-2","prompt":"a cat"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

// Poll-to-complete must yield a standard OpenAI images response whose
// data[0].url is the redirect target, plus one-call billing usage.
func TestYoukouImageTaskFlowPollToComplete(t *testing.T) {
	shrinkYoukouPollTempo(t)
	var polls atomic.Int64
	srv := fakeYoukouImageServer(t, &polls, "")
	defer srv.Close()

	c, w := youkouFlowCtx()
	info := youkouImageTestInfo(srv.URL)
	submitResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"object":"generation.task","status":"queued","id":"img1","status_url":"/v1/tasks/img1"}`)),
	}
	usage, apiErr := doYoukouImageTaskFlow(c, info, submitResp, "img1", "/v1/tasks/img1")
	require.Nil(t, apiErr)
	require.GreaterOrEqual(t, polls.Load(), int64(2))

	u, ok := usage.(*dto.Usage)
	require.True(t, ok, "usage must be *dto.Usage, got %T", usage)
	require.Equal(t, 1, u.PromptTokens)
	require.Equal(t, 1, u.TotalTokens)

	var imgResp dto.ImageResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &imgResp))
	require.Len(t, imgResp.Data, 1)
	require.Equal(t, srv.URL+"/media/x.png", imgResp.Data[0].Url)
	require.Empty(t, imgResp.Data[0].B64Json)
	require.NotZero(t, imgResp.Created)
}

// response_format=b64_json must download the bytes and return b64_json.
func TestYoukouImageTaskFlowB64(t *testing.T) {
	shrinkYoukouPollTempo(t)
	var polls atomic.Int64
	srv := fakeYoukouImageServer(t, &polls, "inline-bytes")
	defer srv.Close()

	c, w := youkouFlowCtx()
	info := youkouImageTestInfo(srv.URL)
	info.Request.(*dto.ImageRequest).ResponseFormat = "b64_json"
	submitResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"object":"generation.task","status":"queued","id":"img1","status_url":"/v1/tasks/img1"}`)),
	}
	usage, apiErr := doYoukouImageTaskFlow(c, info, submitResp, "img1", "/v1/tasks/img1")
	require.Nil(t, apiErr)
	require.NotNil(t, usage)

	var imgResp dto.ImageResponse
	require.NoError(t, common.Unmarshal(w.Body.Bytes(), &imgResp))
	require.Len(t, imgResp.Data, 1)
	require.Empty(t, imgResp.Data[0].Url)
	decoded, err := base64.StdEncoding.DecodeString(imgResp.Data[0].B64Json)
	require.NoError(t, err)
	require.Equal(t, []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x01, 0x02}, decoded)
}

// A task that never completes must fail with a 504-style timeout, not hang.
func TestYoukouImageTaskFlowTimeout(t *testing.T) {
	shrinkYoukouPollTempo(t)
	youkouImageMaxPollTimeout = 60 * time.Millisecond
	var polls atomic.Int64
	srv := fakeYoukouImageServer(t, &polls, "queued-forever")
	defer srv.Close()

	c, _ := youkouFlowCtx()
	info := youkouImageTestInfo(srv.URL)
	submitResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"object":"generation.task","status":"queued","id":"img1","status_url":"/v1/tasks/img1"}`)),
	}
	_, apiErr := doYoukouImageTaskFlow(c, info, submitResp, "img1", "/v1/tasks/img1")
	require.NotNil(t, apiErr)
	require.Equal(t, http.StatusGatewayTimeout, apiErr.StatusCode)
}

// A terminal upstream failure must surface immediately as an error.
func TestYoukouImageTaskFlowTerminalFailure(t *testing.T) {
	shrinkYoukouPollTempo(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"img1","status":"failed","message":"prompt rejected"}`))
	}))
	defer srv.Close()

	c, _ := youkouFlowCtx()
	info := youkouImageTestInfo(srv.URL)
	submitResp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"object":"generation.task","status":"queued","id":"img1","status_url":"/v1/tasks/img1"}`)),
	}
	_, apiErr := doYoukouImageTaskFlow(c, info, submitResp, "img1", "/v1/tasks/img1")
	require.NotNil(t, apiErr)
	assert.Contains(t, apiErr.Error(), "prompt rejected")
}

// Per-call billing keys ModelPrice by OriginModelName (gpt-image-2/2.5,
// quota_type=1). The flow must not hardcode prices: it looks them up from the
// configured price map, so this test pins the lookup contract.
func TestYoukouImageBillingPriceLookup(t *testing.T) {
	previous, err := common.Marshal(ratio_setting.GetModelPriceMap())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(string(previous))) })

	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(
		`{"gpt-image-2":0.01780821917808219,"gpt-image-2.5":0.02054794520547945}`))

	price2, ok := ratio_setting.GetModelPrice("gpt-image-2", false)
	require.True(t, ok)
	require.InEpsilon(t, 0.01780821917808219, price2, 1e-12)

	price25, ok := ratio_setting.GetModelPrice("gpt-image-2.5", false)
	require.True(t, ok)
	require.InEpsilon(t, 0.02054794520547945, price25, 1e-12)

	// One image call consumes ModelPrice*QuotaPerUnit quota units at group
	// ratio 1 (the UsePrice path in calculateTextQuotaSummary).
	require.Equal(t, int(price2*common.QuotaPerUnit), int(0.01780821917808219*common.QuotaPerUnit))
	require.Equal(t, 8904, int(price2*common.QuotaPerUnit))
	require.Equal(t, 10273, int(price25*common.QuotaPerUnit))

	// Peek must not disturb normal OpenAI image responses: the body is
	// restored byte-for-byte for the standard handler.
	normal := []byte(`{"created":1700000000,"data":[{"url":"https://example/x.png"}]}`)
	resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(normal))}
	_, _, ok = peekYoukouImageTask(resp)
	require.False(t, ok)
	restored, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, normal, restored)
}
