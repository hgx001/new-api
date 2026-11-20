package youzanwan3

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
)

func TestOptimizeReferenceImageURL_自有CDN追加质量参数(t *testing.T) {
	const base = "https://arcmedia.heibaidao.cn/projects/video-studio/refs/image/b1673dd198edf6b4/e2bdbe3b3820447e8dedfd8ab64483c5.jpg"
	got := optimizeReferenceImageURL(base)
	assert.Equal(t, base+"?imageMogr2/quality/90", got)

	// 已带查询参数时用 & 续接，不能出现两个 ?。
	withQuery := base + "?x-version=2"
	assert.Equal(t, withQuery+"&imageMogr2/quality/90", optimizeReferenceImageURL(withQuery))
}

func TestOptimizeReferenceImageURL_外部域名原样返回(t *testing.T) {
	cases := []string{
		"https://example.com/a.jpg",
		"https://arcreel.heibaidao.cn/staging/a.jpg", // 源站无 CI，加了只会 404
		"https://other.heibaidao.cn/a.jpg",
		"not-a-url",
		"",
	}
	for _, tc := range cases {
		assert.Equal(t, tc, optimizeReferenceImageURL(tc), "非自有 CDN 图片必须原样透传：%s", tc)
	}
}

func TestOptimizeReferenceImageURL_带签名地址不追加(t *testing.T) {
	cases := []string{
		"https://arcmedia.heibaidao.cn/a.jpg?access=abc123",
		"https://arcmedia.heibaidao.cn/a.jpg?sign=xyz&t=1",
		"https://arcmedia.heibaidao.cn/a.jpg?q-sign-algorithm=sha1",
		"https://arcmedia.heibaidao.cn/a.jpg?X-Amz-Signature=deadbeef",
		"https://arcmedia.heibaidao.cn/a.jpg?X-Cos-Security-Token=abc",
	}
	for _, tc := range cases {
		assert.Equal(t, tc, optimizeReferenceImageURL(tc), "追加参数会破坏签名，必须原样返回：%s", tc)
	}
}

func TestOptimizeReferenceImageURL_已有CI指令不重复叠加(t *testing.T) {
	src := "https://arcmedia.heibaidao.cn/a.jpg?imageMogr2/quality/70"
	assert.Equal(t, src, optimizeReferenceImageURL(src))

	// 大小写不敏感：客户自己写过 ImageMogr2 也不该被再叠一层。
	srcUpper := "https://arcmedia.heibaidao.cn/a.jpg?ImageMogr2/thumbnail/800x"
	assert.Equal(t, srcUpper, optimizeReferenceImageURL(srcUpper))
}

func TestOptimizeReferenceMedia_只动参考图(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "https://arcmedia.heibaidao.cn/a.jpg"},
		{Type: "reference_video", URL: "https://arcmedia.heibaidao.cn/v.mp4"},
		{Type: "reference_audio", URL: "https://arcmedia.heibaidao.cn/a.mp3"},
		{Type: "reference_image", URL: "https://example.com/b.jpg"},
	}
	out := optimizeReferenceMedia(media)
	assert.Equal(t, "https://arcmedia.heibaidao.cn/a.jpg?imageMogr2/quality/90", out[0].URL)
	assert.Equal(t, "https://arcmedia.heibaidao.cn/v.mp4", out[1].URL, "视频不能加图片处理参数")
	assert.Equal(t, "https://arcmedia.heibaidao.cn/a.mp3", out[2].URL, "音频不能加图片处理参数")
	assert.Equal(t, "https://example.com/b.jpg", out[3].URL)
	// 原切片不能被就地修改（调用方可能复用同一份 req）。
	assert.Equal(t, "https://arcmedia.heibaidao.cn/a.jpg", media[0].URL)
}
