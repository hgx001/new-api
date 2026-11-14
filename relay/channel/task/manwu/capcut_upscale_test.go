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
