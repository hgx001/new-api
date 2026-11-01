package wan3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gin-gonic/gin"
)

// 转存（relocate）：把客户给的境外图床 URL 换成 new-api 自家 staging 图床的公网地址，
// 再交给上游。目的是让**上游**不必去拉客户的图床 —— 上游只认自家可达的地址。
//
// 2026-11-01 实测：客户首帧用 tmpfiles.org（TCP 层即断）与 litter.catbox.moe
// （能连但单次耗时 88.54 秒），三者可达性任一不满足即全链路失败。

// relocateClient 抽象「代下载 + 转存」的两个外部动作，便于测试注入。
// 定义在 media_relocate.go（生产代码里同款），此处直接复用。

func TestRelocateMediaReplacesImageURLWithStagingURL(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngMagic)
	}))
	defer origin.Close()

	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return pngMagic, "image/png", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			return "/api/v1/remote-generation/staging/abc/image.png", nil
		},
		stagingURL: func(stagingPath string) string {
			return "https://arcreel.heibaidao.cn" + stagingPath
		},
	}

	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/shot.png"},
		{Type: "last_frame", URL: origin.URL + "/tail.png"},
	}
	out, err := relocateMedia(context.Background(), media, client)
	require.NoError(t, err)
	require.Len(t, out, 2)
	// 客户图床地址不能再出现在交给上游的 media 里
	assert.NotContains(t, out[0].URL, origin.URL)
	assert.Equal(t, "https://arcreel.heibaidao.cn/api/v1/remote-generation/staging/abc/image.png", out[0].URL)
	assert.Equal(t, "first_frame", out[0].Type)
	assert.Equal(t, "last_frame", out[1].Type)
}

func TestRelocateMediaKeepsDataURLUntouched(t *testing.T) {
	dataURL := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: dataURL},
	}
	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			t.Fatal("data URL 不该触发下载")
			return nil, "", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			t.Fatal("data URL 不该触发转存")
			return "", nil
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	out, err := relocateMedia(context.Background(), media, client)
	require.NoError(t, err)
	assert.Equal(t, dataURL, out[0].URL)
}

func TestRelocateMediaSkipsNonImageMedia(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: "https://example.invalid/clip.mp4"},
		{Type: "reference_audio", URL: "https://example.invalid/voice.wav"},
	}
	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			t.Fatal("音频/视频不该触发下载")
			return nil, "", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			return "", nil
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	out, err := relocateMedia(context.Background(), media, client)
	require.NoError(t, err)
	assert.Equal(t, media, out)
}

func TestRelocateMediaFailsWhenDownloadFails(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer origin.Close()

	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return nil, "", assert.AnError
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			return "", nil
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/missing.png"},
	}
	_, err := relocateMedia(context.Background(), media, client)
	require.Error(t, err)
	// 错误里要点明是哪个位置的素材失败
	assert.Contains(t, err.Error(), "first frame")
}

// 转存后交给上游的地址必须自己可达；转存失败要明确报错而不是静默回退原 URL
// （回退等于把客户的境外图床又交给上游，注定在生成阶段失败）。
func TestRelocateMediaFailsWhenStagingFails(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngMagic)
	}))
	defer origin.Close()

	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return pngMagic, "image/png", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			return "", assert.AnError
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: origin.URL + "/a.png"},
	}
	_, err := relocateMedia(context.Background(), media, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reference image")
}

// 代下载回来的字节必须真是图片，不能是「200 + 错误页 HTML」这类假图。
func TestRelocateMediaRejectsNonImagePayload(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("<html>404 not found</html>"))
	}))
	defer origin.Close()

	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return []byte("<html>404 not found</html>"), "image/png", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			t.Fatal("非图片不该被转存")
			return "", nil
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/fake.png"},
	}
	_, err := relocateMedia(context.Background(), media, client)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "first frame")
}

// 体积上限与上游对齐（20MB），超限直接拒单，不把超大文件塞进 staging。
func TestRelocateMediaRejectsOversizedImage(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngMagic)
	}))
	defer origin.Close()

	client := &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return make([]byte, maxReferenceImageBytes+1), "image/png", nil
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			t.Fatal("超限不该被转存")
			return "", nil
		},
		stagingURL: func(stagingPath string) string { return stagingPath },
	}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/big.png"},
	}
	_, err := relocateMedia(context.Background(), media, client)
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "limit")
}

// ---------------------------------------------------------------------------
// adaptor 层：降级策略
// ---------------------------------------------------------------------------

// 暂存图床不可用（没配漫屋渠道）时**不能拒单**：客户若给了自己公司的 CDN
// 地址仍然可用，我们不该因为自己的辅助设施故障而阻断用户的正常请求。
func TestRelocateMediaForUpstreamDegradesWhenStagingUnavailable(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngMagic)
	}))
	defer origin.Close()

	original := resolveStagingChannel
	resolveStagingChannel = func() (*model.Channel, error) {
		return nil, assert.AnError
	}
	defer func() { resolveStagingChannel = original }()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &TaskAdaptor{}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/shot.png"},
	}
	out, taskErr := adaptor.relocateMediaForUpstream(ctx, media)
	require.Nil(t, taskErr)
	// 原样放行
	assert.Equal(t, origin.URL+"/shot.png", out[0].URL)
}

// 转存过程中某张图拉不到 → 必须拒单（400），不能让上游继续失败。
func TestRelocateMediaForUpstreamRejectsUnreachableImage(t *testing.T) {
	disableSSRF(t)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer origin.Close()

	original := resolveStagingChannel
	resolveStagingChannel = func() (*model.Channel, error) {
		return &model.Channel{Id: 20, Key: "svc-token", BaseURL: ptrString("https://arcreel.example.com")}, nil
	}
	defer func() { resolveStagingChannel = original }()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &TaskAdaptor{}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: origin.URL + "/missing.png"},
	}
	_, taskErr := adaptor.relocateMediaForUpstream(ctx, media)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

// 全是 data URL 时不该白跑一趟下载与转存。
func TestRelocateMediaForUpstreamSkipsWhenNothingToRelocate(t *testing.T) {
	original := resolveStagingChannel
	called := false
	resolveStagingChannel = func() (*model.Channel, error) {
		called = true
		return nil, assert.AnError
	}
	defer func() { resolveStagingChannel = original }()

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	adaptor := &TaskAdaptor{}
	media := []relaycommon.TaskMedia{
		{Type: "first_frame", URL: "data:image/png;base64,iVBORw0KGgo="},
	}
	out, taskErr := adaptor.relocateMediaForUpstream(ctx, media)
	require.Nil(t, taskErr)
	assert.Equal(t, media, out)
	assert.False(t, called, "没有需要转存的素材时不该去解析暂存图床渠道")
}

func ptrString(s string) *string { return &s }

// 慢图床不能无限期占着建单连接：单图下载有独立超时。
// 实测 litter.catbox.moe 单次拉取要 88 秒以上，探测阶段只给 8 秒。
func TestDownloadImageTimesOutOnSlowHost(t *testing.T) {
	disableSSRF(t)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(pngMagic)
	}))
	defer slow.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := downloadImage(ctx, slow.URL+"/slow.png", "")
	require.Error(t, err)
	assert.Contains(t, strings.ToLower(err.Error()), "context")
}
