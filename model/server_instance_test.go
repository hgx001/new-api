package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

func createServerTestUser(t *testing.T, quota int) *User {
	t.Helper()
	user := &User{
		Username: "server_test_user",
		Status:   common.UserStatusEnabled,
		Quota:    quota,
	}
	require.NoError(t, DB.Create(user).Error)
	t.Cleanup(func() {
		DB.Exec("DELETE FROM users WHERE id = ?", user.Id)
	})
	return user
}

func serverTestQuote(t *testing.T, config setting.ServerConfig) *setting.ServerQuote {
	t.Helper()
	quote, err := setting.ComputeServerQuote(config)
	require.NoError(t, err)
	return quote
}

func liteServerConfig(durationId string) setting.ServerConfig {
	return setting.ServerConfig{
		Region:     "guangzhou",
		Plan:       "lite",
		SpecId:     "lite-2c2g",
		Mbps:       5,
		DurationId: durationId,
	}
}

func gpuServerConfig(durationId string) setting.ServerConfig {
	return setting.ServerConfig{
		Region:     "guangzhou",
		Plan:       "gpu",
		SpecId:     "gpu-24c96g-rtxpro6000",
		Mbps:       20,
		DurationId: durationId,
	}
}

func TestPurchaseServerInstanceChargesOnceAndIsIdempotent(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)
	required := setting.ServerQuotaForAmount(quote.Total)

	order, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "req-purchase-1")
	require.NoError(t, err)
	require.NotNil(t, order)
	require.NotNil(t, instance)

	var afterPurchase User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&afterPurchase).Error)
	assert.Equal(t, 100_000_000-required, afterPurchase.Quota, "必须且只能扣一次额度")

	assert.Equal(t, ServerInstanceStatusRunning, instance.Status)
	assert.Equal(t, common.TopUpStatusSuccess, order.Status)
	assert.Equal(t, int64(30*24*3600), instance.ExpireAt-instance.CreatedAt)

	view := instance.ToView(common.GetTimestamp())
	assert.True(t, view.PasswordReadable, "密码必须能解密回显")
	assert.GreaterOrEqual(t, len(view.Password), 8)
	assert.Equal(t, "lite-2c2g", view.Spec.Id)
	assert.Equal(t, 2, view.Spec.CPU)

	// 同一请求号重放（双击、网络重试）：返回同一台实例，不再扣钱。
	againOrder, againInstance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "req-purchase-1")
	require.NoError(t, err)
	assert.Equal(t, order.Id, againOrder.Id)
	assert.Equal(t, instance.Id, againInstance.Id)

	var afterRetry User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&afterRetry).Error)
	assert.Equal(t, afterPurchase.Quota, afterRetry.Quota)

	var orderCount int64
	require.NoError(t, DB.Model(&ServerOrder{}).Count(&orderCount).Error)
	assert.Equal(t, int64(1), orderCount)
}

func TestPurchaseServerInstanceRejectsInsufficientBalance(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 1_000)
	config := gpuServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, _, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.ErrorIs(t, err, ErrServerQuotaInsufficient)

	var instanceCount int64
	require.NoError(t, DB.Model(&ServerInstance{}).Count(&instanceCount).Error)
	assert.Zero(t, instanceCount, "余额不足不能留下实例")
	var orderCount int64
	require.NoError(t, DB.Model(&ServerOrder{}).Count(&orderCount).Error)
	assert.Zero(t, orderCount, "余额不足不能留下订单")

	var current User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&current).Error)
	assert.Equal(t, 1_000, current.Quota)
}

func TestRenewServerInstanceExtendsExpiryAndCharges(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	renewQuote := serverTestQuote(t, config)
	order, renewed, err := RenewServerInstance(user.Id, instance.Id, renewQuote, PaymentMethodBalance, "req-renew-1")
	require.NoError(t, err)
	require.NotNil(t, order)
	assert.Equal(t, ServerOrderTypeRenew, order.Type)
	assert.Equal(t, instance.ExpireAt+renewQuote.PeriodSeconds, renewed.ExpireAt)

	var current User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&current).Error)
	expectedQuota := 100_000_000 - setting.ServerQuotaForAmount(quote.Total) - setting.ServerQuotaForAmount(renewQuote.Total)
	assert.Equal(t, expectedQuota, current.Quota)

	// 同一续费请求号重放：不重复延长、不重复扣款。
	_, _, err = RenewServerInstance(user.Id, instance.Id, renewQuote, PaymentMethodBalance, "req-renew-1")
	require.NoError(t, err)
	var afterRetry User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&afterRetry).Error)
	assert.Equal(t, current.Quota, afterRetry.Quota)
}

func TestReleaseServerInstanceRefundsRemainingValue(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	// 用掉一半租期再退：应退约一半实付，不能按标价全退。
	now := common.GetTimestamp()
	halfExpire := now + quote.PeriodSeconds/2
	require.NoError(t, DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("expire_at", halfExpire).Error)

	refundOrder, released, err := ReleaseServerInstance(user.Id, instance.Id)
	require.NoError(t, err)
	require.NotNil(t, refundOrder)
	assert.Equal(t, ServerInstanceStatusReleased, released.Status)
	assert.InDelta(t, quote.Total/2, -refundOrder.Amount, 0.5)

	var current User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&current).Error)
	charged := setting.ServerQuotaForAmount(quote.Total)
	refunded := setting.ServerQuotaForAmount(-refundOrder.Amount)
	assert.InDelta(t, 100_000_000-charged+refunded, current.Quota, 2)

	// 已释放的实例不能再释放一次，也不能开机。
	_, _, err = ReleaseServerInstance(user.Id, instance.Id)
	require.ErrorIs(t, err, ErrServerInstanceReleased)
	_, err = SetServerInstancePower(user.Id, instance.Id, true)
	require.ErrorIs(t, err, ErrServerInstanceReleased)
}

func TestReleaseExpiredInstanceRefundsNothing(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	now := common.GetTimestamp()
	require.NoError(t, DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("expire_at", now-10).Error)

	refundOrder, released, err := ReleaseServerInstance(user.Id, instance.Id)
	require.NoError(t, err)
	assert.Nil(t, refundOrder, "过期实例没有剩余价值，不应产生退款单")
	assert.Equal(t, ServerInstanceStatusReleased, released.Status)

	var current User
	require.NoError(t, DB.Select("quota").Where("id = ?", user.Id).First(&current).Error)
	assert.Equal(t, 100_000_000-setting.ServerQuotaForAmount(quote.Total), current.Quota)
}

func TestExpireServerInstancesMarksThenReleases(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	now := common.GetTimestamp()
	require.NoError(t, DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("expire_at", now-1).Error)

	expired, _, err := ExpireServerInstances(now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), expired)

	var afterExpire ServerInstance
	require.NoError(t, DB.Where("id = ?", instance.Id).First(&afterExpire).Error)
	assert.Equal(t, ServerInstanceStatusExpired, afterExpire.Status)

	// 宽限期内不释放，用户还能续费救回。
	_, released, err := ExpireServerInstances(now)
	require.NoError(t, err)
	assert.Zero(t, released)

	// 超过宽限期自动释放。
	require.NoError(t, DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("expire_at", now-ServerInstanceExpiryGraceSeconds-1).Error)
	_, released, err = ExpireServerInstances(now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), released)

	var afterRelease ServerInstance
	require.NoError(t, DB.Where("id = ?", instance.Id).First(&afterRelease).Error)
	assert.Equal(t, ServerInstanceStatusReleased, afterRelease.Status)
}

func TestSetServerInstancePowerRejectsExpired(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	stopped, err := SetServerInstancePower(user.Id, instance.Id, false)
	require.NoError(t, err)
	assert.Equal(t, ServerInstanceStatusStopped, stopped.Status)

	started, err := SetServerInstancePower(user.Id, instance.Id, true)
	require.NoError(t, err)
	assert.Equal(t, ServerInstanceStatusRunning, started.Status)

	require.NoError(t, DB.Model(&ServerInstance{}).Where("id = ?", instance.Id).
		Update("expire_at", common.GetTimestamp()-1).Error)
	_, err = SetServerInstancePower(user.Id, instance.Id, true)
	require.ErrorIs(t, err, ErrServerInstanceExpired)
}

func TestBuildServerMetricsMatchesSpecAndStatus(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 500_000_000)

	liteQuote := serverTestQuote(t, liteServerConfig("24h"))
	_, lite, err := PurchaseServerInstance(user.Id, liteServerConfig("24h"), liteQuote, PaymentMethodBalance, "")
	require.NoError(t, err)

	now := common.GetTimestamp()
	metrics := BuildServerMetrics(lite, now, 12)
	assert.False(t, metrics.HasGPU, "无显卡规格不能出现 GPU 使用率")
	assert.Zero(t, metrics.GPU)
	assert.Equal(t, 5, metrics.BandwidthMbps)
	for _, point := range metrics.Network {
		assert.LessOrEqual(t, point.Value, float64(5), "网络吞吐不能超过所购带宽")
		assert.GreaterOrEqual(t, point.Value, 0.0)
	}
	assert.Len(t, metrics.CPU, 12)

	// 同一 5 分钟窗口内重复读取必须完全一致（不能每次刷新都跳）。
	assert.Equal(t, metrics, BuildServerMetrics(lite, now, 12))

	gpuQuote := serverTestQuote(t, gpuServerConfig("24h"))
	_, gpuInstance, err := PurchaseServerInstance(user.Id, gpuServerConfig("24h"), gpuQuote, PaymentMethodBalance, "")
	require.NoError(t, err)
	gpuMetrics := BuildServerMetrics(gpuInstance, now, 12)
	assert.True(t, gpuMetrics.HasGPU)
	assert.Greater(t, gpuMetrics.GPU, 0.0)

	// 停机后除磁盘占用外全部归零。
	_, err = SetServerInstancePower(user.Id, gpuInstance.Id, false)
	require.NoError(t, err)
	require.NoError(t, DB.Where("id = ?", gpuInstance.Id).First(gpuInstance).Error)
	stoppedMetrics := BuildServerMetrics(gpuInstance, now, 12)
	assert.Zero(t, stoppedMetrics.GPU)
	assert.Equal(t, 26.0, stoppedMetrics.Temperature)
	for _, point := range stoppedMetrics.CPU {
		assert.Zero(t, point.Value)
	}
}

func TestResetServerInstancePasswordStoresCiphertext(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)
	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	_, err = ResetServerInstancePassword(user.Id, instance.Id, "short")
	require.Error(t, err)

	password, err := ResetServerInstancePassword(user.Id, instance.Id, "N3w-Passw0rd!")
	require.NoError(t, err)
	assert.Equal(t, "N3w-Passw0rd!", password)

	var stored ServerInstance
	require.NoError(t, DB.Where("id = ?", instance.Id).First(&stored).Error)
	assert.NotEqual(t, "N3w-Passw0rd!", stored.Password, "密码不能明文落库")
	view := stored.ToView(common.GetTimestamp())
	assert.Equal(t, "N3w-Passw0rd!", view.Password)
}
