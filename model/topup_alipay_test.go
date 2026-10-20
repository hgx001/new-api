package model

import (
	"sync"
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

// TestRechargeAlipayConcurrentCallbacksCreditOnce 锁定「同一笔订单的并发通知只能入账一次」。
//
// 威胁模型：支付宝的异步通知报文是**可重放的合法签名**（签名无法伪造，但报文可以重放），
// 攻击者可以把自己那笔合法通知并发重放多次。若状态流转不是原子的（先读 pending 再写
// success），多个并发事务会同时通过状态检查，各自给用户加一次额度。
//
// 防线：model 层「行锁（clause.Locking）+ 条件更新 CAS（RowsAffected 判定）」，
// controller 层还有进程内 LockOrder。
//
// 注意：单测用 SQLite（MaxOpenConns=1，串行执行 + 驱动不支持行锁），因此这里断言的是
// 「并发调用后只入账一次、只记一条充值日志」这一**不变量**；真正的行锁语义要在
// PostgreSQL 上验证（生产就是 PG）。
func TestRechargeAlipayConcurrentCallbacksCreditOnce(t *testing.T) {
	setupAlipayTopUpTestState(t)

	user := User{Id: 1, Username: "alipay-concurrent", AffCode: "alipay-concurrent", Password: "password", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	topUp := TopUp{
		UserId:          user.Id,
		Amount:          10,
		Money:           73,
		TradeNo:         "USR1ALIPAYCONCURRENT",
		PaymentMethod:   PaymentMethodAlipay,
		PaymentProvider: PaymentProviderAlipay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, DB.Create(&topUp).Error)

	const workers = 8
	var wg sync.WaitGroup
	errs := make([]error, workers)
	start := make(chan struct{})
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			errs[idx] = RechargeAlipay(topUp.TradeNo, decimal.NewFromFloat(73), "127.0.0.1")
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		assert.NoError(t, err, "worker %d 应幂等返回而不是报错", i)
	}

	var gotUser User
	require.NoError(t, DB.First(&gotUser, user.Id).Error)
	assert.Equal(t, 5_000_000, gotUser.Quota, "并发通知只能入账一次")

	var gotTopUp TopUp
	require.NoError(t, DB.Where("trade_no = ?", topUp.TradeNo).First(&gotTopUp).Error)
	assert.Equal(t, common.TopUpStatusSuccess, gotTopUp.Status)

	// 充值日志只应有一条：RecordTopupLog 只在真正加了额度时写
	var logCount int64
	require.NoError(t, DB.Model(&Log{}).Where("user_id = ? AND content LIKE ?", user.Id, "%支付宝官方直连充值成功%").Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount, "并发通知只能记一条充值日志")
}
