package manwu

// CapCut 备选链路「交付前超分」钩子的测试（2026-11-14）。
//
// 要锁的核心不变量：**只有规则五降级过来的 CapCut 任务**才超分。
//
//	客户直接买 cp-seedance-2.5flash      → 不超分（它自己就是商品档位）；
//	客户直接买 seedance-2-5（豆包正常）  → 不超分（执行模型不是 CapCut）；
//	规则七（电商内容 → 2.5 Flash）        → 不超分（计费原点不是豆包）。
//
// 误判的后果分别是「白花钱多跑一次 MPS」和「模糊了成片」—— 两者都不会报错。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/model"
)

// capcutAltTask 造一条「规则五降级到 CapCut」的任务。
func capcutAltTask() *model.Task {
	task := &model.Task{TaskID: "task_alt_1"}
	task.Properties.OriginModelName = ModelCapcut25Flash
	task.PrivateData.RouteOriginModel = "seedance-2-5"
	return task
}

// 只认「执行 CapCut 2.5 Flash + 计费原点是豆包」这一种组合。
func TestNeedsCapcutFailoverUpscaleMatrix(t *testing.T) {
	assert.True(t, needsCapcutFailoverUpscale(capcutAltTask()), "降级任务必须超分")

	direct := &model.Task{TaskID: "task_direct"}
	direct.Properties.OriginModelName = ModelCapcut25Flash
	assert.False(t, needsCapcutFailoverUpscale(direct),
		"客户直接买 2.5 Flash 时不该超分（它本来就是该档商品）")

	doubao := &model.Task{TaskID: "task_doubao"}
	doubao.Properties.OriginModelName = "seedance-2-5"
	doubao.PrivateData.RouteOriginModel = "seedance-2-5"
	assert.False(t, needsCapcutFailoverUpscale(doubao),
		"执行模型是豆包时不该超分（豆包本来就是 720P 交付）")

	ecom := &model.Task{TaskID: "task_ecom"}
	ecom.Properties.OriginModelName = ModelCapcut25Flash
	ecom.PrivateData.RouteOriginModel = "seedance2.5-特惠"
	assert.False(t, needsCapcutFailoverUpscale(ecom),
		"规则七的促销族改派不该被当成豆包降级")

	other := &model.Task{TaskID: "task_other"}
	other.Properties.OriginModelName = ModelCapcut25
	other.PrivateData.RouteOriginModel = "seedance-2-5"
	assert.False(t, needsCapcutFailoverUpscale(other), "只有 2.5 Flash 这一档走 480p")

	assert.False(t, needsCapcutFailoverUpscale(nil), "nil 任务必须安全")
}

// 钩子注入后：匹配的任务被委托给超分实现（handled=true 让任务保持处理中）。
func TestAfterUpstreamSuccessDelegatesWhenHookInjected(t *testing.T) {
	var submitted bool
	SetSuperResolutionHooks(
		func(_ context.Context, task *model.Task, upstreamURL string) (bool, error) {
			submitted = true
			assert.Equal(t, "https://cdn.example.com/raw.mp4", upstreamURL)
			assert.Equal(t, "task_alt_1", task.TaskID)
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	adaptor := &TaskAdaptor{}
	handled, err := adaptor.AfterUpstreamSuccess(
		context.Background(), capcutAltTask(), "https://cdn.example.com/raw.mp4")
	require.NoError(t, err)
	assert.True(t, handled, "降级任务提交超分后必须保持「处理中」")
	assert.True(t, submitted)
}

// 不匹配的任务：既不提交也不改交付路径（保持一跳行为）。
func TestAfterUpstreamSuccessSkipsOtherTasks(t *testing.T) {
	called := false
	SetSuperResolutionHooks(
		func(context.Context, *model.Task, string) (bool, error) {
			called = true
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	direct := &model.Task{TaskID: "task_direct"}
	direct.Properties.OriginModelName = ModelCapcut25Flash

	adaptor := &TaskAdaptor{}
	handled, err := adaptor.AfterUpstreamSuccess(
		context.Background(), direct, "https://cdn.example.com/raw.mp4")
	require.NoError(t, err)
	assert.False(t, handled)
	assert.False(t, called, "非降级任务不该被提交超分")
}

// 钩子未注入（装配缺失）：不接管、不报错（交付原片），但必须让任务照常收尾。
func TestAfterUpstreamSuccessWithoutHookFallsBackToOriginal(t *testing.T) {
	SetSuperResolutionHooks(nil, nil)
	adaptor := &TaskAdaptor{}

	handled, err := adaptor.AfterUpstreamSuccess(
		context.Background(), capcutAltTask(), "https://cdn.example.com/raw.mp4")
	require.NoError(t, err)
	assert.False(t, handled, "没有超分能力时按原片交付，不把任务挂在处理中")
}

// 轮询：只有带后处理状态且匹配的任务才推进；其余一律「没有后处理」。
func TestPollPostProcessOnlyForCapcutAltTasks(t *testing.T) {
	polled := 0
	SetSuperResolutionHooks(
		func(context.Context, *model.Task, string) (bool, error) { return true, nil },
		func(context.Context, *model.Task) (bool, error) {
			polled++
			return true, nil
		},
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	adaptor := &TaskAdaptor{}

	noPP := capcutAltTask()
	done, err := adaptor.PollPostProcess(context.Background(), noPP)
	require.NoError(t, err)
	assert.True(t, done, "没有后处理状态的任务直接收尾")
	assert.Equal(t, 0, polled)

	withPP := capcutAltTask()
	withPP.PrivateData.PostProcess = &model.TaskPostProcess{Stage: "submitted", TaskID: "mps-1"}
	done, err = adaptor.PollPostProcess(context.Background(), withPP)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, 1, polled, "降级任务必须推进后处理轮询")

	other := &model.Task{TaskID: "task_direct"}
	other.Properties.OriginModelName = ModelCapcut25Flash
	other.PrivateData.PostProcess = &model.TaskPostProcess{Stage: "submitted", TaskID: "mps-2"}
	done, err = adaptor.PollPostProcess(context.Background(), other)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, 1, polled, "非降级任务不该被推进后处理")
}

// ───────────────────────── 成片地址解析（2026-11-14 实测修复）─────────

// ArcReel 走自有托管时 ParseTaskResult 不吐地址，service 层传下来的
// upstreamURL 是空串。此时必须从任务的原始响应里把 sourceUrl 找回来，
// 否则超分拿不到输入、静默跳过 —— 客户拿到 480P 却按 720P 计费。
func TestAfterUpstreamSuccessFallsBackToTaskSourceURL(t *testing.T) {
	var got string
	SetSuperResolutionHooks(
		func(_ context.Context, _ *model.Task, upstreamURL string) (bool, error) {
			got = upstreamURL
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	task := capcutAltTask()
	task.Data = json.RawMessage(`{"status":"ready","sourceUrl":"https://cdn.example.com/out.mp4"}`)

	adaptor := &TaskAdaptor{}
	handled, err := adaptor.AfterUpstreamSuccess(context.Background(), task, "")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "https://cdn.example.com/out.mp4", got,
		"upstreamURL 为空时必须回落到任务原始响应里的 sourceUrl")
}

// ArcReel 的产物接口需要渠道密钥（无鉴权 401），但它会 302 到公开 CDN。
// MPS 只能拉无鉴权地址，所以要先用密钥把 Location 读出来。
func TestAfterUpstreamSuccessResolvesAuthedMediaToPublicURL(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Location", "https://arcmedia.example.com/public/out.mp4")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	var got string
	SetSuperResolutionHooks(
		func(_ context.Context, _ *model.Task, upstreamURL string) (bool, error) {
			got = upstreamURL
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	task := capcutAltTask()
	task.Data = json.RawMessage(`{"sourceUrl":"` + server.URL + `/api/v1/remote-generation/jobs/j1/outputs/o1.mp4"}`)

	adaptor := &TaskAdaptor{apiKey: "sk-test"}
	handled, err := adaptor.AfterUpstreamSuccess(context.Background(), task, "")
	require.NoError(t, err)
	assert.True(t, handled)
	assert.Equal(t, "Bearer sk-test", seenAuth, "换公开地址必须带上渠道密钥")
	assert.Equal(t, "https://arcmedia.example.com/public/out.mp4", got,
		"交给 MPS 的必须是 302 之后的公开地址")
}

// 完全拿不到地址（既没有 upstreamURL 也没有 sourceUrl）：
// 不接管、按原片交付，任务不能挂在「处理中」。
func TestAfterUpstreamSuccessWithoutMediaURLDeliversOriginal(t *testing.T) {
	called := false
	SetSuperResolutionHooks(
		func(context.Context, *model.Task, string) (bool, error) {
			called = true
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	task := capcutAltTask()
	task.Data = json.RawMessage(`{"status":"ready"}`)

	adaptor := &TaskAdaptor{}
	handled, err := adaptor.AfterUpstreamSuccess(context.Background(), task, "")
	require.NoError(t, err)
	assert.False(t, handled)
	assert.False(t, called, "没有可抓取的成片地址时不该提交注定失败的超分任务")
}

// ───────────────────────── ArcReel 产物三种落法（2026-11-14 实测）─────────
//
// 上游把成片放在两个域名下，有三种可观察的响应形态：
//
//	302 + Location → 还没内联，跳转到 arcmedia 公网地址；
//	200 + 正文     → 已内联，arcreel 直接吐 mp4（无鉴权 401）；
//	206 + 正文     → 同上，按 Range 只给一段。
//
// 之前只认 Location，于是 200/206 被判成「拿不到地址」→ 超分静默跳过。
// 这条用例锁死 206 也要能推出公网地址。

// 上游直出正文（206）时，必须推出 arcmedia 公网地址 ——
// MPS 从公网拉片且不能带 Authorization。
func TestResolveCapcutMediaURLDerivesPublicURLWhenBodyServedDirectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟上游：直出 206 正文，没有 Location。
		w.Header().Set("Content-Type", "video/mp4")
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte("x"))
	}))
	defer server.Close()

	// 把服务地址改造成 arcreel 那种路径形态，才能命中推导逻辑。
	raw := strings.Replace(server.URL, "://", "://arcreel.", 1) +
		"/api/v1/remote-generation/jobs/job-1/outputs/out-1.mp4"

	// 单测里 httptest 监听的是 IP，推不出真实的 arcmedia 域，
	// 所以把推导这一步替换掉，只验证「上游直出正文时确实会去推导」。
	previous := deriveArcmediaPublicURLFunc
	deriveArcmediaPublicURLFunc = func(string) string {
		return "https://arcmedia.example.com/projects/remote_outputs/job-1/out-1.mp4"
	}
	t.Cleanup(func() { deriveArcmediaPublicURLFunc = previous })

	adaptor := &TaskAdaptor{apiKey: "sk-test"}
	got := adaptor.resolveCapcutMediaURL(context.Background(), raw)
	assert.Equal(t, "https://arcmedia.example.com/projects/remote_outputs/job-1/out-1.mp4", got,
		"上游直出正文时必须改用 arcmedia 公网地址")
	assert.NotContains(t, got, "/api/v1/remote-generation/",
		"推出来的地址不能再是需要鉴权的那条")
}

// 推导不出（路径不匹配）时返回空串 —— 不能凭空造地址。
func TestDeriveArcmediaPublicURLRejectsUnknownPath(t *testing.T) {
	assert.Equal(t, "", deriveArcmediaPublicURL("https://example.com/some/other/path.mp4"))
	assert.Equal(t, "", deriveArcmediaPublicURL(""))
}

// 路径形态正确时，推导结果要精确。
func TestDeriveArcmediaPublicURL(t *testing.T) {
	got := deriveArcmediaPublicURL(
		"https://arcreel.heibaidao.cn/api/v1/remote-generation/jobs/gen-abc123/outputs/output-x.mp4")
	assert.Equal(t,
		"https://arcmedia.heibaidao.cn/projects/remote_outputs/gen-abc123/output-x.mp4",
		got)
}

// IP 主机推不出 arcmedia 对应域（arcmedia.127.0.0.1 根本不存在），
// 这种情况必须返回空串而不是造一个无效地址。
func TestDeriveArcmediaPublicURLRejectsIPHost(t *testing.T) {
	got := deriveArcmediaPublicURL(
		"http://127.0.0.1:8080/api/v1/remote-generation/jobs/job-1/outputs/out-1.mp4")
	assert.Equal(t, "", got, "IP 主机不能瞎推域名")
}
