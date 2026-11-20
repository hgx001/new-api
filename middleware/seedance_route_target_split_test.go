package middleware

// 促销族 vs 豆包的 CapCut 出口 —— **端到端**证明两条链路目标已统一。
//
// 口径沿革：
//   - 2026-11-16 晚：促销族升到 cp-seedance-2.5 满血档做口碑，豆包备胎保持 2.5flash
//     （两条刻意分叉）；
//   - 2026-11-17：取消超分、改 720p 原生直出，促销族所有 CapCut 出口统一回
//     cp-seedance-2.5flash，与豆包备胎落在同一档。
//
// 配置层契约由 setting/seedanceroute_setting/promo_target_model_test.go 锁定；
// 本文件补的是**改写结果**那一层：同一份策略、同一个进程里跑两条请求，
// 看它们实际被写成哪个模型名。配置对了但中间件里某处硬编码旧目标名，
// 只有这一层能发现。

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// enablePromoAndDoubaoSplit 写入一份**同时**开着规则七与规则五备选的策略。
//
// ⚠️ 必须一次写完：本包多个 enableXxx 辅助函数各自会 UpdateFromJSONString
// 写入一份完整 policy，连续调用时后写的会把先写的整个冲掉
// （症状是「规则七开着却没生效」或「备选开着却走班车」，看起来像代码 bug）。
func enablePromoAndDoubaoSplit(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_models":         []string{testSourceModel},
		"ecommerce_enabled":     true,
		"failover_enabled":      true,
		"failover_source_model": failoverSourceModel,
		"failover_target_model": failoverBackupModel,
		"failover_alt_enabled":  true,
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 同一份策略下：促销族与豆包备胎都改派 cp-seedance-2.5flash。
func TestPromoAndDoubaoTargetsAreUnified(t *testing.T) {
	enablePromoAndDoubaoSplit(t)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget: true,
		failoverSourceModel: false,
		failoverBackupModel: true,
	})
	// 2026-11-17 起豆包备胎也改 720p 原生直出、不再依赖 MPS：探针已不被
	// 生产代码读取，这里无需（也无法）再桩它。

	// ① 促销族：6 秒电商请求（<10 秒，有赞标准版下界之外）→ 促销族的 CapCut 出口。
	promo := decodeBody(t, runAltRoute(t,
		`{"model":"`+testSourceModel+`","seconds":6,"prompt":"好物推荐 种草文案"}`).body)
	assert.Equal(t, constant.Capcut25FlashModel, promo["model"],
		"促销族（特惠/活动）的 CapCut 出口必须是 cp-seedance-2.5flash")

	// ② 豆包：源链路不可用 → 备选备胎，仍是 2.5 Flash。
	doubao := decodeBody(t, runAltRoute(t,
		`{"model":"`+failoverSourceModel+`","seconds":10,"prompt":"a cat","ratio":"16:9"}`).body)
	assert.Equal(t, constant.Capcut25FlashModel, doubao["model"],
		"seedance-2-5 的备胎仍是 cp-seedance-2.5flash（老板明确要求不变）")

	// ③ 正向守卫：两条链路现在落在同一个模型名上。
	assert.Equal(t, promo["model"], doubao["model"],
		"促销族与豆包的 CapCut 出口统一为 2.5 Flash")
}

// 促销族的改派钉 720p 原生生成 —— 超分已取消，不再 480p 生成。
//
// 为什么这条要单独锁：2026-11-17 起超分画质不达标被砍，若中间件某处仍按
// 480p 生成、又依赖 manwu 的超分钩子补回 720P，客户拿到的就是糊片（且
// manwu 侧已不再对促销族触发超分）—— 这是静默降质，不能发生。
func TestPromoTargetGenerates720pNatively(t *testing.T) {
	enablePromoAndDoubaoSplit(t)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget: true,
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := decodeBody(t, runAltRoute(t,
		`{"model":"`+testSourceModel+`","seconds":6,"prompt":"好物推荐 种草文案"}`).body)
	assert.Equal(t, constant.Capcut25FlashModel, body["model"])
	assert.Equal(t, "720p", body["resolution"],
		"促销族改派 CapCut 必须钉 720p 原生直出（不再 480p 生成 + 超分）")
}
