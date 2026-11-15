package middleware

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则八（全量兜底 → cp-seedance-2.5flash）的中间件测试。
//
// 语义（2026-11-15 老板需求）：seedance2.5-特惠 / seedance2.5-活动 的常驻链路
// 是有赞。有赞**彻底不能用**时，前七条规则全部失效（它们都假设源链路还能跑
// 一部分）。这条是唯一不依赖该前提的逃生口：开关一开，请求**无条件**改派
// CapCut 2.5 Flash，不看时长/比例/提示词内容/首尾帧。
//
// 本文件锁两类行为：
//  1. 「无条件」真的无条件 —— 短请求、长请求、非电商提示词、非 9:16 都改派；
//  2. 目标链路的**物理能力**边界不改派（1080P / 2–3 秒 / 参考视频音频 /
//     自适应比例带参考图）—— 那几类改派过去必 400，会把「链路挂了」
//     伪装成「你的参数不合法」。

const totalFallbackTarget = "cp-seedance-2.5flash"

// enableTotalFallback 只打开规则八（前七条都不配），测试结束恢复原状。
func enableTotalFallback(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{testSourceModel, "seedance2.5-活动"},
		"total_fallback_enabled": true,
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, err := common.Marshal(previous)
		require.NoError(t, err)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 5 秒的普通请求（不超时长阈值、非 9:16、提示词不是电商）也要改派 ——
// 这正是「无条件」与规则六/七的区别。
func TestTotalFallbackRoutesShortPlainRequest(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat",
		"metadata":{"ratio":"16:9"},
		"media":[{"type":"reference_image","url":"https://x/a.png"}]}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackTarget, body["model"], "规则八必须无条件改派 CapCut")
	assert.Equal(t, "5", body["seconds"], "时长必须原样下发（字符串形态）")
	assert.Equal(t, "720p", body["resolution"], "CapCut 缺省 480p，必须显式钉住源链路的实际档 720P")
	assert.Equal(t, "16:9", body["ratio"], "比例必须显式落到顶层 ratio")

	images, _ := body["input_reference"].([]interface{})
	require.Len(t, images, 1, "参考图必须抬到顶层 input_reference")
	assert.Equal(t, "https://x/a.png", images[0])
	assert.NotContains(t, body, "media", "顶层 media 必须剥掉，否则同图投两次")

	// 计费原点仍是客户请求的源模型：改派不改对客价格。
	assert.Equal(t, testSourceModel, rec.originModel)
	assert.True(t, rec.hasBillingSec)
	assert.Equal(t, float64(5), rec.billingSeconds)
}

// 带参考图但**没指定比例**时也不改派：源链路缺省是 adaptive（跟随参考图），
// CapCut 没有这一档 —— 替他挑一个画幅就是替客户改需求。
func TestTotalFallbackSkipsAdaptiveRatioWithReferenceImage(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat",
		"media":[{"type":"reference_image","url":"https://x/a.png"}]}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 长请求（超过规则一阈值）也必须走 CapCut：规则八优先级高于规则一。
func TestTotalFallbackBeatsSecondsRule(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(`{
		"enabled":true,
		"source_models":["seedance2.5-特惠"],
		"target_model":"seedance-2-5",
		"trigger_seconds":20,
		"total_fallback_enabled":true
	}`))

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackTarget, body["model"], "规则八开着时不该被规则一抢先改派到豆包")
	assert.Equal(t, testSourceModel, rec.originModel)
}

// 9:16 也一样：规则八不吃比例条件。
func TestTotalFallbackIgnoresRatio(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":8,
		"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackTarget, body["model"])
	assert.Equal(t, "9:16", body["ratio"])
}

// 活动版（按次档）：客户不传时长时先归一成 30 秒，再按 30 秒改派。
//
// 不归一的话改派过去会落 CapCut 自己的缺省 5 秒 —— 那是「按 30 秒卖、
// 实际出 5 秒」的错交付。
func TestTotalFallbackNormalizesPerCallSeconds(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-活动","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackTarget, body["model"])
	assert.Equal(t, "30", body["seconds"], "按次档没传时长必须体现为 30 秒，不能落到 CapCut 的缺省 5 秒")
	assert.Equal(t, "seedance2.5-活动", rec.originModel)
}

// 1080P 不改派：CapCut 只卖 480p/720p，改派过去必 400。 留原链路至少能得到准确的原因（没有可用渠道），而不是把链路故障
// 伪装成「参数不合法」。
func TestTotalFallbackSkips1080P(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":5,"resolution":"1080P","prompt":"a cat"}`
	rewritten, origin := runRoute(t, body)

	assert.Equal(t, body, rewritten, "物理接不住的请求必须原样放行，不能半改写")
	assert.Empty(t, origin, "没改派就不该留计费原点")
}

// 2–3 秒不改派：CapCut 时长下界 4 秒。
func TestTotalFallbackSkipsTooShortRequest(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":3,"prompt":"a cat"}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 带参考视频照常改派，素材抬到顶层 videos（2026-11-15 放开）。
//
// 与规则七同一套准入（共用 prepareCapcutRoute），所以准入口径不会漂 ——
// 这条用例存在的意义是：全量兜底的调用点也吃到了放开后的行为。
func TestTotalFallbackRoutesReferenceVideo(t *testing.T) {
	enableTotalFallback(t)

	rewritten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,
		"media":[{"type":"reference_video","url":"https://x/a.mp4"}],"prompt":"a cat"}`)
	decoded := decodeBody(t, rewritten)

	assert.Equal(t, totalFallbackTarget, decoded["model"], "带参考视频应改派")
	assert.Equal(t, []interface{}{"https://x/a.mp4"}, decoded["videos"],
		"参考视频必须抬到顶层 videos")
	assert.NotContains(t, decoded, "media", "media 必须摘掉，否则同一视频投两次")
}

// 带参考音频照常改派，素材抬到顶层 audios（2026-11-15 放开）。
func TestTotalFallbackRoutesReferenceAudio(t *testing.T) {
	enableTotalFallback(t)

	rewritten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":5,"audios":["https://x/a.mp3"],"prompt":"a cat"}`)
	decoded := decodeBody(t, rewritten)

	assert.Equal(t, totalFallbackTarget, decoded["model"], "带参考音频应改派")
	assert.Equal(t, []interface{}{"https://x/a.mp3"}, decoded["audios"],
		"参考音频必须抬到顶层 audios")
}

// 首尾帧不改派：CapCut 只有参考图语义，改派过去等于把首尾帧降级成参考图。
// 它应该自然落到规则三（配了首尾帧目标的话），而不是被规则八吞掉。
func TestTotalFallbackSkipsFrameMedia(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":5,
		"media":[{"type":"first_frame","url":"https://x/a.png"}],"prompt":"a cat"}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 目标链路没有可用渠道时不改派：改写之后重试只会用同一个模型名重选渠道，
// 放开就是客户撞 503。预检能让请求自然落回源链路。
func TestTotalFallbackSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableTotalFallback(t)
	stubChannelAvailable(t, false)

	const body = `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`
	rewritten, _ := runRouteRaw(t, body)

	assert.Equal(t, body, rewritten)
}

// 没传时长时按 5 秒下发：这是源链路与 CapCut **共同**的缺省值，
// 改派前后成片长度一致。
func TestTotalFallbackDefaultsMissingSecondsToFive(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackTarget, body["model"])
	assert.Equal(t, "5", body["seconds"])
}

// 45 秒不改派：CapCut 上界 30 秒。
//
// ⚠️ 关键是**不能夹到 30 秒再改派** —— 那是「按 45 秒卖、出 30 秒」的静默改档，
// 比直接报错更糟（客户付了钱、拿到短片、账单上看不出来）。
func TestTotalFallbackDoesNotClampLongDuration(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":45,"prompt":"a cat"}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 开关关闭（默认）时一个字节都不能动 —— 这是「升级当天线上流量不变」的保证。
func TestTotalFallbackDisabledKeepsBodyByteIdentical(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(
		`{"enabled":true,"source_models":["seedance2.5-特惠"],"target_model":"seedance-2-5","trigger_seconds":20}`))

	const body = `{"model":"seedance2.5-特惠","seconds":5,"prompt":"a cat"}`
	rewritten, origin := runRoute(t, body)

	assert.Equal(t, body, rewritten, "开关关闭时请求体必须逐字节不变")
	assert.Empty(t, origin)
}

// 非源模型不受影响：seedance-2-5 自己不能被全量兜底抓到（否则会自循环）。
func TestTotalFallbackIgnoresNonSourceModel(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance-2-5","seconds":5,"prompt":"a cat"}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 保险：断言改写后的请求体里没有被塞进空 model（那会让 Distribute 直接选不到渠道）。
func TestTotalFallbackNeverWritesEmptyModel(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":6,"prompt":"a cat"}`)
	require.True(t, strings.Contains(rec.body, `"model":"`+totalFallbackTarget+`"`))
	assert.NotContains(t, rec.body, `"model":""`)
}

// 编译期保证：目标模型常量与 CapCut 2.5 Flash 的对外名一致。
// 改错这个名字的后果是「改派到一个不存在的模型」→ Distribute 503。
func TestTotalFallbackTargetMatchesCapcutFlash(t *testing.T) {
	assert.Equal(t, "cp-seedance-2.5flash", seedanceroute_setting.TotalFallbackTargetModel())
}
