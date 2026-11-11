package youzanwan3

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeJPEG 生成指定尺寸的纯色 JPEG，用来验证参考图尺寸校验。
func makeJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}))
	return buf.Bytes()
}

// 过小的参考图必须在本地被拒。
//
// 2026-11-11 实测：同一张图 204×360 上游 4/4 报 MEDIA_IMAGE_INVALID
// （文案却是「图片无法读取或格式不支持」，误导成格式问题），而同一张图
// 放大到 240×424 / 255×450 / 306×540 / 408×720 后全部出片成功 ——
// 真正的触发条件是分辨率。上游规则不透明，本地按 720P 交付档直接拦，
// 避免客户白等 1~3 分钟 + 一次退款噪音。
func TestFlattenImageRejectsTooSmallReferenceImage(t *testing.T) {
	_, _, err := flattenImage(makeJPEG(t, 204, 360))

	require.Error(t, err, "204×360 远低于交付档，必须本地拒绝")
	assert.Contains(t, err.Error(), "204x360", "报错要带实际尺寸，方便客户判断")
	assert.Contains(t, err.Error(), "720", "报错要带最低要求")

	// 尺寸不足是客户自己换张图就能修的，必须降级成 400，
	// 不能被 relay_task 包成 500（500 会触发重试与渠道 AutoBan）。
	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr)
	assert.Equal(t, "invalid_request", userErr.Code)
}

// 边界：短边差 1px 也要拒绝，避免「差不多就行」的模糊口径。
func TestFlattenImageRejectsShortSideJustBelowMinimum(t *testing.T) {
	_, _, err := flattenImage(makeJPEG(t, 719, 1280))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "719x1280")
}

// 短边刚好达标要放行，并且不能改变尺寸（只重编码，不缩放）。
func TestFlattenImageAcceptsMinimumShortSide(t *testing.T) {
	data, contentType, err := flattenImage(makeJPEG(t, 720, 1280))

	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", contentType)
	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 720, img.Bounds().Dx())
	assert.Equal(t, 1280, img.Bounds().Dy())
}
