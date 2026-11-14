package manwu

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CapCut（剪映国际版）路由与计价契约（2026-11-13 接入，同日扩到五档）。
//
// 口径：
//   - 公开名 cp-seedance-2.5 / -2.5flash / -2.0 / -2.0fast / -2.0mini
//     → 官网档位真名 seedance_2.5 / seedance_2.5_flash / seedance_2.0 /
//       seedance_2.0_fast / seedance_2.0_mini；
//   - 平台 platform_id = capcut（一级建单，不走豆包路由）；
//   - 时长：2.5 与 2.5 Flash = 4–30 秒，其余 4–15 秒，整数秒原样透传、不吸附；
//   - 分辨率 480p / 720p（1080p / 4K 未定价 → 显式 400，不静默降级）；
//   - 参考图 ≤9 张（超出显式 400，不静默截断）；
//   - 计价：五档**统一按秒**（seconds 倍率），720p 再加 size 倍率。

func TestCapcutModelSpecsRouteToCapcutPlatform(t *testing.T) {
	cases := map[string]string{
		ModelCapcut25:      capcutUpstreamModel25,
		ModelCapcut25Flash: capcutUpstreamModel25Flash,
		ModelCapcut20:      capcutUpstreamModel20,
		ModelCapcut20Fast:  capcutUpstreamModel20Fast,
		ModelCapcut20Mini:  capcutUpstreamModel20Mini,
	}
	for model, upstream := range cases {
		spec, ok := SpecFor(model)
		require.True(t, ok, model)
		assert.Equal(t, PlatformIDCapcut, spec.PlatformID, model)
		assert.Equal(t, OutputModeVideo, spec.OutputMode, model)
		assert.Equal(t, kindCapcutVideo, spec.Kind, model)
		assert.Equal(t, upstream, spec.UpstreamModel, model)
	}
}

// 上游真名常量必须是官网模型卡的 data-model-key，一个字都不能错。
func TestCapcutUpstreamModelNames(t *testing.T) {
	assert.Equal(t, "seedance_2.5", capcutUpstreamModel25)
	assert.Equal(t, "seedance_2.5_flash", capcutUpstreamModel25Flash)
	assert.Equal(t, "seedance_2.0", capcutUpstreamModel20)
	assert.Equal(t, "seedance_2.0_fast", capcutUpstreamModel20Fast)
	assert.Equal(t, "seedance_2.0_mini", capcutUpstreamModel20Mini)
}

// 公开名 ↔ 上游真名的**双向**映射必须闭合：
// 公开名 → 真名（modelSpecs.UpstreamModel）→ 再归一回公开名，得到的必须是
// 原来的公开名，不能串到别的档位。
//
// ⚠️ 这一条正是防「2.5 Flash 被当成 2.5」的关键：真名 seedance_2.5_flash
// 与 seedance_2.5 长得不像，但公开名 cp-seedance-2.5flash **包含** cp-seedance-2.5，
// 一旦某处用子串匹配判档，反向归一就会把两个档位串起来。
func TestCapcutOfficialToPublicMappingIsBidirectional(t *testing.T) {
	for model, upstream := range map[string]string{
		ModelCapcut25:      capcutUpstreamModel25,
		ModelCapcut25Flash: capcutUpstreamModel25Flash,
		ModelCapcut20:      capcutUpstreamModel20,
		ModelCapcut20Fast:  capcutUpstreamModel20Fast,
		ModelCapcut20Mini:  capcutUpstreamModel20Mini,
	} {
		spec, ok := SpecFor(model)
		require.True(t, ok, model)
		assert.Equal(t, upstream, spec.UpstreamModel, model)
		assert.Equal(t, model, capcutPublicNameFor(spec.UpstreamModel), model)
	}
	// 未知真名原样返回：不编造，也不静默落到别的档位。
	assert.Equal(t, "some_unknown_model", capcutPublicNameFor("some_unknown_model"))
}

// 与豆包 `seedance-2-0-fast` 同名族但**不互为别名**：豆包公开名不能路由到 CapCut。
func TestCapcutPublicNamesDoNotCollideWithDoubao(t *testing.T) {
	doubao, ok := SpecFor(ModelDoubao20Fast)
	require.True(t, ok)
	assert.Equal(t, PlatformIDDoubao, doubao.PlatformID)
	assert.NotEqual(t, PlatformIDCapcut, doubao.PlatformID)

	_, ok = SpecFor("seedance_2.0_fast")
	assert.False(t, ok, "官网真名不是对外公开名，不能被直接调用")

	_, ok = SpecFor(capcutUpstreamModel25Flash)
	assert.False(t, ok, "官网真名不是对外公开名，不能被直接调用")
}

func TestModelListContainsCapcutModels(t *testing.T) {
	names := ModelList()
	for _, model := range []string{
		ModelCapcut25, ModelCapcut25Flash, ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini,
	} {
		assert.Contains(t, names, model)
	}
}

func TestValidateCapcutDurationRange(t *testing.T) {
	for _, v := range []int{capcutMinDuration, 10, capcutMaxDuration} {
		got, err := validateCapcutDuration("seconds", v, ModelCapcut20)
		require.NoError(t, err)
		assert.Equal(t, v, got)
	}
	for _, v := range []int{3, 16, 30, 0} {
		_, err := validateCapcutDuration("seconds", v, ModelCapcut20)
		require.Error(t, err, "越界时长必须显式失败")
	}
}

// 2.5 与 2.5 Flash 的上界放宽到 30 秒（真机滑杆 max=30000ms），其余模型仍锁 15 秒。
func TestValidateCapcutDurationUpperBoundDependsOnModel(t *testing.T) {
	for _, model := range []string{ModelCapcut25, ModelCapcut25Flash} {
		got, err := validateCapcutDuration("seconds", 30, model)
		require.NoError(t, err, model)
		assert.Equal(t, 30, got, model)

		_, err = validateCapcutDuration("seconds", 31, model)
		require.Error(t, err, model)
	}

	for _, model := range []string{ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini} {
		_, err := validateCapcutDuration("seconds", 30, model)
		require.Error(t, err, "%s 不能到 30 秒", model)
	}
}

// ⚠️ **子串误判守卫**：公开名 `cp-seedance-2.5flash` 包含 `cp-seedance-2.5`。
// 任何用 strings.Contains / 前缀匹配来判「是不是 2.5 档」的写法，都会把
// 2.5 Flash 误判成 2.5（或反过来）。
//
// 本测试钉死两件事：
//
//	① 2.5 Flash 的时长上界确实是 30（与 2.5 同档，不是被当成 2.0 的 15）；
//	② 但它的**其它**档位属性与 2.5 **不同**（上游真名不同），说明两档没有被合并成一条。
func TestCapcut25FlashIsNotConfusedWith25BySubstring(t *testing.T) {
	assert.Equal(t, 30, capcutMaxDurationFor(ModelCapcut25Flash))
	assert.Equal(t, 30, capcutMaxDurationFor(ModelCapcut25))
	// 两档的上界一样，但真名必须不同 —— 真名相同就等于两个名字一个商品。
	assert.NotEqual(t, capcutUpstreamModel25, capcutUpstreamModel25Flash)

	// 公开名不同（防「两档其实指向同一个 spec」）。
	assert.NotEqual(t, ModelCapcut25, ModelCapcut25Flash)

	// 子串关系必须**确实存在** —— 这条守卫的前提。真哪天改名了让它们不再有
	// 子串关系，这条会先红，提醒你重新评估这个守卫还有没有意义。
	assert.True(t, strings.Contains(ModelCapcut25Flash, ModelCapcut25),
		"前提：cp-seedance-2.5flash 必须包含 cp-seedance-2.5，"+
			"否则本组用例失去意义（守卫对象不再存在）")
}

// 未登记的模型名按最窄档（15 秒）处理 —— 宁可少卖，不能因为漏登记就放宽到 30 秒。
func TestCapcutMaxDurationForUnknownModelFallsBackToNarrowest(t *testing.T) {
	assert.Equal(t, capcutMaxDuration, capcutMaxDurationFor("cp-seedance-9.9"))
	assert.Equal(t, capcutMaxDuration, capcutMaxDurationFor(""))
}

func TestResolveCapcutDurationFallsBackToPanelDefault(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, capcutDefaultDuration, req.Duration)

	c2, info2, a2 := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":12}`)
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, info2))
	req2, err := getNormalizedRequest(c2)
	require.NoError(t, err)
	assert.Equal(t, 12, req2.Duration)

	c3, info3, a3 := postVideoCtx(t, `{"model":"cp-seedance-2.5","prompt":"hi","seconds":24}`)
	require.Nil(t, a3.ValidateRequestAndSetAction(c3, info3))
	req3, err := getNormalizedRequest(c3)
	require.NoError(t, err)
	assert.Equal(t, 24, req3.Duration)
}

// 越界时长在入口就 400（不建单、不扣费）。
func TestCapcutRejectsOutOfRangeDuration(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","seconds":30}`)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err)
}

// 2.5 Flash 的 30 秒上限在**入口校验层**就要放行（不是只在 validateCapcutDuration
// 单元里过）—— 走通完整链路，落库时长确实是 30。
func TestCapcut25FlashAccepts30Seconds(t *testing.T) {
	c, info, a := postVideoCtx(t,
		`{"model":"`+ModelCapcut25Flash+`","prompt":"hi","seconds":30}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 30, req.Duration)

	// 31 秒越界 → 入口 400。
	c2, info2, a2 := postVideoCtx(t,
		`{"model":"`+ModelCapcut25Flash+`","prompt":"hi","seconds":31}`)
	require.NotNil(t, a2.ValidateRequestAndSetAction(c2, info2))
}

// 分辨率只卖 480p / 720p：1080p、4K 显式拒绝，不静默降级。
func TestCapcutRejectsUnpricedResolution(t *testing.T) {
	for _, resolution := range []string{"1080p", "4k", "360p"} {
		c, info, a := postVideoCtx(t,
			`{"model":"cp-seedance-2.0","prompt":"hi","resolution":"`+resolution+`"}`)
		err := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, err, resolution)
	}
}

// 2.5 Flash 的分辨率口径与其余四档一致：只认 480p / 720p。
func TestCapcut25FlashRejectsUnpricedResolution(t *testing.T) {
	for _, resolution := range []string{"1080p", "4k", "360p"} {
		c, info, a := postVideoCtx(t,
			`{"model":"`+ModelCapcut25Flash+`","prompt":"hi","resolution":"`+resolution+`"}`)
		err := a.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, err, resolution)
	}

	// 720p 放行。
	c, info, a := postVideoCtx(t,
		`{"model":"`+ModelCapcut25Flash+`","prompt":"hi","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "720p", req.Resolution)
}

// 720p 是已定价档位，必须放行并原样落到 videoParams。
func TestCapcutAccepts720p(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0","prompt":"hi","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "720p", req.Resolution)
}

// 参考图第 10 张显式拒绝（不静默截断到 9 张）。
// 参考图上限 30 张（2026-11-14 用户口径，从 9 放宽到 30）。
// 30 张通过，31 张显式拒绝 —— 静默截断会让客户按 30 张下单却少了一张约束。
func TestCapcutReferenceImagesLimitIs30(t *testing.T) {
	build := func(n int) string {
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprintf(`"https://a/%d.jpg"`, i))
		}
		return fmt.Sprintf(
			`{"model":"cp-seedance-2.0","prompt":"hi","input_reference":[%s]}`,
			strings.Join(items, ","))
	}

	// 30 张：通过
	c, info, a := postVideoCtx(t, build(30))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	// 31 张：显式拒绝
	c2, info2, a2 := postVideoCtx(t, build(31))
	err := a2.ValidateRequestAndSetAction(c2, info2)
	require.NotNil(t, err)
}

// 计价：五档全部按秒（漏一档 = 只收 1 秒的钱）。
func TestCapcutBillingIsPerSecondForAllModels(t *testing.T) {
	for _, model := range []string{
		ModelCapcut25, ModelCapcut25Flash, ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini,
	} {
		c, info, a := postVideoCtx(t,
			`{"model":"`+model+`","prompt":"hi","seconds":8}`)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), model)
		ratios := a.EstimateBilling(c, info)
		require.NotNil(t, ratios, model)
		assert.Equal(t, float64(8), ratios["seconds"], model)
		_, hasSize := ratios["size"]
		assert.False(t, hasSize, "%s 480p 不应有 size 倍率", model)
	}
}

// 720p 的 size 倍率：mini / fast = 1.5，2.0 / 2.5 / 2.5 Flash = 2。
func TestCapcut720pSizeRatio(t *testing.T) {
	cases := map[string]float64{
		ModelCapcut20Mini: 1.5,
		ModelCapcut20Fast: 1.5,
		ModelCapcut20:     2.0,
		ModelCapcut25:     2.0,
		// 2.5 Flash 定价与 2.0 一致 → 720p 倍率也是 2（用户口径：
		// 「2.5 flash 定价和 2.0 一致，2.0 的 720p 倍率是 2」）。
		ModelCapcut25Flash: 2.0,
	}
	for model, want := range cases {
		c, info, a := postVideoCtx(t,
			`{"model":"`+model+`","prompt":"hi","seconds":5,"resolution":"720p"}`)
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), model)
		ratios := a.EstimateBilling(c, info)
		require.NotNil(t, ratios, model)
		assert.Equal(t, float64(5), ratios["seconds"], model)
		assert.Equal(t, want, ratios["size"], model)
	}
}

// 建单载荷：model 传官网档位真名，时长/比例/分辨率透传（默认 480p 显式下发）。
func TestCapcutBuildRequestBodyUsesUpstreamModelKey(t *testing.T) {
	c, info, a := postVideoCtx(t, `{"model":"cp-seedance-2.0fast","prompt":"hi","seconds":9,"ratio":"9:16"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capcutUpstreamModel20Fast, vp["model"])
	assert.Equal(t, float64(9), vp["duration"])
	assert.Equal(t, "9:16", vp["ratio"])
	assert.Equal(t, "480p", vp["resolution"], "缺省分辨率显式下发 480p（价格基准档）")
}

func TestCapcutBuildRequestBodyForMini720p(t *testing.T) {
	c, info, a := postVideoCtx(t,
		`{"model":"cp-seedance-2.0mini","prompt":"hi","seconds":4,"ratio":"9:16","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capcutUpstreamModel20Mini, vp["model"])
	assert.Equal(t, float64(4), vp["duration"])
	assert.Equal(t, "720p", vp["resolution"])
}

// 2.5 Flash 建单载荷：model 必须传官网真名 seedance_2.5_flash
// （**不是** seedance_2.5，也不是公开名），时长/分辨率透传。
func TestCapcutBuildRequestBodyFor25Flash(t *testing.T) {
	c, info, a := postVideoCtx(t,
		`{"model":"`+ModelCapcut25Flash+`","prompt":"hi","seconds":30,"ratio":"16:9","resolution":"720p"}`)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	reader, err := a.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(data, &payload))
	vp, ok := payload["videoParams"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, capcutUpstreamModel25Flash, vp["model"],
		"2.5 Flash 必须下发自己的官网真名，不能落成 2.5 的 seedance_2.5")
	assert.NotEqual(t, capcutUpstreamModel25, vp["model"])
	assert.Equal(t, float64(30), vp["duration"])
	assert.Equal(t, "16:9", vp["ratio"])
	assert.Equal(t, "720p", vp["resolution"])
}

// 平台常量必须与 Worker 侧 RemotePlatformId / ArcReel 侧 platform_id 一致。
func TestCapcutPlatformIDConstant(t *testing.T) {
	assert.Equal(t, "capcut", PlatformIDCapcut)
	assert.Equal(t, constant.ChannelTypeManwu, constant.ChannelTypeManwu)
}
