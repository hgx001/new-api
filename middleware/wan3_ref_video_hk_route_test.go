package middleware

// 带参考视频的万相请求 → hk-wan3（2026-11-20）。
//
// 本文件锁八件事：
//  1. 开关**默认开**（nil = 开）：不配置也生效；显式 false 时一个字都不改；
//  2. 带 reference_video 才改派（media 数组 / 顶层 videos / 顶层 video 三种写法都认）；
//  3. 不带参考视频的请求原样放行（这条路由不是「所有万相都换 hk-wan3」）；
//  4. 时长 4–15 秒才改派：hk-wan3 下界 4 秒、带参考视频生成上限 15 秒；
//  5. 显式写 duration（hk-wan3 只认 duration，不读顶层 seconds）；
//  6. 档位倍率按**源模型**口径覆盖（官网 1080P 3.148 倍 / tj 1080P 4 倍，
//     hk-wan3 自己是 2.667 倍）；
//  7. 准入不满足就留在原链路：adaptive 画幅 / 首尾帧 / 素材超限 / 目标无渠道 /
//     非万相模型 / /remix / 已被前置规则改派；
//  8. 计费原点仍是客户请求的源模型。

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setWan3RefVideoHk 设置开关（nil = 不写这个字段，用来验「缺省开启」）。
func setWan3RefVideoHk(t *testing.T, enabled *bool) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{}
	if enabled != nil {
		payload["wan3_ref_video_hk_enabled"] = *enabled
	}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

func boolPtr(v bool) *bool { return &v }

func runRefVideoHk(t *testing.T, path, contentType, body string) *routeRecorder {
	t.Helper()
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3RefVideoHkRoute())
	request := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return rec
}

const refVideoBody = `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"resolution":"480P",` +
	`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`

// 缺省（没配这个字段）= 开启：必须改派。
func TestWan3RefVideoHkDefaultsOnRoutes(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", refVideoBody)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.Wan3RefVideoHkTargetModel, decoded["model"])
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel,
		"计费原点必须是客户请求的官网模型")
	assert.Equal(t, float64(5), decoded["duration"], "duration 必须显式写入")
}

// 显式关闭时一个字都不改。
func TestWan3RefVideoHkOffKeepsRequest(t *testing.T) {
	setWan3RefVideoHk(t, boolPtr(false))
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", refVideoBody)

	assert.Equal(t, refVideoBody, rec.body)
	assert.Empty(t, rec.originModel)
}

// 不带参考视频的请求原样放行。
func TestWan3RefVideoHkIgnoresRequestsWithoutRefVideo(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"media":[{"type":"reference_image","url":"https://example.com/a.png"}]}`

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "只有参考图、没有参考视频时不该改派")
	assert.Empty(t, rec.originModel)
}

// 顶层 videos 数组也算带参考视频（部分客户端不用 media 形态）。
func TestWan3RefVideoHkRecognisesTopLevelVideos(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-tj","prompt":"a cat","duration":6,"videos":["https://example.com/a.mp4"]}`)

	assert.Equal(t, seedanceroute_setting.Wan3RefVideoHkTargetModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, seedanceroute_setting.Wan3TjModel, rec.originModel)
}

// 生成时长 < 4 秒 → hk-wan3 必 400，必须留在原链路。
func TestWan3RefVideoHkSkipsShortDuration(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":2,` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "2 秒请求不能改派到 4 秒起步的 hk-wan3")
	assert.Empty(t, rec.originModel)
}

// 生成时长 > 15 秒（带参考视频的上限）→ 留在原链路。
func TestWan3RefVideoHkSkipsLongDuration(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":20,` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body)
	assert.Empty(t, rec.originModel)
}

// 官网口径下 1 秒会被钳到 2 秒 —— 仍然 <4，不路由。
func TestWan3RefVideoHkSkipsClampedShortDuration(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":1,` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body)
}

// 顶层 seconds 写法：官网口径认它，改派后必须变成显式 duration。
func TestWan3RefVideoHkConvertsSecondsToDuration(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","seconds":"10",`+
			`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`)

	assert.Equal(t, float64(10), decodeBody(t, rec.body)["duration"])
}

// 1080P：按官网的 3.148 倍覆盖，不能按 hk-wan3 自己的 2.667 倍。
func TestWan3RefVideoHkUsesGuanwangSizeRatio(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"resolution":"1080P",`+
			`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`)

	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 0.85/0.27, rec.billingSizeRatio, 1e-6)
}

// tj 1080P 口径是 4 倍（与官网的 3.148 不同），也要覆盖。
func TestWan3RefVideoHkUsesTjSizeRatio(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-tj","prompt":"a cat","duration":5,"resolution":"1080P",`+
			`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`)

	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 4.0, rec.billingSizeRatio, 1e-6)
}

// 画幅 adaptive 在 hk-wan3 上会回落 16:9 → 不改派。
func TestWan3RefVideoHkSkipsAdaptiveRatio(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"ratio":"adaptive",` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body)
}

// hk-wan3 的首尾帧与参考素材互斥 → 带首尾帧就不改派。
func TestWan3RefVideoHkSkipsFrameMedia(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"media":[` +
		`{"type":"first_frame","url":"https://example.com/f.png"},` +
		`{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body)
}

// 素材数量超 hk-wan3 上限 → 不改派。
func TestWan3RefVideoHkSkipsTooManyVideos(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"videos":[` +
		`"https://example.com/1.mp4","https://example.com/2.mp4","https://example.com/3.mp4",` +
		`"https://example.com/4.mp4","https://example.com/5.mp4","https://example.com/6.mp4"]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "6 段参考视频超过 hk-wan3 的 5 段上限")
}

// 目标没可用渠道 → 放行原请求。
func TestWan3RefVideoHkSkipsWhenTargetUnavailable(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, false)

	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", refVideoBody)

	assert.Equal(t, refVideoBody, rec.body)
	assert.Empty(t, rec.originModel)
}

// 非万相族的模型一律不碰（含 720P 档）。
func TestWan3RefVideoHkLeavesOtherModelsUntouched(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	for _, model := range []string{"wan3.0-720p-官网", "wan3.0-720p-特惠", "wan3.0-video", "seedance-2-5"} {
		body := `{"model":"` + model + `","prompt":"a cat","duration":5,` +
			`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
		rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)
		assert.Equal(t, body, rec.body, model+" 不该被这条路由改派")
	}
}

// /remix 不改派。
func TestWan3RefVideoHkSkipsRemix(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	rec := runRefVideoHk(t, "/v1/videos/video_123/remix", "application/json", refVideoBody)

	assert.Equal(t, refVideoBody, rec.body)
}

// 已被前置规则改派过的请求不二次改派。
func TestWan3RefVideoHkSkipsAlreadyRerouted(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	preRoute := func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyRouteOriginModel, "seedance2.5-特惠")
	}
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, preRoute, Wan3RefVideoHkRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(refVideoBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	assert.Equal(t, refVideoBody, rec.body)
	assert.Equal(t, "seedance2.5-特惠", rec.originModel)
}

// 与「官网→客美 tj」串起来时：带参考视频的请求先被本中间件接走，不再被换货卡改。
func TestWan3RefVideoHkWinsOverGuanwangTj(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	previous := seedanceroute_setting.Get()
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(`{"wan3_guanwang_tj_enabled":true}`))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	stubChannelAvailable(t, true)

	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3RefVideoHkRoute(), Wan3GuanwangTjRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(refVideoBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	assert.Equal(t, seedanceroute_setting.Wan3RefVideoHkTargetModel, decodeBody(t, rec.body)["model"],
		"带参考视频必须落 hk-wan3，而不是被换货卡改成 tj")
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel)
}

// data URI 参考视频：官网根本不读素材（请求照常出片），而 hk-wan3 会本地 400 —
// 不拦就是「以前能跑 → 现在报错」的回归。
func TestWan3RefVideoHkSkipsDataURIRefVideo(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"media":[` +
		`{"type":"reference_video","url":"data:video/mp4;base64,AAAA"}]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "data URI 参考视频必须留在原链路")
	assert.Empty(t, rec.originModel)
}

// 混着一条非 http(s)：整体不路由（hk-wan3 会为那一条 400）。
func TestWan3RefVideoHkSkipsMixedSchemes(t *testing.T) {
	setWan3RefVideoHk(t, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-tj","prompt":"a cat","duration":5,"videos":[` +
		`"https://example.com/a.mp4","/local/b.mp4"]}`
	rec := runRefVideoHk(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body)
	assert.Empty(t, rec.originModel)
}
