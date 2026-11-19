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
	"sort"
	"sync"

	"github.com/QuantumNous/new-api/common"
)

// OptionKey 是这份配置在 options 表里的键名。
const OptionKey = "YouzanWan3Pricing"

// Policy 是自动调价策略。
type Policy struct {
	// Markup 每档加价（人民币/秒）。键是**上游模型 ID**，值是「档位 → 加价」。
	// 档位用 "*" 表示该模型所有档位统一加价。
	// 例：{"wan3.0-video-prime": {"720P": 0.10, "1080P": 0.10}}
	//
	// ⚠️ 键是**上游 ID**（如 wan-3.0），不是对外公开名（wan-3.0-yz）——
	// 因为加价是叠在「上游成本」上的，而成本由上游目录给出。
	Markup map[string]map[string]float64 `json:"markup"`
	// FixedPrices 是**人工钉死售价**的模型（键是**对外公开名**）。
	// 值是「档位 → 售价（人民币/秒）」。
	//
	// 为什么需要它：有些模型的价格是**对外承诺价**（例如 wan-3.0-yz 的
	// 720P ¥0.20/秒、1080P ¥0.25/秒），不能跟着上游成本浮动 ——
	// 上游一改成本，售价就跟着变，等于我们单方面改了客户已知的报价。
	// 这类模型由本表接管，同步任务只做「检测上游变更并告警」，不改价。
	//
	// ⚠️ 键是**对外公开名**（与 ModelPrice 的键一致），不是上游 ID。
	// 两者不同的模型（wan-3.0-yz ↔ wan-3.0）尤其不能搞混：
	// 写错键 = 钉死了一个不存在的模型，真模型反而被上游成本改价。
	FixedPrices map[string]map[string]float64 `json:"fixed_prices"`
	// MaxChangeRatio 单次变动幅度上限（相对当前售价）。超过则只告警不落库。
	// 0 表示不限制 —— 不建议，上游偶发错价会直接变成我们的售价。
	MaxChangeRatio float64 `json:"max_change_ratio"`
	// AutoApply 是否自动落库。false 时同步任务只记录差异、不写价。
	AutoApply bool `json:"auto_apply"`
	// Ratios 是最近一次同步算出的档位倍率（相对基准档，基准档恒为 1.0）。
	// 由同步任务写入，管理员改 markup 时不必填。
	// ⚠️ 键是**对外公开名**（EstimateBilling 按公开名读）。
	Ratios map[string]map[string]float64 `json:"ratios"`
	// UpdatedAt 最近一次成功同步的 Unix 时间戳，0 表示从未同步过。
	UpdatedAt int64 `json:"updated_at"`
}

// defaultPolicy 与代码里原来的常量保持一致，保证「没有这个 option 时行为不变」。
func defaultPolicy() Policy {
	return Policy{
		Markup: map[string]map[string]float64{
			"wan3.0-video":       {"*": 0.05},
			"wan3.0-video-prime": {"720P": 0.10, "1080P": 0.10},
			// 2026-11-18 上游新上架的两个**独立模型**（不是旧名改名）。
			// 加价规则与旧两名保持一致（同一档位定价口径）：标准版每档 +¥0.05，
			// prime 每档 +¥0.10（老板 2026-11-18 口径「沿用旧商品同价」）。
			//
			// ⚠️ SJ-wan3-prime **只有 720P 一个键**：
			// 上游 1080p/480p 实测均回 resolution_disabled（2026-11-18），
			// 我们不卖这两档。留一个 1080P 的加价键会造成「万一上游目录回 1080P
			// 就按一个我们根本不卖的档位算出售价」—— 直接删掉，让它 fail loud。
			//
			// ⚠️ 键是**上游 ID**：wan-3.0-yz 的上游 ID 是 wan-3.0。
			"wan-3.0":       {"*": 0.05},
			"SJ-wan3-prime": {"720P": 0.10},
			// hk-wan3（2026-11-19 接入）：上游 480P/720P/1080P = 0.11/0.21/0.41。
			// 老板口径「上游 + ¥0.07」，但 720P 另外钉死对外价 ¥0.25（= 成本 +0.04），
			// 所以这里的分档加价是 0.07 / 0.04 / 0.07 —— 与 FixedPrices 保持一致，
			// 万一将来把 hk-wan3 从 FixedPrices 拿掉，算出来的价也不会突然跳档。
			"hk-wan3": {"480P": 0.07, "720P": 0.04, "1080P": 0.07},
		},
		// FixedPrices 钉死对外承诺价的模型（键是**对外公开名**）。
		//
		// wan-3.0-yz（老板 2026-11-18 定）：720P ¥0.20/秒、1080P ¥0.25/秒。
		// 这两个价是**对外报价**，不随上游成本浮动 —— 放进 FixedPrices 后，
		// 同步任务遇到它只告警、不改 ModelPrice。
		//
		// ⚠️ 上游 ID 是 wan-3.0、对外名是 wan-3.0-yz，这里必须写**对外名**：
		// ModelPrice 的键与 EstimateBilling 读倍率的键都是对外名。
		FixedPrices: map[string]map[string]float64{
			"wan-3.0-yz": {"720P": 0.20, "1080P": 0.25},
			// hk-wan3（2026-11-19 老板口径）：720P ¥0.25/秒 是对外报价；
			// 480P / 1080P 按「上游 + ¥0.07」→ ¥0.18 / ¥0.48。
			"hk-wan3": {"480P": 0.18, "720P": 0.25, "1080P": 0.48},
		},
		MaxChangeRatio: 0.30,
		AutoApply:      true,
		Ratios: map[string]map[string]float64{
			"wan3.0-video":       {"480P": 1.0, "720P": 1.0, "1080P": 1.0},
			"wan3.0-video-prime": {"720P": 1.0, "1080P": 1.2},
			// wan-3.0-yz：720P ¥0.20/秒、1080P ¥0.25/秒 → 倍率 = 0.25/0.20 = 1.25。
			"wan-3.0-yz": {"720P": 1.0, "1080P": 1.25},
			// SJ-wan3-prime：上游仅 720P 一档（1080p/480p 被禁用）→ 倍率恒为 1。
			"SJ-wan3-prime": {"720P": 1.0},
			// hk-wan3：基准档 480P ¥0.18 → 720P 0.25/0.18、1080P 0.48/0.18。
			"hk-wan3": {"480P": 1.0, "720P": 0.25 / 0.18, "1080P": 0.48 / 0.18},
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
	// FixedPrices 同样要保留：数据库里已有的 option（老版本写入的）根本没有
	// fixed_prices 字段，解析出来是 nil。若不在这里兜住，一次「改加价规则」的
	// 保存就会把钉死价清空 —— 对外承诺价随后被上游成本改写，且无任何痕迹。
	if parsed.FixedPrices == nil {
		parsed.FixedPrices = current.FixedPrices
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

// GetFixedPrice 返回某模型某档位的**钉死售价**（人民币/秒）。
// 第二个返回值为 false 表示这个模型不走钉死价（由上游成本 + 加价决定）。
func GetFixedPrice(modelName, resolution string) (float64, bool) {
	mu.RLock()
	defer mu.RUnlock()
	byResolution, ok := current.FixedPrices[modelName]
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

// HasFixedPrices 判断有没有任何模型走钉死价。
func HasFixedPrices() bool {
	mu.RLock()
	defer mu.RUnlock()
	return len(current.FixedPrices) > 0
}

// FixedPriceModels 列出所有走钉死价的模型（对外公开名）。
func FixedPriceModels() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(current.FixedPrices))
	for name := range current.FixedPrices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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
