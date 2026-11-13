package service

import (
	"context"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 豆包链路的接单计数（2026-11-13 老板定）。
//
// 语义：豆包链路上游给的是运营自己掌握的一小批额度。运营设「最多接 3 单」，
// 豆包链路接满 3 单就自动关闭（后续请求落备胎班车），运营手动再打开时
// 计数归零、再接 3 单。
//
// 为什么计数打在**受理成功**之后：
//
//	打在请求进来时 —— 校验失败、渠道不可用、被上游拒绝的请求也会被计数，
//	  额度白扣，运营看到「才接了 2 单就关了」却找不到是哪两单。
//	打在受理成功之后 —— 计的每一单都是上游真接下了的、真的会消耗额度的单。
//
// 为什么不在 middleware 里数：middleware 在渠道分发**之前**执行，
// 那时还不知道请求最终落到哪个渠道、能不能受理成功。
//
// 为什么只在「配了上限」时才写库：上限 0 = 不限，是默认状态（旧的 option JSON
// 里没有这个字段）。默认状态下每单都写一次 options 表是纯浪费 ——
// 一天几千次无意义的写库，还会把 options 表的 updated_at 刷成噪声。
var (
	// doubaoLimitMu 串行化「读计数 → +1 → 写回」。
	//
	// 必须串行：两个请求同时读到「已接 2」，各写 3 —— 那就多接了一单，
	// 超出运营给的额度。宁可少接不可多发额度。
	doubaoLimitMu sync.Mutex
)

// ObserveDoubaoLinkAccepted 在**豆包链路受理成功一单**后调用，推进接单计数。
//
// executedModel 必须是**实际执行**的模型名（不是客户请求的源模型）：
// 改派到豆包的请求，客户买的是 seedance2.5-特惠，实际跑的是 seedance-2-5 ——
// 只有按实际执行的模型判定，才知道这一单是不是消耗了豆包额度。
//
// 返回 true 表示「这一单把豆包链路接满了」。
func ObserveDoubaoLinkAccepted(executedModel string) bool {
	// 没配上限 = 不限：不加锁、不读策略、不写库。
	//
	// 这一道早退放在最前面，是为了让默认状态（不限）的开销接近零。
	if seedanceroute_setting.DoubaoLinkMaxTasks() <= 0 {
		return false
	}
	if !seedanceroute_setting.IsDoubaoLinkModel(executedModel) {
		return false
	}

	doubaoLimitMu.Lock()
	defer doubaoLimitMu.Unlock()

	// 加锁后**重新读一次**上限与计数：进入临界区前的那次读可能已经过期。
	maxTasks := seedanceroute_setting.DoubaoLinkMaxTasks()
	if maxTasks <= 0 {
		return false
	}
	accepted := seedanceroute_setting.DoubaoLinkAcceptedTasks()
	if accepted >= maxTasks {
		// 已经满了：不再计数。但**要确保关闭状态已落库** ——
		// 可能上一单的写库失败过（数据库抖动），这里补一次。
		if seedanceroute_setting.DoubaoLinkLimitReached() {
			return true
		}
	}

	next := accepted + 1
	policy := seedanceroute_setting.WithDoubaoLinkAcceptedTasks(next)
	if err := persistRoutePolicy(policy); err != nil {
		// 落库失败不能让客户的请求崩掉 —— 任务已经受理成功、额度已经花出去了。
		// 计数只是运营的额度台账，下次受理还会再试一次。
		logger.LogWarn(context.Background(),
			fmt.Sprintf("豆包链路接单计数写入失败（下次受理会再试）：model=%s accepted=%d/%d err=%v",
				executedModel, next, maxTasks, err))
		return false
	}

	logger.LogInfo(context.Background(),
		fmt.Sprintf("豆包链路接单计数：%s 已受理 %d/%d 单", executedModel, next, maxTasks))

	if next < maxTasks {
		return false
	}

	// 接满了：把链路显式关掉，让后续请求落备胎班车。
	//
	// ⚠️ 关闭**不带自动恢复时间**（DoubaoLinkOffUntil 保持 0）：
	// 额度是运营手动给的，必须由人再打开一次。到点自愈会把没给的额度又发出去。
	//
	// 为什么还要显式写一次「关闭」：doubaoLinkOff 的判定已经包含
	// 「计数 >= 上限」，这一单写下去后链路自然就是关的 —— 显式关闭是**冗余**的。
	// 这里不重复写，只靠计数推导（单一真相源）。
	return true
}
