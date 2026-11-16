package seedanceroute_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// 规则九（音视频参考改派 → cp-seedance-2.5flash）的设置层测试。
//
// 契约要点见 ref_av_route.go 包头注释。与其它规则最大的差别是
// **三态默认开启**（nil = enabled）：其它规则是成本优化（默认关、升级当天
// 行为不变），规则九是能力补位（默认关 = 拒单退款继续发生）。

// 旧 option JSON 没有该字段 → 默认开启（部署当天即生效，不需要动 SQL）。
func TestRefAVDefaultsEnabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
	})

	assert.True(t, IsRefAVRouteEnabled(), "字段缺省（nil）必须按开启处理")
}

// 显式关闭才关闭。
func TestRefAVExplicitlyDisabled(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{"seedance2.5-特惠"},
		"ref_av_capcut_enabled": false,
	})

	assert.False(t, IsRefAVRouteEnabled())
}

// 只开规则九（其它规则全没配）时，策略不能被判成「没配全」——
// 否则中间件在 IsAnyRouteEnabled 早退，规则九静默失效、拒单复活。
// 与规则八的 TestTotalFallbackAloneKeepsPolicyReady 同一条契约。
func TestRefAVAloneKeepsPolicyReady(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":       true,
		"source_models": []string{"seedance2.5-特惠"},
	})

	assert.True(t, IsAnyRouteEnabled(), "规则九默认开启，中间件不能早退")
}

// 显式关闭规则九 + 其它规则全没配 → 策略算「没配全」，中间件可以早退。
func TestRefAVDisabledAloneDoesNotKeepPolicyReady(t *testing.T) {
	applyPolicy(t, map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{"seedance2.5-特惠"},
		"ref_av_capcut_enabled": false,
	})

	assert.False(t, IsAnyRouteEnabled(), "规则九显式关闭且无其它规则时不应撑起早退判定")
}
