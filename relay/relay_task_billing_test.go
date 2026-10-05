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
package relay

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// 任务预扣费的 OtherRatios 必须与 map 迭代顺序无关：wan3.0-smart 1080p/5 秒
// 曾经因「边遍历边 int()」出现 102735 与 102739 两个扣费值（2026-10-05 生产实测）。
func TestApplyTaskOtherRatiosIsOrderIndependent(t *testing.T) {
	// 生产真实基数：int(0.038356164383561646 * 500000) = 19178（480P 基准 ¥0.28/秒）
	const baseQuota = 19178
	// wan3.0-smart 1080p：size = 0.30/0.28
	ratios := map[string]float64{
		"seconds": 5,
		"size":    0.30 / 0.28,
	}

	// 精确值 19178*5*(0.30/0.28) = 102739.2857…，单次截断 = 102739。
	require.Equal(t, 102739, applyTaskOtherRatios(baseQuota, ratios))

	// 钉死两种遍历顺序的逐项截断结果：size 先截断会少扣 4 quota，
	// 这正是 2026-10-05 生产上「同一请求两次扣费不同」的来源。
	secondsFirst := int(float64(int(float64(baseQuota)*ratios["seconds"])) * ratios["size"])
	sizeFirst := int(float64(int(float64(baseQuota)*ratios["size"])) * ratios["seconds"])
	require.Equal(t, 102739, secondsFirst, "seconds 先截断恰好等于单次截断")
	require.Equal(t, 102735, sizeFirst, "size 先截断会少扣 4 quota —— 修复前会出现这个值")
	require.NotEqual(t, secondsFirst, sizeFirst, "本测试的前提：两种顺序本就不同，否则回归测不出问题")
}

// 逐项截断的误差会逐级放大，所以单次累乘的结果不能等于任何一种「先截断」的结果，
// 也不能比它离精确值更远。
func TestApplyTaskOtherRatiosAvoidsCompoundedTruncation(t *testing.T) {
	const baseQuota = 19178

	exact := float64(baseQuota) * 5 * (0.30 / 0.28)
	require.InDelta(t, 102739.2857, exact, 0.001, "先固定精确值，避免以后改测试时对不上账")
	require.Equal(t, int(exact), applyTaskOtherRatios(baseQuota, map[string]float64{
		"seconds": 5,
		"size":    0.30 / 0.28,
	}))
	require.Less(t, math.Abs(float64(102739)-exact), math.Abs(float64(102735)-exact),
		"单次累乘必须比逐项截断更接近精确值")
}

func TestApplyTaskOtherRatiosEdgeCases(t *testing.T) {
	require.Equal(t, 500, applyTaskOtherRatios(500, nil), "nil 倍率表保持原值")
	require.Equal(t, 500, applyTaskOtherRatios(500, map[string]float64{}))
	require.Equal(t, 500, applyTaskOtherRatios(500, map[string]float64{"size": 1.0}), "全 1.0 不应改变额度")
	require.Equal(t, 1000, applyTaskOtherRatios(500, map[string]float64{"n": 2}), "Nano Banana Pro 的张数倍率")
}
