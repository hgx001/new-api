package service

import (
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 预扣费并发安全（2026-11-26 审计修复的回归测试）。
//
// 修复前：钱包预扣是「读余额 → 判断 → UPDATE quota = quota - X」，三步不原子，
// N 个并发请求都读到同一个余额时全部通过校验，余额被扣成负数（实测 8 并发、
// 初始 1000、单次 600 → 成功 8 个、余额 -3800）。
//
// 修复后：扣款走 `UPDATE ... WHERE quota >= ?` 条件更新，并发下只有一个能成功，
// 其余拿到 ErrInsufficientWalletQuota → 403。

func seedBillingUser(t *testing.T, quota int) int {
	t.Helper()
	truncate(t)
	u := &model.User{Username: "race", Password: "12345678", Quota: quota, Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, model.DB.Create(u).Error)
	return u.Id
}

func quotaOf(t *testing.T, id int) int {
	t.Helper()
	q, err := model.GetUserQuota(id, true)
	require.NoError(t, err)
	return q
}

func newTaskRelayInfo(uid int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId:          uid,
		ForcePreConsume: true, // 视频任务：不允许走「额度充足就免预扣」的信任旁路
		RequestId:       "race-" + strconv.Itoa(int(time.Now().UnixNano())),
		OriginModelName: "audit-model",
		UserSetting:     dto.UserSetting{BillingPreference: "wallet_only"},
		// 测试环境没调用 model 包的 initCol()（commonKeyCol 为空），令牌相关 SQL 会报语法错；
		// 这里只验证钱包预扣主干。
		IsPlayground: true,
	}
}

// TestWalletPreConsumeConcurrentNoOverdraft 并发提交只允许余额买得起的条数成功。
func TestWalletPreConsumeConcurrentNoOverdraft(t *testing.T) {
	const (
		initial = 1000 // 余额只够 1 条（单条 600）
		amount  = 600
		callers = 8
	)
	uid := seedBillingUser(t, initial)

	var (
		wg        sync.WaitGroup
		success   int64
		rejected  int64
		otherErr  int64
		start     = make(chan struct{})
		startOnce sync.Once
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			<-start
			apiErr := PreConsumeBilling(c, amount, newTaskRelayInfo(uid))
			switch {
			case apiErr == nil:
				atomic.AddInt64(&success, 1)
			case apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota:
				atomic.AddInt64(&rejected, 1)
			default:
				atomic.AddInt64(&otherErr, 1)
				t.Logf("意外错误: %v", apiErr)
			}
		}()
	}
	startOnce.Do(func() { close(start) })
	wg.Wait()

	final := quotaOf(t, uid)
	t.Logf("并发 %d 个请求、单次 %d、初始余额 %d → 成功 %d、余额不足 %d、其它错误 %d、最终余额 %d",
		callers, amount, initial, success, rejected, otherErr, final)

	assert.Equal(t, int64(1), success, "余额只够 1 条，只能有 1 个预扣成功")
	assert.Equal(t, int64(callers-1), rejected, "其余必须明确返回额度不足")
	assert.Zero(t, otherErr)
	assert.GreaterOrEqual(t, final, 0, "余额不得为负")
	assert.Equal(t, initial-amount, final)
}

// TestWalletPreConsumeAllowsWhatBalanceCovers 余额够 N 条就恰好放行 N 条。
func TestWalletPreConsumeAllowsWhatBalanceCovers(t *testing.T) {
	const (
		initial = 1000
		amount  = 300 // 只够 3 条（余 100）
		callers = 6
	)
	uid := seedBillingUser(t, initial)

	var wg sync.WaitGroup
	var success int64
	start := make(chan struct{})
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			<-start
			if apiErr := PreConsumeBilling(c, amount, newTaskRelayInfo(uid)); apiErr == nil {
				atomic.AddInt64(&success, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	final := quotaOf(t, uid)
	assert.Equal(t, int64(3), success, "1000 / 300 = 3 条")
	assert.Equal(t, 100, final)
}

// TestWalletSettleShortfallNoOverdraft 结算补扣：片已交付，能扣多少扣多少，不产生负余额。
func TestWalletSettleShortfallNoOverdraft(t *testing.T) {
	const (
		initial = 100
		delta   = 600
	)
	uid := seedBillingUser(t, initial)

	funding := &WalletFunding{userId: uid}
	require.NoError(t, funding.Settle(delta), "结算阶段不应因为余额不足而报错")

	assert.Equal(t, 0, quotaOf(t, uid), "余额被收干到 0，而不是扣成负数")
}

// TestWalletSettleFullChargeWhenAffordable 余额够就直接扣满。
func TestWalletSettleFullChargeWhenAffordable(t *testing.T) {
	const (
		initial = 1000
		delta   = 600
	)
	uid := seedBillingUser(t, initial)

	funding := &WalletFunding{userId: uid}
	require.NoError(t, funding.Settle(delta))
	assert.Equal(t, initial-delta, quotaOf(t, uid))
}

// TestWalletRefundStillWorks 退款路径不受影响（原路加回）。
func TestWalletRefundStillWorks(t *testing.T) {
	uid := seedBillingUser(t, 1000)

	funding := &WalletFunding{userId: uid}
	require.NoError(t, funding.PreConsume(400))
	assert.Equal(t, 600, quotaOf(t, uid))

	require.NoError(t, funding.Refund())
	assert.Equal(t, 1000, quotaOf(t, uid))
}

// TestWalletPreConsumeRejectsWhenBalanceBelowPrice 连一条都买不起时直接 403，不动余额。
func TestWalletPreConsumeRejectsWhenBalanceBelowPrice(t *testing.T) {
	uid := seedBillingUser(t, 100)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	apiErr := PreConsumeBilling(c, 600, newTaskRelayInfo(uid))
	require.NotNil(t, apiErr)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, 100, quotaOf(t, uid), "被拒时余额必须原封不动")
}

// TestChargeUserQuotaWithArrears 直接锁 model 层：扣不动就收干，返回欠费额度。
func TestChargeUserQuotaWithArrears(t *testing.T) {
	uid := seedBillingUser(t, 250)

	shortfall, err := model.ChargeUserQuotaWithArrears(uid, 1000)
	require.NoError(t, err)
	assert.Equal(t, 750, shortfall, "欠费 = 应收 - 实收")
	assert.Equal(t, 0, quotaOf(t, uid))

	// 余额已空：再结算全额欠费，余额仍是 0
	shortfall, err = model.ChargeUserQuotaWithArrears(uid, 500)
	require.NoError(t, err)
	assert.Equal(t, 500, shortfall)
	assert.Equal(t, 0, quotaOf(t, uid))
}
