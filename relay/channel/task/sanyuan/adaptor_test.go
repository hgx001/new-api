/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package sanyuan

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 时长是 1..30 的自由整数（wanwan duration_options=[]，上游区间内每一秒都接受），
// 与逐秒透传的 seedance 系口径一致：区间外一律 400，不 clamp。
// 静默吸附到 30 会让用户按 31 秒的期待拿到 30 秒成片，属于参数语义被偷改。
func TestResolveDurationRejectsOutOfRange(t *testing.T) {
	tests := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr string
	}{
		{name: "缺省走默认 5 秒", req: relaycommon.TaskSubmitReq{}, want: 5},
		{name: "下界 1 秒", req: relaycommon.TaskSubmitReq{Duration: 1}, want: 1},
		{name: "区间内 15 秒", req: relaycommon.TaskSubmitReq{Duration: 15}, want: 15},
		{name: "上界 30 秒", req: relaycommon.TaskSubmitReq{Duration: 30}, want: 30},
		{name: "31 秒拒绝", req: relaycommon.TaskSubmitReq{Duration: 31}, wantErr: "accepts 1-30"},
		{name: "负数当未传走默认", req: relaycommon.TaskSubmitReq{Duration: -5, Seconds: "12"}, want: 12},
		{name: "seconds 字符串形状", req: relaycommon.TaskSubmitReq{Seconds: "7"}, want: 7},
		{name: "seconds 30", req: relaycommon.TaskSubmitReq{Seconds: "30"}, want: 30},
		{name: "seconds 0 拒绝", req: relaycommon.TaskSubmitReq{Seconds: "0"}, wantErr: "accepts 1-30"},
		{name: "seconds 45 拒绝", req: relaycommon.TaskSubmitReq{Seconds: "45"}, wantErr: "accepts 1-30"},
		{name: "Duration 优先于 seconds", req: relaycommon.TaskSubmitReq{Duration: 3, Seconds: "9"}, want: 3},
		{name: "非法 seconds 回默认", req: relaycommon.TaskSubmitReq{Seconds: "abc"}, want: 5},
		{name: "空 seconds 回默认", req: relaycommon.TaskSubmitReq{Seconds: "  "}, want: 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDuration(tt.req)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// 按次计费（拼车批次制，与时长/分辨率无关），EstimateBilling 必须返回 nil，
// 否则会把时长当倍率叠进按次单价 —— 重蹈 TASK_PRICE_PATCH 跳过相乘的事故形态。
func TestEstimateBillingReturnsNilPerCall(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", relaycommon.TaskSubmitReq{Prompt: "x", Duration: 12, Resolution: "1080p"})
	billing := (&TaskAdaptor{}).EstimateBilling(ctx, &relaycommon.RelayInfo{OriginModelName: PublicModel})
	require.Nil(t, billing)
}

func TestResolveResolution(t *testing.T) {
	tests := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr string
	}{
		{name: "缺省走 480p竖", req: relaycommon.TaskSubmitReq{}, want: defaultResolution + directionPortrait},
		{name: "裸 1080p 无提示默认竖", req: relaycommon.TaskSubmitReq{Resolution: "1080p"}, want: "1080p竖"},
		{name: "大小写归一 480P", req: relaycommon.TaskSubmitReq{Resolution: "480P"}, want: "480p竖"},
		{name: "完整档位 720p横", req: relaycommon.TaskSubmitReq{Resolution: "720p横"}, want: "720p横"},
		{name: "完整档位大小写归一", req: relaycommon.TaskSubmitReq{Resolution: "480P竖"}, want: "480p竖"},
		{name: "tier + ratio 9:16", req: relaycommon.TaskSubmitReq{Resolution: "1080p", Metadata: map[string]interface{}{"ratio": "9:16"}}, want: "1080p竖"},
		{name: "tier + ratio 16:9", req: relaycommon.TaskSubmitReq{Resolution: "1080p", Metadata: map[string]interface{}{"ratio": "16:9"}}, want: "1080p横"},
		{name: "tier + size 横", req: relaycommon.TaskSubmitReq{Resolution: "720p", Size: "1920x1080"}, want: "720p横"},
		{name: "tier + size 竖", req: relaycommon.TaskSubmitReq{Resolution: "720p", Size: "1080x1920"}, want: "720p竖"},
		{name: "tier + orientation", req: relaycommon.TaskSubmitReq{Resolution: "480p", Metadata: map[string]interface{}{"orientation": "landscape"}}, want: "480p横"},
		{name: "无 resolution 但 ratio 9:16", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "9:16"}}, want: "480p竖"},
		{name: "无 resolution 但 ratio 16:9", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "16:9"}}, want: "480p横"},
		{name: "无 resolution 但 size 竖", req: relaycommon.TaskSubmitReq{Size: "720x1280"}, want: "480p竖"},
		{name: "4k 拒绝", req: relaycommon.TaskSubmitReq{Resolution: "4k"}, wantErr: "resolution"},
		{name: "2160p 拒绝", req: relaycommon.TaskSubmitReq{Resolution: "2160p"}, wantErr: "resolution"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveResolution(tt.req)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

// 上游只有竖/横两种画幅，参考视频不支持（拼车工作流无 video_data_urls 入参），
// 尾帧同理；上限取合并协议文档（图 9 / 音 3），比 wanwan capabilities（10/5）更严。
func TestValidateMediaEnforcesUpstreamLimits(t *testing.T) {
	images := make([]relaycommon.TaskMedia, 0, 10)
	for i := 0; i < 10; i++ {
		images = append(images, relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/a.png"})
	}
	err := validateMedia(images)
	require.ErrorContains(t, err, "at most 9")

	audios := make([]relaycommon.TaskMedia, 0, 4)
	for i := 0; i < 4; i++ {
		audios = append(audios, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://cdn.example/a.mp3"})
	}
	err = validateMedia(audios)
	require.ErrorContains(t, err, "at most 3")

	require.ErrorContains(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_video", URL: "https://cdn.example/v.mp4"},
	}), "reference_video")

	require.ErrorContains(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "last_frame", URL: "https://cdn.example/a.png"},
	}), "last_frame")

	require.ErrorContains(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "reference_image", URL: "  "},
	}), "url is required")

	// 边界内通过：9 图 + 3 音
	okMedia := make([]relaycommon.TaskMedia, 0, 12)
	for i := 0; i < 9; i++ {
		okMedia = append(okMedia, relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/a.png"})
	}
	for i := 0; i < 3; i++ {
		okMedia = append(okMedia, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://cdn.example/a.mp3"})
	}
	require.NoError(t, validateMedia(okMedia))
}

// first_frame 必须前插到 image_data_urls 首位（上游把第一张当首帧），且占图额度；
// 其余保持原有相对顺序。
func TestBuildMediaOrdersFirstFrameFirst(t *testing.T) {
	media, err := buildMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://cdn.example/a.png"},
			{Type: "first_frame", URL: "https://cdn.example/first.png"},
			{Type: "reference_audio", URL: "https://cdn.example/bgm.mp3"},
		},
	})
	require.NoError(t, err)
	require.Len(t, media, 3)
	require.Equal(t, "first_frame", media[0].Type)
	require.Equal(t, "reference_image", media[1].Type)
	require.Equal(t, "reference_audio", media[2].Type)

	// 旧版扁平字段：Image/InputReference 落参考图、Audio 落参考音。
	media, err = buildMedia(relaycommon.TaskSubmitReq{
		Image: "https://cdn.example/a.png",
		Audio: "https://cdn.example/bgm.mp3",
	})
	require.NoError(t, err)
	require.Len(t, media, 2)
	require.Equal(t, "reference_image", media[0].Type)
	require.Equal(t, "reference_audio", media[1].Type)
}

// BuildRequestBody 组装上游三元协议：workflow 固定拼车档、时长/分辨率取解析值、
// client_request_id 用 newapi-+公开任务 ID 做幂等（上游 submission_uncertain 依它重提解锁）、
// context_ir_enabled 默认 true 可被 metadata 覆盖、seed 透传。
func TestBuildRequestBody(t *testing.T) {
	gin.SetMode(gin.TestMode)

	seed := int64(42)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:     "一只猫",
		Duration:   10,
		Resolution: "1080p",
		Seed:       &seed,
	})
	info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_abc"}}

	body, err := (&TaskAdaptor{}).BuildRequestBody(ctx, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(raw, &payload))
	assert.Equal(t, UpstreamWorkflow, payload["workflow"])
	assert.Equal(t, "一只猫", payload["prompt"])
	assert.InEpsilon(t, float64(10), payload["duration"], 0)
	assert.Equal(t, "1080p竖", payload["resolution"])
	assert.Equal(t, "newapi-task_abc", payload["client_request_id"])
	assert.Equal(t, true, payload["context_ir_enabled"])
	assert.InEpsilon(t, float64(42), payload["seed"], 0)
	assert.NotContains(t, payload, "image_data_urls")
	assert.NotContains(t, payload, "audio_data_urls")

	// metadata.context_ir_enabled=false 必须覆盖默认 true。
	ctx2, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx2.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:   "x",
		Metadata: map[string]interface{}{"context_ir_enabled": false},
	})
	body2, err := (&TaskAdaptor{}).BuildRequestBody(ctx2, info)
	require.NoError(t, err)
	raw2, err := io.ReadAll(body2)
	require.NoError(t, err)
	var payload2 map[string]interface{}
	require.NoError(t, common.Unmarshal(raw2, &payload2))
	assert.Equal(t, false, payload2["context_ir_enabled"])
}

// prompt 与素材全空必须拒绝，且走 UserError（400 降级）而不是普通 error
// （普通 error 会被 relay_task 包成 500 build_request_failed，触发重试与 AutoBan）。
func TestBuildRequestBodyRejectsEmptyPromptAndMedia(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", relaycommon.TaskSubmitReq{})

	_, err := (&TaskAdaptor{}).BuildRequestBody(
		ctx, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_abc"}})
	require.Error(t, err)
	var userErr *taskcommon.UserError
	require.True(t, errors.As(err, &userErr), "期望 UserError，实际: %v", err)
}

// 素材转换契约：data: URL 原样透传（不解码再编码）；http(s) URL 下载后转成
// data URL（上游只收 data_urls 字段）。下载源用 httptest 本地服务器模拟。
func TestBuildRequestBodyConvertsMediaToDataURLs(t *testing.T) {
	// 单测环境放开 SSRF（默认拦截非白名单端口，会拦掉 httptest 随机端口）。
	fetch := system_setting.GetFetchSetting()
	origFetch := *fetch
	fetch.EnableSSRFProtection = false
	defer func() { *fetch = origFetch }()

	// 完整 PNG magic（8 字节），否则 http.DetectContentType 识别成 text/plain。
	pngBytes := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngBytes)
	}))
	defer srv.Close()

	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt: "x",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "data:image/png;base64,AAAA"},
			{Type: "reference_image", URL: srv.URL + "/a.png"},
			{Type: "reference_audio", URL: "data:audio/mpeg;base64,BBBB"},
		},
	})

	body, err := (&TaskAdaptor{}).BuildRequestBody(
		ctx, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_abc"}})
	require.NoError(t, err)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload struct {
		ImageData []string `json:"image_data_urls"`
		AudioData []string `json:"audio_data_urls"`
	}
	require.NoError(t, common.Unmarshal(raw, &payload))
	require.Len(t, payload.ImageData, 2)
	assert.Equal(t, "data:image/png;base64,AAAA", payload.ImageData[0], "data: URL 原样透传")
	assert.True(t, strings.HasPrefix(payload.ImageData[1], "data:image/png;base64,"),
		"http 源须转 data URL，实际: %s", payload.ImageData[1])
	require.Len(t, payload.AudioData, 1)
	assert.Equal(t, "data:audio/mpeg;base64,BBBB", payload.AudioData[0])
}

func TestGetModelListAndChannelName(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Equal(t, []string{PublicModel}, adaptor.GetModelList())
	require.Equal(t, ChannelName, adaptor.GetChannelName())
}

// 轮询状态映射：上游只有 processing / done 两态；done 无 download_url 不能当成功
//（否则客户端拿到打不开的地址）；ok:false 一律失败并原样透传上游原因。
func TestParseTaskResult(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantErr    bool
		wantStatus string
		wantReason string
	}{
		{
			name:       "done 出片成功",
			body:       `{"ok":true,"status":"done","download_url":"/v1/video/file/e1"}`,
			wantStatus: model.TaskStatusSuccess,
		},
		{
			name:       "processing 进行中",
			body:       `{"ok":true,"status":"processing"}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "未知状态兜底进行中",
			body:       `{"ok":true,"status":"queued"}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "done 无 download_url 不能当成功",
			body:       `{"ok":true,"status":"done"}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "ok false 失败透传原因",
			body:       `{"ok":false,"error":"wanwan 卡不可用：连接超时"}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: "wanwan 卡不可用：连接超时",
		},
		{
			name:       "ok false 无原因给通用文案",
			body:       `{"ok":false}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: reasonUpstreamFailed,
		},
		{
			name:    "非 JSON 报错",
			body:    `<html>502</html>`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tt.body))
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantStatus, result.Status)
			if tt.wantReason != "" {
				require.Equal(t, tt.wantReason, result.Reason)
			}
			if result.Status == model.TaskStatusSuccess {
				// 产物 download_url 需要渠道 Bearer 才能取，Url 必须留空走站内代理。
				assert.Empty(t, result.Url)
				assert.Equal(t, taskcommon.ProgressComplete, result.Progress)
			}
		})
	}
}

// 提交响应：ok:false（含 submission_uncertain / not_submitted 形态）必须透传为
// TaskError（如实报错，不吞不猜）；成功返回 event_id 并回 OpenAI video 形状。
func TestDoResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	info := &relaycommon.RelayInfo{
		OriginModelName: PublicModel,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_abc"},
	}

	t.Run("上游失败透传", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"not_submitted":true,"error":"wanwan 卡不可用"}`)),
		}
		_, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "wanwan 卡不可用")
	})

	t.Run("提交结果未知透传", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":false,"submission_uncertain":true,"client_request_id":"req-1"}`)),
		}
		_, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "submission_uncertain")
	})

	t.Run("成功返回 event_id", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"event_id":"evt_1","cost":1,"balance":99}`)),
		}
		taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
		require.Nil(t, taskErr)
		require.Equal(t, "evt_1", taskID)
		require.Contains(t, string(taskData), "evt_1")

		var out struct {
			ID        string `json:"id"`
			Object    string `json:"object"`
			Model     string `json:"model"`
			Status    string `json:"status"`
			StatusURL string `json:"status_url"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &out))
		assert.Equal(t, "task_abc", out.ID)
		assert.Equal(t, "video", out.Object)
		assert.Equal(t, PublicModel, out.Model)
		assert.Equal(t, dto.VideoStatusQueued, out.Status)
		assert.Contains(t, out.StatusURL, "/v1/videos/task_abc/content")
	})

	t.Run("非 200 透传", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		resp := &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       io.NopCloser(strings.NewReader(`{"error":"invalid token"}`)),
		}
		_, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "invalid token")
	})

	t.Run("成功但无 event_id", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
		}
		_, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
		require.NotNil(t, taskErr)
	})
}

// OpenAI Video 查询转换：成功落站内代理地址（download_url 需渠道 Bearer，绝不透传
// 相对路径）；失败回填任务失败原因；轮询中不带 url。
func TestConvertToOpenAIVideo(t *testing.T) {
	adaptor := &TaskAdaptor{}

	t.Run("成功", func(t *testing.T) {
		raw, err := common.Marshal(map[string]interface{}{"ok": true, "status": "done", "download_url": "/v1/video/file/e1"})
		require.NoError(t, err)
		out, err := adaptor.ConvertToOpenAIVideo(&model.Task{
			TaskID: "task_x", Status: model.TaskStatusSuccess, Data: raw,
		})
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(out, &video))
		assert.Equal(t, dto.VideoStatusCompleted, video.Status)
		assert.Equal(t, taskcommon.BuildProxyURL("task_x"), video.Metadata["url"])
	})

	t.Run("失败", func(t *testing.T) {
		raw, err := common.Marshal(map[string]interface{}{"ok": false, "error": "审核未通过"})
		require.NoError(t, err)
		out, err := adaptor.ConvertToOpenAIVideo(&model.Task{
			TaskID: "task_x", Status: model.TaskStatusFailure, FailReason: "审核未通过", Data: raw,
		})
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(out, &video))
		assert.Equal(t, dto.VideoStatusFailed, video.Status)
		require.NotNil(t, video.Error)
		assert.Equal(t, "审核未通过", video.Error.Message)
	})

	t.Run("进行中", func(t *testing.T) {
		raw, err := common.Marshal(map[string]interface{}{"ok": true, "status": "processing"})
		require.NoError(t, err)
		out, err := adaptor.ConvertToOpenAIVideo(&model.Task{
			TaskID: "task_x", Status: model.TaskStatusInProgress, Data: raw,
		})
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(out, &video))
		assert.Equal(t, dto.VideoStatusInProgress, video.Status)
		assert.NotContains(t, video.Metadata, "url")
	})
}
