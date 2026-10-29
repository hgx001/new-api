package youzanwan3

import (
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/youzanwan3_setting"

	"github.com/pkg/errors"
)

// creditCurrency 是上游计价的点数单位。上游 /v1/models 返回的
// currency 就是这个字符，unitPrice / resolutionPrices 的数字都以它为单位。
//
// 换算口径（沿用 constants.go 里既有的注释）：1 ⚡ = ¥1。
// 这个假设是整条动态调价链路的基础 —— 上游若换单位，必须先改这里再同步，
// 否则算出来的售价会整体错一个量级。FetchUpstreamModels 会校验 currency，
// 不是 ⚡ 直接报错而不是静默按 1:1 处理。
const creditCurrency = "⚡"

// 上游 /v1/models 只支持按秒计价的模型。其它计价单位（次、张）不在本适配器范围内。
const billingUnitSecond = "second"

// UpstreamModel 是 GET /v1/models 里 data[] 的一项。
type UpstreamModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	BillingUnit      string            `json:"billingUnit"`
	Currency         string            `json:"currency"`
	UnitPrice        string            `json:"unitPrice"`
	ResolutionPrices map[string]string `json:"resolutionPrices"`
	DurationPricing  struct {
		Enabled bool `json:"enabled"`
		Bands   []struct {
			MinDuration int     `json:"minDuration"`
			MaxDuration int     `json:"maxDuration"`
			UnitPrice   string  `json:"unitPrice"`
			Multiplier  float64 `json:"multiplier"`
		} `json:"bands"`
	} `json:"durationPricing"`
	SupportsSmartDuration bool `json:"supportsSmartDuration"`
	Parameters            struct {
		Resolution []string `json:"resolution"`
	} `json:"parameters"`
}

type upstreamModelList struct {
	Object string          `json:"object"`
	Data   []UpstreamModel `json:"data"`
}

// ModelPricePlan 是一个模型算完售价后的结果。
type ModelPricePlan struct {
	// Model 是上游模型 ID，同时也是我们的公开模型名。
	Model string
	// Costs 是上游成本（人民币/秒），键是分辨率档位。
	Costs map[string]float64
	// Prices 是我们对外的售价（人民币/秒）。
	Prices map[string]float64
	// BaseResolution 是基准档（成本最低的那档），ModelPrice 按它配置。
	BaseResolution string
	// BasePrice 是基准档售价（人民币/秒）。
	BasePrice float64
	// Ratios 是各档相对基准档的倍率，基准档恒为 1.0。
	Ratios map[string]float64
}

// FetchUpstreamModels 拉取上游模型目录（含计价信息）。
//
// 这是只读请求，不建单、不扣费，可以随时调用。
func FetchUpstreamModels(baseURL, apiKey, proxy string) ([]UpstreamModel, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, errors.New("youzan wan3 pricing sync: channel base url is empty")
	}
	client, err := getHTTPClient(proxy)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodGet, normalizeAPIRoot(baseURL)+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.Wrap(err, "youzan wan3 pricing sync: request /v1/models failed")
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, errors.Wrap(err, "youzan wan3 pricing sync: read /v1/models failed")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := strings.TrimSpace(string(body))
		if len(detail) > 300 {
			detail = detail[:300]
		}
		return nil, errors.Errorf("youzan wan3 pricing sync: /v1/models returned HTTP %d: %s", response.StatusCode, detail)
	}
	var parsed upstreamModelList
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, errors.Wrap(err, "youzan wan3 pricing sync: decode /v1/models failed")
	}
	if len(parsed.Data) == 0 {
		return nil, errors.New("youzan wan3 pricing sync: /v1/models returned no models")
	}
	return parsed.Data, nil
}

// CostsByResolution 把上游模型展开成「档位 → 成本（人民币/秒）」。
//
// 两种上游形态都要支持：
//   - 统一单价（wan3.0-video）：unitPrice + parameters.resolution 列出的每档同价
//   - 分档单价（wan3.0-video-prime）：resolutionPrices 直接给每档价格
//
// 返回 error 的场景一律是「我们没法可靠换算」，宁可不改价也不要按错的数据改：
//   - 计价单位不是秒
//   - 点数单位不是 ⚡（1:1 的换算假设不成立）
//   - 开了阶梯时长定价（按秒单价会随总时长变化，ModelPrice × seconds 算不准）
//   - 一个价格都解析不出来
func (m UpstreamModel) CostsByResolution() (map[string]float64, error) {
	if unit := strings.TrimSpace(m.BillingUnit); unit != "" && unit != billingUnitSecond {
		return nil, errors.Errorf("model %s bills by %q, only per-second pricing is supported", m.ID, unit)
	}
	if currency := strings.TrimSpace(m.Currency); currency != "" && currency != creditCurrency {
		return nil, errors.Errorf("model %s prices in %q, expected %q (1:1 CNY assumption broken)", m.ID, currency, creditCurrency)
	}
	if m.DurationPricing.Enabled {
		return nil, errors.Errorf("model %s enabled duration-banded pricing; per-second unit price is no longer authoritative", m.ID)
	}

	costs := make(map[string]float64, len(m.Parameters.Resolution))
	if len(m.ResolutionPrices) > 0 {
		for resolution, raw := range m.ResolutionPrices {
			value, err := parsePrice(raw)
			if err != nil {
				return nil, errors.Wrapf(err, "model %s resolution %s", m.ID, resolution)
			}
			costs[normalizeResolution(resolution)] = value
		}
	}
	if len(costs) == 0 && strings.TrimSpace(m.UnitPrice) != "" {
		value, err := parsePrice(m.UnitPrice)
		if err != nil {
			return nil, errors.Wrapf(err, "model %s unitPrice", m.ID)
		}
		// 没有 resolution 列表时至少给一个空档键，调用方按「单档」处理。
		resolutions := m.Parameters.Resolution
		if len(resolutions) == 0 {
			resolutions = []string{""}
		}
		for _, resolution := range resolutions {
			costs[normalizeResolution(resolution)] = value
		}
	}
	if len(costs) == 0 {
		return nil, errors.Errorf("model %s exposes neither unitPrice nor resolutionPrices", m.ID)
	}
	return costs, nil
}

// PlanSellingPrices 按加价策略把成本算成售价。
//
// 规则（2026-10-29 老板定）：每档固定加价 —— 售价 = 上游成本 + 该档加价。
// 基准档取成本最低的那档（并列时按档位名排序取第一个），倍率相对它计算。
//
// markupFor 由调用方注入，便于测试；生产传 youzanwan3_setting.MarkupFor。
func PlanSellingPrices(models []UpstreamModel, markupFor func(model, resolution string) (float64, bool)) ([]ModelPricePlan, error) {
	plans := make([]ModelPricePlan, 0, len(models))
	for _, item := range models {
		costs, err := item.CostsByResolution()
		if err != nil {
			return nil, err
		}
		prices := make(map[string]float64, len(costs))
		for resolution, cost := range costs {
			markup, ok := markupFor(item.ID, resolution)
			if !ok {
				return nil, errors.Errorf(
					"model %s resolution %q has no markup configured; refusing to guess a selling price",
					item.ID, displayResolution(resolution))
			}
			if markup < 0 {
				return nil, errors.Errorf("model %s resolution %q has negative markup %v", item.ID, displayResolution(resolution), markup)
			}
			prices[resolution] = roundCny(cost + markup)
		}
		baseResolution, basePrice := lowestTier(prices, item.Parameters.Resolution)
		if basePrice <= 0 {
			return nil, errors.Errorf("model %s computed a non-positive base price", item.ID)
		}
		ratios := make(map[string]float64, len(prices))
		for resolution, price := range prices {
			ratios[resolution] = roundRatio(price / basePrice)
		}
		ratios[baseResolution] = 1.0
		plans = append(plans, ModelPricePlan{
			Model:          item.ID,
			Costs:          costs,
			Prices:         prices,
			BaseResolution: baseResolution,
			BasePrice:      basePrice,
			Ratios:         ratios,
		})
	}
	sort.Slice(plans, func(i, j int) bool { return plans[i].Model < plans[j].Model })
	return plans, nil
}

// lowestTier 返回成本最低的档位，作为 ModelPrice 的基准档。
//
// 并列时按上游 parameters.resolution 的**顺序**取第一个 —— 上游把 480P 排在前面
// 就说明那是它心里的默认档，与 adaptor 的 defaultResolution 也一致。
// 不能按字典序：那样 "1080P" 会排在 "480P" 前面，基准档变成最贵的那档，
// 三档同价时会算出「1080P 是基准、480P 也是 1.0 倍」这种反直觉的配置。
//
// order 里没列到的档位（例如上游只给了 resolutionPrices 没给 parameters）按名称
// 排序补在后面，保证同一份输入两次算出的基准档相同。
func lowestTier(prices map[string]float64, order []string) (string, float64) {
	bestResolution := ""
	bestPrice := 0.0
	first := true
	consider := func(resolution string, price float64) {
		// 严格小于：并列时保留先遇到的那个。
		if first || price < bestPrice {
			bestResolution, bestPrice = resolution, price
			first = false
		}
	}
	seen := make(map[string]bool, len(order))
	for _, raw := range order {
		resolution := normalizeResolution(raw)
		price, ok := prices[resolution]
		if !ok {
			continue
		}
		seen[resolution] = true
		consider(resolution, price)
	}
	rest := make([]string, 0, len(prices))
	for resolution := range prices {
		if !seen[resolution] {
			rest = append(rest, resolution)
		}
	}
	sort.Strings(rest)
	for _, resolution := range rest {
		consider(resolution, prices[resolution])
	}
	return bestResolution, bestPrice
}

func parsePrice(raw string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, errors.New("empty price")
	}
	value, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "invalid price %q", raw)
	}
	if value <= 0 {
		return 0, errors.Errorf("non-positive price %q", raw)
	}
	return value, nil
}

// roundCny 把售价收敛到 0.001 元，避免上游小数位把浮点噪声带进售价。
func roundCny(value float64) float64 {
	return float64(int64(value*1000+0.5)) / 1000
}

// roundRatio 把倍率收敛到 1e-6，保证同一份配置两次算出同一个数。
func roundRatio(value float64) float64 {
	return float64(int64(value*1e6+0.5)) / 1e6
}

func displayResolution(resolution string) string {
	if resolution == "" {
		return "(single tier)"
	}
	return resolution
}

// MarkupFor 是生产用的加价查询函数，读 youzanwan3_setting 里的策略。
func MarkupFor(model, resolution string) (float64, bool) {
	return youzanwan3_setting.MarkupFor(model, resolution)
}
