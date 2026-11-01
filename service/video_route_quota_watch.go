package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 额度自动降级：把「渠道额度用完」这个上游事实，变成「规则一自动暂停」。
//
// 为什么需要它：规则一命中后请求被改派到豆包链路，重试只会用同一个模型名
// 重选渠道，**不会退回源链路**。而豆包额度是按天计的 —— 用完那一刻起，
// 每个 30 秒请求都会失败，直到次日重置。
//
// 人工开关能救，但漏一次就是整天的失败。这里挂在任务终态上自动跑：
//
//	失败且文案是「额度用完」 → 暂停到今天结束（落回源链路，能出片）
//	暂停期间出现成功         → 立刻接回改派（额度提前恢复的情况）
//
// 只动**自动**暂停：人工暂停可能停到月底、理由与额度无关，自动逻辑
// 不许覆盖，也不许替管理员做恢复决定（见 seedanceroute_setting.quota.go）。

var (
	// updateRoutePolicyOption 是落库钩子，测试里替换成内存收集。
	//
	// 走 model.UpdateOption 而不是直接改 setting 包内存：它会先写库、
	// 再经 updateOptionMap 分发到 UpdateFromJSONString。少任何一步都会出现
	// 「重启后失效」或「内存改了库没改」的不一致。
	updateRoutePolicyOption = model.UpdateOption

	// quotaWatchMu 串行化「读策略 → 判断 → 写策略」。
	//
	// 多个任务同时终态时，两个 goroutine 可能都读到「未暂停」再各写一次；
	// 结果本身幂等，但加锁后日志和 UpdatedAt 更可读，也避免成功/失败
	// 两类事件交错时互相覆盖。
	quotaWatchMu sync.Mutex
)

// observeVideoTaskOutcome 在任务**真正**进入终态后调用，驱动额度探针。
//
// 调用方必须在 CAS 成功（status 确实发生了迁移）之后调用，否则同一批
// 轮询里重复观察会反复写 option。
func observeVideoTaskOutcome(task *model.Task) {
	if task == nil {
		return
	}

	var succeeded bool
	switch task.Status {
	case model.TaskStatusSuccess:
		succeeded = true
	case model.TaskStatusFailure:
		succeeded = false
	default:
		return
	}

	modelName := executedModelName(task)
	if modelName == "" {
		return
	}

	quotaWatchMu.Lock()
	defer quotaWatchMu.Unlock()

	policy := seedanceroute_setting.Get()

	if succeeded {
		if !seedanceroute_setting.ShouldAutoResume(policy, modelName) {
			return
		}
		if err := persistRoutePolicy(seedanceroute_setting.WithAutoResume()); err != nil {
			logger.LogWarn(context.Background(),
				fmt.Sprintf("视频路由自动恢复失败（下次成功会再试）：model=%s err=%v", modelName, err))
			return
		}
		logger.LogInfo(context.Background(),
			fmt.Sprintf("视频路由自动恢复：目标链路已出片（model=%s），规则一重新生效", modelName))
		return
	}

	until, ok := seedanceroute_setting.ShouldAutoPause(policy, modelName, task.FailReason)
	if !ok {
		return
	}
	paused := seedanceroute_setting.WithAutoPause(until, seedanceroute_setting.ReasonQuotaExhausted)
	if err := persistRoutePolicy(paused); err != nil {
		logger.LogWarn(context.Background(),
			fmt.Sprintf("视频路由自动暂停失败（下次失败会再试）：model=%s err=%v", modelName, err))
		return
	}
	logger.LogInfo(context.Background(),
		fmt.Sprintf("视频路由自动暂停：%s（model=%s），规则一暂停到 %s，请求落回源链路",
			seedanceroute_setting.ReasonQuotaExhausted, modelName,
			time.Unix(until, 0).Format("2006-01-02 15:04:05")))
}

// executedModelName 取任务**实际执行**的模型名。
//
// 必须用 Properties.OriginModelName 而不是 PublicModelName()：后者对改派任务
// 返回的是改派前的源模型名（对客口径），拿它比对目标模型永远不会命中。
func executedModelName(task *model.Task) string {
	return strings.TrimSpace(task.Properties.OriginModelName)
}

// persistRoutePolicy 把策略写进 options 表并分发到内存状态。
func persistRoutePolicy(policy seedanceroute_setting.Policy) error {
	raw, err := common.Marshal(policy)
	if err != nil {
		return err
	}
	return updateRoutePolicyOption(seedanceroute_setting.OptionKey, string(raw))
}
