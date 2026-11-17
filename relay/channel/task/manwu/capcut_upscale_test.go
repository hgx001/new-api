package manwu

// CapCut「交付前超分」钩子的测试（2026-11-17 起全站取消超分）。
//
// 要锁的核心不变量：**任何任务都不再触发超分**。
//
//	规则五（豆包降级 → 2.5 Flash 720p 原生）→ 不超分；
//	规则六/七/八/九（促销族 → CapCut 720p 原生）→ 不超分；
//	客户直接买 CapCut 档位 / 其它模型          → 不超分。
//
// 误判的后果是「白花钱多跑一次 MPS、还可能把清晰原片越转越糊」——不会报错。

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

// 只认「执行 CapCut + 计费原点按 720P 售卖」这一种组合。
func TestNeedsCapcutUpscaleMatrix(t *testing.T) {
	assert.False(t, needsCapcutUpscale(capcutAltTask()), "规则五降级任务已改 720p 原生，不再超分")

	direct := &model.Task{TaskID: "task_direct"}
	direct.Properties.OriginModelName = ModelCapcut25Flash
	assert.False(t, needsCapcutUpscale(direct),
		"客户直接买 2.5 Flash 时不该超分（买什么档交付什么档）")

	doubao := &model.Task{TaskID: "task_doubao"}
	doubao.Properties.OriginModelName = "seedance-2-5"
	doubao.PrivateData.RouteOriginModel = "seedance-2-5"
	assert.False(t, needsCapcutUpscale(doubao),
		"执行模型是豆包时不该超分（豆包本来就是 720P 交付）")

	// 促销族改派（规则七/八，2026-11-17 起按 720p 原生生成、直接交付）。
	ecom := &model.Task{TaskID: "task_ecom"}
	ecom.Properties.OriginModelName = ModelCapcut25Flash
	ecom.PrivateData.RouteOriginModel = "seedance2.5-特惠"
	assert.False(t, needsCapcutUpscale(ecom),
		"促销族改派 CapCut 已是 720p 原片，不得再超分")

	huodong := &model.Task{TaskID: "task_huodong"}
	huodong.Properties.OriginModelName = ModelCapcut25Flash
	huodong.PrivateData.RouteOriginModel = "seedance2.5-活动"
	assert.False(t, needsCapcutUpscale(huodong), "活动版按次档同样不再超分")

	// 历史别名：别名中间件只改 body.model，计费原点保留旧名。
	legacy := &model.Task{TaskID: "task_legacy"}
	legacy.Properties.OriginModelName = ModelCapcut25Flash
	legacy.PrivateData.RouteOriginModel = "wan3.0-720p-特惠"
	assert.False(t, needsCapcutUpscale(legacy),
		"wan3.0-720p-特惠 是促销族历史别名，同样已是 720p 原片")

	// 规则六短时长分支的目标（2.0 Fast）同样改为 720p 原生。
	fast := &model.Task{TaskID: "task_fast"}
	fast.Properties.OriginModelName = ModelCapcut20Fast
	fast.PrivateData.RouteOriginModel = "seedance2.5-特惠"
	assert.False(t, needsCapcutUpscale(fast), "CapCut 快速档改派也不再超分")

	// 直接买 CapCut 其它档位：不受影响。
	directOther := &model.Task{TaskID: "task_direct_other"}
	directOther.Properties.OriginModelName = ModelCapcut25
	assert.False(t, needsCapcutUpscale(directOther), "直接购买的档位不超分")

	assert.False(t, needsCapcutUpscale(nil), "nil 任务必须安全")
}

// 钩子注入后：匹配的任务被委托给超分实现（handled=true 让任务保持处理中）。
func TestAfterUpstreamSuccessDelegatesWhenHookInjected(t *testing.T) {
	var submitted bool
	SetSuperResolutionHooks(
		func(_ context.Context, task *model.Task, upstreamURL string) (bool, error) {
			submitted = true
			return true, nil
		},
		func(context.Context, *model.Task) (bool, error) { return true, nil },
	)
	t.Cleanup(func() { SetSuperResolutionHooks(nil, nil) })

	adaptor := &TaskAdaptor{}
	handled, err := adaptor.AfterUpstreamSuccess(
		context.Background(), capcutAltTask(), "https://cdn.example.com/raw.mp4")
	require.NoError(t, err)
	assert.False(t, handled, "全站取消超分后不再接管任何新任务（720p 原片直接交付）")
	assert.False(t, submitted, "不得提交 MPS 超分")
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

// 轮询：发版前已提交 MPS 的在途任务（带 PostProcess 状态）必须继续推进到终态，
// 不能因为 needsCapcutUpscale 恒 false 就把 480p 原片当 720P 成品交付。
func TestPollPostProcessContinuesInFlightTasks(t *testing.T) {
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
	assert.Equal(t, 1, polled, "在途的 MPS 任务必须推进到终态（收敛成 720P 产物）")

	// 发版前任何带着 PostProcess 状态的任务都算「在途」，都要收敛，
	// 不再按 origin 过滤（否则会拿 480p 原片当 720P 成品交付）。
	other := &model.Task{TaskID: "task_direct"}
	other.Properties.OriginModelName = ModelCapcut25Flash
	other.PrivateData.PostProcess = &model.TaskPostProcess{Stage: "submitted", TaskID: "mps-2"}
	done, err = adaptor.PollPostProcess(context.Background(), other)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, 2, polled, "所有在途的 MPS 任务都要收敛，不按 origin 过滤")
}

// ───────────────────────── 成片地址解析（2026-11-14 实测修复）─────────

// ArcReel 走自有托管时 ParseTaskResult 不吐地址。
// 超分取消后这些解析函数生产上不再被调用，但保留可恢复性 —— 直接测辅助函数本身。
func TestCapcutSourceURLFromTask(t *testing.T) {
	task := capcutAltTask()
	task.Data = json.RawMessage(`{"status":"ready","sourceUrl":"https://cdn.example.com/out.mp4"}`)
	assert.Equal(t, "https://cdn.example.com/out.mp4", capcutSourceURLFromTask(task),
		"必须能从任务原始响应里取回 sourceUrl")
}

// ArcReel 的产物接口需要渠道密钥（无鉴权 401），但它会 302 到公开 CDN ——
// 这是 resolveCapcutMediaURL 的职责（超分恢复时需要）。
func TestResolveCapcutMediaURLFollowsRedirectWithAuth(t *testing.T) {
	var seenAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		w.Header().Set("Location", "https://arcmedia.example.com/public/out.mp4")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{apiKey: "sk-test"}
	got := adaptor.resolveCapcutMediaURL(context.Background(),
		server.URL+"/api/v1/remote-generation/jobs/j1/outputs/o1.mp4")
	assert.Equal(t, "Bearer sk-test", seenAuth, "换公开地址必须带上渠道密钥")
	assert.Equal(t, "https://arcmedia.example.com/public/out.mp4", got,
		"302 之后必须给出公开地址")
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
