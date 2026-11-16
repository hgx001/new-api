package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initHTTPClient 初始化全局 http client（生产由 main 启动时 InitHttpClient 完成，
// resolveManwuResultURL 经 service.GetHttpClientWithProxy 取它）。
func initHTTPClient(t *testing.T) {
	t.Helper()
	service.InitHttpClient()
}

// newManwuTask 造一个带结果地址的任务（PrivateData 是 JSON 列的镜像结构）。
func newManwuTask(upstreamID, resultURL string) *model.Task {
	return &model.Task{
		TaskID: "task_public001",
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: upstreamID,
			ResultURL:      resultURL,
		},
	}
}

// 漫屋结果分两类：一类是 dola 这类公网 CDN 直链（直传、不带凭证），另一类是
// ArcReel 自有托管的相对路径（必须回查 job 并带渠道密钥）。混淆两者会导致
// 「交付打不开的链接」或「把 service token 发给第三方 CDN」。
func TestResolveManwuResultURLPassesThroughPublicCDN(t *testing.T) {
	task := newManwuTask("job-1", "https://v19-dola.dola.com/v/clip.mp4")

	url, needsAuth, err := resolveManwuResultURL("https://arcreel.example.com", "sk-secret", "", task)
	require.NoError(t, err)
	assert.Equal(t, "https://v19-dola.dola.com/v/clip.mp4", url)
	assert.False(t, needsAuth, "第三方 CDN 绝不能带渠道密钥")
}

// ResultURL 是本服务的 content 代理地址（即 adaptor 拒透传相对路径后的落库值）时，
// 必须回查 ArcReel 拿真实地址；拿到的是相对路径就拼上渠道基址并标记需要鉴权。
func TestResolveManwuResultURLResolvesSelfHostedViaJob(t *testing.T) {
	initHTTPClient(t)
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jobId":"job-1","status":"ready","sourceUrl":"/api/v1/remote-generation/jobs/job-1/outputs/o.png"}`)
	}))
	defer srv.Close()

	task := newManwuTask("job-1", "https://newapi.example.com/v1/videos/task_public001/content")
	url, needsAuth, err := resolveManwuResultURL(srv.URL, "sk-secret", "", task)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/remote-generation/jobs/job-1", gotPath, "回查的是 job 端点")
	assert.Equal(t, "Bearer sk-secret", gotAuth)
	assert.Equal(t, srv.URL+"/api/v1/remote-generation/jobs/job-1/outputs/o.png", url)
	assert.True(t, needsAuth, "ArcReel 自有路径需要渠道密钥")
}

// 上游还没产出地址时必须报错，不能返回空地址让下游拿到 200 空包。
func TestResolveManwuResultURLErrorsWhenSourceMissing(t *testing.T) {
	initHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"jobId":"job-1","status":"failed","error":"worker offline"}`)
	}))
	defer srv.Close()

	task := newManwuTask("job-1", "https://newapi.example.com/v1/videos/task_public001/content")
	_, _, err := resolveManwuResultURL(srv.URL, "sk-secret", "", task)
	require.Error(t, err)
}

// 上游非 200 时把状态带进错误，便于排障（不要静默当成「无结果」）。
func TestResolveManwuResultURLPropagatesUpstreamError(t *testing.T) {
	initHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"detail":"invalid worker token"}`)
	}))
	defer srv.Close()

	task := newManwuTask("job-1", "https://newapi.example.com/v1/videos/task_public001/content")
	_, _, err := resolveManwuResultURL(srv.URL, "bad-token", "", task)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

// ServerAddress 改版后（域名迁移/换端口），落库的老代理地址仍必须被识别为自代理，
// 否则会把本站 /content 当成上游直链去拉 → 自我递归。
func TestIsSelfProxyPathIsHostIndependent(t *testing.T) {
	assert.True(t, isSelfProxyPath("http://localhost:3000/v1/videos/task_x/content"))
	assert.True(t, isSelfProxyPath("https://newapi.example.com/v1/videos/task_x/content"))
	assert.False(t, isSelfProxyPath("https://cdn.example.com/v1/videos/task_x/content.mp4"))
	assert.False(t, isSelfProxyPath("/api/v1/remote-generation/jobs/job-1/outputs/o.png"))
	assert.False(t, isSelfProxyPath("not a url"))
}

// ⚠️ 2026-10-08 生产实测(langdu gemini-web-video 任务)回归: ArcReel/Worker 把
// sourceUrl 拼成了**绝对** URL 落库, resolveManwuResultURL 按"公网直链"透传,
// 客户端 401、/content 代理也 401→502, 成片完全拿不到。自有托管路径必须回查
// job 并带渠道密钥, 与它是不是绝对地址无关。
func TestResolveManwuResultURLResolvesAbsoluteOwnedAssetViaJob(t *testing.T) {
	initHTTPClient(t)
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"jobId":"job-1","status":"ready","sourceUrl":"/api/v1/remote-generation/jobs/job-1/outputs/output.mp4"}`)
	}))
	defer srv.Close()

	abs := srv.URL + "/api/v1/remote-generation/jobs/job-1/outputs/output.mp4"
	task := newManwuTask("job-1", abs)
	url, needsAuth, err := resolveManwuResultURL(srv.URL, "sk-secret", "", task)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/remote-generation/jobs/job-1", gotPath, "绝对自有 URL 也必须回查 job")
	assert.Equal(t, "Bearer sk-secret", gotAuth)
	assert.Equal(t, srv.URL+"/api/v1/remote-generation/jobs/job-1/outputs/output.mp4", url)
	assert.True(t, needsAuth)
}

// ⚠️ 2026-11-16 生产实测（规则九音视频改派 e2e）：CapCut 超分任务落库的
// ResultURL 是 cos-proxy:// 占位符，manwu 分支不认它 → resolveManwuResultURL
// 回查上游 job 拿 sourceUrl，客户按 720P 买到手的却是 480p **原片**
// （md5 与原片完全一致，MPS 超分出的 720P 产物从未交付）。
//
// 这里锁两件事：
//  1. 占位符必须被认领（302 到 COS 预签名直链，而不是回查 job）；
//  2. 非占位符的普通任务不受影响（helper 放行，走原有回源逻辑）。
func TestServeCOSProxyRedirectIfMarked(t *testing.T) {
	// 给 tencentmps_setting 灌一组测试凭据：PresignCOSURL 是纯本地签名
	//（GetPresignedURL 不发网络请求），假 AK/SK 也能签出结构合法的 URL。
	saved := tencentmps_setting.MarshalJSONString()
	require.NoError(t, tencentmps_setting.UpdateFromJSONString(
		`{"secret_id":"AKIDtest","secret_key":"secrettest"}`))
	t.Cleanup(func() { _ = tencentmps_setting.UpdateFromJSONString(saved) })

	gin.SetMode(gin.TestMode)

	t.Run("cos-proxy 占位符 302 预签名直链", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_x/content", nil)

		marker := "cos-proxy://mps-test-1397292858/ap-guangzhou/mps_api_test/sr_task_abc.mp4"
		handled := serveCOSProxyRedirectIfMarked(c, "task_x", marker)

		require.True(t, handled, "占位符必须被认领")
		// gin 的 WriteHeader 延迟到引擎收尾（WriteHeaderNow）才落到 recorder，
		// 单测里手动补一次 —— 与真实请求生命周期等价。
		c.Writer.WriteHeaderNow()
		require.Equal(t, http.StatusFound, w.Code, "应为 302 直链")
		loc := w.Header().Get("Location")
		require.NotEmpty(t, loc)
		assert.Contains(t, loc, "mps-test-1397292858", "签名地址指向占位符里的桶")
		assert.Contains(t, loc, "mps_api_test/sr_task_abc.mp4", "签名地址指向占位符里的对象")
		assert.Contains(t, loc, "q-signature=", "必须是带签名的临时直链（COS 签名体系）")
	})

	t.Run("普通 ResultURL 放行", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_x/content", nil)

		handled := serveCOSProxyRedirectIfMarked(c, "task_x", "https://v19-dola.dola.com/v/clip.mp4")

		assert.False(t, handled, "非占位符必须放行给渠道回源逻辑")
		assert.Equal(t, http.StatusOK, w.Code, "放行时不得写出任何响应")
	})

	t.Run("大小写不敏感（与 ParseCOSProxyMarker 同口径）", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/v1/videos/task_x/content", nil)

		handled := serveCOSProxyRedirectIfMarked(c, "task_x",
			"COS-PROXY://mps-test-1397292858/ap-guangzhou/mps_api_test/sr_task_abc.mp4")
		assert.True(t, handled, "大写形态同样要认领（历史数据可能大小写漂移）")
	})
}
