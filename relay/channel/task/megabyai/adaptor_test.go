package megabyai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModelListContainsSD25AndWan30(t *testing.T) {
	require.Equal(t, []string{"sd-2.5", "wan-3.0"}, (&TaskAdaptor{}).GetModelList())
	require.Equal(t, "MegaAI", (&TaskAdaptor{}).GetChannelName())
}

// sd-2.5 上游按次计费（固定价，与分辨率/时长无关），EstimateBilling 必须返回 nil，
// 禁止带入 sora 的 seconds/size 倍率，否则会按秒多扣费。
func TestEstimateBillingIsFixedPerUse(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.EstimateBilling(nil, nil))
	require.Nil(t, adaptor.EstimateBilling(nil, &relaycommon.RelayInfo{OriginModelName: "sd-2.5"}))
}

// wan-3.0 上游按次计价（时长不参与），只有分辨率档参与计价：
// 480p 1.0 / 720p 1.25 / 1080p 1.666667，且**不能**出现 seconds 倍率。
func TestEstimateBillingWan30UsesResolutionTierOnly(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: "wan-3.0"}

	for resolution, expected := range map[string]float64{
		"480p":  1.0,
		"720p":  1.25,
		"1080p": 1.666667,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(wan30ResolutionContextKey, resolution)
		ratios := adaptor.EstimateBilling(c, info)
		require.Equal(t, map[string]float64{"size": expected}, ratios, resolution)
		require.NotContains(t, ratios, "seconds", "wan-3.0 上游按次计费，时长不得参与计价")
	}
}

// 分辨率归一：认档位名、认 WxH；认不出来必须报错（猜错档位 = 少收钱），
// 完全没给时落到默认档并显式下发。
func TestResolveWan30Resolution(t *testing.T) {
	cases := []struct {
		name      string
		req       relaycommon.TaskSubmitReq
		expected  string
		expectErr bool
	}{
		{name: "档位名", req: relaycommon.TaskSubmitReq{Resolution: "720p"}, expected: "720p"},
		{name: "档位名大写", req: relaycommon.TaskSubmitReq{Resolution: "1080P"}, expected: "1080p"},
		{name: "纯数字", req: relaycommon.TaskSubmitReq{Resolution: "480"}, expected: "480p"},
		{name: "size 横屏", req: relaycommon.TaskSubmitReq{Size: "1280x720"}, expected: "720p"},
		{name: "size 竖屏", req: relaycommon.TaskSubmitReq{Size: "720x1280"}, expected: "720p"},
		{name: "size 1080", req: relaycommon.TaskSubmitReq{Size: "1920x1080"}, expected: "1080p"},
		{name: "未指定走默认档", req: relaycommon.TaskSubmitReq{}, expected: "480p"},
		{name: "非法档位", req: relaycommon.TaskSubmitReq{Resolution: "4k"}, expectErr: true},
		{name: "非法 size", req: relaycommon.TaskSubmitReq{Size: "huge"}, expectErr: true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			resolution, err := resolveWan30Resolution(testCase.req)
			if testCase.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.expected, resolution)
		})
	}
}

// 素材数量超上游上限（参考图 ≤10 / 参考视频 ≤5 / 参考音频 ≤5）时本地 400。
func TestBuildWan30MediaEnforcesUpstreamLimits(t *testing.T) {
	images := make([]string, 11)
	for i := range images {
		images[i] = "https://example.com/" + string(rune('a'+i)) + ".png"
	}
	_, _, _, err := buildWan30Media(relaycommon.TaskSubmitReq{Images: images})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 10 reference images")

	videos := make([]string, 6)
	for i := range videos {
		videos[i] = "https://example.com/" + string(rune('a'+i)) + ".mp4"
	}
	_, _, _, err = buildWan30Media(relaycommon.TaskSubmitReq{Media: []relaycommon.TaskMedia{
		{Type: "reference_video", URL: videos[0]},
		{Type: "reference_video", URL: videos[1]},
		{Type: "reference_video", URL: videos[2]},
		{Type: "reference_video", URL: videos[3]},
		{Type: "reference_video", URL: videos[4]},
		{Type: "reference_video", URL: videos[5]},
	}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 5 reference videos")

	collectedImages, collectedVideos, collectedAudios, err := buildWan30Media(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://example.com/a.png"},
			{Type: "first_frame", URL: "https://example.com/b.png"},
		},
		Audios: []string{"https://example.com/c.mp3"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/a.png", "https://example.com/b.png"}, collectedImages)
	require.Empty(t, collectedVideos)
	require.Equal(t, []string{"https://example.com/c.mp3"}, collectedAudios)
}

// wan-3.0 提交体必须走上游「统一参数」契约：
// duration / ratio / resolution / referenceImages，而不是秒 / 尺寸 / images 那套透传字段。
func TestBuildRequestBodyUsesUnifiedUpstreamContract(t *testing.T) {
	raw := `{"model":"wan-3.0","prompt":"海边日落","duration":10,"ratio":"16:9","resolution":"720p","images":["https://example.com/a.png"],"seed":42}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(raw))
	c.Set(common.KeyRequestBody, []byte(raw))
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:     "海边日落",
		Duration:   10,
		Resolution: "720p",
		Seed:       int64Ptr(42),
		Images:     []string{"https://example.com/a.png"},
	})

	adaptor := &TaskAdaptor{}
	reader, err := adaptor.BuildRequestBody(c, &relaycommon.RelayInfo{
		OriginModelName: "wan-3.0",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "wan-3.0"},
	})
	require.NoError(t, err)

	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	require.Equal(t, "wan-3.0", body["model"])
	require.Equal(t, "海边日落", body["prompt"])
	require.Equal(t, float64(10), body["duration"])
	require.Equal(t, "16:9", body["ratio"])
	require.Equal(t, "720p", body["resolution"])
	require.Equal(t, []any{"https://example.com/a.png"}, body["referenceImages"])
	require.Equal(t, float64(42), body["seed"])
	// 统一参数契约下不能再出现经典字段（上游文档明确不要混用）
	require.NotContains(t, body, "seconds")
	require.NotContains(t, body, "size")
	require.NotContains(t, body, "images")
}

func TestParseTaskResultCapturesVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}

	completed, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"video_url":"https://cdn.example/v.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, completed.Status)
	require.Equal(t, "https://cdn.example/v.mp4", completed.Url)

	nested, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"data":{"url":"https://cdn.example/n.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, nested.Status)
	require.Equal(t, "https://cdn.example/n.mp4", nested.Url)

	queued, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","task_id":"video_1","status":"queued"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusQueued, queued.Status)

	running, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"in_progress","progress":30}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, running.Status)
}

// 上游文档：部分渠道返回 succeeded 等状态，必须当成功处理。
func TestParseTaskResultAcceptsSucceededStatus(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"succeeded","data":{"url":"https://cdn.example/s.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "https://cdn.example/s.mp4", result.Url)
}

// 上游错误字段可能是对象也可能是字符串，两种都要能取出失败原因，
// 且解析失败不能让任务卡在非终态（否则既看不到原因也退不了款）。
func TestParseTaskResultToleratesErrorShapes(t *testing.T) {
	adaptor := &TaskAdaptor{}

	objectError, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"failed","error":{"code":"content_filter","message":"内容审核不通过"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, objectError.Status)
	require.Equal(t, "内容审核不通过", objectError.Reason)

	stringError, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"failed","error":"upstream rejected the request"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, stringError.Status)
	require.Equal(t, "upstream rejected the request", stringError.Reason)

	failReason, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"failed","fail_reason":"reference video too long"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, failReason.Status)
	require.Equal(t, "reference video too long", failReason.Reason)

	// 认不出的状态按「还在跑」处理，避免误判失败把用户已生成的片子作废。
	unknown, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"rendering"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, unknown.Status)
}

func int64Ptr(value int64) *int64 {
	return &value
}

// 对外价必须锁死：480p ¥2.4 / 720p ¥3.0 / 1080p ¥4.0（每次）。
// 改价会红：改价要同时改 defaultModelPrice、本测试的期望值和生产 ModelPrice option。
func TestWan30PriceTiers(t *testing.T) {
	basePrice, ok := ratio_setting.GetDefaultModelPriceMap()["wan-3.0"]
	require.True(t, ok, "wan-3.0 必须配置按次单价")
	baseQuota := int(basePrice * common.QuotaPerUnit)

	for resolution, expectedCny := range map[string]float64{
		"480p":  2.4,
		"720p":  3.0,
		"1080p": 4.0,
	} {
		quota := int(float64(baseQuota) * wan30ResolutionSizeRatio[resolution])
		actualCny := float64(quota) / common.QuotaPerUnit * ratio_setting.USD2RMB
		require.InDelta(t, expectedCny, actualCny, 0.0001, resolution)
	}
}
