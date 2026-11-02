package constant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「按次计费」统一判据的测试。
//
// 背景（2026-11-02 上架 seedance2.5-活动 时发现）：
// 计费链上有三处独立判定「这个模型是不是按次计费」——
//
//	relay/relay_task.go:218    预扣前是否应用 OtherRatios 倍率
//	service/task_billing.go:25 日志文案写「按次计费」还是「计算参数：seconds: N」
//	controller/relay.go:603     轮询阶段是否跳过差额结算
//
// 三处判据一旦不同步，后果都是**静默错价**，没有报错：
//   - 计费处判按次、日志处判按秒 → 账单金额对，但日志文案骗人；
//   - 计费处判按秒、日志处判按次 → 真实超收，日志还写着「按次计费」，事后对账更难查；
//   - 计费处判按次、轮询处判按秒 → 轮询阶段按完成态重算，¥4.5 的固定价被改写。
//
// 所以判据必须收敛成**一个函数**（constant.IsTaskPerCallModel）。

// 能力组按次成员必须被识别为按次档。
func TestIsTaskPerCallModel_Seedance25HuoDong(t *testing.T) {
	require.True(t, IsTaskPerCallModel(Seedance25HuoDongModel),
		"活动版是按次档（¥4.5/次），必须被识别为按次计费")
}

// 母本是按秒档，绝不能被误判为按次 ——
// 误判的后果是「少收 29 秒的钱」（¥0.20 而不是 ¥6.00），同样是无声的钱差。
func TestIsTaskPerCallModel_MotherModelIsNotPerCall(t *testing.T) {
	require.False(t, IsTaskPerCallModel(Seedance25TeHuiModel),
		"母本是按秒档，被误判成按次就会少收钱")
}

// 首尾空格必须容忍：生产上真出现过「 seedance2.5-特惠」这种请求
// （model/log.go 记着同一个事故）。
func TestIsTaskPerCallModel_TrimsSpace(t *testing.T) {
	require.True(t, IsTaskPerCallModel("  "+Seedance25HuoDongModel+"  "),
		"首尾空格必须被容忍，否则同一个模型会因空格不同而计费口径不同")
}

// TASK_PRICE_PATCH 环境变量命中的模型也必须被识别为按次档。
//
// 这是站内既有的按次模型先例（sora 等）—— 判据不能只认能力组。
func TestIsTaskPerCallModel_TaskPricePatchHit(t *testing.T) {
	previous := TaskPricePatches
	TaskPricePatches = []string{"sora-2", " patch-model "}
	t.Cleanup(func() { TaskPricePatches = previous })

	require.True(t, IsTaskPerCallModel("sora-2"),
		"TASK_PRICE_PATCH 命中的模型是按次档")
	require.True(t, IsTaskPerCallModel("patch-model"),
		"TASK_PRICE_PATCH 里的空白必须被 Trim（common/init.go 读 env 时已 Trim，这里再兜一层）")
	require.False(t, IsTaskPerCallModel("other-model"),
		"没命中的模型不是按次档")
}

// 空串不是按次档 —— 判据被空模型名误命中会让「查不到价的模型」变成按次，
// 那是静默免单。
func TestIsTaskPerCallModel_EmptyIsNotPerCall(t *testing.T) {
	assert.False(t, IsTaskPerCallModel(""))
	assert.False(t, IsTaskPerCallModel("   "))
}

// 族外的普通按秒模型不能被误判。
func TestIsTaskPerCallModel_IgnoresRegularModels(t *testing.T) {
	for _, model := range []string{
		"wan3.0-video",
		"wan3.0-720p-特惠",
		"seedance-2-5",
		"gpt-4o",
	} {
		assert.False(t, IsTaskPerCallModel(model), "%s 不是按次档", model)
	}
}

// 与能力组的按次判定必须一致（防止两处判据漂移）。
func TestIsTaskPerCallModel_AgreesWithCapabilityGroup(t *testing.T) {
	assert.True(t, IsTaskPerCallModel(Seedance25HuoDongModel),
		"能力组判按次的，统一判据也必须判按次")
	assert.Equal(t, IsSeedance25PerCall(Seedance25HuoDongModel),
		IsTaskPerCallModel(Seedance25HuoDongModel),
		"两个判据对能力组成员必须给出相同结论")
}
