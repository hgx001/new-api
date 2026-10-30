package controller

import (
	"testing"
	"time"
)

// 「暂停到北京时间次日 0 点」的日期计算。
//
// 这段逻辑错一天，客户就会多停一整天的豆包路由（或者提前恢复），
// 而且**没有任何报错** —— 所以边界必须逐个钉死。

func TestNextBeijingMidnight(t *testing.T) {
	cases := []struct {
		name string
		now  time.Time
		want string // 期望结果的北京时间字面量
	}{
		{
			name: "普通下午",
			now:  time.Date(2026, 10, 30, 15, 4, 5, 0, beijingZone),
			want: "2026-10-31 00:00:00",
		},
		{
			name: "当天最后一秒",
			now:  time.Date(2026, 10, 30, 23, 59, 59, 0, beijingZone),
			want: "2026-10-31 00:00:00",
		},
		{
			// 恰逢 0 点整必须取**次日**：用 `<=` 的直觉写法会算成「现在」，
			// 等于按钮点了没反应。
			name: "恰逢 0 点整取次日",
			now:  time.Date(2026, 10, 31, 0, 0, 0, 0, beijingZone),
			want: "2026-11-01 00:00:00",
		},
		{
			name: "月末进位",
			now:  time.Date(2026, 10, 31, 10, 0, 0, 0, beijingZone),
			want: "2026-11-01 00:00:00",
		},
		{
			name: "年末进位",
			now:  time.Date(2026, 12, 31, 23, 0, 0, 0, beijingZone),
			want: "2027-01-01 00:00:00",
		},
		{
			// 输入是 UTC 也要按北京时间算：UTC 10-30 16:30 = 北京 10-31 00:30，
			// 所以次日是北京 11-01 0 点。按 UTC 算会得出 10-31 0 点（已经过去了）。
			name: "UTC 输入按北京时间折算",
			now:  time.Date(2026, 10, 30, 16, 30, 0, 0, time.UTC),
			want: "2026-11-01 00:00:00",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextBeijingMidnight(tc.now).In(beijingZone).Format("2006-01-02 15:04:05")
			if got != tc.want {
				t.Fatalf("nextBeijingMidnight(%s) = %s, want %s",
					tc.now.Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

func TestNextBeijingMidnightIsStrictlyAfterNow(t *testing.T) {
	// 不管什么时刻，结果都必须严格晚于 now —— 否则暂停等于没暂停。
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, beijingZone)
	for hour := 0; hour < 24; hour++ {
		now := base.Add(time.Duration(hour) * time.Hour)
		got := nextBeijingMidnight(now)
		if !got.After(now) {
			t.Fatalf("nextBeijingMidnight(%s) = %s, must be strictly after now",
				now.Format(time.RFC3339), got.Format(time.RFC3339))
		}
	}
}

func TestBeijingZoneIsUTCPlus8(t *testing.T) {
	// 用 FixedZone 是刻意的（容器里可能没有 tzdata）。
	// 固定偏移必须是 +8 小时，否则暂停会整体偏移。
	if _, offset := time.Now().In(beijingZone).Zone(); offset != 8*3600 {
		t.Fatalf("beijingZone offset = %d, want %d", offset, 8*3600)
	}
}
