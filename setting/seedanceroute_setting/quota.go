// Package seedanceroute_setting 的额度自动降级逻辑。
//
// 背景：规则一（30 秒 → 豆包）依赖的渠道额度是**按天**计的。额度用完后，
// 命中规则的请求会被改派到豆包、然后必然失败，而重试只会用同一个模型名
// 重选渠道，**不会退回源链路**（见 prepareDoubaoRoute 的注释）。
//
// 人工开关能救，但漏一次就是整天的 30 秒请求全失败。这里把判断做成纯函数，
// 由 service 层在任务终态时调用：额度耗尽 → 自动暂停到今天结束；
// 出现成功 → 自动解除。
//
// 为什么暂停粒度是「今天结束」而不是固定 N 分钟：额度是按自然日重置的，
// 暂停到 24 点正好对齐重置点，到点自动失效，不需要任何人值守。
package seedanceroute_setting

import (
	"strings"
	"time"
)

// beijingZone 固定 UTC+8。
//
// 与 controller 里那份同源：用 FixedZone 而不是 LoadLocation("Asia/Shanghai")，
// 因为容器镜像不一定装 tzdata，LoadLocation 会报错并退化成 UTC ——
// 那样「暂停到今天结束」会变成「暂停到北京时间 8 点」，白白多停 8 小时。
// 中国不实行夏令时，固定偏移是准确的。
var beijingZone = time.FixedZone("CST", 8*3600)

// 暂停来源。区分人工与自动，是为了让前端能说清「谁停的」，
// 也为了让自动逻辑不去覆盖人工决定。
const (
	// PauseSourceManual 管理员在后台手动暂停（可能停到月底，理由与额度无关）。
	PauseSourceManual = "manual"
	// PauseSourceAuto 额度探针检测到渠道额度耗尽后自动暂停。
	PauseSourceAuto = "auto"
)

// ReasonQuotaExhausted 是自动暂停时写给前端看的原因文案。
const ReasonQuotaExhausted = "上游渠道今日额度已用完"

// quotaExhaustionMarkers 是「额度用完」在上游/中间层可能出现的写法。
//
// 为什么用包含匹配而不是精确等值：这条文案不产生在我们的代码里，是
// ArcReel 侧回传的 fail_reason 原文（2026-11-01 生产原话是
// 「渠道今日额度已用完，明天再来！」）。上游改一个字，等值匹配就静默失效，
// 而失效的表现是「额度用完了却照样改派过去」—— 没有任何报错。
//
// 反过来，marker 必须收得够窄：像「额度不足，请充值」属于余额问题，
// 不是按天额度，暂停路由对它没有任何帮助，反而会让本该走豆包的低成本
// 请求落回贵链路。
var quotaExhaustionMarkers = []string{
	"额度已用完",
	"额度用完",
	"quota_exhausted",
	"quota exhausted",
	"daily quota exceeded",
	"daily limit reached",
}

// LooksLikeQuotaExhausted 判断失败文案是否表示「上游今日额度用完」。
func LooksLikeQuotaExhausted(reason string) bool {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return false
	}
	lowered := strings.ToLower(trimmed)
	for _, marker := range quotaExhaustionMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// NextBeijingMidnight 返回「严格晚于 now 的下一个北京时间 0 点」（Unix 秒）。
//
// 用 day+1 而不是「当天 24 点」的等价写法：now 恰好是 0 点整时，
// 当天 0 点等于「现在」，暂停会变成空操作。一律取次日，语义才稳定。
// time.Date 会自己处理月末 / 年末进位（11-30 + 1 天 = 12-01）。
func NextBeijingMidnight(nowUnix int64) int64 {
	local := time.Unix(nowUnix, 0).In(beijingZone)
	return time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, beijingZone).Unix()
}

// ShouldAutoPause 判断「这次任务失败要不要自动暂停规则一」，并给出暂停到的时刻。
//
// 三个条件缺一不可：
//   - 任务实际执行的模型是目标模型（seedance-2-5）。源模型自己报额度耗尽
//     是另一条链路的问题，停改派只会让请求继续压在坏链路上。
//   - 失败原因是额度耗尽（而不是内容审核、素材超限 —— 那些改请求才有用）。
//   - 当前不是「人工暂停且尚未过期」。
//
// 第二个返回值 false 有两种含义，调用方都当作「不用写库」处理：
//   - 不该暂停；
//   - 已经暂停到不早于目标时刻（同一天反复观察，重复写 option 没有意义）。
func ShouldAutoPause(p Policy, modelName, failReason string) (int64, bool) {
	if !secondsRuleReady(p) {
		return 0, false
	}
	if strings.TrimSpace(modelName) != p.TargetModel {
		return 0, false
	}
	if !LooksLikeQuotaExhausted(failReason) {
		return 0, false
	}

	now := nowUnix()
	// 人工暂停且仍在有效期内 → 不许改写。
	//
	// 这是个很隐蔽的事故面：运营因为「豆包要停用到月底」手动暂停了，
	// 某天渠道 20 被直连客户耗尽额度触发自动暂停，把截止时间从 11-30
	// 改写成「今天 24 点」—— 次日规则一悄悄恢复，而运营以为还在停用中。
	// 已过期的人工暂停不算：那时它已经不生效了，自动接管没有语义冲突。
	if p.PauseSource == PauseSourceManual && p.SecondsRulePausedUntil > now {
		return 0, false
	}

	until := NextBeijingMidnight(now)
	if p.SecondsRulePausedUntil >= until {
		return 0, false
	}
	return until, true
}

// ShouldAutoResume 判断「这次任务成功要不要自动解除暂停」。
//
// 只解除**自动**暂停：人工暂停的理由可能与额度无关（账号要停用到月底、
// 在做对账），渠道上出现一次成功不能替管理员做恢复决定。
//
// 已过期的自动暂停也不处理 —— 到期本身就自动失效，再写一次库没有意义。
func ShouldAutoResume(p Policy, modelName string) bool {
	if !secondsRuleReady(p) {
		return false
	}
	if strings.TrimSpace(modelName) != p.TargetModel {
		return false
	}
	if p.PauseSource != PauseSourceAuto {
		return false
	}
	return p.SecondsRulePausedUntil > nowUnix()
}

// WithAutoPause 返回「自动暂停到 until」的策略副本（纯函数，不落库）。
func WithAutoPause(until int64, reason string) Policy {
	out := WithSecondsRulePausedUntil(until)
	out.PauseSource = PauseSourceAuto
	out.PauseReason = reason
	return out
}

// WithManualPause 返回「人工暂停到 until」的策略副本（纯函数，不落库）。
//
// until 传 0 表示取消暂停（与 WithSecondsRulePausedUntil 同口径）。
func WithManualPause(until int64) Policy {
	out := WithSecondsRulePausedUntil(until)
	if until > 0 {
		out.PauseSource = PauseSourceManual
		out.PauseReason = ""
		return out
	}
	out.PauseSource = ""
	out.PauseReason = ""
	return out
}

// WithAutoResume 返回「解除暂停」的策略副本（纯函数，不落库）。
//
// 与 WithManualPause(0) 等价，单独一个名字是为了让调用点读起来
// 表达「自动探针在恢复」，而不是「有人手动点了恢复」。
func WithAutoResume() Policy {
	return WithManualPause(0)
}
