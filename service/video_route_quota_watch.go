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

	// ── 规则五（故障转移）：源链路额度耗尽 → 标记不可用 ──
	//
	// 与规则一的暂停是**两件独立的事**，必须分开处理：
	//
	//	规则一：源模型（促销族）→ 目标模型（豆包）。暂停的是「改派给豆包」，
	//	        暂停期间请求留在源模型跑。
	//	规则五：源模型（seedance-2-5）→ 备胎（班车）。这里标记的是
	//	        「**seedance-2-5 这条链路本身**额度耗尽」，之后凡是请求
	//	        seedance-2-5 的都改派到班车。
	//
	// 两者可能同时发生（豆包既是规则一的目标、又是规则五的源），
	// 所以这里的顺序不能反：先判规则五，它决定「这条链路还通不通」；
	// 再判规则一，它决定「促销族要不要改派给这条链路」。
	// 若豆包额度耗尽，规则一改派过去也会失败 —— 但规则五已经把它
	// 标记为不可用了，规则一的暂停与否就不重要（请求本来就不走豆包）。
	if handled := observeFailoverSourceOutcome(policy, task, succeeded); handled {
		// 规则五已经处理完这个终态。规则一是否也要处理，取决于
		// ShouldAutoPause 的判定（它比对的是 modelName 与 TargetModel），
		// 两者互不干扰，所以这里**不 return**，继续往下走。
		policy = seedanceroute_setting.Get()
	}

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

// observeFailoverSourceOutcome 处理规则五的「源链路不可用」标记。
//
// 返回 true 表示这个终态已经被规则五处理过（无论有没有实际改状态）。
//
// 触发条件（两个都要满足）：
//   - 任务实际执行的模型 == 规则五的源模型（seedance-2-5）；
//   - 任务失败，且失败文案表示「额度用完」。
//
// 为什么成功时也要处理：**额度可能提前恢复**（有人充值、或上游按小时重置）。
// 源链路一旦成功出片，就说明它又通了，必须解除标记，否则之后的请求
// 会一直白白改派到备胎（备胎慢 18 倍、且成片规格不同）。
//
// 标记的失效时间对齐**北京时间次日 0 点**：豆包额度按自然日重置，
// 与规则一的暂停口径一致。
func observeFailoverSourceOutcome(policy seedanceroute_setting.Policy, task *model.Task, succeeded bool) bool {
	if !seedanceroute_setting.IsFailoverRouteEnabled() {
		return false
	}
	modelName := executedModelName(task)
	if !seedanceroute_setting.IsFailoverSource(modelName) {
		return false
	}

	if succeeded {
		// 没有标记就不需要写库 —— 避免每次成功都写一次 option。
		if !seedanceroute_setting.FailoverUnavailable(time.Now().Unix()) {
			return true
		}
		// ⚠️ **只解除自己写的自动标记**。
		//
		// 人工标记（运营点按钮标成不可用）**不许**被一次成功自动解除 ——
		// 人工标记的语义是「我知道上游今天一直挂着，别再打过去了」，
		// 可能上游只是偶尔成功一次（重试、缓存命中）。自动解除会让运营
		// 的判断失效，而且他根本不知道发生过这件事。
		//
		// 与规则一的 ShouldAutoResume 同一口径（只解除 PauseSourceAuto）。
		if seedanceroute_setting.FailoverUnavailableSource() != seedanceroute_setting.PauseSourceAuto {
			return true
		}
		if err := persistRoutePolicy(seedanceroute_setting.WithFailoverUnavailable(0, "")); err != nil {
			logger.LogWarn(context.Background(),
				fmt.Sprintf("故障转移标记解除失败（下次成功会再试）：model=%s err=%v", modelName, err))
			return true
		}
		logger.LogInfo(context.Background(),
			fmt.Sprintf("故障转移标记解除：源链路 %s 已恢复出片，请求不再改派到备胎", modelName))
		return true
	}

	if !seedanceroute_setting.LooksLikeQuotaExhausted(task.FailReason) {
		return true
	}

	until := seedanceroute_setting.NextBeijingMidnight(time.Now().Unix())
	// 已经在标记中，且截止时间不早于目标时刻 → 不重复写。
	if seedanceroute_setting.FailoverUnavailable(time.Now().Unix()) &&
		seedanceroute_setting.FailoverUnavailableUntil() >= until {
		return true
	}
	if err := persistRoutePolicy(seedanceroute_setting.WithFailoverUnavailable(
		until, seedanceroute_setting.ReasonQuotaExhausted)); err != nil {
		logger.LogWarn(context.Background(),
			fmt.Sprintf("故障转移标记写入失败（下次失败会再试）：model=%s err=%v", modelName, err))
		return true
	}
	logger.LogInfo(context.Background(),
		fmt.Sprintf("故障转移标记写入：源链路 %s %s，标记不可用到 %s，请求改派到备胎",
			modelName, seedanceroute_setting.ReasonQuotaExhausted,
			time.Unix(until, 0).Format("2006-01-02 15:04:05")))
	return true
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
