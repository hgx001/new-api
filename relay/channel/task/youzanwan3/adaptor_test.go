package youzanwan3

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func onePixelPNG(t *testing.T) string {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&buffer, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestBuildRequestBodyUploadsWan3AssetsAndMapsMentions(t *testing.T) {
	assetIndex := 0
	fileContentTypes := make(map[int]string)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/conversations":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"conversation-1"}`))
		case "/api/multimodal-assets":
			assetIndex++
			// Upstream rejects application/octet-stream parts (MEDIA_MIME_MISMATCH);
			// the file part must declare its real MIME type.
			if reader, err := request.MultipartReader(); err == nil {
				for {
					part, err := reader.NextPart()
					if err != nil {
						break
					}
					_, _ = io.Copy(io.Discard, part)
					if part.FileName() != "" {
						fileContentTypes[assetIndex] = part.Header.Get("Content-Type")
					}
				}
			}
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"asset":{"id":"asset-` + string(rune('0'+assetIndex)) + `","displayAlias":"asset` + string(rune('0'+assetIndex)) + `"}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "test-key"}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:  "wan3.0-smart",
		Prompt: "让@图片1配合@音频1运动",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: onePixelPNG(t)},
			{Type: "reference_audio", URL: "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString([]byte("audio"))},
		},
		Duration: 5,
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "wan3.0-smart", payload["model"])
	require.Equal(t, "conversation-1", payload["conversationId"])
	require.Contains(t, payload["prompt"], "@asset1")
	require.Contains(t, payload["prompt"], "@asset2")
	require.Len(t, payload["mentions"], 2)
	// Upstream rejects application/octet-stream file parts (MEDIA_MIME_MISMATCH).
	require.Len(t, fileContentTypes, 2)
	for _, contentType := range fileContentTypes {
		require.NotEqual(t, "application/octet-stream", contentType)
	}
	require.Contains(t, fileContentTypes[1], "image/")
	require.Equal(t, "audio/mpeg", fileContentTypes[2])
}

func TestGetModelListExposesSmartAndR2VModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Equal(t, []string{"wan3.0-smart", "wan2.7-r2v"}, adaptor.GetModelList())
}

func TestBuildRequestBodyUsesR2VDefaults(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:  r2vModel,
		Prompt: "让画面自然运动",
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, r2vModel, payload["model"])
	require.Equal(t, "1080P", payload["resolution"])
	require.Equal(t, float64(defaultDuration), payload["duration"])
}

func TestParseTaskResultReadsYouzanResult(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"status":"succeeded","result":{"url":"https://cdn.example/video.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/video.mp4", result.Url)
	require.Equal(t, "100%", result.Progress)
}

func TestNormalizeAPIRootStripsVersionSuffix(t *testing.T) {
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip/v1"))
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip/v1/"))
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip"))
	require.Equal(t, "https://example.com/openai", normalizeAPIRoot("https://example.com/openai"))
}

func TestParseTaskResultResolvesRelativeVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: normalizeAPIRoot("https://youzan666.vip/v1")}
	result, err := adaptor.ParseTaskResult([]byte(`{"status":"success","result":{"url":"/outputs/videos/vid_1.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, "https://youzan666.vip/outputs/videos/vid_1.mp4", result.Url)
	require.Equal(t, "100%", result.Progress)
}

func TestParseTaskResultUnifiesUnexplainedFailureAsModeration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	// 上游只回 status=failed、无任何原因：统一记为内容审核不通过。
	result, err := adaptor.ParseTaskResult([]byte(`{"model":"wan3.0-smart","status":"failed","taskId":"wan3_45ea34d8"}`))
	require.NoError(t, err)
	require.Equal(t, "内容审核不通过", result.Reason)

	// 上游给了原因则原样保留。
	result, err = adaptor.ParseTaskResult([]byte(`{"status":"failed","message":"balance insufficient"}`))
	require.NoError(t, err)
	require.Equal(t, "balance insufficient", result.Reason)
}

func TestConvertToOpenAIVideoIncludesFailureReason(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task-youzan-failure",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "WAN3_QUOTA_CAPACITY_INSUFFICIENT: max duration is 4 seconds",
		Properties: model.Properties{OriginModelName: "wan3.0-smart"},
		Data:       []byte(`{"status":"failed"}`),
	}

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &response))
	require.Equal(t, dto.VideoStatusFailed, response.Status)
	require.NotNil(t, response.Error)
	require.Equal(t, task.FailReason, response.Error.Message)
}

func TestEstimateBillingChargesSmartResolutionTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	cases := []struct {
		model      string
		resolution string
		wantSize   float64
	}{
		// 智能调度版档位：480P=¥0.28/秒、720P=¥0.45/秒、1080P=¥0.65/秒。
		{"wan3.0-smart", "480P", 1.0},
		{"wan3.0-smart", "720P", 0.45 / 0.28},
		{"wan3.0-smart", "1080P", 0.65 / 0.28},
		// R2V 以 720P ¥0.60/秒为基准，1080P 为 ¥1.00/秒。
		{"wan2.7-r2v", "720P", 1.0},
		{"wan2.7-r2v", "1080P", 1.0 / 0.6},
		// R2V 不支持 480P，缺省/非法档位回退到官网默认 1080P。
		{"wan2.7-r2v", "480P", 1.0 / 0.6},
		// 未知档位回退 1，避免多扣费。
		{"wan3.0-smart", "4K", 1.0},
	}
	for _, tc := range cases {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Duration:   5,
			Resolution: tc.resolution,
		})
		info := &relaycommon.RelayInfo{OriginModelName: tc.model}
		require.Equal(t, map[string]float64{
			"seconds": 5,
			"size":    tc.wantSize,
		}, adaptor.EstimateBilling(context, info), "model=%s resolution=%s", tc.model, tc.resolution)
	}
}

func TestR2VResolutionAndDurationLimits(t *testing.T) {
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: r2vModel}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{
		Model:      r2vModel,
		Resolution: "720P",
	}))
	require.Equal(t, 15, resolveDuration(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Duration: 30,
	}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Duration: 30,
		Media: []relaycommon.TaskMedia{{
			Type: "reference_video",
			URL:  "https://example.com/reference.mp4",
		}},
	}))
}
