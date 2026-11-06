package seeta

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func seetaTaskContext(req relaycommon.TaskSubmitReq) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", req)
	return c
}

func seetaRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
}

func seetaAdaptor(t *testing.T, model string) *TaskAdaptor {
	t.Helper()
	adaptor := &TaskAdaptor{}
	adaptor.Init(seetaRelayInfo(model))
	return adaptor
}

func seetaPayload(t *testing.T, adaptor *TaskAdaptor, req relaycommon.TaskSubmitReq) map[string]any {
	t.Helper()
	body, err := adaptor.BuildRequestBody(seetaTaskContext(req), seetaRelayInfo(req.Model))
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	return payload
}

// 上游只收 data URI，所以这里用内联图，避免测试依赖网络。
const seetaTestDataURI = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUg=="

// TestBuildRequestBodyTextToVideo 覆盖文生视频的默认载荷。
func TestBuildRequestBodyTextToVideo(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "a calm lake at sunrise",
	})

	require.Equal(t, "a calm lake at sunrise", payload["prompt"])
	require.Equal(t, "480p横", payload["resolution"])
	require.Equal(t, float64(defaultDuration), payload["duration"])
	// 文生工作流不带任何参考图字段
	require.NotContains(t, payload, "first_frame")
	require.NotContains(t, payload, "ref_image_0")
}

// TestBuildRequestBodyImageToVideoRequiresReference 覆盖图生视频的必填图约束。
func TestBuildRequestBodyImageToVideoRequiresReference(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "the red panda turns a page",
	}), seetaRelayInfo("minimax_h3-图生"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires at least one reference image")
}

// TestBuildRequestBodyImageToVideoSendsFirstFrame 图生视频首帧既发 first_frame
// 也发 ref_image_0 —— 上游两个字段名都认，发两个是为了兼容它的不同工作流分支。
func TestBuildRequestBodyImageToVideoSendsFirstFrame(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")
	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "the red panda turns a page",
		Images: []string{seetaTestDataURI},
	})

	require.Equal(t, seetaTestDataURI, payload["first_frame"])
	require.Equal(t, seetaTestDataURI, payload["ref_image_0"])
}

// TestBuildRequestBodyRejectsImagesOnTextToVideo 文生工作流不收参考图。
func TestBuildRequestBodyRejectsImagesOnTextToVideo(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "p",
		Images: []string{seetaTestDataURI},
	}), seetaRelayInfo("minimax_h3-文生"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support reference images")
}

// TestBuildRequestBodyRejectsUnsupportedResolution 非法分辨率要明确报错，
// 不能静默降档。
func TestBuildRequestBodyRejectsUnsupportedResolution(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	for _, resolution := range []string{"1080p横", "720p横", "480p", "XXX"} {
		_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
			Model:      "minimax_h3-文生",
			Prompt:     "p",
			Resolution: resolution,
		}), seetaRelayInfo("minimax_h3-文生"))
		require.Error(t, err, "resolution %q 应该被拒绝", resolution)
		require.Contains(t, err.Error(), "does not support resolution")
	}
}

// TestBuildRequestBodyDerivesResolutionFromSize 用 OpenAI 风格的 size 推导档位。
func TestBuildRequestBodyDerivesResolutionFromSize(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	cases := []struct {
		size string
		want string
	}{
		{"1280x720", "768p横"},
		{"832x480", "480p横"},
		{"480x832", "480p竖"},
		{"720x720", "768p(1:1)"},
	}
	for _, tc := range cases {
		payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
			Model:  "minimax_h3-文生",
			Prompt: "p",
			Size:   tc.size,
		})
		require.Equal(t, tc.want, payload["resolution"], "size %s", tc.size)
	}
}

// TestBuildRequestBodyClampsDuration 时长越界要夹取到上游支持的区间。
func TestBuildRequestBodyClampsDuration(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:    "minimax_h3-文生",
		Prompt:   "p",
		Duration: 30,
	})
	require.Equal(t, float64(maxDuration), payload["duration"])

	payload = seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:    "minimax_h3-文生",
		Prompt:   "p",
		Duration: 0,
		Seconds:  "7",
	})
	require.Equal(t, float64(7), payload["duration"])
}

// TestBuildRequestBodyForwardsSeed seed 透传给上游。
func TestBuildRequestBodyForwardsSeed(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	seed := int64(123456)

	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "p",
		Seed:   &seed,
	})
	require.Equal(t, float64(seed), payload["seed"])
}

// TestBuildRequestBodyRejectsNonDataURIRemoteImage 远程图要走下载转 data URI，
// 但非 http(s) 的本地路径必须在本地拦掉。
func TestBuildRequestBodyRejectsNonDataURIRemoteImage(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
		Images: []string{"/tmp/local.png"},
	}), seetaRelayInfo("minimax_h3-图生"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be an http(s) URL or a data URI")
}

// TestEstimateBillingResolutionRatios 计费倍率：480P 基准 1.0，768p 1.667。
func TestEstimateBillingResolutionRatios(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "minimax_h3-文生",
		Prompt:     "p",
		Duration:   10,
		Resolution: "480p横",
	})
	billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3-文生"))
	require.Equal(t, float64(10), billing["seconds"])
	require.Equal(t, 1.0, billing["size"])

	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "minimax_h3-文生",
		Prompt:     "p",
		Duration:   10,
		Resolution: "768p横",
	})
	billing = adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3-文生"))
	require.Equal(t, 10.0, billing["seconds"])
	require.InDelta(t, 1.667, billing["size"], 0.001)
}

// TestParseTaskResultStatuses 覆盖上游的状态枚举映射。
func TestParseTaskResultStatuses(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	cases := []struct {
		name       string
		body       string
		wantStatus string
	}{
		{"pending", `{"data":{"task_id":"t","status":"PENDING","results":[]}}`, model.TaskStatusQueued},
		{"running", `{"data":{"task_id":"t","status":"RUNNING","results":[]}}`, model.TaskStatusInProgress},
		{"success", `{"data":{"task_id":"t","status":"SUCCESS","results":[{"url":"https://x/a.mp4"}]}}`, model.TaskStatusSuccess},
		{"failed", `{"data":{"task_id":"t","status":"FAILED","message":"boom","results":[]}}`, model.TaskStatusFailure},
		{"error branch", `{"error":{"message":"boom","type":"invalid_request_error"}}`, model.TaskStatusFailure},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := adaptor.ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			require.Equal(t, tc.wantStatus, result.Status)
		})
	}
}

// TestParseTaskResultSuccessWithoutResultKeepsPolling 已成功但成片还没转存时，
// 不能误报成功。
func TestParseTaskResultSuccessWithoutResultKeepsPolling(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	result, err := adaptor.ParseTaskResult([]byte(`{"data":{"task_id":"t","status":"SUCCESS","results":[]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, result.Status)
}

// TestParseTaskResultSuccessReturnsURL 成片 URL 要落到 TaskInfo.Url。
func TestParseTaskResultSuccessReturnsURL(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	result, err := adaptor.ParseTaskResult([]byte(
		`{"data":{"task_id":"t","status":"SUCCESS","results":[{"url":"https://files/a.mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "https://files/a.mp4", result.Url)
}

// TestParseTaskResultUnknownStatusKeepsPolling 未知状态保守继续轮询。
func TestParseTaskResultUnknownStatusKeepsPolling(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	result, err := adaptor.ParseTaskResult([]byte(`{"data":{"task_id":"t","status":"WEIRD","results":[]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, result.Status)
}

// TestBuildRequestURLUsesWorkflowPath 提交 URL 必须带上游工作流名。
func TestBuildRequestURLUsesWorkflowPath(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	adaptor.baseURL = "https://example.test:8443"

	url, err := adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3-文生"))
	require.NoError(t, err)
	require.Equal(t, "https://example.test:8443/api/v1/comfyui/comfyui_workflow/minimax_h3_lightx2v_no_pic", url)

	adaptor = seetaAdaptor(t, "minimax_h3-图生")
	adaptor.baseURL = "https://example.test:8443"
	url, err = adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3-图生"))
	require.NoError(t, err)
	require.Equal(t, "https://example.test:8443/api/v1/comfyui/comfyui_workflow/minimax_h3_lightx2v", url)
}

// TestBuildRequestHeaderUsesRawToken 上游是裸 token 鉴权，不带 Bearer。
func TestBuildRequestHeaderUsesRawToken(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	adaptor.apiKey = "raw-token"

	req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	require.NoError(t, adaptor.BuildRequestHeader(nil, req, seetaRelayInfo("minimax_h3-文生")))
	require.Equal(t, "raw-token", req.Header.Get("Authorization"))
}

// TestGetModelList 暴露的模型名不含上游工作流名。
func TestGetModelList(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	list := adaptor.GetModelList()
	require.Contains(t, list, "minimax_h3-文生")
	require.Contains(t, list, "minimax_h3-图生")
	for _, name := range list {
		require.NotContains(t, name, "lightx2v")
	}
}

// TestValidateDataURI 非法 data URI 要在本地拦掉。
func TestValidateDataURI(t *testing.T) {
	require.NoError(t, validateDataURI(seetaTestDataURI))
	require.Error(t, validateDataURI("data:text/plain;base64,aGk="))
	require.Error(t, validateDataURI("not-a-data-uri"))
}

// TestDetectImageMime 靠魔数识别图片类型。
func TestDetectImageMime(t *testing.T) {
	require.Equal(t, "image/png", detectImageMime([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}))
	require.Equal(t, "image/jpeg", detectImageMime([]byte{0xFF, 0xD8, 0xFF}))
}

// TestCollectReferenceImages 兼容 images / image / input_reference / media 四种入口。
func TestCollectReferenceImages(t *testing.T) {
	require.Equal(t, []string{"a"}, collectReferenceImages(relaycommon.TaskSubmitReq{Images: []string{" a "}}))
	require.Equal(t, []string{"b"}, collectReferenceImages(relaycommon.TaskSubmitReq{Image: "b"}))
	require.Equal(t, []string{"c"}, collectReferenceImages(relaycommon.TaskSubmitReq{InputReference: "c"}))
	require.Equal(t, []string{"d"}, collectReferenceImages(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{{Type: "image", URL: "d"}},
	}))
	require.Empty(t, collectReferenceImages(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{{Type: "video", URL: "v"}},
	}))
}

// TestExtractTierValue 解析上游的分辨率 token。
func TestExtractTierValue(t *testing.T) {
	require.Equal(t, 480, extractTierValue("480p横"))
	require.Equal(t, 768, extractTierValue("768p竖"))
	require.Equal(t, 768, extractTierValue("768p(1:1)"))
	require.Equal(t, 0, extractTierValue("bad"))
}

// TestTruncateForError 长 base64 不能整段打进错误信息。
func TestTruncateForError(t *testing.T) {
	long := strings.Repeat("A", 100)
	got := truncateForError(long)
	require.Less(t, len(got), len(long))
	require.True(t, strings.HasSuffix(got, "..."))
	require.Equal(t, "short", truncateForError("short"))
}

// TestModelListMatchesWorkflowConfig 每个对外模型都要有工作流配置。
func TestModelListMatchesWorkflowConfig(t *testing.T) {
	for _, name := range ModelList {
		cfg, ok := workflowByModel[name]
		require.True(t, ok, "model %s 缺少工作流配置", name)
		require.NotEmpty(t, cfg.WorkflowID)
		require.NotEmpty(t, cfg.Resolutions)
	}
}

// TestWorkflowResolutionsCoverRatios 每个允许的分辨率都要有计费倍率。
func TestWorkflowResolutionsCoverRatios(t *testing.T) {
	for name, cfg := range workflowByModel {
		for _, resolution := range cfg.Resolutions {
			ratio, ok := cfg.ResolutionRatios[resolution]
			require.True(t, ok, "model %s 的分辨率 %s 缺少计费倍率", name, resolution)
			require.Greater(t, ratio, 0.0)
		}
	}
}

// TestPricePerSecondMatchesBasePrice 基础价常量要与价格表一致，避免两边各写一份。
func TestPricePerSecondMatchesBasePrice(t *testing.T) {
	require.InDelta(t, 0.03, pricePerSecondCNY, 0.0001)
}

// TestDurationBounds 时长区间与上游一致（1-15 秒）。
func TestDurationBounds(t *testing.T) {
	require.Equal(t, 1, minDuration)
	require.Equal(t, 15, maxDuration)
	for name, cfg := range workflowByModel {
		require.LessOrEqual(t, cfg.MaxDuration, maxDuration, "model %s 的最大时长超过上游上限", name)
	}
}

// TestIndexNumbersAreStable 索引字段拼接要与 strconv 一致（防重构踩坑）。
func TestIndexNumbersAreStable(t *testing.T) {
	require.Equal(t, "ref_image_0", "ref_image_"+strconv.Itoa(0))
	require.Equal(t, "ref_image_8", "ref_image_"+strconv.Itoa(8))
}
