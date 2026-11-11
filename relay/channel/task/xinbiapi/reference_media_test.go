package xinbiapi

import (
	"encoding/json"
	"io"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 心笔 wan3.0-video 的 parameterSchema 声明 duration 2–30（2026-11-11 实测
// GET /v1/media/models），不再是固定 30 秒。缺省仍按 30 秒下发，
// 越界必须报错而不是夹取 —— 静默夹取会让客户以为买的是 5 秒、实收 30 秒的钱。
func TestResolveDurationAcceptsFullRange(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr bool
	}{
		{name: "2 秒", req: relaycommon.TaskSubmitReq{Duration: 2}, want: 2},
		{name: "10 秒", req: relaycommon.TaskSubmitReq{Duration: 10}, want: 10},
		{name: "30 秒", req: relaycommon.TaskSubmitReq{Duration: 30}, want: 30},
		{name: "缺省按 30 秒", req: relaycommon.TaskSubmitReq{}, want: 30},
		{name: "seconds 字符串 8", req: relaycommon.TaskSubmitReq{Seconds: "8"}, want: 8},
		{name: "1 秒越界拒绝", req: relaycommon.TaskSubmitReq{Duration: 1}, wantErr: true},
		{name: "31 秒越界拒绝", req: relaycommon.TaskSubmitReq{Duration: 31}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDuration(tc.req, ModelNamePayPerCall)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 上游 mediaConstraints 声明 VIDEO_REFERENCE 可带 reference_image ≤10、
// reference_video ≤5、reference_audio ≤5。适配器必须把三类素材都送上去，
// 不能像旧实现那样直接拒绝音视频。
func TestCollectReferenceMediaCarriesVideoAndAudio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Image:  "https://cdn.example.com/first.png",
		Images: []string{"https://cdn.example.com/2.png"},
		Video:  "https://cdn.example.com/v0.mp4",
		Videos: []string{"https://cdn.example.com/v2.mp4"},
		Media: []relaycommon.TaskMedia{
			{Type: "reference_video", URL: "https://cdn.example.com/v1.mp4"},
			{Type: "reference_audio", URL: "https://cdn.example.com/a1.mp3"},
		},
		Audios: []string{"https://cdn.example.com/a2.mp3"},
	}
	media, err := collectReferenceMedia(req)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"https://cdn.example.com/first.png",
		"https://cdn.example.com/2.png",
	}, media.Images)
	// 顶层 video / videos 与 media[type=reference_video] 是并行来源，都要送上游。
	assert.ElementsMatch(t, []string{
		"https://cdn.example.com/v0.mp4",
		"https://cdn.example.com/v1.mp4",
		"https://cdn.example.com/v2.mp4",
	}, media.Videos)
	assert.ElementsMatch(t, []string{
		"https://cdn.example.com/a1.mp3",
		"https://cdn.example.com/a2.mp3",
	}, media.Audios)
}

func TestCollectReferenceMediaRejectsOverLimit(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		wantErr bool
	}{
		{
			name: "10 张图允许",
			req:  relaycommon.TaskSubmitReq{Images: repeatURL("https://cdn.example.com/i.png", maxReferenceImages)},
		},
		{
			name:    "11 张图拒绝",
			req:     relaycommon.TaskSubmitReq{Images: repeatURL("https://cdn.example.com/i.png", maxReferenceImages+1)},
			wantErr: true,
		},
		{
			name: "5 段视频允许",
			req:  relaycommon.TaskSubmitReq{Media: repeatMedia("video", "https://cdn.example.com/v.mp4", maxReferenceVideos)},
		},
		{
			name:    "6 段视频拒绝",
			req:     relaycommon.TaskSubmitReq{Media: repeatMedia("video", "https://cdn.example.com/v.mp4", maxReferenceVideos+1)},
			wantErr: true,
		},
		{
			name: "顶层 5 段视频允许",
			req:  relaycommon.TaskSubmitReq{Videos: repeatURL("https://cdn.example.com/v.mp4", maxReferenceVideos)},
		},
		{
			name:    "顶层 6 段视频拒绝",
			req:     relaycommon.TaskSubmitReq{Videos: repeatURL("https://cdn.example.com/v.mp4", maxReferenceVideos+1)},
			wantErr: true,
		},
		{
			name: "5 段音频允许",
			req:  relaycommon.TaskSubmitReq{Audios: repeatURL("https://cdn.example.com/a.mp3", maxReferenceAudios)},
		},
		{
			name:    "6 段音频拒绝",
			req:     relaycommon.TaskSubmitReq{Audios: repeatURL("https://cdn.example.com/a.mp3", maxReferenceAudios+1)},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := collectReferenceMedia(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

// 首尾帧在兼容口没有声明字段（参考生成与首尾帧互斥），继续本地拒绝。
func TestCollectReferenceMediaRejectsFirstLastFrame(t *testing.T) {
	for _, mediaType := range []string{"first_frame", "last_frame"} {
		t.Run(mediaType, func(t *testing.T) {
			req := relaycommon.TaskSubmitReq{Media: []relaycommon.TaskMedia{
				{Type: mediaType, URL: "https://cdn.example.com/frame.png"},
			}}
			_, err := collectReferenceMedia(req)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "first frame")
		})
	}
}

// 非 https 直链必须本地拒绝：上游代取入库时会 422，错误信息不点名字段。
func TestCollectReferenceMediaRequiresHTTPS(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Media: []relaycommon.TaskMedia{
		{Type: "video", URL: "http://cdn.example.com/v.mp4"},
	}}
	_, err := collectReferenceMedia(req)
	require.Error(t, err)
}

func TestBuildRequestBodyCarriesVideoAndAudio(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Prompt:   "テスト",
		Duration: 2,
		Image:    "https://cdn.example.com/first.png",
		Media: []relaycommon.TaskMedia{
			{Type: "video", URL: "https://cdn.example.com/v.mp4"},
			{Type: "audio", URL: "https://cdn.example.com/a.mp3"},
		},
	}
	c := newTestContext(t, req)
	info := &relaycommon.RelayInfo{OriginModelName: "seedance2.5-活动"}
	reader, err := newAdaptor().BuildRequestBody(c, info)
	require.NoError(t, err)
	raw, err := io.ReadAll(reader)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, "wan3.0-video", decoded["model"])
	assert.Equal(t, float64(2), decoded["duration"])
	assert.Equal(t, []any{"https://cdn.example.com/v.mp4"}, decoded["videos"])
	assert.Equal(t, []any{"https://cdn.example.com/a.mp3"}, decoded["audios"])
	assert.Equal(t, []any{"https://cdn.example.com/first.png"}, decoded["reference_images"])
}

func repeatURL(url string, count int) []string {
	items := make([]string, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, url)
	}
	return items
}

func repeatMedia(mediaType, url string, count int) []relaycommon.TaskMedia {
	items := make([]relaycommon.TaskMedia, 0, count)
	for i := 0; i < count; i++ {
		items = append(items, relaycommon.TaskMedia{Type: mediaType, URL: url})
	}
	return items
}
