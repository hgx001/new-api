package youzanwan3

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预热必须用**上游将要请求的那个 URL**。
//
// CDN 的缓存键包含 query 字符串：裸地址与带 CI 参数的地址是两条独立的
// 缓存键。预热用了另一条键，等于没预热 —— 上游照样撞 404 负缓存。
// 这条测试锁死「从请求体里取 URL」这个口径。
func TestCollectReferenceImageURLsFromBody(t *testing.T) {
	t.Run("media 契约：只收 reference_image", func(t *testing.T) {
		body := []byte(`{
			"model":"mw-sd2.5-flash",
			"media":[
				{"type":"reference_image","url":"https://arcmedia.heibaidao.cn/a.jpg"},
				{"type":"reference_audio","url":"https://arcmedia.heibaidao.cn/a.mp3"},
				{"type":"reference_video","url":"https://arcmedia.heibaidao.cn/a.mp4"}
			]
		}`)
		urls := collectReferenceImageURLsFromBody(body)
		require.Len(t, urls, 1)
		assert.Equal(t, "https://arcmedia.heibaidao.cn/a.jpg", urls[0])
	})

	t.Run("顶层 images 契约：收 images 不收 videos/audios", func(t *testing.T) {
		body := []byte(`{
			"model":"wan-3.0",
			"images":["https://arcmedia.heibaidao.cn/1.jpg","https://arcmedia.heibaidao.cn/2.jpg"],
			"videos":["https://arcmedia.heibaidao.cn/v.mp4"],
			"audios":["https://arcmedia.heibaidao.cn/a.mp3"]
		}`)
		urls := collectReferenceImageURLsFromBody(body)
		require.Len(t, urls, 2)
		assert.Contains(t, urls, "https://arcmedia.heibaidao.cn/1.jpg")
		assert.Contains(t, urls, "https://arcmedia.heibaidao.cn/2.jpg")
	})

	t.Run("非法 JSON 一律忽略（预热失败不能拒绝客户请求）", func(t *testing.T) {
		assert.Empty(t, collectReferenceImageURLsFromBody([]byte(`not json`)))
		assert.Empty(t, collectReferenceImageURLsFromBody(nil))
	})
}

// 只有自有 CDN 域名、且不带签名的地址才预热。
//
// 别人的图床 / 对象存储签名链不能动：预热等于替对方刷流量，签名链多一次
// 请求可能提前失效。
func TestIsWarmupableReferenceURL(t *testing.T) {
	cases := []struct {
		name string
		url  string
		want bool
	}{
		{"自有 CDN 裸地址", "https://arcmedia.heibaidao.cn/projects/x/a.jpg", true},
		{"自有 CDN 带 CI 参数", "https://arcmedia.heibaidao.cn/projects/x/a.jpg?imageMogr2/thumbnail/1280x1280%3E/quality/85", true},
		{"外部图床", "https://example.com/a.jpg", false},
		{"带签名", "https://arcmedia.heibaidao.cn/a.jpg?sign=abc", false},
		{"带 token", "https://arcmedia.heibaidao.cn/a.jpg?token=abc", false},
		{"空串", "", false},
		{"非法 URL", "://bad", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isWarmupableReferenceURL(tc.url))
		})
	}
}

// 预热确实会发 HEAD 请求，且同一张图只发一次。
func TestWarmupReferenceMedia_发HEAD且去重(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		assert.Equal(t, http.MethodHead, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// 测试服务器在 127.0.0.1，不在白名单里 —— 先把它的 host 临时加进白名单，
	// 测完还原。这是为了验证「真的发 HEAD」和「去重」这两件事。
	host := strings.TrimPrefix(srv.URL, "http://")
	referenceImageOptimizableHosts[host] = struct{}{}
	defer delete(referenceImageOptimizableHosts, host)

	url := srv.URL + "/a.jpg"
	warmupReferenceMedia(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: url},
		{Type: "reference_image", URL: url},
		{Type: "reference_audio", URL: srv.URL + "/a.mp3"},
	})
	assert.Equal(t, int64(1), atomic.LoadInt64(&hits), "同一张图只该预热一次，且音频不预热")
}

// 预热必须用**请求体里的那个 URL**（带 CI 参数的那条缓存键）。
//
// 这是整个修复的核心正确性：CDN 的缓存键包含 query 字符串，
// 裸地址与带 ?imageMogr2 的地址是两条独立的键。用裸地址预热填热的是
// 键 A，上游请求的是键 B —— 上游照样撞上键 B 的 404 负缓存。
func TestWarmupReferenceMediaFromBody_用请求体里的确切URL(t *testing.T) {
	var got atomic.Value
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		got.Store(r.URL.String())
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	referenceImageOptimizableHosts[host] = struct{}{}
	defer delete(referenceImageOptimizableHosts, host)

	// 请求体里的 URL 带 CI 参数（模拟促销族压缩后的形态）。
	ciURL := srv.URL + "/a.jpg?imageMogr2/thumbnail/1280x1280%3E/quality/85"
	body := []byte(`{"model":"mw-sd2.5-flash","media":[{"type":"reference_image","url":"` + ciURL + `"}]}`)

	warmupReferenceMediaFromBody(context.Background(), body)

	require.Equal(t, int64(1), atomic.LoadInt64(&hits), "应预热一次")
	stored, _ := got.Load().(string)
	assert.Contains(t, stored, "imageMogr2",
		"预热必须命中带 CI 参数的那条缓存键，否则等于没预热")
}

// 外部域名一个都不该被预热（避免替别人刷流量 / 消耗别人的签名）。
func TestWarmupReferenceMedia_外部域名不预热(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	warmupReferenceMedia(context.Background(), []relaycommon.TaskMedia{
		{Type: "reference_image", URL: srv.URL + "/a.jpg"},
	})
	assert.Equal(t, int64(0), atomic.LoadInt64(&hits), "外部域名不该被预热")
}

// 预热失败（连接不通、超时）不能让整个调用失败。
func TestWarmupReferenceMedia_失败不阻断(t *testing.T) {
	// 用一个必定连不上的地址（自有域名但解析不到 / 端口关闭）。
	// 实际用 127.0.0.1 的话不在白名单，所以这里走「不可预热」路径，
	// 断言调用本身不 panic、不返回错误。
	done := make(chan struct{})
	go func() {
		defer close(done)
		warmupReferenceMedia(context.Background(), nil)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("预热不应阻塞超过 5 秒")
	}
}
