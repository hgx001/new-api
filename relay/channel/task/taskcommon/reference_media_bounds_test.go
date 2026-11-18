package taskcommon

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- MP4 tkhd 解析（纯字节，无需网络） ----

func TestParseTkhdDimensionsVersions(t *testing.T) {
	cases := []struct {
		name string
		make func(w, h uint32) []byte
		w, h int
	}{
		{"version0", tkhdPayloadV0, 1280, 720},
		{"version1", tkhdPayloadV1, 720, 1280},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h, ok := parseTkhdDimensions(tc.make(uint32(tc.w), uint32(tc.h)))
			require.True(t, ok)
			assert.Equal(t, tc.w, w)
			assert.Equal(t, tc.h, h)
		})
	}
}

func TestParseTkhdDimensionsSkipsZeroAudioTrack(t *testing.T) {
	// 音频轨宽高为 0 → 跳过，取下一个（视频轨）。
	// 注意 tkhdPayloadV0/V1 返回的是**载荷**，必须先包成 tkhd box 再塞进 trak。
	audioTrack := box("trak", box("tkhd", tkhdPayloadV0(0, 0)))
	videoTrack := box("trak", box("tkhd", tkhdPayloadV0(1280, 720)))
	w, h, ok := ParseMP4Dimensions(box("moov", audioTrack, videoTrack))
	require.True(t, ok)
	assert.Equal(t, 1280, w)
	assert.Equal(t, 720, h)
}

func TestParseMP4DimensionsNoMoov(t *testing.T) {
	_, _, ok := ParseMP4Dimensions([]byte("not an mp4"))
	assert.False(t, ok)
}

// 模拟 downloadReferenceVideoForDimensions 的流式场景：moov 先到 mvhd、tkhd 尚未到齐时
// 解析失败（fail-open 放行），等 tkhd 到齐后同一 buffer 解析成功 —— 证明提前返回条件
// 用 ParseMP4Dimensions（而非 ParseMP4Seconds）才能拿到宽高。
func TestParseMP4DimensionsStreamingPartialThenComplete(t *testing.T) {
	videoTrack := box("trak", box("tkhd", tkhdPayloadV0(1280, 720)))
	// 只有 mvhd、没有 trak 的 moov：解析不出尺寸。
	partial := append(box("ftyp", []byte("x")), box("moov", box("mvhd", []byte("y")))...)
	_, _, ok := ParseMP4Dimensions(partial)
	assert.False(t, ok, "tkhd 未到齐时不应解析出尺寸")

	// trak/tkhd 补齐后，同一次读取的完整 buffer 解析成功。
	complete := append(box("ftyp", []byte("x")), box("moov", box("mvhd", []byte("y")), videoTrack)...)
	w, h, ok := ParseMP4Dimensions(complete)
	require.True(t, ok)
	assert.Equal(t, 1280, w)
	assert.Equal(t, 720, h)
}

// ---- 图片尺寸探测（data URL，无需网络） ----

func TestProbeReferenceImageDimensionsDataURL(t *testing.T) {
	url := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, 300, 500))
	w, h, err := ProbeReferenceImageDimensions(context.Background(), url, nil)
	require.NoError(t, err)
	assert.Equal(t, 300, w)
	assert.Equal(t, 500, h)
}

func TestEnforceMinBoundsImageShortSide(t *testing.T) {
	// 竖屏 300×720：短边 300，恰好等于下限 → 放行（不误杀竖屏）。
	okURL := dataImageURL(t, 300, 720)
	_, err := EnforceReferenceMediaMinBounds(context.Background(),
		[]string{okURL}, nil, nil,
		ReferenceMediaMinBounds{ImageMinShortSide: 300}, nil)
	require.NoError(t, err)

	// 短边 299 → 实测越界 → 400 文案。
	badURL := dataImageURL(t, 299, 720)
	_, err = EnforceReferenceMediaMinBounds(context.Background(),
		[]string{badURL}, nil, nil,
		ReferenceMediaMinBounds{ImageMinShortSide: 300}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "短边 299px 低于 300px")
}

// ---- 探测失败放行（fail-open）：地址抓取失败不误杀 ----

func TestEnforceMinBoundsProbeFailureAllows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	unmeasured, err := EnforceReferenceMediaMinBounds(context.Background(),
		[]string{srv.URL + "/gone.png"}, nil, nil,
		ReferenceMediaMinBounds{ImageMinShortSide: 300}, srv.Client())
	require.NoError(t, err, "探测失败必须放行")
	assert.Len(t, unmeasured, 1, "探测失败必须留痕")
}

// ---- 辅助：构造最小 MP4 box / PNG ----

func box(boxType string, payload ...[]byte) []byte {
	var body []byte
	for _, p := range payload {
		body = append(body, p...)
	}
	out := make([]byte, 8+len(body))
	out[0] = byte((8 + len(body)) >> 24)
	out[1] = byte((8 + len(body)) >> 16)
	out[2] = byte((8 + len(body)) >> 8)
	out[3] = byte(8 + len(body))
	copy(out[4:8], boxType)
	copy(out[8:], body)
	return out
}

// tkhdPayloadV0 构造 version 0 的 tkhd 载荷：version+flags(4) + 固定头(72) + width/height(8)。
// width/height 是 16.16 定点数，真实 MP4 里像素值存于高 16 位。
func tkhdPayloadV0(width, height uint32) []byte {
	payload := make([]byte, 84)
	payload[0] = 0 // version 0
	writeU32(payload, 76, width<<16)
	writeU32(payload, 80, height<<16)
	return payload
}

// tkhdPayloadV1 version 1 的宽高偏移为 88/92。
func tkhdPayloadV1(width, height uint32) []byte {
	payload := make([]byte, 96)
	payload[0] = 1
	writeU32(payload, 88, width<<16)
	writeU32(payload, 92, height<<16)
	return payload
}

func writeU32(b []byte, off int, v uint32) {
	b[off] = byte(v >> 24)
	b[off+1] = byte(v >> 16)
	b[off+2] = byte(v >> 8)
	b[off+3] = byte(v)
}

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	// image.DecodeConfig 按魔数识别为 jpeg；类型标签只是 data URL 的提示，不影响解码。
	return buf.Bytes()
}

func dataImageURL(t *testing.T, w, h int) string {
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(pngBytes(t, w, h))
}
