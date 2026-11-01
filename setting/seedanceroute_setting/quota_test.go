package seedanceroute_setting

import (
	"testing"
	"time"
)

// 这组用例锁的是「额度自动降级」的契约：
//
//	渠道额度用完 → 自动暂停「30 秒改派到豆包」，落回源链路（有赞，能出片）
//	额度恢复（出现成功）→ 自动解除，改派重新生效
//
// 为什么必须自动化：额度是按天计的、耗尽时间点不固定（2026-11-01 实测
// 03:37 耗尽、10-31 是 18:38 耗尽且 23:22 才恢复）。靠人盯开关一定漏，
// 而漏掉的方向是「30 秒请求持续失败」—— 客户直接看到任务失败。

func beijingNow(t *testing.T, year int, month time.Month, day, hour, minute int) int64 {
	t.Helper()
	return time.Date(year, month, day, hour, minute, 0, 0, beijingZone).Unix()
}

func beijingMidnight(t *testing.T, year int, month time.Month, day int) int64 {
	t.Helper()
	return time.Date(year, month, day, 0, 0, 0, 0, beijingZone).Unix()
}

// 自动暂停的基线：三条规则都配全，未暂停。
func autoPauseBaseline(t *testing.T) {
	t.Helper()
	restore(t)
	if err := UpdateFromJSONString(`{"enabled":true,"source_model":"seedance2.5-特惠",` +
		`"target_model":"seedance-2-5","trigger_seconds":30,` +
		`"ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`); err != nil {
		t.Fatalf("baseline update: %v", err)
	}
}

func TestLooksLikeQuotaExhaustedMatchesUpstreamWording(t *testing.T) {
	// 命中：上游/中间层报「额度用完」的各种写法。
	exhausted := []string{
		"渠道今日额度已用完，明天再来！", // 2026-11-01 生产原话（54 条失败全是这句）
		"额度已用完",
		"今日额度用完",
		"quota_exhausted",
		"Quota Exhausted",
		"upstream quota exhausted for today",
		"daily quota exceeded",
		"DAILY LIMIT REACHED",
	}
	for _, reason := range exhausted {
		if !LooksLikeQuotaExhausted(reason) {
			t.Errorf("LooksLikeQuotaExhausted(%q) = false, want true", reason)
		}
	}

	// 不命中：这些是「该改请求」的问题，暂停路由没有任何帮助，
	// 反而会让本该走豆包的低成本请求落回贵链路。
	notExhausted := []string{
		"",
		"   ",
		"上游生成失败（未提供具体原因），请稍后重试或联系客服",
		"内容审核不通过：检测到真人面部",
		"参考素材时长过长：当前为 31.609 秒，最长允许 15 秒",
		"MEDIA_PARAMETER_INVALID: ratio must be 16:9",
		"任务已取消",
		"额度不足，请充值", // 余额不足是另一回事，不该触发按天降级
	}
	for _, reason := range notExhausted {
		if LooksLikeQuotaExhausted(reason) {
			t.Errorf("LooksLikeQuotaExhausted(%q) = true, want false", reason)
		}
	}
}

func TestAutoPauseUsesNextBeijingMidnight(t *testing.T) {
	cases := []struct {
		name string
		now  int64
		want int64
	}{
		{
			name: "下午 1 点 → 次日 0 点",
			now:  beijingNow(t, 2026, 11, 1, 13, 0),
			want: beijingMidnight(t, 2026, 11, 2),
		},
		{
			name: "正好 0 点整 → 也是次日 0 点（不是今天，否则等于没暂停）",
			now:  beijingNow(t, 2026, 11, 1, 0, 0),
			want: beijingMidnight(t, 2026, 11, 2),
		},
		{
			name: "23:59 → 次日 0 点（跨天只差 1 分钟）",
			now:  beijingNow(t, 2026, 11, 1, 23, 59),
			want: beijingMidnight(t, 2026, 11, 2),
		},
		{
			name: "月末 → 下月 1 号 0 点（time.Date 自动进位）",
			now:  beijingNow(t, 2026, 11, 30, 7, 44),
			want: beijingMidnight(t, 2026, 12, 1),
		},
		{
			name: "年末 → 次年 1 月 1 日 0 点",
			now:  beijingNow(t, 2026, 12, 31, 22, 0),
			want: beijingMidnight(t, 2027, 1, 1),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NextBeijingMidnight(tc.now); got != tc.want {
				t.Fatalf("NextBeijingMidnight() = %d (%s), want %d (%s)",
					got, time.Unix(got, 0).In(beijingZone).Format(time.RFC3339),
					tc.want, time.Unix(tc.want, 0).In(beijingZone).Format(time.RFC3339))
			}
		})
	}
}

func TestAutoPauseTriggersOnTargetModelQuotaFailure(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)

	until, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！")
	if !ok {
		t.Fatal("ShouldAutoPause() = false, want true（目标模型 + 额度耗尽）")
	}
	if want := NextBeijingMidnight(now); until != want {
		t.Fatalf("until = %d, want %d", until, want)
	}
}

func TestAutoPauseIgnoresOtherModels(t *testing.T) {
	autoPauseBaseline(t)
	stubNow(t, beijingNow(t, 2026, 11, 1, 13, 0))

	// 源模型自己报额度耗尽（有赞侧），不该停「改派到豆包」这条规则 ——
	// 那是另一条链路的问题，停了只会让请求继续压在坏链路上。
	for _, model := range []string{
		"seedance2.5-特惠",
		"wan3.0-video-prime",
		"wan3.0-720p-特惠",
		"",
	} {
		if _, ok := ShouldAutoPause(Get(), model, "渠道今日额度已用完，明天再来！"); ok {
			t.Errorf("ShouldAutoPause(model=%q) = true, want false", model)
		}
	}
}

func TestAutoPauseIgnoresOtherFailureReasons(t *testing.T) {
	autoPauseBaseline(t)
	stubNow(t, beijingNow(t, 2026, 11, 1, 13, 0))

	for _, reason := range []string{
		"内容审核不通过：检测到真人面部",
		"参考素材时长过长：当前为 31.609 秒，最长允许 15 秒",
		"",
	} {
		if _, ok := ShouldAutoPause(Get(), "seedance-2-5", reason); ok {
			t.Errorf("ShouldAutoPause(reason=%q) = true, want false", reason)
		}
	}
}

func TestAutoPauseRequiresEnabledPolicy(t *testing.T) {
	restore(t)
	_ = UpdateFromJSONString(`{"enabled":false,"source_model":"seedance2.5-特惠",` +
		`"target_model":"seedance-2-5","trigger_seconds":30}`)
	stubNow(t, beijingNow(t, 2026, 11, 1, 13, 0))

	if _, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！"); ok {
		t.Fatal("ShouldAutoPause() = true, want false（总开关关着，不该写暂停状态）")
	}
}

// 关键用例：人工把规则一停到 11-30，自动检测不能把它缩到今天结束。
//
// 这会是一个很隐蔽的事故：运营因为「豆包要停用到月底」手动暂停了，
// 结果某天渠道 20 因为直连客户耗尽额度触发自动暂停，把截止时间
// 从 11-30 改写成「今天 24 点」—— 第二天规则一悄悄恢复，
// 而运营以为还在停用中。
func TestAutoPauseDoesNotOverrideManualPause(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)

	manualUntil := beijingMidnight(t, 2026, 12, 1)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithManualPause(manualUntil))); err != nil {
		t.Fatalf("manual pause: %v", err)
	}

	if _, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！"); ok {
		t.Fatal("ShouldAutoPause() = true, want false（人工暂停期间不许自动改写截止时间）")
	}
	if got := SecondsRulePausedUntil(); got != manualUntil {
		t.Fatalf("pausedUntil = %d, want %d（人工截止时间必须原样保留）", got, manualUntil)
	}
}

// 人工暂停的时间**早于**今天结束时的保护。
//
// 这个用例是补上来的：上面那个用例用 12-01 作截止时间，它恰好晚于
// 「今天结束」，所以即使删掉 manual 保护，也会被「已暂停到不早于目标时刻」
// 的幂等判断兜住 —— 测试照样绿，保护其实没被执行到。
//
// 真正的漏洞在这里：人工暂停到今天 18:00，自动检测算出「今天结束」是
// 次日 0 点，18:00 < 次日 0 点，幂等判断放行 → 把截止时间**延长**到明天。
// 运营说的是「停到 18 点」，系统却停到了明天。
func TestAutoPauseDoesNotExtendShorterManualPause(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)

	// 人工暂停到今天 18:00 —— 比「今天结束」更早。
	manualUntil := beijingNow(t, 2026, 11, 1, 18, 0)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithManualPause(manualUntil))); err != nil {
		t.Fatalf("manual pause: %v", err)
	}

	if _, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！"); ok {
		t.Fatal("ShouldAutoPause() = true, want false（不许把人工的 18:00 延长到明天 0 点）")
	}
	if got := SecondsRulePausedUntil(); got != manualUntil {
		t.Fatalf("pausedUntil = %d, want %d（人工截止时间必须原样保留）", got, manualUntil)
	}
}

// 自动暂停是幂等的：同一天反复观察到额度耗尽，不该每次都写一次 option。
func TestAutoPauseIsIdempotentWithinSameDay(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)

	if err := UpdateFromJSONString(MarshalJSONStringOf(WithAutoPause(NextBeijingMidnight(now), "额度用完"))); err != nil {
		t.Fatalf("auto pause: %v", err)
	}

	// 半小时后又一条额度耗尽失败：已经暂停到今天结束，不需要再写库。
	stubNow(t, now+1800)
	if _, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！"); ok {
		t.Fatal("ShouldAutoPause() = true, want false（已暂停到同一时刻，重复写库没有意义）")
	}
}

// 自动暂停过期后（跨天）可以再次触发。
func TestAutoPauseRefreshesExpiredAutoPause(t *testing.T) {
	autoPauseBaseline(t)
	day1 := beijingNow(t, 2026, 11, 1, 13, 0)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithAutoPause(NextBeijingMidnight(day1), "额度用完"))); err != nil {
		t.Fatalf("auto pause: %v", err)
	}

	// 第二天中午又额度用完：旧的自动暂停已过期，应该能再次触发。
	day2 := beijingNow(t, 2026, 11, 2, 12, 0)
	stubNow(t, day2)
	until, ok := ShouldAutoPause(Get(), "seedance-2-5", "渠道今日额度已用完，明天再来！")
	if !ok {
		t.Fatal("ShouldAutoPause() = false, want true（跨天后应能再次触发）")
	}
	if want := NextBeijingMidnight(day2); until != want {
		t.Fatalf("until = %d, want %d", until, want)
	}
}

func TestAutoResumeClearsAutoPauseOnSuccess(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithAutoPause(NextBeijingMidnight(now), "额度用完"))); err != nil {
		t.Fatalf("auto pause: %v", err)
	}

	// 额度提前恢复（滚动窗口 / 换了账号），出现成功就该立刻接回改派，
	// 不必等到次日 0 点 —— 省的是每一单的差价。
	if !ShouldAutoResume(Get(), "seedance-2-5") {
		t.Fatal("ShouldAutoResume() = false, want true（自动暂停期间出现成功）")
	}
}

func TestAutoResumeDoesNotClearManualPause(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithManualPause(beijingMidnight(t, 2026, 12, 1)))); err != nil {
		t.Fatalf("manual pause: %v", err)
	}

	// 人工暂停的理由可能是「豆包账号要停用到月底」，跟额度无关。
	// 渠道上出现一次成功不能替运营做恢复决定。
	if ShouldAutoResume(Get(), "seedance-2-5") {
		t.Fatal("ShouldAutoResume() = true, want false（不许自动解除人工暂停）")
	}
}

func TestAutoResumeNoopWhenNotPaused(t *testing.T) {
	autoPauseBaseline(t)
	stubNow(t, beijingNow(t, 2026, 11, 1, 13, 0))

	if ShouldAutoResume(Get(), "seedance-2-5") {
		t.Fatal("ShouldAutoResume() = true, want false（本来就没暂停）")
	}
}

func TestAutoResumeIgnoresOtherModels(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)
	if err := UpdateFromJSONString(MarshalJSONStringOf(WithAutoPause(NextBeijingMidnight(now), "额度用完"))); err != nil {
		t.Fatalf("auto pause: %v", err)
	}

	for _, model := range []string{"seedance2.5-特惠", "wan3.0-video-prime", ""} {
		if ShouldAutoResume(Get(), model) {
			t.Errorf("ShouldAutoResume(model=%q) = true, want false", model)
		}
	}
}

// 自动暂停也要如实记录来源与原因，否则前端分不清「谁停的」。
func TestAutoPauseRecordsSourceAndReason(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)

	paused := WithAutoPause(NextBeijingMidnight(now), ReasonQuotaExhausted)
	if paused.PauseSource != PauseSourceAuto {
		t.Fatalf("PauseSource = %q, want %q", paused.PauseSource, PauseSourceAuto)
	}
	if paused.PauseReason != ReasonQuotaExhausted {
		t.Fatalf("PauseReason = %q, want %q", paused.PauseReason, ReasonQuotaExhausted)
	}
	if paused.UpdatedAt != now {
		t.Fatalf("UpdatedAt = %d, want %d", paused.UpdatedAt, now)
	}

	manual := WithManualPause(now + 3600)
	if manual.PauseSource != PauseSourceManual {
		t.Fatalf("manual PauseSource = %q, want %q", manual.PauseSource, PauseSourceManual)
	}
	if manual.PauseReason != "" {
		t.Fatalf("manual PauseReason = %q, want empty（人工暂停没有自动原因）", manual.PauseReason)
	}

	resumed := WithAutoResume()
	if resumed.PauseSource != "" || resumed.PauseReason != "" || resumed.SecondsRulePausedUntil != 0 {
		t.Fatalf("resume 后应清空来源/原因/截止时间，got %+v", resumed)
	}
}

// 纯函数契约：WithAutoPause / WithAutoResume / WithManualPause 都不能改全局状态。
func TestAutoPauseHelpersDoNotMutateCurrent(t *testing.T) {
	autoPauseBaseline(t)
	now := beijingNow(t, 2026, 11, 1, 13, 0)
	stubNow(t, now)
	before := Get()

	_ = WithAutoPause(NextBeijingMidnight(now), ReasonQuotaExhausted)
	_ = WithAutoResume()
	_ = WithManualPause(now + 3600)

	after := Get()
	if after.SecondsRulePausedUntil != before.SecondsRulePausedUntil ||
		after.PauseSource != before.PauseSource ||
		after.PauseReason != before.PauseReason {
		t.Fatalf("helpers 改动了全局状态：before=%+v after=%+v", before, after)
	}
}
