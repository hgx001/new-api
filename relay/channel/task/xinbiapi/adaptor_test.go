package xinbiapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", req)
	return c
}

func newAdaptor() *TaskAdaptor {
	return &TaskAdaptor{baseURL: "https://www.xinbiapi.com", apiKey: "test-key"}
}

func TestResolveDuration(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr bool
	}{
		{name: "显式 30 秒", req: relaycommon.TaskSubmitReq{Duration: 30}, want: 30},
		{name: "缺省按固定 30 秒", req: relaycommon.TaskSubmitReq{}, want: 30},
		{name: "seconds 字符串 30", req: relaycommon.TaskSubmitReq{Seconds: "30"}, want: 30},
		{name: "传 2 秒被拒（固定时长）", req: relaycommon.TaskSubmitReq{Duration: 2}, wantErr: true},
		{name: "传 5 秒被拒（固定时长）", req: relaycommon.TaskSubmitReq{Duration: 5}, wantErr: true},
		{name: "seconds 非数字", req: relaycommon.TaskSubmitReq{Seconds: "abc"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDuration(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveRatio(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr bool
	}{
		{name: "缺省 adaptive", req: relaycommon.TaskSubmitReq{}, want: "adaptive"},
		{name: "metadata.ratio", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "16:9"}}, want: "16:9"},
		{name: "size 里写比例", req: relaycommon.TaskSubmitReq{Size: "9:16"}, want: "9:16"},
		{name: "size 是像素不认", req: relaycommon.TaskSubmitReq{Size: "1280x720"}, want: "adaptive"},
		{name: "非法比例", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "99:1"}}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRatio(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCollectReferenceImages(t *testing.T) {
	t.Run("https 通过", func(t *testing.T) {
		got, err := collectReferenceImages(relaycommon.TaskSubmitReq{
			Image:  "https://cdn.example.com/a.png",
			Images: []string{"https://cdn.example.com/b.png"},
		})
		require.NoError(t, err)
		assert.Equal(t, []string{"https://cdn.example.com/a.png", "https://cdn.example.com/b.png"}, got)
	})

	t.Run("data URL 通过", func(t *testing.T) {
		got, err := collectReferenceImages(relaycommon.TaskSubmitReq{Images: []string{"data:image/png;base64,AAAA"}})
		require.NoError(t, err)
		assert.Len(t, got, 1)
	})

	t.Run("http 被拒（上游 FILE_3000037）", func(t *testing.T) {
		_, err := collectReferenceImages(relaycommon.TaskSubmitReq{Images: []string{"http://cdn.example.com/a.png"}})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "https")
	})

	t.Run("超过 9 张被拒", func(t *testing.T) {
		images := make([]string, 10)
		for i := range images {
			images[i] = "https://cdn.example.com/x.png"
		}
		_, err := collectReferenceImages(relaycommon.TaskSubmitReq{Images: images})
		require.Error(t, err)
	})
}

// 上游只认 480P 生成 + 固定别名；请求体里绝不能出现 negative_prompt
// （兼容口实测返回 UNSUPPORTED_FIELD）。
func TestBuildRequestBodyShape(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:   "雨后的青石板小巷",
		Duration: 30,
		Metadata: map[string]interface{}{"ratio": "16:9"},
		Images:   []string{"https://cdn.example.com/ref.png"},
	})

	reader, err := a.BuildRequestBody(c, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &decoded))

	assert.Equal(t, upstreamModel, decoded["model"], "必须固定发上游别名，不信任渠道映射")
	assert.Equal(t, float64(fixedDuration), decoded["duration"], "固定 30 秒，缺省也显式下发")
	metadata, ok := decoded["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, generationResolution, metadata["resolution"], "生成档位恒为 480P")
	assert.Equal(t, "16:9", metadata["ratio"])
	assert.NotContains(t, decoded, "negative_prompt")
	assert.NotContains(t, decoded, "negativePrompt")
	assert.NotContains(t, decoded, "parameters", "兼容口没有 parameters 桶")
	assert.Equal(t, []interface{}{"https://cdn.example.com/ref.png"}, decoded["reference_images"])
}

// seed 不在上游 parameterSchema 里（additionalProperties: false），
// 发出去必吃 422 UNKNOWN_MEDIA_PARAMETER，且报错不点名字段。
// 所以 BuildRequestBody 绝不能带它，ValidateRequestAndSetAction 必须本地先拒。
func TestSeedIsRejectedLocallyAndNeverSent(t *testing.T) {
	seed := int64(20261101)
	req := relaycommon.TaskSubmitReq{
		Prompt:   "雨后的青石板小巷",
		Duration: 30,
		Seed:     &seed,
	}

	a := newAdaptor()
	// 1. 提交体里不能出现 seed
	c := newTestContext(t, req)
	reader, err := a.BuildRequestBody(c, &relaycommon.RelayInfo{})
	require.NoError(t, err)
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &decoded))
	assert.NotContains(t, decoded, "seed")

	// 2. 校验阶段就拒，且客户端能从错误码认出来
	seedReq := relaycommon.TaskSubmitReq{Prompt: "x", Duration: 30, Seed: &seed}
	err = rejectUnsupportedFields(seedReq)
	require.Error(t, err, "带 seed 必须被拒")
	assert.Contains(t, err.Error(), "seed")

	// 不带 seed 时不拦
	assert.NoError(t, rejectUnsupportedFields(relaycommon.TaskSubmitReq{Prompt: "x", Duration: 30}))
}

// ConvertToOpenAIVideo 不实现时 GET /v1/videos/{id} 会落到 not_implemented:68，
// 任务照跑照扣费但客户端永远查不到 —— 这里锁住「能查 + 不泄露内部地址」。
func TestConvertToOpenAIVideoUsesProxyURL(t *testing.T) {
	a := newAdaptor()
	task := &model.Task{
		TaskID:   "task_abc",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: ModelNamePayPerCall,
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "cos-proxy://mps-test-1397292858/ap-guangzhou/out.mp4",
		},
	}
	raw, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "task_abc", decoded["id"])
	assert.Equal(t, "completed", decoded["status"])
	assert.Equal(t, ModelNamePayPerCall, decoded["model"])

	metadata, ok := decoded["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, taskcommon.BuildProxyURL("task_abc"), metadata["url"])
	assert.NotContains(t, string(raw), "cos-proxy", "内部存储标记不能出现在客户端响应里")
}

// 失败任务要给出可读原因，否则客户端只会看到一个永远 queued 的任务。
func TestConvertToOpenAIVideoMapsFailure(t *testing.T) {
	a := newAdaptor()
	task := &model.Task{
		TaskID:     "task_bad",
		Status:     model.TaskStatusFailure,
		FailReason: "上游拒绝了这次请求",
		Properties: model.Properties{OriginModelName: ModelNamePayPerCall},
	}
	raw, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "failed", decoded["status"])
	errObj, ok := decoded["error"].(map[string]interface{})
	require.True(t, ok)
	assert.NotEmpty(t, errObj["message"])
}

// 按次计价：任何倍率都会在按次价上再乘一次，必须返回 nil。
func TestEstimateBillingIsPerCall(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{Duration: 30})
	assert.Nil(t, a.EstimateBilling(c, &relaycommon.RelayInfo{}))
}

func TestParseTaskResultStatus(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantStatus string
	}{
		{name: "QUEUED", body: `{"status":"QUEUED"}`, wantStatus: model.TaskStatusQueued},
		{name: "RUNNING", body: `{"status":"RUNNING"}`, wantStatus: model.TaskStatusInProgress},
		{name: "FAILED", body: `{"status":"FAILED","failureMessage":"内容审核不通过"}`, wantStatus: model.TaskStatusFailure},
		{name: "未知状态按处理中", body: `{"status":"WHATEVER"}`, wantStatus: model.TaskStatusInProgress},
	}
	a := newAdaptor()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, err := a.ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, info.Status)
		})
	}
}

func TestParseTaskResultSuccessUsesDirectURL(t *testing.T) {
	a := newAdaptor()
	info, err := a.ParseTaskResult([]byte(`{"status":"SUCCEEDED","url":"https://cdn.example.com/out.mp4"}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, info.Status)
	assert.Equal(t, "https://cdn.example.com/out.mp4", info.Url)
}

// 只有 resultFileIds 时要去取件接口换稳定直链 —— 这条地址随后要交给腾讯 MPS 超分，
// MPS 没有渠道密钥，所以必须是公网可直接抓的链接。
func TestParseTaskResultResolvesDownloadURL(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		assert.Equal(t, "/v1/media/files/file_123/download", r.URL.Path)
		_, _ = w.Write([]byte(`{"downloadUrl":"https://dl.example.com/signed.mp4"}`))
	}))
	defer server.Close()

	a := &TaskAdaptor{baseURL: server.URL, apiKey: "k-1"}
	info, err := a.ParseTaskResult([]byte(`{"status":"SUCCEEDED","resultFileIds":["file_123"]}`))
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, info.Status)
	assert.Equal(t, "https://dl.example.com/signed.mp4", info.Url)
	assert.Equal(t, "Bearer k-1", gotAuth)
}

// 终态却拿不到地址时必须报错让轮询重试，不能判成功交付空链接。
func TestParseTaskResultSuccessWithoutFileFails(t *testing.T) {
	a := newAdaptor()
	_, err := a.ParseTaskResult([]byte(`{"status":"SUCCEEDED","resultFileIds":[]}`))
	require.Error(t, err)
}

func TestNeedsSuperResolutionOnlyForPayPerCallModel(t *testing.T) {
	assert.True(t, needsSuperResolution(ModelNamePayPerCall))
	assert.False(t, needsSuperResolution("wan3.0-720p-特惠"))
	assert.False(t, needsSuperResolution("wan3.0-video"))
}

func TestBuildRequestURLEndpoints(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://www.xinbiapi.com/", apiKey: "k"}
	url, err := a.BuildRequestURL(&relaycommon.RelayInfo{})
	require.NoError(t, err)
	assert.Equal(t, "https://www.xinbiapi.com/newapi/v1/video/generations", url)
	assert.True(t, strings.HasPrefix(url, "https://"))
}
