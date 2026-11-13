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

// 大图必须缩到长边 1920 再上传。
//
// 2026-11-13：flattenImage 只转 JPEG、质量 95、**不缩放** —— 一张 4000×3000 的
// 手机原图仍是 4MB，经 5Mbps 出口要 7 秒，还让上游受理慢了 26 秒。
// 缩到 1920 长边 + q88 后约 500KB，既省出口带宽也减少上游处理时间。
func TestFlattenImageScalesDownOversizedReferenceImage(t *testing.T) {
	data, contentType, err := flattenImage(makeJPEG(t, 3000, 4000))

	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", contentType)

	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 1440, img.Bounds().Dx(), "长边必须压到 1920，比例不能变")
	assert.Equal(t, 1920, img.Bounds().Dy())
}

func TestFlattenImageScalesLandscapeTo1920LongSide(t *testing.T) {
	data, _, err := flattenImage(makeJPEG(t, 4000, 3000))

	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 1920, img.Bounds().Dx())
	assert.Equal(t, 1440, img.Bounds().Dy())
}

// 缩放只能缩小，不能放大：放大会凭空增加体积，且不会增加任何信息。
func TestFlattenImageNeverUpscalesSmallReferenceImage(t *testing.T) {
	data, _, err := flattenImage(makeJPEG(t, 800, 800))

	require.NoError(t, err)
	img, err := jpeg.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, 800, img.Bounds().Dx())
	assert.Equal(t, 800, img.Bounds().Dy())
}

// 极窄长图缩放后短边会掉到 720 以下，上游会回「图片无法读取或格式不支持」
// （实际是分辨率不够）。必须本地拒绝并说清是「缩放后」不达标。
func TestFlattenImageRejectsPanoramaBelowMinimumAfterScaling(t *testing.T) {
	_, _, err := flattenImage(makeJPEG(t, 5000, 1000))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "1920x384", "报错要带缩放后的实际尺寸")
	assert.Contains(t, err.Error(), "720")

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr)
}

// 解码失败不能把原字节贴上 image/jpeg 硬传上游。
//
// 生产实例：logo.png 的 CRC 损坏（invalid checksum），旧逻辑把 PNG 字节当 JPEG
// 上传，上游回「文件内容与声明格式不符」，客户完全看不懂。
func TestFlattenImageRejectsUndecodableImage(t *testing.T) {
	_, _, err := flattenImage([]byte("this is definitely not an image"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "损坏")

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr, "坏图是客户换一张就能修的，必须 400 而不是 500")
}

// 缩放 + q88 必须让「大图变小」，否则这次改动没有意义。
func TestFlattenImageShrinksLargePhotoBytes(t *testing.T) {
	original := makeJPEG(t, 3000, 4000)

	data, _, err := flattenImage(original)

	require.NoError(t, err)
	assert.Less(t, len(data)*2, len(original),
		"处理后 %d 字节，不到原图 %d 字节的一半", len(data), len(original))
}
