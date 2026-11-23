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
	require.Equal(t,
		[]string{"sd-2.5", "wan-3.0-megabyai-retired", "seedance2.5-特惠", "T3-seedance-2.5"},
		(&TaskAdaptor{}).GetModelList())
	require.Equal(t, "MegaAI", (&TaskAdaptor{}).GetChannelName())
}

// sd-2.5 上游按次计费（固定价，与分辨率/时长无关），EstimateBilling 必须返回 nil，
// 禁止带入 sora 的 seconds/size 倍率，否则会按秒多扣费。
func TestEstimateBillingIsFixedPerUse(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Nil(t, adaptor.EstimateBilling(nil, nil))
	require.Nil(t, adaptor.EstimateBilling(nil, &relaycommon.RelayInfo{OriginModelName: "sd-2.5"}))
}

// wan-3.0-megabyai-retired 上游按次计价（时长不参与），只有分辨率档参与计价：
// 480p 1.0 / 720p 3.5/3.0 / 1080p 4.0/3.0，且**不能**出现 seconds 倍率。
func TestEstimateBillingWan30UsesResolutionTierOnly(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: "wan-3.0-megabyai-retired"}

	for resolution, expected := range map[string]float64{
		"480p":  1.0,
		"720p":  3.5 / 3.0,
		"1080p": 4.0 / 3.0,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(wan30ResolutionContextKey, resolution)
		ratios := adaptor.EstimateBilling(c, info)
		require.Equal(t, map[string]float64{"size": expected}, ratios, resolution)
		require.NotContains(t, ratios, "seconds", "wan-3.0-megabyai-retired 上游按次计费，时长不得参与计价")
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

// wan-3.0-megabyai-retired 提交体必须走上游「统一参数」契约：
// duration / ratio / resolution / referenceImages，而不是秒 / 尺寸 / images 那套透传字段。
func TestBuildRequestBodyUsesUnifiedUpstreamContract(t *testing.T) {
	raw := `{"model":"wan-3.0-megabyai-retired","prompt":"海边日落","duration":10,"ratio":"16:9","resolution":"720p","images":["https://example.com/a.png"],"seed":42}`
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
		OriginModelName: "wan-3.0-megabyai-retired",
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "wan-3.0-megabyai-retired"},
	})
	require.NoError(t, err)

	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	require.Equal(t, "wan-3.0-megabyai-retired", body["model"])
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

// 上游回执里的各种成片地址形状都要能「认出来」，但**一律不向外透传**。
//
// 认出来是为了把「成片已出」判对；不透传是为了不把上游存储域名报给客户。
func TestParseTaskResultCapturesVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}

	completed, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"video_url":"https://cdn.example/v.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, completed.Status)
	require.Empty(t, completed.Url, "顶层 video_url 不得进入 result_url")

	nested, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"data":{"url":"https://cdn.example/n.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, nested.Status)
	require.Empty(t, nested.Url, "嵌套 data.url 同样不得进入 result_url")

	queued, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","task_id":"video_1","status":"queued"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusQueued, queued.Status)

	running, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"in_progress","progress":30}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, running.Status)
}

// 上游文档：部分渠道返回 succeeded 等状态，必须当成功处理。
//
// ⚠️ 2026-11-23 审查修正：成片地址一律**不透传给客户**（本站代理口径）。
// 上游回执里的 video_url / url / result_url 指向它自己的存储域名，原样写进
// result_url 等于把真实货源报给客户。留空 Url 后，轮询层落成
// /v1/videos/{本站ID}/content，由 video_proxy 的 type=64 分支回源取片。
func TestParseTaskResultAcceptsSucceededStatus(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body := []byte(`{"id":"video_1","status":"succeeded","data":{"url":"https://cdn.example/s.mp4"}}`)
	result, err := adaptor.ParseTaskResult(body)
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Empty(t, result.Url,
		"上游地址不得进入 result_url（会暴露货源），必须交给轮询层落成站内代理路径")

	// 反向证明：上游确实给了地址，是我们主动丢掉的（不是解析不到）。
	var parsed megaVideoResult
	require.NoError(t, common.Unmarshal(body, &parsed))
	require.True(t, megaHasResult(parsed),
		"上游回执里确实带成片地址 —— 上面的 Empty 断言才是「主动不透传」而不是「解析失败」")
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

// MegaAI 归档名必须**不再有价**。
//
// ⚠️ 这条商品已随渠道（type=64）于 2026-11-10 停售，生产库里它的渠道、卡片、
// ModelPrice 键全部清空。代码默认值里那个 "wan-3.0" 的按次价（¥3.0/次）也已删除 ——
// 留着它有两个害处：
//  1. 名字被有赞的新模型接管后，同一个 ModelPrice 键会同时表达
//     「按次 ¥3.0」和「按秒」两种口径，计费必然算错；
//  2. 归档名还挂着一个价，等于给一条死链路留了个能下单的口子。
//
// 所以这里断言的是「查不到价」—— 死商品就该没有价。
//
// ⚠️ 2026-11-18：对外公开名已从 "wan-3.0" 改为 "wan-3.0-yz"（上游真名仍是 wan-3.0）。
func TestWan30PriceTiers(t *testing.T) {
	_, ok := ratio_setting.GetDefaultModelPriceMap()["wan-3.0-megabyai-retired"]
	require.False(t, ok,
		"MegaAI 归档名不得再有 ModelPrice —— 它的渠道已停售，"+
			"而 'wan-3.0-yz' 这个键归有赞新模型（按秒）所有")

	// 对照组：有赞新模型必须有价，否则按秒计费会落到「查不到价」分支算成 ¥0。
	youzanPrice, youzanOK := ratio_setting.GetDefaultModelPriceMap()["wan-3.0-yz"]
	require.True(t, youzanOK, "有赞 wan-3.0-yz 必须配价")
	require.InDelta(t, 0.20/ratio_setting.USD2RMB, youzanPrice, 1e-12,
		"有赞 wan-3.0-yz 基准档（720P）必须是 ¥0.20/秒")
}
