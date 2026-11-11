package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupFinanceDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&ChannelCost{}, &TopUp{}, &QuotaData{}, &Channel{}))

	oldDB := DB
	oldQuotaPerUnit := common.QuotaPerUnit
	oldRate := operation_setting.USDExchangeRate
	DB = db
	common.QuotaPerUnit = 500000
	operation_setting.USDExchangeRate = 7.3
	t.Cleanup(func() {
		DB = oldDB
		common.QuotaPerUnit = oldQuotaPerUnit
		operation_setting.USDExchangeRate = oldRate
	})
}

func financeTestRange() (int64, int64) {
	start := time.Date(2026, 11, 1, 0, 0, 0, 0, time.Local).Unix()
	end := time.Date(2026, 11, 30, 23, 59, 59, 0, time.Local).Unix()
	return start, end
}

func addFinanceTopUp(t *testing.T, tradeNo, status string, money float64, completeTime int64) {
	t.Helper()
	require.NoError(t, DB.Create(&TopUp{
		TradeNo: tradeNo, UserId: 1, Money: money, Status: status,
		CreateTime: completeTime, CompleteTime: completeTime,
	}).Error)
}

func addFinanceCost(t *testing.T, channelId int, amount string, ts int64) *ChannelCost {
	t.Helper()
	cents, err := ParseChannelCostAmount(amount)
	require.NoError(t, err)
	cost := &ChannelCost{
		ChannelId: channelId, AmountCents: cents, OccurredAt: ts,
		Category: ChannelCostCategoryTopUp, CreatedBy: 1,
	}
	require.NoError(t, CreateChannelCost(cost))
	return cost
}

func TestParseChannelCostAmount(t *testing.T) {
	cases := []struct {
		input string
		want  int64
		ok    bool
	}{
		{"100", 10000, true},
		{"100.50", 10050, true},
		{" 12.3 ", 1230, true},
		{"-20.25", -2025, true},
		{"0.005", 0, false},
		{"0", 0, false},
		{"", 0, false},
		{"abc", 0, false},
		{"1000000000000", 0, false},
	}
	for _, tc := range cases {
		got, err := ParseChannelCostAmount(tc.input)
		if !tc.ok {
			assert.Error(t, err, tc.input)
			continue
		}
		require.NoError(t, err, tc.input)
		assert.Equal(t, tc.want, got, tc.input)
	}
	assert.Equal(t, "123.45", CentsToAmountString(12345))
	assert.Equal(t, "-5.00", CentsToAmountString(-500))
}

func TestQuotaToCNYUsesConfiguredRates(t *testing.T) {
	setupFinanceDB(t)
	assert.Equal(t, "7.30", QuotaToCNY(500000).Round(2).StringFixed(2))
	assert.Equal(t, "14.60", QuotaToCNY(1000000).Round(2).StringFixed(2))
}

// TestFinanceSummaryKeepsTwoMeasuresSeparate 现金实收与渠道消耗金额是两套账：
// 汇总差额必须单列，渠道表不能把实收摊进来；作废成本与未成功充值都不参与统计。
func TestFinanceSummaryKeepsTwoMeasuresSeparate(t *testing.T) {
	setupFinanceDB(t)
	start, end := financeTestRange()
	paidAt := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local).Unix()

	require.NoError(t, DB.Create(&Channel{Id: 1, Name: "ch-one"}).Error)
	require.NoError(t, DB.Create(&Channel{Id: 2, Name: "ch-two"}).Error)
	addFinanceTopUp(t, "FIN-1", common.TopUpStatusSuccess, 100.00, paidAt)
	addFinanceTopUp(t, "FIN-2", common.TopUpStatusSuccess, 24.80, paidAt)
	addFinanceTopUp(t, "FIN-PENDING", common.TopUpStatusPending, 999.00, paidAt)

	addFinanceCost(t, 1, "30.00", paidAt)
	addFinanceCost(t, 0, "8.80", paidAt)
	voided := addFinanceCost(t, 1, "100.00", paidAt)
	ok, err := VoidChannelCost(voided.Id)
	require.NoError(t, err)
	assert.True(t, ok)

	require.NoError(t, DB.Create(&QuotaData{
		ChannelID: 1, ModelName: "model-a", CreatedAt: paidAt,
		Quota: 1000000, Count: 4, TokenUsed: 100,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		ChannelID: 2, ModelName: "model-b", CreatedAt: paidAt,
		Quota: 500000, Count: 2, TokenUsed: 50,
	}).Error)

	summary, err := GetFinanceSummary(start, end)
	require.NoError(t, err)
	assert.Equal(t, "124.80", summary.Revenue)
	assert.Equal(t, int64(2), summary.RevenueCount)
	assert.Equal(t, "38.80", summary.Cost)
	assert.Equal(t, int64(2), summary.CostCount)
	assert.Equal(t, "86.00", summary.Net)
	assert.Equal(t, "68.91", summary.MarginRate)
	assert.Equal(t, "21.90", summary.Billed)
	assert.Equal(t, "102.90", summary.Diff)
	assert.Equal(t, "8.80", summary.UnallocatedCost)
	assert.Equal(t, int64(1500000), summary.BilledQuota)
	assert.Equal(t, int64(6), summary.Requests)

	stats, err := GetFinanceChannelStats(start, end)
	require.NoError(t, err)
	require.Len(t, stats, 3)
	byChannel := make(map[int]FinanceChannelStat, len(stats))
	for _, s := range stats {
		byChannel[s.ChannelId] = s
	}
	assert.Equal(t, "ch-one", byChannel[1].ChannelName)
	assert.Equal(t, "14.60", byChannel[1].Billed)
	assert.Equal(t, "30.00", byChannel[1].Cost)
	assert.Equal(t, "-15.40", byChannel[1].GrossProfit)
	assert.Equal(t, "100.00", byChannel[2].MarginRate)
	assert.Equal(t, "Unallocated", byChannel[0].ChannelName)
	assert.Equal(t, "0.00", byChannel[0].Billed)
	assert.Equal(t, "8.80", byChannel[0].Cost)
}

func TestFinanceCostEditsAndVoidAreGuarded(t *testing.T) {
	setupFinanceDB(t)
	ts := time.Date(2026, 11, 10, 12, 0, 0, 0, time.Local).Unix()
	cost := addFinanceCost(t, 1, "30.00", ts)

	duplicates, err := FindDuplicateChannelCosts(1, 3000, ts, ts+86400)
	require.NoError(t, err)
	assert.Equal(t, []int{cost.Id}, duplicates)

	updated, err := UpdateChannelCost(cost.Id, map[string]interface{}{"amount_cents": int64(2500)})
	require.NoError(t, err)
	assert.True(t, updated)
	after, err := GetChannelCostById(cost.Id)
	require.NoError(t, err)
	assert.Equal(t, int64(2500), after.AmountCents)

	first, err := VoidChannelCost(cost.Id)
	require.NoError(t, err)
	assert.True(t, first)
	second, err := VoidChannelCost(cost.Id)
	require.NoError(t, err)
	assert.False(t, second)
	editVoided, err := UpdateChannelCost(cost.Id, map[string]interface{}{"amount_cents": int64(1000)})
	require.NoError(t, err)
	assert.False(t, editVoided)

	start, end := financeTestRange()
	summary, err := GetFinanceSummary(start, end)
	require.NoError(t, err)
	assert.Equal(t, "0.00", summary.Cost)
	assert.Equal(t, int64(0), summary.CostCount)

	items, total, totalCents, err := SearchChannelCosts(ChannelCostQuery{Page: 1, PageSize: 10, ChannelId: -1})
	require.NoError(t, err)
	assert.Len(t, items, 0)
	assert.Equal(t, int64(0), total)
	assert.Equal(t, int64(0), totalCents)
	withVoided, totalWithVoided, _, err := SearchChannelCosts(ChannelCostQuery{Page: 1, PageSize: 10, ChannelId: -1, IncludeVoided: true})
	require.NoError(t, err)
	assert.Len(t, withVoided, 1)
	assert.Equal(t, int64(1), totalWithVoided)
}

// TestFinanceTrendFillsEmptyPeriods Go 侧按本地时区归并，缺数据的日期补零，
// 且 top_ups.money 的 float64 尾差不会污染按分汇总。
func TestFinanceTrendFillsEmptyPeriods(t *testing.T) {
	setupFinanceDB(t)
	day := time.Date(2026, 11, 10, 0, 0, 0, 0, time.Local)
	paidAt := day.Add(10 * time.Hour).Unix()
	start := day.AddDate(0, 0, -1).Unix()
	end := day.AddDate(0, 0, 1).Unix()

	addFinanceTopUp(t, "TREND-1", common.TopUpStatusSuccess, 10.10, paidAt)
	addFinanceTopUp(t, "TREND-2", common.TopUpStatusSuccess, 0.20, paidAt+3600)
	addFinanceCost(t, 1, "3.30", paidAt+7200)
	require.NoError(t, DB.Create(&QuotaData{
		ChannelID: 1, ModelName: "m", CreatedAt: paidAt,
		Quota: 500000, Count: 1,
	}).Error)
	require.NoError(t, DB.Create(&QuotaData{
		ChannelID: 1, ModelName: "m", CreatedAt: paidAt + 3600,
		Quota: 500000, Count: 1,
	}).Error)

	points, err := GetFinanceTrend(start, end, FinanceGranularityDay)
	require.NoError(t, err)
	require.Len(t, points, 3)
	assert.Equal(t, day.AddDate(0, 0, -1).Format("2006-01-02"), points[0].Period)
	assert.Equal(t, "0.00", points[0].Revenue)
	assert.Equal(t, day.Format("2006-01-02"), points[1].Period)
	assert.Equal(t, "10.30", points[1].Revenue)
	assert.Equal(t, "3.30", points[1].Cost)
	assert.Equal(t, "14.60", points[1].Billed)
	assert.Equal(t, "7.00", points[1].Net)
	assert.Equal(t, "0.00", points[2].Revenue)

	months, err := GetFinanceTrend(start, end, FinanceGranularityMonth)
	require.NoError(t, err)
	require.Len(t, months, 1)
	assert.Equal(t, "2026-11", months[0].Period)
	assert.Equal(t, "10.30", months[0].Revenue)
}

func TestFinancePeriodKeyUsesLocalTime(t *testing.T) {
	ts := time.Date(2026, 11, 10, 13, 0, 0, 0, time.Local).Unix()
	assert.Equal(t, "2026-11-10", FinancePeriodKey(ts, FinanceGranularityDay))
	assert.Equal(t, "2026-11", FinancePeriodKey(ts, FinanceGranularityMonth))
	assert.Equal(t, "2026-11-10", FinancePeriodKey(ts, "unknown"))
}
