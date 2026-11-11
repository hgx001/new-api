// Package seedanceroute_setting 保存「seedance2.5-特惠 → seedance-2-5」条件路由的配置。
//
// 为什么单独一个包：这份配置描述的是「什么条件下把请求改派到另一个模型」这件事本身，
// 不是通用的倍率表，也不是模型定价。放独立包里，路由中间件只依赖这一份策略，
// 不会把计费链（ratio_setting）和路由决策耦合在一起。
//
// 为什么需要它：同一个对外模型名，在「时长 = 30 秒」时走豆包官网链路（按次 ¥3.50），
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
	"errors"
	"fmt"
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
//	规则五（故障转移）：FailoverSourceModel 源链路不可用 → FailoverTargetModel（最优先）
//	规则三（首尾帧）：SourceModel + media 含 first_frame/last_frame → FramesTargetModel
//	规则一（时长）：SourceModel + 时长 > TriggerSeconds → TargetModel（豆包按次）
//	               （2026-11-09 起时长原样下发，不再吸附档位）
//	规则二（比例）：SourceModel + TriggerRatio 比例 → RatioTargetModel
//	               生产 2026-11-11 起：9:16 → wan3.0-720p-特惠-内链（心笔 480P 链路）
//
// 2026-11-11 删除原「规则四（长提示词 > 5000 字符 → TargetModel）」：
// 它几乎总被规则一抢先，且超长提示词已有 Wan3PromptCompress 处理，
// 不需要第二条隐式改派。中间件里不再读提示词长度，Policy 也不再有对应字段。
//
// **优先级：规则五 → 规则三 → 规则一 → 规则二。**
//
//	规则一/二之间：老板 2026-10-30 拍板「长时长优先」，即超过阈值的 9:16 走豆包，
//	  只有未超阈值的 9:16 才走比例规则。
//
//	规则三为什么必须排在最前：源链路（youzan wan3）**已经彻底不收首尾帧**
//	  （2026-10-28 上游取消 firstFrameAssetId/lastFrameAssetId），带首尾帧的请求
//	  打到源模型必然 400；而规则一的目标（漫屋）**也不支持首尾帧**。
//	  所以「带首尾帧」的请求只有 wan3 直连能跑，这个约束优先级高于时长/比例
//	  的成本优化 —— 否则 30 秒的首尾帧请求会被规则一改派到漫屋，直接失败。
//
// ⚠️ 规则二/三/五的字段**全部 omitempty 且可为空**：旧的生产 option JSON 里没有它们，
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
	// TriggerSeconds 触发阈值（秒）。**请求时长大于该值即路由**，规则一用。
	//
	// 2026-11-08 起语义由「等于」改为「大于」：豆包按次成本固定（¥3.50/次），
	// 源链路按秒计价（¥0.20/秒），超过 20 秒以后源链路已经比豆包更贵，
	// 没必要非等到 30 秒整才改派。2026-11-09 起命中后的时长**原样下发**，
	// 不再吸附到豆包的旧四档（用户口径：传多少秒就多少秒）。
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

	// ─────────────────── 规则五：故障转移（failover）───────────────────
	//
	// 前三条规则的触发源都是**请求特征**（时长 / 比例 / 首尾帧），
	// 规则五的触发源完全不同：**源链路本身不可用**（额度用完 / 渠道被关）。
	//
	// 为什么必须独立成一条规则、不能塞进前三条：
	//  1. 触发条件不是「请求长什么样」，而是「渠道状态如何」，两者没有共同表达；
	//  2. 源模型的口径相反 —— 前三条是「促销族模型 → 别的模型」，
	//     规则五是「**已经是别的模型的对外名** → 备胎模型」；
	//     seedance-2-5 既是规则一的**目标**，又是规则五的**源**，
	//     混用同一份 source_models 会让「源 == 目标」的自循环风险出现；
	//  3. 规则五的可用性判定要查**渠道状态**，前三条不查。
	//
	// FailoverSourceModel 是「会触发故障转移的源模型名」（如 seedance-2-5）。
	//
	// ⚠️ **刻意与 SourceModel / SourceModels 分开存**：那两份是前三条规则的源，
	// 规则一的 target_model 恰好就是 seedance-2-5 —— 若把规则五的源写进
	// source_models，中间件会先命中规则一、又把请求改派成同一个模型名，
	// 形成「改派到自己」的循环。这里用独立字段，从结构上杜绝。
	FailoverSourceModel string `json:"failover_source_model,omitempty"`

	// FailoverTargetModel 是故障转移的目标模型名（备胎，如 wan3.0-班车）。空 = 规则五不启用。
	FailoverTargetModel string `json:"failover_target_model,omitempty"`

	// FailoverEnabled 规则五的总开关。默认 false（不改派）。
	//
	// 用三态指针还是 bool：这里用 bool 即可，理由与 SecondsRuleEnabled 不同 ——
	// 规则五是**全新**的规则，旧的生产 option JSON 里根本没有这两个字段，
	// 解析后是零值 false = 不启用，语义正好是「没配过就不生效」。
	// 没有「升级当天静默改变行为」的风险。
	FailoverEnabled bool `json:"failover_enabled,omitempty"`

	// FailoverUnavailableUntil 是「源链路被判定为不可用」的截止时间（Unix 秒）。
	// 0 = 当前没有被判定为不可用。
	//
	// 为什么需要它：额度用完是**持续状态**，不是一次性的失败。渠道被 AutoBan
	// 下线时，渠道状态本身就是持久事实；但「额度用完但渠道没被下线」这种
	// 情况（上游只在任务终态回一句「今日额度已用完」）只能靠探针感知 ——
	// 而探针是**事件驱动**的，只在任务失败的那一刻才跑一次。
	// 没有这个时间戳，下一个请求进来时中间件根本不知道源链路已经坏了，
	// 会照常把请求送过去再失败一次（客户白等一轮 + 白烧一次预扣）。
	//
	// 判定不可用的时间点决定生效区间：探针在 T 时刻发现额度用完，
	// 就置成「T 起不可用」，直到 T+窗口期结束才恢复尝试。
	FailoverUnavailableUntil int64 `json:"failover_unavailable_until,omitempty"`

	// FailoverUnavailableReason 是「源链路不可用」的原因文案，给前端展示。
	FailoverUnavailableReason string `json:"failover_unavailable_reason,omitempty"`

	// FailoverUnavailableSource 是「谁判定源链路不可用」："auto"（额度探针）
	// 或 "manual"（管理员在后台标记）。空 = 未标记。
	//
	// 为什么必须区分：自动标记会在源链路成功出片后**自动解除**，
	// 人工标记不会（人工可能知道上游今天一直挂着）。前端如果只显示
	// 「不可用」，运营会以为关掉开关就等于源链路恢复了 —— 那是两种
	// 完全不同的语义（自动标记的开关是「要不要启用故障转移」，
	// 与「源链路好不好使」是两回事）。
	FailoverUnavailableSource string `json:"failover_unavailable_source,omitempty"`

	// SecondsRulePausedUntil 规则一的暂停截止时间（Unix 秒）。0 = 未暂停。
	//
	// 为什么需要「到期时间」而不是一个布尔开关：目标链路（豆包官网）的额度是
	// **按天**计的，用完当天不可用、到**北京时间凌晨 2 点**自动恢复（上游的真实
	// 重置点，不是午夜）。用布尔开关就得有人记得回来打开 —— 漏一次就会长期
	// 悄悄跑在另一条链路上，且没有任何报错。
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

	// SourceOverrides 是**按源模型分别配置**的目标模型覆盖（2026-11-03 起）。
	//
	// 为什么需要：source_models 是一个列表，三条规则**共用同一套目标模型**。
	// 但促销族的两个成员需要不同的兜底链路：
	//
	//	seedance2.5-特惠（母本，按秒 ¥0.20/秒）→ 30 秒走豆包 seedance-2-5（成本 ≈ ¥0）
	//	seedance2.5-活动（按次 ¥4.5/次）       → 30 秒走心笔 wan3.0-720p-按次
	//
	// 两者**不能共用目标模型**：母本按秒计价，活动版按次计价，
	// 目标链路的计费口径与能力都不同（心笔是固定 30 秒按次，
	// 豆包是按次且时长原样透传）。
	//
	// 只改 option 做不到这一点 —— 改了必然同时改到另一个成员。
	// 所以这里加一层「按源模型覆盖」：未覆盖的源模型继续用公共字段，
	// 行为与改造前逐字节一致。
	SourceOverrides map[string]SourceOverride `json:"source_overrides,omitempty"`

	// UpdatedAt 最近一次修改的 Unix 时间戳，0 表示从未改过。
	UpdatedAt int64 `json:"updated_at"`
}

// SourceOverride 是「按源模型分别配置」的目标模型覆盖。
//
// 语义：**只覆盖非空字段**。空字段 = 沿用 Policy 上的公共字段。
// 这样配一条「只改时长目标」的覆盖不会把比例/首尾帧目标清空。
type SourceOverride struct {
	// TargetModel 覆盖规则一（时长）的目标模型。
	TargetModel string `json:"target_model,omitempty"`
	// RatioTargetModel 覆盖规则二（比例）的目标模型。
	RatioTargetModel string `json:"ratio_target_model,omitempty"`
	// FramesTargetModel 覆盖规则三（首尾帧）的目标模型。
	FramesTargetModel string `json:"frames_target_model,omitempty"`

	// SecondsRuleEnabled 是该源模型专属的「按时长改派」开关（2026-11-03 起）。
	//
	// 语义：
	//   - nil（配置里没写这个字段）= **默认开启**，行为与加这个字段之前逐字节一致；
	//   - 显式 false = 该源模型的规则一不改派，请求留在自己的常驻链路；
	//   - 显式 true = 明确开启（用于把别人关掉的开关改回来）。
	//
	// 为什么用指针而不是 bool：必须是三态。用 bool 会让「没配」和「配了 false」
	// 无法区分 —— 旧的生产 option 里没有这个字段，解析后是 false，
	// 若把它当作「关掉」，升级当天就会静默停掉全站 30 秒改派，而且没有任何报错。
	//
	// 为什么要按源模型分：促销族两个成员的经营口径不同（母本按秒、活动版按次），
	// 母本改派豆包是纯赚差价，活动版改派豆包会贴钱。老板需要能**只停一个**，
	// 而全局的 SecondsRulePausedUntil 一停就是两个一起停。
	SecondsRuleEnabled *bool `json:"seconds_rule_enabled,omitempty"`

	// RatioRuleEnabled 是该源模型专属的「按比例改派」开关（2026-11-11 起）。
	//
	// 语义与 SecondsRuleEnabled 完全一致（三态指针）：
	//   - nil（配置里没写这个字段）= **默认开启**，行为与加这个字段之前逐字节一致；
	//   - 显式 false = 该源模型的规则二不改派，请求留在自己的常驻链路；
	//   - 显式 true = 明确开启（用于把别人关掉的开关改回来）。
	//
	// 为什么需要它：老板 2026-11-11 要求「只取消 seedance2.5-特惠 → 心笔 的
	// 9:16 改派，别的模型路由到心笔的不变」。公共 ratio_target_model 一改就是
	// 所有源模型一起改，source_overrides[ratio_target_model] 为空又会回落公共值
	// （现有字段做不到按源关闭）—— 只有独立开关才能「只停一个、默认全开」。
	RatioRuleEnabled *bool `json:"ratio_rule_enabled,omitempty"`
}

// overrideFor 返回「这个源模型的专属覆盖」以及是否存在。
//
// ⚠️ 键要 Trim 后比对：生产上真出现过带空格的模型名（model/log.go 里记着
// 同一个事故），配置侧和请求侧都要 Trim —— 否则「配了却不生效」且没有任何报错。
func (p Policy) overrideFor(sourceModel string) (SourceOverride, bool) {
	trimmed := trimModelName(sourceModel)
	if trimmed == "" {
		return SourceOverride{}, false
	}
	if override, ok := p.SourceOverrides[trimmed]; ok {
		return override, true
	}
	for key, override := range p.SourceOverrides {
		if trimModelName(key) == trimmed {
			return override, true
		}
	}
	return SourceOverride{}, false
}

// secondsRuleEnabledFor 判断该源模型的「按时长改派」是否被单独关掉。
//
// 默认（没配覆盖、或覆盖里没写这个字段）= 开启。
func (p Policy) secondsRuleEnabledFor(sourceModel string) bool {
	override, ok := p.overrideFor(sourceModel)
	if !ok || override.SecondsRuleEnabled == nil {
		return true
	}
	return *override.SecondsRuleEnabled
}

// ratioRuleEnabledFor 判断该源模型的「按比例改派」是否被单独关掉。
//
// 默认（没配覆盖、或覆盖里没写这个字段）= 开启，与 secondsRuleEnabledFor 同口径。
func (p Policy) ratioRuleEnabledFor(sourceModel string) bool {
	override, ok := p.overrideFor(sourceModel)
	if !ok || override.RatioRuleEnabled == nil {
		return true
	}
	return *override.RatioRuleEnabled
}

// targetFor 返回「这个源模型实际生效的目标模型名」。
//
// 三级回落：源模型专属覆盖 → 公共字段 → 空串。
// 为什么要三级：源模型专属覆盖只覆盖它配了的字段（比如只配了 TargetModel），
// 没配的字段仍要用公共值 —— 否则「只给活动版换时长兜底」会把比例规则
// 一起关掉，那是配置外的副作用。
//
// ⚠️ 覆盖表的键也要 Trim：配置里的 " seedance2.5-活动 " 与请求里的
// "seedance2.5-活动" 必须匹配得上 —— 生产上真出现过带空格的模型名
// （model/log.go 里记着同一个事故），配置侧和请求侧都要 Trim。
func (p Policy) targetFor(sourceModel, field string) string {
	trimmed := trimModelName(sourceModel)
	if trimmed == "" {
		return ""
	}
	override, ok := p.overrideFor(trimmed)
	if !ok {
		return ""
	}
	switch field {
	case fieldTarget:
		return override.TargetModel
	case fieldRatio:
		return override.RatioTargetModel
	case fieldFrames:
		return override.FramesTargetModel
	}
	return ""
}

// 三条规则的字段名。用常量而不是裸字符串，避免调用点写错字段名后
// 静默取到空值（那会让规则「配了却不生效」，且没有任何报错）。
const (
	fieldTarget = "target_model"
	fieldRatio  = "ratio_target_model"
	fieldFrames = "frames_target_model"
)

// trimModelName 去掉首尾空格，与 IsSourceModel 的口径保持一致。
func trimModelName(name string) string {
	return strings.TrimSpace(name)
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

// SourceSwitchState 是「按源模型的按时长改派开关」快照，给管理后台展示用。
//
// 为什么要有这个结构而不是直接吐 map：运营在后台看到的是「哪一个模型被关了」，
// 不是一堆布尔值。带上 target_model 才能看出「关了之后这些请求会留在哪条链路」。
type SourceSwitchState struct {
	SourceModel        string `json:"source_model"`
	SecondsRuleEnabled bool   `json:"seconds_rule_enabled"`
	TargetModel        string `json:"target_model"`
}

// SourceSwitchStates 返回全部源模型的改派开关快照（按配置顺序去重）。
func SourceSwitchStates() []SourceSwitchState {
	mu.RLock()
	defer mu.RUnlock()
	sources := current.allSourceModels()
	out := make([]SourceSwitchState, 0, len(sources))
	for _, source := range sources {
		out = append(out, SourceSwitchState{
			SourceModel:        source,
			SecondsRuleEnabled: current.secondsRuleEnabledFor(source),
			TargetModel:        current.TargetModelFor(source),
		})
	}
	return out
}

// WithSourceSecondsRuleEnabled 返回「把某个源模型的按时长改派开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由调用方用 model.UpdateOption(OptionKey, json) 完成，
// 与 WithSecondsRulePausedUntil 同一套做法。
//
// ⚠️ 只接受**已配置的源模型**：拼错一个模型名就静默什么都不做，
// 比报错更难发现（运营以为关了，实际没有）。
func WithSourceSecondsRuleEnabled(sourceModel string, enabled bool) (Policy, error) {
	trimmed := trimModelName(sourceModel)
	if trimmed == "" {
		return Policy{}, errors.New("source model is required")
	}
	mu.RLock()
	known := current.IsSourceModel(trimmed)
	var out Policy
	if known {
		out = clonePolicy(current)
	}
	mu.RUnlock()
	if !known {
		return Policy{}, fmt.Errorf("unknown source model: %s", trimmed)
	}

	// 沿用配置里已有的键（可能带空格），没有就新建。
	key := trimmed
	for existing := range out.SourceOverrides {
		if trimModelName(existing) == trimmed {
			key = existing
			break
		}
	}
	if out.SourceOverrides == nil {
		out.SourceOverrides = make(map[string]SourceOverride, 1)
	}
	override := out.SourceOverrides[key]
	value := enabled
	override.SecondsRuleEnabled = &value
	out.SourceOverrides[key] = override
	out.UpdatedAt = nowUnix()
	return out, nil
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
// 「目标模型名」既可以是公共字段，也可以由 source_overrides 按源模型单独配置 ——
// 只要**至少有一个源模型**能配全某条规则，整条策略就算配全。
//
// 一条都没配全就当作没开（Enabled 置 false）—— 宁可不开也不要半开，
// 半开的状态（比如 target_model 为空）会把请求改写成一个空模型名，线上直接全挂。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Policy
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	if parsed.Enabled {
		if !parsed.anyRuleReady() {
			parsed.Enabled = false
		}
	}
	mu.Lock()
	defer mu.Unlock()
	current = parsed
	return nil
}

// anyRuleReady 判断「至少有一条规则对至少一个源模型配全」。
//
// 为什么要按源模型逐个判：策略改成按源模型分别配置后，
// 公共字段可能全空（每个源模型各有各的目标），
// 只看公共字段会误判成「一条都没配全」而把路由整个关掉。
func (p Policy) anyRuleReady() bool {
	if failoverRuleReady(p) {
		return true
	}
	for _, source := range p.allSourceModels() {
		if secondsRuleConfiguredFor(p, source) || ratioRuleConfiguredFor(p, source) || framesRuleReadyFor(p, source) {
			return true
		}
	}
	return false
}

// allSourceModels 返回策略涉及的全部源模型名（单值 + 多值，去重去空）。
func (p Policy) allSourceModels() []string {
	out := make([]string, 0, len(p.SourceModels)+1)
	seen := make(map[string]bool, len(p.SourceModels)+1)
	appendName := func(name string) {
		trimmed := trimModelName(name)
		if trimmed == "" || seen[trimmed] {
			return
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	appendName(p.SourceModel)
	for _, name := range p.SourceModels {
		appendName(name)
	}
	return out
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
//
// 也要考虑按源模型单独配置的规则：公共字段全空、每个源模型各有目标时，
// 只看公共字段会误判成「没有规则生效」，把中间件整个跳过。
func IsAnyRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	if secondsRuleActive(current, nowUnix()) ||
		ratioRuleReady(current) ||
		framesRuleReady(current) ||
		failoverRuleReady(current) {
		return true
	}
	for _, source := range current.allSourceModels() {
		if secondsRuleActiveFor(current, source, nowUnix()) ||
			ratioRuleReadyFor(current, source) ||
			framesRuleReadyFor(current, source) {
			return true
		}
	}
	return false
}

// ─────────────────────── 规则五：故障转移 ───────────────────────

// failoverRuleReady 判断规则五是否配全（可以生效）。
//
// 必要条件三个：总开关开着、源模型名非空、目标模型名非空。
// 少任何一个都不生效 —— 与其他规则的「配全才生效」口径一致，
// 避免半开状态下把请求改派成一个空模型名。
func failoverRuleReady(p Policy) bool {
	return p.FailoverEnabled &&
		trimModelName(p.FailoverSourceModel) != "" &&
		trimModelName(p.FailoverTargetModel) != ""
}

// IsFailoverRouteEnabled 判断规则五（故障转移）当前是否生效。
func IsFailoverRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return failoverRuleReady(current)
}

// IsFailoverSource 判断模型名是不是规则五的源模型。
func IsFailoverSource(model string) bool {
	mu.RLock()
	defer mu.RUnlock()
	target := trimModelName(model)
	if target == "" {
		return false
	}
	return trimModelName(current.FailoverSourceModel) == target
}

// FailoverTargetModel 返回规则五的目标模型名（备胎）。空 = 未配置。
func FailoverTargetModel() string {
	mu.RLock()
	defer mu.RUnlock()
	return trimModelName(current.FailoverTargetModel)
}

// FailoverSourceModel 返回规则五的源模型名。空 = 未配置。
func FailoverSourceModel() string {
	mu.RLock()
	defer mu.RUnlock()
	return trimModelName(current.FailoverSourceModel)
}

// FailoverUnavailable 判断「源链路当前是否被判定为不可用」。
//
// 两种不可用来源，判定方式不同：
//
//	动态（探针）：FailoverUnavailableUntil > now —— 上游明确回报「额度用完」，
//	              探针置了这个时间戳。额度按天重置，所以到期自动失效。
//	静态（渠道）：源模型在当前分组下**没有启用中的渠道** —— 渠道被 AutoBan
//	              下线或人工关闭。这不需要状态位，查渠道缓存即可，
//	              而且渠道一恢复就自动恢复，不需要任何状态维护。
//
// 中间件要把这两种都算上。只认其中一种的话：渠道真被关了但探针没跑过时，
// 请求会继续撞坏渠道；额度用完但渠道没被下线时，请求会继续白烧预扣。
func FailoverUnavailable(now int64) bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.FailoverUnavailableUntil > now
}

// FailoverReason 返回「源链路不可用」的原因文案（给前端展示）。没有则空串。
func FailoverReason() string {
	mu.RLock()
	defer mu.RUnlock()
	return current.FailoverUnavailableReason
}

// FailoverUnavailableUntil 返回「源链路被判为不可用」的截止时间（Unix 秒）。0 = 未标记。
func FailoverUnavailableUntil() int64 {
	mu.RLock()
	defer mu.RUnlock()
	return current.FailoverUnavailableUntil
}

// FailoverUnavailableSource 返回「谁判定源链路不可用」："auto" / "manual" / ""。
func FailoverUnavailableSource() string {
	mu.RLock()
	defer mu.RUnlock()
	return current.FailoverUnavailableSource
}

// WithFailoverUnavailable 返回「把源链路标记为不可用直到 until」的策略副本（纯函数，不落库）。
//
// until 传 0 表示取消标记（恢复）。
func WithFailoverUnavailable(until int64, reason string) Policy {
	mu.RLock()
	defer mu.RUnlock()
	out := clonePolicy(current)
	out.FailoverUnavailableUntil = until
	out.FailoverUnavailableReason = reason
	if until > 0 {
		out.FailoverUnavailableSource = PauseSourceAuto
		out.UpdatedAt = nowUnix()
	} else {
		out.FailoverUnavailableSource = ""
	}
	return out
}

// WithManualFailoverUnavailable 返回「人工把源链路标记为不可用直到 until」的策略副本。
//
// 与 WithFailoverUnavailable 的区别只有「来源」：人工标记**不会**被
// 额度探针的成功事件自动解除（探针只解除自己写的 auto 标记）。
//
// 为什么需要人工标记：上游有时不回报「额度用完」这句话，只是直接失败
// （或者失败文案是别的样子，探针认不出来）。这时只能由人点一下按钮
// 把源链路标成不可用，让请求改派到备胎。
func WithManualFailoverUnavailable(until int64, reason string) Policy {
	mu.RLock()
	defer mu.RUnlock()
	out := clonePolicy(current)
	out.FailoverUnavailableUntil = until
	out.FailoverUnavailableReason = reason
	if until > 0 {
		out.FailoverUnavailableSource = PauseSourceManual
		out.UpdatedAt = nowUnix()
	} else {
		out.FailoverUnavailableSource = ""
	}
	return out
}

// WithFailoverEnabled 返回「规则五开关设成 enabled」的策略副本（纯函数，不落库）。
func WithFailoverEnabled(enabled bool) (Policy, error) {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	if enabled {
		if trimModelName(out.FailoverSourceModel) == "" || trimModelName(out.FailoverTargetModel) == "" {
			return Policy{}, errors.New("failover source model and target model are required")
		}
	}
	out.FailoverEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out, nil
}

// WithFailoverModels 返回「规则五的源/目标模型设成 source/target」的策略副本。
//
// 为什么要单独一个函数：规则五的源与目标是**一对**（谁挂了改派给谁），
// 不像规则一~四那样源与目标分属不同维度。让后台接口分别提交两个字段
// 会出现「只配了源、没配目标」的半开状态 —— 那种状态在中间件里表现为
// 「配了却不生效」，且没有任何报错。
//
// 校验：
//   - 两个模型名都必须非空（Trim 后）；
//   - 两者不能相同 —— 相同就是「改派到自己」，请求会在链路里原地打转。
//
// ⚠️ 这里**不**改 FailoverEnabled：开关是独立的经营决策，配置完模型名
// 不代表运营已经决定启用。是否启用由 WithFailoverEnabled 单独控制。
func WithFailoverModels(sourceModel, targetModel string) (Policy, error) {
	source := trimModelName(sourceModel)
	target := trimModelName(targetModel)
	if source == "" {
		return Policy{}, errors.New("failover source model is required")
	}
	if target == "" {
		return Policy{}, errors.New("failover target model is required")
	}
	if source == target {
		return Policy{}, errors.New("failover source and target model must differ")
	}
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.FailoverSourceModel = source
	out.FailoverTargetModel = target
	out.UpdatedAt = nowUnix()
	return out, nil
}

// FailoverConfigured 判断规则五的源与目标是否都配好了（不要求开关开着）。
//
// 用途：后台接口要区分「还没配过规则五」与「配了但开关关着」——
// 前者要提示先配模型名，后者只提示可以打开开关。两者都返回 false 的话，
// 运营永远不知道自己缺哪一步。
func FailoverConfigured() bool {
	mu.RLock()
	defer mu.RUnlock()
	return trimModelName(current.FailoverSourceModel) != "" &&
		trimModelName(current.FailoverTargetModel) != ""
}

// IsSecondsRuleEnabledFor 判断「规则一（时长）对某个源模型是否生效」。
//
// 与 IsEnabled 的区别：目标模型名按**源模型**取，并且叠加该源模型的专属开关
// （source_overrides[模型].seconds_rule_enabled）。所以规则一对两个源模型
// 可以一个开、一个关 —— 这正是「只停活动版改派豆包、母本照旧」的实现方式。
//
// 全局暂停（SecondsRulePausedUntil）仍然优先：豆包真没额度时，
// 单独开启某个源模型并不能让它跑起来，所以暂停期内一律不生效。
func IsSecondsRuleEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return secondsRuleActiveFor(current, sourceModel, nowUnix())
}

// IsRatioRouteEnabledFor 判断「规则二（比例）对某个源模型是否生效」。
// 目标模型名按源模型取。
func IsRatioRouteEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return ratioRuleReadyFor(current, sourceModel)
}

// IsFramesRouteEnabledFor 判断「规则三（首尾帧）对某个源模型是否生效」。
// 目标模型名按源模型取。
func IsFramesRouteEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return framesRuleReadyFor(current, sourceModel)
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

// secondsRuleReadyFor 判断「规则一对某个具体源模型是否配全」。
//
// 与 secondsRuleReady 的区别：目标模型名按**源模型**取
// （公共值或该源模型的专属覆盖）。
//
// 为什么需要单独一份：活动版专属覆盖了 TargetModel 时，
// 公共 TargetModel 可能仍指向豆包 —— 那时母本走豆包、活动版走心笔，
// 两条规则各自配全，但共用一条校验口径会误判「都没配全」而把路由关掉。
// secondsRuleReadyFor 判断「规则一对某个具体源模型是否配全且未被单独关掉」。
//
// 与 secondsRuleConfiguredFor 的区别：**这里叠加了该源模型的专属开关**。
// 开关是「要不要改派」的经营决策，不影响「这条规则配没配全」——
// 后者决定整份策略算不算数（见 anyRuleReady）。
func secondsRuleReadyFor(p Policy, sourceModel string) bool {
	return secondsRuleConfiguredFor(p, sourceModel) && p.secondsRuleEnabledFor(sourceModel)
}

// secondsRuleConfiguredFor 判断「规则一对某个源模型是否配全」（不看开关）。
//
// 为什么需要单独一份：anyRuleReady 用它判断「整份策略配全了没有」。
// 若那里也看开关，两个模型的改派都被关掉时会把 Enabled 判成 false ——
// 首尾帧规则会跟着一起停，带首尾帧的请求将 100% 400，而且没有任何报错。
func secondsRuleConfiguredFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.TargetModelFor(sourceModel) != "" &&
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
	target := trimModelName(model)
	if target == "" {
		return false
	}
	if trimModelName(p.SourceModel) == target {
		return true
	}
	for _, name := range p.SourceModels {
		if trimModelName(name) == target {
			return true
		}
	}
	return false
}

// ExceedsTriggerSeconds 判断请求时长是否超过规则一的改派阈值。
//
// 语义是**严格大于**：阈值配 20 时，21–30 秒改派、20 秒及以下留在源链路。
// 与「等于」相比的好处：豆包按次成本固定，源链路按秒计价，时长越长源链路越贵，
// 没必要为了对齐 30 秒整档而让 21–29 秒的请求继续跑在更贵的源链路上。
func (p Policy) ExceedsTriggerSeconds(seconds int) bool {
	return p.TriggerSeconds > 0 && seconds > p.TriggerSeconds
}

// TargetModelFor 返回「规则一（时长）对某个源模型生效的目标模型名」。
//
// 优先级：源模型专属覆盖 > 公共 TargetModel。
// 为什么要按源模型区分：促销族的两个成员必须走不同的兜底链路
// （母本 → 豆包按次，活动版 → 心笔按次），只改公共字段会同时改到两个。
//
// ⚠️ 这里的返回值**只对源模型有效**：非源模型返回空串，
// 调用方不应拿它去改派请求。
func (p Policy) TargetModelFor(sourceModel string) string {
	if override := p.targetFor(sourceModel, fieldTarget); override != "" {
		return override
	}
	return p.TargetModel
}

// RatioTargetModelFor 返回「规则二（比例）对某个源模型生效的目标模型名」。
// 优先级同 TargetModelFor。
func (p Policy) RatioTargetModelFor(sourceModel string) string {
	if override := p.targetFor(sourceModel, fieldRatio); override != "" {
		return override
	}
	return p.RatioTargetModel
}

// FramesTargetModelFor 返回「规则三（首尾帧）对某个源模型生效的目标模型名」。
// 优先级同 TargetModelFor。
func (p Policy) FramesTargetModelFor(sourceModel string) string {
	if override := p.targetFor(sourceModel, fieldFrames); override != "" {
		return override
	}
	return p.FramesTargetModel
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

// secondsRuleActiveFor = 对某个源模型配全了 且 不在暂停期。
func secondsRuleActiveFor(p Policy, sourceModel string, now int64) bool {
	return secondsRuleReadyFor(p, sourceModel) && !secondsRulePaused(p, now)
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

// ratioRuleReadyFor 判断「规则二对某个具体源模型是否配全**且未被单独关掉**」。
// 目标模型名按源模型取（公共值或该源模型的专属覆盖）。
//
// 与 ratioRuleConfiguredFor 的分工完全对齐 secondsRuleReadyFor / secondsRuleConfiguredFor：
// **这里叠加该源模型的专属开关**（source_overrides[模型].ratio_rule_enabled），
// 开关决定「要不要改派」；「配没配全」由 configured 那一份供 anyRuleReady 使用。
func ratioRuleReadyFor(p Policy, sourceModel string) bool {
	return ratioRuleConfiguredFor(p, sourceModel) && p.ratioRuleEnabledFor(sourceModel)
}

// ratioRuleConfiguredFor 判断「规则二对某个源模型是否配全」（不看开关）。
//
// 为什么需要单独一份：anyRuleReady 用它判断「整份策略配全了没有」。
// 若那里也看开关，两个源模型的比例改派都被关掉时会把 Enabled 判成 false ——
// 配置的语义只是「先别改派」，不是「这份配置作废」；
// 将来重新打开开关时路由已经死了，而且没有任何报错。
func ratioRuleConfiguredFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.RatioTargetModelFor(sourceModel) != "" &&
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

// framesRuleReadyFor 判断「规则三对某个具体源模型是否配全」。
// 目标模型名按源模型取（公共值或该源模型的专属覆盖）。
func framesRuleReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.FramesTargetModelFor(sourceModel) != ""
}

func clonePolicy(p Policy) Policy {
	out := p
	// SourceOverrides 是 map：浅拷贝会让 Get() 返回的副本与全局状态共享底层 map，
	// 调用方改一个键就悄悄改到了全局配置。必须深拷贝。
	if p.SourceOverrides != nil {
		out.SourceOverrides = make(map[string]SourceOverride, len(p.SourceOverrides))
		for key, value := range p.SourceOverrides {
			// SecondsRuleEnabled / RatioRuleEnabled 是指针：浅拷贝会让副本与全局状态
			// 共享同一个 bool，调用方一改就悄悄改到了全局配置（与 map 浅拷贝同一类问题）。
			if value.SecondsRuleEnabled != nil {
				enabled := *value.SecondsRuleEnabled
				value.SecondsRuleEnabled = &enabled
			}
			if value.RatioRuleEnabled != nil {
				enabled := *value.RatioRuleEnabled
				value.RatioRuleEnabled = &enabled
			}
			out.SourceOverrides[key] = value
		}
	}
	return out
}
