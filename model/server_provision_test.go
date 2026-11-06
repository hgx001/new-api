/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting"
)

// paidAwaitingOrder 造一笔「支付宝已付款、等管理员分配」的订单。
func paidAwaitingOrder(t *testing.T, userId int, config setting.ServerConfig) *ServerOrder {
	t.Helper()
	quote := serverTestQuote(t, config)
	order, _, err := PurchaseServerInstanceWithProvider(
		userId, config, quote, "alipay", PaymentProviderAlipay, "",
	)
	require.NoError(t, err)
	completed, _, err := CompleteServerOrderPaid(
		order.TradeNo, PaymentProviderAlipay, order.Amount, "alipay",
	)
	require.NoError(t, err)
	require.True(t, completed)
	return order
}

func TestAssignServerOrderDeliversCredentialsAndStartsClock(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := gpuServerConfig("1mo")
	order := paidAwaitingOrder(t, user.Id, config)

	var pending ServerInstance
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&pending).Error)
	require.Equal(t, ServerInstanceStatusProvisioning, pending.Status)
	require.Zero(t, pending.ExpireAt)

	before := common.GetTimestamp()
	updatedOrder, instance, err := AssignServerOrder(order.TradeNo, 1, ServerAssignInput{
		IP:         "203.0.113.10",
		SshPort:    2222,
		Username:   "ubuntu",
		Password:   "s3cret-pass",
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----",
		Note:       "Ubuntu 22.04",
	})
	require.NoError(t, err)
	require.NotNil(t, instance)

	assert.Equal(t, ServerInstanceStatusRunning, instance.Status)
	assert.Equal(t, "203.0.113.10", instance.IP)
	assert.Equal(t, 2222, instance.SshPort)
	assert.Equal(t, "ubuntu", instance.Username)
	assert.Equal(t, "Ubuntu 22.04", instance.AdminNote)
	assert.Equal(t, 1, instance.AssignedBy)
	assert.Equal(t, ServerOrderProvisionAssigned, updatedOrder.ProvisionStatus)

	// 到期时间从分配这一刻起算，排队等待的时间不计费。
	// 允许几秒误差：now 在事务前取，分配动作本身也有耗时。
	assert.InDelta(
		t, pending.PurchasedSeconds, instance.ExpireAt-before, 5,
		"到期时间应为「分配时刻 + 已购时长」",
	)

	// 凭据必须是密文落库，但能解回明文。
	assert.NotEqual(t, "s3cret-pass", instance.Password)
	plain, err := common.DecryptSecret(instance.Password)
	require.NoError(t, err)
	assert.Equal(t, "s3cret-pass", plain)
	key, err := common.DecryptSecret(instance.PrivateKey)
	require.NoError(t, err)
	assert.Contains(t, key, "BEGIN OPENSSH PRIVATE KEY")

	// 用户视图里能看到凭据，且端口一并下发。
	view := instance.ToView(common.GetTimestamp())
	assert.True(t, view.PasswordReadable)
	assert.True(t, view.PrivateKeyReady)
	assert.Equal(t, 2222, view.SshPort)
	assert.Greater(t, view.RemainingSeconds, int64(0))

	// 订单侧也要落库，列表才不会一直显示待分配。
	var stored ServerOrder
	require.NoError(t, DB.Where("id = ?", order.Id).First(&stored).Error)
	assert.Equal(t, ServerOrderProvisionAssigned, stored.ProvisionStatus)
	assert.NotZero(t, stored.ProvisionedAt)
	assert.Equal(t, 1, stored.ProvisionedBy)

	// 操作记录留痕，用户能在「操作日志」里看到开通动作。
	var events []*ServerInstanceEvent
	require.NoError(t, DB.Where("instance_id = ?", instance.Id).Find(&events).Error)
	actions := make([]string, 0, len(events))
	for _, event := range events {
		actions = append(actions, event.Action)
	}
	assert.Contains(t, actions, "assign")
}

func TestAssignServerOrderRejectsDoubleAssignment(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	order := paidAwaitingOrder(t, user.Id, config)

	input := ServerAssignInput{
		IP:       "203.0.113.11",
		Username: "root",
		Password: "first-password",
	}
	_, _, err := AssignServerOrder(order.TradeNo, 1, input)
	require.NoError(t, err)

	// 第二个管理员又点了一次：不能再写一遍凭据。
	_, _, err = AssignServerOrder(order.TradeNo, 1, ServerAssignInput{
		IP:       "198.51.100.9",
		Username: "root",
		Password: "second-password",
	})
	require.ErrorIs(t, err, ErrServerOrderNotAwaiting)

	var instance ServerInstance
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&instance).Error)
	assert.Equal(t, "203.0.113.11", instance.IP, "重复分配不得覆盖已交付的凭据")
}

func TestAssignServerOrderValidatesInput(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	order := paidAwaitingOrder(t, user.Id, config)

	cases := []struct {
		name  string
		input ServerAssignInput
	}{
		{"IP 非法", ServerAssignInput{IP: "not-an-ip", Username: "root", Password: "x"}},
		{"端口越界", ServerAssignInput{IP: "203.0.113.1", SshPort: 70000, Username: "root", Password: "x"}},
		{"没有用户名", ServerAssignInput{IP: "203.0.113.1", Password: "x"}},
		{"密码与密钥都为空", ServerAssignInput{IP: "203.0.113.1", Username: "root"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := AssignServerOrder(order.TradeNo, 1, tc.input)
			require.ErrorIs(t, err, ErrServerAssignInvalid)
		})
	}
}

func TestAssignServerOrderRejectsUnpaidOrder(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	// 只下单不付款：不允许提前分配。
	order, _, err := PurchaseServerInstanceWithProvider(
		user.Id, config, quote, "alipay", PaymentProviderAlipay, "",
	)
	require.NoError(t, err)
	_, _, err = AssignServerOrder(order.TradeNo, 1, ServerAssignInput{
		IP: "203.0.113.12", Username: "root", Password: "x",
	})
	require.ErrorIs(t, err, ErrServerOrderNotAwaiting)
}

func TestReleaseProvisioningInstanceRefundsInFull(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)

	// 用余额购买但停在待分配：等价于「付了钱、机器还没给」。
	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)
	require.Equal(t, ServerInstanceStatusProvisioning, instance.Status)

	refundOrder, released, err := ReleaseServerInstance(user.Id, instance.Id)
	require.NoError(t, err)
	require.NotNil(t, refundOrder)
	// 等待时间不扣钱：实付多少退多少。
	assert.Equal(t, quote.Total, released.RefundedAmount)

	// 订单必须退出待分配队列，否则管理员会看到一条点了必然失败的幽灵待办。
	var order ServerOrder
	require.NoError(t, DB.Where("instance_id = ? AND type = ?",
		instance.Id, ServerOrderTypePurchase).First(&order).Error)
	assert.Equal(t, ServerOrderProvisionCanceled, order.ProvisionStatus)
}

// 未分配的实例不能被续费：续费会把状态直接改成 running，
// 用户拿到一台既没有凭据、也再分配不出去的空壳机器。
func TestRenewServerInstanceRejectsProvisioning(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)
	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)
	require.Equal(t, ServerInstanceStatusProvisioning, instance.Status)

	_, _, err = RenewServerInstance(user.Id, instance.Id, quote, PaymentMethodBalance, "")
	require.ErrorIs(t, err, ErrServerInstancePending)

	var after ServerInstance
	require.NoError(t, DB.First(&after, instance.Id).Error)
	assert.Equal(t, ServerInstanceStatusProvisioning, after.Status)
	assert.Zero(t, after.ExpireAt)
}

func TestExpireServerInstancesSkipsProvisioning(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	order := paidAwaitingOrder(t, user.Id, config)

	// 一年后再扫描：待分配的机器还没开始计时，不能被判过期。
	expired, released, err := ExpireServerInstances(common.GetTimestamp() + 365*24*3600)
	require.NoError(t, err)
	assert.Zero(t, expired)
	assert.Zero(t, released)

	var instance ServerInstance
	require.NoError(t, DB.Where("order_id = ?", order.Id).First(&instance).Error)
	assert.Equal(t, ServerInstanceStatusProvisioning, instance.Status)
}

func TestSetServerInstancePowerRejectsProvisioning(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)
	config := liteServerConfig("1mo")
	quote := serverTestQuote(t, config)
	_, instance, err := PurchaseServerInstance(user.Id, config, quote, PaymentMethodBalance, "")
	require.NoError(t, err)

	_, err = SetServerInstancePower(user.Id, instance.Id, true)
	require.ErrorIs(t, err, ErrServerInstancePending)
}

func TestListServerProvisionOrdersShowsWaitingFirst(t *testing.T) {
	truncateTables(t)
	user := createServerTestUser(t, 100_000_000)

	first := paidAwaitingOrder(t, user.Id, liteServerConfig("1mo"))
	second := paidAwaitingOrder(t, user.Id, liteServerConfig("3mo"))

	orders, err := ListServerProvisionOrders(ServerOrderProvisionAwaiting, 10)
	require.NoError(t, err)
	require.Len(t, orders, 2)
	// 先付的先办：按支付时间升序
	assert.Equal(t, first.TradeNo, orders[0].TradeNo)
	assert.Equal(t, second.TradeNo, orders[1].TradeNo)
	assert.Equal(t, user.Username, orders[0].Username)
	assert.Equal(t, "lite", orders[0].Plan)

	awaiting, _, err := ServerProvisionSummary()
	require.NoError(t, err)
	assert.Equal(t, int64(2), awaiting)

	// 分配掉一单：待办数量减少，已交付列表出现一条。
	_, _, err = AssignServerOrder(first.TradeNo, 1, ServerAssignInput{
		IP: "203.0.113.20", Username: "root", Password: "x",
	})
	require.NoError(t, err)

	awaiting, _, err = ServerProvisionSummary()
	require.NoError(t, err)
	assert.Equal(t, int64(1), awaiting)

	assigned, err := ListServerProvisionOrders(ServerOrderProvisionAssigned, 10)
	require.NoError(t, err)
	require.Len(t, assigned, 1)
	assert.Equal(t, first.TradeNo, assigned[0].TradeNo)
}
