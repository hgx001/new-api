// Package seedanceroute_setting 保存「seedance2.5-特惠 → seedance-2-5」条件路由的配置。
//
// 为什么单独一个包：这份配置描述的是「什么条件下把请求改派到另一个模型」这件事本身，
// 不是通用的倍率表，也不是模型定价。放独立包里，路由中间件只依赖这一份策略，
// 不会把计费链（ratio_setting）和路由决策耦合在一起。
//
// 为什么需要它：同一个对外模型名，在「时长 = 30 秒」时走豆包官网链路（按次 ¥5.00），
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
	"strings"
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
// 目前有**四条独立规则**，共用 Enabled 总开关与 SourceModel：
//
//	规则三（首尾帧）：SourceModel + media 含 first_frame/last_frame → FramesTargetModel
//	               seedance2.5-特惠 + 首尾帧 → wan3.0-720p-特惠
//	规则一（时长）：SourceModel + TriggerSeconds 秒 → TargetModel
//	               seedance2.5-特惠 + 30 秒 → seedance-2-5（豆包按次 ¥5.00）
//	规则四（长提示词）：SourceModel + 提示词 > 5000 字符 → TargetModel
//	               seedance2.5-特惠 + 超长提示词 → seedance-2-5
//	规则二（比例）：SourceModel + TriggerRatio 比例 → RatioTargetModel
//	               seedance2.5-特惠 + 9:16 → wan3.0-720p-特惠（¥0.20/秒）
//
// **优先级：规则三最先判，然后规则一、规则四，最后规则二。**
//
//	规则一/二之间：老板 2026-10-30 拍板「30 秒优先，豆包仍接 9:16」，
//	  即 30 秒 + 9:16 走豆包，只有非 30 秒的 9:16 才走 wan3。
//
//	规则四为什么和规则一共用 TargetModel 与暂停窗口：两者的目标链路是同一条
//	  豆包链路、同一份额度。额度用完时规则一会被自动暂停，规则四必须一起停 ——
//	  否则暂停期间超长提示词照样打过去、照样失败。也因为这个原因，规则四
//	  **没有独立的开关字段**：它跟着 IsEnabled() 走。
//
//	规则三为什么必须排在最前：源链路（youzan wan3）**已经彻底不收首尾帧**
//	  （2026-10-28 上游取消 firstFrameAssetId/lastFrameAssetId），带首尾帧的请求
//	  打到源模型必然 400；而规则一的目标（漫屋）**也不支持首尾帧**。
//	  所以「带首尾帧」的请求只有 wan3 直连能跑，这个约束优先级高于时长/比例
//	  的成本优化 —— 否则 30 秒的首尾帧请求会被规则一改派到漫屋，直接失败。
//
// ⚠️ 规则二/三/四的字段**全部 omitempty 且可为空**：旧的生产 option JSON 里没有它们，
// 解析后为空 → 对应规则不生效 → 行为与改造前逐字节一致。这是硬要求，
// 因为改这份配置的人不一定同步改 SQL。
type Policy struct {
	// Enabled 总开关。false = 完全不路由，全部走源模型（默认，行为不变）。
	Enabled bool `json:"enabled"`
	// SourceModel 源模型名（对外名，客户请求里写的那个）。三条规则共用。
	//
	// ⚠️ 保留这个字段是为了**向后兼容**：生产 options 里存的是单值格式
	// （"source_model": "seedance2.5-特惠"），它必须继续生效。
	//
	// 多源场景用 SourceModels；两个字段都配时取**并集**（不是覆盖）。
	SourceModel string `json:"source_model"`
	// SourceModels 是源模型的**列表**（2026-11-02 起）。
	//
	// 为什么需要：seedance2.5 促销族有两个对外模型名（母本 seedance2.5-特惠、
	// 活动版 seedance2.5-活动），它们是同一个商品的两档售价，必须**共享同一套
	// 路由规则** —— 尤其 30 秒规则：活动版走有赞成本 ¥4.50、卖 ¥4.50（零毛利），
	// 路由到豆包后成本 ≈ ¥0。
	//
	// 只有一个源模型时配置这个字段与配 SourceModel 完全等价。
	SourceModels []string `json:"source_models,omitempty"`
	// TargetModel 目标模型名（对外名），规则一用。
	TargetModel string `json:"target_model"`
	// TriggerSeconds 触发时长（秒）。**只有等于该值才路由**，规则一用。
	TriggerSeconds int `json:"trigger_seconds"`

	// RatioTargetModel 规则二的目标模型名。空 = 规则二不启用。
	RatioTargetModel string `json:"ratio_target_model,omitempty"`
	// TriggerRatio 规则二的触发比例（如 "9:16"）。空 = 规则二不启用。
	TriggerRatio string `json:"trigger_ratio,omitempty"`

	// FramesTargetModel 规则三的目标模型名。空 = 规则三不启用。
	//
	// 触发条件没有可配的值：**请求 media 里出现 first_frame / last_frame 就命中**。
	// 首尾帧是能力约束不是调度偏好，不存在「阈值」可调。
	FramesTargetModel string `json:"frames_target_model,omitempty"`

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

	// PauseSource 记录当前这次暂停是谁发起的："manual"（管理员）或
	// "auto"（额度探针）。空 = 未暂停。
	//
	// 为什么要区分：自动暂停不许覆盖人工暂停（人工可能停到月底、理由与
	// 额度无关），自动恢复也不许解除人工暂停。没有这个字段就分不清
	// 「谁停的」，两条规则只能靠猜。
	PauseSource string `json:"pause_source,omitempty"`

	// PauseReason 是自动暂停的原因文案，给前端直接展示（人工暂停时为空）。
	PauseReason string `json:"pause_reason,omitempty"`

	// UpdatedAt 最近一次修改的 Unix 时间戳，0 表示从未改过。
	UpdatedAt int64 `json:"updated_at"`
}

// defaultPolicy 与「不做这个路由」等价：开关关、模型名为空。
//
// ⚠️ 默认值必须让路由**完全不生效**：没有这个 option 时（或者 option 被删了），
// Get() 返回这里的内容，中间件直接放行 —— 行为与改造前逐字节一致。
func defaultPolicy() Policy {
	return Policy{
		Enabled:           false,
		SourceModel:       "",
		TargetModel:       "",
		TriggerSeconds:    0,
		RatioTargetModel:  "",
		TriggerRatio:      "",
		FramesTargetModel: "",
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
//   - 规则二配全 = 源模型名 + 比例目标模型 + 触发比例三者都有值；
//   - 规则三配全 = 源模型名 + 首尾帧目标模型两者都有值（触发条件不可配）。
//
// 「源模型名」可以是单值（source_model）或多值（source_models），任一非空即算配了。
//
// 一条都没配全就当作没开（Enabled 置 false）—— 宁可不开也不要半开，
// 半开的状态（比如 target_model 为空）会把请求改写成一个空模型名，线上直接全挂。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Policy
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	if parsed.Enabled {
		if !secondsRuleReady(parsed) && !ratioRuleReady(parsed) && !framesRuleReady(parsed) {
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

// IsFramesRouteEnabled 判断「规则三（首尾帧）」当前是否生效。
//
// 同样不受 SecondsRulePausedUntil 影响 —— 暂停只针对「按时长改派到豆包」。
// 首尾帧改派是**能力兜底**（源链路根本跑不了首尾帧），不能因为豆包额度用完
// 就跟着停掉，否则带首尾帧的请求会直接 400。
func IsFramesRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return framesRuleReady(current)
}

// IsAnyRouteEnabled 判断是否有**任意一条**规则生效。
//
// 中间件早退必须用这个，不能用 IsEnabled —— 只配了比例/首尾帧规则时
// IsEnabled 是 false，用它会直接跳过整个中间件，那两条规则永远不生效。
func IsAnyRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return secondsRuleActive(current, nowUnix()) ||
		ratioRuleReady(current) ||
		framesRuleReady(current)
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
		p.hasSourceModel() &&
		p.TargetModel != "" &&
		p.TriggerSeconds > 0
}

// hasSourceModel 判断策略是否配了**至少一个**源模型。
//
// 单值字段（SourceModel）与多值字段（SourceModels）任一非空即算配了 ——
// 只配 source_models 的新式配置不能被判定成「没配全」，否则路由会被静默关掉。
func (p Policy) hasSourceModel() bool {
	if strings.TrimSpace(p.SourceModel) != "" {
		return true
	}
	for _, name := range p.SourceModels {
		if strings.TrimSpace(name) != "" {
			return true
		}
	}
	return false
}

// IsSourceModel 判断「这个模型名是不是本策略的源模型」。
//
// 判定口径 = SourceModel（单值）与 SourceModels（多值）的**并集**。
//
// 为什么两者都留着：生产的 options 里存的是单值格式（"source_model"），
// 它必须继续生效；而多源场景（seedance2.5 促销族）需要数组形式。
// 两个字段都配时取并集 —— 不能是「后者覆盖前者」，那样改配置的人
// 会发现原来的单源突然不生效了。
//
// ⚠️ 两边都容忍首尾空格：生产上真出现过「 seedance2.5-特惠」这种请求
// （model/log.go 里记着同一个事故）。请求侧与配置侧都要 Trim。
func (p Policy) IsSourceModel(model string) bool {
	target := strings.TrimSpace(model)
	if target == "" {
		return false
	}
	if strings.TrimSpace(p.SourceModel) == target {
		return true
	}
	for _, name := range p.SourceModels {
		if strings.TrimSpace(name) == target {
			return true
		}
	}
	return false
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
		p.hasSourceModel() &&
		p.RatioTargetModel != "" &&
		p.TriggerRatio != ""
}

// framesRuleReady 判断规则三是否配全。
//
// 比另外两条少一个条件：**没有「触发值」可配**。首尾帧是能力约束，
// 请求里出现 first_frame/last_frame 就命中，不存在阈值。
func framesRuleReady(p Policy) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.FramesTargetModel != ""
}

func clonePolicy(p Policy) Policy {
	out := p
	return out
}
