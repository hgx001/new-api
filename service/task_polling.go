package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/bytedance/gopkg/util/gopool"
	"github.com/samber/lo"
)

// TaskPollingAdaptor 定义轮询所需的最小适配器接口，避免 service -> relay 的循环依赖
type TaskPollingAdaptor interface {
	Init(info *relaycommon.RelayInfo)
	FetchTask(baseURL string, key string, body map[string]any, proxy string) (*http.Response, error)
	ParseTaskResult(body []byte) (*relaycommon.TaskInfo, error)
	// AdjustBillingOnComplete 在任务到达终态（成功/失败）时由轮询循环调用。
	// 返回正数触发差额结算（补扣/退还），返回 0 保持预扣费金额不变。
	AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int
}

// TaskPostProcessor 是**可选**的成片后处理接口（2026-10-29）。
//
// 用途：部分模型是**两跳**链路 —— 上游先出一版成片，再交给第二个服务加工
// （例如把 480P 提升到 720P），用户最终拿到的是第二跳的产物。
//
// 为什么用可选接口而不是加进 TaskAdaptor：
//
//	加进 TaskAdaptor 会让 19 个 adaptor 全部被迫实现（哪怕是空实现），
//	属于「为了一个功能改所有人的接口」。这里用类型断言：实现了就调，
//	没实现就走原路径，**现有 adaptor 零改动**。
//
// 语义约定：
//
//	AfterUpstreamSuccess —— 上游第一跳成功、**在置 SUCCESS 之前**调用。
//	  返回 (handled=true, err=nil) 表示「已接管，本次不置 SUCCESS，产物还没好」；
//	  返回 (handled=false, err=nil) 表示「不接管，按原流程置 SUCCESS」；
//	  返回 err 表示接管了但出错 —— 调用方**不判任务失败**，保持处理中，下轮再试。
//
//	PollPostProcess —— 第二跳进行中时每轮调用。
//	  返回 (done=true) 表示第二跳已到终态（成功或已回落原片），可以置 SUCCESS；
//	  返回 (done=false) 表示还在处理中，保持任务为处理中；
//	  返回 err 表示本轮查询失败，保持现状、下轮再试。
type TaskPostProcessor interface {
	// AfterUpstreamSuccess 在上游返回成功后、任务置 SUCCESS 之前调用。
	AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error)
	// PollPostProcess 在任务已提交后处理、尚未完成时每轮调用。
	PollPostProcess(ctx context.Context, task *model.Task) (bool, error)
}

// postProcessorFor 返回任务对应的后处理器（没有则返回 nil）。
//
// 用类型断言探测，**不要求** adaptor 实现 TaskAdaptor 的所有方法 ——
// 只要它额外实现了 TaskPostProcessor 就会被识别。
func postProcessorFor(adaptor TaskPollingAdaptor) TaskPostProcessor {
	if pp, ok := adaptor.(TaskPostProcessor); ok {
		return pp
	}
	return nil
}

// GetTaskAdaptorFunc 由 main 包注入，用于获取指定平台的任务适配器。
// 打破 service -> relay -> relay/channel -> service 的循环依赖。
var GetTaskAdaptorFunc func(platform constant.TaskPlatform) TaskPollingAdaptor

// 超时扫描判死前的上游探测结论。
type timeoutProbeOutcome int

const (
	// 上游没给出可用结论（请求失败、5xx、状态无法识别）→ 按原超时语义处理。
	timeoutProbeUnknown timeoutProbeOutcome = iota
	// 上游明确成功 → 不许判超时，交给正常轮询收敛并结算。
	timeoutProbeSucceeded
)

// probeUpstreamBeforeTimeout 在把任务判超时失败（并退款）之前，问一次上游的真实状态。
//
// 为什么必须有这一步：sweepTimedOutTasks 在主轮询**之前**执行，判死时完全不看上游。
// 「上游刚 ready、这轮轮询还没轮到它」的任务会被超时扫描抢先杀掉 → 客户拿了片、
// 平台退了钱，正好违反「App 成功了 newapi 就不能退款」。
//
// 只回答一个问题：**上游是否已经明确成功**。其余一律返回 unknown，让调用方维持
// 原有的超时语义 —— 否则一条真的卡死的任务会永远挂着，前端一直转圈。
//
// 尽力而为：任何异常都当 unknown，绝不让探测本身阻断回收。
func probeUpstreamBeforeTimeout(ctx context.Context, task *model.Task) (timeoutProbeOutcome, bool) {
	if GetTaskAdaptorFunc == nil || task == nil {
		return timeoutProbeUnknown, false
	}
	upstreamID := task.GetUpstreamTaskID()
	if upstreamID == "" {
		return timeoutProbeUnknown, false
	}
	ch, err := model.CacheGetChannel(task.ChannelId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("timeout probe: get channel %d failed: %s", task.ChannelId, err.Error()))
		return timeoutProbeUnknown, false
	}
	adaptor := GetTaskAdaptorFunc(task.Platform)
	if adaptor == nil {
		return timeoutProbeUnknown, false
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: ch.GetBaseURL(),
		ChannelType:    ch.Type,
	}
	// 与 updateVideoTasks 同款：轮询路径的 info 是空壳，必须初始化内嵌
	// TaskRelayInfo，否则适配器 Init 访问提升字段会 nil panic。
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	info.ApiKey = ch.Key
	adaptor.Init(info)

	key := ch.Key
	if task.PrivateData.Key != "" {
		key = task.PrivateData.Key
	}
	resp, err := adaptor.FetchTask(ch.GetBaseURL(), key, map[string]any{
		"task_id": upstreamID,
		"action":  task.Action,
	}, ch.GetSetting().Proxy)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("timeout probe: fetch task %s failed: %s", task.TaskID, err.Error()))
		return timeoutProbeUnknown, false
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return timeoutProbeUnknown, false
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return timeoutProbeUnknown, false
	}
	taskResult, err := adaptor.ParseTaskResult(body)
	if err != nil || taskResult == nil {
		return timeoutProbeUnknown, false
	}
	if taskResult.Status == model.TaskStatusSuccess {
		return timeoutProbeSucceeded, true
	}
	return timeoutProbeUnknown, true
}

// sweepTimedOutTasks 在主轮询之前独立清理超时任务。
// 每次最多处理 100 条，剩余的下个周期继续处理。
// 使用 per-task CAS (UpdateWithStatus) 防止覆盖被正常轮询已推进的任务。
func sweepTimedOutTasks(ctx context.Context) {
	if constant.TaskTimeoutMinutes <= 0 {
		return
	}
	cutoff := time.Now().Unix() - int64(constant.TaskTimeoutMinutes)*60
	tasks := model.GetTimedOutUnfinishedTasks(cutoff, 100)
	if len(tasks) == 0 {
		return
	}

	const legacyTaskCutoff int64 = 1740182400 // 2026-02-22 00:00:00 UTC
	reason := fmt.Sprintf("任务超时（%d分钟）", constant.TaskTimeoutMinutes)
	legacyReason := "任务超时（旧系统遗留任务，不进行退款，请联系管理员）"
	now := time.Now().Unix()
	timedOutCount := 0

	for _, task := range tasks {
		isLegacy := task.SubmitTime > 0 && task.SubmitTime < legacyTaskCutoff

		// 【判死前先问一次上游】（2026-10-24）
		//
		// 老板的要求：「app 成功了 newapi 那边就不能退款、不能失败」。
		// 本函数在主轮询**之前**跑，且到点直接判失败 + 退款，完全不看上游 ——
		// 于是「上游刚 ready、这轮轮询还没轮到它」的任务会被超时扫描抢先杀掉，
		// 客户拿了片、平台退了钱，账目反着走。
		//
		// 这里补一次上游确认：上游明确成功 → 按成功收敛（结算，不退款）；
		// 其余情况（失败/进行中/问不到）→ 保持原有超时语义，避免任务永远挂着。
		// 只对「有预扣、非遗留」的任务做这次额外请求：没预扣的任务退款本来就是 no-op，
		// 不值得为它多打一次上游。
		if !isLegacy && task.Quota != 0 {
			if outcome, ok := probeUpstreamBeforeTimeout(ctx, task); ok {
				if outcome == timeoutProbeSucceeded {
					logger.LogWarn(ctx, fmt.Sprintf(
						"sweepTimedOutTasks: task %s already succeeded upstream, settle instead of refund",
						task.TaskID,
					))
					continue // 交给随后的正常轮询收敛到 SUCCESS 并结算
				}
			}
		}

		oldStatus := task.Status
		task.Status = model.TaskStatusFailure
		task.Progress = "100%"
		task.FinishTime = now
		if isLegacy {
			task.FailReason = legacyReason
		} else {
			task.FailReason = reason
		}

		won, err := task.UpdateWithStatus(oldStatus)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("sweepTimedOutTasks CAS update error for task %s: %v", task.TaskID, err))
			continue
		}
		if !won {
			logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: task %s already transitioned, skip", task.TaskID))
			continue
		}
		timedOutCount++
		if !isLegacy && task.Quota != 0 {
			RefundTaskQuota(ctx, task, reason)
		}
	}

	if timedOutCount > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("sweepTimedOutTasks: timed out %d tasks", timedOutCount))
	}
}

// TaskPollSummary is the result recorded on an async_task_poll system task row,
// summarizing one polling pass.
type TaskPollSummary struct {
	UnfinishedTasks  int `json:"unfinished_tasks"`
	PlatformsScanned int `json:"platforms_scanned"`
	NullTasksFailed  int `json:"null_tasks_failed"`
}

// lastTaskLogPruneAt 记录上次清理任务流水的时间（秒）。
// 用 atomic 而不是 sync.Once：进程常驻多天，要按天重复清，不是只清一次。
var lastTaskLogPruneAt atomic.Int64

// pruneTaskLogsIfDue 每天最多清一次过期任务流水，避免 task_logs 无限增长 ——
// 这张表随任务量线性增长，不设上限等于给自己埋一次磁盘写满。
// 挂在轮询入口：轮询本来就按周期跑，不额外起 goroutine。
func pruneTaskLogsIfDue(ctx context.Context) {
	now := time.Now().Unix()
	last := lastTaskLogPruneAt.Load()
	if last != 0 && now-last < 24*3600 {
		return
	}
	if !lastTaskLogPruneAt.CompareAndSwap(last, now) {
		// 另一个实例刚抢到，跳过。
		return
	}
	before := now - int64(model.TaskLogRetentionDays)*24*3600
	deleted, err := model.DeleteTaskLogsBefore(before, 1000)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("prune task logs failed: %s", err.Error()))
		return
	}
	if deleted > 0 {
		logger.LogInfo(ctx, fmt.Sprintf("prune task logs: deleted %d rows older than %d days",
			deleted, model.TaskLogRetentionDays))
	}
}

// RunTaskPollingOnce performs one async-task (Suno/video) polling pass
// synchronously. It honors ctx cancellation (the system-task runner cancels it
// when the lease is lost) and, when report is non-nil, reports progress as
// (processedPlatforms, totalPlatforms). It returns immediately if the task
// adaptor factory has not been wired yet, to avoid a nil call during startup.
func RunTaskPollingOnce(ctx context.Context, report func(processed, total int)) TaskPollSummary {
	summary := TaskPollSummary{}
	if GetTaskAdaptorFunc == nil {
		return summary
	}
	if ctx == nil {
		ctx = context.Background()
	}

	common.SysLog("任务进度轮询开始")
	pruneTaskLogsIfDue(ctx)
	sweepTimedOutTasks(ctx)
	allTasks := model.GetAllUnFinishSyncTasks(constant.TaskQueryLimit)
	summary.UnfinishedTasks = len(allTasks)
	platformTask := make(map[constant.TaskPlatform][]*model.Task)
	for _, t := range allTasks {
		platformTask[t.Platform] = append(platformTask[t.Platform], t)
	}

	totalPlatforms := len(platformTask)
	processedPlatforms := 0
	for platform, tasks := range platformTask {
		if ctx.Err() != nil {
			break
		}
		if report != nil {
			report(processedPlatforms, totalPlatforms)
		}
		processedPlatforms++
		if len(tasks) == 0 {
			continue
		}
		summary.PlatformsScanned++
		taskChannelM := make(map[int][]string)
		taskM := make(map[string]*model.Task)
		nullTaskIds := make([]int64, 0)
		for _, task := range tasks {
			upstreamID := task.GetUpstreamTaskID()
			if upstreamID == "" {
				// 统计失败的未完成任务
				nullTaskIds = append(nullTaskIds, task.ID)
				continue
			}
			taskM[upstreamID] = task
			taskChannelM[task.ChannelId] = append(taskChannelM[task.ChannelId], upstreamID)
		}
		if len(nullTaskIds) > 0 {
			summary.NullTasksFailed += len(nullTaskIds)
			err := model.TaskBulkUpdateByID(nullTaskIds, map[string]any{
				"status":   "FAILURE",
				"progress": "100%",
			})
			if err != nil {
				logger.LogError(ctx, fmt.Sprintf("Fix null task_id task error: %v", err))
			} else {
				logger.LogInfo(ctx, fmt.Sprintf("Fix null task_id task success: %v", nullTaskIds))
			}
		}
		if len(taskChannelM) == 0 {
			continue
		}

		DispatchPlatformUpdate(ctx, platform, taskChannelM, taskM)
	}
	if report != nil && ctx.Err() == nil {
		report(totalPlatforms, totalPlatforms)
	}
	common.SysLog("任务进度轮询完成")
	return summary
}

// DispatchPlatformUpdate 按平台分发轮询更新
func DispatchPlatformUpdate(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) {
	if ctx == nil {
		ctx = context.Background()
	}
	switch platform {
	case constant.TaskPlatformMidjourney:
		// MJ 轮询由其自身处理，这里预留入口
	case constant.TaskPlatformSuno:
		_ = UpdateSunoTasks(ctx, taskChannelM, taskM)
	default:
		if err := UpdateVideoTasks(ctx, platform, taskChannelM, taskM); err != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTasks fail: %s", err))
		}
	}
}

// UpdateSunoTasks 按渠道更新所有 Suno 任务
func UpdateSunoTasks(ctx context.Context, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	for channelId, taskIds := range taskChannelM {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := updateSunoTasks(ctx, channelId, taskIds, taskM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("渠道 #%d 更新异步任务失败: %s", channelId, err.Error()))
		}
	}
	return nil
}

func updateSunoTasks(ctx context.Context, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("渠道 #%d 未完成的任务有: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	ch, err := model.CacheGetChannel(channelId)
	if err != nil {
		common.SysLog(fmt.Sprintf("CacheGetChannel: %v", err))
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		err = model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("获取渠道信息失败，请联系管理员，渠道ID：%d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if err != nil {
			common.SysLog(fmt.Sprintf("UpdateSunoTask error: %v", err))
		}
		return err
	}
	adaptor := GetTaskAdaptorFunc(constant.TaskPlatformSuno)
	if adaptor == nil {
		return errors.New("adaptor not found")
	}
	proxy := ch.GetSetting().Proxy
	resp, err := adaptor.FetchTask(*ch.BaseURL, ch.Key, map[string]any{
		"ids": taskIds,
	}, proxy)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Task Do req error: %v", err))
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.LogError(ctx, fmt.Sprintf("Get Task status code: %d", resp.StatusCode))
		return fmt.Errorf("Get Task status code: %d", resp.StatusCode)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		common.SysLog(fmt.Sprintf("Get Suno Task parse body error: %v", err))
		return err
	}
	var responseItems dto.TaskResponse[[]dto.SunoDataResponse]
	err = common.Unmarshal(responseBody, &responseItems)
	if err != nil {
		logger.LogError(ctx, fmt.Sprintf("Get Suno Task parse body error2: %v, body: %s", err, string(responseBody)))
		return err
	}
	if !responseItems.IsSuccess() {
		common.SysLog(fmt.Sprintf("渠道 #%d 未完成的任务有: %d, 成功获取到任务数: %s", channelId, len(taskIds), string(responseBody)))
		return err
	}

	for _, responseItem := range responseItems.Data {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		task := taskM[responseItem.TaskID]
		if task == nil {
			logger.LogWarn(ctx, fmt.Sprintf("Suno task response ignored: unknown task_id=%s", responseItem.TaskID))
			continue
		}
		if !taskNeedsUpdate(task, responseItem) {
			continue
		}

		task.Status = lo.If(model.TaskStatus(responseItem.Status) != "", model.TaskStatus(responseItem.Status)).Else(task.Status)
		task.FailReason = lo.If(responseItem.FailReason != "", responseItem.FailReason).Else(task.FailReason)
		task.SubmitTime = lo.If(responseItem.SubmitTime != 0, responseItem.SubmitTime).Else(task.SubmitTime)
		task.StartTime = lo.If(responseItem.StartTime != 0, responseItem.StartTime).Else(task.StartTime)
		task.FinishTime = lo.If(responseItem.FinishTime != 0, responseItem.FinishTime).Else(task.FinishTime)
		if responseItem.FailReason != "" || task.Status == model.TaskStatusFailure {
			logger.LogInfo(ctx, task.TaskID+" 构建失败，"+task.FailReason)
			task.Progress = "100%"
			RefundTaskQuota(ctx, task, task.FailReason)
		}
		if responseItem.Status == model.TaskStatusSuccess {
			task.Progress = "100%"
		}
		task.Data = responseItem.Data

		err = task.Update()
		if err != nil {
			common.SysLog("UpdateSunoTask task error: " + err.Error())
		}
	}
	return nil
}

// taskNeedsUpdate 检查 Suno 任务是否需要更新
func taskNeedsUpdate(oldTask *model.Task, newTask dto.SunoDataResponse) bool {
	if oldTask.SubmitTime != newTask.SubmitTime {
		return true
	}
	if oldTask.StartTime != newTask.StartTime {
		return true
	}
	if oldTask.FinishTime != newTask.FinishTime {
		return true
	}
	if string(oldTask.Status) != newTask.Status {
		return true
	}
	if oldTask.FailReason != newTask.FailReason {
		return true
	}

	if (oldTask.Status == model.TaskStatusFailure || oldTask.Status == model.TaskStatusSuccess) && oldTask.Progress != "100%" {
		return true
	}

	oldData, _ := common.Marshal(oldTask.Data)
	newData, _ := common.Marshal(newTask.Data)

	sort.Slice(oldData, func(i, j int) bool {
		return oldData[i] < oldData[j]
	})
	sort.Slice(newData, func(i, j int) bool {
		return newData[i] < newData[j]
	})

	if string(oldData) != string(newData) {
		return true
	}
	return false
}

// UpdateVideoTasks 按渠道更新所有视频任务
func UpdateVideoTasks(ctx context.Context, platform constant.TaskPlatform, taskChannelM map[int][]string, taskM map[string]*model.Task) error {
	channelIDs := make([]int, 0, len(taskChannelM))
	for channelID := range taskChannelM {
		channelIDs = append(channelIDs, channelID)
	}
	sort.Ints(channelIDs)

	var wg sync.WaitGroup
	for _, channelId := range channelIDs {
		taskIds := taskChannelM[channelId]
		if len(taskIds) == 0 {
			continue
		}
		taskIds = append([]string(nil), taskIds...)

		wg.Add(1)
		gopool.Go(func() {
			defer wg.Done()
			if err := updateVideoTasks(ctx, platform, channelId, taskIds, taskM); err != nil {
				logger.LogError(ctx, fmt.Sprintf("Channel #%d failed to update video async tasks: %s", channelId, err.Error()))
			}
		})
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}

func updateVideoTasks(ctx context.Context, platform constant.TaskPlatform, channelId int, taskIds []string, taskM map[string]*model.Task) error {
	logger.LogInfo(ctx, fmt.Sprintf("Channel #%d pending video tasks: %d", channelId, len(taskIds)))
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if len(taskIds) == 0 {
		return nil
	}
	cacheGetChannel, err := model.CacheGetChannel(channelId)
	if err != nil {
		// Collect DB primary key IDs for bulk update (taskIds are upstream IDs, not task_id column values)
		var failedIDs []int64
		for _, upstreamID := range taskIds {
			if t, ok := taskM[upstreamID]; ok {
				failedIDs = append(failedIDs, t.ID)
			}
		}
		errUpdate := model.TaskBulkUpdateByID(failedIDs, map[string]any{
			"fail_reason": fmt.Sprintf("Failed to get channel info, channel ID: %d", channelId),
			"status":      "FAILURE",
			"progress":    "100%",
		})
		if errUpdate != nil {
			common.SysLog(fmt.Sprintf("UpdateVideoTask error: %v", errUpdate))
		}
		return fmt.Errorf("CacheGetChannel failed: %w", err)
	}
	adaptor := GetTaskAdaptorFunc(platform)
	if adaptor == nil {
		return fmt.Errorf("video adaptor not found")
	}
	info := &relaycommon.RelayInfo{}
	info.ChannelMeta = &relaycommon.ChannelMeta{
		ChannelBaseUrl: cacheGetChannel.GetBaseURL(),
		ChannelType:    cacheGetChannel.Type,
	}
	// 轮询路径的 info 是空壳：必须初始化内嵌 TaskRelayInfo，
	// 否则适配器 Init 访问 info.PublicTaskID 等提升字段时会 nil panic
	// （曾导致 wan3 适配器每轮轮询崩溃，见 adaptor.go Init）。
	info.TaskRelayInfo = &relaycommon.TaskRelayInfo{}
	info.ApiKey = cacheGetChannel.Key
	adaptor.Init(info)
	disablePollingSleep := cacheGetChannel.GetOtherSettings().DisableTaskPollingSleep
	for i, taskId := range taskIds {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := updateVideoSingleTask(ctx, adaptor, cacheGetChannel, taskId, taskM); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update video task %s: %s", taskId, err.Error()))
		}
		if disablePollingSleep || i == len(taskIds)-1 {
			continue
		}

		// sleep 1 second between tasks for this channel only.
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
	return nil
}

func updateVideoSingleTask(ctx context.Context, adaptor TaskPollingAdaptor, ch *model.Channel, taskId string, taskM map[string]*model.Task) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	baseURL := ch.GetBaseURL()
	proxy := ch.GetSetting().Proxy

	task := taskM[taskId]
	if task == nil {
		logger.LogError(ctx, fmt.Sprintf("Task %s not found in taskM", taskId))
		return fmt.Errorf("task %s not found", taskId)
	}
	key := ch.Key

	privateData := task.PrivateData
	if privateData.Key != "" {
		key = privateData.Key
	}
	resp, err := adaptor.FetchTask(baseURL, key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil {
		return fmt.Errorf("fetchTask failed for task %s: %w", taskId, err)
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("readAll failed for task %s: %w", taskId, err)
	}

	logger.LogDebug(ctx, "updateVideoSingleTask response: %s", responseBody)

	// 【上游临时故障 ≠ 任务失败】（2026-10-24）
	//
	// 老板的要求：「app 成功了 newapi 那边就不能退款、不能失败」。这条在轮询侧
	// 最容易被打破的地方就在这里 —— 旧实现**完全不看 HTTP 状态码**：
	//
	//   · ArcReel 部署重启期间回 502/503/504，响应体是 nginx 的 HTML 或一段
	//     不认识的 JSON；
	//   · 解析不出 status → 落进下面的「unrecognized error format」分支 →
	//     FailTaskInfo → 任务当场判 FAILURE 并退款。
	//
	// 而官网那边片其实已经出了，下一轮轮询本来就能拿到 ready。
	// 退款不可逆，多等一轮只是晚几分钟 —— 所以这里**保持原状态直接返回**，
	// 让下一轮轮询重新问一次上游。
	//
	// 5xx / 408 / 429 都是典型的「这次没问到，不代表任务失败」。
	if resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests {
		logger.LogWarn(ctx, fmt.Sprintf(
			"Task %s upstream returned %d, keep status and retry next round: %s",
			taskId, resp.StatusCode, truncateBase64(string(responseBody)),
		))
		return nil
	}

	snap := task.Snapshot()

	taskResult := &relaycommon.TaskInfo{}
	// try parse as New API response format
	var responseItems dto.TaskResponse[model.Task]
	if err = common.Unmarshal(responseBody, &responseItems); err == nil && responseItems.IsSuccess() {
		logger.LogDebug(ctx, "updateVideoSingleTask parsed as new api response format: %+v", responseItems)
		t := responseItems.Data
		taskResult.TaskID = t.TaskID
		taskResult.Status = string(t.Status)
		taskResult.Url = t.GetResultURL()
		taskResult.ResultText = t.PrivateData.ResultText
		taskResult.Progress = t.Progress
		taskResult.Reason = t.FailReason
		task.Data = t.Data
	} else if taskResult, err = adaptor.ParseTaskResult(responseBody); err != nil {
		return fmt.Errorf("parseTaskResult failed for task %s: %w", taskId, err)
	}

	task.Data = redactVideoResponseBody(responseBody)

	logger.LogDebug(ctx, "updateVideoSingleTask taskResult: %+v", taskResult)

	now := time.Now().Unix()
	if taskResult.Status == "" {
		// 上游回了 200 但**没有给出任务状态**。这种响应有几种可能：
		//   · ArcReel 的 {"detail": "..."}（FastAPI 错误）；
		//   · 网关/反代插进来的 {"error": {...}} / {"message": "..."}；
		//   · 完全不是 JSON（HTML 错误页）。
		//
		// 无论哪种，都**不构成「任务失败」的结论**。旧实现把它们一律
		// FailTaskInfo → 判 FAILURE + 退款（429 除外），而官网那边片可能
		// 已经出了 —— 这正是「App 成功了却被退款」的形态。
		//
		// 现在统一保持原状态、等下一轮轮询重新问一次。多等一轮只晚几分钟，
		// 退款却是不可逆的。
		//
		// ⚠️ 唯一的例外是上游**明确**表达了失败语义（`{"status":"failed"}`）——
		// 那会走到下面 switch 的 TaskStatusFailure 分支，照常收敛 + 退款。
		logger.LogWarn(ctx, fmt.Sprintf(
			"Task %s upstream returned empty status, keep status and retry next round: %s",
			taskId, truncateBase64(string(responseBody)),
		))
		return nil
	}

	shouldRefund := false
	shouldSettle := false
	quota := task.Quota
	// postProcessPending 表示「第一跳成功但成片还在后处理中」。
	// 置 true 时不能置 SUCCESS、不结算，任务保持处理中。
	postProcessPending := false

	task.Status = model.TaskStatus(taskResult.Status)
	switch taskResult.Status {
	case model.TaskStatusSubmitted:
		task.Progress = taskcommon.ProgressSubmitted
	case model.TaskStatusQueued:
		task.Progress = taskcommon.ProgressQueued
	case model.TaskStatusInProgress:
		task.Progress = taskcommon.ProgressInProgress
		if task.StartTime == 0 {
			task.StartTime = now
		}
	case model.TaskStatusSuccess:
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		// ── 成片后处理（两跳链路）──────────────────────────────────────
		//
		// 上游第一跳成功了，但产物可能还要交给第二个服务加工（如 480P → 720P）。
		// 这里在置 SUCCESS **之前**接管：接管了就保持「处理中」，
		// 等第二跳完成后再由 PollPostProcess 收敛。
		//
		// ⚠️ 后处理失败**不判任务失败**（第一跳的钱已经花了，用户也拿到了片），
		//	由 adaptor 内部回落到原片，任务照常成功。
		//
		// postProcessOwnsURL 表示「成片地址已由后处理决定」——
		// 此时**不能**再用上游 URL 覆盖 ResultURL，否则 720P 产物会被 480P 顶掉。
		postProcessOwnsURL := false
		if pp := postProcessorFor(adaptor); pp != nil {
			// 已经有后处理任务在跑 → 这轮只推进它。
			if task.PrivateData.PostProcess != nil {
				done, perr := pp.PollPostProcess(ctx, task)
				if perr != nil {
					logger.LogWarn(ctx, fmt.Sprintf(
						"Task %s post process poll failed: %s", taskId, perr.Error()))
					model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
						model.TaskLogLevelError, "post process poll failed: "+perr.Error(), nil)
					// 查询失败不等于失败：保持处理中，下轮再试。
					postProcessPending = true
					break
				}
				if !done {
					postProcessPending = true
					break
				}
				model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
					model.TaskLogLevelInfo, "post process done", nil)
				// done=true：后处理已到终态（成功或回落原片），继续往下走置 SUCCESS。
				// 此时 ResultURL 已由 PollPostProcess 写好（成功=加工后产物，
				// 失败=回落原片），不能再被上游 URL 覆盖。
				postProcessOwnsURL = true
			} else {
				upstreamURL := strings.TrimSpace(taskResult.Url)
				handled, perr := pp.AfterUpstreamSuccess(ctx, task, upstreamURL)
				if perr != nil {
					// 接管了但出错：保持处理中，下轮重试，**不退款、不判失败**。
					logger.LogWarn(ctx, fmt.Sprintf(
						"Task %s post process submit failed: %s", taskId, perr.Error()))
					model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
						model.TaskLogLevelError, "post process submit failed: "+perr.Error(), nil)
					postProcessPending = true
					break
				}
				if handled {
					// 已提交后处理，产物还没好：保持处理中。
					model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePostProcess,
						model.TaskLogLevelInfo, "post process submitted", nil)
					postProcessPending = true
					break
				}
			}
		}
		if postProcessOwnsURL {
			// 成片地址已确定（后处理产物或回落原片），不再动它。
			task.PrivateData.ResultText = taskResult.ResultText
			shouldSettle = true
			break
		}
		if strings.HasPrefix(taskResult.Url, "data:") {
			// data: URI (e.g. Vertex base64 encoded video) — keep in Data, not in ResultURL
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		} else if taskResult.Url != "" {
			// Direct upstream URL (e.g. Kling, Ali, Doubao, etc.)
			task.PrivateData.ResultURL = taskResult.Url
		} else {
			// No URL from adaptor — construct proxy URL using public task ID
			task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
		}
		// 文本交付物（如视频反解的提示词）：与 ResultURL 互斥，单独落库。
		task.PrivateData.ResultText = taskResult.ResultText
		shouldSettle = true
	case model.TaskStatusFailure:
		logger.LogJson(ctx, fmt.Sprintf("Task %s failed", taskId), task)
		task.Status = model.TaskStatusFailure
		task.Progress = taskcommon.ProgressComplete
		if task.FinishTime == 0 {
			task.FinishTime = now
		}
		// 上游无理由失败统一为内容审核不通过，方便下游对接时定位
		// （视频任务跑一段时间才失败时，上游常不返回具体原因）。
		task.FailReason = taskcommon.NormalizeFailureReason(taskResult.Reason)
		taskResult.Reason = task.FailReason
		logger.LogInfo(ctx, fmt.Sprintf("Task %s failed: %s", task.TaskID, task.FailReason))
		taskResult.Progress = taskcommon.ProgressComplete
		if quota != 0 {
			shouldRefund = true
		}
	default:
		return fmt.Errorf("unknown task status %s for task %s", taskResult.Status, task.TaskID)
	}
	if taskResult.Progress != "" {
		task.Progress = taskResult.Progress
	}
	if postProcessPending {
		// 后处理还在跑：强制保持「处理中」，**不结算、不退款**。
		//
		// 这里必须盖掉 taskResult.Progress —— 上游回的是 SUCCESS，
		// ParseTaskResult 会带 100%，但用户拿到的产物还没加工完，
		// 显示 100% 会让前端以为已经完成。
		task.Status = model.TaskStatusInProgress
		task.Progress = taskcommon.ProgressInProgress
		// 结算与退款都跳过：等第二跳完成、真正交付产物后再结算。
		shouldSettle = false
		shouldRefund = false
	}

	isDone := task.Status == model.TaskStatusSuccess || task.Status == model.TaskStatusFailure
	// statusChanged/statusPersisted 用于在 CAS 成功之后才写流水：
	// 另一个进程已经推进过状态时，这里并没有真的发生迁移，记了就是假证据。
	statusChanged := snap.Status != task.Status
	statusPersisted := false
	if isDone && snap.Status != task.Status {
		won, err := task.UpdateWithStatus(snap.Status)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("UpdateWithStatus failed for task %s: %s", task.TaskID, err.Error()))
			shouldRefund = false
			shouldSettle = false
		} else if !won {
			logger.LogWarn(ctx, fmt.Sprintf("Task %s already transitioned by another process, skip billing", task.TaskID))
			shouldRefund = false
			shouldSettle = false
		} else {
			statusPersisted = true
		}
	} else if !snap.Equal(task.Snapshot()) {
		if _, err := task.UpdateWithStatus(snap.Status); err != nil {
			logger.LogError(ctx, fmt.Sprintf("Failed to update task %s: %s", task.TaskID, err.Error()))
		} else {
			statusPersisted = statusChanged
		}
	} else {
		// No changes, skip update
		logger.LogDebug(ctx, "No update needed for task %s", task.TaskID)
	}

	if statusChanged && statusPersisted {
		level := model.TaskLogLevelInfo
		if task.Status == model.TaskStatusFailure {
			level = model.TaskLogLevelError
		}
		model.AddTaskLog(task.TaskID, task.GetUpstreamTaskID(), model.TaskLogStagePoll, level,
			fmt.Sprintf("status %s -> %s", snap.Status, task.Status),
			map[string]any{
				"from":        snap.Status,
				"to":          task.Status,
				"progress":    task.Progress,
				"fail_reason": task.FailReason,
				"settle":      shouldSettle,
				"refund":      shouldRefund,
			})
	}

	if shouldSettle {
		settleTaskBillingOnComplete(ctx, adaptor, task, taskResult)
	}
	if shouldRefund {
		RefundTaskQuota(ctx, task, task.FailReason)
	}

	return nil
}

func redactVideoResponseBody(body []byte) []byte {
	var m map[string]any
	if err := common.Unmarshal(body, &m); err != nil {
		return body
	}
	resp, _ := m["response"].(map[string]any)
	if resp != nil {
		delete(resp, "bytesBase64Encoded")
		if v, ok := resp["video"].(string); ok {
			resp["video"] = truncateBase64(v)
		}
		if vs, ok := resp["videos"].([]any); ok {
			for i := range vs {
				if vm, ok := vs[i].(map[string]any); ok {
					delete(vm, "bytesBase64Encoded")
				}
			}
		}
	}
	b, err := common.Marshal(m)
	if err != nil {
		return body
	}
	return b
}

func truncateBase64(s string) string {
	const maxKeep = 256
	if len(s) <= maxKeep {
		return s
	}
	return s[:maxKeep] + "..."
}

// settleTaskBillingOnComplete 任务完成时的统一计费调整。
// 优先级：1. adaptor.AdjustBillingOnComplete 返回正数 → 使用 adaptor 计算的额度
//
//  2. taskResult.TotalTokens > 0 → 按 token 重算
//  3. 都不满足 → 保持预扣额度不变
func settleTaskBillingOnComplete(ctx context.Context, adaptor TaskPollingAdaptor, task *model.Task, taskResult *relaycommon.TaskInfo) {
	// 0. 按次计费的任务不做差额结算
	if bc := task.PrivateData.BillingContext; bc != nil && bc.PerCallBilling {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 按次计费，跳过差额结算", task.TaskID))
		return
	}
	// 1. 优先让 adaptor 决定最终额度
	if actualQuota := adaptor.AdjustBillingOnComplete(task, taskResult); actualQuota > 0 {
		RecalculateTaskQuota(ctx, task, actualQuota, "adaptor计费调整")
		return
	}
	// 2. 回退到 token 重算
	if taskResult.TotalTokens > 0 {
		RecalculateTaskQuotaByTokens(ctx, task, taskResult.TotalTokens)
		return
	}
	// 3. 无调整，保持预扣额度
}
