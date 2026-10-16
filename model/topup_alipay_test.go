package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAlipayTopUpTestState(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM users").Error)
	require.NoError(t, DB.Exec("DELETE FROM top_ups").Error)
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}

func TestRechargeAlipayCreditsOnceAndRecordsTopUp(t *testing.T) {
	setupAlipayTopUpTestState(t)

	user := User{
		Id:       1,
		Username: "alipay-user",
		AffCode:  "alipay-user",
		Password: "password",
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(&user).Error)
	topUp := TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1ALIPAYTEST",
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	require.NoError(t, RechargeAlipay(topUp.TradeNo, decimal.NewFromFloat(73), "127.0.0.1"))

	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)
	var gotTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", topUp.TradeNo).First(&gotTopUp).Error)
	assert.Equal(t, common.TopUpStatusSuccess, gotTopUp.Status)

	// Alipay may retry the same notification; it must not credit twice.
	require.NoError(t, RechargeAlipay(topUp.TradeNo, decimal.NewFromFloat(73), "127.0.0.1"))
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota)
}

func TestRechargeAlipayRejectsAmountMismatch(t *testing.T) {
	setupAlipayTopUpTestState(t)

	user := User{Id: 1, Username: "alipay-mismatch", AffCode: "alipay-mismatch", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	topUp := TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1ALIPAYMISMATCH",
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	require.Error(t, RechargeAlipay(topUp.TradeNo, decimal.NewFromFloat(72.99), "127.0.0.1"))
	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 0, gotUser.Quota)
	var gotTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", topUp.TradeNo).First(&gotTopUp).Error)
	assert.Equal(t, common.TopUpStatusPending, gotTopUp.Status)
}
