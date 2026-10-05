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
	"strconv"
	"strings"
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

func TestGetModelListExposesYouzanModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Equal(t, []string{"wan3.0-smart", primeModel, "wan2.7-r2v"}, adaptor.GetModelList())
}

func TestBuildRequestBodyUsesR2VDefaults(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:  r2vModel,
		Prompt: "让画面自然运动",
		Images: []string{"https://cdn.example/a.png"},
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, r2vModel, payload["model"])
	require.Equal(t, "1080p", payload["resolution"])
	require.Equal(t, "16:9", payload["ratio"])
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

// 有赞的 error 字段有两种形态，且 status 还有 refunded 这个终态。两者任一处理不对，
// 任务就会永远停在 NOT_START / 0%：2026-10-05 生产事故 task 249（wan2.7-r2v）就是
// 上游回了 {code,message} 对象，退款已在有赞侧发生（refundedPoints），new-api 却因
// 反序列化失败拿不到失败原因，用户的钱一直卡在预扣里。
func TestParseTaskResultHandlesUpstreamErrorShapesAndRefundedStatus(t *testing.T) {
	adaptor := &TaskAdaptor{}

	// 生产真实报文（task_1791125558575_71psn3）：对象形态 error + refunded 终态。
	result, err := adaptor.ParseTaskResult([]byte(`{
		"taskId":"task_1791125558575_71psn3","type":"video","model":"wan2.7-r2v",
		"status":"refunded","result":null,
		"error":{"code":"UPSTREAM_TASK_FAILED","message":"参考素材已失效或无法访问，请重新上传素材后再试"},
		"chargedPoints":0,"refundedPoints":10
	}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status, "refunded 必须收敛为失败，否则用户永远拿不到退款")
	require.Equal(t, "100%", result.Progress)
	require.Equal(t, "UPSTREAM_TASK_FAILED: 参考素材已失效或无法访问，请重新上传素材后再试", result.Reason)

	// error 为裸字符串：保持原有 "code: message" 风格不变。
	result, err = adaptor.ParseTaskResult([]byte(`{"status":"failed","error":"WAN3_QUOTA_CAPACITY_INSUFFICIENT: max duration is 4 seconds"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	require.Equal(t, "WAN3_QUOTA_CAPACITY_INSUFFICIENT: max duration is 4 seconds", result.Reason)

	// 对象只带 message / 只带 code：都不能丢信息，也不能报错。
	result, err = adaptor.ParseTaskResult([]byte(`{"status":"error","error":{"message":"素材不可访问"}}`))
	require.NoError(t, err)
	require.Equal(t, "素材不可访问", result.Reason)

	result, err = adaptor.ParseTaskResult([]byte(`{"status":"error","error":{"code":"ASSET_EXPIRED"}}`))
	require.NoError(t, err)
	require.Equal(t, "ASSET_EXPIRED", result.Reason)

	// error 为 null 且无任何原因：回落内容审核不通过，不得反序列化失败。
	result, err = adaptor.ParseTaskResult([]byte(`{"status":"failed","error":null,"result":null}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	require.Equal(t, "内容审核不通过", result.Reason)
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
		// 智能调度版档位：480P=¥0.28/秒、720P=¥0.32/秒、1080P=¥0.40/秒。
		{"wan3.0-smart", "480P", 1.0},
		{"wan3.0-smart", "720P", 0.32 / 0.28},
		{"wan3.0-smart", "1080P", 0.40 / 0.28},
		// R2V 对外全分辨率统一价（¥0.10/秒），各档倍率均为 1（上游只认小写分辨率）。
		{"wan2.7-r2v", "720p", 1.0},
		{"wan2.7-r2v", "1080p", 1.0},
		// R2V 不支持 480P，缺省/非法档位回退 1（落在统一价上）。
		{"wan2.7-r2v", "480P", 1.0},
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

func TestPrimeForces1080PAnd30Seconds(t *testing.T) {
	// 上游满血档仅支持 1080P，请求里的其它分辨率被忽略。
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "480P"}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "720P"}))
	// 时长固定 30 秒。
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Duration: 2}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Duration: 30}))
}

func TestPrimeBillsPerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 30, Resolution: "1080P"})
	info := &relaycommon.RelayInfo{OriginModelName: primeModel}
	// 按次计费：不返回 seconds/size 倍率，一次任务的价格直接由 ModelPrice 决定。
	require.Nil(t, (&TaskAdaptor{}).EstimateBilling(context, info))
}

func TestValidateMediaRejectsFrameReferenceMixing(t *testing.T) {
	frame := relaycommon.TaskMedia{Type: "first_frame", URL: "https://cdn.example/first.png"}
	lastFrame := relaycommon.TaskMedia{Type: "last_frame", URL: "https://cdn.example/last.png"}
	reference := relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/ref.png"}

	// 首尾帧可成对使用，但不能和普通参考素材混用（上游会拒绝）。
	require.NoError(t, validateMedia([]relaycommon.TaskMedia{frame, lastFrame}, "wan3.0-smart"))
	require.Error(t, validateMedia([]relaycommon.TaskMedia{frame, reference}, "wan3.0-smart"))
	require.Error(t, validateMedia([]relaycommon.TaskMedia{lastFrame, reference}, "wan3.0-smart"))
	// wan2.7-r2v 只支持参考图：首尾帧、参考视频、参考音频都会被静默丢弃，本地直接拒绝。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{frame}, r2vModel))
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		reference,
		{Type: "reference_video", URL: "https://cdn.example/v.mp4"},
	}, r2vModel))
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		reference,
		{Type: "reference_audio", URL: "https://cdn.example/a.mp3"},
	}, r2vModel))
}

func TestPrimeBodyShapeAndReferenceLimit(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:    primeModel,
		Prompt:   "海边的日落延时",
		Duration: 5,
	})
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, float64(30), payload["duration"])
	require.Equal(t, "1080P", payload["resolution"])

	// 参考图上限 8 张（smart 为 10 张）。
	images := make([]relaycommon.TaskMedia, 0, 9)
	for index := 0; index < 9; index++ {
		images = append(images, relaycommon.TaskMedia{
			Type: "reference_image",
			URL:  "https://cdn.example/" + strconv.Itoa(index) + ".png",
		})
	}
	require.Error(t, validateMedia(images, primeModel))
	require.NoError(t, validateMedia(images[:8], primeModel))
	require.NoError(t, validateMedia(images, "wan3.0-smart"))
}

func TestR2VResolutionAndDurationLimits(t *testing.T) {
	// 上游只认小写 720p / 1080p，缺省 1080p。
	require.Equal(t, "1080p", resolveResolution(relaycommon.TaskSubmitReq{Model: r2vModel}))
	require.Equal(t, "720p", resolveResolution(relaycommon.TaskSubmitReq{
		Model:      r2vModel,
		Resolution: "720P",
	}))
	// 480P 不支持，回退上游默认 1080p。
	require.Equal(t, "1080p", resolveResolution(relaycommon.TaskSubmitReq{
		Model:      r2vModel,
		Resolution: "480P",
	}))
	// 上游 wan2.7-r2v 时长仅支持 5s / 10s，就近取档。
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Model: r2vModel, Duration: 2}))
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Model: r2vModel, Duration: 5}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: r2vModel, Duration: 7}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: r2vModel, Duration: 30}))
	// wan3.0-smart 仍按 2-30s 保留原值。
	require.Equal(t, 7, resolveDuration(relaycommon.TaskSubmitReq{Model: "wan3.0-smart", Duration: 7}))
}

func TestR2VBodyUsesFlatReferenceImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Prompt:   "@图片1 中的人物转身",
		Duration: 7,
		Images:   []string{"https://cdn.example/a.png", "https://cdn.example/b.png"},
	})
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, []interface{}{"https://cdn.example/a.png", "https://cdn.example/b.png"}, payload["referenceImages"])
	require.Equal(t, float64(10), payload["duration"], "7s 应就近取 10s 档")
	// r2v 不走 assets/mentions 协议。
	require.NotContains(t, payload, "conversationId")
	require.NotContains(t, payload, "mentions")

	// 没有参考图时本地就拒绝，不消耗上游调用。
	_, err = adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Prompt:   "no image",
		Duration: 5,
	})
	require.Error(t, err)
}

func TestR2VRatioAndReferenceImageLimits(t *testing.T) {
	// r2v 不接受 adaptive，缺省回退 16:9；显式传入的合法比例原样保留。
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{Model: r2vModel}))
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Metadata: map[string]interface{}{"ratio": "adaptive"},
	}))
	require.Equal(t, "9:16", resolveRatio(relaycommon.TaskSubmitReq{
		Model:    r2vModel,
		Metadata: map[string]interface{}{"ratio": "9:16"},
	}))
	// smart 保留 adaptive 与 4:3 / 3:4。
	require.Equal(t, "adaptive", resolveRatio(relaycommon.TaskSubmitReq{Model: "wan3.0-smart"}))
	require.Equal(t, "4:3", resolveRatio(relaycommon.TaskSubmitReq{
		Model:    "wan3.0-smart",
		Metadata: map[string]interface{}{"ratio": "4:3"},
	}))

	// r2v 上游最多 3 张参考图，第 4 张要报错；smart 上限仍是 10。
	media := func(n int) []relaycommon.TaskMedia {
		items := make([]relaycommon.TaskMedia, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, relaycommon.TaskMedia{Type: "reference_image", URL: "https://example.com/i.png"})
		}
		return items
	}
	require.NoError(t, validateMedia(media(3), r2vModel))
	require.Error(t, validateMedia(media(4), r2vModel))
	require.NoError(t, validateMedia(media(4), "wan3.0-smart"))
}

func TestValidateRequestRejectsBadMediaAsLocal400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	// r2v 缺参考图：客户端参数错误，必须是 400 本地错误（不触发渠道重试）。
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public001"},
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos",
		strings.NewReader(`{"model":"wan2.7-r2v","prompt":"x","seconds":5}`))
	context.Request.Header.Set("Content-Type", "application/json")
	taskErr := adaptor.ValidateRequestAndSetAction(context, info)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)

	// 首尾帧与参考素材混用：同样是 400 本地错误。
	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(
		`{"model":"wan3.0-smart","prompt":"x","metadata":{"media":[{"type":"first_frame","url":"https://cdn.example/a.png"},{"type":"reference_image","url":"https://cdn.example/b.png"}]}}`))
	context.Request.Header.Set("Content-Type", "application/json")
	taskErr = adaptor.ValidateRequestAndSetAction(context, info)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)
}
