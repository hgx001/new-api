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

// 最小合法请求：仅 model + prompt，其余走缺省（时长 30、比例 16:9、自动幂等键）。
func TestValidateAcceptsMinimalRequest(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"dola-seedance-2.5","prompt":"一只猫在屋顶奔跑"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, constant.TaskActionGenerate, info.Action)

	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "一只猫在屋顶奔跑", req.Prompt)
	assert.Equal(t, 30, req.Duration)
	assert.Equal(t, "16:9", req.Ratio)
	assert.Empty(t, req.Images)
	assert.True(t, strings.HasPrefix(req.IdempotencyKey, "manwu-"))
}

// 字段别名与宽容解析：seconds 字符串形态、size 首选、images 兜底、自带幂等键保留。
func TestValidateResolvesAliases(t *testing.T) {
	c, info, a := postVideoCtx(t, `{
		"model":"dola-seedance-2.5","prompt":"hi",
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
		"model":"dola-seedance-2.5","prompt":"hi",
		"duration":10,"size":"1:1","ratio":"9:16",
		"input_reference":["https://a.example/1.png"],"images":["https://b.example/2.png"]}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	req2, err := getNormalizedRequest(c2)
	require.NoError(t, err)
	assert.Equal(t, 10, req2.Duration)
	assert.Equal(t, "1:1", req2.Ratio)
	assert.Equal(t, []string{"https://a.example/1.png"}, req2.Images)
}

// 非法组合必须在本地拦截，错误码指明原因（上游 ArcReel 对非法时长会静默回落 30，
// 厂商侧必须显式拒绝，避免计费与实际生成时长脱节）。
func TestValidateRejectsIllegalRequests(t *testing.T) {
	threeImgs := `["https://a.example/1.png","https://a.example/2.png","https://a.example/3.png"]`
	cases := map[string]struct {
		body string
		code string
	}{
		"wrong model":        {`{"model":"kling-v1","prompt":"hi"}`, "invalid_model"},
		"empty model":        {`{"prompt":"hi"}`, "invalid_model"},
		"empty prompt":       {`{"model":"dola-seedance-2.5","prompt":"   "}`, "invalid_request"},
		"bad json":           {`{`, "invalid_request"},
		"seconds over list":  {`{"model":"dola-seedance-2.5","prompt":"hi","seconds":20}`, "invalid_duration"},
		"duration over list": {`{"model":"dola-seedance-2.5","prompt":"hi","duration":7}`, "invalid_duration"},
		"seconds malformed":  {`{"model":"dola-seedance-2.5","prompt":"hi","seconds":"abc"}`, "invalid_duration"},
		"seconds float":      {`{"model":"dola-seedance-2.5","prompt":"hi","seconds":10.5}`, "invalid_duration"},
		"ratio not allowed":  {`{"model":"dola-seedance-2.5","prompt":"hi","ratio":"16:10"}`, "invalid_ratio"},
		"too many images":    {`{"model":"dola-seedance-2.5","prompt":"hi","input_reference":` + threeImgs + `}`, "invalid_input_reference"},
		"non http url":       {`{"model":"dola-seedance-2.5","prompt":"hi","input_reference":["ftp://a.example/x.png"]}`, "invalid_input_reference"},
		"url without host":   {`{"model":"dola-seedance-2.5","prompt":"hi","input_reference":["not-a-url"]}`, "invalid_input_reference"},
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
		"model":"dola-seedance-2.5","prompt":"hi","seconds":10,"size":"1:1",
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

// 无参考图时 inputs 必须整体省略（omitempty），时长缺省时显式下发 30（与上游兜底口径一致）。
func TestBuildRequestBodyOmitsEmptyInputs(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"dola-seedance-2.5","prompt":"hi"}`)
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

// EstimateBilling 返回 seconds 倍率（ModelPrice × seconds 计费）；未校验的 context 返回 nil。
func TestEstimateBilling(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"dola-seedance-2.5","prompt":"hi","seconds":15}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	assert.Equal(t, map[string]float64{"seconds": 15}, a.EstimateBilling(c, info))

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
	assert.Equal(t, []string{ModelName}, a.GetModelList())
	assert.Equal(t, ChannelName, a.GetChannelName())
}
