package constant

// ════════════════════════════════════════════════════════════════════════════
// 按次固定档常量 —— 业务值的守卫
// ════════════════════════════════════════════════════════════════════════════
//
// 这道闸门守的是**业务契约本身**，不是代码结构：
// 「seedance2.5-活动 = ¥4.5/次、固定 30 秒、固定 720P」是卖出去的商业承诺，
// 客户按这个参数下单，改了就是改了价目表。
//
// 为什么要有这一层：派生关系（youzanwan3 / middleware 都从 constant 派生）
// 保证了三处**不会互相漂移**，但保证不了「三处一致地变成了错的值」——
// 比如有人把 30 改成 45，派生关系照样成立、前端交叉校验会跟着一起变绿，
// 而线上已经按 45 秒收 ¥4.5 了（成本 ¥6.75，直接倒挂）。
//
// 所以这里把 30 / 720P 当成**需要显式改测试才能改的常量**钉死。
// 真的要改（比如上游降价后想锁 45 秒），必须先改这个测试并说明理由。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSeedance25_固定档秒数是30 守的是「按次档就是 30 秒」。
//
// 数字本身有商业含义：
//   - 30 秒 × ¥0.15/秒 = ¥4.50，正好等于售价 → 这是「按 30 秒卖」不亏本的临界点；
//   - 改成 45 秒 → 成本 ¥6.75 > 售价 ¥4.50 → **每单倒挂 ¥2.25**；
//   - 改成 15 秒 → 客户要 30 秒的片却只出 15 秒 → 错交付 + 少收 ¥2.25。
//
// 两种改法都是事故，而且**都不报错**。
func TestSeedance25_固定档秒数是30(t *testing.T) {
	assert.Equal(t, 30, Seedance25PerCallFixedSeconds,
		"按次固定档的时长是 30 秒 —— "+
			"这是「¥4.5/次 ≈ 30 × ¥0.15/秒」的成本临界点，"+
			"改大会倒挂、改小会错交付。真要改请先改本测试并写明理由")
}

// TestSeedance25_固定档分辨率是720P 守的是「按次档锁 720P」。
//
// 上游 prime 的 1080P 成本是 720P 的 1.33 倍（¥0.20/秒 vs ¥0.15/秒），
// 卖同一个 ¥4.5 的价就在 1080P 上倒挂。
func TestSeedance25_固定档分辨率是720P(t *testing.T) {
	assert.Equal(t, "720P", Seedance25PerCallFixedResolution,
		"按次固定档锁 720P —— 1080P 成本是 720P 的 1.33 倍，同价会倒挂")
}

// TestSeedance25_固定档常量非零 守的是「别把常量清成 0」。
//
// 固定时长为 0 的后果：源链路的 fail-loud 门控形同虚设
// （duration != 0 && duration != 0 永远为假 → 任何时长都放行），
// 而路由层的归一会把请求体写成 seconds=0 → 上游按 0 秒处理。
// 这类退化不报错，只会表现为「客户拿到空片」。
func TestSeedance25_固定档常量非零(t *testing.T) {
	require.NotZero(t, Seedance25PerCallFixedSeconds,
		"固定档时长不能为 0 —— 0 会让门控与归一同时失效")
	require.NotEmpty(t, Seedance25PerCallFixedResolution,
		"固定档分辨率不能为空 —— 空会让分辨率校验失效")
}

// TestSeedance25_固定档只属于按次成员 守的是「固定档不能误挂到母本」。
//
// 母本是**按秒**档：时长 2—30 秒可选、客户要 5 秒就给 5 秒。
// 如果母本也被当成固定 30 秒，就是把母本的既有能力砍掉 ——
// 这正是需求里「母本行为不能变」的回归。
func TestSeedance25_固定档只属于按次成员(t *testing.T) {
	assert.True(t, IsSeedance25PerCall(Seedance25HuoDongModel),
		"活动版必须是按次档")
	assert.False(t, IsSeedance25PerSecond(Seedance25HuoDongModel),
		"活动版不能同时是按秒档 —— 两个集合互斥，否则前端显示 /秒 但后端按次结算")

	assert.True(t, IsSeedance25PerSecond(Seedance25TeHuiModel),
		"母本必须仍是按秒档")
	assert.False(t, IsSeedance25PerCall(Seedance25TeHuiModel),
		"母本不能被标成按次档 —— 会让母本漏收秒数，少收 30 倍")
}

// TestSeedance25_按次判据覆盖活动版 守的是「活动版在任何判据下都是按次」。
//
// 这是 constant.IsTaskPerCallModel 的能力组侧断言：
// 计费链、日志、轮询三处都靠它判断，所以它必须是活动版的唯一结论。
func TestSeedance25_按次判据覆盖活动版(t *testing.T) {
	assert.True(t, IsTaskPerCallModel(Seedance25HuoDongModel),
		"活动版必须被判为按次 —— 否则计费链会乘上 30 秒倍率（¥4.5 收成 ¥135）")
	assert.False(t, IsTaskPerCallModel(Seedance25TeHuiModel),
		"母本不能被判为按次 —— 否则母本会漏收秒数")
}
