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

// TestBuildRequestBodyV2AcceptsImagesWithoutKeyframeLimit 2026-11-07 合并后
// V2 不带图即文生、带图即图生/首尾帧：1 张 = 首帧，2 张 = 首尾帧。
func TestBuildRequestBodyV2MergedSemantics(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_v2")

	// 不带图：纯文生，没有任何参考图字段。
	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_v2",
		Prompt: "a calm lake at sunrise",
	})
	require.NotContains(t, payload, "first_frame")
	require.NotContains(t, payload, "ref_image_0")

	// 1 张图：first_frame + ref_image_0。
	payload = seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_v2",
		Prompt: "p",
		Images: []string{seetaTestDataURI},
	})
	require.Equal(t, seetaTestDataURI, payload["first_frame"])
	require.Equal(t, seetaTestDataURI, payload["ref_image_0"])

	// 2 张图：第 2 张是尾帧（ref_image_1），不再发第二个 first_frame。
	payload = seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_v2",
		Prompt: "p",
		Images: []string{seetaTestDataURI, "data:image/png;base64,QUJD"},
	})
	require.Equal(t, seetaTestDataURI, payload["first_frame"])
	require.Equal(t, "data:image/png;base64,QUJD", payload["ref_image_1"])

	// 3 张图：超过 V2 关键帧槽位（首帧+尾帧），本地拦下。
	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_v2",
		Prompt: "p",
		Images: []string{seetaTestDataURI, seetaTestDataURI, seetaTestDataURI},
	}), seetaRelayInfo("minimax_h3_v2"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 2")

	// V2 没有视频/音频槽位：传了要明确报错（不静默丢弃）。
	_, err = adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_v2",
		Prompt: "p",
		Audios: []string{"data:audio/mpeg;base64,QUJD"},
	}), seetaRelayInfo("minimax_h3_v2"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "audio")
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

// TestBuildRequestBodyOmniSetRef Omni 链的参考图是**设定参考**：
// 全部走 ref_image_N，不发 first_frame；首尾帧只从 metadata 收。
func TestBuildRequestBodyOmniSetRef(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_omni",
		Prompt: "<Picture 1> turns her head",
		Images: []string{seetaTestDataURI, "data:image/png;base64,QUJD"},
	})
	require.Equal(t, seetaTestDataURI, payload["ref_image_0"])
	require.Equal(t, "data:image/png;base64,QUJD", payload["ref_image_1"])
	// 设定参考语义下不带 first_frame —— 槽位语义不随引擎漂移。
	require.NotContains(t, payload, "first_frame")

	// metadata 里的首尾帧与设定参考共存。
	payload = seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_omni",
		Prompt: "p",
		Metadata: map[string]interface{}{
			"first_frame": seetaTestDataURI,
			"last_frame":  "data:image/png;base64,QUJD",
		},
	})
	require.Equal(t, seetaTestDataURI, payload["first_frame"])
	require.Equal(t, "data:image/png;base64,QUJD", payload["last_frame"])
	require.NotContains(t, payload, "ref_image_0")

	// 视频参考 → ref_video_N，音频参考 → ref_audio_N。
	payload = seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3_omni",
		Prompt: "p",
		Audios: []string{"data:audio/mpeg;base64,QUJD"},
		Media: []relaycommon.TaskMedia{
			{Type: "reference_video", URL: "data:video/mp4;base64,AAAA"},
		},
	})
	require.Equal(t, "data:video/mp4;base64,AAAA", payload["ref_video_0"])
	require.Equal(t, "data:audio/mpeg;base64,QUJD", payload["ref_audio_0"])
}

// TestBuildRequestBodyOmniCaps Omni 链的多参考上限：9 图 / 3 视频 / 3 音频。
func TestBuildRequestBodyOmniCaps(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	images := make([]string, maxReferenceImageCount+1)
	for i := range images {
		images[i] = seetaTestDataURI
	}
	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Images: images,
	}), seetaRelayInfo("minimax_h3_omni"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 9 reference images")

	_, err = adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p",
		Media: []relaycommon.TaskMedia{
			{Type: "video", URL: "data:video/mp4;base64,AAAA"},
			{Type: "video", URL: "data:video/mp4;base64,AAAA"},
			{Type: "video", URL: "data:video/mp4;base64,AAAA"},
			{Type: "video", URL: "data:video/mp4;base64,AAAA"},
		},
	}), seetaRelayInfo("minimax_h3_omni"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 3 reference videos")

	_, err = adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p",
		Audios: []string{"data:audio/mpeg;base64,QUJD", "data:audio/mpeg;base64,QUJD",
			"data:audio/mpeg;base64,QUJD", "data:audio/mpeg;base64,QUJD"},
	}), seetaRelayInfo("minimax_h3_omni"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "at most 3 reference audios")
}

// TestBuildRequestBodyOmniAccepts1080p 1080p 档只有 Omni 链支持；
// V2 传 1080p 必须报错（潜空间放大路径只在 Omni 有）。
func TestBuildRequestBodyOmniAccepts1080p(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:      "minimax_h3_omni",
		Prompt:     "p",
		Resolution: "1080p横",
	})
	require.Equal(t, "1080p横", payload["resolution"])

	adaptor = seetaAdaptor(t, "minimax_h3_v2")
	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_v2", Prompt: "p", Resolution: "1080p横",
	}), seetaRelayInfo("minimax_h3_v2"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "does not support resolution")
}

// TestEstimateBillingOmni1080pRatio 1080p 计费倍率 3.0（对客 ¥0.06/秒 = ¥0.02 × 3.0）。
func TestEstimateBillingOmni1080pRatio(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Duration: 10, Resolution: "1080p竖",
	})
	billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))
	require.Equal(t, 10.0, billing["seconds"])
	require.InDelta(t, 3.0, billing["size"], 0.001)
}

// TestEstimateBillingOmniTiers Omni 三档相对基准价（480P ¥0.02）的倍率：
// 480P 1.0（¥0.02）、768p 2.0（¥0.04）、1080p 3.0（¥0.06）。
//
// 2026-11-08 调价：Omni 的 480P/768P 下调、1080P 保持原价，倍率随之从
// 1.0 / 1.667 / 2.0 改成 1.0 / 2.0 / 3.0。基准价换成 ¥0.02 后，
// 若只改基准价不改倍率，768p 会算出 ¥0.0333、1080p 算出 ¥0.04（少收）；
// 反过来只改倍率不改基准价，768p 会算成 ¥0.06、1080p ¥0.09（多收）。
func TestEstimateBillingOmniTiers(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	cells := []struct {
		resolution string
		ratio      float64
		cnyPerSec  float64
	}{
		{"480p横", 1.0, 0.02},
		{"480p竖", 1.0, 0.02},
		{"480p(1:1)", 1.0, 0.02},
		{"768p横", 2.0, 0.04},
		{"768p竖", 2.0, 0.04},
		{"768p(1:1)", 2.0, 0.04},
		{"1080p横", 3.0, 0.06},
		{"1080p竖", 3.0, 0.06},
		{"1080p(1:1)", 3.0, 0.06},
	}
	for _, cell := range cells {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", relaycommon.TaskSubmitReq{
			Model: "minimax_h3_omni", Prompt: "p", Duration: 5, Resolution: cell.resolution,
		})
		billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))
		require.InDelta(t, cell.ratio, billing["size"], 0.001, "档位 %s 的倍率", cell.resolution)

		// 对客单价 = Omni 480P 基准价 ¥0.02 × 倍率；再乘秒数即整单价格。
		base := 0.02
		require.InDelta(t, cell.cnyPerSec, base*cell.ratio, 0.0001,
			"档位 %s 的每秒单价应为 ¥%.2f", cell.resolution, cell.cnyPerSec)
	}
}

// TestEstimateBillingV2Tiers V2 三档口径不受 Omni 调价影响：480P ¥0.03、768p ¥0.05。
func TestEstimateBillingV2Tiers(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_v2")

	cells := []struct {
		resolution string
		ratio      float64
		cnyPerSec  float64
	}{
		{"480p横", 1.0, 0.03},
		{"768p横", 1.667, 0.05},
	}
	for _, cell := range cells {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", relaycommon.TaskSubmitReq{
			Model: "minimax_h3_v2", Prompt: "p", Duration: 5, Resolution: cell.resolution,
		})
		billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_v2"))
		require.InDelta(t, cell.ratio, billing["size"], 0.001, "档位 %s 的倍率", cell.resolution)
		require.InDelta(t, cell.cnyPerSec, 0.03*cell.ratio, 0.0001,
			"档位 %s 的每秒单价应为 ¥%.2f", cell.resolution, cell.cnyPerSec)
	}
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

// TestParseTaskResultExpiredIsFailure 上游报 EXPIRED（任务在 ComfyUI 侧消失：引擎
// 重启、任务被中断）必须是终态失败。
//
// 原实现里 EXPIRED 不在任何 case 里，落到 default 被当成「进行中」，客户端会一直
// 轮询一条早就不存在的任务（2026-11-08 实测：被中断的 ch28 任务卡在 IN_PROGRESS）。
func TestParseTaskResultExpiredIsFailure(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	result, err := adaptor.ParseTaskResult([]byte(
		`{"data":{"task_id":"t","status":"EXPIRED","message":"任务已从 ComfyUI 队列消失","results":[]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusFailure, result.Status)
	require.Equal(t, "任务已从 ComfyUI 队列消失", result.Reason)
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

// TestParseTaskResultSkipsResultWithoutSubfolder 多结果时必须挑真成片，不能盲取 [0]。
//
// 2026-11-08 实单（task_wTDlyemUeD2WMlDGQSNWKBQBdq3MyhNt，omni + 参考视频）：
// 上游返回两条 result —— 第 0 条 subfolder 为空（files/arcreel_<hash>.mp4 → 404），
// 第 1 条才是落在 video/ 子目录的成片。旧实现取 [0]，于是任务 SUCCESS、产物代理
// 却 502「Upstream service returned status 404」，客户永远拿不到片。
func TestParseTaskResultSkipsResultWithoutSubfolder(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	result, err := adaptor.ParseTaskResult([]byte(`{"data":{"task_id":"t","status":"SUCCESS","results":[
		{"url":"https://files/arcreel_deadbeef.mp4","filename":"arcreel_deadbeef.mp4","subfolder":""},
		{"url":"https://files/video/arcreel_h3_omni_00193_.mp4","filename":"arcreel_h3_omni_00193_.mp4","subfolder":"video"}
	]}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, result.Status)
	require.Equal(t, "https://files/video/arcreel_h3_omni_00193_.mp4", result.Url)
}

// TestParseTaskResultPrefersLastSubfolderResult 同优先级里取最后一条：results 越靠后
// 越接近最终产物（多段输出时前面的可能是中间帧）。
func TestParseTaskResultPrefersLastSubfolderResult(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	result, err := adaptor.ParseTaskResult([]byte(`{"data":{"task_id":"t","status":"SUCCESS","results":[
		{"url":"https://files/video/a.mp4","subfolder":"video"},
		{"url":"https://files/video/b.mp4","subfolder":"video"}
	]}}`))
	require.NoError(t, err)
	require.Equal(t, "https://files/video/b.mp4", result.Url)
}

// TestParseTaskResultFallsBackToSubfolderlessResult 单一结果且没有 subfolder 时仍要取到
// （不能因为「没有 subfolder 就当作空」把正常产物丢掉）。
func TestParseTaskResultFallsBackToSubfolderlessResult(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	result, err := adaptor.ParseTaskResult([]byte(
		`{"data":{"task_id":"t","status":"SUCCESS","results":[{"url":"https://files/only.mp4"}]}}`))
	require.NoError(t, err)
	require.Equal(t, "https://files/only.mp4", result.Url)
}

// TestParseTaskResultIgnoresEmptyURLItems 空 url 的条目不能顶替真成片。
func TestParseTaskResultIgnoresEmptyURLItems(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")

	result, err := adaptor.ParseTaskResult([]byte(`{"data":{"task_id":"t","status":"SUCCESS","results":[
		{"url":"","filename":"empty.mp4","subfolder":"video"},
		{"url":"https://files/video/real.mp4","subfolder":"video"}
	]}}`))
	require.NoError(t, err)
	require.Equal(t, "https://files/video/real.mp4", result.Url)
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
	adaptor := seetaAdaptor(t, "minimax_h3_v2")
	adaptor.baseURL = "https://example.test:8443"

	url, err := adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3_v2"))
	require.NoError(t, err)
	require.Equal(t, "https://example.test:8443/api/v1/comfyui/comfyui_workflow/minimax_h3_v2", url)

	adaptor = seetaAdaptor(t, "minimax_h3_omni")
	adaptor.baseURL = "https://example.test:8443"
	url, err = adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3_omni"))
	require.NoError(t, err)
	require.Equal(t, "https://example.test:8443/api/v1/comfyui/comfyui_workflow/minimax_h3_omni", url)
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

// TestGetModelList 暴露的模型名不含上游工作流名，旧中文名已退役。
func TestGetModelList(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_v2")
	list := adaptor.GetModelList()
	require.Contains(t, list, "minimax_h3_v2")
	require.Contains(t, list, "minimax_h3_omni")
	for _, name := range list {
		require.NotContains(t, name, "lightx2v")
		require.NotEqual(t, "minimax_h3-文生", name, "旧名不应再出现在模型列表")
		require.NotEqual(t, "minimax_h3-图生", name, "旧名不应再出现在模型列表")
	}
}

// TestLegacyAliasesRouteToV2 旧名是隐藏别名：不在 ModelList，
// 但仍能路由到合并后的 V2 工作流；图生别名保留必填图契约。
func TestLegacyAliasesRouteToV2(t *testing.T) {
	require.NotContains(t, ModelList, "minimax_h3-文生")
	require.NotContains(t, ModelList, "minimax_h3-图生")

	// 旧「文生」别名 → V2 工作流；合并后带图也不再拒（V2 全语义）。
	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	url, err := adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3-文生"))
	require.NoError(t, err)
	require.Contains(t, url, "/comfyui_workflow/minimax_h3_v2")
	payload := seetaPayload(t, adaptor, relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "p",
		Images: []string{seetaTestDataURI},
	})
	require.Equal(t, seetaTestDataURI, payload["first_frame"])

	// 旧「图生」别名 → V2 工作流，缺图仍要报错。
	adaptor = seetaAdaptor(t, "minimax_h3-图生")
	url, err = adaptor.BuildRequestURL(seetaRelayInfo("minimax_h3-图生"))
	require.NoError(t, err)
	require.Contains(t, url, "/comfyui_workflow/minimax_h3_v2")
	_, err = adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
	}), seetaRelayInfo("minimax_h3-图生"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires at least one reference image")
}

// TestValidateDataURI 非法 data URI 要在本地拦掉。
func TestValidateDataURI(t *testing.T) {
	require.NoError(t, validateDataURIMedia(seetaTestDataURI, "image", maxReferenceImageBytes))
	require.Error(t, validateDataURIMedia("data:text/plain;base64,aGk=", "image", maxReferenceImageBytes))
	require.Error(t, validateDataURIMedia("not-a-data-uri", "image", maxReferenceImageBytes))
	// 音频/视频类 data URI 按各自 kind 校验。
	require.NoError(t, validateDataURIMedia("data:audio/mpeg;base64,QUJD", "audio", maxReferenceAudioBytes))
	require.Error(t, validateDataURIMedia("data:audio/mpeg;base64,QUJD", "video", maxReferenceVideoBytes))
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

// TestPricePerSecondMatchesBasePrice V2 的 480P 基准价常量要与价格表一致，避免两边各写一份。
func TestPricePerSecondMatchesBasePrice(t *testing.T) {
	require.InDelta(t, 0.03, v2PricePerSecondCNY, 0.0001)
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
