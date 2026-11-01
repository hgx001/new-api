package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 管理后台的「跨模型改派」开关：前端要展示「当前谁停的、停到几点、
// 三条规则各自是否生效」，所以状态里必须带上来源与原因 ——
// 只给一个 paused 布尔值，运营分不清「额度自动停的」还是「人为停的」。

func withAdminRoutePolicy(t *testing.T, raw string) {
	t.Helper()
	previous := seedanceroute_setting.MarshalJSONString()
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(raw))
	t.Cleanup(func() {
		_ = seedanceroute_setting.UpdateFromJSONString(previous)
	})
}

func TestParsePauseUntilDefaultsToNextBeijingMidnight(t *testing.T) {
	// 不传 until：客户端/前端最常见用法，按「今天额度用完，明天再来」的语义
	// 自动算到次日 0 点，不需要调用方懂时区。
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)
	until, err := parsePauseUntil(pauseDoubaoRouteRequest{}, now)
	require.NoError(t, err)
	assert.Equal(t, nextBeijingMidnight(now).Unix(), until)
}

func TestParsePauseUntilAcceptsExplicitDeadline(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)
	want := now.Add(3 * time.Hour).Unix()
	until, err := parsePauseUntil(pauseDoubaoRouteRequest{Until: want}, now)
	require.NoError(t, err)
	assert.Equal(t, want, until)
}

func TestParsePauseUntilRejectsPastDeadline(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)

	// 静默接受「过去的时间」会得到「看起来成功但完全没暂停」的结果，
	// 比报错更难排查 —— 常见来源是把毫秒当秒传。
	for _, until := range []int64{now.Unix() - 1, now.Unix() - 86400, -1} {
		if until == -1 {
			continue // -1 与「不传」同义，走默认分支
		}
		_, err := parsePauseUntil(pauseDoubaoRouteRequest{Until: until}, now)
		assert.Error(t, err, "until=%d 是过去时间，应拒绝", until)
	}
}

func TestParsePauseUntilRejectsTooFarFuture(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)

	// 上限 30 天：把毫秒当秒传会把路由停几十年，而这种故障没有任何报错，
	// 只会悄悄长期跑在更贵的链路上。
	tooFar := now.Add(maxDoubaoRoutePauseDuration + 24*time.Hour).Unix()
	_, err := parsePauseUntil(pauseDoubaoRouteRequest{Until: tooFar}, now)
	assert.Error(t, err)
}

func TestDoubaoRouteAdminStateReportsPauseSource(t *testing.T) {
	now := time.Now()
	until := seedanceroute_setting.NextBeijingMidnight(now.Unix())
	withAdminRoutePolicy(t, `{"enabled":true,"source_model":"seedance2.5-特惠",`+
		`"target_model":"seedance-2-5","trigger_seconds":30,`+
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16",`+
		`"frames_target_model":"wan3.0-720p-特惠",`+
		`"seconds_rule_paused_until":`+itoaControllerTest(until)+`,`+
		`"pause_source":"auto","pause_reason":"上游渠道今日额度已用完"}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, true, state["paused"])
	assert.Equal(t, "auto", state["pause_source"], "前端要靠它区分「自动停的」")
	assert.Equal(t, "上游渠道今日额度已用完", state["pause_reason"])
	assert.Equal(t, false, state["seconds_rule_enabled"])
	assert.Equal(t, true, state["ratio_rule_enabled"], "比例规则不受影响")
	assert.Equal(t, true, state["frames_rule_enabled"], "首尾帧规则不受影响")

	// 前端要把「改派的是什么」也展示出来，否则运营看到的是一个没有主语的开关。
	assert.Equal(t, "seedance2.5-特惠", state["source_model"])
	assert.Equal(t, "seedance-2-5", state["target_model"])
	assert.Equal(t, 30, state["trigger_seconds"])
}

func TestDoubaoRouteAdminStateWhenNotPaused(t *testing.T) {
	withAdminRoutePolicy(t, `{"enabled":true,"source_model":"seedance2.5-特惠",`+
		`"target_model":"seedance-2-5","trigger_seconds":30}`)

	state := doubaoRouteAdminState()

	assert.Equal(t, false, state["paused"])
	assert.Equal(t, int64(0), state["paused_until"])
	assert.Equal(t, "", state["pause_source"])
	assert.Equal(t, "", state["pause_reason"])
	assert.Equal(t, true, state["seconds_rule_enabled"])
}

// itoaControllerTest 把 int64 转成十进制字符串，拼 JSON 用。
func itoaControllerTest(v int64) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
