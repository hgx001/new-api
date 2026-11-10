package controller

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 人工标记/暂停的**缺省截止时间**必须对齐上游真实的额度重置点
// （北京时间凌晨 2 点），而不是「次日 0 点」。
//
// 为什么这条要单独钉死：豆包的日额度在凌晨 2 点才重置。若默认按 0 点算，
// 路由会在午夜提前 2 小时接回改派，那 2 小时里的请求全部撞「额度还没重置」
// 而失败 —— 而**系统没有任何报错**（路由层认为改派成功，失败发生在上游）。
// 表现是「每天半夜多出一截莫名失败率」，且没有任何日志能指向原因。
//
// 与 setting 包的 NextBeijingQuotaReset 同口径：这里只锁「默认值落在
// 凌晨 2 点」，边界日期计算由 setting 包自己的测试负责。

func TestParseFailoverUntilDefaultsToQuotaResetTime(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)

	until, err := parseFailoverUntil(0, now)
	require.NoError(t, err)

	// 缺省值必须是「下一个北京时间凌晨 2 点」。
	want := time.Date(2026, 11, 2, 2, 0, 0, 0, beijingZone).Unix()
	assert.Equal(t, want, until, "缺省截止时间应对齐凌晨 2 点的额度重置点")
	assert.Equal(t,
		seedanceroute_setting.NextBeijingQuotaReset(now.Unix()), until,
		"缺省截止时间必须与 setting 包的口径一致")
}

func TestParsePauseUntilDefaultsToQuotaResetTime(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)

	until, err := parsePauseUntil(pauseDoubaoRouteRequest{}, now)
	require.NoError(t, err)

	want := time.Date(2026, 11, 2, 2, 0, 0, 0, beijingZone).Unix()
	assert.Equal(t, want, until, "缺省截止时间应对齐凌晨 2 点的额度重置点")
	assert.Equal(t,
		seedanceroute_setting.NextBeijingQuotaReset(now.Unix()), until,
		"缺省截止时间必须与 setting 包的口径一致")
}

// 显式传入的截止时间不受影响 —— 运营说停到几点就停到几点。
func TestParseFailoverUntilKeepsExplicitDeadline(t *testing.T) {
	now := time.Date(2026, 11, 1, 13, 0, 0, 0, beijingZone)
	want := now.Add(3 * time.Hour).Unix()

	until, err := parseFailoverUntil(want, now)
	require.NoError(t, err)
	assert.Equal(t, want, until)
}
