package service

// 「App 成功 ⟹ new-api 不退款、不失败」的轮询侧不变式。
//
// 老板的要求是一句大白话：「app 成功了 newapi 那边就不能退款、不能不能失败」。
// 这条链在 new-api 侧有三处会把它打断，全部集中在 updateVideoSingleTask：
//
//  1. **不校验 HTTP 状态码**。上游（ArcReel）部署重启期间回 502/503，响应体是
//     nginx 的 HTML 或一段不认识 JSON —— 解析不出 status 就落进
//     「unrecognized error format → FailTaskInfo」分支，任务当场判 FAILURE 并退款。
//     而官网那边片已经出了，下一轮轮询本来就能拿到 ready。
//
//  2. **任何 error 形态的响应都当任务失败**。429 已经特判（保持原状态等下一轮），
//     但 5xx / 网关错误 / 限流以外的临时故障没有同样的待遇。
//
//  3. **超时扫描不看上游**。sweepTimedOutTasks 到点直接判失败退款，即使任务其实
//     刚在上游完成（默认 1440 分钟，但 picsart 30s 真机单条 13–21 分钟，
//     长任务 + 队列积压时并非不可能撞上）。
//
// 这三处的共同修法只有一句话：**没有拿到上游的明确结论时，不要改变任务状态**。
// 保持原状态等下一轮轮询，远比误判失败 + 退款安全 —— 退款不可逆，而多等一轮
// 只是晚几分钟。

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// upstreamFailureAdaptor 回放一个「上游临时故障」的响应：
// 指定的 HTTP 状态码 + 指定的响应体，用于验证轮询不会把它当成任务失败。
type upstreamFailureAdaptor struct {
	statusCode int
	body       string
	// parseResult 非 nil 时用它替代默认的 ParseTaskResult（模拟能解析出结论的路径）。
	parseResult *relaycommon.TaskInfo
}

func (a *upstreamFailureAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *upstreamFailureAdaptor) FetchTask(_ string, _ string, _ map[string]any, _ string) (*http.Response, error) {
	return &http.Response{
		StatusCode: a.statusCode,
		Body:       io.NopCloser(bytes.NewReader([]byte(a.body))),
	}, nil
}

func (a *upstreamFailureAdaptor) ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error) {
	if a.parseResult != nil {
		return a.parseResult, nil
	}
	// 与 manwu 适配器同款：解析不出 status 时返回空 Status（不报错），
	// 让 updateVideoSingleTask 走「unrecognized」兜底分支。
	var res struct {
		Status string `json:"status"`
	}
	if err := common.Unmarshal(body, &res); err != nil {
		return nil, err
	}
	return &relaycommon.TaskInfo{Status: res.Status}, nil
}

func (a *upstreamFailureAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func seedQuotaTask(t *testing.T, channelID int, publicID, upstreamID string, quota int) *model.Task {
	t.Helper()
	task := &model.Task{
		TaskID:    publicID,
		Platform:  constant.TaskPlatform("kling"),
		UserId:    1,
		ChannelId: channelID,
		Action:    constant.TaskActionGenerate,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		Quota:     quota,
		CreatedAt: time.Now().Unix(),
		UpdatedAt: time.Now().Unix(),
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: upstreamID,
		},
	}
	require.NoError(t, model.DB.Create(task).Error)
	return task
}

func reloadTask(t *testing.T, id int64) *model.Task {
	t.Helper()
	var task model.Task
	require.NoError(t, model.DB.First(&task, id).Error)
	return &task
}

// pollOnce 驱动一次真实的单任务轮询（走 updateVideoSingleTask 全流程）。
func pollOnce(t *testing.T, channelID int, task *model.Task, adaptor TaskPollingAdaptor) {
	t.Helper()
	ch, err := model.CacheGetChannel(channelID)
	require.NoError(t, err)
	taskM := map[string]*model.Task{task.GetUpstreamTaskID(): task}
	_ = updateVideoSingleTask(context.Background(), adaptor, ch, task.GetUpstreamTaskID(), taskM)
}

// TestUpstream5xxDoesNotFailTask 是本组最核心的一条：
// 上游回 5xx 时任务**必须保持原状态**，绝不能判 FAILURE（判了就会退款）。
func TestUpstream5xxDoesNotFailTask(t *testing.T) {
	truncate(t)

	const channelID = 301
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_5xx", "upstream_5xx", 100)

	for _, code := range []int{
		http.StatusBadGateway,         // 502：ArcReel 部署重启期间的典型响应
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout,     // 504
		http.StatusInternalServerError, // 500
	} {
		adaptor := &upstreamFailureAdaptor{
			statusCode: code,
			body:       `<html><body>502 Bad Gateway</body></html>`,
		}
		pollOnce(t, channelID, task, adaptor)

		reloaded := reloadTask(t, task.ID)
		assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status,
			"上游 %d 是临时故障，任务必须保持进行中（判失败会触发退款）", code)
		assert.Empty(t, reloaded.FailReason, "不许写入失败原因")
	}
}

// TestUpstreamErrorBodyDoesNotFailTask：上游回了 200 但响应体是错误对象
// （ArcReel 的 {"detail": "..."} / {"error": {...}}），同样不能判失败。
func TestUpstreamErrorBodyDoesNotFailTask(t *testing.T) {
	truncate(t)

	const channelID = 302
	seedTaskPollingChannel(t, channelID, true)

	for _, body := range []string{
		`{"detail":"failed to get task"}`,
		`{"error":{"message":"upstream boom","code":"500"}}`,
		`{"message":"service unavailable"}`,
		`not a json at all`,
	} {
		task := seedQuotaTask(t, channelID, "task_err_"+body[:4], "upstream_err_"+body[:4], 100)
		adaptor := &upstreamFailureAdaptor{statusCode: http.StatusOK, body: body}
		pollOnce(t, channelID, task, adaptor)

		reloaded := reloadTask(t, task.ID)
		assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloaded.Status,
			"上游没给出明确结论（响应体 %q）时必须保持进行中", body)
	}
}

// TestUpstream429KeepsState 是既有行为，锁住别被后续改动破坏。
func TestUpstream429KeepsState(t *testing.T) {
	truncate(t)

	const channelID = 303
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_429", "upstream_429", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusTooManyRequests,
		body:       `{"error":{"message":"rate limited","code":"429"}}`,
	}
	pollOnce(t, channelID, task, adaptor)

	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), reloadTask(t, task.ID).Status)
}

// TestUpstreamExplicitFailureStillFails 是对照组：上游**明确**说失败时，
// 必须照常判 FAILURE 并退款 —— 别把守卫写成「永远不失败」。
func TestUpstreamExplicitFailureStillFails(t *testing.T) {
	truncate(t)

	const channelID = 304
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_explicit_fail", "upstream_explicit_fail", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode:  http.StatusOK,
		body:        `{"status":"failed","error":"官网生成失败"}`,
		parseResult: &relaycommon.TaskInfo{Status: model.TaskStatusFailure, Reason: "官网生成失败"},
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status, "上游明确失败必须照常收敛为失败")
	assert.NotEmpty(t, reloaded.FailReason)
}

// TestUpstreamSuccessStillSettles 是对照组：上游明确成功时照常收敛成功。
func TestUpstreamSuccessStillSettles(t *testing.T) {
	truncate(t)

	const channelID = 305
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_ok", "upstream_ok", 100)

	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusOK,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	pollOnce(t, channelID, task, adaptor)

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), reloaded.Status)
	assert.Equal(t, "100%", reloaded.Progress)
}

// TestSweepSkipsTaskThatJustSucceededUpstream：超时扫描不得杀掉一个上游已成功的任务。
//
// 场景：任务在 new-api 侧 submit_time 很早（超过 TASK_TIMEOUT_MINUTES），但上游
// 其实刚刚 ready —— 轮询还没轮到它，超时扫描先跑（RunTaskPollingOnce 里
// sweepTimedOutTasks 在主轮询之前）。此时判失败退款就是「App 成功了却被退款」。
func TestSweepSkipsTaskThatJustSucceededUpstream(t *testing.T) {
	truncate(t)

	// 缩短超时窗口到 1 分钟，构造一条「提交于 10 分钟前」的任务。
	prevTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = prevTimeout })

	const channelID = 306
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_timeout_ok", "upstream_timeout_ok", 100)
	// 把 submit_time 推到超时窗口之外
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Updates(map[string]any{"submit_time": time.Now().Unix() - 600}).Error)

	// 上游此时已经成功
	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusOK,
		body:       `{"status":"SUCCESS","url":"https://cdn.example.com/v.mp4"}`,
		parseResult: &relaycommon.TaskInfo{
			Status: model.TaskStatusSuccess,
			Url:    "https://cdn.example.com/v.mp4",
		},
	}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	sweepTimedOutTasks(context.Background())

	reloaded := reloadTask(t, task.ID)
	assert.NotEqual(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status,
		"上游已成功时，超时扫描不许把它判失败退款")
}

// TestSweepStillTimesOutDeadTask 对照组：上游确实没结论、任务真的卡死 → 照常判超时。
func TestSweepStillTimesOutDeadTask(t *testing.T) {
	truncate(t)

	prevTimeout := constant.TaskTimeoutMinutes
	constant.TaskTimeoutMinutes = 1
	t.Cleanup(func() { constant.TaskTimeoutMinutes = prevTimeout })

	const channelID = 307
	seedTaskPollingChannel(t, channelID, true)
	task := seedQuotaTask(t, channelID, "task_dead", "upstream_dead", 100)
	require.NoError(t, model.DB.Model(&model.Task{}).Where("id = ?", task.ID).
		Updates(map[string]any{"submit_time": time.Now().Unix() - 600}).Error)

	// 上游持续 5xx（拿不到结论）
	adaptor := &upstreamFailureAdaptor{
		statusCode: http.StatusBadGateway,
		body:       `<html>bad gateway</html>`,
	}
	previousFactory := GetTaskAdaptorFunc
	GetTaskAdaptorFunc = func(constant.TaskPlatform) TaskPollingAdaptor { return adaptor }
	t.Cleanup(func() { GetTaskAdaptorFunc = previousFactory })

	sweepTimedOutTasks(context.Background())

	reloaded := reloadTask(t, task.ID)
	assert.Equal(t, model.TaskStatus(model.TaskStatusFailure), reloaded.Status,
		"上游拿不到结论 + 已超时 → 照常判超时失败（否则任务永远挂着）")
	assert.Contains(t, reloaded.FailReason, "超时")
}
