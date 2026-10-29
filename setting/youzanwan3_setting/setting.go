// Package youzanwan3_setting 保存「有赞 Wan3 上游动态调价」的策略与最近一次同步结果。
//
// 为什么单独一个包而不是写进 ratio_setting：这份配置描述的是「怎么根据上游成本
// 算售价」这件事本身（加价规则、变动阈值、档位倍率），不是通用的倍率表；
// 而算出来的最终基准价仍然写回 ratio_setting 的 ModelPrice，计费链不用改。
//
// 数据流：
//
//	上游 GET /v1/models（成本 ⚡/秒）
//	  → controller.SyncYouzanWan3Pricing 按 Policy.Markup 算售价
//	  → 写 ModelPrice（基准档，USD/秒）+ 本包的 Ratios（各档相对倍率）
//	  → youzanwan3.EstimateBilling 读 Ratios 得到 size 倍率
package youzanwan3_setting

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "YouzanWan3Pricing"

// Policy 是自动调价策略。
type Policy struct {
	// Markup 每档加价（人民币/秒）。键是上游模型 ID，值是「档位 → 加价」。
	// 档位用 "*" 表示该模型所有档位统一加价。
	// 例：{"wan3.0-video-prime": {"720P": 0.05, "1080P": 0.10}}
	Markup map[string]map[string]float64 `json:"markup"`
	// MaxChangeRatio 单次变动幅度上限（相对当前售价）。超过则只告警不落库。
	// 0 表示不限制 —— 不建议，上游偶发错价会直接变成我们的售价。
	MaxChangeRatio float64 `json:"max_change_ratio"`
	// AutoApply 是否自动落库。false 时同步任务只记录差异、不写价。
	AutoApply bool `json:"auto_apply"`
	// Ratios 是最近一次同步算出的档位倍率（相对基准档，基准档恒为 1.0）。
	// 由同步任务写入，管理员改 markup 时不必填。
	Ratios map[string]map[string]float64 `json:"ratios"`
	// UpdatedAt 最近一次成功同步的 Unix 时间戳，0 表示从未同步过。
	UpdatedAt int64 `json:"updated_at"`
}

// defaultPolicy 与代码里原来的常量保持一致，保证「没有这个 option 时行为不变」。
func defaultPolicy() Policy {
	return Policy{
		Markup: map[string]map[string]float64{
			"wan3.0-video":       {"*": 0.05},
			"wan3.0-video-prime": {"720P": 0.05, "1080P": 0.10},
		},
		MaxChangeRatio: 0.30,
		AutoApply:      true,
		Ratios: map[string]map[string]float64{
			"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
			"wan3.0-video-prime": {"720P": 1.0, "1080P": 1.5},
		},
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
// 只覆盖「策略」部分：如果提交内容里没带 ratios（管理员通常只改加价规则），
// 就保留当前同步出来的倍率，避免一次改配置把算好的档差清空。
func UpdateFromJSONString(jsonStr string) error {
	var parsed Policy
	if err := common.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if parsed.Markup == nil {
		parsed.Markup = current.Markup
	}
	if parsed.Ratios == nil {
		parsed.Ratios = current.Ratios
		parsed.UpdatedAt = current.UpdatedAt
	}
	current = parsed
	return nil
}

// SetRatios 记录一次成功同步的结果（同步任务专用）。
func SetRatios(ratios map[string]map[string]float64, updatedAt int64) {
	mu.Lock()
	defer mu.Unlock()
	if ratios != nil {
		current.Ratios = cloneNested(ratios)
	}
	current.UpdatedAt = updatedAt
}

// GetResolutionRatio 返回某模型某档位的相对倍率。
// 第二个返回值为 false 表示没有配置 —— 调用方应回退到代码里的默认常量。
func GetResolutionRatio(modelName, resolution string) (float64, bool) {
	mu.RLock()
	defer mu.RUnlock()
	byResolution, ok := current.Ratios[modelName]
	if !ok {
		return 0, false
	}
	ratio, ok := byResolution[resolution]
	if !ok || ratio <= 0 {
		return 0, false
	}
	return ratio, true
}

// MarkupFor 返回某模型某档位的加价（人民币/秒）。
// 精确档位优先，其次回退到 "*"。
func MarkupFor(modelName, resolution string) (float64, bool) {
	mu.RLock()
	defer mu.RUnlock()
	byResolution, ok := current.Markup[modelName]
	if !ok {
		return 0, false
	}
	if value, ok := byResolution[resolution]; ok {
		return value, true
	}
	if value, ok := byResolution["*"]; ok {
		return value, true
	}
	return 0, false
}

func clonePolicy(p Policy) Policy {
	p.Markup = cloneNested(p.Markup)
	p.Ratios = cloneNested(p.Ratios)
	return p
}

func cloneNested(src map[string]map[string]float64) map[string]map[string]float64 {
	if src == nil {
		return nil
	}
	dst := make(map[string]map[string]float64, len(src))
	for key, inner := range src {
		copied := make(map[string]float64, len(inner))
		for k, v := range inner {
			copied[k] = v
		}
		dst[key] = copied
	}
	return dst
}
