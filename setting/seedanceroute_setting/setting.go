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

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "Seedance25TeHuiRoute30s"

// Policy 是条件路由策略。
type Policy struct {
	// Enabled 总开关。false = 完全不路由，全部走源模型（默认，行为不变）。
	Enabled bool `json:"enabled"`
	// SourceModel 源模型名（对外名，客户请求里写的那个）。
	SourceModel string `json:"source_model"`
	// TargetModel 目标模型名（对外名）。
	TargetModel string `json:"target_model"`
	// TriggerSeconds 触发时长（秒）。**只有等于该值才路由**。
	TriggerSeconds int `json:"trigger_seconds"`
	// UpdatedAt 最近一次修改的 Unix 时间戳，0 表示从未改过。
	UpdatedAt int64 `json:"updated_at"`
}

// defaultPolicy 与「不做这个路由」等价：开关关、模型名为空。
//
// ⚠️ 默认值必须让路由**完全不生效**：没有这个 option 时（或者 option 被删了），
// Get() 返回这里的内容，中间件直接放行 —— 行为与改造前逐字节一致。
func defaultPolicy() Policy {
	return Policy{
		Enabled:        false,
		SourceModel:    "",
		TargetModel:    "",
		TriggerSeconds: 0,
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

// UpdateFromJSONString 用管理员提交的 JSON 覆盖策略。
//
// 开启路由的**必要条件**：源模型名、目标模型名、触发时长三者都要有值。
// 缺任何一项就当作没开（Enabled 置 false）—— 宁可不开也不要半开，
// 半开的状态（比如 target_model 为空）会把请求改写成一个空模型名，线上直接全挂。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Policy
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	if parsed.Enabled {
		if parsed.SourceModel == "" || parsed.TargetModel == "" || parsed.TriggerSeconds <= 0 {
			parsed.Enabled = false
		}
	}
	mu.Lock()
	defer mu.Unlock()
	current = parsed
	return nil
}

// IsEnabled 判断当前是否需要做条件路由。
// 三个必要条件缺一不可 —— 与 UpdateFromJSONString 的校验口径保持一致，
// 防止有人绕过 option 分发直接改内存状态。
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return current.Enabled &&
		current.SourceModel != "" &&
		current.TargetModel != "" &&
		current.TriggerSeconds > 0
}

func clonePolicy(p Policy) Policy {
	out := p
	return out
}
