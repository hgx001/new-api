package youkou

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/megabyai"
	"github.com/QuantumNous/new-api/relay/channel/task/sora"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance2.5特惠版在 youkou 目录（catalog-v1-a4b7eaff2a16f6ee02e4209d，
// family hm-seedance-2-5-discount）中声明 pricing fixed/request（按次计费），
// 必须走按次计费的 openai-video 子适配器，禁止带入 sora 的 seconds/size
// 倍率，否则多秒任务会被扣到上百元。
func TestSelectSubRoutesSeedance25ToFixedBillingSub(t *testing.T) {
	a := &TaskAdaptor{}
	a.selectSub("seedance2.5特惠版")
	require.IsType(t, &megabyai.TaskAdaptor{}, a.sub)
	require.Nil(t, a.EstimateBilling(nil, nil))
}

// 回归守卫：h3 / wan3.0 继续走按秒计费的 sora 子适配器。
func TestSelectSubKeepsPerSecondModelsOnSoraSub(t *testing.T) {
	for _, m := range []string{
		"aliyun:wan-3.0",
		"hailuo-h3-cankaosheng-fast",
		"hailuo-h3-cankaosheng-night",
	} {
		a := &TaskAdaptor{}
		a.selectSub(m)
		require.IsType(t, &sora.TaskAdaptor{}, a.sub, m)
	}
}

func fixedModelInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    59,
			ChannelBaseUrl: "https://youkou.cc",
			ApiKey:         "test-key",
		},
		// Action/OriginTaskID 经由嵌入的 *TaskRelayInfo 提升，nil 会导致 panic，
		// 生产环境由框架初始化，测试夹具必须显式补上。
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "seedance2.5特惠版",
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
	require.IsType(t, &megabyai.TaskAdaptor{}, a.sub)
	return c, info, a
}

// 2.5 的 openai-video 契约：POST {base}/v1/videos（目录声明，非旧 /v1/video/generations）。
func TestFixedModelUsesOpenAIVideoContract(t *testing.T) {
	a := &TaskAdaptor{}
	info := fixedModelInfo()
	a.Init(info)
	url, err := a.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t, "https://youkou.cc/v1/videos", url)
}

// 目录最小合法请求必须通过。
func TestValidateFixedModelAcceptsMinimalRequest(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"seedance2.5特惠版","prompt":"一只纸飞机飞过城市上空","duration":4}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
}

// 非法组合必须在本地拦截（目录 min_duration 4 / max_duration 30，
// 8 种 aspect ratio，images≤30/videos≤10/audios≤10，prompt≤5M字符）。
func TestValidateFixedModelRejectsIllegalCombinations(t *testing.T) {
	imgs := `"` + strings.Repeat("x", 10) + `"`
	manyImgs := "[" + strings.Repeat(imgs+",", 31) + imgs + "]"
	longPrompt := strings.Repeat("飞", 5000001)
	cases := map[string]string{
		"duration too long":  `{"model":"seedance2.5特惠版","prompt":"hi","duration":31}`,
		"duration too short": `{"model":"seedance2.5特惠版","prompt":"hi","duration":3}`,
		"seconds too long":   `{"model":"seedance2.5特惠版","prompt":"hi","seconds":"31"}`,
		"bad ratio":          `{"model":"seedance2.5特惠版","prompt":"hi","duration":4,"ratio":"16:10"}`,
		"too many images":    `{"model":"seedance2.5特惠版","prompt":"hi","duration":4,"images":` + manyImgs + `}`,
		"too many videos":    `{"model":"seedance2.5特惠版","prompt":"hi","duration":4,"videos":["a","b","c","d","e","f","g","h","i","j","k"]}`,
		"too many audios":    `{"model":"seedance2.5特惠版","prompt":"hi","duration":4,"audios":["a","b","c","d","e","f","g","h","i","j","k"]}`,
		"prompt too long":    `{"model":"seedance2.5特惠版","prompt":"` + longPrompt + `","duration":4}`,
	}
	for name, body := range cases {
		c, info, a := postVideoCtx(t, body)
		require.NotNil(t, a.ValidateRequestAndSetAction(c, info), name)
	}
}

// 每次业务提交必须带新的 Idempotency-Key；调用方自带的 Key 必须保留。
func TestBuildRequestHeaderSetsIdempotencyKey(t *testing.T) {
	newHeaderReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "https://youkou.cc/v1/videos", nil)
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	a := &TaskAdaptor{}
	info := fixedModelInfo()
	a.Init(info)

	req1 := newHeaderReq()
	require.NoError(t, a.BuildRequestHeader(testHeaderCtx(), req1, info))
	key1 := req1.Header.Get("Idempotency-Key")
	require.Len(t, key1, 36)

	req2 := newHeaderReq()
	require.NoError(t, a.BuildRequestHeader(testHeaderCtx(), req2, info))
	require.Len(t, req2.Header.Get("Idempotency-Key"), 36)
	require.NotEqual(t, key1, req2.Header.Get("Idempotency-Key"))

	req3 := newHeaderReq()
	req3.Header.Set("Idempotency-Key", "keep-me")
	require.NoError(t, a.BuildRequestHeader(testHeaderCtx(), req3, info))
	require.Equal(t, "keep-me", req3.Header.Get("Idempotency-Key"))
}

func testHeaderCtx() *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("Content-Type", "application/json")
	return c
}

// 轮询直接返回成片 URL 时必须回填，否则本站 /content 代理无地址可抓。
func TestFixedModelParseTaskResultBackfillsVideoURL(t *testing.T) {
	a := &TaskAdaptor{}
	a.selectSub("seedance2.5特惠版")
	done, err := a.ParseTaskResult([]byte(`{"id":"task_1","status":"completed","progress":100,"video_url":"https://cdn.example/v.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/v.mp4", done.Url)
}

// 查询/轮询路径拿到的 adaptor 未经过 Init，sub 为 nil。
// 回归：ConvertToOpenAIVideo / ParseTaskResult 在此路径下不得 panic，且正确委托给 sora 适配器。
func TestQueryPathWithoutInit(t *testing.T) {
	adaptor := &TaskAdaptor{}

	task := &model.Task{
		TaskID: "task_regress001",
		Data:   json.RawMessage(`{"id":"task_upstream001","status":"completed","model":"aliyun:wan-3.0"}`),
	}
	task.Properties.OriginModelName = "aliyun:wan-3.0"

	out, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(out), "task_regress001")

	info, err := adaptor.ParseTaskResult([]byte(`{"id":"x","status":"queued","progress":10}`))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, model.TaskStatusQueued, info.Status)
}
