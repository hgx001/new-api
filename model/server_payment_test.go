package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// 第三方支付的模型层测试。
//
// 这一段是整条支付链路最关键的部分：订单落库 → 回调开通。
// 出错的后果是真金白银 —— 要么「付了钱没发货」，要么「没付钱发了货」。

func TestPurchaseServerWithEpayDoesNotChargeOrProvision(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)
	required := setting.ServerQuotaForAmount(quote.Total)

	order, instance, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-1",
	)
	require.NoError(t, err)
	require.NotNil(t, order)
	// 第三方支付是异步的：下单这一刻不能有实例，也**不能扣额度**。
	assert.Nil(t, instance)
	assert.Equal(t, common.TopUpStatusPending, order.Status)
	assert.Equal(t, PaymentProviderEpay, order.PaymentProvider)
	assert.Equal(t, 0, order.QuotaDelta)

	var after User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&after).Error)
	assert.Equal(t, 100_000_000, after.Quota, "下单阶段不能扣额度")

	// 订单必须带上配置快照：回调时才知道要开什么机器。
	assert.NotEmpty(t, order.Config)
	assert.NotEmpty(t, order.Spec)
	assert.Equal(t, quote.UnitPrice, order.UnitPrice)
	assert.Equal(t, quote.PeriodSeconds, order.PeriodSeconds)
	assert.NotZero(t, order.TradeNoExpires)
	_ = required
}

func TestCompleteServerOrderPaidProvisionsInstance(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := gpuServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-2",
	)
	require.NoError(t, err)

	completed, instance, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, order.Amount, "alipay",
	)
	require.NoError(t, err)
	assert.True(t, completed)
	require.NotNil(t, instance)

	assert.Equal(t, ServerInstanceStatusRunning, instance.Status)
	assert.Equal(t, order.Id, instance.OrderId)
	assert.NotEmpty(t, instance.Name)
	assert.Equal(t, quote.Total, instance.PaidAmount)
	assert.Equal(t, quote.PeriodSeconds, instance.PurchasedSeconds)
	// 密码是密文存的，不是明文。
	assert.NotEmpty(t, instance.Password)
	plain, err := common.DecryptSecret(instance.Password)
	require.NoError(t, err)
	assert.Len(t, plain, 20)

	// 订单回写实例信息
	var updated ServerOrder
	require.NoError(t, DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusSuccess, updated.Status)
	assert.Equal(t, instance.Id, updated.InstanceId)
	assert.Equal(t, instance.Name, updated.InstanceName)
}

func TestCompleteServerOrderPaidIsIdempotent(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-3",
	)
	require.NoError(t, err)

	completed, first, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, order.Amount, "alipay",
	)
	require.NoError(t, err)
	assert.True(t, completed)
	require.NotNil(t, first)

	// 网关重放同一笔通知：不能再开一台机器。
	completed, second, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, order.Amount, "alipay",
	)
	require.NoError(t, err)
	assert.False(t, completed, "重复回调不得再开一台")
	require.NotNil(t, second)
	assert.Equal(t, first.Id, second.Id)

	var count int64
	require.NoError(t, DB.Model(&ServerInstance{}).Where("order_id = ?", order.Id).Count(&count).Error)
	assert.Equal(t, int64(1), count, "同一订单只能有一台实例")
}

func TestCompleteServerOrderPaidRejectsWrongAmount(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-4",
	)
	require.NoError(t, err)

	// 少付钱：必须拒绝，且不能开机器。
	completed, instance, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, 0.01, "alipay",
	)
	require.Error(t, err)
	assert.False(t, completed)
	assert.Nil(t, instance)

	var count int64
	require.NoError(t, DB.Model(&ServerInstance{}).Count(&count).Error)
	assert.Equal(t, int64(0), count, "金额不符绝不能发货")
}

func TestCompleteServerOrderPaidRejectsProviderMismatch(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-5",
	)
	require.NoError(t, err)

	// 订单走的是易支付，别让别的网关回调把它入账。
	_, _, err = CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderStripe, order.Amount, "alipay",
	)
	require.ErrorIs(t, err, ErrServerPaymentProviderMismatch)
}

func TestCompleteServerOrderPaidRejectsExpiredOrder(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-6",
	)
	require.NoError(t, err)

	// 把订单改成已过期，再模拟迟到的回调。
	require.NoError(t, DB.Model(&ServerOrder{}).Where("id = ?", order.Id).
		Update("trade_no_expires", common.GetTimestamp()-10).Error)

	_, instance, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, order.Amount, "alipay",
	)
	require.ErrorIs(t, err, ErrServerOrderExpired)
	assert.Nil(t, instance, "过期订单不得发货")
}

func TestCompleteServerOrderPaidRejectsUnknownTradeNo(t *testing.T) {
	truncateTables(t)
	_, _, err := CompleteServerOrderPaid(
		"SRV-NOT-EXIST", PaymentProviderEpay, 10, "alipay",
	)
	require.ErrorIs(t, err, ErrServerOrderNotFound)
}

func TestExpireServerOrdersFailsPendingOrders(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-7",
	)
	require.NoError(t, err)

	// 未超时：不动它。
	expired, err := ExpireServerOrders(common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, int64(0), expired)

	// 超时后作废。
	expired, err = ExpireServerOrders(common.GetTimestamp() + ServerOrderPaymentTimeout + 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), expired)

	var updated ServerOrder
	require.NoError(t, DB.Where("id = ?", order.Id).First(&updated).Error)
	assert.Equal(t, common.TopUpStatusFailed, updated.Status)

	// 作废后的回调不得发货。
	_, instance, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderEpay, order.Amount, "alipay",
	)
	require.Error(t, err)
	assert.Nil(t, instance)
}

func TestEpayPurchaseKeepsIdempotency(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	first, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-8",
	)
	require.NoError(t, err)

	// 同一个 client_request_id 重放：必须拿到同一张订单，
	// 不能产生第二张 pending 订单（否则用户会被收两次钱）。
	second, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderEpay, "req-epay-8",
	)
	require.NoError(t, err)
	assert.Equal(t, first.Id, second.Id)
	assert.Equal(t, first.TradeNo, second.TradeNo)

	var count int64
	require.NoError(t, DB.Model(&ServerOrder{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
