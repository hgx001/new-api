package model

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupVpnCredentialDB(t *testing.T) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&VpnOrder{}, &VpnCredential{}, &VpnCredentialAccess{}))
	oldDB := DB
	DB = db
	t.Cleanup(func() { DB = oldDB })
}

func newVpnTestOrder(t *testing.T, tradeNo string, status string, completeTime int64) *VpnOrder {
	t.Helper()
	o := &VpnOrder{
		UserId: 61, TradeNo: tradeNo, Plan: "year", Money: 199,
		PaymentMethod: PaymentMethodAlipay, PaymentProvider: PaymentProviderAlipay,
		CreateTime: time.Now().Unix(), Status: status, CompleteTime: completeTime,
	}
	require.NoError(t, CreateVpnOrderWithCredential(o))
	return o
}

// TestCreateVpnOrderWithCredentialIsAtomic 下单必须同时拿到订单、订阅令牌和节点凭据。
func TestCreateVpnOrderWithCredentialIsAtomic(t *testing.T) {
	setupVpnCredentialDB(t)
	o := newVpnTestOrder(t, "VPN-NEW-1", common.TopUpStatusPending, 0)
	require.NotEmpty(t, o.SubToken)
	require.Greater(t, o.Id, 0)

	cred, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	assert.Equal(t, VpnCredentialActive, cred.Status)
	assert.Equal(t, fmt.Sprintf("v%d", cred.Id), cred.Name)
	assert.Len(t, strings.Split(cred.Uuid, "-"), 5)
	assert.Empty(t, o.NodeKey, "新订单不能再持有共享密钥")

	byToken, err := GetVpnOrderBySubToken(o.SubToken)
	require.NoError(t, err)
	assert.Equal(t, o.Id, byToken.Id)
}

// TestVpnCredentialAbuseDetection 超过阈值的不同源 IP 才冻结，重复 IP 不累加。
func TestVpnCredentialAbuseDetection(t *testing.T) {
	setupVpnCredentialDB(t)
	o := newVpnTestOrder(t, "VPN-NEW-2", common.TopUpStatusSuccess, time.Now().Unix())
	cred, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	now := time.Now().Unix()

	for i := 1; i <= 3; i++ {
		ip := fmt.Sprintf("10.0.0.%d", i)
		require.NoError(t, RecordVpnCredentialAccess(cred, ip, VpnAccessSourceNode, now))
	}
	require.NoError(t, RecordVpnCredentialAccess(cred, "10.0.0.1", VpnAccessSourceNode, now+1))
	frozen, count, err := EvaluateVpnCredentialAbuse(cred, VpnAccessSourceNode, 3, now+1)
	require.NoError(t, err)
	assert.False(t, frozen)
	assert.Equal(t, int64(3), count)

	require.NoError(t, RecordVpnCredentialAccess(cred, "203.0.113.9", VpnAccessSourceNode, now+2))
	frozen, count, err = EvaluateVpnCredentialAbuse(cred, VpnAccessSourceNode, 3, now+2)
	require.NoError(t, err)
	assert.True(t, frozen)
	assert.Equal(t, int64(4), count)

	got, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	assert.Equal(t, VpnCredentialFrozen, got.Status)
}

// TestRotateVpnCredentialKeepsSubscription 重置换 UUID，但订阅地址不变，旧凭据作废。
func TestRotateVpnCredentialKeepsSubscription(t *testing.T) {
	setupVpnCredentialDB(t)
	o := newVpnTestOrder(t, "VPN-NEW-3", common.TopUpStatusSuccess, time.Now().Unix())
	old, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)

	next, err := RotateVpnCredential(old)
	require.NoError(t, err)
	assert.NotEqual(t, old.Uuid, next.Uuid)
	assert.NotEqual(t, old.Name, next.Name)
	assert.Equal(t, o.SubToken, newVpnTestOrderToken(t, o.Id))

	stale, err := GetVpnCredentialByName(old.Name)
	require.NoError(t, err)
	assert.Equal(t, VpnCredentialRevoked, stale.Status)

	current, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	assert.Equal(t, next.Id, current.Id)
	assert.Equal(t, VpnCredentialActive, current.Status)

	_, err = RotateVpnCredential(old)
	assert.Error(t, err, "同一条旧凭据不能被重复重置")
}

func newVpnTestOrderToken(t *testing.T, id int) string {
	t.Helper()
	var o VpnOrder
	require.NoError(t, DB.Where("id = ?", id).First(&o).Error)
	return o.SubToken
}

// TestListVpnNodeManifestFiltersInactiveOrders 只有已支付且未过期的 active 凭据才下发节点。
func TestListVpnNodeManifestFiltersInactiveOrders(t *testing.T) {
	setupVpnCredentialDB(t)
	now := time.Now().Unix()
	cutoff := now - 365*86400

	paid := newVpnTestOrder(t, "VPN-PAID", common.TopUpStatusSuccess, now)
	pending := newVpnTestOrder(t, "VPN-PENDING", common.TopUpStatusPending, 0)
	expired := newVpnTestOrder(t, "VPN-EXPIRED", common.TopUpStatusSuccess, cutoff-10)

	paidCred, err := GetVpnCredentialByOrder(paid.Id)
	require.NoError(t, err)
	pendingCred, err := GetVpnCredentialByOrder(pending.Id)
	require.NoError(t, err)
	_, err = GetVpnCredentialByOrder(expired.Id)
	require.NoError(t, err)

	manifest, err := ListVpnNodeManifest(cutoff, now)
	require.NoError(t, err)
	require.Len(t, manifest.Users, 1)
	assert.Equal(t, paidCred.Name, manifest.Users[0].Name)
	assert.Equal(t, paidCred.Uuid, manifest.Users[0].Uuid)
	for _, user := range manifest.Users {
		assert.NotEqual(t, pendingCred.Name, user.Name, "未支付订单的凭据不能下发节点")
	}

	_, err = FreezeVpnCredential(paidCred, now)
	require.NoError(t, err)
	manifest, err = ListVpnNodeManifest(cutoff, now)
	require.NoError(t, err)
	assert.Empty(t, manifest.Users)
	assert.Contains(t, manifest.Frozen, paidCred.Name)
}

// TestGetVpnCredentialInfosAggregatesIPs 管理端列表按订单聚合 24h 不同源 IP 数，
// 只统计节点侧真实连接 IP，订阅拉取 IP 不算 —— 与冻结判定口径保持一致。
func TestGetVpnCredentialInfosAggregatesIPs(t *testing.T) {
	setupVpnCredentialDB(t)
	now := time.Now().Unix()
	o := newVpnTestOrder(t, "VPN-ADMIN", common.TopUpStatusSuccess, now)
	cred, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	require.NoError(t, RecordVpnCredentialAccess(cred, "1.1.1.1", VpnAccessSourceSub, now))
	require.NoError(t, RecordVpnCredentialAccess(cred, "2.2.2.2", VpnAccessSourceNode, now))
	require.NoError(t, RecordVpnCredentialAccess(cred, "2.2.2.2", VpnAccessSourceNode, now+1))

	infos := GetVpnCredentialInfos([]int{o.Id}, now+2)
	assert.Equal(t, int64(1), infos[o.Id].IPCount)
	assert.Equal(t, cred.Name, infos[o.Id].Name)
	assert.Equal(t, VpnCredentialActive, infos[o.Id].Status)
}

// TestVpnSubscriptionFetchIPsNeverFreeze 订阅拉取换网络不应冻结凭据：
// 只有真实连上节点的不同出口 IP 才算共享证据。
func TestVpnSubscriptionFetchIPsNeverFreeze(t *testing.T) {
	setupVpnCredentialDB(t)
	now := time.Now().Unix()
	o := newVpnTestOrder(t, "VPN-SUB-IPS", common.TopUpStatusSuccess, now)
	cred, err := GetVpnCredentialByOrder(o.Id)
	require.NoError(t, err)
	for i := 1; i <= 6; i++ {
		require.NoError(t, RecordVpnCredentialAccess(cred, fmt.Sprintf("198.51.100.%d", i), VpnAccessSourceSub, now))
	}
	count, err := CountVpnCredentialIPs(cred.Id, VpnAccessSourceSub, now)
	require.NoError(t, err)
	assert.Equal(t, int64(6), count)

	frozen, nodeCount, err := EvaluateVpnCredentialAbuse(cred, VpnAccessSourceNode, 3, now)
	require.NoError(t, err)
	assert.False(t, frozen, "订阅拉取 IP 不参与共享判定")
	assert.Equal(t, int64(0), nodeCount)
}
