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

// 参考图短边 <480 必须本地 400，禁止透传上游（上游只会回误导性的
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
	require.Contains(t, err.Error(), "短边至少需要 480")
}

// 2026-11-24 由 720 下调到 480：客户 854x480 的参考图被误拦（ArcReel 端同口径）。
func TestValidateReferenceImagesAccepts480pImage(t *testing.T) {
	allowLoopbackFetchForTest(t)
	image480p := testReferenceImagePNG(t, 854, 480)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(image480p)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	require.NoError(t, adaptor.validateReferenceImages(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: server.URL + "/480p.png"},
	}))
}

// 边界不能被过度放宽：短边 479 仍要拦住。
func TestValidateReferenceImagesRejectsJustBelow480(t *testing.T) {
	allowLoopbackFetchForTest(t)
	image := testReferenceImagePNG(t, 479, 854)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "image/png")
		_, _ = writer.Write(image)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{}
	err := adaptor.validateReferenceImages(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: server.URL + "/479.png"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "短边至少需要 480")
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
		},
		Duration:   12,
		Resolution: "480P",
	})
	require.NoError(t, err)
	require.Zero(t, upstreamCalls, "构建请求体必须是纯本地的")

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	// 公开别名 wan3.0-smart 必须映射成上游现存的 wan3.0-video。
	require.Equal(t, "wan3.0-video", payload["model"])
	// 上游 /v1/videos 收 seconds 字符串。
	require.Equal(t, "12", payload["seconds"])
	// 2026-11-15 上游切换后 480P 下架：显式 480P 按最低档 720P 执行。
	require.Equal(t, "720P", payload["resolution"])
	require.Equal(t, "adaptive", payload["ratio"])
	require.NotContains(t, payload, "duration")

	media, ok := payload["media"].([]any)
	require.True(t, ok)
	require.Len(t, media, 1)
	first := media[0].(map[string]any)
	require.Equal(t, "reference_image", first["type"])
	require.Equal(t, "https://cdn.example/ref.png", first["url"], "素材 URL 必须原样透传，不再换签名地址")
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

	// completed 但没有地址：2026-11-15 上游切换后的新协议常态
	// （{"status":"completed"} 不再带 url/metadata，成片走 /content 端点）。
	// 必须收敛为 Success 且 Url 留空，由轮询层落成站内代理地址；
	// 再保持 IN_PROGRESS 就会永远卡死（生产 task 2997 实测）。
	newProto, err := adaptor.ParseTaskResult([]byte(`{"id":"task_1","object":"video","status":"completed","progress":100,"error":null}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, newProto.Status)
	require.Equal(t, "100%", newProto.Progress)
	require.Equal(t, "", newProto.Url, "无地址时留空，让轮询落代理地址")

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

	// 2026-11-15 上游切换：标准版（促销族/直连/smart）仅接受 reference_image，
	// 参考视频/音频本地直接 400（不透传给上游让它回看不懂的文案）。
	media := func(kind string, n int) []relaycommon.TaskMedia {
		items := make([]relaycommon.TaskMedia, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, relaycommon.TaskMedia{Type: kind, URL: "https://cdn.example/" + strconv.Itoa(i)})
		}
		return items
	}
	require.Error(t, validateMedia(media("reference_video", 1), "wan3.0-smart"))
	require.Error(t, validateMedia(media("reference_audio", 1), "wan3.0-smart"))

	// 图片上限 10；prime 直连未接素材新闸（上游文档未明确 prime 的 media 契约），
	// 视频/音频的条数上限用 prime 验证。
	require.NoError(t, validateMedia(media("reference_image", 10), "wan3.0-smart"))
	require.Error(t, validateMedia(media("reference_image", 11), "wan3.0-smart"))
	require.NoError(t, validateMedia(media("reference_video", 5), primeModel))
	require.Error(t, validateMedia(media("reference_video", 6), primeModel))
	require.NoError(t, validateMedia(media("reference_audio", 5), primeModel))
	require.Error(t, validateMedia(media("reference_audio", 6), primeModel))

	// prime 直连：10 图 + 5 视频 + 5 音频 = 20 允许，再多一项就超总量。
	twenty := append(append(media("reference_image", 10), media("reference_video", 5)...), media("reference_audio", 5)...)
	require.NoError(t, validateMedia(twenty, primeModel))
	require.Error(t, validateMedia(append(twenty, image()), primeModel))

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
		// 2026-11-18 上游新上架的两个独立模型。
		//
		// ⚠️ wan-3.0-yz 的**对外公开名**与**上游 ID** 不同：
		// 下发时必须换成上游真名 wan-3.0（上游没有 wan-3.0-yz 这个模型）。
		{wan30Model, "wan-3.0"},
		{sjWan3PrimeModel, "SJ-wan3-prime"},
		// 旧名继续路由到**旧的上游模型** —— 旧客户是按旧契约付费的
		//（按秒、10-30 秒、resolution+ratio 形态），改指向新模型等于换算力收一样的钱。
		{videoModel, "wan3.0-video"},
		{smartLegacyModel, "wan3.0-video"},
		{primeModel, "wan3.0-video-prime"},
		{legacyPrimeModel, "wan3.0-video-prime"},
		// 对外别名 seedance2.5-特惠：2026-11-20 起常驻 mw-sd2.5-flash
		//（4-30 秒、480p/720p；对客恒定交付 720P）。
		{tehuiModel, "mw-sd2.5-flash"},
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

// TestNewModelsAreIndependent 钉死「wan-3.0 / SJ-wan3-prime 是新上架的独立
// 模型，不是旧名的改名」这个事实。
//
// 2026-11-18 一度按「上游改名」处理（把旧名映射到新 ID），被上游文档纠正：
// api.youzan666.vip/docs 里 wan3.0-video / wan3.0-video-prime 与 wan-3.0 /
// SJ-wan3-prime 是**并列的四行**，契约完全不同（时长下界、参数形态、素材规则）。
//
// 这条测试的作用：一旦有人再次把新旧混为一谈（例如把旧标准版也映射到
// wan-3.0），这里立刻失败。
func TestNewModelsAreIndependent(t *testing.T) {
	// 新模型下发到上游。
	//
	// ⚠️ wan30Model（对外公开名 wan-3.0-yz）下发时必须换成上游真实 ID
	// wan-3.0 —— 上游没有 "wan-3.0-yz" 这个模型，填对外名直接 404。
	// SJ-wan3-prime 的公开名与上游 ID 相同，原样下发。
	for _, tc := range []struct{ public, upstream string }{
		{wan30Model, wan30UpstreamModel},
		{sjWan3PrimeModel, sjWan3PrimeModel},
	} {
		got, err := upstreamModelFor(tc.public)
		require.NoError(t, err)
		require.Equal(t, tc.upstream, got, "公开名 %s 必须下发为上游 ID %s", tc.public, tc.upstream)
	}
	// 旧名**不能**落到新模型上。
	for _, legacy := range []string{videoModel, smartLegacyModel, primeModel, legacyPrimeModel} {
		got, err := upstreamModelFor(legacy)
		require.NoError(t, err)
		require.NotEqual(t, wan30UpstreamModel, got, "旧名 %s 不能落到新模型 wan-3.0", legacy)
		require.NotEqual(t, sjWan3PrimeModel, got, "旧名 %s 不能落到新模型 SJ-wan3-prime", legacy)
	}
	// 促销族母本 2026-11-20 起常驻 mw-sd2.5-flash（活动版仍落标准版）。
	got, err := upstreamModelFor(tehuiModel)
	require.NoError(t, err)
	require.Equal(t, mwSd25FlashModel, got)
}

func TestResolutionDurationRatio(t *testing.T) {
	// 2026-11-15 上游切换后旧标准版只有 720P/1080P（480P 下架），缺省 720P。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel, Resolution: "720p"}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel, Size: "1080"}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: videoModel, Resolution: "480P"}))
	// 上游 10—30 秒，越界夹紧（显式 <10 在 validate 阶段已 400，夹紧只是兜底）。
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Duration: 1}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Duration: 99}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: videoModel, Seconds: "7"}))
	// 2026-10-28 新增 21:9 宽银幕（旧标准版仍然支持）。
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
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: smartLegacyModel, Duration: 8}))
}

// TestWan30Contract 钉死新模型 wan-3.0 的参数契约（2026-11-18 实测口径）。
//
// 上游原文：
//   - 时长硬边界 10-30 秒："Duration 4s is outside the allowed range
//     10-30 seconds for model wan-3.0"；
//   - 参数形态是 size 四个固定值，发 resolution+ratio 回
//     "unsupported resolution or ratio"；
//   - 画幅仅 16:9 / 9:16。
//
// ⚠️ 文档参数表写的是「4–30」，与实测不符 —— 按实测走。放行 5-9 秒就是
// 「客户付了钱、建了单，上游才拒」。
func TestWan30Contract(t *testing.T) {
	// 时长：10-30 秒，缺省 10 秒。
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: wan30Model}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: wan30Model, Duration: 4}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: wan30Model, Duration: 99}))
	require.Equal(t, 15, resolveDuration(relaycommon.TaskSubmitReq{Model: wan30Model, Duration: 15}))

	// 显式越界必须在**校验阶段** fail-loud（不能只靠夹紧）。
	for _, bad := range []int{4, 5, 9} {
		taskErr := validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
			Model: wan30Model, Duration: bad,
		})
		require.NotNil(t, taskErr, "wan-3.0 %d 秒必须被拒 —— 上游硬边界是 10 秒", bad)
	}
	taskErr := validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
		Model: wan30Model, Duration: 10,
	})
	require.Nil(t, taskErr, "10 秒必须放行")

	// 分辨率：720P / 1080P 两档；480P 按最低档 720P 执行。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: wan30Model}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: wan30Model, Resolution: "1080p"}))
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: wan30Model, Resolution: "480P"}))

	// 画幅：仅 16:9 / 9:16，缺省 16:9（上游不认 adaptive）。
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{Model: wan30Model}))
	require.Equal(t, "9:16", resolveRatio(relaycommon.TaskSubmitReq{
		Model: wan30Model, Metadata: map[string]interface{}{"ratio": "9:16"},
	}))
	// 21:9 不在白名单 → 回退 16:9；显式传 21:9 必须 400（不能静默改画幅）。
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{
		Model: wan30Model, Metadata: map[string]interface{}{"ratio": "21:9"},
	}))
	ratioErr := validateNewModelExplicitRatio(relaycommon.TaskSubmitReq{
		Model: wan30Model, Ratio: "1:1",
	})
	require.NotNil(t, ratioErr, "wan-3.0 显式 1:1 必须被拒 —— 上游仅 16:9 / 9:16")

	// 请求体：size 契约 + seconds 字符串，**不带** resolution / ratio。
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model: wan30Model, Prompt: "清晨湖泊", Duration: 10, Resolution: "720P",
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "wan-3.0", payload["model"])
	require.Equal(t, "10", payload["seconds"])
	require.Equal(t, "1280x720", payload["size"])
	require.NotContains(t, payload, "resolution", "wan-3.0 走 size 契约，不发 resolution")
	require.NotContains(t, payload, "ratio", "wan-3.0 走 size 契约，不发 ratio")
}

// TestWan30TopLevelMediaContract 钉死 wan-3.0 的**顶层素材数组**契约。
//
// 上游文档原文：「wan-3.0、wan-3.0-pro、seedance-2.0 支持 images、videos、
// audios 数组」—— 三个模型并列，素材形态一致（不是 media 数组）。
//
// 回归背景：原先 buildMedia 只对 wan-3.0-pro 并入顶层 videos / audios，
// validateMedia 也只对 wan-3.0-pro 放行参考音视频。wan-3.0 因此有两个洞：
//  1. 顶层 videos/audios 被**静默丢弃** —— 任务 SUCCESS、照常扣费，
//     成片里却没有参考素材（最差的失败方式）；
//  2. 走 media 传音视频会被本地 400 拦下，错误文案还写着「已不再支持」，
//     与上游文档直接矛盾。
func TestWan30TopLevelMediaContract(t *testing.T) {
	// 顶层 videos / audios 必须被并入素材（不能丢）。
	media, err := buildMedia(relaycommon.TaskSubmitReq{
		Model:  wan30Model,
		Images: []string{"https://e.com/a.png"},
		Videos: []string{"https://e.com/v.mp4"},
		Audios: []string{"https://e.com/a.mp3"},
	})
	require.NoError(t, err)
	counts := map[string]int{}
	for _, item := range media {
		counts[item.Type]++
	}
	require.Equal(t, 1, counts["reference_image"], "顶层 images 必须进素材")
	require.Equal(t, 1, counts["reference_video"], "顶层 videos 必须进素材 —— 丢了就是静默吞素材")
	require.Equal(t, 1, counts["reference_audio"], "顶层 audios 必须进素材 —— 丢了就是静默吞素材")

	// 走 media 传音视频也必须放行（上游文档明确支持）。
	require.NoError(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_video", URL: "https://e.com/v.mp4"},
	}, wan30Model), "wan-3.0 上游支持参考视频，不该被本地闸门拦下")

	// 对照组：旧标准版仍然只接受 reference_image（闸门没被误开）。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_video", URL: "https://e.com/v.mp4"},
	}, videoModel), "旧标准版 wan3.0-video 上游只接受 reference_image，必须继续拦")

	// 下发体：顶层三数组必须出现（wan-3.0-pro 是同款契约，一并验证不串味）。
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model: wan30Model, Prompt: "清晨湖泊", Duration: 10, Resolution: "720P",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://e.com/a.png"},
			{Type: "reference_video", URL: "https://e.com/v.mp4"},
			{Type: "reference_audio", URL: "https://e.com/a.mp3"},
		},
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, []any{"https://e.com/a.png"}, payload["images"])
	require.Equal(t, []any{"https://e.com/v.mp4"}, payload["videos"])
	require.Equal(t, []any{"https://e.com/a.mp3"}, payload["audios"])
	require.NotContains(t, payload, "media", "wan-3.0 走顶层三数组，不发 media")
}

// TestSJWan3PrimeContract 钉死新模型 SJ-wan3-prime 的参数契约
// （2026-11-18 实测口径）。
//
// 上游原文：
//   - 时长 2-30 秒："duration must be an integer between 2 and 30"；
//   - 分辨率仅 720P："Resolution 1080p is disabled for model SJ-wan3-prime"；
//   - 参数形态是 size：单发 resolution=720P 回 "WAN_RATIO_INVALID"，
//     而 size=720x1280 建单成功；
//   - 画幅支持 16:9 / 9:16 / 1:1 / 4:3 / 3:4（21:9 与 adaptive 不可用）。
//
// ⚠️ 文档参数表写的是「2–30 / 480P·720P·1080P」，与实测不符 —— 按实测走。
func TestSJWan3PrimeContract(t *testing.T) {
	// 时长：上游 2-30 秒，对外统一收口到 5-30 秒（老板 2026-11-18 口径）。
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel}))
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel, Duration: 2}))
	require.Equal(t, 10, resolveDuration(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel, Duration: 10}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel, Duration: 99}))

	// 对外下界 5 秒：2-4 秒必须在校验阶段被拒。
	for _, bad := range []int{2, 3, 4} {
		taskErr := validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
			Model: sjWan3PrimeModel, Duration: bad,
		})
		require.NotNil(t, taskErr, "SJ-wan3-prime %d 秒必须被拒 —— 对外统一 ≥5 秒", bad)
	}
	require.Nil(t, validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
		Model: sjWan3PrimeModel, Duration: 5,
	}))

	// 分辨率：仅 720P。1080P / 480P 显式传入必须 400（静默降档等于骗客户）。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel}))
	for _, bad := range []string{"1080P", "1080p", "480P"} {
		taskErr := validateSJWan3PrimeExplicitResolution(relaycommon.TaskSubmitReq{
			Model: sjWan3PrimeModel, Resolution: bad,
		})
		require.NotNil(t, taskErr, "SJ-wan3-prime 显式 %s 必须被拒 —— 上游只提供 720P", bad)
	}

	// 画幅：16:9 / 9:16 / 1:1 / 4:3 / 3:4 可用；21:9 与 adaptive 回退 16:9。
	for _, ratio := range []string{"16:9", "9:16", "1:1", "4:3", "3:4"} {
		require.Equal(t, ratio, resolveRatio(relaycommon.TaskSubmitReq{
			Model: sjWan3PrimeModel, Metadata: map[string]interface{}{"ratio": ratio},
		}), "SJ-wan3-prime 应支持画幅 %s", ratio)
	}
	require.Equal(t, "16:9", resolveRatio(relaycommon.TaskSubmitReq{
		Model: sjWan3PrimeModel, Metadata: map[string]interface{}{"ratio": "21:9"},
	}))

	// 请求体：size 契约 + seconds 字符串。
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model: sjWan3PrimeModel, Prompt: "金色阳光照亮雪山", Duration: 5,
	})
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "SJ-wan3-prime", payload["model"])
	require.Equal(t, "5", payload["seconds"])
	require.Equal(t, "1280x720", payload["size"])
	require.NotContains(t, payload, "resolution")
	require.NotContains(t, payload, "ratio")
}

func TestPrimeTiersAndDuration(t *testing.T) {
	// 旧 prime（wan3.0-video-prime）只有 720P / 1080P，缺省 720P（最低档，不静默升价）。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel}))
	require.Equal(t, "1080P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "1080p"}))
	// 480P 请求按最低档 720P 执行，而不是被丢掉或升到 1080P。
	require.Equal(t, "720P", resolveResolution(relaycommon.TaskSubmitReq{Model: primeModel, Resolution: "480P"}))
	// 2026-11-15 上游切换后 prime 仅 30 秒：没传补 30；显式非 30 在
	// validateExplicitDurationForModel 400，夹紧只是兜底。
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Duration: 2}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel, Seconds: "12"}))
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

// TestNewPrimeIsNotOldPrime 钉死「新 prime 逐秒可选，旧 prime 仅 30 秒」。
//
// 这是两者最容易混的一处：新 prime（SJ-wan3-prime）上游允许 2-30 秒逐秒，
// 旧 prime（wan3.0-video-prime）只接 30 秒。如果把旧 prime 的「仅 30 秒」
// 闸门套到新 prime 上，客户传 10 秒就会被本地 400 —— 明明上游能跑。
func TestNewPrimeIsNotOldPrime(t *testing.T) {
	// 旧 prime 走「仅 30 秒」闸门：10 秒显式被拒。
	require.NotNil(t, validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
		Model: primeModel, Duration: 10,
	}), "旧 prime 只接 30 秒，10 秒必须被拒")
	// 新 prime 逐秒可选：10 秒放行。
	require.Nil(t, validateExplicitDurationForModel(relaycommon.TaskSubmitReq{
		Model: sjWan3PrimeModel, Duration: 10,
	}), "新 prime 逐秒可选，10 秒必须放行")
	// 两者的默认时长也不同（旧 30 秒 / 新 5 秒）。
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Model: primeModel}))
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Model: sjWan3PrimeModel}))
	// 新 prime 不进旧 prime 的能力组（否则会继承「仅 30 秒」与素材上限）。
	require.False(t, isPrimeOnlyModel(sjWan3PrimeModel),
		"新 prime 不能被当成旧 prime 直连 —— 否则会被「仅 30 秒」闸门误伤")
	require.True(t, isPrimeOnlyModel(primeModel))
}

func TestEstimateBillingChargesResolutionTiers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	cases := []struct {
		model       string
		resolution  string
		wantSize    float64
		wantSeconds int
	}{
		// wan3.0-video：上游三档同价（0.18 ⚡/秒），售价统一 ¥0.23/秒，倍率全为 1。
		{videoModel, "480P", 1.0, 10},
		{videoModel, "720P", 1.0, 10},
		{videoModel, "1080P", 1.0, 10},
		// wan3.0-video-prime：上游 720P 0.15 / 1080P 0.20 ⚡/秒，
		// 每档加价 ¥0.10 → 售价 ¥0.25 / ¥0.30（2026-10-29 第二轮：720P 由 ¥0.20 上调），
		// 1080P 倍率 = 0.30/0.25 = 1.2。
		// 2026-11-15 起 prime 仅 30 秒：显式时长统一落 30。
		{primeModel, "720P", 1.0, 30},
		{primeModel, "1080P", 1.2, 30},
		// 480P 不在 prime 档位内，按 720P 计费（不低于成本）。
		{primeModel, "480P", 1.0, 30},
		// wan-3.0-yz（2026-11-18 新上架）：**分档价**（老板定）
		// 720P ¥0.20/秒、1080P ¥0.25/秒 → 基准档 720P 倍率 1.0，
		// 1080P 倍率 = 0.25/0.20 = 1.25；按秒计费 → 必须带 seconds。
		{wan30Model, "720P", 1.0, 10},
		{wan30Model, "1080P", 1.25, 10},
		// SJ-wan3-prime（2026-11-18 新上架）：沿用旧 prime 同价 ¥0.25/秒，
		// 上游仅 720P 一档 → 倍率恒为 1。
		{sjWan3PrimeModel, "720P", 1.0, 5},
		// 对外别名 seedance2.5-特惠：720P ¥0.20/秒，基准倍率 1.0。
		// 2026-11-20 起常驻 mw-sd2.5-flash（仅 480p/720p）—— 1080P 在 validate
		// 阶段 400，即使走到计费也已归一为 720P，倍率只会是 1.0。
		{tehuiModel, "720P", 1.0, 10},
		{tehuiModel, "1080P", 1.0, 10},
		{tehuiModel, "480P", 1.0, 10},
		// 未知档位回退 1，避免多扣费。
		{videoModel, "4K", 1.0, 10},
	}
	for _, tc := range cases {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Duration:   tc.wantSeconds,
			Resolution: tc.resolution,
		})
		info := &relaycommon.RelayInfo{OriginModelName: tc.model}
		require.Equal(t, map[string]float64{
			"seconds": float64(tc.wantSeconds),
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

// TestTehuiAliasMirrorsPrime 锁定对外别名 seedance2.5-特惠 的**参数行为**
// 与其上游执行模型（2026-11-15 起为标准版 wan3.0-video）一致：
// 720P/1080P 两档、缺省 720P、时长 10—30 秒夹紧、比例白名单含 21:9。
//
// ⚠️ 价格不在「等价」范围内：2026-10-29 老板明确特惠独立定价
// （720P ¥0.20 / 1080P ¥0.30），上游调价它不动。计费差异见
// TestTehuiPriceIsIndependent。
//
// 为什么单独立一条：别名一旦在某个分支上漏判（例如 isPrimeModel 没认它），
// 表现是「480P 请求被放行到不支持的上游」（400），不报错的另一种
// 是静默算错价。这类偏差只能靠断言钉住。
func TestTehuiMwFlashResolutionDurationRatio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// 2026-11-20：特惠常驻 mw-sd2.5-flash —— 只有 480p/720p、对客恒定交付 720P，
	// 缺省时长 5 秒、缺省画幅 16:9（上游不认历史写法 adaptive）。
	for _, tc := range []struct {
		name      string
		req       relaycommon.TaskSubmitReq
		wantRes   string
		wantDur   int
		wantRatio string
	}{
		{"default", relaycommon.TaskSubmitReq{Model: tehuiModel}, "720P", 5, "16:9"},
		// 1080P 在 validate 阶段 400（见 validatePromoResolutionForModel）；
		// 归一层只保证「绝不会把 1080P 原样发给只支持 480p/720p 的上游」。
		{"1080p normalized", relaycommon.TaskSubmitReq{Model: tehuiModel, Resolution: "1080p"}, "720P", 5, "16:9"},
		{"480p upgraded", relaycommon.TaskSubmitReq{Model: tehuiModel, Resolution: "480P"}, "720P", 5, "16:9"},
		// 促销族下界 5 秒：<5 秒夹到 5（兜底夹紧；显式越界在 validate 阶段 400），
		// ≥5 秒原样透传。
		{"duration clamped low", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 1}, "720P", 5, "16:9"},
		{"duration 4 clamped to 5", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 4}, "720P", 5, "16:9"},
		{"duration 5 kept", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 5}, "720P", 5, "16:9"},
		{"duration 8 kept", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 8}, "720P", 8, "16:9"},
		{"duration clamped high", relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 99}, "720P", 30, "16:9"},
		{"21:9 allowed", relaycommon.TaskSubmitReq{
			Model:    tehuiModel,
			Metadata: map[string]interface{}{"ratio": "21:9"},
		}, "720P", 5, "21:9"},
		{"auto allowed", relaycommon.TaskSubmitReq{Model: tehuiModel, Ratio: "auto"}, "720P", 5, "auto"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.wantRes, resolveResolution(tc.req))
			require.Equal(t, tc.wantDur, resolveDuration(tc.req))
			require.Equal(t, tc.wantRatio, resolveRatio(tc.req))
		})
	}
	// 首尾帧：mw-sd2.5-flash 支持，成对出现必须放行；孤儿尾帧仍 400。
	require.NoError(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "first_frame", URL: "https://cdn.example/f.png"},
		{Type: "last_frame", URL: "https://cdn.example/l.png"},
	}, tehuiModel))
	require.Error(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "last_frame", URL: "https://cdn.example/l.png"},
	}, tehuiModel))
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
	// 2026-11-18 上游新上架 wan-3.0 / SJ-wan3-prime 两个独立模型；
	// 旧的 wan3.0-video / wan3.0-video-prime 仍在列（老客户还在用）。
	// ⚠️ 这里列的是**上游真名** wan-3.0，不是对外公开名 wan-3.0-yz ——
	// ModelList 必须与上游 /v1/models 逐字一致，否则「同步上游模型」会把
	// 别名当成真实上游模型写回渠道配置，上游直接 404。
	// 2026-11-19 上游在售 hk-wan3（公开名 == 上游 ID），一并列入。
	// 2026-11-20 上游在售 mw-sd2.5-flash（促销族母本的常驻上游），一并列入。
	// 2026-11-25 上游在售 magi-2（公开名 == 上游 ID，多模态参考、按分辨率按条计费）。
	require.Equal(t, []string{
		"wan3.0-video", "wan3.0-video-prime", "wan-3.0-pro",
		"wan-3.0", "SJ-wan3-prime", "hk-wan3", "mw-sd2.5-flash", "magi-2",
	}, adaptor.GetModelList())
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
			"wan-3.0":            {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
			"SJ-wan3-prime":      {"720P": 1.0, "1080P": 1.2},
		}, 0)
	})
	youzanwan3_setting.SetRatios(map[string]map[string]float64{
		"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
		"wan3.0-video-prime": {"720P": 1.0, "1080P": 2.0},
		"wan-3.0":            {"480P": 1.0, "720P": 1.0, "1080P": 3.0},
		"SJ-wan3-prime":      {"720P": 1.0, "1080P": 4.0},
	}, 0)

	// prime 与它的历史旧名读 prime 的动态倍率。
	// 2026-11-15 起 prime 仅 30 秒：显式时长统一落 30。
	for _, model := range []string{primeModel, legacyPrimeModel} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 30, Resolution: "1080P"})
		info := &relaycommon.RelayInfo{OriginModelName: model}
		require.Equal(t, map[string]float64{"seconds": 30, "size": 2.0},
			(&TaskAdaptor{}).EstimateBilling(context, info), "model=%s", model)
	}
	// 新模型读**自己**的动态倍率 —— 不能串到旧 prime / 旧标准版的配置上。
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "1080P"})
	info := &relaycommon.RelayInfo{OriginModelName: wan30Model}
	require.Equal(t, map[string]float64{"seconds": 10, "size": 1.25},
		(&TaskAdaptor{}).EstimateBilling(context, info),
		"wan-3.0-yz 必须读自己的倍率 1.25（¥0.25/秒 ÷ ¥0.20/秒），不能串到旧 prime 的 2.0")

	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "720P"})
	info = &relaycommon.RelayInfo{OriginModelName: sjWan3PrimeModel}
	require.Equal(t, map[string]float64{"seconds": 10, "size": 1.0},
		(&TaskAdaptor{}).EstimateBilling(context, info),
		"SJ-wan3-prime 的 720P 倍率是 1（720P 基准档）")
	// 标准版读自己的配置，不受 prime 的倍率影响。
	context, _ = gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "1080P"})
	info = &relaycommon.RelayInfo{OriginModelName: videoModel}
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

	// 2026-11-20：特惠常驻上游换成 mw-sd2.5-flash（仅 720P），1080P 已 400。
	// 这里直接测倍率函数而不是 EstimateBilling —— 后者会把 1080P 归一成 720P，
	// 量到的是「降档后的 1.0」，钉不住分支顺序。
	require.Equal(t, 1.5, resolutionRatioForModel(tehuiModel, "1080P"),
		"特惠的倍率表必须独立于 prime：1080P 仍是自己的 1.5（0.30/0.20），不是 prime 的 2.0")

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 10, Resolution: "720P"})
	info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
	require.Equal(t, map[string]float64{"seconds": 10, "size": 1.0},
		(&TaskAdaptor{}).EstimateBilling(context, info),
		"特惠 720P 按自己的倍率 1.0 结算")
}
