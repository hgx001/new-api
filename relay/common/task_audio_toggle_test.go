package common

import (
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 顶层 audio 有两种同名不同义的写法，必须都接住：
//   - 字符串/数组 = 参考音频 URL（老口径，各渠道通用）；
//   - 布尔       = 「成片要不要音轨」开关（上游 hk-wan3 文档口径）。
//
// 2026-11-20 之前布尔值会落到字符串分支 → 400 "audio must be a string or array"，
// 客户照上游文档传参直接被拒，看着像我们接口坏了。
func TestTaskSubmitReqUnmarshalJSONAcceptsBooleanAudio(t *testing.T) {
	cases := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "audio true", payload: `{"model":"hk-wan3","prompt":"p","audio":true}`, want: true},
		{name: "audio false", payload: `{"model":"hk-wan3","prompt":"p","audio":false}`, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var req TaskSubmitReq
			require.NoError(t, commonpkg.Unmarshal([]byte(tc.payload), &req))

			require.NotNil(t, req.AudioEnabled, "布尔 audio 必须收进 AudioEnabled")
			assert.Equal(t, tc.want, *req.AudioEnabled)
			assert.Empty(t, req.Audio, "布尔值不能当成参考音频 URL")
			assert.Empty(t, req.Audios)
			assert.False(t, req.HasAudio(), "布尔开关不是参考音频素材")
		})
	}
}

// 字符串/数组口径必须原样不变 —— 这条是回归保护，不能被布尔分支吃掉。
func TestTaskSubmitReqUnmarshalJSONKeepsStringAudioAsURL(t *testing.T) {
	var req TaskSubmitReq
	require.NoError(t, commonpkg.Unmarshal([]byte(
		`{"model":"autodl:x","prompt":"p","audio":"https://example.com/a.mp3"}`), &req))
	assert.Equal(t, "https://example.com/a.mp3", req.Audio)
	assert.Nil(t, req.AudioEnabled)
	assert.True(t, req.HasAudio())

	var multi TaskSubmitReq
	require.NoError(t, commonpkg.Unmarshal([]byte(
		`{"model":"autodl:x","prompt":"p","audio":["https://example.com/a.mp3","https://example.com/b.wav"]}`), &multi))
	assert.Equal(t, []string{"https://example.com/a.mp3", "https://example.com/b.wav"}, multi.Audios)
	assert.Nil(t, multi.AudioEnabled)
}

// 既不是字符串/数组也不是布尔（数字、对象）仍然报错，不能静默吞掉。
func TestTaskSubmitReqUnmarshalJSONRejectsInvalidAudio(t *testing.T) {
	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(`{"model":"x","prompt":"p","audio":123}`), &req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "audio must be a string, array or boolean")
}

// 字段带 json:"-" 标签：把它序列化回去时不能多出一个 audio 字段，
// 否则会把内部开关泄露给上游/日志。
func TestTaskSubmitReqMarshalOmitsAudioEnabled(t *testing.T) {
	enabled := true
	req := TaskSubmitReq{Model: "hk-wan3", Prompt: "p", AudioEnabled: &enabled}
	raw, err := commonpkg.Marshal(req)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "AudioEnabled")
	assert.NotContains(t, string(raw), `"audio"`)
}
