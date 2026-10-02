package hailuo

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v2 成功：content.url 单步取流，不做 file_id 中转。
func TestParseV2TaskResultSucceededVideo(t *testing.T) {
	body := []byte(`{"task":{"id":"424010985738629","model":"MiniMax-H3","status":"succeeded",
		"content":{"url":"https://cdn.example.com/h3.mp4"},"resolution":"2K","duration":5,
		"usage":{"total_seconds":5,"input_seconds":0,"output_seconds":5,"input_image_count":0},
		"ratio":"16:9","task_type":"generation","modality":"video"}}`)
	res, err := parseV2TaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, res.Status)
	assert.Equal(t, "100%", res.Progress)
	assert.Equal(t, "https://cdn.example.com/h3.mp4", res.Url)
	assert.Empty(t, res.ResultText)
}

// Context-IR 成功：产物是提示词文本，**不能**同时给 url。
func TestParseV2TaskResultSucceededContextIR(t *testing.T) {
	body := []byte(`{"task":{"id":"1","model":"MiniMax-H3","status":"succeeded",
		"content":{"prompt":"逆光下，一只猫在屋顶奔跑，镜头缓慢推进"},"task_type":"h3_context_ir",
		"modality":"text"}}`)
	res, err := parseV2TaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusSuccess, res.Status)
	assert.Equal(t, "逆光下，一只猫在屋顶奔跑，镜头缓慢推进", res.ResultText)
	assert.Empty(t, res.Url, "文本交付物不得伪造媒体 URL")
}

func TestParseV2TaskResultProgressStates(t *testing.T) {
	for _, tc := range []struct {
		status        string
		wantStatus    string
		wantProgress  string
		wantIsSuccess bool
	}{
		{V2StatusQueued, model.TaskStatusInProgress, "30%", false},
		{V2StatusRunning, model.TaskStatusInProgress, "50%", false},
		{V2StatusFailed, model.TaskStatusFailure, "100%", false},
		{V2StatusCancelled, model.TaskStatusFailure, "100%", false},
	} {
		t.Run(tc.status, func(t *testing.T) {
			body := []byte(`{"task":{"id":"1","status":"` + tc.status + `"}}`)
			res, err := parseV2TaskResult(body)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, res.Status)
			assert.Equal(t, tc.wantProgress, res.Progress)
		})
	}
}

// v2 失败形状：{"type":"error","error":{...}} —— 没有 base_resp。
func TestParseV2TaskResultErrorShape(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"insufficient_balance_error",
		"message":"insufficient balance (1008)","http_code":"402"},"request_id":"x"}`)
	res, err := parseV2TaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, res.Status)
	assert.Contains(t, res.Reason, "insufficient balance")
}

// 成功却既无 url 也无 prompt → 必须报失败。报成功会让客户端拿到空任务。
func TestParseV2TaskResultSuccessWithoutArtifactFails(t *testing.T) {
	body := []byte(`{"task":{"id":"1","status":"succeeded","content":{}}}`)
	res, err := parseV2TaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, model.TaskStatusFailure, res.Status)
	assert.Contains(t, res.Reason, "neither url nor prompt")
}

// v2 上游余额不足的 http_code=402 必须**原样**传给本端余额分类器，
// 不能被降级成本端错误——否则渠道永不 AutoBan。
func TestV2ErrorToTaskErrorPreservesUpstreamBalanceSignal(t *testing.T) {
	taskErr := v2ErrorToTaskError(&V2ErrorBody{
		Type: "insufficient_balance_error", Message: "insufficient balance (1008)", HTTPCode: "402",
	})
	require.NotNil(t, taskErr)
	assert.Equal(t, 402, taskErr.StatusCode)
	assert.False(t, taskErr.LocalError, "上游余额耗尽不是本端错误，必须触发渠道下线/切换")
	assert.Contains(t, taskErr.Message, "insufficient balance")
}

func TestLooksLikeV2Query(t *testing.T) {
	assert.True(t, looksLikeV2Query([]byte(`{"task":{"id":"1"}}`)))
	assert.True(t, looksLikeV2Query([]byte(`{"type":"error","error":{"http_code":"402"}}`)))
	// v1 形状：顶层有 base_resp、没有 task。
	assert.False(t, looksLikeV2Query([]byte(`{"task_id":"1","status":"Success","file_id":"2","base_resp":{"status_code":0}}`)))
	assert.False(t, looksLikeV2Query(nil))
	assert.False(t, looksLikeV2Query([]byte(`  `)))
}

// 轮询阶段拿不到 RelayInfo，只能靠落库标记判断 v1/v2。
func TestIsV2UpstreamModelFromStoredBody(t *testing.T) {
	assert.True(t, isV2UpstreamModel(map[string]any{"model": ModelH3}))
	assert.True(t, isV2UpstreamModel(map[string]any{"_minimax_endpoint": "v2"}))
	assert.False(t, isV2UpstreamModel(map[string]any{"model": "MiniMax-Hailuo-2.3"}))
	assert.False(t, isV2UpstreamModel(map[string]any{}))
}

// 文本交付物走 metadata.prompt + media_type=text，且不给 url。
func TestConvertV2ToOpenAIVideoContextIR(t *testing.T) {
	originTask := &model.Task{
		TaskID: "task-1", Status: model.TaskStatusSuccess,
		Data: []byte(`{"task":{"id":"1","status":"succeeded","content":{"prompt":"镜头从低角度仰拍"},"duration":5}}`),
		PrivateData: model.TaskPrivateData{
			ResultText: "镜头从低角度仰拍",
		},
	}
	raw, err := convertV2ToOpenAIVideo(originTask)
	require.NoError(t, err)
	got := string(raw)
	assert.Contains(t, got, `"prompt":"镜头从低角度仰拍"`)
	assert.Contains(t, got, `"media_type":"text"`)
	assert.NotContains(t, got, `"url":`, "文本任务不得返回伪媒体 URL")
}

func TestConvertV2ToOpenAIVideoVideo(t *testing.T) {
	originTask := &model.Task{
		TaskID: "task-2", Status: model.TaskStatusSuccess,
		Data: []byte(`{"task":{"id":"1","status":"succeeded",
			"content":{"url":"https://cdn.example.com/h3.mp4"},"resolution":"768P","duration":8,"ratio":"16:9"}}`),
	}
	raw, err := convertV2ToOpenAIVideo(originTask)
	require.NoError(t, err)
	got := string(raw)
	assert.Contains(t, got, "https://cdn.example.com/h3.mp4")
	assert.Contains(t, got, `"media_type":"video"`)
	assert.Contains(t, got, `"seconds":"8"`)
	assert.Contains(t, got, `"size":"1366x768"`)
}

func TestConvertV2ToOpenAIVideoFailure(t *testing.T) {
	originTask := &model.Task{
		TaskID: "task-3", Status: model.TaskStatusFailure, FailReason: "内容审核不通过",
		Data: []byte(`{"task":{"id":"1","status":"failed"}}`),
	}
	raw, err := convertV2ToOpenAIVideo(originTask)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "内容审核不通过")
}
