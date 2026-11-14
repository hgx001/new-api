// Package seedanceroute_setting 的「规则七：电商/条件 → CapCut 快速档」。
//
// 背景（2026-11-14 老板需求）：
//
//	seedance2.5-特惠 与 seedance2.5-活动 这两个对外模型名，
//	当请求的提示词是**电商类 / 产品推荐**类内容时，改派到
//	cp-seedance-2.5flash（CapCut 剪映国际版 Seedance 2.5 Flash 档）。
//
// 为什么单独一个包文件而不是塞进 setting.go：
//
//	这条规则与前六条**形态完全不同** —— 前六条的触发源都是「请求的客观参数」
//	（时长 / 比例 / 首尾帧 / 素材），而这条规则的触发源是**提示词的内容**。
//	内容判定需要一份**关键词表 + 匹配口径**，把它独立成一个文件，
//	是为了让「电商判定」这件事有一处唯一事实源，而不是散落在中间件里。
//
// 与其它规则的**共同约束**（全部继承本包既有纪律）：
//
//  1. 默认关闭：没配 ecommerce_enabled 就不生效（升级当天线上流量逐字节不变）；
//  2. 任何一步不确定就放弃路由：放行原请求走源模型，绝不挡客户；
//  3. 计费仍按源模型：改派只是内部调度，客户账单不变。
//
// ── 触发条件的组合语义 ──
//
//	命中 = 命中「内容条件」 **或** 命中「参数条件」
//
//	· 内容条件：提示词命中电商/产品推荐关键词（ecommerce_keywords）。
//	· 参数条件：勾选的条件项里**任意一项**命中即可（比例 9:16、时长 > 阈值）。
//
//	为什么是「或」而不是「且」：老板的原始诉求是「电商类 **或** 产品推荐的提示词」
//	就该走快档；后面追加的 9:16 / 时长条件是**补充入口**
//	（「另外 9:16 的也路由过去」）—— 两者是并列的进料口，不是串联的闸门。
//	用「且」会让「电商提示词但不是 9:16」的请求永远不命中，
//	等于把老板明确要的电商流量整个漏掉。
//
//	所以运营侧的语义是「勾选哪些入口」：**内容入口恒可用**（由关键词表决定），
//	参数入口按勾选启用。全部关掉参数入口时仍然只按内容命中。
package seedanceroute_setting

import (
	"errors"
	"strings"
)

// DefaultEcommerceTargetModel 是规则七的目标模型（CapCut Seedance 2.5 Flash 档）。
//
// 为什么选 2.5 Flash 而不是 2.0：2.5 Flash 时长 4–30 秒、参考图 ≤30 张，
// 能覆盖源链路（有赞 prime 2–30 秒）的大部分请求；2.0 系列时长上界只有 15 秒，
// 超过 15 秒的请求改派过去必被上游 400。
const DefaultEcommerceTargetModel = "cp-seedance-2.5flash"

// DefaultEcommerceTriggerSeconds 是规则七「时长条件」的默认阈值（秒）。
//
// 语义与规则一**相反**：这里是「时长**超过**阈值才命中」。
// 默认 20 秒 —— 与规则一（>20 秒改派豆包）同一阈值，
// 老板可以理解成「长视频优先走快档」。
const DefaultEcommerceTriggerSeconds = 20

// DefaultEcommerceRatioCondition 是规则七「比例条件」对应的比例。
//
// 为什么固定成 9:16 而不给运营开输入框：老板的原话是「另外 9:16 的也路由过去」，
// 竖屏是电商内容的主形态（短视频带货）。开放自由输入会让「配了 21:9 也路由」
// 这种误配置变成静默的行为变化 —— 比例写错时命中的是一批毫不相干的流量。
//
// 与规则一的 SecondsRuleRatioGate 用同一个默认值（9:16）但**不是同一份配置**：
// 两条规则是不同入口、可以各自独立勾选，共用一个字段会让「取消规则一的勾选」
// 顺手把规则七的勾选也取消掉。
const DefaultEcommerceRatioCondition = "9:16"

// DefaultEcommerceKeywords 是电商/产品推荐提示词的默认关键词表。
//
// ⚠️ 这是一份**默认表**，不是唯一事实源：运营可以在后台覆盖
// （Policy.EcommerceKeywords 为空时用这份默认值；非空时用运营配的）。
//
// 为什么关键词要有默认值：这条规则的价值全在关键词表上 ——
// 运营第一次打开开关时如果看到的是一张空表，只能自己去查「什么词算电商」，
// 那多半就忘了配了。有默认表 = 打开就有一份能用的起点，
// 运营再按自己的品类增删。
//
// 判定口径见 MatchesEcommercePrompt：大小写不敏感的**子串**匹配。
var DefaultEcommerceKeywords = []string{
	// 电商动作 / 场景
	"电商", "带货", "种草", "好物", "剁手", "开箱", "上新",
	// 产品推荐
	"产品推荐", "推荐理由", "卖点", "购买链接", "下单",
	"试用装", "口碑", "性价比", "值得买", "回购",
	// 营销文案形态
	"促销", "优惠", "折扣", "限时", "爆款", "热销",
	"广告", "宣传片", "营销视频", "详情页",

	// 英文电商词（2026-11-14 补）。
	//
	// 为什么要英文：CapCut 是**剪映国际版**，客户在这条链路上写的提示词
	// 本来就可能是英文（"Black Friday SALE promotion" 这种）。
	// 只认中文会让一批货真价实的电商内容留在源链路上，
	// 而运营在后台看到的是一张「已经配好了」的关键词表。
	//
	// ⚠️ 只放「出现即大概率是电商」的词，不放 buy / price / product 这类
	// 在任何产品说明里都常见的词 —— 子串匹配下它们会把大量普通提示词
	// 误判成电商内容，而误判的代价是改派到另一条链路（档位/时长能力都不同）。
	"e-commerce", "ecommerce", "product review", "unboxing",
	"shopping haul", "sponsored", "discount", "coupon",
	"promo code", "black friday", "cyber monday", "limited offer",
}

// EcommerceKeywords 返回「当前生效的电商关键词表」。
//
// 空表时回落 DefaultEcommerceKeywords：没有关键词表 = 判定不出电商类，
// 规则七就只剩参数条件（比例/时长）可用 —— 那正是运营只想要参数入口时的状态。
func (p Policy) EcommerceKeywords() []string {
	if len(p.EcommercePromptKeywords) > 0 {
		return p.EcommercePromptKeywords
	}
	return DefaultEcommerceKeywords
}

// EcommerceTargetModel 返回规则七生效时的目标模型名。
//
// 优先级：源模型专属覆盖 > 公共字段 > 默认值。
func (p Policy) EcommerceTargetModelFor(sourceModel string) string {
	if override := p.targetFor(sourceModel, fieldEcommerce); override != "" {
		return override
	}
	if trimmed := trimModelName(p.EcommerceTargetModel); trimmed != "" {
		return trimmed
	}
	return DefaultEcommerceTargetModel
}

// EcommerceTriggerSeconds 返回规则七「时长条件」的阈值（秒）。
//
// 0 = 没配 = 用默认值（DefaultEcommerceTriggerSeconds）。
func (p Policy) EcommerceTriggerSeconds() int {
	if p.EcommerceTriggerSecondsValue > 0 {
		return p.EcommerceTriggerSecondsValue
	}
	return DefaultEcommerceTriggerSeconds
}

// EcommerceRatioEnabled 判断「9:16 比例条件」是否勾选。
func (p Policy) EcommerceRatioEnabled() bool {
	return p.EcommerceRatioCondition
}

// EcommerceSecondsEnabled 判断「时长条件」是否勾选。
func (p Policy) EcommerceSecondsEnabled() bool {
	return p.EcommerceSecondsCondition
}

// EcommerceEnabledFor 判断「规则七对某个源模型是否生效」。
//
// 三层闸门，缺一不可：
//
//  1. 全局开关（ecommerce_enabled）；
//  2. 整份策略可用（Enabled + 配了源模型）；
//  3. 该源模型的专属开关（source_overrides[模型].ecommerce_rule_enabled，三态指针）。
//
// 第 3 层用三态指针的理由与另外三个按源模型开关完全一致：
// 必须区分「没配」（= 默认开启，行为与加这个字段之前逐字节一致）
// 与「显式 false」（= 该源模型不走规则七）。
// 用 bool 会让「没配」与「配了 false」无法区分 —— 升级当天就会静默停掉线上流量。
func EcommerceEnabledFor(sourceModel string) bool {
	mu.RLock()
	defer mu.RUnlock()
	return ecommerceReadyFor(current, sourceModel)
}

// ecommerceReadyFor 判断「规则七对某个源模型是否配全且未被单独关掉」。
func ecommerceReadyFor(p Policy, sourceModel string) bool {
	return p.Enabled &&
		p.hasSourceModel() &&
		p.EcommerceEnabled &&
		p.ecommerceRuleEnabledFor(sourceModel)
}

// ecommerceRuleEnabledFor 判断该源模型的规则七是否被单独关掉。默认 = 开启。
func (p Policy) ecommerceRuleEnabledFor(sourceModel string) bool {
	override, ok := p.overrideFor(sourceModel)
	if !ok || override.EcommerceRuleEnabled == nil {
		return true
	}
	return *override.EcommerceRuleEnabled
}

// IsEcommerceRouteEnabled 判断「规则七当前是否生效」（不分源模型）。
func IsEcommerceRouteEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Enabled && current.EcommerceEnabled && current.hasSourceModel()
}

// WithEcommerceEnabled 返回「把规则七总开关设成 enabled」的策略副本。
//
// 纯函数，**不落库** —— 落库由 controller 用 model.UpdateOption 完成，
// 与 WithShortYouzanEnabled / WithShortLinkEnabled 同一套做法。
func WithEcommerceEnabled(enabled bool) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.EcommerceEnabled = enabled
	out.UpdatedAt = nowUnix()
	return out
}

// WithEcommerceConditions 返回「把规则七的参数条件设成指定勾选状态」的策略副本。
//
// 语义（与 WithSecondsRuleRatioGate 同款后台勾选语义）：
//
//	ratioEnabled   = 勾上「9:16 比例」入口；
//	secondsEnabled = 勾上「时长超过阈值」入口；
//
// ⚠️ **只改这两个条件字段**，不动关键词表、不动总开关、不动其它经营配置 ——
// 这份策略里还压着暂停截止时间、按源模型覆盖、各规则目标模型、豆包链路开关状态，
// 勾一个条件把它们冲掉是没有任何报错的静默事故。
//
// 阈值非法（<= 0）时返回错误而不是写进去：一个永远匹配不上的阈值
// 会让所有超长请求都不改派，而后台勾选看起来仍是勾着的。
func WithEcommerceConditions(ratioEnabled, secondsEnabled bool, triggerSeconds int) (Policy, error) {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()

	if secondsEnabled && triggerSeconds <= 0 {
		return Policy{}, errors.New("trigger_seconds must be positive")
	}
	out.EcommerceRatioCondition = ratioEnabled
	out.EcommerceSecondsCondition = secondsEnabled
	if triggerSeconds > 0 {
		out.EcommerceTriggerSecondsValue = triggerSeconds
	}
	out.UpdatedAt = nowUnix()
	return out, nil
}

// WithEcommerceKeywords 返回「把电商关键词表设成 keywords」的策略副本。
//
// 空列表 = 回落 DefaultEcommerceKeywords（不是「关掉内容判定」）——
// 内容判定是这条规则的主入口，没有「只留参数条件」的表达方式；
// 运营想关掉内容入口请用总开关。
func WithEcommerceKeywords(keywords []string) Policy {
	mu.RLock()
	out := clonePolicy(current)
	mu.RUnlock()
	out.EcommercePromptKeywords = normalizeEcommerceKeywords(keywords)
	out.UpdatedAt = nowUnix()
	return out
}

// normalizeEcommerceKeywords 去空白、去重（保持原顺序）、丢空串。
//
// 去重不是为了好看：运营在后台反复增删时很容易留下重复项，
// 重复项不会改变判定结果，但会让「这份表有 12 个词」看起来像是 12 个不同口径。
func normalizeEcommerceKeywords(keywords []string) []string {
	out := make([]string, 0, len(keywords))
	seen := make(map[string]bool, len(keywords))
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		out = append(out, trimmed)
	}
	return out
}

// MatchesEcommercePrompt 判断提示词是否命中电商/产品推荐内容。
//
// 口径：
//
//	· **子串**匹配，不做分词：中文没有空格边界，分词器对
//	  「电商」「带货」这类词的效果远不如直接子串可靠，且引入分词依赖会让
//	  「关键词表」这件事从配置变成代码行为；
//	· **大小写不敏感**：英文词（sale / discount / unboxing）大小写混用很常见；
//	· **空表 = 不命中**：没有任何关键词可比对时返回 false，
//	  绝不因为「表是空的」就判定成「不是电商」而让规则七整体失效 ——
//	  空表时只保留参数条件入口。
func MatchesEcommercePrompt(prompt string, keywords []string) bool {
	text := strings.ToLower(strings.TrimSpace(prompt))
	if text == "" {
		return false
	}
	for _, keyword := range keywords {
		trimmed := strings.TrimSpace(keyword)
		if trimmed == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(trimmed)) {
			return true
		}
	}
	return false
}
