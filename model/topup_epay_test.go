package model

import (
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func insertEpayTopUpForTest(t *testing.T, tradeNo string, provider string) *TopUp {
	t.Helper()
	user := User{
		Id:       2,
		Username: "epay-user-" + tradeNo,
		AffCode:  "epay-" + tradeNo,
		Password: "password",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	topUp := TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         tradeNo,
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: provider,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&topUp).Error)
	return &topUp
}

// TestCompleteEpayTopUpCreditsOnce 易支付回调只入账一次（重放不重复加额度）。
func TestCompleteEpayTopUpCreditsOnce(t *testing.T) {
	setupAlipayTopUpTestState(t)
	topUp := insertEpayTopUpForTest(t, "EPAY1", PaymentProviderEpay)

	credited, err := CompleteEpayTopUp(topUp.TradeNo, PaymentMethodAlipay)
	require.NoError(t, err)
	require.True(t, credited)

	var gotUser User
	require.NoError(t, DB.First(&gotUser, topUp.UserId).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)

	// 同一笔回调重放：幂等返回，不再加额度
	credited, err = CompleteEpayTopUp(topUp.TradeNo, PaymentMethodAlipay)
	require.NoError(t, err)
	assert.False(t, credited)
	require.NoError(t, DB.First(&gotUser, topUp.UserId).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)
}

// TestCompleteEpayTopUpRejectsOtherProvider 防止跨网关回调拿别人的订单入账。
func TestCompleteEpayTopUpRejectsOtherProvider(t *testing.T) {
	setupAlipayTopUpTestState(t)
	topUp := insertEpayTopUpForTest(t, "EPAY2", PaymentProviderAlipay)

	credited, err := CompleteEpayTopUp(topUp.TradeNo, PaymentMethodAlipay)
	require.ErrorIs(t, err, ErrPaymentMethodMismatch)
	assert.False(t, credited)

	var gotUser User
	require.NoError(t, DB.First(&gotUser, topUp.UserId).Error)
	assert.Equal(t, 0, gotUser.Quota)
}

// TestCompleteEpayTopUpConcurrentCallbacksCreditOnce 并发回调只入账一次。
func TestCompleteEpayTopUpConcurrentCallbacksCreditOnce(t *testing.T) {
	setupAlipayTopUpTestState(t)
	topUp := insertEpayTopUpForTest(t, "EPAY3", PaymentProviderEpay)

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
			results[idx], errs[idx] = CompleteEpayTopUp(topUp.TradeNo, PaymentMethodAlipay)
		}(i)
	}
	close(start)
	wg.Wait()

	creditedCount := 0
	for i := range results {
		assert.NoError(t, errs[i], "worker %d", i)
		if results[i] {
			creditedCount++
		}
	}
	assert.Equal(t, 1, creditedCount, "并发回调只能有一个拿到入账权")

	var gotUser User
	require.NoError(t, DB.First(&gotUser, topUp.UserId).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)
}
