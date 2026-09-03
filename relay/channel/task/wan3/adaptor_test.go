package wan3

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    2.0,
	}, adaptor.EstimateBilling(context, nil))
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
