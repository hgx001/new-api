package service

// 「上游失败复核」的轮询侧行为锁定（2026-11-16）。
//
// 背景（有赞生产实锤）：上游余额耗尽时把任务标成 failed 并非真终态 ——
// 充值后会把**同一个任务**复活继续执行并计费（当时 HTTP 402 被挂起的请求
// 在上游充值后被执行计费）。我方轮询若立即退款 → 上游扣成本、我方没收
// 客户钱 → 纯亏损。
//
// 修复：实现 TaskFailureRechecker 的渠道，首次 failed 挂起复核窗口；
// 窗口内复活 → 正常收敛成功并结算；窗口过后仍是 failed → 才收敛失败退款。
// 没实现该接口的渠道维持「failed 即退款」原语义（对照组见
// TestUpstreamExplicitFailureStillFails）。

import (
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recheckAdaptor 在 upstreamFailureAdaptor 基础上实现失败复核接口，
// 用来模拟「有赞这类会把 failed 复活的上游」。
type recheckAdaptor struct {
	upstreamFailureAdaptor
	delay time.Duration
}

func (a *recheckAdaptor) FailureRecheckDelay() time.Duration { return a.delay }

func upstreamFailedResult(reason string) *relaycommon.TaskInfo {
	return &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: reason}
}

// TestFailureRecheckHoldsWithinWindow：首次 failed 必须挂起，不得立即退款。
func TestFailureRecheckHoldsWithinWindow(t *testing.T) {
	truncate(t)

	const channelID = 307
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_recheck_hold", "upstream_recheck_hold", 100)

	adaptor := &recheckAdaptor{
		upstreamFailureAdaptor: upstreamFailureAdaptor{
			statusCode:  http.StatusOK,
			parseResult: upstreamFailedResult("未知错误请重试"),
		},
		delay: 10 * time.Minute,
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status,
		"复核窗口内不得收敛失败（立即退款会在上游复活时形成纯亏损）")
	assert.Empty(t, reloaded.FailReason, "挂起期间不得写失败原因")
	assert.True(t, reloaded.PrivateData.FirstFailedAt > 0,
		"首次失败时间必须落库：Snapshot 不含 PrivateData，丢了时间戳任务会无限挂起")
	assert.EqualValues(t, 100, reloaded.Quota, "挂起期间不得退款")
}

// TestFailureRecheckSettlesAfterWindow：窗口过后仍是 failed，必须照常收敛失败退款。
func TestFailureRecheckSettlesAfterWindow(t *testing.T) {
	truncate(t)

	const channelID = 308
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_recheck_settle", "upstream_recheck_settle", 100)

	adaptor := &recheckAdaptor{
		upstreamFailureAdaptor: upstreamFailureAdaptor{
			statusCode:  http.StatusOK,
			parseResult: upstreamFailedResult("未知错误请重试"),
		},
		delay: 10 * time.Minute,
	}
	// 第一轮：挂起 + 落库首次失败时间。
	pollOnce(t, channelID, task, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloadTask(t, task.ID).Status)

	// 把首次失败时间拨回窗口之前，模拟「复核窗口已过」。
	task.PrivateData.FirstFailedAt = time.Now().Add(-11 * time.Minute).Unix()
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status,
		"窗口过后上游仍是 failed，必须收敛为失败并退款，不能无限挂起")
	assert.NotEmpty(t, reloaded.FailReason)
}

// TestFailureRecheckResurrectedSuccess：窗口内上游复活，必须正常收敛成功并结算。
//
// 这是本次加固要保住的商业场景：上游充值后把 failed 任务复活跑完 ——
// 客户已付费，任务必须交付，而不是退款了事。
func TestFailureRecheckResurrectedSuccess(t *testing.T) {
	truncate(t)

	const channelID = 309
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_recheck_revive", "upstream_recheck_revive", 100)

	adaptor := &recheckAdaptor{
		upstreamFailureAdaptor: upstreamFailureAdaptor{
			statusCode: http.StatusOK,
		},
		delay: 10 * time.Minute,
	}
	adaptor.parseResult = upstreamFailedResult("未知错误请重试")
	pollOnce(t, channelID, task, adaptor)
	require.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloadTask(t, task.ID).Status,
		"前置：首轮 failed 必须挂起")

	// 上游充值后复活：同一个任务返回成功。
	adaptor.parseResult = &relaycommon.TaskInfo{
		Status: model.TaskStatusSuccess,
		Url:    "https://cdn.example.com/revived.mp4",
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status,
		"窗口内上游复活必须正常交付，绝不能因为此前报过 failed 而拒绝交付")
	assert.Equal(t, "100%", reloaded.Progress)
}

// TestFailureRecheckZeroDelayFailsImmediately：delay=0 等价于不参与复核，
// 维持「failed 即退款」原语义。
func TestFailureRecheckZeroDelayFailsImmediately(t *testing.T) {
	truncate(t)

	const channelID = 310
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_recheck_zero", "upstream_recheck_zero", 100)

	adaptor := &recheckAdaptor{
		upstreamFailureAdaptor: upstreamFailureAdaptor{
			statusCode:  http.StatusOK,
			parseResult: upstreamFailedResult("官网生成失败"),
		},
		delay: 0,
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status)
	assert.NotEmpty(t, reloaded.FailReason)
}
