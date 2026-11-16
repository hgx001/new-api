package middleware

// 促销族 vs 豆包的 CapCut 出口 —— **端到端**证明两条链路目标不同。
//
// 2026-11-16 晚老板口径（同日两条，必须同时成立）：
//
//	「特惠和活动这两个模型会路由到的 cp-seedance-2.5flash 替换成
//	 cp-seedance-2.5，这个保证质量做口碑」
//	「seedance-2-5 这个模型的路由还是不变，还是路由到 cp-seedance-2.5flash」
//
// 配置层的分叉契约由 setting/seedanceroute_setting/promo_target_model_test.go
// 锁定；本文件补的是**改写结果**那一层：同一份策略、同一个进程里跑两条请求，
// 看它们实际被写成哪两个模型名。配置对了但中间件里某处硬编码旧目标名，
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
//（症状是「规则七开着却没生效」或「备选开着却走班车」，看起来像代码 bug）。
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

// 同一份策略下：促销族改派 cp-seedance-2.5，豆包备胎仍是 cp-seedance-2.5flash。
func TestPromoAndDoubaoTargetsStaySplit(t *testing.T) {
	enablePromoAndDoubaoSplit(t)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget:       true,
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	// ① 促销族：6 秒电商请求（<10 秒，有赞标准版下界之外）→ 促销族的 CapCut 出口。
	promo := decodeBody(t, runAltRoute(t,
		`{"model":"`+testSourceModel+`","seconds":6,"prompt":"好物推荐 种草文案"}`).body)
	assert.Equal(t, constant.Capcut25Model, promo["model"],
		"促销族（特惠/活动）的 CapCut 出口必须是 cp-seedance-2.5（满血档）")

	// ② 豆包：源链路不可用 → 备选备胎，仍是 2.5 Flash。
	doubao := decodeBody(t, runAltRoute(t,
		`{"model":"`+failoverSourceModel+`","seconds":10,"prompt":"a cat","ratio":"16:9"}`).body)
	assert.Equal(t, constant.Capcut25FlashModel, doubao["model"],
		"seedance-2-5 的备胎仍是 cp-seedance-2.5flash（老板明确要求不变）")

	// ③ 反向守卫：两条链路不能落到同一个模型名上。
	assert.NotEqual(t, promo["model"], doubao["model"],
		"促销族与豆包的 CapCut 出口必须分属两档，不能合并")
}

// 促销族的改派仍钉 480p 生成 —— 换档只提升模型画质，不改降本结构。
//
// 为什么这条要单独锁：2.5 档 720p 的单价是 480p 的 2 倍，而促销族对客售价
// 与 2.5 档 480p 基准价持平。若因为「要保证质量」把这里改成 720p 直生，
// 每一单都会亏本 —— 而代码不会报错，只会悄悄把毛利吃成负数。
func TestPromoTargetStillGenerates480p(t *testing.T) {
	enablePromoAndDoubaoSplit(t)
	stubChannelAvailableByModel(t, map[string]bool{
		testEcommerceTarget:       true,
		failoverSourceModel:       false,
		failoverBackupModel:       true,
		testCapcutAltChannelModel: true,
	})
	stubSuperResolution(t, true)

	body := decodeBody(t, runAltRoute(t,
		`{"model":"`+testSourceModel+`","seconds":6,"prompt":"好物推荐 种草文案"}`).body)
	assert.Equal(t, constant.Capcut25Model, body["model"])
	assert.Equal(t, "480p", body["resolution"],
		"促销族改派 CapCut 必须仍钉 480p 生成 + 超分交付 720P（720p 直生会亏本）")
}
