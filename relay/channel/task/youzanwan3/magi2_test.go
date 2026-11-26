package youzanwan3

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// magi-2（2026-11-25 接入）的契约照上游文档 2026-11-25 版：
//   - 480p / 720p / 1080p **各支持 4–30 整数秒**；
//   - resolution 默认 **1080p**、ratio 默认 16:9；
//   - 画幅 16:9 / 9:16 / 4:3 / 3:4 / 1:1 / 21:9（无 adaptive / auto）；
//   - 素材走顶层 image_urls / video_urls / audio_urls，最多 10 图 / 5 视频 / 5 音频；
//     参考音视频单段与累计都是 2–30 秒；
//   - **按分辨率固定按条计费**（同档时长同价）。
//
// 定价：上游 480p ⚡2.5 / 720p ⚡2.9 / 1080p ⚡2.9（每次）→ 对外 +¥1.0
// = 480p ¥3.5 / 720p ¥3.9 / 1080p ¥3.9。
//
// 这组测试钉死四件最容易回退的事：
//  1. 时长下界是 4 秒、缺省 30 秒（不是标准版的 10 秒 / 10 秒）；
//  2. 计费**只给 size 倍率、绝不给 seconds**（给了就按秒多收 30 倍）；
//  3. 缺省分辨率是 1080P（上游文档），不是最低档 480P；
//  4. 请求体是 duration + 小写 resolution + image_urls/video_urls/audio_urls。

func TestMagi2RegisteredInModelList(t *testing.T) {
	found := false
	for _, m := range ModelList {
		if m == magi2Model {
			found = true
		}
	}
	assert.True(t, found, "magi-2 必须在 ModelList 里（否则「同步上游模型」会把它删掉）")
	upstream, err := upstreamModelFor(magi2Model)
	require.NoError(t, err)
	assert.Equal(t, "magi-2", upstream, "公开名 == 上游 ID")
}

func TestMagi2DurationBounds(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantRej bool
	}{
		{name: "未传默认 30 秒", req: relaycommon.TaskSubmitReq{Model: magi2Model}, want: 30},
		{name: "显式 4 秒合法（下界）", req: relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 4}, want: 4},
		{name: "显式 17 秒合法", req: relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 17}, want: 17},
		{name: "显式 30 秒合法（上界）", req: relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 30}, want: 30},
		{name: "低于 4 秒 400", req: relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 3}, wantRej: true},
		{name: "高于 30 秒 400", req: relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 31}, wantRej: true},
		{name: "seconds 字符串写法 12 秒", req: relaycommon.TaskSubmitReq{Model: magi2Model, Seconds: "12"}, want: 12},
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

// 三档分辨率都在售且逐档定价：480P 是**最便宜**的一档，不能被静默升到 720P
// （那是「客户要便宜档、我们按贵档收」）；缺省必须是 1080P（上游文档默认）。
func TestMagi2Resolutions(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"480P", "480P"},
		{"480", "480P"},
		{"720P", "720P"},
		{"1080p", "1080P"},
		{"", "1080P"},   // 文档缺省 1080p
		{"4K", "1080P"}, // 不认的档位 → 缺省 1080P
	}
	for _, tc := range cases {
		req := relaycommon.TaskSubmitReq{Model: magi2Model, Resolution: tc.raw}
		assert.Equal(t, tc.want, resolveResolutionForModel(req, req.Model), "resolution=%q", tc.raw)
	}
}

func TestMagi2RatioWhitelist(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"16:9", "16:9"},
		{"9:16", "9:16"},
		{"1:1", "1:1"},
		{"4:3", "4:3"},
		{"3:4", "3:4"},
		{"21:9", "21:9"},     // 上游文档明确支持 21:9
		{"adaptive", "16:9"}, // 上游不认 → 落缺省，不 400
		{"auto", "16:9"},
		{"", "16:9"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, resolveRatio(relaycommon.TaskSubmitReq{Model: magi2Model, Ratio: tc.raw}), "ratio=%q", tc.raw)
	}
}

// 计费：按条 + 分辨率档，**只给 size**。带 seconds 会把 ¥3.5 乘成 ¥105。
func TestMagi2BillingIsPerCallByResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	billing := func(req relaycommon.TaskSubmitReq) map[string]float64 {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", req)
		return adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: magi2Model})
	}

	// 基准档是 1080P（¥3.9）：480P 折扣（3.5/3.9），720P/1080P 为 1。
	got := billing(relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 17, Resolution: "480P"})
	require.NotNil(t, got)
	assert.InDelta(t, 3.5/3.9, got["size"], 1e-9, "480P 相对 1080P 的折扣")
	assert.NotContains(t, got, "seconds", "按条计费绝不能带秒数倍率（30 秒会被乘成 ¥105）")

	got = billing(relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 30, Resolution: "720P"})
	require.NotNil(t, got)
	assert.InDelta(t, 1.0, got["size"], 1e-9, "720P 与 1080P 同价")
	assert.NotContains(t, got, "seconds")

	got = billing(relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 30, Resolution: "1080P"})
	require.NotNil(t, got)
	assert.InDelta(t, 1.0, got["size"], 1e-9)
	assert.NotContains(t, got, "seconds")

	got = billing(relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 8})
	require.NotNil(t, got)
	assert.InDelta(t, 1.0, got["size"], 1e-9, "没传分辨率 → 1080P 档（= 基准，倍率 1）")

	// 参考视频秒数不并入售价（按条计价）。
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 10, Resolution: "720P"})
	c.Set(refVideoSecondsContextKey, 25)
	got = adaptor.EstimateBilling(c, &relaycommon.RelayInfo{OriginModelName: magi2Model})
	assert.NotContains(t, got, "seconds", "参考视频不影响按条售价")
}

// 基价落库口径：480P 基准 ¥3.5，内部按 USD 等值（÷7.3）存储。
func TestMagi2DefaultPrice(t *testing.T) {
	price, ok := ratio_setting.GetDefaultModelPriceMap()["magi-2"]
	require.True(t, ok, "defaultModelPrice 必须配 magi-2，否则查不到价会静默免单")
	assert.InDelta(t, 3.9/7.3, price, 1e-12)
}

func TestMagi2MediaContract(t *testing.T) {
	image := relaycommon.TaskMedia{Type: "reference_image", URL: "https://example.com/a.jpg"}
	video := relaycommon.TaskMedia{Type: "reference_video", URL: "https://example.com/a.mp4"}
	audio := relaycommon.TaskMedia{Type: "reference_audio", URL: "https://example.com/a.mp3"}

	require.NoError(t, validateMedia([]relaycommon.TaskMedia{image, video, audio}, magi2Model),
		"magi-2 支持参考视频/音频，不能被标准版的老闸门拦掉")

	// 数量上限：10 图 / 5 视频 / 5 音频。
	images := make([]relaycommon.TaskMedia, 0, maxReferenceImages+1)
	for i := 0; i <= maxReferenceImages; i++ {
		images = append(images, image)
	}
	require.Error(t, validateMedia(images, magi2Model), "超过 10 张图必须本地 400")

	videos := make([]relaycommon.TaskMedia, 0, maxReferenceVideos+1)
	for i := 0; i <= maxReferenceVideos; i++ {
		videos = append(videos, video)
	}
	require.Error(t, validateMedia(videos, magi2Model), "超过 5 段视频必须本地 400")

	// 顶层数组（客户照上游文档传 image_urls/video_urls/audio_urls）必须被并入。
	media, err := buildMedia(relaycommon.TaskSubmitReq{
		Model:  magi2Model,
		Prompt: "多模态参考",
		Images: []string{"https://example.com/a.jpg"},
		Videos: []string{"https://example.com/a.mp4"},
		Audios: []string{"https://example.com/a.mp3"},
	})
	require.NoError(t, err)
	counts := map[string]int{}
	for _, item := range media {
		counts[item.Type]++
	}
	assert.Equal(t, 1, counts["reference_image"])
	assert.Equal(t, 1, counts["reference_video"])
	assert.Equal(t, 1, counts["reference_audio"])
}

// 有参考视频时，magi-2 **不受**标准版那条「生成时长 ≤15 秒」的约束（上游允许 4–30）。
func TestMagi2GenerationWithReferenceVideoNotCappedAt15(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", relaycommon.TaskSubmitReq{Model: magi2Model, Duration: 30})
	video := relaycommon.TaskMedia{Type: "reference_video", URL: "https://example.com/a.mp4"}
	require.Nil(t, validateGenerationSecondsWithReferenceVideo(c, []relaycommon.TaskMedia{video}, 30),
		"magi-2 允许 30 秒生成 + 参考视频", "")

	// 反向：标准版仍然被 15 秒闸门拦（这条规则不能被 magi-2 的例外放开）。
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Set("task_request", relaycommon.TaskSubmitReq{Model: videoModel, Duration: 30})
	require.NotNil(t, validateGenerationSecondsWithReferenceVideo(c2, []relaycommon.TaskMedia{video}, 30))
}

// 请求体：duration（整数）+ 小写 resolution + ratio + image_urls/video_urls/audio_urls。
func TestMagi2RequestBodyShape(t *testing.T) {
	body, err := (&TaskAdaptor{}).buildRequestBody(relaycommon.TaskSubmitReq{
		Model:      magi2Model,
		Prompt:     "清晨的海边，镜头沿木栈道平稳前移",
		Duration:   17,
		Resolution: "720P",
		Ratio:      "21:9",
		Images:     []string{"https://example.com/a.jpg"},
		Videos:     []string{"https://example.com/a.mp4"},
		Audios:     []string{"https://example.com/a.mp3"},
	})
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, common.Unmarshal(body, &parsed))
	assert.Equal(t, "magi-2", parsed["model"], "公开名 == 上游 ID")
	assert.Equal(t, float64(17), parsed["duration"], "时长字段是 duration（整数），不是 seconds")
	assert.NotContains(t, parsed, "seconds")
	assert.Equal(t, "720p", parsed["resolution"], "分辨率按文档下发小写")
	assert.Equal(t, "21:9", parsed["ratio"])
	assert.NotContains(t, parsed, "media", "magi-2 不收 media 数组")
	assert.NotContains(t, parsed, "size")

	assert.Equal(t, []interface{}{"https://example.com/a.jpg"}, parsed["image_urls"])
	assert.Equal(t, []interface{}{"https://example.com/a.mp4"}, parsed["video_urls"])
	assert.Equal(t, []interface{}{"https://example.com/a.mp3"}, parsed["audio_urls"])
}
