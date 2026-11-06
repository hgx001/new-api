package wan3

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

func TestResolveDurationUsesSecondsWhenDurationIsAbsent(t *testing.T) {
	require.Equal(t, 12, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "12"}))
}

func TestResolveResolutionAcceptsTopLevelResolution(t *testing.T) {
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Resolution: "720p"}))
}

func TestResolveResolutionAcceptsSizeTierString(t *testing.T) {
	// ArcReel 客户端把分辨率档放 size 字段（如 "720P"），须与顶层 resolution 等价解析。
	require.Equal(t, "480P", resolveResolution(relaycommon.TaskSubmitReq{Size: "480P"}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Size: "720P"}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Size: "1080P"}))
	require.Equal(t, "720P", normalizeResolution("720p"))
}

func TestResolveRatioPrefersMetadataAspectRatio(t *testing.T) {
	// ArcReel 客户端经 metadata 传 aspect_ratio（9:16/16:9 等），优先于 size 推断。
	require.Equal(t, "9:16", resolveRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"aspect_ratio": "9:16"},
	}))
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"ratio": "16:9"},
	}))
	require.Equal(t, "adaptive", resolveRatio(relaycommon.TaskSubmitReq{Size: "720P"}))
}

func TestResolveRatioInfersFromWxHSize(t *testing.T) {
	require.Equal(t, "9:16", resolveRatio(relaycommon.TaskSubmitReq{Size: "405x720"}))
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{Size: "720x405"}))
}

func TestEstimateBillingChargesWan3ResolutionRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "720P",
	})

	// 标准版模型名走 1/2/4，720P 倍率 2 —— 价格不随官网渠道调价而变动。
	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    2.0,
	}, adaptor.EstimateBilling(context, &relaycommon.RelayInfo{OriginModelName: "wan3.0-video"}))

	// 官网渠道模型名走调价后的 1/2.25/4，720P 倍率 2.25。
	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    2.25,
	}, adaptor.EstimateBilling(context, &relaycommon.RelayInfo{OriginModelName: OfficialModelName}))
}

// 官网渠道对外单价（2026-10-22 调价）：480P ¥0.20/秒、720P ¥0.45/秒、1080P ¥0.80/秒。
// 带参考视频时，参考视频秒数按**同一档**单价一起计（seconds = 生成 + 参考视频总秒数）。
// 额度算法与框架一致：quota = int(ModelPrice × QuotaPerUnit)，再 × seconds × size（倍率乘完取整）。
func TestOfficialPerSecondPriceTable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ratio_setting.InitRatioSettings()

	price, ok := ratio_setting.GetModelPrice("wan3.0-video-官网", false)
	require.True(t, ok, "wan3.0-video-官网 must have an explicit per-second price")
	require.InDelta(t, 0.20/ratio_setting.USD2RMB, price, 1e-9, "480P base must equal ¥0.20/sec in USD")

	baseQuota := int(price * common.QuotaPerUnit)

	cases := []struct {
		name       string
		resolution string
		refSeconds int
		wantCNY    float64
	}{
		{"480P 10s", "480P", 0, 2.00},
		{"480P 10s + 10s reference video", "480P", 10, 4.00},
		{"720P 10s", "720P", 0, 4.50},
		{"720P 10s + 10s reference video", "720P", 10, 9.00},
		{"1080P 10s", "1080P", 0, 8.00},
		{"1080P 10s + 10s reference video", "1080P", 10, 16.00},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Set("task_request", relaycommon.TaskSubmitReq{
				Duration:   10,
				Resolution: tc.resolution,
			})
			if tc.refSeconds > 0 {
				context.Set(refVideoSecondsContextKey, tc.refSeconds)
			}

			ratios := adaptor.EstimateBilling(context, &relaycommon.RelayInfo{OriginModelName: OfficialModelName})
			multiplier := 1.0
			for _, ratio := range ratios {
				multiplier *= ratio
			}
			quota := int(float64(baseQuota) * multiplier)
			cny := float64(quota) / common.QuotaPerUnit * ratio_setting.USD2RMB
			require.InDelta(t, tc.wantCNY, cny, 0.005, "charged CNY")
		})
	}
}

func TestTaskSubmitReqTreatsTypedMediaAsInput(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{{Type: "reference_video", URL: "https://example.com/motion.mp4"}},
	}
	require.True(t, req.HasImage())
}

func TestWan3BuildRequestBodyUsesNativeMediaPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "wan3.0-video",
		},
		OriginModelName: "wan3.0-video",
	}
	adaptor.Init(info)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "@图1是人物，@图2是服装，@视频1提供动作参考。",
		Images: []string{
			"data:image/jpeg;base64,aW1hZ2Ux",
			"data:image/jpeg;base64,aW1hZ2Uy",
			"data:video/mp4;base64,dmlkZW8=",
		},
		Size:    "864x480",
		Seconds: "7",
	})

	body, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload struct {
		Model string `json:"model"`
		Input struct {
			Prompt string                  `json:"prompt"`
			Media  []relaycommon.TaskMedia `json:"media"`
		} `json:"input"`
		Parameters struct {
			Resolution string `json:"resolution"`
			Ratio      string `json:"ratio"`
			Duration   int    `json:"duration"`
		} `json:"parameters"`
	}
	require.NoError(t, json.Unmarshal(encoded, &payload))
	require.Equal(t, "wan3.0-video", payload.Model)
	require.Equal(t, "图1是人物，图2是服装，视频1提供动作参考。", payload.Input.Prompt)
	require.Equal(t, []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "data:image/jpeg;base64,aW1hZ2Ux"},
		{Type: "reference_image", URL: "data:image/jpeg;base64,aW1hZ2Uy"},
		{Type: "reference_video", URL: "data:video/mp4;base64,dmlkZW8="},
	}, payload.Input.Media)
	require.Equal(t, "480P", payload.Parameters.Resolution)
	require.Equal(t, "16:9", payload.Parameters.Ratio)
	require.Equal(t, 7, payload.Parameters.Duration)
}

func TestWan3BuildRequestBodyUsesDashScopeEndpoint(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://workspace.cn-beijing.maas.aliyuncs.com"},
	}
	adaptor.Init(info)

	url, err := adaptor.BuildRequestURL(info)
	require.NoError(t, err)
	require.Equal(t,
		"https://workspace.cn-beijing.maas.aliyuncs.com/api/v1/services/aigc/video-generation/video-synthesis",
		url,
	)
}

func TestWan3BuildRequestBodyPreservesTypedMediaAndMetadata(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: "wan3.0-video",
	}
	adaptor.Init(info)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "视频1提供动作参考。",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_video", URL: "https://example.com/motion.mp4"},
		},
		Metadata: map[string]interface{}{
			"audio":         false,
			"prompt_extend": false,
			"watermark":     true,
		},
	})

	body, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	var payload struct {
		Input struct {
			Media []relaycommon.TaskMedia `json:"media"`
		} `json:"input"`
		Parameters struct {
			PromptExtend bool `json:"prompt_extend"`
			Audio        bool `json:"audio"`
			Watermark    bool `json:"watermark"`
		} `json:"parameters"`
	}
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, &payload))
	require.Equal(t, []relaycommon.TaskMedia{{Type: "reference_video", URL: "https://example.com/motion.mp4"}}, payload.Input.Media)
	require.False(t, payload.Parameters.PromptExtend)
	require.False(t, payload.Parameters.Audio)
	require.True(t, payload.Parameters.Watermark)
}

func TestWan3BuildRequestBodyRejectsMixedFrameAndReferenceMedia(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: "wan3.0-video",
	}
	adaptor.Init(info)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "invalid combination",
		Media: []relaycommon.TaskMedia{
			{Type: "first_frame", URL: "https://example.com/first.png"},
			{Type: "reference_image", URL: "https://example.com/reference.png"},
		},
	})

	_, err := adaptor.BuildRequestBody(context, info)
	require.EqualError(t, err, "wan3 first_frame/last_frame cannot be combined with reference media")
}

func TestWan3BuildRequestHeaderUsesDashScopeAsync(t *testing.T) {
	adaptor := &TaskAdaptor{}
	req := httptest.NewRequest("POST", "https://example.com", nil)
	require.NoError(t, adaptor.BuildRequestHeader(nil, req, nil))
	require.Equal(t, "application/json", req.Header.Get("Content-Type"))
	require.Equal(t, "enable", req.Header.Get("X-DashScope-Async"))
	require.Empty(t, req.Header.Get("Idempotency-Key"))
}

func TestWan3ParseTaskResultReadsOfficialOutput(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{
		"request_id": "req-1",
		"output": {
			"task_id": "task-1",
			"task_status": "SUCCEEDED",
			"video_url": "https://example.com/video.mp4"
		}
	}`))

	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "https://example.com/video.mp4", result.Url)
}

// 特惠只有一个交付档位：客户端传 1080P 也只会拿到 720P，
// 回执必须报实际交付档位，不能把请求值原样回显成「订单承诺」。
func TestWan3DoResponseReportsDeliveredResolutionForOfficial720P(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      Official720PModelName,
		Resolution: "1080P",
		Seconds:    "2",
	})

	resp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"request_id":"req-1","output":{"task_id":"up-1","task_status":"PENDING"}}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: Official720PModelName,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
	}

	adaptor := &TaskAdaptor{}
	_, _, taskErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, taskErr)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out))
	require.Equal(t, "720P", out["resolution"], "特惠只交付 720P，回执不能回显 1080P")
	require.Equal(t, "2", out["seconds"])
}

// 非特惠模型的分辨率照旧由请求决定，不能被这次修复带偏。
func TestWan3DoResponseKeepsRequestedResolutionForOtherModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "wan3.0-video",
		Resolution: "1080P",
		Seconds:    "2",
	})

	resp := &http.Response{
		StatusCode: 200,
		Body: io.NopCloser(strings.NewReader(
			`{"request_id":"req-2","output":{"task_id":"up-2","task_status":"PENDING"}}`)),
	}
	info := &relaycommon.RelayInfo{
		OriginModelName: "wan3.0-video",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_2"},
	}

	adaptor := &TaskAdaptor{}
	_, _, taskErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, taskErr)

	var out map[string]interface{}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &out))
	require.Equal(t, "1080P", out["resolution"])
}
