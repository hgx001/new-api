package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestVideoProxyRedirectClient_CrossHostNotFollowed 覆盖本功能的核心断言：
// 跨主机 302 必须**不被跟随**，这样 VideoProxy 才能把它透传给客户端，
// 让视频字节直连 CDN、不经过本机 3Mbps 出口。
func TestVideoProxyRedirectClient_CrossHostNotFollowed(t *testing.T) {
	// 目标站（模拟 CDN）：一旦被访问就说明重定向被跟随了
	cdnHits := 0
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cdnHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer cdn.Close()

	// 上游站（模拟 ArcReel）：返回 302 → CDN
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", cdn.URL+"/projects/remote_outputs/x.mp4")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	client := videoProxyRedirectClient(&http.Client{})
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/output", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound {
		t.Fatalf("跨主机重定向应保持 302 交给调用方透传，实际 %d", resp.StatusCode)
	}
	if cdnHits != 0 {
		t.Fatalf("跨主机重定向被跟随了（CDN 被访问 %d 次），视频字节会经过本机出口", cdnHits)
	}
	if loc := resp.Header.Get("Location"); loc == "" {
		t.Fatal("302 响应缺少 Location，透传会失败")
	}
}

// TestVideoProxyRedirectClient_SameHostFollowed 保证同主机重定向仍然跟随。
// 这条不能破：Gemini / Vertex / OpenAI 的同主机重定向要靠请求头里的凭证
// 才能取到内容，透传给客户端必然 401。
func TestVideoProxyRedirectClient_SameHostFollowed(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/output" {
			// 同主机重定向
			w.Header().Set("Location", srv.URL+"/real")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("video-bytes"))
	}))
	defer srv.Close()

	client := videoProxyRedirectClient(&http.Client{})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/output", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("同主机重定向应被跟随并最终拿到 200，实际 %d", resp.StatusCode)
	}
}

// TestVideoProxyRedirectClient_TooManyRedirects 防止同主机重定向死循环。
func TestVideoProxyRedirectClient_TooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", srv.URL+"/loop")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	client := videoProxyRedirectClient(&http.Client{})
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/loop", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	if _, err := client.Do(req); err == nil {
		t.Fatal("同主机无限重定向应当报错，实际没有")
	}
}

// TestVideoProxyRelayRedirectEnabled 覆盖 kill switch：
// 默认开启，显式关闭值才返回 false。这个开关的作用是**不重新构建镜像就能回滚**。
func TestVideoProxyRelayRedirectEnabled(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", true},
		{"1", true},
		{"true", true},
		{"yes", true},
		{"0", false},
		{"false", false},
		{"off", false},
		{"FALSE", false},
		{" off ", false},
	}
	for _, tc := range cases {
		t.Setenv(videoProxyRelayRedirectEnv, tc.value)
		if got := videoProxyRelayRedirectEnabled(); got != tc.want {
			t.Errorf("env=%q: got %v, want %v", tc.value, got, tc.want)
		}
	}
}

// TestIsRelayableRedirect 确认只透传真正的重定向状态码，别把 304/3xx 边缘值放过去。
func TestIsRelayableRedirect(t *testing.T) {
	relayable := []int{
		http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect,
	}
	for _, code := range relayable {
		if !isRelayableRedirect(code) {
			t.Errorf("%d 应当可透传", code)
		}
	}
	notRelayable := []int{
		http.StatusOK,
		http.StatusNotModified,
		http.StatusMultipleChoices,
		http.StatusUseProxy,
		http.StatusBadGateway,
	}
	for _, code := range notRelayable {
		if isRelayableRedirect(code) {
			t.Errorf("%d 不应透传", code)
		}
	}
}
