package model

import (
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// 金额口径说明：
// - revenue（现金实收）= top_ups.money where status=success，按 complete_time 落期；
//   订阅购买成功时 upsertSubscriptionTopUpTx 会往 top_ups 同步一条同 trade_no 的
//   镜像流水（amount=0），它属于 subscription_orders 独立账，必须排除，否则重复计收入。
// - billed（渠道消耗金额）= quota_data.quota / QuotaPerUnit * USDExchangeRate，按消费小时桶落期；
// 两者是不同的账，差额单独下发，绝不互相替代。成本表只提供 cash out 的金额。
// VPN / 服务器订单各有独立订单表，不写 top_ups，天然不在现金口径内。

const FinanceGranularityDay = "day"
const FinanceGranularityMonth = "month"

type FinanceSummary struct {
	Start           int64   `json:"start"`
	End             int64   `json:"end"`
	Revenue         string  `json:"revenue"`
	RevenueCount    int64   `json:"revenue_count"`
	Cost            string  `json:"cost"`
	CostCount       int64   `json:"cost_count"`
	Net             string  `json:"net"`
	MarginRate      string  `json:"margin_rate"`
	Billed          string  `json:"billed"`
	BilledQuota     int64   `json:"billed_quota"`
	Requests        int64   `json:"requests"`
	Diff            string  `json:"diff"`
	UnallocatedCost string  `json:"unallocated_cost"`
	QuotaPerUnit    float64 `json:"quota_per_unit"`
	USDExchangeRate float64 `json:"usd_exchange_rate"`
}

type FinanceChannelStat struct {
	ChannelId   int    `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	BilledQuota int64  `json:"billed_quota"`
	Billed      string `json:"billed"`
	Cost        string `json:"cost"`
	GrossProfit string `json:"gross_profit"`
	MarginRate  string `json:"margin_rate"`
	Requests    int64  `json:"requests"`
	TokenUsed   int64  `json:"token_used"`
	CostShare   string `json:"cost_share"`
}

type FinanceTrendPoint struct {
	Period  string `json:"period"`
	Revenue string `json:"revenue"`
	Cost    string `json:"cost"`
	Billed  string `json:"billed"`
	Net     string `json:"net"`
}

type FinanceUsage struct {
	BilledQuota int64
	Requests    int64
	TokenUsed   int64
}

// QuotaToCNY 把内部额度折成人民币展示金额，与 logger.FormatQuota / billing.go 同口径。
// 返回未舍入的值，调用方统一在最终展示时 Round(2)。
func QuotaToCNY(quota int64) decimal.Decimal {
	if common.QuotaPerUnit <= 0 {
		return decimal.Zero
	}
	return decimal.NewFromInt(quota).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(operation_setting.USDExchangeRate))
}

func moneyString(d decimal.Decimal) string {
	return d.Round(2).StringFixed(2)
}

// cashRevenueQuery 现金实收只取真实充值流水。subscription_orders 由 AutoMigrate 建表，
// NOT EXISTS 在 SQLite / MySQL / PostgreSQL 都可用；不用 amount > 0 区分是因为
// TOKENS 显示模式下真实充值也可能算出 amount=0，用 trade_no 关联才可靠。
func cashRevenueQuery(start, end int64) *gorm.DB {
	return DB.Table("top_ups").Where(
		"status = ? AND complete_time >= ? AND complete_time <= ? AND NOT EXISTS (SELECT 1 FROM subscription_orders s WHERE s.trade_no = top_ups.trade_no)",
		common.TopUpStatusSuccess, start, end,
	)
}

func percentString(part, whole decimal.Decimal) string {
	if whole.LessThanOrEqual(decimal.Zero) {
		return "0.00"
	}
	return part.Div(whole).Mul(decimal.NewFromInt(100)).Round(2).StringFixed(2)
}

// GetFinanceUsageByChannel 从 quota_data 按渠道聚合消耗。
// quota_data 已按 channel_id 落库且不受日志清理影响，所以这里只加 GROUP BY，不改写入链路。
// SUM 返回类型在三库不同，统一用 decimal.Decimal.Scan 后取整。
func GetFinanceUsageByChannel(start, end int64) (map[int]FinanceUsage, error) {
	rows := make([]struct {
		ChannelId int             `gorm:"column:channel_id"`
		Quota     decimal.Decimal `gorm:"column:quota"`
		Requests  decimal.Decimal `gorm:"column:requests"`
		TokenUsed decimal.Decimal `gorm:"column:token_used"`
	}, 0)
	err := DB.Table("quota_data").
		Select("channel_id, COALESCE(SUM(quota), 0) AS quota, COALESCE(SUM(count), 0) AS requests, COALESCE(SUM(token_used), 0) AS token_used").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("channel_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[int]FinanceUsage, len(rows))
	for _, row := range rows {
		result[row.ChannelId] = FinanceUsage{
			BilledQuota: row.Quota.IntPart(),
			Requests:    row.Requests.IntPart(),
			TokenUsed:   row.TokenUsed.IntPart(),
		}
	}
	return result, nil
}

// getChannelNames 批量解析渠道名；已删除的渠道由调用方回退成 channel-<id>。
func getChannelNames(ids []int) map[int]string {
	names := make(map[int]string, len(ids))
	if len(ids) == 0 {
		return names
	}
	rows := make([]struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}, 0)
	if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return names
	}
	for _, row := range rows {
		names[row.Id] = row.Name
	}
	return names
}

// GetFinanceChannelStats 渠道维度统计：消耗金额（计费额）、归属成本、毛利、毛利率、成本占比。
// 成本表里有支出但本期无消耗的渠道也会出现；channel_id=0 单列公共成本，
// 若该行无计费额则毛利率按 0 展示（分母为 0 无定义），不计入渠道毛利判断。
// 汇总与渠道表共用本函数，保证两处金额严格一致。
func GetFinanceChannelStats(start, end int64) ([]FinanceChannelStat, error) {
	usage, err := GetFinanceUsageByChannel(start, end)
	if err != nil {
		return nil, err
	}
	costs, err := SumActiveChannelCostsByChannel(start, end)
	if err != nil {
		return nil, err
	}

	ids := make([]int, 0, len(usage)+len(costs))
	seen := make(map[int]struct{})
	for id := range usage {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	for id := range costs {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	names := getChannelNames(ids)
	totalCostCents := int64(0)
	for _, cents := range costs {
		totalCostCents += cents
	}
	totalCost := CentsToDecimal(totalCostCents)

	stats := make([]FinanceChannelStat, 0, len(ids))
	for _, id := range ids {
		u := usage[id]
		cost := CentsToDecimal(costs[id])
		billed := QuotaToCNY(u.BilledQuota).Round(2)
		gross := billed.Sub(cost)
		name := names[id]
		if id == 0 {
			name = "Unallocated"
		} else if name == "" {
			name = "channel-" + strconv.Itoa(id)
		}
		stats = append(stats, FinanceChannelStat{
			ChannelId:   id,
			ChannelName: name,
			BilledQuota: u.BilledQuota,
			Billed:      moneyString(billed),
			Cost:        moneyString(cost),
			GrossProfit: moneyString(gross),
			MarginRate:  percentString(gross, billed),
			Requests:    u.Requests,
			TokenUsed:   u.TokenUsed,
			CostShare:   percentString(cost, totalCost),
		})
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].BilledQuota != stats[j].BilledQuota {
			return stats[i].BilledQuota > stats[j].BilledQuota
		}
		return stats[i].ChannelId < stats[j].ChannelId
	})
	return stats, nil
}

// GetFinanceSummary 现金口径汇总：实收只取成功状态的充值实付金额（按 complete_time 落期）。
// 套餐 / 订阅 / 服务器订单是独立账，不在这里混入，避免重复计收入。
// 渠道消耗金额与成本复用 GetFinanceChannelStats，保证汇总卡与渠道表金额严格一致。
func GetFinanceSummary(start, end int64) (FinanceSummary, error) {
	summary := FinanceSummary{
		Start:           start,
		End:             end,
		QuotaPerUnit:    common.QuotaPerUnit,
		USDExchangeRate: operation_setting.USDExchangeRate,
	}

	var revenue decimal.Decimal
	if err := cashRevenueQuery(start, end).
		Select("COALESCE(SUM(money), 0)").
		Row().Scan(&revenue); err != nil {
		return summary, err
	}
	var revenueCount int64
	if err := cashRevenueQuery(start, end).Count(&revenueCount).Error; err != nil {
		return summary, err
	}

	stats, err := GetFinanceChannelStats(start, end)
	if err != nil {
		return summary, err
	}
	billed := decimal.Zero
	cost := decimal.Zero
	costCount := int64(0)
	for _, s := range stats {
		v, _ := decimal.NewFromString(s.Billed)
		billed = billed.Add(v)
		c, _ := decimal.NewFromString(s.Cost)
		cost = cost.Add(c)
	}
	if err := DB.Model(&ChannelCost{}).
		Where("status = ? AND occurred_at >= ? AND occurred_at <= ?", ChannelCostStatusActive, start, end).
		Count(&costCount).Error; err != nil {
		return summary, err
	}

	unallocated := decimal.Zero
	for _, s := range stats {
		if s.ChannelId == 0 {
			unallocated, _ = decimal.NewFromString(s.Cost)
		}
	}

	net := revenue.Sub(cost)
	summary.Revenue = moneyString(revenue)
	summary.RevenueCount = revenueCount
	summary.Cost = moneyString(cost)
	summary.CostCount = costCount
	summary.Net = moneyString(net)
	summary.MarginRate = percentString(net, revenue)
	summary.Billed = moneyString(billed)
	summary.Diff = moneyString(revenue.Sub(billed))
	summary.UnallocatedCost = moneyString(unallocated)
	for _, s := range stats {
		summary.BilledQuota += s.BilledQuota
		summary.Requests += s.Requests
	}
	return summary, nil
}

// GetFinanceTrend 按日/月给出三条曲线。用 Go 侧按本地时区归并，
// 避免 strftime / to_char / DATE_FORMAT 这类跨库方言差异；缺数据的期间补零，图表不出现断点。
func GetFinanceTrend(start, end int64, granularity string) ([]FinanceTrendPoint, error) {
	if granularity != FinanceGranularityMonth {
		granularity = FinanceGranularityDay
	}
	type bucket struct {
		revenue decimal.Decimal
		cost    int64
		quota   int64
	}
	buckets := make(map[string]*bucket)
	get := func(key string) *bucket {
		if b, ok := buckets[key]; ok {
			return b
		}
		b := &bucket{}
		buckets[key] = b
		return b
	}

	topups := make([]struct {
		CompleteTime int64   `gorm:"column:complete_time"`
		Money        float64 `gorm:"column:money"`
	}, 0)
	if err := cashRevenueQuery(start, end).
		Select("complete_time, money").
		Scan(&topups).Error; err != nil {
		return nil, err
	}
	for _, row := range topups {
		// top_ups.money 已在下单时 Round(2) 到分，这里再 Round(2) 消掉 float64 的二进制尾差。
		b := get(FinancePeriodKey(row.CompleteTime, granularity))
		b.revenue = b.revenue.Add(decimal.NewFromFloat(row.Money).Round(2))
	}

	costs := make([]struct {
		OccurredAt  int64 `gorm:"column:occurred_at"`
		AmountCents int64 `gorm:"column:amount_cents"`
	}, 0)
	if err := DB.Table("channel_costs").
		Select("occurred_at, amount_cents").
		Where("status = ? AND occurred_at >= ? AND occurred_at <= ?", ChannelCostStatusActive, start, end).
		Scan(&costs).Error; err != nil {
		return nil, err
	}
	for _, row := range costs {
		get(FinancePeriodKey(row.OccurredAt, granularity)).cost += row.AmountCents
	}

	usage := make([]struct {
		CreatedAt int64           `gorm:"column:created_at"`
		Quota     decimal.Decimal `gorm:"column:quota"`
	}, 0)
	if err := DB.Table("quota_data").
		Select("created_at, COALESCE(SUM(quota), 0) AS quota").
		Where("created_at >= ? AND created_at <= ?", start, end).
		Group("created_at").
		Scan(&usage).Error; err != nil {
		return nil, err
	}
	for _, row := range usage {
		get(FinancePeriodKey(row.CreatedAt, granularity)).quota += row.Quota.IntPart()
	}

	points := make([]FinanceTrendPoint, 0)
	for _, period := range financePeriods(start, end, granularity) {
		b := get(period)
		cost := CentsToDecimal(b.cost)
		points = append(points, FinanceTrendPoint{
			Period:  period,
			Revenue: moneyString(b.revenue),
			Cost:    moneyString(cost),
			Billed:  moneyString(QuotaToCNY(b.quota)),
			Net:     moneyString(b.revenue.Sub(cost)),
		})
	}
	return points, nil
}

func FinancePeriodKey(ts int64, granularity string) string {
	t := time.Unix(ts, 0).In(time.Local)
	if granularity == FinanceGranularityMonth {
		return t.Format("2006-01")
	}
	return t.Format("2006-01-02")
}

func financePeriods(start, end int64, granularity string) []string {
	first := time.Unix(start, 0).In(time.Local)
	var t time.Time
	if granularity == FinanceGranularityMonth {
		t = time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.Local)
	} else {
		t = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, time.Local)
	}
	layout := "2006-01-02"
	if granularity == FinanceGranularityMonth {
		layout = "2006-01"
	}
	keys := make([]string, 0, 32)
	for t.Unix() <= end && len(keys) < 400 {
		keys = append(keys, t.Format(layout))
		if granularity == FinanceGranularityMonth {
			t = t.AddDate(0, 1, 0)
		} else {
			t = t.AddDate(0, 0, 1)
		}
	}
	return keys
}
