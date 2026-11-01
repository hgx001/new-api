package wan3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pngMagic 是最小可识别的 PNG 头，只用来让魔数判断通过。
var pngMagic = []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}

func TestProbeReferenceImageAcceptsReachablePNG(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(pngMagic)
	}))
	defer server.Close()

	require.NoError(t, probeReferenceImage(context.Background(), server.URL+"/ref.png?tk=secret"))
}

// HEAD 被对象存储拒绝（403/405）时必须退化为 GET，不能把能下载的图误杀。
func TestProbeReferenceImageFallsBackToGetWhenHeadRejected(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0})
	}))
	defer server.Close()

	require.NoError(t, probeReferenceImage(context.Background(), server.URL+"/ref.jpg"))
}

func TestProbeReferenceImageRejectsUnreachableURL(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	err := probeReferenceImage(context.Background(), server.URL+"/missing.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestProbeReferenceImageRejectsNonImageContentType(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html>login</html>"))
	}))
	defer server.Close()

	err := probeReferenceImage(context.Background(), server.URL+"/ref.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an image")
}

// 200 + image/png 但正文是错误页：必须靠魔数拦住。
func TestProbeReferenceImageRejectsFakeImageBody(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("<html>error page</html>"))
	}))
	defer server.Close()

	err := probeReferenceImage(context.Background(), server.URL+"/ref.png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a JPEG/PNG")
}

func TestProbeReferenceImageDataURL(t *testing.T) {
	require.NoError(t, probeReferenceImage(context.Background(), "data:image/png;base64,iVBORw0KGgo="))

	err := probeReferenceImage(context.Background(), "data:text/plain;base64,aGVsbG8=")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an image")
}

func TestValidateReferenceImageURLsReportsIndex(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: "data:image/png;base64,iVBORw0KGgo="},
		{Type: "reference_image", URL: server.URL + "/broken.png?tk=leak-me"},
	}
	err := validateReferenceImageURLs(media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference image #2")
	// 签名参数不能进错误信息（会回给客户端并落日志）
	assert.NotContains(t, err.Error(), "leak-me")
}

func TestValidateReferenceImageURLsIgnoresVideo(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: "https://example.invalid/clip.mp4?tk=x"},
	}
	require.NoError(t, validateReferenceImageURLs(media))
}

func TestRedactMediaURLStripsSignature(t *testing.T) {
	assert.Equal(t,
		"https://api.example.com/api/rpc/video.png",
		redactMediaURL("https://api.example.com/api/rpc/video.png?tk=secret&Expires=1"))
	assert.Equal(t, "(relative url)", redactMediaURL(""))
}

// base64 data URL 里整张图片都在字符串里，原样落日志等于把用户图片写进日志文件
// （describeMedia 的 "wan3 media accepted:" 那行会打全量）。redactMediaURL 之前
// 只处理 http(s) 的 query string，data URL 的 scheme 是 "data"、query 为空，
// 于是 payload 一字不漏地透了出去。2026-11-01 随 base64 通道上线前修掉。
func TestRedactMediaURLRedactsDataURL(t *testing.T) {
	redacted := redactMediaURL("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==")
	assert.NotContains(t, redacted, "iVBORw0KGgo")
	assert.Contains(t, redacted, "data:image/png")
	// 只保留可读摘要，不留 payload
	assert.Contains(t, redacted, "base64")
	assert.Less(t, len(redacted), 120)
}

func TestDescribeMediaDoesNotLeakDataURLPayload(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="},
	}
	line := describeMedia(media)
	assert.NotContains(t, line, "iVBORw0KGgo")
	assert.Contains(t, line, "first_frame")
}

// 首帧/尾帧和参考图一样，都是「上游直传 URL、生成时才下载」，
// 拉不到同样要白等一两分钟。2026-11-01 线上两单都是 first_frame 指向
// 境外图床（tmpfiles.org / catbox.moe），探测被跳过，直接进了上游。
func TestValidateReferenceImageURLsChecksFirstFrame(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: server.URL + "/opening.png"},
	}
	err := validateReferenceImageURLs(media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "first frame #1")
}

func TestValidateReferenceImageURLsChecksLastFrame(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: "data:image/png;base64,iVBORw0KGgo="},
		{Type: "last_frame", URL: server.URL + "/ending.png"},
	}
	err := validateReferenceImageURLs(media)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "last frame #2")
}

func TestValidateReferenceImageURLsAcceptsReachableFirstFrame(t *testing.T) {
	disableSSRF(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(pngMagic)
	}))
	defer server.Close()

	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: server.URL + "/opening.png"},
		{Type: "last_frame", URL: server.URL + "/ending.png"},
	}
	require.NoError(t, validateReferenceImageURLs(media))
}

// 音频/视频不该走图片探测：探测只看 Content-Type 是不是 image/*，
// 误加进去会把正常请求全拒掉。
func TestValidateReferenceImageURLsIgnoresAudio(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "driving_audio", URL: "https://example.invalid/voice.mp3"},
	}
	require.NoError(t, validateReferenceImageURLs(media))
}
