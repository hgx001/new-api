package kemeitv

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 1x1 透明 PNG —— 最小的合法图片 data URI，用来验证「data URI → 公网 URL」转存。
const tinyPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// ────────────────────────────────────────────────────────────────────────────
// metadata.input.media 兼容（官网链路 → 客美改派后的首帧丢失修复）
//
// 客户请求 wan3.0-video-官网 时，ArcReel 把首帧放在 metadata.input.media，
// 而官网适配器读的正是这个位置。改派到客美后，kemeitv 原实现只读顶层
// media / images / image / input_reference，首帧被静默丢弃 ——
// 任务照建、钱照扣、成片却完全没有首帧。
// ────────────────────────────────────────────────────────────────────────────

func TestCollectMedia_metadata_input_media首帧不再丢失(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Prompt: "x",
		Metadata: map[string]any{
			"input": map[string]any{
				"media": []any{
					map[string]any{"type": "first_frame", "url": "https://cdn.example.com/f.png"},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Images, 1)
	assert.Equal(t, roleFirstFrame, bundle.Images[0].Role)
	assert.Equal(t, "https://cdn.example.com/f.png", bundle.Images[0].URL)
}

// metadata.media（不套 input）是站内另一条既有口径，必须同样兼容。
func TestCollectMedia_metadata_media也兼容(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Metadata: map[string]any{
			"media": []any{
				map[string]any{"type": "reference_image", "url": "https://cdn.example.com/r.png"},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Images, 1)
	assert.Equal(t, roleReferenceImage, bundle.Images[0].Role)
}

// 顶层 media 是显式契约，优先于 metadata —— 与 wan3 buildWan3Media 同口径，
// 避免同一张首帧被两个来源重复计入。
func TestCollectMedia_顶层media优先于metadata(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://cdn.example.com/top.png"},
		},
		Metadata: map[string]any{
			"input": map[string]any{
				"media": []any{
					map[string]any{"type": "first_frame", "url": "https://cdn.example.com/meta.png"},
				},
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Images, 1)
	assert.Equal(t, "https://cdn.example.com/top.png", bundle.Images[0].URL)
}

// metadata 里的未知 role 同样要本地拒绝 —— 不能因为换了来源就绕过校验。
func TestCollectMedia_metadata未知role报错(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Metadata: map[string]any{
			"input": map[string]any{
				"media": []any{
					map[string]any{"type": "bogus", "url": "https://cdn.example.com/a.png"},
				},
			},
		},
	})
	require.Error(t, err)
}

// ────────────────────────────────────────────────────────────────────────────
// data URI → 暂存图床公网 URL
//
// 客美上游只接受公网 URL（文档的 images[].url 为 https 地址），
// data URI 会被当成 URL 去抓取而失败。改派场景下首帧正是 data URI。
// ────────────────────────────────────────────────────────────────────────────

func withStagingUploader(t *testing.T, fn stagingUploadFunc) {
	t.Helper()
	original := uploadStagedImage
	t.Cleanup(func() { uploadStagedImage = original })
	uploadStagedImage = fn
}

func TestStageDataURIImages_图片转存为公网URL(t *testing.T) {
	var gotContentType string
	var gotBytes []byte
	withStagingUploader(t, func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
		gotContentType = contentType
		gotBytes = data
		return "https://arc.example.com/api/v1/remote-generation/staging/abc/image.png", nil
	})

	staged, err := stageDataURIImages(context.Background(), &mediaBundle{
		Images: []mediaItem{{URL: tinyPNGDataURL, Role: roleFirstFrame}},
	})
	require.NoError(t, err)
	require.Len(t, staged.Images, 1)
	assert.Equal(t, "https://arc.example.com/api/v1/remote-generation/staging/abc/image.png", staged.Images[0].URL)
	// role 必须保留：首帧/尾帧/参考图的语义靠它区分。
	assert.Equal(t, roleFirstFrame, staged.Images[0].Role)
	assert.Equal(t, "image/png", gotContentType)
	assert.NotEmpty(t, gotBytes, "转存必须上传解码后的字节，不能上传 data URI 文本")
}

func TestStageDataURIImages_公网URL原样保留(t *testing.T) {
	called := false
	withStagingUploader(t, func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
		called = true
		return "", nil
	})

	staged, err := stageDataURIImages(context.Background(), &mediaBundle{
		Images: []mediaItem{{URL: "https://cdn.example.com/a.png", Role: roleReferenceImage}},
	})
	require.NoError(t, err)
	assert.False(t, called, "已经是公网 URL 的素材不该白跑一次转存")
	assert.Equal(t, "https://cdn.example.com/a.png", staged.Images[0].URL)
}

// 暂存图床不可用时必须 fail-loud：客美拿 data URI 必然失败，
// 放行等于建单扣费后交付一个没有首帧的成片。
func TestStageDataURIImages_转存失败拒绝请求(t *testing.T) {
	withStagingUploader(t, func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
		return "", errors.New("staging 不可用")
	})

	_, err := stageDataURIImages(context.Background(), &mediaBundle{
		Images: []mediaItem{{URL: tinyPNGDataURL, Role: roleFirstFrame}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging")
}

// 图片集合里混入非图片 data URI（声明与用途不符）必须明确报错：
// 这类素材无法转存到图片暂存图床，放行只会让上游拿到一个不可用的地址。
func TestStageDataURIImages_图片位混入非图片dataURI报错(t *testing.T) {
	_, err := stageDataURIImages(context.Background(), &mediaBundle{
		Images: []mediaItem{{
			URL:  "data:video/mp4;base64,dmlkZW8=",
			Role: roleFirstFrame,
		}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "data URI")
}

// BuildRequestBody 必须使用校验阶段转存后的素材：否则 Validate 上传了、
// Build 又把原始 data URI 发出去，转存等于白做。
func TestBuildRequestBody_使用转存后的素材(t *testing.T) {
	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x",
		Metadata: map[string]any{
			"input": map[string]any{
				"media": []any{
					map[string]any{"type": "first_frame", "url": tinyPNGDataURL},
				},
			},
		},
	})
	c.Set(stagedMediaContextKey, &mediaBundle{
		Images: []mediaItem{{
			URL:  "https://arc.example.com/api/v1/remote-generation/staging/abc/image.png",
			Role: roleFirstFrame,
		}},
	})

	body, err := newAdaptor().BuildRequestBody(c, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: 71},
		OriginModelName: ModelNameTJ,
	})
	require.NoError(t, err)
	raw, err := readAll(body)
	require.NoError(t, err)

	decoded := submitRequest{}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	require.Len(t, decoded.Images, 1)
	assert.Equal(t, "https://arc.example.com/api/v1/remote-generation/staging/abc/image.png", decoded.Images[0].URL)
	assert.Equal(t, roleFirstFrame, decoded.Images[0].Role)
}
