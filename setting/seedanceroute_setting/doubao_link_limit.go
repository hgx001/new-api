// Package seedanceroute_setting 的「豆包链路接单上限」。
//
// 背景（2026-11-13 老板定）：豆包链路上游给的是**运营自己掌握的一小批额度**，
// 不是「按天在凌晨 2 点重置」的那种日额度。运营的口径是：
//
//	「这批额度只接 3 单，接满就把豆包链路关掉，我手动再打开时又来 3 单。」
//
// 与既有的两个「关」语义的关系：
//
//	手动关闭（人工点开关）：关到下一个北京时间凌晨 2 点自动恢复。
//	    —— 对应「今天的日额度用完了，等它自己重置」。
//	限额关闭（接满 N 单）：**没有**自动恢复，必须由人再打开一次。
//	    —— 对应「这一批额度发完了，新的额度等我手动发」。
//
// 这两件事必须能区分：一个是「等着就好」，一个是「等我发额度」。
// 到点自愈的限额会让运营以为「等一会儿就好了」，实际是把没给的额度又发出去。
//
// 计数口径：**受理成功**的单数（不是请求数、不是进来的请求数）。
// 必须打在「上游已受理」之后 —— 打在请求进来时会把校验失败、
// 渠道不可用、被上游拒绝的请求也算进去，额度会白扣。
//
// 单一真相源：「是否接满」是**算出来的**（已接数 >= 上限），不存冗余标记。
// 存了就多一个可能与计数不一致的真相源 —— 运营改上限时它不会自动跟着变。
package seedanceroute_setting

// 豆包链路关闭原因。用来区分「为什么关」，让前端能给出对应的下一步动作。
const (
	// DoubaoLinkOffReasonManual 人工点开关关闭（关到凌晨 2 点自动恢复）。
	DoubaoLinkOffReasonManual = "manual"
	// DoubaoLinkOffReasonLimit 接单数达上限（没有自动恢复，等人再打开）。
	DoubaoLinkOffReasonLimit = "limit"
)

// DoubaoLinkMaxTasks 返回「豆包链路最多接多少单」。0 = 不限。
//
// 无限限的判定口径是 0 而不是负数：旧的生产 option JSON 里没有这个字段，
// 解析后是 0 —— 行为必须与改造前逐字节一致，不能因为加了字段就静默改变接单。
func DoubaoLinkMaxTasks() int {
	mu.RLock()
	defer mu.RUnlock()
	return current.DoubaoLinkMaxTasks
}

// DoubaoLinkAcceptedTasks 返回「本轮已受理多少单」。手动重新打开时归零。
func DoubaoLinkAcceptedTasks() int {
	mu.RLock()
	defer mu.RUnlock()
	return current.DoubaoLinkAcceptedTasks
}

// DoubaoLinkLimitReached 判断「是否已接满上限」。
//
// 只有「配了上限」才有这个状态：上限为 0（不限）时永远返回 false。
//
// 纯推导（已接数 >= 上限），不存标记 —— 存了就多一个可能与计数不一致的真相源。
func DoubaoLinkLimitReached() bool {
	mu.RLock()
	defer mu.RUnlock()
	return doubaoLinkLimitReached(current)
}

// doubaoLinkLimitReached = 配了上限 且 已接数 >= 上限。
func doubaoLinkLimitReached(p Policy) bool {
	if p.DoubaoLinkMaxTasks <= 0 {
		return false
	}
	return p.DoubaoLinkAcceptedTasks >= p.DoubaoLinkMaxTasks
}

// DoubaoLinkAccepting 判断「豆包链路现在还在接单吗」。
//
// false = 已关闭（人工关或限额满），请求应该落备胎班车。
func DoubaoLinkAccepting() bool {
	return !IsDoubaoLinkOff()
}

// DoubaoLinkRemaining 返回「还能接几单」。0 = 不限（没有这个概念）。
//
// 负数不可能出现：WithDoubaoLinkAcceptedTasks 不接受负值，
// 且这里额外兜一层 —— 计数一旦异常（配置被手改成负数），
// 剩余数算成比上限还多会让运营以为额度变多了。
func DoubaoLinkRemaining() int {
	mu.RLock()
	defer mu.RUnlock()
	return doubaoLinkRemaining(current)
}

func doubaoLinkRemaining(p Policy) int {
	if p.DoubaoLinkMaxTasks <= 0 {
		return 0
	}
	remaining := p.DoubaoLinkMaxTasks - p.DoubaoLinkAcceptedTasks
	if remaining < 0 {
		return 0
	}
	return remaining
}

// DoubaoLinkOffReason 返回「当前为什么关」。开着时返回空串。
//
// 优先级：人工关闭 > 限额满员。
//
// 人工优先的理由：人工关闭是**主动意图**（今天不接豆包了），
// 限额满员是**被动结果**（这一批发完了）。两者同时成立时，
// 运营真正需要知道的是「我什么时候能再打开」—— 手动关闭有自动恢复时间，
// 限额满员没有。
func DoubaoLinkOffReason() string {
	mu.RLock()
	defer mu.RUnlock()
	return doubaoLinkOffReason(current, nowUnix())
}

func doubaoLinkOffReason(p Policy, now int64) string {
	if p.DoubaoLinkOffUntil > now {
		return DoubaoLinkOffReasonManual
	}
	if doubaoLinkLimitReached(p) {
		return DoubaoLinkOffReasonLimit
	}
	return ""
}

// IsDoubaoLinkModel 判断「这个模型是不是豆包链路上的模型」。
//
// 口径 = 豆包链路承载的**对外模型名**集合：
//
//	· 规则一的目标模型（公共 + 按源模型覆盖）—— 超时长改派到豆包的那一档；
//	· 规则五的源模型 —— 客户直接请求豆包的那一档。
//
// 生产上两者都是 seedance-2-5（同一个对外名），但它们是**两个不同字段**：
// 规则一的 target_model 可能被按源模型覆盖成别的链路（活动版可以单独指向
// 心笔按次），规则五的源模型才是「豆包链路本身」。
//
// ⚠️ **不能**把规则五的目标（备胎 wan3.0-班车）算进来：备胎是豆包没额度时的
// 落点，它消耗的是备胎的额度，不是豆包的额度。算进来会导致「豆包关闭期间
// 落到备胎的每一单都被计数」，运营重新打开时发现一单都接不到。
func IsDoubaoLinkModel(modelName string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return isDoubaoLinkModel(current, modelName)
}

func isDoubaoLinkModel(p Policy, modelName string) bool {
	target := trimModelName(modelName)
	if target == "" {
		return false
	}
	if trimModelName(p.FailoverSourceModel) == target {
		return true
	}
	for _, source := range p.allSourceModels() {
		if trimModelName(p.TargetModelFor(source)) == target {
			return true
		}
	}
	return false
}

// WithDoubaoLinkMaxTasks 返回「把接单上限设成 maxTasks」的策略副本（纯函数，不落库）。
//
// maxTasks = 0 表示不限；负数按 0 处理（不制造一个「永远接不到单」的非法状态）。
//
// 改成 0（不再限额）时清掉计数：计数只在限额状态下有意义，
// 留着会让运营在重新配上限后看到「已接 7 单」这种历史数字。
//
// ⚠️ 上限从 3 改成 5 时**不清零**：已接掉的 3 单不会退回（额度已经发出去了），
// 清零等于白送 3 单额度。剩下的口径就是「本轮总共接 5 单」= 还能接 2 单。
func WithDoubaoLinkMaxTasks(maxTasks int) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	if maxTasks < 0 {
		maxTasks = 0
	}
	out.DoubaoLinkMaxTasks = maxTasks
	if maxTasks == 0 {
		out.DoubaoLinkAcceptedTasks = 0
	}
	out.UpdatedAt = nowUnix()
	return out
}

// WithDoubaoLinkAcceptedTasks 返回「把已接单数设成 accepted」的策略副本（纯函数，不落库）。
//
// 负数按 0 处理：剩余数算成比上限还多会让运营以为额度变多了。
//
// ⚠️ 计数**不许超过上限**：接单是并发发生的，两个请求可能同时读到「已接 2」
// 然后各写 3 —— 那会多发一单额度。收敛到上限，宁可少接不可多发。
func WithDoubaoLinkAcceptedTasks(accepted int) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	if accepted < 0 {
		accepted = 0
	}
	if out.DoubaoLinkMaxTasks > 0 && accepted > out.DoubaoLinkMaxTasks {
		accepted = out.DoubaoLinkMaxTasks
	}
	out.DoubaoLinkAcceptedTasks = accepted
	out.UpdatedAt = nowUnix()
	return out
}
