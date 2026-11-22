// TikTok Symphony（ads.tiktok.com）路由与参数契约（2026-11-15 接入）。
//
// 口径（Worker 真机 2026-11-15 实测，详见 cron_video_brach docs/agent-guides/tiktok.md）：
//   - 公开名 tk-symphony-1.5pro → 上游模型名 tiktok-symphony
//     （ArcReel 侧 TikTok 按**平台**记账，billing_model_id 固定 "tiktok-symphony"）；
//   - 平台 platform_id = tiktok（一级建单，不走豆包路由；与 capcut 是两个平台）；
//   - 时长**只三档** 5 / 10 / 12 秒（官网原生下拉），缺省 5；
//     白名单外显式 400，**不回落**（回落 = 把买的长片做成短片）；
//   - 比例**只有 9:16**（官网无比例控件，成片固定 9:16）；
//   - 分辨率不可选（实测成片 720x1280 / 360x640）；
//   - 参考图 ≤4 张（官网面板 "Upload up to 4 images"），超出显式 400；
//   - 不收参考视频 / 参考音频。
package manwu

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 公开名必须路由到 tiktok 平台，且上游模型名是 ArcReel 侧认识的 tiktok-symphony。
func TestTikTokModelSpecsRouteToTiktokPlatform(t *testing.T) {
	spec, ok := modelSpecs[ModelTikTokSymphony]
	require.True(t, ok, "公开名 %s 必须登记", ModelTikTokSymphony)
	assert.Equal(t, PlatformIDTiktok, spec.PlatformID)
	assert.Equal(t, OutputModeVideo, spec.OutputMode)
	assert.Equal(t, kindTikTokVideo, spec.Kind)
	assert.Equal(t, tiktokUpstreamModel, spec.UpstreamModel)
}

// 公开名**不能**用 cp- 前缀（那是 CapCut/剪映国际版的身份），也不能与豆包撞名。
func TestTikTokPublicNameDoesNotCollideWithCapcut(t *testing.T) {
	assert.False(t, strings.HasPrefix(ModelTikTokSymphony, "cp-"),
		"TikTok 公开名不得复用 cp- 前缀（CapCut 专用）")
	for _, capcutModel := range []string{
		ModelCapcut25, ModelCapcut25Flash, ModelCapcut20, ModelCapcut20Fast, ModelCapcut20Mini,
	} {
		assert.NotEqual(t, capcutModel, ModelTikTokSymphony)
	}
	// 与 CapCut 是两个平台，归一表不能串台
	assert.NotEqual(t, PlatformIDCapcut, PlatformIDTiktok)
}

// 时长三档白名单：5 / 10 / 12 通过，其余显式拒绝（不回落 5 秒）。
func TestTikTokDurationWhitelist(t *testing.T) {
	for _, ok := range []int{5, 10, 12} {
		c, info, a := postVideoCtx(t,
			fmt.Sprintf(`{"model":"%s","prompt":"hi","seconds":%d}`, ModelTikTokSymphony, ok))
		require.Nil(t, a.ValidateRequestAndSetAction(c, info), "seconds=%d 应通过", ok)
	}
	for _, bad := range []int{1, 8, 15, 30} {
		c, info, a := postVideoCtx(t,
			fmt.Sprintf(`{"model":"%s","prompt":"hi","seconds":%d}`, ModelTikTokSymphony, bad))
		require.NotNil(t, a.ValidateRequestAndSetAction(c, info), "seconds=%d 应显式拒绝", bad)
	}
}

// 时长白名单**逐模型不同**（2026-11-22 老板口径）：
//   - 1.5 Pro / Symphony 档：官网原生 5 / 10 / 12 秒；
//   - 2.0 系列（Fast / 标准）：4–15 秒逐秒。
// 合成一张表会两头都错：2.0 档被挡在 12 秒上限外，或 1.5 Pro 档被放进官网没有的秒数。
func TestTikTokDurationWhitelistPerModel(t *testing.T) {
	for _, model := range []string{ModelTiktok20Fast, ModelTiktok20} {
		for _, ok := range []int{4, 5, 9, 13, 15} {
			c, info, a := postVideoCtx(t,
				fmt.Sprintf(`{"model":"%s","prompt":"hi","seconds":%d}`, model, ok))
			require.Nil(t, a.ValidateRequestAndSetAction(c, info), "%s seconds=%d 应通过", model, ok)
		}
		for _, bad := range []int{1, 3, 16, 30} {
			c, info, a := postVideoCtx(t,
				fmt.Sprintf(`{"model":"%s","prompt":"hi","seconds":%d}`, model, bad))
			require.NotNil(t, a.ValidateRequestAndSetAction(c, info), "%s seconds=%d 应显式拒绝", model, bad)
		}
	}
	// 1.5 Pro 档仍然只有三档：2.0 的 4/13/15 在它这里必须拒绝
	for _, bad := range []int{4, 13, 15} {
		c, info, a := postVideoCtx(t,
			fmt.Sprintf(`{"model":"%s","prompt":"hi","seconds":%d}`, ModelTikTokSymphony, bad))
		require.NotNil(t, a.ValidateRequestAndSetAction(c, info), "symphony seconds=%d 应显式拒绝", bad)
	}
}

// 2.0 档的缺省时长仍是 5 秒。
func TestTikTok20DefaultDurationIs5(t *testing.T) {
	c, info, a := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi"}`, ModelTiktok20Fast))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 5, req.Duration)
}

// 缺省时长 = 5 秒（官网默认值）。
func TestTikTokDefaultDurationIs5(t *testing.T) {
	c, info, a := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi"}`, ModelTikTokSymphony))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, 5, req.Duration)
}

// 比例只有 9:16：其它比例显式拒绝（官网无比例控件，静默当成 9:16 = 骗用户）。
func TestTikTokRatioMustBe916(t *testing.T) {
	c, info, a := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi","ratio":"9:16"}`, ModelTikTokSymphony))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	req, err := getNormalizedRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "9:16", req.Ratio)

	c2, info2, a2 := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi","ratio":"16:9"}`, ModelTikTokSymphony))
	require.NotNil(t, a2.ValidateRequestAndSetAction(c2, info2), "16:9 必须显式拒绝")
}

// 分辨率不可选：只有 720p 能过（实测输出），其它档位显式拒绝。
func TestTikTokResolutionIsNotSelectable(t *testing.T) {
	c, info, a := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi","resolution":"720p"}`, ModelTikTokSymphony))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info))

	c2, info2, a2 := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi","resolution":"1080p"}`, ModelTikTokSymphony))
	require.NotNil(t, a2.ValidateRequestAndSetAction(c2, info2), "1080p 必须显式拒绝")
}

// 参考图 ≤4 张：4 张通过，5 张显式拒绝（官网会静默丢弃 → 用户以为素材都参与了）。
func TestTikTokReferenceImagesLimitIs9(t *testing.T) {
	build := func(n int) string {
		items := make([]string, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, fmt.Sprintf(`"https://a/%d.jpg"`, i))
		}
		return fmt.Sprintf(
			`{"model":"%s","prompt":"hi","input_reference":[%s]}`,
			ModelTikTokSymphony, strings.Join(items, ","))
	}
	// 9 张必须通过（官网 Video 档上限，2026-11-22 真机取证）
	c, info, a := postVideoCtx(t, build(9))
	require.Nil(t, a.ValidateRequestAndSetAction(c, info), "9 张参考图应通过")

	// 10 张必须显式拒绝（官网会静默丢弃超出部分 → 用户以为全参与了）
	c2, info2, a2 := postVideoCtx(t, build(10))
	require.NotNil(t, a2.ValidateRequestAndSetAction(c2, info2), "10 张参考图必须显式拒绝")
}

// 参考视频 / 音频：官网面板只有图位，显式拒绝。
func TestTikTokRejectsReferenceVideoAndAudio(t *testing.T) {
	c, info, a := postVideoCtx(t,
		fmt.Sprintf(`{"model":"%s","prompt":"hi","input_reference":["https://a/1.mp4"]}`, ModelTikTokSymphony))
	require.NotNil(t, a.ValidateRequestAndSetAction(c, info))
}

// 对外模型清单里必须能看到 TikTok 模型（否则广场/下拉里根本没有这个商品）。
func TestModelListContainsTikTokModel(t *testing.T) {
	names := ModelList()
	assert.Contains(t, names, ModelTikTokSymphony)
}
