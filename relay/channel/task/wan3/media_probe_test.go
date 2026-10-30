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
