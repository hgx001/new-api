package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertVpnOrderForTest(t *testing.T, tradeNo string, provider string, money float64) *VpnOrder {
	t.Helper()
	order := VpnOrder{
		UserId:          1,
		TradeNo:         tradeNo,
		Plan:            "year",
		Money:           money,
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
		NodeKey:         "vless://test",
	}
	require.NoError(t, DB.Create(&order).Error)
	return &order
}

func setupVpnOrderTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM vpn_orders").Error)
}

// TestCompleteVpnOrderPaidCompletesOnce 正常回调把订单置成功；重放幂等、不重复置成功。
func TestCompleteVpnOrderPaidCompletesOnce(t *testing.T) {
	setupVpnOrderTestState(t)
	order := insertVpnOrderForTest(t, "VPN1ALIPAYTEST", PaymentProviderAlipay, 199.00)

	completed, err := CompleteVpnOrderPaid(
		order.TradeNo,
		PaymentProviderAlipay,
		decimal.NewFromFloat(199.00),
		common.GetTimestamp(),
		nil,
	)
	require.NoError(t, err)
	require.True(t, completed)

	got := GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Greater(t, got.CompleteTime, int64(0))

	// 同一笔通知重放：幂等返回，不再改写 complete_time
	completed, err = CompleteVpnOrderPaid(
		order.TradeNo,
		PaymentProviderAlipay,
		decimal.NewFromFloat(199.00),
		common.GetTimestamp()+100,
		nil,
	)
	require.NoError(t, err)
	assert.False(t, completed)

	got = GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
	assert.Less(t, got.CompleteTime, common.GetTimestamp()+100)
}

// TestCompleteVpnOrderPaidRejectsAmountMismatch 金额对不上绝不能置成功
// （签名只能证明报文来自网关，不能证明金额是 199 元）。
func TestCompleteVpnOrderPaidRejectsAmountMismatch(t *testing.T) {
	setupVpnOrderTestState(t)
	order := insertVpnOrderForTest(t, "VPN2ALIPAYCHEAP", PaymentProviderAlipay, 199.00)

	completed, err := CompleteVpnOrderPaid(
		order.TradeNo,
		PaymentProviderAlipay,
		decimal.NewFromFloat(0.01),
		common.GetTimestamp(),
		nil,
	)
	require.Error(t, err)
	assert.False(t, completed)

	got := GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestCompleteVpnOrderPaidRejectsOtherProvider 防止跨通道回调拿别人的订单入账。
func TestCompleteVpnOrderPaidRejectsOtherProvider(t *testing.T) {
	setupVpnOrderTestState(t)
	order := insertVpnOrderForTest(t, "VPN3ALIPAYMISMATCH", PaymentProviderEpay, 199.00)

	completed, err := CompleteVpnOrderPaid(
		order.TradeNo,
		PaymentProviderAlipay,
		decimal.NewFromFloat(199.00),
		common.GetTimestamp(),
		nil,
	)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.False(t, completed)

	got := GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusPending, got.Status)
}

// TestCompleteVpnOrderPaidConcurrentCallbacksCompleteOnce 并发回调只有一个拿到转换权。
func TestCompleteVpnOrderPaidConcurrentCallbacksCompleteOnce(t *testing.T) {
	setupVpnOrderTestState(t)
	order := insertVpnOrderForTest(t, "VPN4ALIPAYCONCURRENT", PaymentProviderAlipay, 199.00)

	const workers = 8
	var wg sync.WaitGroup
	results := make([]bool, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			results[idx], errs[idx] = CompleteVpnOrderPaid(
				order.TradeNo,
				PaymentProviderAlipay,
				decimal.NewFromFloat(199.00),
				common.GetTimestamp(),
				nil,
			)
		}(i)
	}
	close(start)
	wg.Wait()

	completedCount := 0
	for i := range results {
		assert.NoError(t, errs[i], "worker %d", i)
		if results[i] {
			completedCount++
		}
	}
	assert.Equal(t, 1, completedCount, "并发回调只能有一个拿到转换权")

	got := GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
}

// TestCompleteVpnOrderPaidWritesExtraUpdates 回写网关回传的实际支付方式。
func TestCompleteVpnOrderPaidWritesExtraUpdates(t *testing.T) {
	setupVpnOrderTestState(t)
	order := insertVpnOrderForTest(t, "VPN5NOALIPAY", PaymentProviderEpay, 199.00)

	completed, err := CompleteVpnOrderPaid(
		order.TradeNo,
		PaymentProviderEpay,
		decimal.NewFromFloat(199.00),
		common.GetTimestamp(),
		map[string]interface{}{"payment_method": "alipay"},
	)
	require.NoError(t, err)
	require.True(t, completed)

	got := GetVpnOrderByTradeNo(order.TradeNo)
	require.NotNil(t, got)
	assert.Equal(t, "alipay", got.PaymentMethod)
	assert.Equal(t, common.TopUpStatusSuccess, got.Status)
}
