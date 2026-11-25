package seedanceroute_setting

// 促销族常驻改派 / 自营短档 / 规则一时长下界的策略访问器契约（2026-11-23）。
//
// 这三处都是「按源模型」的字段，最容易踩的坑是：
//  1. 常驻改派写成全局默认 → 活动版（按次 ¥4.5/次）被一起改派，账单翻几十倍；
//  2. 时长下界写成公共 trigger_seconds → 活动版「不看时长全部改派」的旧口径被改掉；
//  3. 自营短档漏掉 short_rule_enabled 闸门 → 打开按钮把按次档带进短时长链路。

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyPolicyJSON(t *testing.T, payload string) {
	t.Helper()
	previous := Get()
	require.NoError(t, UpdateFromJSONString(payload))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = UpdateFromJSONString(string(restored))
	})
}

const promoPolicyJSON = `{
  "enabled": true,
  "source_model": "seedance2.5-特惠",
  "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
  "target_model": "seedance-2-5",
  "trigger_seconds": 20,
  "short_trigger_seconds": 15,
  "short_fast_enabled": true,
  "promo_short_own_enabled": true,
  "source_overrides": {
    "seedance2.5-特惠": {
      "seconds_rule_enabled": true,
      "seconds_trigger_seconds": 15,
      "short_rule_enabled": true,
      "promo_resident_model": "wan3.0-video-tj"
    }
  }
}`

// TestPromoResidentModelForOnlyConfiguredSource 常驻改派只认配了字段的源模型。
func TestPromoResidentModelForOnlyConfiguredSource(t *testing.T) {
	applyPolicyJSON(t, promoPolicyJSON)

	assert.Equal(t, "wan3.0-video-tj", PromoResidentModelFor("seedance2.5-特惠"))
	assert.Empty(t, PromoResidentModelFor("seedance2.5-活动"),
		"活动版没配常驻改派，必须留在原链路（它是按次一口价商品）")
	assert.Empty(t, PromoResidentModelFor("seedance-2-5"), "非源模型一律不改派")
}

// TestSecondsTriggerSecondsForPerSource 时长下界按源模型取，活动版保持不设下界。
func TestSecondsTriggerSecondsForPerSource(t *testing.T) {
	applyPolicyJSON(t, promoPolicyJSON)

	policy := Get()
	assert.Equal(t, 15, policy.SecondsTriggerSecondsFor("seedance2.5-特惠"))
	assert.Equal(t, 0, policy.SecondsTriggerSecondsFor("seedance2.5-活动"),
		"活动版必须保持 0（2026-11-20 口径：不带参考音视频就改派）")
}

// TestPromoShortOwnEnabledForGates 自营短档要同时满足总开关、源模型清单、短时长闸门。
//
// ⚠️ 活动版在**声明层**也会返回 true：short_rule_enabled 的缺省语义是「未配 = 开启」
// （与规则六四条分支同口径）。真正拦住它的是中间件的
// `constant.IsSeedance25PerCall(sourceModel)` —— 按次档固定 30 秒一口价，
// 不能被短时长分流（见 middleware/seedance_route_promo_resident.go）。
func TestPromoShortOwnEnabledForGates(t *testing.T) {
	applyPolicyJSON(t, promoPolicyJSON)

	assert.True(t, PromoShortOwnEnabledFor("seedance2.5-特惠"))
	assert.True(t, IsPromoShortOwnEnabled())

	// 显式关掉该源模型的短时长闸门 → 不再参与。
	applyPolicyJSON(t, `{
	  "enabled": true,
	  "source_model": "seedance2.5-特惠",
	  "target_model": "seedance-2-5",
	  "trigger_seconds": 20,
	  "promo_short_own_enabled": true,
	  "source_overrides": {"seedance2.5-特惠": {"short_rule_enabled": false}}
	}`)
	assert.False(t, PromoShortOwnEnabledFor("seedance2.5-特惠"),
		"显式关掉 short_rule_enabled 后，自营短档不能再接手")
}

// TestPromoShortOwnOffByDefault 旧 option（没有新字段）解析后必须是关闭状态。
func TestPromoShortOwnOffByDefault(t *testing.T) {
	applyPolicyJSON(t, `{"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":20}`)

	assert.False(t, IsPromoShortOwnEnabled(), "升级当天行为必须不变：默认关闭")
	assert.False(t, PromoShortOwnEnabledFor("seedance2.5-特惠"))
	assert.Empty(t, PromoResidentModelFor("seedance2.5-特惠"), "没配常驻字段就不改派")
	assert.Equal(t, 0, PrimarySourceTriggerSeconds(), "没配下界就是 0")
}

// TestPromoShortOwnCloneIsolation 开关是落库策略的一部分：clone 后改副本不能改到全局。
func TestPromoShortOwnCloneIsolation(t *testing.T) {
	applyPolicyJSON(t, promoPolicyJSON)

	policy := WithPromoShortOwnEnabled(false)
	assert.False(t, policy.PromoShortOwnEnabled)
	assert.True(t, IsPromoShortOwnEnabled(), "落库前的副本修改不能影响全局状态")

	withSource := ApplyPromoShortOwnEnabled(policy, true)
	assert.True(t, withSource.PromoShortOwnEnabled)
}

// TestPromoShortFullOffByDefault 旧 option（没有满血档字段）解析后必须是关闭状态。
func TestPromoShortFullOffByDefault(t *testing.T) {
	applyPolicyJSON(t, `{"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":20}`)

	assert.False(t, IsPromoShortFullEnabled(), "升级当天行为必须不变：默认关闭")
	assert.False(t, PromoShortFullEnabledFor("seedance2.5-特惠"))
}

// TestPromoShortTiersAreMutuallyExclusive 满血档 / 自营短档三选一，最多勾一个。
//
// 写入口保证互斥：勾满血档自动关自营短档，勾自营短档自动关满血档；
// 两个都不勾 → 豆包 Fast。互斥必须落在设置层，否则控制器各写各的字段，
// 总有一条路径会同时点亮两个按钮。
func TestPromoShortTiersAreMutuallyExclusive(t *testing.T) {
	applyPolicyJSON(t, promoPolicyJSON)

	full := WithPromoShortFullEnabled(true)
	assert.True(t, full.PromoShortFullEnabled)
	assert.False(t, full.PromoShortOwnEnabled, "勾满血档必须关掉自营短档")

	own := WithPromoShortOwnEnabled(true)
	assert.True(t, own.PromoShortOwnEnabled)
	assert.False(t, own.PromoShortFullEnabled, "勾自营短档必须关掉满血档")

	applied := ApplyPromoShortFullEnabled(Policy{}, true)
	assert.True(t, applied.PromoShortFullEnabled)
	assert.False(t, applied.PromoShortOwnEnabled)

	appliedOwn := ApplyPromoShortOwnEnabled(Policy{PromoShortFullEnabled: true}, true)
	assert.True(t, appliedOwn.PromoShortOwnEnabled)
	assert.False(t, appliedOwn.PromoShortFullEnabled)
}

// TestPromoShortFullEnabledForGates 满血档要同时满足总开关、源模型清单、短时长闸门。
func TestPromoShortFullEnabledForGates(t *testing.T) {
	applyPolicyJSON(t, `{
	  "enabled": true,
	  "source_model": "seedance2.5-特惠",
	  "target_model": "seedance-2-5",
	  "trigger_seconds": 20,
	  "short_trigger_seconds": 15,
	  "promo_short_full_enabled": true,
	  "source_overrides": {"seedance2.5-特惠": {"short_rule_enabled": true}}
	}`)

	assert.True(t, PromoShortFullEnabledFor("seedance2.5-特惠"))
	assert.True(t, IsPromoShortFullEnabled())

	applyPolicyJSON(t, `{
	  "enabled": true,
	  "source_model": "seedance2.5-特惠",
	  "target_model": "seedance-2-5",
	  "trigger_seconds": 20,
	  "promo_short_full_enabled": true,
	  "source_overrides": {"seedance2.5-特惠": {"short_rule_enabled": false}}
	}`)
	assert.False(t, PromoShortFullEnabledFor("seedance2.5-特惠"),
		"显式关掉 short_rule_enabled 后，满血档不能再接手")
}
