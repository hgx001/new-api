package controller

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allowLoopbackPorts 放开 SSRF 的私有 IP 与端口限制，让 httptest 的随机端口能通过
// service/http_client.go 的 checkRedirect。SSRF 防护本身**保持开启**（只放行本机），
// 这样测的仍是生产那条真实重定向路径，而不是绕开校验。
func allowLoopbackPorts(t *testing.T, servers ...*httptest.Server) {
	t.Helper()
	setting := system_setting.GetFetchSetting()
	saved := *setting
	setting.AllowPrivateIp = true
	ports := make([]string, 0, len(servers))
	for _, srv := range servers {
		ports = append(ports, strconv.Itoa(srv.Listener.Addr().(*net.TCPAddr).Port))
	}
	setting.AllowedPorts = ports
	t.Cleanup(func() { *setting = saved })
}

// erchun 成片在 /v1/tasks/{upstream_id}/content，必须带渠道密钥才能取回；上游
// delivery=cdn 时会 302 跳到第三方 CDN 签名地址。生产依赖 Go net/http 在**跨 host**
// 重定向时自动剥掉 Authorization，密钥才不会跟着跳给 CDN。
//
// 这个安全性此前**只写在注释里**，没有任何测试锁住它。只要有人为了超时/重试给
// service.GetHttpClientWithProxy 返回的 client 加自定义 CheckRedirect（很常见的改动），
// 渠道密钥就会静默地发到第三方 CDN，用户侧毫无感知。
//
// ⚠️ 断言写法上的坑：Go 的 shouldCopyHeaderOnRedirect 比对的是 **host（不含端口）**。
// httptest 两个 server 即使端口不同，hostname 都是 127.0.0.1 → 判为同源 → Authorization
// **会被保留**。所以 CDN 侧必须换主机名（127.0.0.1 → localhost）才能真实触发剥除逻辑。
func TestChannelKeyIsNotForwardedAcrossHostRedirect(t *testing.T) {
	initHTTPClient(t)

	var leaked string
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("video-bytes-from-cdn"))
	}))
	defer cdn.Close()
	// 换主机名，让 Go 判定为跨 host；仅换端口无效。
	cdnURL := strings.Replace(cdn.URL, "127.0.0.1", "localhost", 1)
	require.NotEqual(t, cdn.URL, cdnURL, "CDN 侧必须与源站不同 host 才能验证剥除逻辑")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 第一跳必须确实带了渠道密钥，否则本测试就是空转（根本没走到剥除逻辑）
		assert.Equal(t, "Bearer sk-channel-secret", r.Header.Get("Authorization"),
			"取 erchun content 时必须带渠道密钥")
		http.Redirect(w, r, cdnURL+"/final.mp4", http.StatusFound)
	}))
	defer upstream.Close()
	allowLoopbackPorts(t, cdn, upstream)

	client, err := service.GetHttpClientWithProxy("")
	require.NoError(t, err)
	require.NotNil(t, client)

	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/v1/tasks/55102/content", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer sk-channel-secret")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "video-bytes-from-cdn", string(body), "应跟到 CDN 正常拿到内容")
	assert.Empty(t, leaked, "🚨 渠道密钥被转发到了第三方 CDN")
}

// 同一 host 的重定向（含仅端口不同）要保留 Authorization —— 否则 CDN 与上游同机部署时
// 内容取不回来。把这个方向也钉住，避免有人「为了安全」过度剥离导致线上取不到成片。
func TestChannelKeySurvivesSameHostRedirect(t *testing.T) {
	initHTTPClient(t)

	var seen string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("ok"))
	}))
	defer downstream.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, downstream.URL+"/x.mp4", http.StatusFound)
	}))
	defer upstream.Close()
	allowLoopbackPorts(t, downstream, upstream)

	client, err := service.GetHttpClientWithProxy("")
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodGet, upstream.URL+"/content", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer sk-channel-secret")

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	// 两个 server 同为 127.0.0.1 → Go 判同源 → 凭证保留
	assert.Equal(t, "Bearer sk-channel-secret", seen)
}