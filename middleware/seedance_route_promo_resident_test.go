package middleware

// 促销族常驻改派 + 自营短档（2026-11-23 老板口径）。
//
// 本文件锁六件事：
//  1. 带参考视频的请求**留在常驻**（客美 tj），计费原点仍是 seedance2.5-特惠；
//  2. 常驻改派把交付档钉成 720P（源链路对客恒定 720P），并把档位倍率覆盖成
//     源模型口径（否则客美 1.81 倍会把 ¥0.20/秒 乘成 ¥0.36/秒）；
//  3. 1080P / 21:9 / 目标无渠道 → 不改派（静默降档、静默改画幅都比「留在原链路」糟）；
//  4. >15 秒 → seedance-2-5（规则一 + seconds_trigger_seconds=15）；
//  5. ≤15 秒 → seedance-2-0-fast；开关打开且 9:16 → sd-seedance-2.0fast；
//  6. 自营档的边界（非 9:16 / 参考图 >4 张 / 带参考音视频 / 越界时长）→ 回落豆包 Fast。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// applyPromoPolicy 写一份「与生产同形」的策略：母本特惠走规则一/六 + 常驻改派，
// 活动版不在本文件用例范围内。满血档默认关闭（与升级当天一致）。
func applyPromoPolicy(t *testing.T, shortOwnEnabled bool, extraOverride map[string]interface{}) {
	t.Helper()
	applyPromoPolicyWithTiers(t, shortOwnEnabled, false, extraOverride)
}

// applyPromoPolicyWithTiers 同上，但可以单独指定满血档开关。
//
// 两个开关在写入口互斥；这里允许同时传 true 是为了验读侧兜底
// （配置被 SQL 手改过时，落哪一条必须是确定的）。
func applyPromoPolicyWithTiers(
	t *testing.T,
	shortOwnEnabled bool,
	shortFullEnabled bool,
	extraOverride map[string]interface{},
) {
	t.Helper()
	previous := seedanceroute_setting.Get()

	override := map[string]interface{}{
		"seconds_rule_enabled":    true,
		"seconds_trigger_seconds": 15,
		"short_rule_enabled":      true,
		"promo_resident_model":    seedanceroute_setting.DefaultPromoResidentModel,
		// 规则九（音视频 → CapCut）必须关掉：它在常驻之前判，开着会把
		// 带参考视频的请求截走，与「带参考视频留在常驻」直接冲突。
		"ref_av_rule_enabled":    false,
		"frames_rule_enabled":    false,
		"ratio_rule_enabled":     false,
		"ecommerce_rule_enabled": false,
	}
	for key, value := range extraOverride {
		override[key] = value
	}

	payload := map[string]interface{}{
		"enabled":                  true,
		"source_model":             "seedance2.5-特惠",
		"source_models":            []string{"seedance2.5-特惠"},
		"target_model":             "seedance-2-5",
		"trigger_seconds":          20,
		"short_trigger_seconds":    15,
		"short_fast_enabled":       true,
		"promo_short_own_enabled":  shortOwnEnabled,
		"promo_short_full_enabled": shortFullEnabled,
		"source_overrides": map[string]interface{}{
			"seedance2.5-特惠": override,
		},
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// TestPromoResidentRefVideoStaysOnResident 带参考视频 → 留常驻 tj。
func TestPromoResidentRefVideoStaysOnResident(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9","videos":["https://cdn.example.com/ref.mp4"]}`)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoResidentModel, decoded["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点必须是源模型")
	assert.Equal(t, float64(10), decoded["duration"], "时长要显式落成 duration（客美不读顶层 seconds）")
	assert.Equal(t, "720P", decoded["resolution"], "源链路对客恒定 720P，不能降成 480P")
	assert.Equal(t, "16:9", decoded["ratio"], "画幅原样保留")

	require.True(t, rec.hasBillingSize, "必须覆盖档位倍率")
	assert.InDelta(t, 1.0, rec.billingSizeRatio, 1e-9,
		"特惠 720P 倍率是 1.0（¥0.20/秒），不能被客美的 1.81 覆盖")
	assert.False(t, rec.hasBillingSec, "秒数交给客美 adaptor 算（生成 + 参考视频），不在这里钉死")
}

// TestPromoResidentSkips1080P 1080P 源链路本来会 400，不能改成静默 720P 交付。
func TestPromoResidentSkips1080P(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","resolution":"1080P","videos":["https://cdn.example.com/ref.mp4"]}`
	rec := runRouteFull(t, body)

	assert.Equal(t, body, rec.body, "1080P 请求必须原样放行，由源链路 fail-loud")
	assert.Empty(t, rec.originModel)
}

// TestPromoResidentSkipsUnsupportedRatio 21:9 客美会静默回落 16:9 → 不改派。
func TestPromoResidentSkipsUnsupportedRatio(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"21:9","videos":["https://cdn.example.com/ref.mp4"]}`
	rec := runRouteFull(t, body)

	assert.Equal(t, body, rec.body, "客美没有 21:9，改派过去等于替客户钉死画幅")
}

// TestPromoResidentSkipsBelowTargetMinDuration 客美 2026-11-24 起下限 5 秒：
// 2–4 秒的请求改派过去会被本地 400，必须留在源链路。
func TestPromoResidentSkipsBelowTargetMinDuration(t *testing.T) {
	for _, seconds := range []string{"2", "4"} {
		t.Run(seconds, func(t *testing.T) {
			applyPromoPolicy(t, false, nil)

			body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"` + seconds +
				`","videos":["https://cdn.example.com/ref.mp4"]}`
			rec := runRouteFull(t, body)

			assert.Equal(t, body, rec.body, seconds+" 秒低于客美下限，必须留在源链路")
			assert.Empty(t, rec.originModel)
		})
	}
}

// TestPromoResidentNoChannelKeepsSource 目标没渠道时不改写（改写后重试不会退回源模型）。
func TestPromoResidentNoChannelKeepsSource(t *testing.T) {
	applyPromoPolicy(t, false, nil)
	stubChannelAvailable(t, false)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","videos":["https://cdn.example.com/ref.mp4"]}`
	got, origin := runRouteRaw(t, body)

	assert.Equal(t, body, got, "目标无渠道时必须原样放行")
	assert.Empty(t, origin)
}

// TestPromoShortOwnRoutes916ToOwn 开关打开 + 9:16 + ≤15 秒 → 自营 sd- 档。
func TestPromoShortOwnRoutes916ToOwn(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16"}`)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decoded["model"])
	assert.Equal(t, "9:16", decoded["ratio"])
	assert.Equal(t, "720P", decoded["resolution"], "自营档固定 720P")
	assert.Equal(t, "10", decoded["seconds"], "秒数必须显式落盘（自营 adaptor 不给 seconds）")
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点仍是源模型")

	require.True(t, rec.hasBillingSec, "必须记源模型口径的秒数，否则按 1 秒收费")
	assert.InDelta(t, 10.0, rec.billingSeconds, 1e-9)
	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 1.0, rec.billingSizeRatio, 1e-9)
}

// TestPromoShortOwnOffFallsToDoubaoFast 开关关着 → 同一个请求走豆包 Fast。
func TestPromoShortOwnOffFallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel)
}

// TestPromoShortOwnNon916FallsToDoubaoFast 非 9:16 不走自营档（老板口径）。
func TestPromoShortOwnNon916FallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"16:9 改派到固定 9:16 的自营档 = 静默改画幅")
}

// TestPromoRuleSixLiftsWan3MediaToInputReference 规则六（16:9 → 豆包 Fast）与自营档同口径：
// ArcReel 的有赞 Wan3 形状把参考图放在 metadata.input.media，manwu 只认顶层别名，
// 不抬就是静默丢图。2026-11-25 任务 4047（特惠 / 15 秒 / 16:9 / 8 张图）实测
// 上游回「抱歉，没有参考图」并全额退款。
func TestPromoRuleSixLiftsWan3MediaToInputReference(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15",` +
		`"metadata":{"ratio":"16:9","input":{"media":[` +
		`{"type":"reference_image","url":"https://cdn.example.com/1.png"},` +
		`{"type":"reference_image","url":"https://cdn.example.com/2.png"}]}}}`
	rec := runRouteFull(t, body)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, "seedance-2-0-fast", decoded["model"], "16:9 + 15 秒应走豆包 Fast")

	refs, ok := decoded["input_reference"].([]interface{})
	require.True(t, ok, "参考图必须抬到顶层 input_reference，否则 manwu 丢弃")
	require.Len(t, refs, 2)
	assert.Equal(t, "https://cdn.example.com/1.png", refs[0])

	_, hasMeta := decoded["metadata"]
	assert.False(t, hasMeta, "metadata 里的旧素材位置要剥掉，否则同图投两次")
	assert.Equal(t, "16:9", decoded["ratio"], "metadata.ratio 必须抬到顶层，否则画幅丢失")
}

// TestPromoRuleSixSkipsDataURIReferenceImage 漫屋只收公网 http(s)：
// data URI 参考图改派 Fast 必 400，规则六必须放弃（后续由常驻链路按 Wan3 体接手）。
func TestPromoRuleSixSkipsDataURIReferenceImage(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15",` +
		`"metadata":{"input":{"media":[{"type":"reference_image","url":"data:image/png;base64,AAAA"}]}}}`
	rec := runRouteFull(t, body)

	decoded := decodeBody(t, rec.body)
	assert.NotEqual(t, "seedance-2-0-fast", decoded["model"],
		"data URI 参考图不能改派豆包 Fast（会被上游 400/丢图）")
	assert.NotNil(t, decoded["metadata"], "素材仍保留在 Wan3 形状里交给能处理它的链路")
}

// TestPromoShortOwnAccepts9Images 9 张参考图是自营档的上限（官网 Video 档口径），必须路由成功。
func TestPromoShortOwnAccepts9Images(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	images := make([]string, 0, 9)
	for i := 0; i < 9; i++ {
		images = append(images, fmt.Sprintf(`"https://a/%d.png"`, i))
	}
	body := fmt.Sprintf(`{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16","images":[%s]}`, strings.Join(images, ","))
	rec := runRouteFull(t, body)

	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decodeBody(t, rec.body)["model"],
		"9 张参考图在自营档上限内（2026-11-22 官网 Video 档真机取证），必须走自营档")
}

// TestPromoShortOwnLiftsMetadataInputMediaToTopLevel 源图只在 metadata.input.media
// （ArcReel 的有赞 Wan3 形状）时，改派自营短档必须把图抬到 manwu 白名单字段。
//
// 2026-11-24 A 机事故（任务 09:45，seedance2.5-特惠 / 15 秒 / 9:16 / 1 张图）：
// 路由命中自营短档后只换了 model / resolution / ratio / seconds，图还留在
// metadata.input.media；manwu adaptor 只认顶层别名（referenceImageFields），
// 日志 `source=<none> count=0` → Worker 判「TikTok 图生视频至少需要 1 张参考图」
// 直接失败。客户看到的是「提供了图片，却显示没有图」。
func TestPromoShortOwnLiftsMetadataInputMediaToTopLevel(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15",` +
		`"metadata":{"ratio":"9:16","aspect_ratio":"9:16","input":{"media":[` +
		`{"type":"reference_image","url":"https://cdn.example.com/a.png"}]}}}`
	rec := runRouteFull(t, body)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decoded["model"])

	lifted, ok := decoded["input_reference"].([]interface{})
	require.True(t, ok, "参考图必须抬到 manwu 白名单字段 input_reference，否则 adaptor 解析 0 张")
	require.Len(t, lifted, 1, "抬图不能丢也不能重复投递")
	assert.Equal(t, "https://cdn.example.com/a.png", lifted[0])

	// 源位置不能留副本：CapCut 等读 metadata.media / metadata.input.media 的目标
	// 会与顶层 input_reference 重复投同一张图。
	metadata, ok := decoded["metadata"].(map[string]interface{})
	if ok {
		assert.NotContains(t, metadata, "media", "metadata.media 必须摘掉")
		if input, hasInput := metadata["input"].(map[string]interface{}); hasInput {
			assert.NotContains(t, input, "media", "metadata.input.media 必须摘掉")
		}
	}
}

// TestPromoShortOwnTooManyImagesFallsToDoubaoFast 参考图 >9 张时自营档接不住。
func TestPromoShortOwnTooManyImagesFallsToDoubaoFast(t *testing.T) {
	applyPromoPolicy(t, true, nil)

	images := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		images = append(images, fmt.Sprintf(`"https://a/%d.png"`, i))
	}
	body := fmt.Sprintf(`{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16","images":[%s]}`, strings.Join(images, ","))
	rec := runRouteFull(t, body)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"10 张参考图超过自营档上限 9 张，必须回落豆包 Fast")
}

// TestPromoLongSecondsGoesDoubao25 >15 秒 → seedance-2-5（规则一 + 下界 15）。
func TestPromoLongSecondsGoesDoubao25(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"20","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-5", decodeBody(t, rec.body)["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel)
	require.True(t, rec.hasBillingSec)
	assert.InDelta(t, 20.0, rec.billingSeconds, 1e-9, "计费秒数按客户请求的 20 秒")
}

// TestPromoExactly15SecondsGoesShort 15 秒（含）算短档 —— 老板口径「>15 才算长」。
func TestPromoExactly15SecondsGoesShort(t *testing.T) {
	applyPromoPolicy(t, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
		"15 秒必须走短档：老板口径是「大于 15 秒」才路由到 seedance-2-5")
}

// TestPromoShortFullRoutesToDoubaoStandard 满血档打开：促销族 ≤15 秒请求
// （不带参考音视频/首尾帧、720P）改派 seedance-2-0（豆包官网标准版）。
//
// 参考图必须与豆包 Fast 同一条抬升路径：ArcReel 发的有赞 Wan3 形状把图放在
// metadata.input.media，manwu 只认顶层白名单字段 —— 不抬就是重演 2026-11-25
// 任务 4047「提供了图片却显示没有图」。
func TestPromoShortFullRoutesToDoubaoStandard(t *testing.T) {
	applyPromoPolicyWithTiers(t, false, true, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15",` +
		`"metadata":{"ratio":"16:9","input":{"media":[` +
		`{"type":"reference_image","url":"https://cdn.example.com/1.png"},` +
		`{"type":"reference_image","url":"https://cdn.example.com/2.png"}]}}}`
	rec := runRouteFull(t, body)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortFullModel, decoded["model"])
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点必须是源模型")

	refs, ok := decoded["input_reference"].([]interface{})
	require.True(t, ok, "参考图必须抬到顶层 input_reference，否则 manwu 丢弃")
	require.Len(t, refs, 2)
	assert.Equal(t, "https://cdn.example.com/1.png", refs[0])

	_, hasMeta := decoded["metadata"]
	assert.False(t, hasMeta, "metadata 必须整块剥掉，否则同图投两次")
	assert.Equal(t, "16:9", decoded["ratio"], "画幅要抬到顶层，否则丢失")
	assert.Equal(t, "15", decoded["seconds"], "秒数必须显式落盘")

	require.True(t, rec.hasBillingSec, "必须记源模型口径的秒数")
	assert.InDelta(t, 15.0, rec.billingSeconds, 1e-9)
	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 1.0, rec.billingSizeRatio, 1e-9)
}

// TestPromoShortFullOffFallsToDoubaoFast 两个都不勾 → 豆包 Fast（老板口径）。
func TestPromoShortFullOffFallsToDoubaoFast(t *testing.T) {
	applyPromoPolicyWithTiers(t, false, false, nil)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9"}`)

	assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"])
}

// TestPromoShortFullSkipsNonFixedResolution 标准版固定 720p：显式 480P/1080P
// 改派过去会被 adaptor 本地 400，必须留给支持三档的豆包 Fast。
func TestPromoShortFullSkipsNonFixedResolution(t *testing.T) {
	for _, resolution := range []string{"480P", "1080P"} {
		t.Run(resolution, func(t *testing.T) {
			applyPromoPolicyWithTiers(t, false, true, nil)

			rec := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","resolution":"`+resolution+`"}`)

			assert.Equal(t, "seedance-2-0-fast", decodeBody(t, rec.body)["model"],
				resolution+" 不能改派固定 720p 的标准版（会本地 400）")
		})
	}
}

// TestPromoShortFullSkipsDataURIReferenceImage 与豆包 Fast 同口径：
// data URI 参考图改派标准版必 400，必须放弃这条分支（交给后面的链路）。
func TestPromoShortFullSkipsDataURIReferenceImage(t *testing.T) {
	applyPromoPolicyWithTiers(t, false, true, nil)

	body := `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"15",` +
		`"metadata":{"input":{"media":[{"type":"reference_image","url":"data:image/png;base64,AAAA"}]}}}`
	rec := runRouteFull(t, body)

	decoded := decodeBody(t, rec.body)
	assert.NotEqual(t, seedanceroute_setting.DefaultPromoShortFullModel, decoded["model"])
	assert.NotNil(t, decoded["metadata"], "素材仍保留在 Wan3 形状里交给能处理它的链路")
}

// TestPromoShortOwnWinsWhenBothEnabled 读侧兜底：配置被 SQL 手改到两个都开时，
// 9:16 归自营档、其它比例归满血档 —— 落哪一条必须是确定的。
func TestPromoShortOwnWinsWhenBothEnabled(t *testing.T) {
	applyPromoPolicyWithTiers(t, true, true, nil)

	own := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"9:16"}`)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortOwnModel, decodeBody(t, own.body)["model"],
		"两个都开时自营档先判（9:16 是它的专属区间）")

	full := runRouteFull(t, `{"model":"seedance2.5-特惠","prompt":"a cat","seconds":"10","ratio":"16:9"}`)
	assert.Equal(t, seedanceroute_setting.DefaultPromoShortFullModel, decodeBody(t, full.body)["model"],
		"自营档接不住的非 9:16 请求由满血档接手")
}
