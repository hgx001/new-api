package youzanwan3

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hk-wan3（2026-11-19 接入）的契约照上游文档 2026-11-19 版：
//   - 时长 4–30 整数秒，缺省 5 秒；
//   - 分辨率 480p / 720p / 1080p；
//   - 画幅 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / auto；
//   - 参数形态 resolution + ratio（**不是** size，与 wan-3.0 相反）；
//   - 素材走 media（reference_* 与首尾帧都支持）。
//
// 这组测试钉住四件容易回退的事：
//  1. 时长下界是 4 秒（不是标准版的 10 秒）；
//  2. 480P 不被静默升到 720P；
//  3. 参考视频/音频与首尾帧不被「仅 reference_image」的老闸门拦掉；
//  4. 请求体是 resolution+ratio（小写分辨率），不是 size。

func TestHkWan3DurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantRej bool
	}{
		{name: "未传默认 5 秒", req: relaycommon.TaskSubmitReq{Model: hkWan3Model}, want: 5},
		{name: "显式 4 秒合法（下界）", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 4}, want: 4},
		{name: "显式 10 秒合法", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 10}, want: 10},
		{name: "显式 30 秒合法（上界）", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 30}, want: 30},
		{name: "低于 4 秒 400", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 3}, wantRej: true},
		{name: "高于 30 秒 400", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 31}, wantRej: true},
		{name: "seconds 字符串写法 12 秒", req: relaycommon.TaskSubmitReq{Model: hkWan3Model, Seconds: "12"}, want: 12},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			taskErr := validateExplicitDurationForModel(tc.req)
			if tc.wantRej {
				require.NotNil(t, taskErr, "越界时长必须 400，不能夹紧")
				assert.Equal(t, 400, taskErr.StatusCode)
				return
			}
			require.Nil(t, taskErr)
			assert.Equal(t, tc.want, resolveDurationForModel(tc.req, tc.req.Model))
		})
	}
}

// TestHkWan3KeepsAllThreeResolutions 锁定 480P 不被静默升档：
// 480P 是三档里最便宜的一档，升到 720P 就是「客户要便宜档、我们按贵档收」。
func TestHkWan3KeepsAllThreeResolutions(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"480P", "480P"},
		{"480", "480P"},
		{"720P", "720P"},
		{"1080p", "1080P"},
		{"", "720P"},   // 文档缺省 720p
		{"4K", "720P"}, // 不认的档位 → 缺省 720P
	}
	for _, tc := range cases {
		req := relaycommon.TaskSubmitReq{Model: hkWan3Model, Resolution: tc.raw}
		assert.Equal(t, tc.want, resolveResolutionForModel(req, req.Model), "resolution=%q", tc.raw)
	}
}

// TestHkWan3RatioWhitelist 锁定画幅白名单：认 auto、不认 adaptive / 21:9，
// 非法写法回退到缺省 16:9（不 400 —— 画幅不是计费硬参数）。
func TestHkWan3RatioWhitelist(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"16:9", "16:9"},
		{"9:16", "9:16"},
		{"1:1", "1:1"},
		{"4:3", "4:3"},
		{"3:4", "3:4"},
		{"auto", "auto"},
		{"adaptive", "16:9"}, // 其它模型的写法，hk-wan3 不认 → 缺省
		{"21:9", "16:9"},
		{"", "16:9"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, resolveRatio(relaycommon.TaskSubmitReq{Model: hkWan3Model, Ratio: tc.raw}), "ratio=%q", tc.raw)
	}
}

// TestHkWan3MediaContract hk-wan3 支持参考视频/音频与首尾帧，
// 但不能把「上游必拒」的组合放过去。
func TestHkWan3MediaContract(t *testing.T) {
	image := relaycommon.TaskMedia{Type: "reference_image", URL: "https://example.com/a.jpg"}
	video := relaycommon.TaskMedia{Type: "reference_video", URL: "https://example.com/a.mp4"}
	audio := relaycommon.TaskMedia{Type: "reference_audio", URL: "https://example.com/a.mp3"}
	first := relaycommon.TaskMedia{Type: "first_frame", URL: "https://example.com/f.jpg"}
	last := relaycommon.TaskMedia{Type: "last_frame", URL: "https://example.com/l.jpg"}

	require.NoError(t, validateMedia([]relaycommon.TaskMedia{image, video, audio}, hkWan3Model),
		"hk-wan3 支持参考视频/音频，不能被标准版的老闸门拦掉")

	require.NoError(t, validateMedia([]relaycommon.TaskMedia{first}, hkWan3Model))
	require.NoError(t, validateMedia([]relaycommon.TaskMedia{first, last}, hkWan3Model))

	require.Error(t, validateMedia([]relaycommon.TaskMedia{last}, hkWan3Model),
		"只有尾帧没有首帧：上游必拒，本地要拦")
	require.Error(t, validateMedia([]relaycommon.TaskMedia{first, last, image}, hkWan3Model),
		"首尾帧与普通参考素材互斥")
	require.Error(t, validateMedia([]relaycommon.TaskMedia{first, first}, hkWan3Model),
		"首帧只能一张")

	// 其它模型仍然拒首尾帧 —— 这条不能被 hk-wan3 的例外放开。
	require.Error(t, validateMedia([]relaycommon.TaskMedia{first}, videoModel))
}

// TestHkWan3LegacyArraysMerged 客户照文档传顶层 videos / audios 时不能静默丢弃。
func TestHkWan3LegacyArraysMerged(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Model:   hkWan3Model,
		Prompt:  "参考素材生成",
		Images:  []string{"https://example.com/a.jpg"},
		Videos:  []string{"https://example.com/a.mp4"},
		Audios:  []string{"https://example.com/a.mp3"},
		Seconds: "5",
	}
	media, err := buildMedia(req)
	require.NoError(t, err)
	counts := map[string]int{}
	for _, item := range media {
		counts[item.Type]++
	}
	assert.Equal(t, 1, counts["reference_image"])
	assert.Equal(t, 1, counts["reference_video"])
	assert.Equal(t, 1, counts["reference_audio"])
}

// TestHkWan3RequestBodyShape 锁定 resolution + ratio 契约与小写分辨率。
func TestHkWan3RequestBodyShape(t *testing.T) {
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model:      hkWan3Model,
		Prompt:     "海边木栈道，镜头缓慢推进",
		Seconds:    "5",
		Resolution: "720P",
		Ratio:      "16:9",
		Images:     []string{"https://example.com/a.jpg"},
	})
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &parsed))
	assert.Equal(t, hkWan3Model, parsed["model"], "公开名 == 上游 ID")
	assert.Equal(t, "720p", parsed["resolution"], "分辨率按文档示例下发小写")
	assert.Equal(t, "16:9", parsed["ratio"])
	assert.Equal(t, float64(5), parsed["seconds"], "seconds 下发整数")
	assert.NotContains(t, parsed, "size", "hk-wan3 不接受 size")
	require.NotNil(t, parsed["media"])
}

// TestHkWan3Billing 720P 对外 ¥0.25/s、480P ¥0.18/s、1080P ¥0.48/s：
// ModelPrice 配基准档 480P，其余档用倍率表达。
func TestHkWan3Billing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	billing := func(req relaycommon.TaskSubmitReq) map[string]float64 {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", req)
		return adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: hkWan3Model})
	}

	got := billing(relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 10, Resolution: "480P"})
	assert.InDelta(t, 1.0, got["size"], 1e-9)
	assert.InDelta(t, 10, got["seconds"], 1e-9)

	got = billing(relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 10, Resolution: "720P"})
	assert.InDelta(t, 0.25/0.18, got["size"], 1e-6, "720P 相对 480P 的倍率")

	got = billing(relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 10, Resolution: "1080P"})
	assert.InDelta(t, 0.48/0.18, got["size"], 1e-6, "1080P 相对 480P 的倍率")

	// 参考视频**不额外收费**（2026-11-20 老板口径）：10 秒生成 + 12 秒参考视频
	// 仍按 10 秒计费。参考视频照旧在 validate 阶段探测时长（上游能力约束），
	// 但不并入 seconds —— 这里钉死，防止有人照同渠道其它模型的写法把秒数加回来。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Model: hkWan3Model, Duration: 10, Resolution: "720P"})
	c.Set(refVideoSecondsContextKey, 12)
	got = adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: hkWan3Model})
	assert.InDelta(t, 10, got["seconds"], 1e-9, "参考视频不计费：只按生成秒数收")
}

// TestHkWan3AudioAlwaysOn 成片必须有声：上游 audio 默认 false，不显式下发
// true 就交付无声片（2026-11-19 实测：同参数 1.73MB 纯视频 vs 1.80MB 带 AAC）。
func TestHkWan3AudioAlwaysOn(t *testing.T) {
	build := func(metadata map[string]interface{}) map[string]interface{} {
		body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
			Model:    hkWan3Model,
			Prompt:   "有声成片",
			Seconds:  "4",
			Metadata: metadata,
		})
		require.NoError(t, err)
		var parsed map[string]interface{}
		require.NoError(t, common.Unmarshal(body, &parsed))
		return parsed
	}

	parsed := build(nil)
	assert.Equal(t, true, parsed["audio"], "默认必须下发 audio:true")

	parsed = build(map[string]interface{}{"with_audio": false})
	assert.NotContains(t, parsed, "audio", "显式 with_audio=false 才允许静音")

	// 非布尔值不改变默认：宁可有声，也不要因为一个坏参数交付无声片。
	parsed = build(map[string]interface{}{"with_audio": "false"})
	assert.Equal(t, true, parsed["audio"], "字符串 false 不当作开关")
}

// TestHkWan3AudioNotSentToOtherModels audio 是 hk-wan3 的专属开关，
// 不能漏到同渠道其它模型（它们不认这个字段，可能整单 400）。
func TestHkWan3AudioNotSentToOtherModels(t *testing.T) {
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model:   wan30ProModel,
		Prompt:  "按条模型",
		Seconds: "10",
		Ratio:   "16:9",
	})
	require.NoError(t, err)
	var parsed map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &parsed))
	assert.NotContains(t, parsed, "audio")
}

// TestHkWan3ResolutionRatioTablePinsPrices 直接把「倍率 × 基准价」换算回人民币，
// 防止有人只改 ModelPrice 或只改倍率表造成两档价错位。
func TestHkWan3ResolutionRatioTablePinsPrices(t *testing.T) {
	base := 0.18
	assert.InDelta(t, 0.18, base*hkWan3ResolutionSizeRatio["480P"], 1e-9)
	assert.InDelta(t, 0.25, base*hkWan3ResolutionSizeRatio["720P"], 1e-9)
	assert.InDelta(t, 0.48, base*hkWan3ResolutionSizeRatio["1080P"], 1e-9)
}
