package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixedModelInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    constant.ChannelTypeManwu,
			ChannelBaseUrl: "https://arcreel.example.com",
			ApiKey:         "sk-manwu-test",
		},
		// PublicTaskID 经由嵌入的 *TaskRelayInfo 提升，nil 会导致 panic，
		// 生产环境由框架初始化，测试夹具必须显式补上。
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public001"},
		OriginModelName: ModelName,
	}
}

func postVideoCtx(t *testing.T, body string) (*gin.Context, *relaycommon.RelayInfo, *TaskAdaptor) {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := fixedModelInfo()
	a := &TaskAdaptor{}
	a.Init(info)
	return c, info, a
}

// Init 必须从渠道元信息装配 baseURL/apiKey，未配置 BaseURL 时兜底 ArcReel 生产地址。
func TestInitFillsChannelMeta(t *testing.T) {
	a := &TaskAdaptor{}
	a.Init(fixedModelInfo())
	assert.Equal(t, constant.ChannelTypeManwu, a.ChannelType)
	assert.Equal(t, "https://arcreel.example.com", a.baseURL)
	assert.Equal(t, "sk-manwu-test", a.apiKey)

	fallback := &TaskAdaptor{}
	fallback.Init(&relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeManwu, ApiKey: "k"},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
	})
	assert.Equal(t, baseURLDefault, fallback.baseURL)

	// 查询/轮询路径拿到的 adaptor 可能未经过 Init，不得 panic。
	noInit := &TaskAdaptor{}
	noInit.Init(nil)
	assert.Empty(t, noInit.baseURL)
}

// 最小合法请求：仅 model + prompt，其余走缺省（时长 15、比例 16:9、自动幂等键）。
func TestValidateAcceptsMinimalRequest(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.0","prompt":"一只猫在屋顶奔跑"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, constant.TaskActionGenerate, info.Action)

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "一只猫在屋顶奔跑", req.Prompt)
	assert.Equal(t, 15, req.Duration)
	assert.Equal(t, "16:9", req.Ratio)
	assert.Empty(t, req.Images)
	assert.True(t, strings.HasPrefix(req.IdempotencyKey, "manwu-"))
}

// Seedance 2.5 是独立公开模型：缺省时长固定为 30 秒，分辨率固定为 720p。
func TestValidateAcceptsSeedance25FixedSpec(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.5","prompt":"hi","resolution":"720P"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "seedance-2.5", req.Model)
	assert.Equal(t, 30, req.Duration)
	assert.Equal(t, "16:9", req.Ratio)
}

func TestValidateRejectsSeedance25VariableSpec(t *testing.T) {
	cases := map[string]struct {
		body string
		code string
	}{
		"duration is not fixed":   {`{"model":"seedance-2.5","prompt":"hi","duration":15}`, "invalid_duration"},
		"seconds is not fixed":    {`{"model":"seedance-2.5","prompt":"hi","seconds":"10"}`, "invalid_duration"},
		"resolution is not fixed": {`{"model":"seedance-2.5","prompt":"hi","resolution":"1080p"}`, "invalid_request"},
	}
	for name, tc := range cases {
		c, info, a := postVideoCtx(t, tc.body)
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, name)
		assert.Equal(t, tc.code, taskErr.Code, name)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode, name)
	}
}

// 字段别名与宽容解析：seconds 字符串形态、size 首选、images 兜底、自带幂等键保留。
func TestValidateResolvesAliases(t *testing.T) {
	c, info, a := postVideoCtx(t, `{
		"model":"seedance-2.0","prompt":"hi",
		"seconds":"15","aspect_ratio":"9:16",
		"images":["https://a.example/1.png"],
		"idempotency_key":"my-key"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 15, req.Duration)
	assert.Equal(t, "9:16", req.Ratio)
	assert.Equal(t, []string{"https://a.example/1.png"}, req.Images)
	assert.Equal(t, "my-key", req.IdempotencyKey)

	// size 优先于 ratio；input_reference 优先于 images；duration 数字形态。
	c2, info2, a2 := postVideoCtx(t, `{
		"model":"seedance-2.0","prompt":"hi",
		"duration":10,"size":"1:1","ratio":"9:16",
		"input_reference":["https://a.example/1.png"],"images":["https://b.example/2.png"]}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	req2, err := getNormalizedRequest(c2)
	require.NoError(t, err)
	assert.Equal(t, 10, req2.Duration)
	assert.Equal(t, "1:1", req2.Ratio)
	assert.Equal(t, []string{"https://a.example/1.png"}, req2.Images)
}

// dola 官网真机验证支持 10 张参考图，中转层按官网口径放行（11 张起拒绝）。
func TestValidateAcceptsTenReferenceImages(t *testing.T) {
	imgs := `["https://a.example/1.png","https://a.example/2.png","https://a.example/3.png","https://a.example/4.png","https://a.example/5.png","https://a.example/6.png","https://a.example/7.png","https://a.example/8.png","https://a.example/9.png","https://a.example/10.png"]`
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.0","prompt":"hi","input_reference":`+imgs+`}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	require.Len(t, req.Images, 10)
}

// 非法组合必须在本地拦截，错误码指明原因（上游 ArcReel 对非法时长会静默回落 15，
// 厂商侧必须显式拒绝，避免计费与实际生成时长脱节）。
func TestValidateRejectsIllegalRequests(t *testing.T) {
	elevenImgs := `["https://a.example/1.png","https://a.example/2.png","https://a.example/3.png","https://a.example/4.png","https://a.example/5.png","https://a.example/6.png","https://a.example/7.png","https://a.example/8.png","https://a.example/9.png","https://a.example/10.png","https://a.example/11.png"]`
	cases := map[string]struct {
		body string
		code string
	}{
		"wrong model":        {`{"model":"kling-v1","prompt":"hi"}`, "invalid_model"},
		"empty model":        {`{"prompt":"hi"}`, "invalid_model"},
		"empty prompt":       {`{"model":"seedance-2.0","prompt":"   "}`, "invalid_request"},
		"bad json":           {`{`, "invalid_request"},
		"seconds over list":  {`{"model":"seedance-2.0","prompt":"hi","seconds":20}`, "invalid_duration"},
		"duration over list": {`{"model":"seedance-2.0","prompt":"hi","duration":7}`, "invalid_duration"},
		"seconds malformed":  {`{"model":"seedance-2.0","prompt":"hi","seconds":"abc"}`, "invalid_duration"},
		"seconds float":      {`{"model":"seedance-2.0","prompt":"hi","seconds":10.5}`, "invalid_duration"},
		"ratio not allowed":  {`{"model":"seedance-2.0","prompt":"hi","ratio":"16:10"}`, "invalid_ratio"},
		"too many images":    {`{"model":"seedance-2.0","prompt":"hi","input_reference":` + elevenImgs + `}`, "invalid_input_reference"},
		"non http url":       {`{"model":"seedance-2.0","prompt":"hi","input_reference":["ftp://a.example/x.png"]}`, "invalid_input_reference"},
		"url without host":   {`{"model":"seedance-2.0","prompt":"hi","input_reference":["not-a-url"]}`, "invalid_input_reference"},
	}
	for name, tc := range cases {
		c, info, a := postVideoCtx(t, tc.body)
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, name)
		assert.Equal(t, tc.code, taskErr.Code, name)
		assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode, name)
	}
}

// BuildRequestBody 必须映射成 ArcReel 建单载荷：platformId/outputMode/videoParams/inputs/idempotencyKey。
func TestBuildRequestBodyMapsArcReelPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{
		"model":"seedance-2.0","prompt":"hi","seconds":10,"size":"1:1",
		"input_reference":["https://a.example/1.png","https://a.example/2.png"],
		"idempotency_key":"key-1"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	assert.Equal(t, PlatformID, payload["platformId"])
	assert.Equal(t, OutputModeVideo, payload["outputMode"])
	videoPayload, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "seedance-2.0", videoPayload["model"])
	assert.Equal(t, "hi", payload["prompt"])
	assert.Equal(t, "key-1", payload["idempotencyKey"])

	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1:1", vp["ratio"])
	assert.Equal(t, float64(10), vp["duration"])

	inputs, ok := payload["inputs"].([]any)
	require.True(t, ok)
	require.Len(t, inputs, 2)
	first, ok := inputs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, InputTypeImage, first["type"])
	assert.Equal(t, "https://a.example/1.png", first["url"])
}

func TestBuildRequestBodyMapsSeedance25FixedPayload(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.5","prompt":"hi","duration":30}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	videoPayload, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "seedance-2.5", videoPayload["model"])
	assert.Equal(t, float64(30), videoPayload["duration"])
	assert.Equal(t, "720p", videoPayload["resolution"])
}

// 无参考图时 inputs 必须整体省略（omitempty），时长缺省时显式下发 15（与上游兜底口径一致）。
func TestBuildRequestBodyOmitsEmptyInputs(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.0","prompt":"hi"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	_, hasInputs := payload["inputs"]
	assert.False(t, hasInputs)

	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(defaultDuration), vp["duration"])
	assert.Equal(t, defaultRatio, vp["ratio"])
}

// EstimateBilling 必须返回 nil：seedance-2.0 按次计费（¥1.5/次），
// 带 seconds 倍率也不改变固定价格。
func TestEstimateBillingIsPerCall(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance-2.0","prompt":"hi","seconds":15}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Nil(t, a.EstimateBilling(c, info))
	assert.Nil(t, a.EstimateBilling(nil, nil))

	emptyC, _ := gin.CreateTestContext(httptest.NewRecorder())
	assert.Nil(t, a.EstimateBilling(emptyC, info))
}

// ParseTaskResult 状态映射全分支（状态大小写不敏感）。
func TestParseTaskResultStatusMapping(t *testing.T) {
	a := &TaskAdaptor{}

	queued, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"queued"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusQueued, queued.Status)
	assert.Equal(t, taskcommon.ProgressQueued, queued.Progress)

	inProgressStates := []string{
		"assigned", "accepted", "submitting",
		"provider_running", "provider_succeeded", "url_validating",
	}
	for _, state := range inProgressStates {
		res, err := a.ParseTaskResult([]byte(fmt.Sprintf(`{"jobId":"job-1","status":%q}`, state)))
		require.NoError(t, err, state)
		assert.Equal(t, model.TaskStatusInProgress, res.Status, state)
		assert.Equal(t, taskcommon.ProgressInProgress, res.Progress, state)
	}

	ready, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","sourceUrl":"https://cdn.example/v.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, ready.Status)
	assert.Equal(t, "https://cdn.example/v.mp4", ready.Url)

	// source_url 别名 + 状态大小写不敏感。
	readyAlt, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"READY","source_url":"https://cdn.example/alt.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, readyAlt.Status)
	assert.Equal(t, "https://cdn.example/alt.mp4", readyAlt.Url)

	failed, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"failed","error":"生成超时"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, failed.Status)
	assert.Equal(t, "生成超时", failed.Reason)

	unavailable, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"url_unavailable"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, unavailable.Status)
	assert.Equal(t, reasonOutputUnavailable, unavailable.Reason)

	cancelled, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"cancelled","error":"用户取消"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, cancelled.Status)
	assert.Equal(t, reasonCancelled, cancelled.Reason)

	for _, state := range []string{"unknown", "some_future_state", ""} {
		res, err := a.ParseTaskResult([]byte(fmt.Sprintf(`{"jobId":"job-1","status":%q}`, state)))
		require.NoError(t, err, state)
		assert.Equal(t, model.TaskStatusUnknown, res.Status, state)
	}

	_, err = a.ParseTaskResult([]byte(`not json`))
	require.Error(t, err)
}

// DoResponse：200 响应宽容解析 jobId / id，回显 PublicTaskID，异常响应报明确错误。
func TestDoResponse(t *testing.T) {
	newCtx := func() (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		return c, w
	}
	doRequest := func(t *testing.T, handler http.HandlerFunc) *http.Response {
		t.Helper()
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)
		resp, err := http.Get(srv.URL)
		require.NoError(t, err)
		return resp
	}

	// jobId 字段。
	c, w := newCtx()
	resp := doRequest(t, func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		fmt.Fprint(rw, `{"jobId":"job-abc","status":"queued"}`)
	})
	a := &TaskAdaptor{}
	taskID, data, taskErr := a.DoResponse(c, resp, fixedModelInfo())
	require.Nil(t, taskErr)
	_ = resp.Body.Close()
	assert.Equal(t, "job-abc", taskID)
	assert.Contains(t, string(data), "job-abc")
	assert.Equal(t, http.StatusOK, w.Code)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ov))
	assert.Equal(t, "task_public001", ov["id"])
	assert.Equal(t, "task_public001", ov["task_id"])
	assert.Equal(t, ModelName, ov["model"])

	// id 别名字段。
	c2, _ := newCtx()
	resp2 := doRequest(t, func(rw http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(rw, `{"id":"job-xyz"}`)
	})
	taskID2, _, taskErr2 := a.DoResponse(c2, resp2, fixedModelInfo())
	require.Nil(t, taskErr2)
	_ = resp2.Body.Close()
	assert.Equal(t, "job-xyz", taskID2)

	// 缺失任务 ID。
	c3, _ := newCtx()
	resp3 := doRequest(t, func(rw http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(rw, `{"status":"queued"}`)
	})
	_, _, taskErr3 := a.DoResponse(c3, resp3, fixedModelInfo())
	require.NotNil(t, taskErr3)
	_ = resp3.Body.Close()
	assert.Equal(t, "invalid_response", taskErr3.Code)

	// 非 JSON 响应。
	c4, _ := newCtx()
	resp4 := doRequest(t, func(rw http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(rw, `not json`)
	})
	_, _, taskErr4 := a.DoResponse(c4, resp4, fixedModelInfo())
	require.NotNil(t, taskErr4)
	_ = resp4.Body.Close()
	assert.Equal(t, "unmarshal_response_body_failed", taskErr4.Code)
}

// FetchTask：GET {base}/api/v1/remote-generation/jobs/{id}，Bearer 鉴权。
func TestFetchTask(t *testing.T) {
	// FetchTask 经 service.GetHttpClientWithProxy 取全局 client，
	// 该 client 由 main 启动时的 InitHttpClient 初始化，测试环境必须显式补上。
	service.InitHttpClient()

	var gotPath, gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotAccept = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jobId":"job-123","status":"queued"}`)
	}))
	defer srv.Close()

	a := &TaskAdaptor{}
	resp, err := a.FetchTask(srv.URL, "sk-poll", map[string]any{"task_id": "job-123"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/remote-generation/jobs/job-123", gotPath)
	assert.Equal(t, "Bearer sk-poll", gotAuth)
	assert.Equal(t, "application/json", gotAccept)
	assert.Contains(t, string(body), "queued")

	// 缺失 task_id 必须报错，而不是请求一个无效 URL。
	_, err = a.FetchTask(srv.URL, "sk-poll", map[string]any{}, "")
	require.Error(t, err)

	// task_id 做路径转义，防止特殊字符拼接越界。
	_, err = a.FetchTask(srv.URL, "sk-poll", map[string]any{"task_id": "a b/c"}, "")
	require.NoError(t, err)
}

func TestModelListAndChannelName(t *testing.T) {
	a := &TaskAdaptor{}
	assert.Equal(t, []string{
		"Nano Banana Pro",
		"db-seedance-2-0",
		"db-seedance-2-5",
		"gemini-web-video",
		"jimeng-video-reverse",
		"seedance-2.0",
		"seedance-2.5",
	}, a.GetModelList())
	assert.Equal(t, ChannelName, a.GetChannelName())
}

// ── 多模型路由 ──

func TestSpecForRoutesEveryModel(t *testing.T) {
	dola, ok := SpecFor(ModelDola)
	require.True(t, ok)
	assert.Equal(t, PlatformID, dola.PlatformID)
	assert.Equal(t, OutputModeVideo, dola.OutputMode)
	assert.Equal(t, kindDolaVideo, dola.Kind)

	dola25, ok := SpecFor(ModelDola25)
	require.True(t, ok)
	assert.Equal(t, PlatformID, dola25.PlatformID)
	assert.Equal(t, OutputModeVideo, dola25.OutputMode)
	assert.Equal(t, kindDola25Video, dola25.Kind)

	// 豆包官网两个模型：与 dola 同为 Seedance 系但**独立平台**，必须路由到
	// doubao + doubao_video kind（2026-10-08 用户定稿：只放开 2.5 与 2.0 Fast）。
	doubao25, ok := SpecFor(ModelDoubao25)
	require.True(t, ok)
	assert.Equal(t, PlatformIDDoubao, doubao25.PlatformID)
	assert.Equal(t, OutputModeVideo, doubao25.OutputMode)
	assert.Equal(t, kindDoubaoVideo, doubao25.Kind)

	doubaoFast, ok := SpecFor(ModelDoubao20Fast)
	require.True(t, ok)
	assert.Equal(t, PlatformIDDoubao, doubaoFast.PlatformID)
	assert.Equal(t, OutputModeVideo, doubaoFast.OutputMode)
	assert.Equal(t, kindDoubaoVideo, doubaoFast.Kind)

	veo, ok := SpecFor(ModelVideo)
	require.True(t, ok)
	assert.Equal(t, PlatformIDGemini, veo.PlatformID)
	assert.Equal(t, OutputModeVideo, veo.OutputMode)
	assert.Equal(t, kindVeoVideo, veo.Kind)

	image, ok := SpecFor(ModelImage)
	require.True(t, ok)
	assert.Equal(t, PlatformIDImage, image.PlatformID)
	assert.Equal(t, OutputModeImage, image.OutputMode)
	assert.Equal(t, kindImage, image.Kind)

	reverse, ok := SpecFor(ModelReverse)
	require.True(t, ok)
	assert.Equal(t, PlatformIDJimeng, reverse.PlatformID)
	assert.Equal(t, OutputModeText, reverse.OutputMode)
	assert.Equal(t, kindReverse, reverse.Kind)

	// 未知模型必须 miss（不能默默落到 dola）。
	_, ok = SpecFor("veo-3.1-generate-preview")
	assert.False(t, ok, "官方 Gemini 模型名不得被本渠道误接")
	_, ok = SpecFor("")
	assert.False(t, ok)
}

// ── Nano Banana Pro ──

func TestValidateImageAcceptsMinimal(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"一只柴犬"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, kindImage, req.Kind)
	assert.Equal(t, defaultImageCount, req.Count, "缺省出 1 张")
	assert.Equal(t, defaultRatio, req.Ratio)

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	assert.Equal(t, PlatformIDImage, payload["platformId"])
	assert.Equal(t, OutputModeImage, payload["outputMode"])

	ip, ok := payload["imageParams"].(map[string]any)
	require.True(t, ok, "图片任务必须走 imageParams")
	assert.Equal(t, float64(1), ip["count"])
	assert.Equal(t, defaultRatio, ip["aspectRatio"])
	_, hasVideoParams := payload["videoParams"]
	assert.False(t, hasVideoParams, "图片任务不得带 videoParams")
}

// 图片按张计费：n/count 都认，张数作为 n 倍率乘到 ¥0.3/张。
func TestImageCountDrivesBillingMultiplier(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"猫","n":3}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, map[string]float64{"n": 3}, a.EstimateBilling(c, info))

	c2, info2, a2 := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"猫","count":"4","size":"9:16"}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	assert.Equal(t, map[string]float64{"n": 4}, a2.EstimateBilling(c2, info2))

	req, err := getNormalizedRequest(c2)
	require.NoError(t, err)
	assert.Equal(t, 4, req.Count)
	assert.Equal(t, "9:16", req.Ratio)

	// 张数为 1 时不注入倍率（避免无意义的 quota 乘 1 误差）。
	c3, info3, a3 := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"猫","n":1}`)
	require.Nil(t, a3.ValidateRequestAndSetAction(c3, info3))
	assert.Nil(t, a3.EstimateBilling(c3, info3))
}

// 图片参考图：支持 URL 列表，上限 10 张（与 dola 同上限）。
func TestImageRejectsIllegalRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{"missing prompt", `{"model":"Nano Banana Pro"}`, "invalid_request"},
		{"count above cap", `{"model":"Nano Banana Pro","prompt":"x","n":11}`, "invalid_count"},
		{"count below one", `{"model":"Nano Banana Pro","prompt":"x","n":0}`, "invalid_count"},
		{"non integer count", `{"model":"Nano Banana Pro","prompt":"x","n":"many"}`, "invalid_count"},
		{"bad ratio", `{"model":"Nano Banana Pro","prompt":"x","ratio":"21:10"}`, "invalid_ratio"},
		{"data uri reference", `{"model":"Nano Banana Pro","prompt":"x","input_reference":"data:image/png;base64,AAAA"}`, "invalid_input_reference"},
		{"video seconds on image", `{"model":"Nano Banana Pro","prompt":"x","seconds":5}`, "invalid_request"},
		{"resolution on image", `{"model":"Nano Banana Pro","prompt":"x","resolution":"2k"}`, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, info, a := postVideoCtx(t, tc.body)
			taskErr := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr, tc.name)
			assert.Equal(t, tc.code, taskErr.Code)
		})
	}

	// 10 张放行 / 11 张拒绝。
	okCtx, okInfo, okAdaptor := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"x","input_reference":[
		"https://a.example/1.png","https://a.example/2.png","https://a.example/3.png","https://a.example/4.png","https://a.example/5.png",
		"https://a.example/6.png","https://a.example/7.png","https://a.example/8.png","https://a.example/9.png","https://a.example/10.png"]}`)
	require.Nil(t, okAdaptor.ValidateRequestAndSetAction(okCtx, okInfo))

	overCtx, overInfo, overAdaptor := postVideoCtx(t, `{"model":"Nano Banana Pro","prompt":"x","images":[
		"https://a.example/1.png","https://a.example/2.png","https://a.example/3.png","https://a.example/4.png","https://a.example/5.png","https://a.example/6.png",
		"https://a.example/7.png","https://a.example/8.png","https://a.example/9.png","https://a.example/10.png","https://a.example/11.png"]}`)
	taskErr := overAdaptor.ValidateRequestAndSetAction(overCtx, overInfo)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_input_reference", taskErr.Code)
}

// ── gemini-web-video ──

func TestVeoVideoRejectsUnsupportedParams(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"gemini-web-video","prompt":"海浪","ratio":"9:16"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, kindVeoVideo, req.Kind)
	assert.Equal(t, "9:16", req.Ratio)
	assert.Nil(t, a.EstimateBilling(c, info), "Veo 按次计费，不乘任何倍率")

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	assert.Equal(t, PlatformIDGemini, payload["platformId"])
	assert.Equal(t, OutputModeVideo, payload["outputMode"])

	// 官网无时长/分辨率/模型控件：Worker 会 fail-fast，这里提前 400。
	cases := []struct {
		name string
		body string
		code string
	}{
		{"seconds", `{"model":"gemini-web-video","prompt":"x","seconds":8}`, "invalid_request"},
		{"duration", `{"model":"gemini-web-video","prompt":"x","duration":5}`, "invalid_request"},
		{"resolution", `{"model":"gemini-web-video","prompt":"x","resolution":"1080p"}`, "invalid_request"},
		{"missing prompt", `{"model":"gemini-web-video"}`, "invalid_request"},
		{"bad ratio", `{"model":"gemini-web-video","prompt":"x","size":"4:2"}`, "invalid_ratio"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, info, a := postVideoCtx(t, tc.body)
			taskErr := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr, tc.name)
			assert.Equal(t, tc.code, taskErr.Code)
		})
	}
}

// ── jimeng-video-reverse ──

func TestReverseBuildsPromptModeJob(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"jimeng-video-reverse","input_reference":"https://cdn.example/clip.mp4"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	assert.Equal(t, PlatformIDJimeng, payload["platformId"])
	assert.Equal(t, OutputModeText, payload["outputMode"])

	_, hasVideoParams := payload["videoParams"]
	assert.False(t, hasVideoParams, "反解不得带 videoParams")
	_, hasImageParams := payload["imageParams"]
	assert.False(t, hasImageParams, "反解不得带 imageParams")

	inputs, ok := payload["inputs"].([]any)
	require.True(t, ok)
	require.Len(t, inputs, 1, "ArcReel 要求反解恰好 1 个参考视频")
	first, ok := inputs[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, InputTypeVideo, first["type"])
	assert.Equal(t, "https://cdn.example/clip.mp4", first["url"])
}

// 反解的 prompt 可选（ArcReel 建单允许空 prompt），带 prompt 则当附加指令透传。
func TestReversePromptIsOptional(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"jimeng-video-reverse","input_reference":"https://cdn.example/c.mp4","prompt":"突出镜头运动"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	assert.Equal(t, "突出镜头运动", decodePayload(t, reader)["prompt"])
}

func TestReverseRejectsIllegalRequests(t *testing.T) {
	cases := []struct {
		name string
		body string
		code string
	}{
		{"missing video", `{"model":"jimeng-video-reverse"}`, "invalid_input_reference"},
		{"two videos", `{"model":"jimeng-video-reverse","videos":["https://a.example/1.mp4","https://a.example/2.mp4"]}`, "invalid_input_reference"},
		{"relative path video", `{"model":"jimeng-video-reverse","input_reference":"/tmp/clip.mp4"}`, "invalid_input_reference"},
		{"data uri video", `{"model":"jimeng-video-reverse","video":"data:video/mp4;base64,AAAA"}`, "invalid_input_reference"},
		{"ratio", `{"model":"jimeng-video-reverse","video":"https://a.example/1.mp4","ratio":"16:9"}`, "invalid_request"},
		{"seconds", `{"model":"jimeng-video-reverse","video":"https://a.example/1.mp4","seconds":10}`, "invalid_request"},
		{"images instead of video", `{"model":"jimeng-video-reverse","images":["https://a.example/1.png"]}`, "invalid_request"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, info, a := postVideoCtx(t, tc.body)
			taskErr := a.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr, tc.name)
			assert.Equal(t, tc.code, taskErr.Code)
		})
	}
}

// 反解传 multipart 文件直传时要给可读原因，而不是 JSON 语法错误。
func TestReverseRejectsMultipartUpload(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("whatever"))
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")

	info := fixedModelInfo()
	info.OriginModelName = ModelReverse
	a := &TaskAdaptor{}
	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_input_reference", taskErr.Code)
}

// ── 结果收敛 ──

// ready 时的文本结果（视频反解）落入 TaskInfo.ResultText，不占用 Url。
func TestParseTaskResultSurfacesTextResult(t *testing.T) {
	a := &TaskAdaptor{}
	res, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","sourceUrl":null,"resultPrompt":"一只猫在屋顶奔跑，逆光"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, res.Status)
	assert.Equal(t, "一只猫在屋顶奔跑，逆光", res.ResultText)
	assert.Empty(t, res.Url)

	// result_prompt 兼容字段。
	alt, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","result_prompt":"alt text"}`))
	require.NoError(t, err)
	assert.Equal(t, "alt text", alt.ResultText)

	// ready 但既无 URL 也无文本 → 失败（触发退款），不能算成功交付。
	empty, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, empty.Status)
	assert.Equal(t, reasonEmptyResult, empty.Reason)
}

// ArcReel 托管产物是相对路径（需渠道密钥），不得当作可直交付 URL 透传出去。
func TestParseTaskResultHidesSelfHostedRelativeURL(t *testing.T) {
	a := &TaskAdaptor{}
	res, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","sourceUrl":"/api/v1/remote-generation/jobs/job-1/outputs/output-a.png"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, res.Status)
	assert.Empty(t, res.Url, "相对路径必须留给 content 代理，不能透传给客户端")

	// 公网 CDN 直链照旧透传（dola 既有行为不变）。
	direct, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","sourceUrl":"https://v19-dola.dola.com/v/clip.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, "https://v19-dola.dola.com/v/clip.mp4", direct.Url)
}

// ⚠️ 2026-10-08 生产回归(langdu gemini-web-video): ArcReel/Worker 把 sourceUrl
// 拼成了**绝对** URL 上报, 旧逻辑只判 http 前缀就直传 → 客户端拿到的链接没有
// 渠道密钥, 401 打不开, content 代理同样透传后 502。自有托管路径无论相对还是
// 绝对都必须留给 content 代理回源。
func TestParseTaskResultHidesSelfHostedAbsoluteURL(t *testing.T) {
	a := &TaskAdaptor{}
	res, err := a.ParseTaskResult([]byte(`{"jobId":"job-1","status":"ready","sourceUrl":"https://arcreel.heibaidao.cn/api/v1/remote-generation/jobs/gen-abc/outputs/output-3f381fa.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, res.Status)
	assert.Empty(t, res.Url, "绝对形式的自有托管 URL 同样不能透传给客户端")

	// 判据本身: 路径形状识别, 与 host 无关
	assert.True(t, IsOwnedAssetPath("/api/v1/remote-generation/jobs/job-1/outputs/o.png"))
	assert.True(t, IsOwnedAssetPath("https://arcreel.heibaidao.cn/api/v1/remote-generation/jobs/gen-abc/outputs/o.mp4"))
	assert.True(t, IsOwnedAssetPath("http://localhost:8200/api/v1/remote-generation/jobs/job-1/outputs/o.png"))
	assert.False(t, IsOwnedAssetPath("https://v19-dola.dola.com/v/clip.mp4"))
	assert.False(t, IsOwnedAssetPath("https://arcreel.heibaidao.cn/api/v1/other/thing.png"))
	assert.False(t, IsOwnedAssetPath(""))
}

// ConvertToOpenAIVideo：文本结果进 metadata.prompt，图片/视频进 metadata.url + media_type。
func TestConvertToOpenAIVideoExposesResults(t *testing.T) {
	a := &TaskAdaptor{}

	textTask := &model.Task{
		TaskID:     "task_public001",
		Progress:   "100%",
		Properties: model.Properties{OriginModelName: ModelReverse},
		Data:       []byte(`{"jobId":"job-1","status":"ready","resultPrompt":"镜头缓慢推近"}`),
	}
	raw, err := a.ConvertToOpenAIVideo(textTask)
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	assert.Equal(t, "completed", ov["status"])
	metadata, ok := ov["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "镜头缓慢推近", metadata["prompt"])
	assert.Equal(t, "text", metadata["media_type"])

	videoTask := &model.Task{
		TaskID:     "task_public002",
		Progress:   "100%",
		Properties: model.Properties{OriginModelName: ModelDola},
		Data:       []byte(`{"jobId":"job-2","status":"ready","sourceUrl":"https://v19-dola.dola.com/v/clip.mp4"}`),
	}
	rawVideo, err := a.ConvertToOpenAIVideo(videoTask)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawVideo, &ov))
	metadata = ov["metadata"].(map[string]any)
	assert.Equal(t, "https://v19-dola.dola.com/v/clip.mp4", metadata["url"])
	assert.Equal(t, "video", metadata["media_type"])

	imageTask := &model.Task{
		TaskID:     "task_public003",
		Progress:   "100%",
		Properties: model.Properties{OriginModelName: ModelImage},
		Data:       []byte(`{"jobId":"job-3","status":"ready","sourceUrl":"/api/v1/remote-generation/jobs/job-3/outputs/o.png"}`),
	}
	rawImage, err := a.ConvertToOpenAIVideo(imageTask)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(rawImage, &ov))
	metadata = ov["metadata"].(map[string]any)
	// 相对路径 → 回落到 content 代理地址，客户端不会拿到打不开的链接。
	assert.Contains(t, metadata["url"], "/v1/videos/task_public003/content")
}

func decodePayload(t *testing.T, reader io.Reader) map[string]any {
	t.Helper()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	return payload
}

// 拒绝原因里必须带**模型名**而不是占位符，否则用户看到 "prompt is fixed-duration
// web Veo" 这种无法定位的错误。
func TestRejectionMessagesNameTheModel(t *testing.T) {
	cases := []struct {
		body       string
		wantSubstr string
	}{
		{`{"model":"gemini-web-video","prompt":"x","seconds":8}`, "gemini-web-video"},
		{`{"model":"Nano Banana Pro","prompt":"x","resolution":"2k"}`, "Nano Banana Pro"},
		{`{"model":"jimeng-video-reverse","video":"https://a.example/1.mp4","ratio":"16:9"}`, "jimeng-video-reverse"},
	}
	for _, tc := range cases {
		c, info, a := postVideoCtx(t, tc.body)
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, tc.body)
		assert.Contains(t, taskErr.Message, tc.wantSubstr, tc.body)
	}
}

// Veo 不下发 duration：官网没有时长控件，写进载荷只会让人误以为时长被执行了。
func TestVeoPayloadOmitsDuration(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"gemini-web-video","prompt":"x","ratio":"1:1"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	vp, ok := decodePayload(t, reader)["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "1:1", vp["ratio"])
	_, hasDuration := vp["duration"]
	assert.False(t, hasDuration, "Veo 载荷不得含 duration")
}

// multipart 拒绝对四个模型都成立（ArcReel 只吃公网 URL），文案不能说成只讲反解。
func TestMultipartRejectionIsModelAgnostic(t *testing.T) {
	for _, model := range ModelList() {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("whatever"))
		c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")

		info := fixedModelInfo()
		info.OriginModelName = model
		a := &TaskAdaptor{}
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, model)
		assert.Equal(t, "invalid_input_reference", taskErr.Code, model)
		assert.Contains(t, taskErr.Message, "公网可访问", model)
		assert.NotContains(t, taskErr.Message, "视频反解暂不支持", model)
	}
}

// 反解的文本结果优先取落库的 PrivateData（权威副本），task.Data 缺字段也能返回。
func TestConvertToOpenAIVideoPrefersStoredResultText(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_text_only",
		Progress:   "100%",
		Properties: model.Properties{OriginModelName: ModelReverse},
		PrivateData: model.TaskPrivateData{
			ResultText: "落库的权威提示词",
		},
		// 快照里没有 resultPrompt（模拟脱敏/字段丢失）
		Data: []byte(`{"jobId":"job-1","status":"ready"}`),
	}
	raw, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var ov map[string]any
	require.NoError(t, json.Unmarshal(raw, &ov))
	assert.Equal(t, "completed", ov["status"])
	metadata, ok := ov["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "落库的权威提示词", metadata["prompt"])
	// 文本交付物不得给 url（否则客户端会去拉不存在的媒体文件）。
	_, hasURL := metadata["url"]
	assert.False(t, hasURL, "文本任务不得带 metadata.url")
}

// ── doubao（豆包官网，独立于 dola 的执行站点）──

func TestDoubaoVideoRoutesToDoubaoPlatform(t *testing.T) {
	// 公开名是短名 db-seedance-2-5，载荷里必须翻译成官网真名。
	c, info, a := postVideoCtx(t, `{"model":"db-seedance-2-5","prompt":"海底世界","seconds":30}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, kindDoubaoVideo, req.Kind)

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	assert.Equal(t, "doubao", payload["platformId"])
	assert.Equal(t, OutputModeVideo, payload["outputMode"])
	vp, _ := payload["videoParams"].(map[string]any)
	require.NotNil(t, vp)
	assert.Equal(t, "doubao-seedance-2-5-260628", vp["model"], "公开短名必须翻译成官网档位真名")
	assert.Equal(t, float64(30), vp["duration"], "豆包支持 30s 档（与 dola 2.0 的三档不同）")
}

func TestDoubaoRatioNotInventedWhenUnspecified(t *testing.T) {
	// 豆包「未指定不编造」：公共段 resolveRatio 会把空值填成 16:9（dola 口径），
	// 豆包必须覆盖回空 —— Worker 端保持官网当前状态（出厂「自动」档）。
	c, info, a := postVideoCtx(t, `{"model":"db-seedance-2-0","prompt":"x"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "", req.Ratio, "未指定比例时不得编造 16:9")

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	payload := decodePayload(t, reader)
	vp, _ := payload["videoParams"].(map[string]any)
	assert.Equal(t, "", vp["ratio"])

	// 缺省时长回落 5（与 Worker 侧 DOUBAO_DEFAULT_DURATION 一致）。
	assert.Equal(t, 5, req.Duration)
}

func TestDoubao20FastRejects30s(t *testing.T) {
	// 2.0 Fast 官网没有 30s 档（2026-10-08 用户定稿：只支持 5/10/15 秒），
	// 30s 组合必须在入口 400，而不是等 Worker setDoubaoDuration 诚实报错。
	for _, field := range []string{"seconds", "duration"} {
		c, info, a := postVideoCtx(t, fmt.Sprintf(`{"model":"db-seedance-2-0","prompt":"x","%s":30}`, field))
		taskErr := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr, field)
		assert.Equal(t, "invalid_duration", taskErr.Code)
	}
}

func TestDoubaoAcceptsAutoRatioAndRejectsIllegal(t *testing.T) {
	// auto 是豆包官网出厂档，必须接受。
	c, info, a := postVideoCtx(t, `{"model":"db-seedance-2-5","prompt":"x","ratio":"auto","seconds":10}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "auto", req.Ratio)

	cases := []struct {
		name string
		body string
		code string
	}{
		{"bad ratio", `{"model":"db-seedance-2-5","prompt":"x","ratio":"4:2"}`, "invalid_ratio"},
		{"bad seconds", `{"model":"db-seedance-2-5","prompt":"x","seconds":8}`, "invalid_duration"},
		{"bad duration", `{"model":"db-seedance-2-5","prompt":"x","duration":20}`, "invalid_duration"},
		{"resolution", `{"model":"db-seedance-2-5","prompt":"x","resolution":"1080p"}`, "invalid_request"},
		{"unknown model", `{"model":"doubao-seedance-2-0-260128","prompt":"x"}`, "invalid_model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			badCtx, badInfo, badAdaptor := postVideoCtx(t, tc.body)
			taskErr := badAdaptor.ValidateRequestAndSetAction(badCtx, badInfo)
			require.NotNil(t, taskErr)
			assert.Equal(t, tc.code, taskErr.Code)
		})
	}
}
