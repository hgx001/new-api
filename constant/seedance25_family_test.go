package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ════════════════════════════════════════════════════════════════════════════
// seedance2.5 促销族的能力组契约
// ════════════════════════════════════════════════════════════════════════════
//
// 这个文件守的是需求方的核心要求：
// 「如果其中一个模型有新增的能力，也要同步到另外一个」。
//
// 实现方式是「单一能力组」——所有判定点查 Seedance25Family，而不是各自
// 维护名单。本文件用三类断言把「不会退化回两份名单」这件事钉住：

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：按次 / 按秒分组必须互斥（**最重要**）
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25_按次按秒分组互斥 守的是**静默错价**。
//
// 一个模型同时出现在两个集合里时：
//
//	· 后端：按次成员走 EstimateBilling 返回 nil → 收固定价
//	· 前端：isPerSecondModel 命中按秒表 → 显示「/秒」
//	→ 客户看到「¥4.5/秒」，账单扣 ¥4.5/次，30 秒的活儿按 30 秒的钱收
//
// 这类错价**不报错、不告警**，只有对账时才发现。所以必须在编译期/测试期挡住。
func TestSeedance25_按次按秒分组互斥(t *testing.T) {
	for model := range Seedance25Family {
		inPerCall := Seedance25PerCallModels[model]
		inPerSecond := Seedance25PerSecondModels[model]
		assert.False(t, inPerCall && inPerSecond,
			"模型 %q 同时在按次与按秒分组里 —— 前端会显示「/秒」但后端按次结算，静默错价", model)
	}
}

// TestSeedance25_每个族成员都有计费口径 守的是「族成员漏登记计费口径」。
//
// 漏登记的表现：新模型既不在按次组也不在按秒组 → 前端落到默认的「/次」，
// 而后端可能按秒结算 → 又是一次静默错价。
func TestSeedance25_每个族成员都有计费口径(t *testing.T) {
	for model := range Seedance25Family {
		inPerCall := Seedance25PerCallModels[model]
		inPerSecond := Seedance25PerSecondModels[model]
		assert.True(t, inPerCall || inPerSecond,
			"模型 %q 是族成员但没登记计费口径 —— 必须明确归入按次或按秒组", model)
	}
}

// TestSeedance25_计费口径组内无遗漏 守的是「计费口径组里有非族成员」。
//
// 非族成员混进计费口径组，说明有人把它加进了组却忘了加进 Family ——
// 那样它就只有计费口径、没有「与母本共享能力」的资格，语义是残缺的。
func TestSeedance25_计费口径组内无遗漏(t *testing.T) {
	for model := range Seedance25PerCallModels {
		assert.True(t, Seedance25Family[model],
			"模型 %q 在按次组里但不在族里 —— 族与计费口径组必须一致", model)
	}
	for model := range Seedance25PerSecondModels {
		assert.True(t, Seedance25Family[model],
			"模型 %q 在按秒组里但不在族里 —— 族与计费口径组必须一致", model)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：具体成员判定
// ────────────────────────────────────────────────────────────────────────────

func TestSeedance25_族成员判定(t *testing.T) {
	assert.True(t, IsSeedance25Family(Seedance25TeHuiModel))
	assert.True(t, IsSeedance25Family(Seedance25HuoDongModel))

	// 反证：别家渠道的相近模型名不该被误判成这一族。
	//
	// 为什么重要：站内同时存在 seedance-2-5（漫屋→豆包）、seedance-2.5（dola）、
	// seedance2.5特惠版（历史旧名）等模型。前缀匹配会全部误命中，
	// 那会让豆包链路被套上有赞的档位规则与脱敏规则。
	for _, other := range []string{
		"seedance-2-5",
		"seedance-2.5",
		"seedance-2-0-fast",
		"seedance2.5特惠版", // 历史旧名（按次旧模型，不属本族）
		"wan3.0-video-prime",
		"wan3.0-720p-按次",
		"gpt-4o",
		"",
	} {
		assert.False(t, IsSeedance25Family(other),
			"模型 %q 不属于 seedance2.5 促销族，不该被族判定命中", other)
	}
}

func TestSeedance25_计费口径判定(t *testing.T) {
	// 母本按秒、活动版按次 —— 这是本次最核心的差异。
	assert.True(t, IsSeedance25PerSecond(Seedance25TeHuiModel))
	assert.False(t, IsSeedance25PerCall(Seedance25TeHuiModel))

	assert.True(t, IsSeedance25PerCall(Seedance25HuoDongModel))
	assert.False(t, IsSeedance25PerSecond(Seedance25HuoDongModel))
}

// TestSeedance25_容忍首尾空格 守的是**生产真实事故**。
//
// model/log.go 的注释记着一次真实事故：客户传了「 seedance2.5-特惠」
// （带一个前导空格，界面上看不出来），日志按精确匹配筛不到。
// 族判定若不 TrimSpace，同类问题会在能力判定上重演。
func TestSeedance25_容忍首尾空格(t *testing.T) {
	for _, padded := range []string{
		" " + Seedance25TeHuiModel,
		Seedance25TeHuiModel + " ",
		"  " + Seedance25TeHuiModel + "  ",
	} {
		assert.True(t, IsSeedance25Family(padded),
			"带首尾空格的 %q 应当被识别为族成员", padded)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 3：族成员必须「同源」—— 本族的全部成员都走有赞 prime
// ────────────────────────────────────────────────────────────────────────────

// TestSeedance25_族成员模型名稳定 守的是「对外模型名不能被随手改」。
//
// 这些名字是**对外契约**：客户的请求里写的就是它们，渠道的 models 字段、
// abilities 表、options.ModelPrice 里也是它们。
// 改名 = 客户的既有请求全部 404，且老名字会掉进「查不到价 → 按次算成 ¥0」
// 的静默免单分支（见 setting/ratio_setting/model_ratio.go 的注释）。
func TestSeedance25_族成员模型名稳定(t *testing.T) {
	assert.Equal(t, "seedance2.5-特惠", Seedance25TeHuiModel)
	assert.Equal(t, "seedance2.5-活动", Seedance25HuoDongModel)
}

// TestSeedance25_族非空 守的是「别把族定义清空了」。
//
// 族空 = 所有族判定全部返回 false = 母本的脱敏、档位、上游映射全部失效。
// 这类退化不会报错，只会「上游名字漏给客户」，所以必须有守卫。
func TestSeedance25_族非空(t *testing.T) {
	require.NotEmpty(t, Seedance25Family)
	assert.GreaterOrEqual(t, len(Seedance25Family), 2,
		"族至少要有母本与活动版两个成员")
}
