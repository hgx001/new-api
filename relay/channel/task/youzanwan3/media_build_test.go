package youzanwan3

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 核心回归（2026-11-02）：media 与 legacy 字段是**并行**的两个通道，必须合并。
//
// 这是渠道 17「有赞 Wan3」（ChannelTypeYouzanWan3）真正走的 adaptor。
// 线上症状：客户端把图片放 images、音视频放 media，原实现「media 非空就跳过
// images」导致任务照常成功、照常扣费，成片却没有任何参考图。
func TestBuildMediaMergesLegacyImagesWithMedia(t *testing.T) {
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
	media, err := buildMedia(req)
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
// 任务 1194 / 1197 就是这种形态（2 段音频 + 3 张图）。
func TestBuildMediaMergesImagesWithAudioOnly(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_audio", URL: "https://example.com/a.mp3"},
			{Type: "reference_audio", URL: "https://example.com/b.mp3"},
		},
		Images: []string{"https://example.com/1.png", "https://example.com/2.png", "https://example.com/3.png"},
	}
	media, err := buildMedia(req)
	require.NoError(t, err)
	require.Len(t, media, 5)
	for _, item := range media[2:] {
		assert.Equal(t, "reference_image", item.Type)
	}
}

// media 为空时 legacy 照旧生效（既有行为不能退化）。
func TestBuildMediaFallsBackToLegacyWhenMediaEmpty(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		wantLen int
	}{
		{name: "images 多张", req: relaycommon.TaskSubmitReq{Images: []string{"https://example.com/1.png", "https://example.com/2.png"}}, wantLen: 2},
		{name: "image 单张", req: relaycommon.TaskSubmitReq{Image: "https://example.com/1.png"}, wantLen: 1},
		{name: "input_reference", req: relaycommon.TaskSubmitReq{InputReference: "https://example.com/1.png"}, wantLen: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			media, err := buildMedia(tc.req)
			require.NoError(t, err)
			require.Len(t, media, tc.wantLen)
			for _, item := range media {
				assert.Equal(t, "reference_image", item.Type)
			}
		})
	}
}

// 三个 legacy 字段仍是「取第一个非空」的关系，不能同一张图重复计入。
func TestBuildMediaLegacyFieldsDoNotStack(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Images:         []string{"https://example.com/from-images.png"},
		Image:          "https://example.com/from-image.png",
		InputReference: "https://example.com/from-input-ref.png",
	}
	media, err := buildMedia(req)
	require.NoError(t, err)
	require.Len(t, media, 1)
	assert.Equal(t, "https://example.com/from-images.png", media[0].URL)
}

// 空白 legacy 值要被过滤，不能生成空 URL 的 media 条目去撞上游。
func TestBuildMediaSkipsBlankLegacyURLs(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images: []string{"   ", "", "https://example.com/1.png"},
	}
	media, err := buildMedia(req)
	require.NoError(t, err)
	require.Len(t, media, 2)
	assert.Equal(t, "https://example.com/1.png", media[1].URL)
}

// metadata.input.media 仍是 media 为空时的第二来源，且与 legacy 合并。
func TestBuildMediaMergesMetadataMediaWithLegacy(t *testing.T) {
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
	media, err := buildMedia(req)
	require.NoError(t, err)
	require.Len(t, media, 2)
	assert.Equal(t, "reference_audio", media[0].Type)
	assert.Equal(t, "reference_image", media[1].Type)
}

// 合并后仍要受图片数量上限约束，不能因为走了合并路径就绕过校验。
func TestBuildMediaEnforcesImageLimitAfterMerge(t *testing.T) {
	images := make([]string, 0, maxReferenceImages+2)
	for i := 0; i < maxReferenceImages+2; i++ {
		images = append(images, "https://example.com/img.png")
	}
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images: images,
	}
	_, err := buildMedia(req)
	require.Error(t, err)
}

// 空请求返回空，交给上层判「prompt 与 media 不能同时为空」。
func TestBuildMediaEmpty(t *testing.T) {
	media, err := buildMedia(relaycommon.TaskSubmitReq{})
	require.NoError(t, err)
	assert.Empty(t, media)
}
