package seeta

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seetaUserErr 断言 err 是「请求不合法」的用户错误（relay_task 会转成 400）。
//
// 背景：relay_task.go 只对 taskcommon.UserError 降级成 400 LocalError，
// 其他 error 一律 500 build_request_failed —— 500 会触发重试与 AutoBan，
// 客户传错参数就能把整条渠道打掉。
func seetaUserErr(t *testing.T, err error) *taskcommon.UserError {
	t.Helper()
	require.Error(t, err)
	var userErr *taskcommon.UserError
	require.True(t, errors.As(err, &userErr),
		"应当是 taskcommon.UserError（→400），实际是 %T: %v", err, err)
	return userErr
}

// TestBuildRequestBodyUserErrorForInvalidResolution 非法分辨率必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForInvalidResolution(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:      "minimax_h3-文生",
		Prompt:     "p",
		Resolution: "1080p横",
	}), seetaRelayInfo("minimax_h3-文生"))

	userErr := seetaUserErr(t, err)
	assert.Equal(t, "invalid_resolution", userErr.Code)
}

// TestBuildRequestBodyUserErrorForMissingPrompt 缺 prompt 必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForMissingPrompt(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3-文生",
	}), seetaRelayInfo("minimax_h3-文生"))

	userErr := seetaUserErr(t, err)
	assert.Equal(t, "invalid_request", userErr.Code)
}

// TestBuildRequestBodyUserErrorForUnsupportedImages 文生传参考图必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForUnsupportedImages(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "p",
		Images: []string{seetaTestDataURI},
	}), seetaRelayInfo("minimax_h3-文生"))

	userErr := seetaUserErr(t, err)
	assert.Equal(t, "invalid_request", userErr.Code)
}

// TestBuildRequestBodyUserErrorForMissingReferenceImage 图生缺参考图必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForMissingReferenceImage(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
	}), seetaRelayInfo("minimax_h3-图生"))

	userErr := seetaUserErr(t, err)
	assert.Equal(t, "invalid_request", userErr.Code)
}

// TestBuildRequestBodyUserErrorForTooManyImages 参考图超限必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForTooManyImages(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	images := make([]string, maxReferenceImageCount+1)
	for i := range images {
		images[i] = seetaTestDataURI
	}

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
		Images: images,
	}), seetaRelayInfo("minimax_h3-图生"))

	userErr := seetaUserErr(t, err)
	assert.Equal(t, "invalid_request", userErr.Code)
}

// TestBuildRequestBodyUserErrorForBadReferenceImage 参考图格式非法必须是 400 类用户错误。
func TestBuildRequestBodyUserErrorForBadReferenceImage(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
		Images: []string{"data:text/plain;base64,QUJD"},
	}), seetaRelayInfo("minimax_h3-图生"))

	seetaUserErr(t, err)
}

// TestBuildRequestBodyRejectsAudioMaterials H3 工作流没有音频槽位：
// 客户传 audio 时必须明确报错，不能静默丢弃（静默丢弃 = 客户以为做了配音）。
func TestBuildRequestBodyRejectsAudioMaterials(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-文生",
		Prompt: "p",
		Audios: []string{"https://example.com/voice.mp3"},
	}), seetaRelayInfo("minimax_h3-文生"))

	userErr := seetaUserErr(t, err)
	assert.Contains(t, userErr.Message, "audio")
}

// TestBuildRequestBodyRejectsVideoMaterials H3 工作流也没有视频参考槽位。
func TestBuildRequestBodyRejectsVideoMaterials(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
		Images: []string{seetaTestDataURI},
		Media: []relaycommon.TaskMedia{
			{Type: "reference_video", URL: "https://example.com/clip.mp4"},
		},
	}), seetaRelayInfo("minimax_h3-图生"))

	userErr := seetaUserErr(t, err)
	assert.Contains(t, userErr.Message, "video")
}

// TestBuildRequestBodyRejectsTooManyReferenceImagesForUpstream 上游工作流的关键帧
// 槽位上限是 2 张（首帧 + 尾帧），传更多必须报错而不是让上游默默忽略。
func TestBuildRequestBodyRejectsTooManyReferenceImagesForUpstream(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-图生")

	_, err := adaptor.BuildRequestBody(seetaTaskContext(relaycommon.TaskSubmitReq{
		Model:  "minimax_h3-图生",
		Prompt: "p",
		Images: []string{seetaTestDataURI, seetaTestDataURI, seetaTestDataURI},
	}), seetaRelayInfo("minimax_h3-图生"))

	userErr := seetaUserErr(t, err)
	assert.Contains(t, userErr.Message, "at most 2")
}

// TestDoResponseEchoesRequestedResolution 提交回执的 size 必须回显**实际解析出的**档位，
// 而不是工作流默认值 —— 客户传 768p竖 时回执说 480p横 属于对客口径错误。
func TestDoResponseEchoesRequestedResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Model:      "minimax_h3-文生",
		Prompt:     "p",
		Duration:   3,
		Resolution: "768p竖",
	})
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader("{}"))

	adaptor := seetaAdaptor(t, "minimax_h3-文生")
	info := seetaRelayInfo("minimax_h3-文生")
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{PublicTaskID: "task_test"}

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"data":{"task_id":"up-123"}}`)),
	}

	_, _, taskErr := adaptor.DoResponse(c, resp, info)
	require.Nil(t, taskErr)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &payload))
	assert.Equal(t, "768p竖", payload["size"])
	assert.Equal(t, "3", payload["seconds"])
}

// TestConvertToOpenAIVideoUsesTaskStatus 查询态的 status 必须以任务状态为基准。
//
// 背景：提交后 task.Data 存的是**提交回执**（没有上游 status 字段），
// 只有轮询同步过之后 task.Data 才是上游轮询响应。轮询前查任务会返回
// status=unknown，客户会以为任务挂了。
func TestConvertToOpenAIVideoUsesTaskStatus(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	task := &model.Task{
		TaskID:    "task_test",
		Status:    model.TaskStatusInProgress,
		CreatedAt: 100,
		UpdatedAt: 150,
		Progress:  "30%",
		Properties: model.Properties{
			OriginModelName: "minimax_h3-文生",
		},
		// 提交回执：没有上游 status 字段。
		Data: json.RawMessage(`{"data":{"task_id":"up-123"}}`),
	}

	out, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, json.Unmarshal(out, &video))
	assert.Equal(t, dto.VideoStatusInProgress, video.Status)
	assert.Equal(t, 30, video.Progress)
}

// TestConvertToOpenAIVideoNotStartIsQueued 任务刚提交、轮询还没跑过时，
// 库里状态是 NOT_START —— 它和 QUEUED 一样是「已受理在排队」，
// 不能报 unknown（客户提交完立刻查一次会以为任务挂了）。
func TestConvertToOpenAIVideoNotStartIsQueued(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	for _, status := range []model.TaskStatus{
		model.TaskStatusNotStart,
		model.TaskStatusSubmitted,
		model.TaskStatusQueued,
	} {
		task := &model.Task{
			TaskID: "task_test",
			Status: status,
			Data:   json.RawMessage(`{"data":{"task_id":"up-123"}}`),
		}

		out, err := adaptor.ConvertToOpenAIVideo(task)
		require.NoError(t, err)

		var video dto.OpenAIVideo
		require.NoError(t, json.Unmarshal(out, &video))
		assert.Equal(t, dto.VideoStatusQueued, video.Status, "status=%s", status)
	}
}

// TestConvertToOpenAIVideoUsesProxyURL 查询态的 metadata.url 必须是站内代理地址，
// 不能把上游实例地址（u113...seetacloud.com:8443）直接吐给客户 ——
// 那等于把自建实例的入口暴露出去。
func TestConvertToOpenAIVideoUsesProxyURL(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3-文生")

	task := &model.Task{
		TaskID: "task_test",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "minimax_h3-文生",
		},
		Data: json.RawMessage(`{"data":{"status":"SUCCESS","results":[{"url":"https://u1133789-a936-c07acd4f.westd.seetacloud.com:8443/files/video/x.mp4"}]}}`),
	}

	out, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, json.Unmarshal(out, &video))
	assert.Equal(t, dto.VideoStatusCompleted, video.Status)

	url, _ := video.Metadata["url"].(string)
	assert.Contains(t, url, "/v1/videos/task_test/content")
	assert.NotContains(t, url, "seetacloud.com")
}
