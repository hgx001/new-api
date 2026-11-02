package wan3

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 核心回归（2026-11-02）：media 与 legacy 字段是**并行**的两个通道，必须合并。
//
// 线上症状：客户端把图片放 images、音视频放 media，原实现「media 非空就跳过
// images」导致任务照常成功、照常扣费，成片却没有任何参考图。任务 1084 的 prompt
// 引用「@图片1~4 + @视频1 + @音频1~3」，上游只收到 1 视频 + 3 音频。
func TestBuildWan3MediaMergesLegacyImagesWithMedia(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_video", URL: "https://example.com/ref.mp4"},
			{Type: "reference_audio", URL: "https://example.com/a.mp3"},
		},
		Images: []string{
			"https://example.com/1.png",
			"https://example.com/2.jpg",
			"https://example.com/3.webp",
		},
	}
	media, err := buildWan3Media(req)
	require.NoError(t, err)
	require.Len(t, media, 5, "2 条音视频 + 3 张图，一张都不能少")

	// 原有 media 顺序不动，legacy 图追加在后 —— 图与图之间的相对顺序保持不变，
	// prompt 里「@图片1/2/3」的指认才不会错位。
	assert.Equal(t, "reference_video", media[0].Type)
	assert.Equal(t, "reference_audio", media[1].Type)
	assert.Equal(t, []string{
		"https://example.com/1.png",
		"https://example.com/2.jpg",
		"https://example.com/3.webp",
	}, []string{media[2].URL, media[3].URL, media[4].URL})
	for _, item := range media[2:] {
		assert.Equal(t, "reference_image", item.Type)
	}
}

// 只有音频、没有视频时同样要合并 —— 触发条件是「media 非空」，与素材类型无关。
func TestBuildWan3MediaMergesImagesWithAudioOnly(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_audio", URL: "https://example.com/a.mp3"},
			{Type: "reference_audio", URL: "https://example.com/b.mp3"},
		},
		Images: []string{"https://example.com/1.png"},
	}
	media, err := buildWan3Media(req)
	require.NoError(t, err)
	require.Len(t, media, 3)
	assert.Equal(t, "reference_image", media[2].Type)
}

// media 为空时 legacy 照旧生效（既有行为不能退化）。
func TestBuildWan3MediaFallsBackToLegacyWhenMediaEmpty(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantLen int
	}{
		{
			name:    "images 多张",
			req:     relaycommon.TaskSubmitReq{Images: []string{"https://example.com/1.png", "https://example.com/2.png"}},
			want:    "reference_image",
			wantLen: 2,
		},
		{
			name:    "image 单张",
			req:     relaycommon.TaskSubmitReq{Image: "https://example.com/1.png"},
			want:    "reference_image",
			wantLen: 1,
		},
		{
			name:    "input_reference",
			req:     relaycommon.TaskSubmitReq{InputReference: "https://example.com/1.png"},
			want:    "reference_image",
			wantLen: 1,
		},
		{
			name:    "音频后缀推断成 reference_audio",
			req:     relaycommon.TaskSubmitReq{Images: []string{"https://example.com/a.mp3"}},
			want:    "reference_audio",
			wantLen: 1,
		},
		{
			name:    "视频后缀推断成 reference_video",
			req:     relaycommon.TaskSubmitReq{Images: []string{"https://example.com/a.mp4"}},
			want:    "reference_video",
			wantLen: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media, err := buildWan3Media(tc.req)
			require.NoError(t, err)
			require.Len(t, media, tc.wantLen)
			assert.Equal(t, tc.want, media[0].Type)
		})
	}
}

// 三个 legacy 字段仍是「取第一个非空」的关系，不能同一张图重复计入。
func TestBuildWan3MediaLegacyFieldsDoNotStack(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Images:         []string{"https://example.com/from-images.png"},
		Image:          "https://example.com/from-image.png",
		InputReference: "https://example.com/from-input-ref.png",
	}
	media, err := buildWan3Media(req)
	require.NoError(t, err)
	require.Len(t, media, 1)
	assert.Equal(t, "https://example.com/from-images.png", media[0].URL)
}

// 空白 legacy 值要被过滤，不能生成空 URL 的 media 条目去撞上游。
func TestBuildWan3MediaSkipsBlankLegacyURLs(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images: []string{"   ", "", "https://example.com/1.png"},
	}
	media, err := buildWan3Media(req)
	require.NoError(t, err)
	require.Len(t, media, 2)
	assert.Equal(t, "https://example.com/1.png", media[1].URL)
}

// 首帧 + 参考图是矛盾入参：现在明确 400，不再静默丢掉其中一份。
//
// 旧实现会把 images 整段吞掉、只发 first_frame —— 用户以为带了参考图，
// 实际没有，且没有任何报错。
func TestBuildWan3MediaRejectsFramesCombinedWithLegacyImages(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "first_frame", URL: "https://example.com/first.png"}},
		Images: []string{"https://example.com/ref.png"},
	}
	_, err := buildWan3Media(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be combined")
}

// metadata.input.media 仍是 media 为空时的第二来源，且与 legacy 合并。
func TestBuildWan3MediaMergesMetadataMediaWithLegacy(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Metadata: map[string]any{
			"input": map[string]any{
				"media": []any{
					map[string]any{"type": "reference_audio", "url": "https://example.com/a.mp3"},
				},
			},
		},
		Images: []string{"https://example.com/1.png"},
	}
	media, err := buildWan3Media(req)
	require.NoError(t, err)
	require.Len(t, media, 2)
	assert.Equal(t, "reference_audio", media[0].Type)
	assert.Equal(t, "reference_image", media[1].Type)
}

// 什么都没有时返回空，交给上层判「prompt 与 media 不能同时为空」。
func TestBuildWan3MediaEmpty(t *testing.T) {
	media, err := buildWan3Media(relaycommon.TaskSubmitReq{})
	require.NoError(t, err)
	assert.Empty(t, media)
}

// 图数量超限时合并后仍要被拦下，不能因为走了合并路径就绕过上限。
func TestBuildWan3MediaEnforcesImageLimitAfterMerge(t *testing.T) {
	images := make([]string, 0, maxReferenceImages+2)
	for i := 0; i < maxReferenceImages+2; i++ {
		images = append(images, "https://example.com/img.png")
	}
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images: images,
	}
	_, err := buildWan3Media(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference images")
}
