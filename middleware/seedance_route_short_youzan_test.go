package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 规则六「有赞标准版兜底」的中间件测试（2026-11-14）。
//
// 老板口径：
//   - 有赞按钮打开  → ≤阈值（生产 9 秒）请求改派有赞标准版 wan3.0-video；
//   - 两个按钮都开  → 心笔优先，心笔接不住（关闭 / 无渠道 / 契约不兼容）才落有赞；
//   - 两个都关闭    → 不路由，走原链路（源模型保持 seedance2.5-特惠）。
//
// 与心笔路径的关键差异：有赞标准版与源链路是**同一个 adaptor**（youzanwan3），
// 入参契约完全一致，所以只换 model 并把分辨率钉成源链路实际生效的档位；
// 参考视频/音频/seed 全部原样保留（心笔路径会拒绝这些）。

const testYouzanStandardModel = "wan3.0-video"

// enableShortToggles 配置规则六的两个按钮，测试结束恢复原状。
func enableShortToggles(t *testing.T, xinbi, youzan bool) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"source_model":          testSourceModel,
		"short_target_model":    testShortTargetModel,
		"short_trigger_seconds": 10,
		"short_link_enabled":    xinbi,
		"short_youzan_enabled":  youzan,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 心笔关闭、有赞打开：短请求改派有赞标准版，并钉住源链路实际交付的 720P。
func TestShortYouzanRoutesWhenXinbiOff(t *testing.T) {
	enableShortToggles(t, false, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("心笔关闭时短请求应改派有赞标准版 %s，实际 model=%v", testYouzanStandardModel, body["model"])
	}
	if body["resolution"] != "720P" {
		t.Fatalf("源链路 prime 缺省执行 720P，改派必须显式钉住，实际 resolution=%v", body["resolution"])
	}
	if rec.originModel != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, rec.originModel)
	}
	if !rec.hasBillingSec || rec.billingSeconds != 5 {
		t.Fatalf("计费秒数应为 5，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
	if !rec.hasBillingSize || rec.billingSizeRatio != 1.0 {
		t.Fatalf("720P 应按源模型倍率 1.0 计费，实际 has=%v value=%v", rec.hasBillingSize, rec.billingSizeRatio)
	}
}

// 两个按钮都打开：心笔优先，不能被有赞兜底抢走。
func TestShortYouzanPrefersXinbiWhenBothOn(t *testing.T) {
	enableShortToggles(t, true, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testShortTargetModel {
		t.Fatalf("两个按钮都开时必须心笔优先，实际 model=%v", body["model"])
	}
	if rec.hasBillingSize {
		t.Fatalf("心笔恒交付 720P，不该记 size 倍率，实际 %v", rec.billingSizeRatio)
	}
}

// 心笔没可用渠道 → 落有赞标准版。
func TestShortYouzanFallsBackWhenXinbiChannelUnavailable(t *testing.T) {
	enableShortToggles(t, true, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testShortTargetModel:    false,
		testYouzanStandardModel: true,
	})

	seen, origin := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("心笔无可用渠道时应兜底到 %s，实际 model=%v", testYouzanStandardModel, body["model"])
	}
	if origin != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, origin)
	}
}

// 心笔契约不兼容（21:9 不在心笔枚举内）→ 有赞标准版接住。
func TestShortYouzanTakesOverWhenXinbiIncompatible(t *testing.T) {
	enableShortToggles(t, true, true)

	seen, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"metadata":{"ratio":"21:9"},"prompt":"a cat"}`)
	body := decodeBody(t, seen)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("心笔不兼容时应由有赞标准版接住，实际 model=%v", body["model"])
	}
	metadata, _ := body["metadata"].(map[string]interface{})
	if metadata["ratio"] != "21:9" {
		t.Fatalf("有赞标准版支持 21:9，比例必须原样保留，实际 %v", metadata["ratio"])
	}
}

// 两个按钮都关闭 → 不路由，请求逐字节留在源链路。
func TestShortYouzanBothOffKeepsSourceChain(t *testing.T) {
	enableShortToggles(t, false, false)

	seen, origin := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("两个按钮都关闭时短请求必须留在源链路，实际 model=%v", decodeBody(t, seen)["model"])
	}
	if origin != "" {
		t.Fatalf("没有改派却记下了计费原点 %q", origin)
	}
}

// 带首尾帧 → 有赞标准版也不能接（上游 2026-10-28 起取消首尾帧入参），留在源链路。
func TestShortYouzanSkipsFramesMedia(t *testing.T) {
	enableShortToggles(t, false, true)

	body := `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("带首尾帧的短请求不该走有赞标准版，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// 参考视频/音频必须原样带过去：有赞标准版与源链路同一个 adaptor，契约一致。
func TestShortYouzanPreservesReferenceVideoAndAudio(t *testing.T) {
	enableShortToggles(t, false, true)

	body := `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat","media":[
		{"type":"reference_video","url":"https://x/a.mp4"},
		{"type":"reference_audio","url":"https://x/a.mp3"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testYouzanStandardModel {
		t.Fatalf("带参考视频/音频的短请求应改派有赞标准版，实际 model=%v", out["model"])
	}
	media, _ := out["media"].([]interface{})
	if len(media) != 2 {
		t.Fatalf("参考视频/音频必须原样保留，实际 %v", out["media"])
	}
}

// 分辨率与倍率：源链路 prime 的实际执行档是「空/480P → 720P，1080P → 1080P」，
// 有赞标准版支持这三档，必须按源模型口径钉档并计费。
func TestShortYouzanPinsSourceResolutionAndBillsSourceTier(t *testing.T) {
	enableShortToggles(t, false, true)

	cases := []struct {
		name      string
		body      string
		wantRes   string
		wantRatio float64
	}{
		{
			name:      "未指定分辨率按源链路 720P 执行",
			body:      `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`,
			wantRes:   "720P",
			wantRatio: 1.0,
		},
		{
			name:      "显式 480P 按源链路 720P 执行",
			body:      `{"model":"seedance2.5-特惠","seconds":5,"resolution":"480P","prompt":"a cat"}`,
			wantRes:   "720P",
			wantRatio: 1.0,
		},
		{
			name:      "1080P 原样保留并按 1.5 倍计费",
			body:      `{"model":"seedance2.5-特惠","seconds":5,"resolution":"1080P","prompt":"a cat"}`,
			wantRes:   "1080P",
			wantRatio: 1.5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := runRouteFull(t, tc.body)
			body := decodeBody(t, rec.body)
			if body["resolution"] != tc.wantRes {
				t.Fatalf("resolution = %v, want %s", body["resolution"], tc.wantRes)
			}
			if !rec.hasBillingSize || rec.billingSizeRatio != tc.wantRatio {
				t.Fatalf("size 倍率 = %v (has=%v), want %v", rec.billingSizeRatio, rec.hasBillingSize, tc.wantRatio)
			}
		})
	}
}

// 有赞标准版也没有可用渠道 → 放弃改派，放行原请求。
func TestShortYouzanSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableShortToggles(t, false, true)
	stubChannelAvailableByModel(t, map[string]bool{
		testYouzanStandardModel: false,
	})

	seen, _ := runRouteRaw(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`)
	if decodeBody(t, seen)["model"] != testSourceModel {
		t.Fatalf("有赞标准版无可用渠道时不该改派，实际 model=%v", decodeBody(t, seen)["model"])
	}
}

// metadata 里的秒数必须落成顶层 seconds：youzanwan3 adaptor 只读顶层，
// 不补的话请求会按缺省 5 秒执行，而计费却按 metadata 里的 8 秒 —— 多扣钱。
func TestShortYouzanWritesSecondsForMetadataOnlyRequests(t *testing.T) {
	enableShortToggles(t, false, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","metadata":{"seconds":"8"},"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testYouzanStandardModel {
		t.Fatalf("metadata.seconds=8 应触发有赞兜底，实际 model=%v", body["model"])
	}
	if body["seconds"] != "8" {
		t.Fatalf("metadata.seconds=8 必须落成顶层字符串 8，实际 %v", body["seconds"])
	}
	if !rec.hasBillingSec || rec.billingSeconds != 8 {
		t.Fatalf("计费秒数应为 8，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
}

// 阈值边界同规则六：10 秒命中、11 秒不命中。
func TestShortYouzanBoundaryIsInclusive(t *testing.T) {
	enableShortToggles(t, false, true)

	ten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`)
	if decodeBody(t, ten)["model"] != testYouzanStandardModel {
		t.Fatalf("10 秒是闭区间边界，必须改派，实际 model=%v", decodeBody(t, ten)["model"])
	}

	eleven, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":11,"prompt":"a cat"}`)
	if decodeBody(t, eleven)["model"] != testSourceModel {
		t.Fatalf("11 秒已超阈值，必须留在源链路，实际 model=%v", decodeBody(t, eleven)["model"])
	}
}
