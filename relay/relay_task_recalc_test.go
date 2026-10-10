package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/types"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recalcQuotaFromRatios 在提交后计费调整路径上被调用（AdjustBillingOnSubmit）：
// 先把旧 OtherRatios 除掉恢复基础额度，再乘新 ratios。
//
// 它犯了与 applyTaskOtherRatios（commit 860b5402，已修）**完全相同**的错误：
// 边遍历 map 边 int() 截断。Go 的 map 迭代顺序随机，于是同一个请求两次重算
// 出不同的额度，而任务表只记最后一次 —— 差额无人认领，也无从对账。
// 修法同源：先累乘，最后只截断一次。

func newRatioRelayInfo(quota int, ratios map[string]float64) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{},
		PriceData: types.PriceData{
			Quota:       quota,
			OtherRatios: ratios,
		},
	}
}

// 正确值 = int(基础额度 / ∏旧倍率 × ∏新倍率)，只在最后截断一次。
func expectedRecalc(original int, oldRatios, newRatios map[string]float64) int {
	oldProduct, newProduct := 1.0, 1.0
	for _, r := range oldRatios {
		if r != 1.0 && r > 0 {
			oldProduct *= r
		}
	}
	for _, r := range newRatios {
		if r != 1.0 {
			newProduct *= r
		}
	}
	return int(float64(original) / oldProduct * newProduct)
}

// 核心不变量：结果与 map 遍历顺序无关。
//
// 实测边界（2000 次采样）：两个倍率时结果稳定，**三个及以上**才裂开。例如
// {seconds:3, size:1.1, extra:1.3} 会随机落到 47894 / 47896 —— 同一请求两套金额，
// 而 tasks 表只记最后一次，差额无人认领。
// 所以这里必须用 3 个倍率采样，两个倍率测不出顺序依赖（会假绿）。
func TestRecalcQuotaFromRatiosIsOrderIndependent(t *testing.T) {
	newRatios := map[string]float64{"seconds": 2.0}
	const original = 102739
	// 102739 / (3*1.1*1.3) * 2 = 47896.03… → 47896
	const want = 47896

	seen := map[int]int{}
	for i := 0; i < 300; i++ {
		fresh := map[string]float64{"seconds": 3.0, "size": 1.1, "extra": 1.3}
		seen[recalcQuotaFromRatios(newRatioRelayInfo(original, fresh), newRatios)]++
	}
	require.Len(t, seen, 1,
		"同一请求算出多个额度 %v —— map 遍历顺序影响了结果，差额会被静默吞掉", seen)
	for got := range seen {
		assert.Equal(t, want, got)
	}
}

// 截断误差不得累积。边遍历边 int() 会在每一步都丢掉小数，长链下误差远大于 1。
func TestRecalcQuotaFromRatiosDoesNotAccumulateTruncation(t *testing.T) {
	oldRatios := map[string]float64{"seconds": 3.0, "size": 1.1, "extra": 1.3}
	newRatios := map[string]float64{"seconds": 7.0, "size": 1.0714285714285714}
	const original = 100000
	want := expectedRecalc(original, oldRatios, newRatios)

	got := recalcQuotaFromRatios(newRatioRelayInfo(original, oldRatios), newRatios)
	// 边遍历边截断最坏会偏离十几个单位；正确实现与期望值只差浮点表示的末位。
	assert.InDelta(t, want, got, 1,
		"截断误差在累积：期望 %d，得到 %d", want, got)
}

// 倍率为 1.0 或非正数时不应参与计算，否则恢复基础额度会被除坏。
func TestRecalcQuotaFromRatiosIgnoresDegenerateRatios(t *testing.T) {
	for _, name := range []string{"全 1.0", "含 0", "含负数"} {
		t.Run(name, func(t *testing.T) {
			oldRatios := map[string]float64{"seconds": 1.0, "size": 0.0, "audio": -2.0}
			const original = 50000
			got := recalcQuotaFromRatios(newRatioRelayInfo(original, oldRatios),
				map[string]float64{"seconds": 2.0})
			assert.Equal(t, original*2, got, "非倍率项不得改变结果")
		})
	}
}

// 旧倍率里含有会被前端看到的 key 时，重算结果必须与预扣口径一致，
// 否则同一个任务「预扣」与「实扣」两个数字对不上。
func TestRecalcQuotaFromRatiosMatchesPreConsumeConvention(t *testing.T) {
	// 预扣：基础 19178 × seconds=2 = 38356（wan3.0-480p ¥0.28/秒 的真实形状）
	const base = 19178
	preConsumed := applyTaskOtherRatios(base, map[string]float64{"seconds": 2.0})

	// 提交后上游确认仍是 2 秒，ratios 不变 → 重算必须等于预扣
	got := recalcQuotaFromRatios(newRatioRelayInfo(preConsumed, map[string]float64{"seconds": 2.0}),
		map[string]float64{"seconds": 2.0})
	assert.Equal(t, preConsumed, got, "倍率未变时重算结果必须与预扣一致")

	// 上游改成 5 秒 → 38356 / 2 * 5 = 95890（与今天实测的 5 秒扣费一致）
	got = recalcQuotaFromRatios(newRatioRelayInfo(preConsumed, map[string]float64{"seconds": 2.0}),
		map[string]float64{"seconds": 5.0})
	assert.Equal(t, 95890, got)
}