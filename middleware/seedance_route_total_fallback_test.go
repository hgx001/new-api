package middleware

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 规则八（全量兜底 → CapCut，按时长分档）的中间件测试。
//
// 语义：seedance2.5-特惠 / seedance2.5-活动 的常驻链路是有赞。有赞
// **彻底不能用**时，前七条规则全部失效（它们都假设源链路还能跑一部分）。
// 这条是唯一不依赖该前提的逃生口：开关一开，请求**无条件**改派 CapCut，
// 不看比例/提示词内容/首尾帧。
//
// 2026-11-18 起目标按时长在 CapCut 族内分档：
//
//	≤15 秒 → cp-seedance-2.0（短档）；>15 秒 → cp-seedance-2.5flash（长档）。
//
// 本文件锁三类行为：
//  1. 「无条件」真的无条件 —— 短请求、长请求、非电商提示词、非 9:16 都改派；
//  2. **分档边界**（15/16 秒、参考图 9/10 张）—— 差一档就是另一条上游、
//     另一套价格，而两条链路在后台看起来只差一个模型名；
//  3. 目标链路的**物理能力**边界不改派（1080P / 2–3 秒 / 首尾帧 /
//     自适应比例带参考图）—— 那几类改派过去必 400。

const totalFallbackTarget = "cp-seedance-2.5flash"
const totalFallbackShortTarget = "cp-seedance-2.0"

// enableTotalFallback 只打开规则八（前七条都不配），测试结束恢复原状。
//
// 2026-11-17 起促销族改派 CapCut 恒为 720p 原生直出，与超分配置无关
// （见 middleware/seedance_route_ecommerce.go 的 capcutGenerationResolutionValue）。
func enableTotalFallback(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":                true,
		"source_models":          []string{testSourceModel, "seedance2.5-活动"},
		"total_fallback_enabled": true,
	}
	withSecondsRuleOff(payload, testSourceModel, testSourceModelAlt)
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, err := common.Marshal(previous)
		require.NoError(t, err)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 12 秒的普通请求（不超时长阈值、非 9:16、提示词不是电商）也要改派 ——
// 这正是「无条件」与规则六/七的区别。
//
// ⚠️ 用例时长必须 ≥10 秒：2026-11-15 起 <10 秒的请求**跳过规则八**
// （老板口径：<10 秒落 CapCut 满血档 cp-seedance-2.0，见
// TestTotalFallbackSkipsBelowTenSeconds）。
func TestTotalFallbackRoutesShortPlainRequest(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat",
		"metadata":{"ratio":"16:9"},
		"media":[{"type":"reference_image","url":"https://x/a.png"}]}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackShortTarget, body["model"], "12 秒必须落规则八短档")
	assert.Equal(t, "12", body["seconds"], "时长必须原样下发（字符串形态）")
	assert.Equal(t, "720p", body["resolution"], "生成档必须钉 720p 原生直出（不再超分）")
	assert.Equal(t, "16:9", body["ratio"], "比例必须显式落到顶层 ratio")

	images, _ := body["input_reference"].([]interface{})
	require.Len(t, images, 1, "参考图必须抬到顶层 input_reference")
	assert.Equal(t, "https://x/a.png", images[0])
	assert.NotContains(t, body, "media", "顶层 media 必须剥掉，否则同图投两次")

	// 计费原点仍是客户请求的源模型：改派不改对客价格。
	assert.Equal(t, testSourceModel, rec.originModel)
	assert.True(t, rec.hasBillingSec)
	assert.Equal(t, float64(12), rec.billingSeconds)
}

// 带参考图但**没指定比例**时也不改派：源链路缺省是 adaptive（跟随参考图），
// CapCut 没有这一档 —— 替他挑一个画幅就是替客户改需求。
func TestTotalFallbackSkipsAdaptiveRatioWithReferenceImage(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat",
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

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":15,
		"metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackShortTarget, body["model"])
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

// 超分配置与否不再影响促销族改派的生成档（已彻底解耦），恒为 720p 原生。
func TestTotalFallbackResolutionIgnoresSuperResolutionConfig(t *testing.T) {
	for _, configured := range []bool{true, false} {
		enableTotalFallback(t)

		rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
		body := decodeBody(t, rec.body)

		assert.Equal(t, totalFallbackShortTarget, body["model"])
		assert.Equal(t, "720p", body["resolution"], "超分配置=%v 时都应原生出 720p", configured)
	}
}

// 生成档 720p 的对客价格仍按源模型 720P 档算
// （size 倍率来自 sourceModelSizeRatio，覆盖 CapCut 自己的档位差价）。
//
// 漏了这条覆盖的后果：CapCut adaptor 按自己的档位价给倍率，
// 客户价就跟着调度策略漂（换目标档 = 换价格）。
func TestTotalFallbackKeepsSourceTierBillingRatio(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	require.Equal(t, totalFallbackShortTarget, body["model"])
	require.Equal(t, "720p", body["resolution"])
	assert.True(t, rec.hasBillingSize, "必须显式记源模型口径的 size 倍率")
	assert.Equal(t, 1.0, rec.billingSizeRatio, "720P 源档的倍率是 1.0")
}

// 1080P 不改派：CapCut 只卖 480p/720p，改派过去必 400。 留原链路至少能得到准确的原因（没有可用渠道），而不是把链路故障
// 伪装成「参数不合法」。
func TestTotalFallbackSkips1080P(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":12,"resolution":"1080P","prompt":"a cat"}`
	rewritten, origin := runRoute(t, body)

	assert.Equal(t, body, rewritten, "物理接不住的请求必须原样放行，不能半改写")
	assert.Empty(t, origin, "没改派就不该留计费原点")
}

// <10 秒的请求**也**走规则八（触发上无条件），但 2026-11-18 起落在短档
// cp-seedance-2.0 —— 规则六的 ≤10 秒分支在规则八开着时根本没机会执行。
//
// 注意这与 11-15 的旧口径不同：那时 <10 秒是**跳过规则八**去落规则六，
// 也就是留在源链路；现在两条都是 CapCut 出口，只是档位不同。
func TestTotalFallbackTakesBelowTenSecondsToo(t *testing.T) {
	enableTotalFallback(t)

	rewritten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":8,"prompt":"a cat"}`)
	decoded := decodeBody(t, rewritten)

	assert.Equal(t, totalFallbackShortTarget, decoded["model"],
		"规则八是无条件全量兜底，8 秒同样改派且落短档")
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

	rewritten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":12,
		"media":[{"type":"reference_video","url":"https://x/a.mp4"}],"prompt":"a cat"}`)
	decoded := decodeBody(t, rewritten)

	assert.Equal(t, totalFallbackShortTarget, decoded["model"], "带参考视频应改派（12 秒落短档）")
	assert.Equal(t, []interface{}{"https://x/a.mp4"}, decoded["videos"],
		"参考视频必须抬到顶层 videos")
	assert.NotContains(t, decoded, "media", "media 必须摘掉，否则同一视频投两次")
}

// 带参考音频照常改派，素材抬到顶层 audios（2026-11-15 放开）。
func TestTotalFallbackRoutesReferenceAudio(t *testing.T) {
	enableTotalFallback(t)

	rewritten, _ := runRoute(t, `{"model":"seedance2.5-特惠","seconds":12,"audios":["https://x/a.mp3"],"prompt":"a cat"}`)
	decoded := decodeBody(t, rewritten)

	assert.Equal(t, totalFallbackShortTarget, decoded["model"], "带参考音频应改派（12 秒落短档）")
	assert.Equal(t, []interface{}{"https://x/a.mp3"}, decoded["audios"],
		"参考音频必须抬到顶层 audios")
}

// 首尾帧不改派：CapCut 只有参考图语义，改派过去等于把首尾帧降级成参考图。
// 它应该自然落到规则三（配了首尾帧目标的话），而不是被规则八吞掉。
func TestTotalFallbackSkipsFrameMedia(t *testing.T) {
	enableTotalFallback(t)

	const body = `{"model":"seedance2.5-特惠","seconds":12,
		"media":[{"type":"first_frame","url":"https://x/a.png"}],"prompt":"a cat"}`
	rewritten, _ := runRoute(t, body)

	assert.Equal(t, body, rewritten)
}

// 目标链路没有可用渠道时不改派：改写之后重试只会用同一个模型名重选渠道，
// 放开就是客户撞 503。预检能让请求自然落回源链路。
func TestTotalFallbackSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableTotalFallback(t)
	stubChannelAvailable(t, false)

	const body = `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat"}`
	rewritten, _ := runRouteRaw(t, body)

	assert.Equal(t, body, rewritten)
}

// 没传时长时按 5 秒下发：这是源链路与 CapCut **共同**的缺省值，
// 改派前后成片长度一致。
// 没传时长 → 规则八按缺省 5 秒处理，并按 5 秒落短档 cp-seedance-2.0。
func TestTotalFallbackDefaultsMissingSecondsToFive(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat"}`)
	body := decodeBody(t, rec.body)

	assert.Equal(t, totalFallbackShortTarget, body["model"],
		"规则八是无条件全量兜底，没传时长（缺省 5 秒）同样改派且落短档")
	assert.Equal(t, "5", body["seconds"],
		"缺省时长必须按 5 秒下发 —— 与源链路、CapCut 的共同缺省一致")
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
	// 规则一按源关掉：本条验的是「规则八关着时不要因为规则八改派」。
	// 规则一自 2026-11-20 起是「无音视频即全量改派」，开着会把 12 秒请求
	// 送去豆包，掩盖掉规则八的行为。
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(
		`{"enabled":true,"source_models":["seedance2.5-特惠"],"target_model":"seedance-2-5","trigger_seconds":20,"source_overrides":{"seedance2.5-特惠":{"seconds_rule_enabled":false}}}`))

	const body = `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat"}`
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

	// 12 秒 → 规则八短档。
	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":12,"prompt":"a cat"}`)
	require.True(t, strings.Contains(rec.body, `"model":"`+totalFallbackShortTarget+`"`))
	assert.NotContains(t, rec.body, `"model":""`)
}

// ── 2026-11-18 分档边界回归锁 ────────────────────────────────────────
//
// 这两条是本次需求的核心不变量：15 秒含边界走短档，16 秒走长档；
// 参考图 9 张走短档，10 张落长档。差一格就是另一条上游、另一套价格，
// 而代码里只是 `<=` 与 `<` 的差别 —— 编译和类型检查都抓不住。
func TestTotalFallbackTierBoundaryBySeconds(t *testing.T) {
	enableTotalFallback(t)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":15,"prompt":"a cat"}`)
	body := decodeBody(t, rec.body)
	assert.Equal(t, totalFallbackShortTarget, body["model"], "15 秒含边界必须走短档")

	rec = runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":16,"prompt":"a cat"}`)
	body = decodeBody(t, rec.body)
	assert.Equal(t, totalFallbackTarget, body["model"], "16 秒必须走长档")
	assert.Equal(t, "16", body["seconds"], "分档不能改变下发的时长")
}

func TestTotalFallbackShortTierReferenceImageLimit(t *testing.T) {
	enableTotalFallback(t)

	// 9 张（含边界）仍走短档。显式给 ratio —— 带参考图而无比例会被
	// CapCut 的 adaptive 闸门挡掉，那是另一条不变量，不是这里要测的。
	nine := `{"model":"seedance2.5-特惠","seconds":12,"ratio":"16:9","prompt":"a cat","images":[`
	for i := 0; i < 9; i++ {
		if i > 0 {
			nine += ","
		}
		nine += fmt.Sprintf(`"https://x/%d.png"`, i)
	}
	nine += `]}`
	body := decodeBody(t, runRouteFull(t, nine).body)
	assert.Equal(t, totalFallbackShortTarget, body["model"], "9 张参考图应走短档")

	ten := strings.Replace(nine, `"https://x/8.png"`, `"https://x/8.png","https://x/9.png"`, 1)
	body = decodeBody(t, runRouteFull(t, ten).body)
	assert.Equal(t, totalFallbackTarget, body["model"],
		"10 张参考图必须落长档，而不是留在已挂掉的源链路")
}

// 编译期保证：两个目标模型常量与 CapCut 对外名一致。
// 改错名字的后果是「改派到一个不存在的模型」→ Distribute 503。
func TestTotalFallbackTargetMatchesCapcutFlash(t *testing.T) {
	assert.Equal(t, "cp-seedance-2.5flash", seedanceroute_setting.TotalFallbackTargetModel())
	assert.Equal(t, "cp-seedance-2.0", seedanceroute_setting.DefaultTotalFallbackShortTargetModel)
}
