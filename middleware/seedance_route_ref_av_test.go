package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 规则九（音视频参考改派 → cp-seedance-2.5）的中间件测试。
//
// 背景（2026-11-16 老板需求）：有赞上游 2026-11-15 切换后标准版 wan3.0-video
// 的 media 仅收 reference_image，带参考视频/音频的请求打到促销族与 wan3.0-video
// 直连都会被 youzanwan3 adaptor 的闸 400。CapCut 链路 2026-11-15 起参考音视频
// 均可透传（prepareCapcutRoute ①），所以这类请求不再拒单，改派 CapCut。
//
// 触发面（两类，覆盖 youzanwan3 adaptor 拦截的全部对外模型）：
//   - 促销族源模型（policy.IsSourceModel：seedance2.5-特惠 / 活动）；
//   - wan3.0-video 直连（不在源清单里，但同被该闸拦截）。
//   prime 直连不在此列 —— 有赞 prime 路由仍收音视频，adaptor 的闸对它豁免。

const testRefAVTarget = "cp-seedance-2.5"

// enableRefAVRoute 配置一个只开规则九所需最小项的策略，测试结束恢复。
//
// refAVEnabled=false 时显式写入 ref_av_capcut_enabled=false（三态开关的
// 「显式关闭」档）；true/省略 = 默认开启（生产 option JSON 里没有该字段，
// 解析后为 nil，按开启处理 —— 部署当天即生效，不需要动 SQL）。
func enableRefAVRoute(t *testing.T, refAVEnabled bool) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":        true,
		"source_models":  []string{testSourceModel, testSourceModelAlt},
		"target_model":   testTargetModel,
		"trigger_seconds": 20,
		// 隔离其它规则，保证断言只反映规则九的行为：
		"short_link_enabled":   false,
		"short_youzan_enabled": false,
		"ecommerce_enabled":    false,
	}
	if !refAVEnabled {
		payload["ref_av_capcut_enabled"] = false
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

// disableRefAVRoute 在既有 enable helper 之后显式关闭规则九。
//
// 规则九默认开启（nil = enabled），会抢先接走带参考音视频的请求改派
// cp-seedance-2.5flash。部分老用例验证的是**各规则自己的音视频守卫**
// （规则六/一的改写不搬音视频，改派即静默丢素材），必须把规则九关掉
// 才能隔离出守卫本身的行为。生产默认开的行为由本文件 TestRefAV* 套件锁定。
func disableRefAVRoute(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	policy := seedanceroute_setting.Get()
	disabled := false
	policy.RefAVCapcutEnabled = &disabled
	encoded, err := common.Marshal(policy)
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

// 促销族源模型 + 参考音频 → 改派 CapCut，音频抬到顶层透传，计费按源模型。
func TestRefAVRoutesAudioToCapcut(t *testing.T) {
	enableRefAVRoute(t, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"]}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testRefAVTarget {
		t.Fatalf("带参考音频应改派 %s，实际 model=%v", testRefAVTarget, body["model"])
	}
	audios, _ := body["audios"].([]interface{})
	if len(audios) != 1 || audios[0] != "https://cdn.example.com/ref.m4a" {
		t.Fatalf("参考音频必须抬到顶层 audios 透传，实际 %v", body["audios"])
	}
	if rec.originModel != testSourceModel {
		t.Fatalf("计费原点必须是源模型 %s，实际 %s", testSourceModel, rec.originModel)
	}
	if !rec.hasBillingSec || rec.billingSeconds != 10 {
		t.Fatalf("计费秒数应为 10，实际 has=%v value=%v", rec.hasBillingSec, rec.billingSeconds)
	}
}

// 参考视频同样改派，且顶层 videos 透传。
func TestRefAVRoutesVideoToCapcut(t *testing.T) {
	enableRefAVRoute(t, true)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat","videos":["https://cdn.example.com/ref.mp4"]}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testRefAVTarget {
		t.Fatalf("带参考视频应改派 %s，实际 model=%v", testRefAVTarget, body["model"])
	}
	videos, _ := body["videos"].([]interface{})
	if len(videos) != 1 || videos[0] != "https://cdn.example.com/ref.mp4" {
		t.Fatalf("参考视频必须抬到顶层 videos 透传，实际 %v", body["videos"])
	}
}

// wan3.0-video 直连（非促销族源）+ 参考音频 → 同样改派。
// 该模型被 youzanwan3 的「仅收 reference_image」闸拦截，却不在 policy 源清单里，
// 规则九必须在 IsSourceModel 早退之前接住它。
func TestRefAVRoutesWan3VideoDirect(t *testing.T) {
	enableRefAVRoute(t, true)

	rec := runRouteFull(t, `{"model":"wan3.0-video","seconds":10,"prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"]}`)
	body := decodeBody(t, rec.body)

	if body["model"] != testRefAVTarget {
		t.Fatalf("wan3.0-video 直连带音频应改派 %s，实际 model=%v", testRefAVTarget, body["model"])
	}
	if rec.originModel != "wan3.0-video" {
		t.Fatalf("计费原点必须是客户请求的 wan3.0-video，实际 %s", rec.originModel)
	}
}

// 显式关闭（ref_av_capcut_enabled=false）→ 请求体逐字节不变。
func TestRefAVDisabledKeepsBodyByteIdentical(t *testing.T) {
	enableRefAVRoute(t, false)

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"]}`
	seen, originModel := runRoute(t, body)
	if seen != body {
		t.Fatalf("显式关闭后 body 不该被改:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("显式关闭后不该记改派原点，实际 %q", originModel)
	}
}

// 带首尾帧 + 音频：规则九不接（CapCut 无首尾帧语义），
// 留给源链路 adaptor 给出准确的 400 —— 伪装成普通参考图是错交付。
func TestRefAVSkipsFrameMedia(t *testing.T) {
	enableRefAVRoute(t, true)

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"],"media":[{"type":"first_frame","url":"https://cdn.example.com/first.png"}]}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("带首尾帧的音视频请求不该被规则九改派:\n got %s\nwant %s", seen, body)
	}
}

// 1080P 不改派：CapCut 只卖 480p/720p，改派过去必 400。
func TestRefAVSkips1080P(t *testing.T) {
	enableRefAVRoute(t, true)

	body := `{"model":"seedance2.5-特惠","seconds":10,"resolution":"1080P","prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"]}`
	seen, _ := runRoute(t, body)
	parsed := decodeBody(t, seen)
	if parsed["model"] != testSourceModel {
		t.Fatalf("1080P 请求不该改派，实际 model=%v", parsed["model"])
	}
}

// 2–3 秒不改派：CapCut 时长下界 4 秒，改派过去必 400。
func TestRefAVSkipsBelowCapcutMinDuration(t *testing.T) {
	enableRefAVRoute(t, true)

	body := `{"model":"seedance2.5-特惠","seconds":2,"prompt":"a cat","audios":["https://cdn.example.com/ref.m4a"]}`
	seen, _ := runRoute(t, body)
	parsed := decodeBody(t, seen)
	if parsed["model"] != testSourceModel {
		t.Fatalf("2 秒请求不该改派，实际 model=%v", parsed["model"])
	}
}

// 素材不是 http(s) 直链（data URI）不改派：CapCut 的 assertHTTPURL 只收公网直链，
// 改派过去必 400 且 error 里会回显整段 base64。
func TestRefAVSkipsDataURLAudio(t *testing.T) {
	enableRefAVRoute(t, true)

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","audios":["data:audio/mp4;base64,AAAA"]}`
	seen, _ := runRoute(t, body)
	parsed := decodeBody(t, seen)
	if parsed["model"] != testSourceModel {
		t.Fatalf("data URI 音频不该改派，实际 model=%v", parsed["model"])
	}
}

// 没带音视频的普通请求：规则九必须完全沉默（body 逐字节不变）。
func TestRefAVSilentWithoutAVMedia(t *testing.T) {
	enableRefAVRoute(t, true)

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat"}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("普通请求不该被规则九触碰:\n got %s\nwant %s", seen, body)
	}
}
