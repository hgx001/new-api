package controller

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/youzanwan3"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/youzanwan3_setting"
)

// youzanPricingSyncInitialDelay 是进程启动后第一次同步的等待时间。
// 不在启动瞬间就打上游：那时往往刚重启、缓存还没热，而且多实例会同时打。
const youzanPricingSyncInitialDelay = 5 * time.Minute

// youzanPricingSyncInterval 是常规同步间隔。上游调价是人工操作，按小时足够。
const youzanPricingSyncInterval = time.Hour

// youzanAliasTargets 是「对外别名 → 真实上游模型」，用于**同步售价**。
//
// ⚠️ 2026-10-29 起这里**故意是空的**：seedance2.5-特惠 虽然是 prime 的别名，
// 但老板明确它的价格独立（720P ¥0.20 / 1080P ¥0.30），prime 调价它不动。
// 放进这张表就会被 applyYouzanPricePlans 覆盖成 prime 的价（而且是静默的：
// 两边都不报错，只是客户换个名字调用价格不同）。
//
// 它的 ModelPrice 单独配在 options 里，档位倍率走 youzanwan3 包里的
// tehuiResolutionSizeRatio 常量；applyYouzanPricePlans 只写 plans 里的模型，
// 不会碰它。
//
// 以后要加**跟价**的别名再往这里放。
var youzanAliasTargets = map[string]string{}

// YouzanPricingChange 是一档售价的变化。
type YouzanPricingChange struct {
	Model      string  `json:"model"`
	Resolution string  `json:"resolution"`
	OldPrice   float64 `json:"old_price_cny"`
	NewPrice   float64 `json:"new_price_cny"`
	// ChangeRatio 是相对变化幅度（绝对值），0.1 表示 10%。
	ChangeRatio float64 `json:"change_ratio"`
}

// YouzanPricingSyncResult 是一次同步的完整结果，直接作为接口响应返回。
type YouzanPricingSyncResult struct {
	ChannelID int                         `json:"channel_id"`
	Applied   bool                        `json:"applied"`
	Plans     []youzanwan3.ModelPricePlan `json:"plans"`
	Changes   []YouzanPricingChange       `json:"changes"`
	// Blocked 记录「变化太大、被阈值拦下」的档位，非空时整批不落库。
	Blocked []string `json:"blocked"`
	// Warnings 是「没拦但要知道」的信息（例如上游新增/下架了模型）。
	Warnings []string `json:"warnings"`
}

// StartYouzanWan3PricingSync 启动后台同步循环。
//
// 只做一件事：定时把上游成本换算成售价写回配置。任何一步失败都只记日志、
// 保持原价 —— 调价是「锦上添花」，绝不能因为它把现有计费搞挂。
func StartYouzanWan3PricingSync() {
	time.Sleep(youzanPricingSyncInitialDelay)
	for {
		if result, err := SyncYouzanWan3Pricing(false); err != nil {
			common.SysError("youzan wan3 pricing sync failed: " + err.Error())
		} else if result != nil {
			// 每小时一条，即使零变动也记 —— 否则「任务到底跑没跑」无从判断。
			common.SysLog(fmt.Sprintf("youzan wan3 pricing sync: applied=%v changes=%d blocked=%d warnings=%d",
				result.Applied, len(result.Changes), len(result.Blocked), len(result.Warnings)))
			for _, warning := range result.Warnings {
				common.SysLog("youzan wan3 pricing sync warning: " + warning)
			}
		}
		time.Sleep(youzanPricingSyncInterval)
	}
}

// SyncYouzanWan3Pricing 拉一次上游价格并按策略落库。
//
// force=true 时跳过「变动幅度阈值」这道护栏（手动触发用），
// 但仍然遵守 auto_apply=false（那是明确关掉自动改价，不是护栏）。
func SyncYouzanWan3Pricing(force bool) (*YouzanPricingSyncResult, error) {
	channel, err := pickYouzanWan3Channel()
	if err != nil {
		return nil, err
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, fmt.Errorf("youzan wan3 pricing sync: 取渠道密钥失败: %w", apiErr)
	}
	models, err := youzanwan3.FetchUpstreamModels(channel.GetBaseURL(), strings.TrimSpace(key), channel.GetSetting().Proxy)
	if err != nil {
		return nil, err
	}
	plans, err := youzanwan3.PlanSellingPrices(models, youzanwan3.MarkupFor)
	if err != nil {
		return nil, err
	}

	result := &YouzanPricingSyncResult{ChannelID: channel.Id, Plans: plans}
	result.Warnings = append(result.Warnings, upstreamCatalogWarnings(models)...)
	result.Warnings = append(result.Warnings, missingPriceWarnings(models, plans)...)
	result.Changes, result.Blocked = diffYouzanPrices(plans, ratio_setting.GetModelPriceCopy(), youzanwan3_setting.Get().MaxChangeRatio)

	policy := youzanwan3_setting.Get()
	if !policy.AutoApply {
		result.Warnings = append(result.Warnings, "auto_apply=false：只检测不落库")
		return result, nil
	}
	if len(result.Blocked) > 0 && !force {
		result.Warnings = append(result.Warnings, "存在超出阈值的变动，本批未落库；确认无误后可用 force 重试")
		return result, nil
	}
	if err := applyYouzanPricePlans(plans); err != nil {
		return result, err
	}
	result.Applied = true
	return result, nil
}

// pickYouzanWan3Channel 取一个可用的有赞 Wan3 渠道。
// 同一上游配多个渠道时随便取一个即可 —— 价格来自上游，与渠道无关。
func pickYouzanWan3Channel() (*model.Channel, error) {
	var channels []*model.Channel
	err := model.DB.
		Where("type = ? AND status = ?", constant.ChannelTypeYouzanWan3, common.ChannelStatusEnabled).
		Order("id asc").
		Limit(1).
		Find(&channels).Error
	if err != nil {
		return nil, fmt.Errorf("youzan wan3 pricing sync: 查询渠道失败: %w", err)
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("youzan wan3 pricing sync: 没有启用的有赞 Wan3 渠道（type=%d）", constant.ChannelTypeYouzanWan3)
	}
	return channels[0], nil
}

// upstreamCatalogWarnings 报告上游模型目录的变化。
//
// 只告警、不自动上架/下架：上游新增模型要不要卖、卖多少钱是商业决定，
// 自动加进渠道反而会让一个未经定价的模型直接对客户可见。
func upstreamCatalogWarnings(models []youzanwan3.UpstreamModel) []string {
	warnings := make([]string, 0)
	upstream := make(map[string]bool, len(models))
	for _, item := range models {
		upstream[item.ID] = true
	}
	// ⚠️ 上游 2026-11-18 起是**四个并列的独立模型**，不是改名关系：
	//
	//	wan3.0-video        旧标准版（仍可提交）
	//	wan3.0-video-prime  旧 prime（上游通道已禁用：workflow_unavailable）
	//	wan-3.0            新标准版
	//	SJ-wan3-prime      新 prime
	//
	// 加上已上架的 wan-3.0-pro，一共五个「已知模型」。
	// 旧名也留在集合里：上游目录接口可能只回旧名（尚未刷新），
	// 两种都算「已知」，免得每次同步都刷一条「上游新增模型」的假告警。
	//
	// ⚠️ 这个集合里的键**全部是上游 ID**。对外公开名（wan-3.0-yz）**不能**
	// 混进来 —— 上游目录里根本不会出现公开名，加进来会让「上游已下架」
	// 那条检查永远报假告警（上游有 wan-3.0、没有 wan-3.0-yz）。
	knownUpstreamIDs := map[string]bool{
		"wan3.0-video":       true,
		"wan3.0-video-prime": true,
		"wan-3.0":            true,
		"SJ-wan3-prime":      true,
		"wan-3.0-pro":        true,
	}
	for id := range upstream {
		if !knownUpstreamIDs[id] {
			warnings = append(warnings, fmt.Sprintf("上游新增模型 %s：未自动上架，需要确认定价后手工加渠道与卡片", id))
		}
	}
	for id := range knownUpstreamIDs {
		if !upstream[id] {
			// 告警里说客户认识的名字（对外公开名），便于直接去广场/卡片改。
			publicName := youzanwan3.PublicModelName(id)
			warnings = append(warnings, fmt.Sprintf("上游已下架模型 %s：价格不再更新，请同步下架", publicName))
		}
	}
	return warnings
}

// missingPriceWarnings 报告「上游目录里有、但这次没算出价」的模型。
//
// 上游的 /v1/models 一度不再返回 unitPrice / resolutionPrices（2026-11-18 实测）。
// 那种情况下 PlanSellingPrices 会**跳过**这些模型（不让一个模型的缺价拖垮
// 整批调价），但跳过必须留痕 —— 否则管理员看到「同步成功」却不知道
// 某个模型的价已经不再自动更新了。
func missingPriceWarnings(models []youzanwan3.UpstreamModel, plans []youzanwan3.ModelPricePlan) []string {
	planned := make(map[string]bool, len(plans))
	for _, plan := range plans {
		planned[plan.Model] = true
	}
	warnings := make([]string, 0)
	for _, item := range models {
		if planned[item.ID] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf(
			"上游模型 %s 未提供按秒价格，本次跳过（售价保持不变，请人工确认）", item.ID))
	}
	return warnings
}

// diffYouzanPrices 把新算出的售价与当前生效的售价逐档对比。
//
// currentPrices 是 ModelPrice（USD/秒）的快照，由调用方注入以便测试。
// 当前售价 = ModelPrice × USD2RMB × 当前倍率。
// 倍率优先取 setting 里的动态值，没有则视为「与新计划相同」——
// 那种情况说明还没同步过，只比较基准价即可。
func diffYouzanPrices(plans []youzanwan3.ModelPricePlan, currentPrices map[string]float64, maxChangeRatio float64) ([]YouzanPricingChange, []string) {
	changes := make([]YouzanPricingChange, 0)
	blocked := make([]string, 0)

	for _, plan := range plans {
		// ⚠️ currentPrices 是 ModelPrice 快照，键是**对外公开名**；
		// plan.Model 是上游 ID。两者不同的模型（wan-3.0-yz ↔ wan-3.0）
		// 不转换的话 oldBaseUSD 永远取不到 → 每次同步都当成「首次配置」，
		// 变动幅度这道护栏就形同虚设。
		publicName := youzanwan3.PublicModelName(plan.Model)
		oldBaseUSD, ok := currentPrices[publicName]
		if !ok || oldBaseUSD <= 0 {
			// 从来没有配过价：这是首次配置，不算「变动」，也不需要阈值保护。
			continue
		}
		oldBaseCNY := oldBaseUSD * ratio_setting.USD2RMB
		for resolution, newPrice := range plan.Prices {
			oldRatio, hasRatio := youzanwan3_setting.GetResolutionRatio(publicName, resolution)
			if !hasRatio {
				// 没有动态倍率时用新计划的倍率反推旧价，等价于「只比基准价」。
				oldRatio = plan.Ratios[resolution]
			}
			oldPrice := roundCnyForCompare(oldBaseCNY * oldRatio)
			changeRatio := relativeChange(oldPrice, newPrice)
			if changeRatio == 0 {
				continue
			}
			changes = append(changes, YouzanPricingChange{
				Model:       publicName,
				Resolution:  resolution,
				OldPrice:    oldPrice,
				NewPrice:    newPrice,
				ChangeRatio: changeRatio,
			})
			if maxChangeRatio > 0 && changeRatio > maxChangeRatio {
				blocked = append(blocked, fmt.Sprintf(
					"%s %s：¥%.4f → ¥%.4f（变动 %.1f%%，阈值 %.0f%%）",
					publicName, resolutionLabel(resolution), oldPrice, newPrice,
					changeRatio*100, maxChangeRatio*100))
			}
		}
	}
	return changes, blocked
}

// applyYouzanPricePlans 把售价写进 ModelPrice 与动态倍率配置。
//
// 两个 option 一起写（UpdateOptionsBulk 在一个事务里），
// 否则中途失败会出现「基准价换了、倍率还是旧的」，客户按新价 × 旧倍率被多扣。
func applyYouzanPricePlans(plans []youzanwan3.ModelPricePlan) error {
	prices := ratio_setting.GetModelPriceCopy()
	ratios := make(map[string]map[string]float64, len(plans))
	for _, plan := range plans {
		// ⚠️ 键必须是**对外公开名**（wan-3.0-yz），不是上游 ID（wan-3.0）。
		// 计费链路（ModelPriceHelper / EstimateBilling）读的是客户请求里的
		// 那个名字；写上游 ID 等于把价格写到一个没人调用的键上。
		publicName := youzanwan3.PublicModelName(plan.Model)
		// 不额外取整：BasePrice/7.3 是确定性的 IEEE 运算，同一份上游价每次算出的
		// 值逐位相同，JSON 也会输出同样的最短表示。取整反而会让 ModelPrice 与
		// 手写配置的精度不一致（显示上虽看不出，但没必要引入差异）。
		prices[publicName] = plan.BasePrice / ratio_setting.USD2RMB
		ratios[publicName] = plan.Ratios
	}
	// 跟价的别名（见 youzanAliasTargets）。当前为空：seedance2.5-特惠 独立定价。
	// 这段保留是因为「同一个上游模型对外卖两个名字」以后还会再出现，
	// 到时的正确做法就是在这里登记，让价格逐字相同。
	for alias, target := range youzanAliasTargets {
		baseUSD, ok := prices[target]
		if !ok {
			continue
		}
		prices[alias] = baseUSD
		if ratio, ok := ratios[target]; ok {
			ratios[alias] = ratio
		}
	}
	// 钉死售价的模型：覆盖上游算出来的价，保证对外承诺价不被上游成本改写。
	//
	// ⚠️ 必须在这里覆盖（而不是在 PlanSellingPrices 里跳过）：跳过的话，
	// ModelPrice 里这个模型的键会**保持旧值**——而旧值可能是按上游成本算的，
	// 上游一改成本，我们对外的报价就跟着变，而且没有任何痕迹。
	// 覆盖是显式的、每次同步都重新写一遍，行为可预期。
	for _, publicName := range youzanwan3_setting.FixedPriceModels() {
		tiers := youzanwan3_setting.Get().FixedPrices[publicName]
		if len(tiers) == 0 {
			continue
		}
		// 基准档 = 售价最低的那档（与 ModelPrice「基准档配最低价」的口径一致），
		// 其余档位用相对倍率表达。
		baseResolution, basePrice := lowestPricedTier(tiers)
		prices[publicName] = basePrice / ratio_setting.USD2RMB
		tierRatios := make(map[string]float64, len(tiers))
		for resolution, price := range tiers {
			if price <= 0 {
				continue
			}
			tierRatios[resolution] = price / basePrice
		}
		ratios[publicName] = tierRatios
		_ = baseResolution
	}

	priceJSON, err := common.Marshal(prices)
	if err != nil {
		return fmt.Errorf("youzan wan3 pricing sync: 序列化 ModelPrice 失败: %w", err)
	}
	youzanwan3_setting.SetRatios(ratios, time.Now().Unix())
	if err := model.UpdateOptionsBulk(map[string]string{
		"ModelPrice":        string(priceJSON),
		"YouzanWan3Pricing": youzanwan3_setting.MarshalJSONString(),
	}); err != nil {
		return fmt.Errorf("youzan wan3 pricing sync: 写入配置失败: %w", err)
	}
	return nil
}

// lowestPricedTier 找出售价最低的那一档（并列时按档位名排序取第一个）。
//
// 为什么按最低档当基准：ModelPrice 只能配一个数，而广场徽标直接显示它。
// 基准档配最低价 + 其它档用倍率表达，才不会在高档位上少收钱。
func lowestPricedTier(tiers map[string]float64) (string, float64) {
	bestResolution := ""
	bestPrice := 0.0
	first := true
	for _, resolution := range sortedKeys(tiers) {
		price := tiers[resolution]
		if price <= 0 {
			continue
		}
		if first || price < bestPrice {
			bestResolution = resolution
			bestPrice = price
			first = false
		}
	}
	return bestResolution, bestPrice
}

func sortedKeys(m map[string]float64) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func relativeChange(oldPrice, newPrice float64) float64 {
	if oldPrice <= 0 {
		return 1
	}
	return math.Abs(newPrice-oldPrice) / oldPrice
}

// roundCnyForCompare 与 youzanwan3.roundCny 同口径（0.001 元），
// 避免「0.2000001 vs 0.2」被当成真实变动。
func roundCnyForCompare(value float64) float64 {
	return math.Round(value*1000) / 1000
}

func resolutionLabel(resolution string) string {
	if resolution == "" {
		return "(single tier)"
	}
	return resolution
}
