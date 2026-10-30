// Package seedanceroute_setting 保存「seedance2.5-特惠 → seedance-2-5」条件路由的配置。
//
// 为什么单独一个包：这份配置描述的是「什么条件下把请求改派到另一个模型」这件事本身，
// 不是通用的倍率表，也不是模型定价。放独立包里，路由中间件只依赖这一份策略，
// 不会把计费链（ratio_setting）和路由决策耦合在一起。
//
// 为什么需要它：同一个对外模型名，在「时长 = 30 秒」时走豆包官网链路（按次 ¥3.00），
// 其余情况走有赞链路（按秒 ¥0.20/秒）。两条链路的成本结构完全不同，
// 必须由开关控制、且默认关闭 —— 没有这个 option 时行为与改造前完全一致。
//
// 数据流：
//
//	管理员/SQL 改 options 表的 Seedance25TeHuiRoute30s
//	  → model/option.go 分发到本包 UpdateFromJSONString
//	  → middleware/seedance_route.go 在 Distribute 之前读 Get()
//	  → 命中则改写请求体（model 换成目标模型 + 参考图转格式）
//	  → 后续选渠道/计费全部自动正确（不需要改任何 adaptor）
package seedanceroute_setting

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "Seedance25TeHuiRoute30s"

// MaintenanceKeyOption 是「条件路由运维开关」的共享密钥在 options 表里的键名。
//
// 为什么单独一个 option 而不是塞进 Policy：Policy 会被 Get() 返回、会被
// MarshalJSONString 序列化进 common.OptionMap，密钥混在里面很容易随日志或
// 调试输出泄露。单独一个键，只有鉴权中间件读它。
//
// 空值 = 运维接口关闭（返回 404），这是默认状态。
const MaintenanceKeyOption = "VideoRouteMaintenanceKey"

// Policy 是条件路由策略。
//
// 目前有**两条独立规则**，共用 Enabled 总开关与 SourceModel：
//
//	规则一（时长）：SourceModel + TriggerSeconds 秒 → TargetModel
//	               seedance2.5-特惠 + 30 秒 → seedance-2-5（豆包按次 ¥3.00）
//	规则二（比例）：SourceModel + TriggerRatio 比例 → RatioTargetModel
//	               seedance2.5-特惠 + 9:16 → wan3.0-720p-特惠（¥0.20/秒）
//
// **优先级：规则一先判**（老板 2026-10-30 拍板「30 秒优先，豆包仍接 9:16」）。
// 即 30 秒 + 9:16 走豆包；只有非 30 秒的 9:16 才走 wan3。
//
// ⚠️ 规则二的字段**全部 omitempty 且可为空**：旧的生产 option JSON 里没有它们，
// 解析后为空 → 规则二不生效 → 行为与改造前逐字节一致。这是硬要求，
// 因为改这份配置的人不一定同步改 SQL。
type Policy struct {
	// Enabled 总开关。false = 完全不路由，全部走源模型（默认，行为不变）。
	Enabled bool `json:"enabled"`
	// SourceModel 源模型名（对外名，客户请求里写的那个）。两条规则共用。
	SourceModel string `json:"source_model"`
	// TargetModel 目标模型名（对外名），规则一用。
	TargetModel string `json:"target_model"`
	// TriggerSeconds 触发时长（秒）。**只有等于该值才路由**，规则一用。
	TriggerSeconds int `json:"trigger_seconds"`

	// RatioTargetModel 规则二的目标模型名。空 = 规则二不启用。
	RatioTargetModel string `json:"ratio_target_model,omitempty"`
	// TriggerRatio 规则二的触发比例（如 "9:16"）。空 = 规则二不启用。
	TriggerRatio string `json:"trigger_ratio,omitempty"`

	// SecondsRulePausedUntil 规则一的暂停截止时间（Unix 秒）。0 = 未暂停。
	//
	// 为什么需要「到期时间」而不是一个布尔开关：目标链路（豆包官网）的额度是
	// **按天**计的，用完当天不可用、次日 0 点自动恢复。用布尔开关就得有人记得
	// 回来打开 —— 漏一次就会长期悄悄跑在另一条链路上，且没有任何报错。
	// 用到期时间，到点自动恢复，不需要任何人值守。
	//
	// ⚠️ **只影响规则一**。不要用 Enabled 来表达「暂停」：
	// Enabled 是两条规则的总开关，关掉会把「9:16 → wan3」也一起停掉。
	SecondsRulePausedUntil int64 `json:"seconds_rule_paused_until,omitempty"`

	// UpdatedAt 最近一次修改的 Unix 时间戳，0 表示从未改过。
	UpdatedAt int64 `json:"updated_at"`
}

// defaultPolicy 与「不做这个路由」等价：开关关、模型名为空。
//
// ⚠️ 默认值必须让路由**完全不生效**：没有这个 option 时（或者 option 被删了），
// Get() 返回这里的内容，中间件直接放行 —— 行为与改造前逐字节一致。
func defaultPolicy() Policy {
	return Policy{
		Enabled:          false,
		SourceModel:      "",
		TargetModel:      "",
		TriggerSeconds:   0,
		RatioTargetModel: "",
		TriggerRatio:     "",
	}
}

var (
	mu      sync.RWMutex
	current = defaultPolicy()
)

// Get 返回当前策略的深拷贝，调用方随便改都不会影响全局状态。
func Get() Policy {
	mu.RLock()
	defer mu.RUnlock()
	return clonePolicy(current)
}

// MarshalJSONString 把当前策略序列化成 JSON（写回 option 用）。
func MarshalJSONString() string {
	mu.RLock()
	defer mu.RUnlock()
	encoded, err := common.Marshal(current)
	if err != nil {
		return "{}"
	}
	return string(encoded)
}

// WithSecondsRulePausedUntil 返回一份「规则一暂停到 until」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成，
// 走的是正常的 option 分发链路（updateOptionMap → UpdateFromJSONString）。
// 把「怎么改」收敛在这里，controller 就不需要知道字段名，将来加字段也不用改两处。
//
// until 传 0 表示取消暂停（恢复）。
func WithSecondsRulePausedUntil(until int64) Policy {
	mu.RLock()
	defer mu.RUnlock()
	out := clonePolicy(current)
	out.SecondsRulePausedUntil = until
	out.UpdatedAt = nowUnix()
	return out
}

// UpdateFromJSONString 用管理员提交的 JSON 覆盖策略。
//
// 开启路由的**必要条件**：至少有一条规则配全。
//   - 规则一配全 = 源模型名 + 目标模型名 + 触发时长三者都有值；
//   - 规则二配全 = 源模型名 + 比例目标模型 + 触发比例三者都有值。
//
// 一条都没配全就当作没开（Enabled 置 false）—— 宁可不开也不要半开，
// 半开的状态（比如 target_model 为空）会把请求改写成一个空模型名，线上直接全挂。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Policy
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	if parsed.Enabled {
		secondsReady := parsed.SourceModel != "" && parsed.TargetModel != "" && parsed.TriggerSeconds > 0
		ratioReady := parsed.SourceModel != "" && parsed.RatioTargetModel != "" && parsed.TriggerRatio != ""
		if !secondsReady && !ratioReady {
			parsed.Enabled = false
		}
	}
	mu.Lock()
	defer mu.Unlock()
	current = parsed
	return nil
}

// IsEnabled 判断「规则一（时长）」当前是否生效。
// 三个必要条件缺一不可 —— 与 UpdateFromJSONString 的校验口径保持一致，
// 防止有人绕过 option 分发直接改内存状态。另外还要**不在暂停期内**。
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return secondsRuleActive(current, nowUnix())
}

// IsRatioRouteEnabled 判断「规则二（比例）」当前是否生效。
//
// 不受 SecondsRulePausedUntil 影响：暂停的是「按时长改派到豆包」这一条，
// 与「按比例改派到 wan3」是两件独立的事。
func IsRatioRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return ratioRuleReady(current)
}

// IsAnyRouteEnabled 判断是否有**任意一条**规则生效。
//
// 中间件早退必须用这个，不能用 IsEnabled —— 只配了比例规则时
// IsEnabled 是 false，用它会直接跳过整个中间件，比例规则永远不生效。
func IsAnyRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return secondsRuleActive(current, nowUnix()) || ratioRuleReady(current)
}

// IsSecondsRulePaused 报告规则一当前是否处于暂停期。
func IsSecondsRulePaused() bool {
	mu.RLock()
	defer mu.RUnlock()
	return secondsRulePaused(current, nowUnix())
}

// SecondsRulePausedUntil 返回规则一的暂停截止时间（Unix 秒），0 表示未暂停。
//
// ⚠️ 返回的是**配置值**，不是「是否生效」—— 已过期的截止时间同样会原样返回。
// 判断是否生效请用 IsSecondsRulePaused。
func SecondsRulePausedUntil() int64 {
	mu.RLock()
	defer mu.RUnlock()
	return current.SecondsRulePausedUntil
}

func secondsRuleReady(p Policy) bool {
	return p.Enabled &&
		p.SourceModel != "" &&
		p.TargetModel != "" &&
		p.TriggerSeconds > 0
}

// secondsRulePaused 判断当前是否处于规则一的暂停期。
//
// 用 `>` 而不是 `>=`：截止时间那一秒**已经恢复**。
// 例：截止 1793404800，在 now=1793404800 时返回 false（不再暂停）。
func secondsRulePaused(p Policy, now int64) bool {
	return p.SecondsRulePausedUntil > now
}

// secondsRuleActive = 配全了 且 不在暂停期。
func secondsRuleActive(p Policy, now int64) bool {
	return secondsRuleReady(p) && !secondsRulePaused(p, now)
}

// nowUnix 是当前时间的 Unix 秒。
//
// 声明成变量而不是直接调 time.Now()，**只为单测可替换** ——
// 测「到期自动恢复」不可能真的等一天。生产代码路径永远走默认实现。
var nowUnix = func() int64 { return time.Now().Unix() }

func ratioRuleReady(p Policy) bool {
	return p.Enabled &&
		p.SourceModel != "" &&
		p.RatioTargetModel != "" &&
		p.TriggerRatio != ""
}

func clonePolicy(p Policy) Policy {
	out := p
	return out
}
