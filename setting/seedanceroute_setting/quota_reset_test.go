package seedanceroute_setting

import (
	"testing"
	"time"
)

// 「额度重置时刻」的日期计算。
//
// 背景：豆包（万兴/ByteDance 系视频渠道）的日额度**不是**在午夜重置，
// 而是**北京时间凌晨 2 点**重置 —— 那是上游真实的重置点。
//
// 为什么这条必须钉死：默认截止时间如果按「次日 0 点」算，路由会在午夜
// 提前 2 小时恢复，然后整段时间的请求全部撞上「额度还没重置」的失败，
// 而系统**没有任何报错**（路由层认为改派是成功的）。表现就是「半夜一段时间
// 莫名多了一倍失败率」，排查成本极高。
//
// 所以口径统一为：**默认恢复时间 = 严格晚于 now 的下一个北京时间凌晨 2 点**。

func TestNextBeijingQuotaReset(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want string // 期望结果的北京时间字面量
	}{
		{
			name: "普通下午 → 次日凌晨 2 点",
			now:  time.Date(2026, 10, 30, 15, 4, 5, 0, beijingZone),
			want: "2026-10-31 02:00:00",
		},
		{
			// 凌晨 1 点还在当天额度周期内：下一个重置点就是当天 2 点，
			// 不能算成「次日 2 点」（那会多停 24 小时）。
			name: "凌晨 1 点 → 当天 2 点（还没到重置点）",
			now:  time.Date(2026, 10, 30, 1, 0, 0, 0, beijingZone),
			want: "2026-10-30 02:00:00",
		},
		{
			// 恰逢 2 点整：失败发生在重置点这一刻，它消耗的是**刚重置**的新周期额度，
			// 这一周期的额度要到次日 2 点才再次重置 —— 所以要停满 24 小时。
			// （若按「当天 2 点」算，暂停时长为 0，等于完全没暂停。）
			name: "恰逢 2 点整 → 次日 2 点（消耗的是刚重置的新周期额度）",
			now:  time.Date(2026, 10, 30, 2, 0, 0, 0, beijingZone),
			want: "2026-10-31 02:00:00",
		},
		{
			// 2 点之后仍在当天周期（额度已重置但当天仍会再耗尽），
			// 下一个重置点是次日 2 点。
			name: "凌晨 3 点 → 次日 2 点",
			now:  time.Date(2026, 10, 30, 3, 0, 0, 0, beijingZone),
			want: "2026-10-31 02:00:00",
		},
		{
			name: "当天最后一秒 → 次日凌晨 2 点",
			now:  time.Date(2026, 10, 30, 23, 59, 59, 0, beijingZone),
			want: "2026-10-31 02:00:00",
		},
		{
			name: "月末进位",
			now:  time.Date(2026, 10, 31, 7, 44, 0, 0, beijingZone),
			want: "2026-11-01 02:00:00",
		},
		{
			name: "年末进位",
			now:  time.Date(2026, 12, 31, 22, 0, 0, 0, beijingZone),
			want: "2027-01-01 02:00:00",
		},
		{
			// 输入是 UTC 也要按北京时间算：UTC 10-30 18:00 = 北京 10-31 02:00，
			// 恰逢重置点，按「严格晚于」语义下一个重置点是 11-01 02:00。
			name: "UTC 输入按北京时间折算",
			now:  time.Date(2026, 10, 30, 18, 0, 0, 0, time.UTC),
			want: "2026-11-01 02:00:00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NextBeijingQuotaReset(tc.now.Unix())
			gotText := time.Unix(got, 0).In(beijingZone).Format("2006-01-02 15:04:05")
			if gotText != tc.want {
				t.Fatalf("NextBeijingQuotaReset(%s) = %s, want %s",
					tc.now.Format(time.RFC3339), gotText, tc.want)
			}
		})
	}
}

func TestNextBeijingQuotaResetIsNotInThePast(t *testing.T) {
	// 结果绝不能早于 now —— 那样「暂停」等于「没暂停」，
	// 路由会立刻恢复，然后整晚撞额度耗尽。
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, beijingZone)
	for hour := 0; hour < 24; hour++ {
		now := base.Add(time.Duration(hour) * time.Hour)
		got := time.Unix(NextBeijingQuotaReset(now.Unix()), 0).In(beijingZone)
		if got.Before(now) {
			t.Fatalf("NextBeijingQuotaReset(%s) = %s, must not be in the past",
				now.Format(time.RFC3339), got.Format(time.RFC3339))
		}
	}
}
