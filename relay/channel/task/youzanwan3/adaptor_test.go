package youzanwan3

import (
	"bytes"
	"context"
	"image"
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
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/QuantumNous/new-api/setting/youzanwan3_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func testReferenceImagePNG(t *testing.T, width, height int) []byte {
	t.Helper()
	var buffer bytes.Buffer
	require.NoError(t, png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, width, height))))
	return buffer.Bytes()
}

// allowLoopbackFetchForTest 放行 httptest 的随机端口与本机地址。
// 生产默认只允许 80/443/8080/8443 且禁内网，测试服务器不满足。
func allowLoopbackFetchForTest(t *testing.T) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	originalPorts, originalPrivate := setting.AllowedPorts, setting.AllowPrivateIp
	setting.AllowedPorts = append(append([]string{}, originalPorts...), "10000-65535")
	setting.AllowPrivateIp = true
	t.Cleanup(func() {
		setting.AllowedPorts = originalPorts
		setting.AllowPrivateIp = originalPrivate
	})
}

// 参考图短边 <720 必须本地 400，禁止透传上游（上游只会回误导性的
// MEDIA_IMAGE_INVALID「图片无法读取或格式不支持」）。新版协议下我们只读图片头部。
func TestValidateReferenceImagesRejectsSmallImage(t *testing.T) {
	allowLoopbackFetchForTest(t)
	small := testReferenceImagePNG(t, 300, 500)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(small)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	err := adaptor.validateReferenceImages(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: server.URL + "/small.png"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "短边至少需要 720")
}

func TestValidateReferenceImagesAcceptsLargeImage(t *testing.T) {
	allowLoopbackFetchForTest(t)
	large := testReferenceImagePNG(t, 720, 1280)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(large)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	require.NoError(t, adaptor.validateReferenceImages(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: server.URL + "/large.png"},
		// 非图片素材不参与尺寸校验。
		{Type: "reference_audio", URL: "https://cdn.example/a.mp3"},
	}))
}

// 2026-11-15 上游切到 New API 协议：建单 POST /v1/videos，素材在 body 的 media 里
// **直接给原始 URL**（上游自己抓取），不再有 POST /v1/assets 上传换签名地址这一步。
// duration 也随之改成上游 TaskSubmitReq 认的 seconds 字符串。
func TestBuildRequestBodyPostsInlineMediaToV1Videos(t *testing.T) {
	var upstreamCalls int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		upstreamCalls++
		t.Errorf("新协议下构建请求体不应访问上游素材接口，实际请求: %s", request.URL.Path)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "test-key"}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:  "wan3.0-smart",
		Prompt: "城市夜景缓慢推进",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://cdn.example/ref.png"},
			{Type: "reference_audio", URL: "https://cdn.example/a.mp3"},
		},
		Duration:   2,
		Resolution: "480P",
	})
	require.NoError(t, err)
	require.Zero(t, upstreamCalls, "构建请求体必须是纯本地的")

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	// 公开别名 wan3.0-smart 必须映射成上游现存的 wan3.0-video。
	require.Equal(t, "wan3.0-video", payload["model"])
	// 上游 /v1/videos 收 seconds 字符串（2026-11-15 探针实测回显 seconds=2）。
	require.Equal(t, "2", payload["seconds"])
	require.Equal(t, "480P", payload["resolution"])
	require.Equal(t, "adaptive", payload["ratio"])
	require.NotContains(t, payload, "duration")

	media, ok := payload["media"].([]any)
	require.True(t, ok)
	require.Len(t, media, 2)
	first := media[0].(map[string]any)
	require.Equal(t, "reference_image", first["type"])
	require.Equal(t, "https://cdn.example/ref.png", first["url"], "素材 URL 必须原样透传，不再换签名地址")
	second := media[1].(map[string]any)
	require.Equal(t, "reference_audio", second["type"])
	require.Equal(t, "https://cdn.example/a.mp3", second["url"])
}

func TestBuildRequestURLAndIdempotencyKey(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: normalizeAPIRoot("https://youzan666.vip/v1")}
	url, err := adaptor.BuildRequestURL(nil)
	require.NoError(t, err)
	require.Equal(t, "https://youzan666.vip/v1/videos", url)

	// 上游要求 8—100 位、仅字母/数字/下划线/连字符。
	require.Equal(t, "task_1791125558575_71psn3", idempotencyKey(&relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_1791125558575_71psn3"},
	}))
	// 非法字符要净化，不能透传给上游。
	key := idempotencyKey(&relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task/1 2:3"},
	})
	require.Equal(t, "task-1-2-3", key)
	require.NotContains(t, key, "/")
	require.NotContains(t, key, " ")
	// 过短要补足 8 位，过长要截到 100 位。
	require.GreaterOrEqual(t, len(idempotencyKey(&relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "a"},
	})), 8)
	require.Len(t, idempotencyKey(&relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: strings.Repeat("a", 150)},
	}), 100)
	require.GreaterOrEqual(t, len(idempotencyKey(nil)), 8)
}

func TestFetchTaskHitsV1VideosPath(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		gotPath = request.URL.Path
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"id":"task_gWJ8","object":"video","model":"wan3.0-video","status":"in_progress","progress":30,"created_at":1794711440}`))
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	resp, err := adaptor.FetchTask(server.URL, "k", map[string]any{"id": "task_gWJ8"}, "")
	require.NoError(t, err)
	require.NotNil(t, resp)
	_ = resp.Body.Close()
	require.Equal(t, "/v1/videos/task_gWJ8", gotPath)
}

// 上游拒绝建单时返回 {"error":{...}}（New API 的 OpenAI 风格错误对象），
// 响应里没有 task id。适配器必须把上游的真实原因透出去，
// 而不是退化成无信息量的「returned empty task id」（2026-10-28 生产实测）。
func TestDoResponseSurfacesUpstreamRejection(t *testing.T) {
	cases := []struct {
		name     string
		payload  string
		contains string
	}{
		{
			name:     "对象形态 error",
			payload:  `{"error":{"code":"invalid_request_error","message":"seconds must be between 2 and 30"}}`,
			contains: "seconds must be between 2 and 30",
		},
		{
			name:     "裸字符串 error",
			payload:  `{"error":"积分不足"}`,
			contains: "积分不足",
		},
		{
			name:     "顶层 message",
			payload:  `{"message":"提示词不能为空或超过 20000 字"}`,
			contains: "提示词不能为空",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)

			adaptor := &TaskAdaptor{}
			response := &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(tc.payload)),
			}

			_, _, taskErr := adaptor.DoResponse(context, response, nil)
			require.NotNil(t, taskErr, "上游拒绝时必须返回错误，不能当成空 task id")
			require.Contains(t, taskErr.Message, tc.contains)
			require.NotContains(t, taskErr.Message, "empty task id")
		})
	}
}

// 上游确实建了任务时，错误字段必须被忽略，照常返回 task id。
func TestDoResponseIgnoresErrorWhenTaskIDPresent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	adaptor := &TaskAdaptor{}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(
			`{"id":"task_gWJ8","object":"video","model":"wan3.0-video","status":"queued","progress":0,"created_at":1794711440}`)),
	}

	taskID, _, taskErr := adaptor.DoResponse(context, response, nil)
	require.Nil(t, taskErr)
	require.Equal(t, "task_gWJ8", taskID, "轮询要靠这个上游 id")
}

// 2026-11-15 上游切到 New API 的 OpenAIVideo 形态（探针实测）：
// {"id":...,"object":"video","model":...,"status":"queued|in_progress|failed","progress":N,...}
// 完成态的成片地址按 New API 惯例放在 metadata.url；顶层 url 变体也认。
func TestParseTaskResultReadsOpenAIVideoShape(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://youzan666.vip"}

	queued, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","object":"video","status":"queued","progress":0}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusQueued, queued.Status)
	require.Equal(t, taskcommon.ProgressQueued, queued.Progress)

	processing, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","object":"video","status":"in_progress","progress":42}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, processing.Status)
	require.Equal(t, "42%", processing.Progress, "上游给了真实进度就该透传给客户")

	// 没有 progress 时退回通用进度文案，不能回显 0%（看着像卡死）。
	noProgress, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"in_progress"}`))
	require.NoError(t, err)
	require.Equal(t, taskcommon.ProgressInProgress, noProgress.Progress)

	done, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"completed","progress":100,"metadata":{"url":"https://oss.example/v.mp4?Expires=1&Signature=x"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, done.Status)
	require.Equal(t, "100%", done.Progress)
	require.Equal(t, "https://oss.example/v.mp4?Expires=1&Signature=x", done.Url)

	topLevel, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"completed","url":"https://oss.example/v2.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, topLevel.Status)
	require.Equal(t, "https://oss.example/v2.mp4", topLevel.Url)

	// completed 但还没有地址：不能提前收尾，继续轮询。
	pending, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"completed","metadata":{}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, pending.Status)

	failed, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"failed","error":{"code":"CONTENT_REJECTED","message":"生成结果疑似包含真人面部，未通过内容安全审核，请更换或风格化相关素材后重试"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, failed.Status)
	require.Equal(t, "100%", failed.Progress)
	require.Equal(t, "生成结果疑似包含真人面部，未通过内容安全审核，请更换或风格化相关素材后重试", failed.Reason,
		"对客文案取 error.message，不能把机器码拼进来")

	// 只有 code 没有 message 时也不能丢信息。
	codeOnly, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"failed","error":{"code":"MEDIA_INVALID"}}`))
	require.NoError(t, err)
	require.Equal(t, "MEDIA_INVALID", codeOnly.Reason)

	// 无理由失败统一归因内容审核不通过。
	noReason, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"failed"}`))
	require.NoError(t, err)
	require.Equal(t, "内容审核不通过", noReason.Reason)

	// 相对路径（历史上游会回 host 相对地址）要按 API 根解析。
	relative, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"completed","metadata":{"url":"/outputs/videos/vid_1.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, "https://youzan666.vip/outputs/videos/vid_1.mp4", relative.Url)
}

// 上游的终态失败词形都必须收敛成失败，否则任务永远停在预扣里、客户拿不到退款。
func TestParseTaskResultCollapsesTerminalFailureStates(t *testing.T) {
	adaptor := &TaskAdaptor{}
	for _, status := range []string{"failed", "failure", "canceled", "cancelled", "error", "refunded"} {
		result, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","status":"` + status + `","error":{"message":"素材不可访问"}}`))
		require.NoError(t, err, "status=%s", status)
		require.Equal(t, model.TaskStatusFailure, result.Status, "status=%s", status)
		require.Equal(t, "100%", result.Progress, "status=%s", status)
		require.Equal(t, "素材不可访问", result.Reason, "status=%s", status)
	}
}

func TestValidateMediaRejectsUnsupportedTypesAndLimits(t *testing.T) {
	image := func() relaycommon.TaskMedia {
		return relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/r.png"}
	}

	// 2026-10-28 上游取消首尾帧（firstFrameAssetId/lastFrameAssetId 已不存在），
	// 必须明确拒绝而不是静默降级成参考图。
	err := validateMedia([]relaycommon.TaskMedia{
		{Type: "first_frame", URL: "https://cdn.example/f.png"},
	}, "wan3.0-smart")
	require.EqualError(t, err, "youzan wan3 no longer supports first_frame/last_frame, use reference_image instead")
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "last_frame", URL: "https://cdn.example/l.png"},
	}, "wan3.0-smart"))

	// 三类素材各自上限：图片 10 / 视频 5 / 音频 5，总量 20。
	media := func(kind string, n int) []relaycommon.TaskMedia {
		items := make([]relaycommon.TaskMedia, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, relaycommon.TaskMedia{Type: kind, URL: "https://cdn.example/" + strconv.Itoa(i)})
		}
		return items
	}
	require.NoError(t, validateMedia(media("reference_image", 10), "wan3.0-smart"))
	require.Error(t, validateMedia(media("reference_image", 11), "wan3.0-smart"))
	require.NoError(t, validateMedia(media("reference_video", 5), "wan3.0-smart"))
	require.Error(t, validateMedia(media("reference_video", 6), "wan3.0-smart"))
	require.NoError(t, validateMedia(media("reference_audio", 5), "wan3.0-smart"))
	require.Error(t, validateMedia(media("reference_audio", 6), "wan3.0-smart"))

	// 10 + 5 + 5 = 20 允许，再多一项就超总量。
	twenty := append(append(media("reference_image", 10), media("reference_video", 5)...), media("reference_audio", 5)...)
	require.NoError(t, validateMedia(twenty, "wan3.0-smart"))
	require.Error(t, validateMedia(append(twenty, image()), "wan3.0-smart"))

	// 空 URL 直接拒绝，避免把无效素材传上游。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{{Type: "reference_image", URL: " "}}, "wan3.0-smart"))
	// 未知类型拒绝。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{{Type: "mask", URL: "https://cdn.example/m.png"}}, "wan3.0-smart"))

	// 新版上游自己按 URL 抓素材，data URL / 相对路径它抓不到 —— 必须本地 400，
	// 不能等它建单跑完才报「素材取不到」（那时客户已经付了钱）。
	err = validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_image", URL: "data:image/png;base64,AAAA"},
	}, "wan3.0-smart")
	require.Error(t, err)
	require.Contains(t, err.Error(), "http(s)")
	require.NotContains(t, err.Error(), "AAAA", "错误信息不能回显 base64 原文")
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_image", URL: "/local/relative.png"},
	}, "wan3.0-smart"))
}

func TestUpstreamModelFor(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{videoModel, "wan3.0-video"},
		{smartLegacyModel, "wan3.0-video"},
		{primeModel, "wan3.0-video-prime"},
		{legacyPrimeModel, "wan3.0-video-prime"},
		// 对外别名 seedance2.5-特惠 实际由 prime 执行，上游只能收到真实模型名。
		{tehuiModel, "wan3.0-video-prime"},
	}
	for _, tc := range cases {
		got, err := upstreamModelFor(tc.input)
		require.NoError(t, err, "input=%s", tc.input)
		require.Equal(t, tc.want, got, "input=%s", tc.input)
	}
	// wan2.7-r2v 已从上游 /v1/models 目录消失，必须报错而不是发出去。
	_, err := upstreamModelFor(r2vModel)
	require.Error(t, err)
	_, err = upstreamModelFor("unknown-model")
	require.Error(t, err)
}

func TestResolutionDurationRatio(t *testing.T) {
	// wan3.0-video：480P / 720P / 1080P 三档都受支持，缺省 480P。
	require.Equal(t, "480P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel, Resolution: "720p"}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel, Size: "1080"}))
	// 上游 2—30 秒，越界夹紧。
	require.Equal(t, 2, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Duration: 1}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Duration: 99}))
	require.Equal(t, 7, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Seconds: "7"}))
	// 2026-10-28 新增 21:9 宽银幕。
	require.Equal(t, "21:9", resolveRatio(relaycommon.TaskSubmitReq{
		Model:    videoModel,
		Metadata: map[string]interface{}{"ratio": "21:9"},
	}))
	require.Equal(t, "adaptive", resolveRatio(relaycommon.TaskSubmitReq{Model: videoModel}))
	// 非法比例回退缺省值。
	require.Equal(t, "adaptive", resolveRatio(relaycommon.TaskSubmitReq{
		Model:    videoModel,
		Metadata: map[string]interface{}{"aspect_ratio": "2:1"},
	}))
	// 历史别名 wan3.0-smart 解析结果与 wan3.0-video 完全一致。
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: smartLegacyModel, Resolution: "1080p"}))
	require.Equal(t, 8, resolveDuration(relaycommon.TaskSubmitReq{Model: smartLegacyModel, Duration: 8}))
}

func TestPrimeTiersAndDuration(t *testing.T) {
	// 上游 prime 只有 720P / 1080P，缺省 720P（最低档，不静默升价）。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "1080p"}))
	// 480P 请求按最低档 720P 执行，而不是被丢掉或升到 1080P。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "480P"}))
	// 2026-10-28 起 prime 也按秒计费，2—30 秒逐秒可选，不再固定 30 秒。
	require.Equal(t, 2, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Duration: 2}))
	require.Equal(t, 12, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Seconds: "12"}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Duration: 99}))
	// 参考图上限与 video 一致（10 张），旧版的 8 张限制已随新协议取消。
	media := make([]relaycommon.TaskMedia, 0, 10)
	for index := 0; index < 10; index++ {
		media = append(media, relaycommon.TaskMedia{
			Type: "reference_image",
			URL:  "https://cdn.example/" + strconv.Itoa(index) + ".png",
		})
	}
	require.NoError(t, validateMedia(media, primeModel))
	require.Error(t, validateMedia(append(media, relaycommon.TaskMedia{
		Type: "reference_image",
		URL:  "https://cdn.example/10.png",
	}), primeModel))
}

func TestEstimateBillingChargesResolutionTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	cases := []struct {
		model      string
		resolution string
		wantSize   float64
	}{
		// wan3.0-video：上游三档同价（0.18 ⚡/秒），售价统一 ¥0.23/秒，倍率全为 1。
		{videoModel, "480P", 1.0},
		{videoModel, "720P", 1.0},
		{videoModel, "1080P", 1.0},
		// wan3.0-video-prime：上游 720P 0.15 / 1080P 0.20 ⚡/秒，
		// 每档加价 ¥0.10 → 售价 ¥0.25 / ¥0.30（2026-10-29 第二轮：720P 由 ¥0.20 上调），
		// 1080P 倍率 = 0.30/0.25 = 1.2。
		{primeModel, "720P", 1.0},
		{primeModel, "1080P", 1.2},
		// 480P 不在 prime 档位内，按 720P 计费（不低于成本）。
		{primeModel, "480P", 1.0},
		// 对外别名 seedance2.5-特惠 **独立定价**（老板 2026-10-29 明确）：
		// 720P ¥0.20 / 1080P ¥0.30，倍率 1.0 / 1.5，不跟 prime 联动。
		{tehuiModel, "720P", 1.0},
		{tehuiModel, "1080P", 1.5},
		{tehuiModel, "480P", 1.0},
		// 未知档位回退 1，避免多扣费。
		{videoModel, "4K", 1.0},
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

func TestPrimeBillsPerSecondNotPerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 30, Resolution: "1080P"})
	info := &relaycommon.RelayInfo{OriginModelName: primeModel}
	// 2026-10-28 起 prime 改为按秒计费：必须返回 seconds/size 倍率，
	// 否则 quota 会退化成「一次任务 = ModelPrice」的按次计费。
	require.Equal(t, map[string]float64{"seconds": 30, "size": 1.2},
		(&TaskAdaptor{}).EstimateBilling(context, info))
}

// TestTehuiAliasMirrorsPrime 锁定对外别名 seedance2.5-特惠 与 wan3.0-video-prime
// 在**参数行为**上完全等价：同样的档位校验、同样的时长夹紧、同样的比例白名单。
//
// ⚠️ 价格不在「等价」范围内：2026-10-29 老板明确特惠独立定价
// （720P ¥0.20 / 1080P ¥0.30），prime 调价它不动。计费差异见
// TestTehuiPriceIsIndependent。
//
// 为什么单独立一条：别名一旦在某个分支上漏判（例如 isPrimeModel 没认它），
// 表现是「480P 请求被放行到只支持 720P/1080P 的上游」（400），不报错的另一种
// 是静默算错价。这类偏差只能靠断言钉住。
func TestTehuiAliasMirrorsPrime(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		req       relaycommon.TaskSubmitReq
		wantRes   string
		wantDur   int
		wantRatio string
	}{
		{"default", relaycommon.TaskSubmitReq{Model: tehuiModel}, "720P", 5, "adaptive"},
		{"1080p", relaycommon.TaskSubmitReq{Model: tehuiModel, Resolution: "1080p"}, "1080P", 5, "adaptive"},
		{"480p falls back to 720P", relaycommon.TaskSubmitReq{Model: tehuiModel, Resolution: "480P"}, "720P", 5, "adaptive"},
		{"duration clamped low", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 1}, "720P", 2, "adaptive"},
		{"duration clamped high", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 99}, "720P", 30, "adaptive"},
		{"21:9 allowed", relaycommon.TaskSubmitReq{
			Model:    tehuiModel,
			Metadata: map[string]interface{}{"ratio": "21:9"},
		}, "720P", 5, "21:9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantRes, resolveResolution(tc.req))
			require.Equal(t, tc.wantDur, resolveDuration(tc.req))
			require.Equal(t, tc.wantRatio, resolveRatio(tc.req))
		})
	}
	// 首尾帧与 prime 一样被拒绝（上游 2026-10-28 已取消该能力）。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{{
		Type: "first_frame",
		URL:  "https://cdn.example/f.png",
	}}, tehuiModel))
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

func TestValidateRequestRejectsBadMediaAsLocal400(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:   &relaycommon.ChannelMeta{},
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public001"},
	}

	// 首尾帧已不被上游接受：属于客户端参数错误，必须是 400 本地错误（不触发渠道重试）。
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(
		`{"model":"wan3.0-smart","prompt":"x","metadata":{"media":[{"type":"first_frame","url":"https://cdn.example/a.png"}]}}`))
	context.Request.Header.Set("Content-Type", "application/json")
	taskErr := adaptor.ValidateRequestAndSetAction(context, info)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)
}

func TestNormalizeAPIRootStripsVersionSuffix(t *testing.T) {
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip/v1"))
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip/v1/"))
	require.Equal(t, "https://youzan666.vip", normalizeAPIRoot("https://youzan666.vip"))
	require.Equal(t, "https://example.com/openai", normalizeAPIRoot("https://example.com/openai"))
}

func TestGetModelListExposesYouzanModels(t *testing.T) {
	adaptor := &TaskAdaptor{}
	// 与上游 /v1/models 完全一致（wan3.0-smart / wan2.7-r2v 已不再展示）。
	require.Equal(t, []string{"wan3.0-video", "wan3.0-video-prime"}, adaptor.GetModelList())
}

// TestEstimateBillingPrefersDynamicRatio 锁定「动态调价优先于代码常量」。
//
// 上游成本一变，同步任务会写 setting 里的倍率；计费必须立刻按新倍率算。
// 如果这里读不到动态值，表现是「上游涨了价、我们还在按旧价卖」——
// 不报错、不掉单，只是每单少赚，只能靠断言发现。
func TestEstimateBillingPrefersDynamicRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 恢复成默认倍率，避免污染同包其它用例（它们断言的是 1.2）。
	t.Cleanup(func() {
		youzanwan3_setting.SetRatios(map[string]map[string]float64{
			"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
			"wan3.0-video-prime": {"720P": 1.0, "1080P": 1.2},
		}, 0)
	})
	youzanwan3_setting.SetRatios(map[string]map[string]float64{
		"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
		"wan3.0-video-prime": {"720P": 1.0, "1080P": 2.0},
	}, 0)

	// prime 与它的历史旧名读 prime 的动态倍率。
	for _, model := range []string{primeModel, legacyPrimeModel} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "1080P"})
		info := &relaycommon.RelayInfo{OriginModelName: model}
		require.Equal(t, map[string]float64{"seconds": 10, "size": 2.0},
			(&TaskAdaptor{}).EstimateBilling(context, info), "model=%s", model)
	}
	// 标准版读自己的配置，不受 prime 的倍率影响。
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "1080P"})
	info := &relaycommon.RelayInfo{OriginModelName: videoModel}
	require.Equal(t, map[string]float64{"seconds": 10, "size": 1.0},
		(&TaskAdaptor{}).EstimateBilling(context, info))
}

// TestTehuiPriceIsIndependent 锁定「seedance2.5-特惠 独立定价」。
//
// 2026-10-29 老板明确：prime 调价它不动。实现上它有自己的倍率表
// （tehuiResolutionSizeRatio：720P 1.0 / 1080P 1.5），并且不在
// youzanAliasTargets 里。
//
// 为什么必须单独钉住：它 isPrimeModel 为真，一旦 resolutionRatioForModel
// 的分支顺序写反（先走 prime 分支），就会读到 prime 的倍率 —— 不报错、
// 不报警，只是每单静默算错价。
func TestTehuiPriceIsIndependent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 把 prime 的动态倍率拉到 2.0，模拟「prime 被调价」。
	t.Cleanup(func() {
		youzanwan3_setting.SetRatios(map[string]map[string]float64{
			"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
			"wan3.0-video-prime": {"720P": 1.0, "1080P": 1.2},
		}, 0)
	})
	youzanwan3_setting.SetRatios(map[string]map[string]float64{
		"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
		"wan3.0-video-prime": {"720P": 1.0, "1080P": 2.0},
	}, 0)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "1080P"})
	info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
	require.Equal(t, map[string]float64{"seconds": 10, "size": 1.5},
		(&TaskAdaptor{}).EstimateBilling(context, info),
		"特惠 1080P 必须按自己的 1.5 倍率算（0.30/0.20），不能跟 prime 的 2.0")
}
