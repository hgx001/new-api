package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

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
