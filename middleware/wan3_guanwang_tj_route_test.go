package middleware

// 万相官网 → 客美 tj 的手动改派（2026-11-19）。
//
// 本文件锁七件事：
//  1. 开关关着时**一个字都不改**（升级当天线上流量逐字节不变）；
//  2. 开关打开且目标有渠道时：模型名换成 tj、计费原点仍是官网；
//  3. 档位倍率按**官网口径**覆盖（1/2.25/4：720P ¥0.45/秒），解析不出档位时
//     会掉回 480P 的 1.0 倍 —— 2026-11-23 事故，size:"720P" 写法必须能识别；
//  4. 时长按官网口径钳制并显式写进 duration（官网钳制、客美 400；官网认
//     顶层 seconds、客美不认）—— 否则「买 10 秒收 5 秒」或直接报错；
//  5. 目标没渠道 / 画幅 adaptive / 别的模型 / /remix → 一律放行原请求；
//  6. 与促销族总开关 Enabled 无关（连坐检查）；
//  7. 素材字段原样保留（客美收 images/videos/audios，官网会丢弃它们，
//     改派后素材才真的生效 —— 这是本开关的价值之一，不能被顺手剥掉）。

import (
	"bytes"
	"math"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setWan3GuanwangTj 把开关设成 enabled，并在用例结束后恢复原策略。
func setWan3GuanwangTj(t *testing.T, enabled bool, extra map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{}
	for key, value := range extra {
		payload[key] = value
	}
	payload["wan3_guanwang_tj_enabled"] = enabled
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// runWan3GuanwangTj 按真实顺序只跑本中间件（渠道预检另行 stub）。
func runWan3GuanwangTj(t *testing.T, path, contentType, body string) *routeRecorder {
	t.Helper()
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3GuanwangTjRoute())
	request := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return rec
}

const wan3GuanwangBody = `{"model":"wan3.0-video-官网","prompt":"a cat","resolution":"480P","ratio":"16:9","duration":2}`

func TestWan3GuanwangTjOffKeepsRequest(t *testing.T) {
	setWan3GuanwangTj(t, false, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body, "开关关着时请求体必须逐字节不变")
	assert.Empty(t, rec.originModel, "开关关着时不该记计费原点")
}

func TestWan3GuanwangTjRewritesToTarget(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.Wan3TjTargetModel, decoded["model"])
	assert.Equal(t, "a cat", decoded["prompt"], "提示词不能被动")
	assert.Equal(t, "480P", decoded["resolution"], "分辨率档原样保留")
	assert.Equal(t, "16:9", decoded["ratio"], "画幅原样保留")
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel,
		"计费原点必须是客户请求的官网模型")
}

// 1080P 的档位倍率必须用官网口径（¥0.80/¥0.20 = 4.0），不是 dashscope 的 3.148。
func TestWan3GuanwangTjUsesSourceSizeRatio(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","resolution":"1080P","duration":2}`)

	require.True(t, rec.hasBillingSize, "必须写源模型口径的档位倍率")
	assert.InDelta(t, 4.0, rec.billingSizeRatio, 1e-6,
		"1080P 必须按官网的 ¥0.80/¥0.20 = 4 倍收")
	assert.False(t, math.Abs(0.85/0.27-rec.billingSizeRatio) < 1e-6, "dashscope 的 3.148 是错的")
}

// metadata.resolution 也要能识别（客户端两种写法都有）。
func TestWan3GuanwangTjReadsMetadataResolution(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","metadata":{"resolution":"720P"},"duration":2}`)

	require.True(t, rec.hasBillingSize)
	assert.InDelta(t, 2.25, rec.billingSizeRatio, 1e-6, "720P 必须按官网的 ¥0.45/¥0.20 = 2.25 倍")
}

// 客户端常把档位直接写在 size（"720P" / "1280x720"），不是 resolution。
// 2026-11-23 事故回归：这类请求此前解析不出档位，720P 被按 480P 的 1.0 倍计费。
func TestWan3GuanwangTjReadsSizeTier(t *testing.T) {
	cases := []string{"720P", "1280x720"}
	for _, size := range cases {
		t.Run(size, func(t *testing.T) {
			setWan3GuanwangTj(t, true, nil)
			stubChannelAvailable(t, true)

			rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
				`{"model":"wan3.0-video-官网","prompt":"a cat","size":"`+size+`","seconds":"6","duration":6}`)

			require.True(t, rec.hasBillingSize)
			assert.InDelta(t, 2.25, rec.billingSizeRatio, 1e-6,
				"size=%s 必须识别成 720P，按 2.25 倍计费", size)
			assert.Equal(t, float64(6), decodeBody(t, rec.body)["duration"], "时长不能被改")
		})
	}
}

// 官网钳制越界时长（1 → 2 秒），改派必须把钳制结果显式写进 duration：
// 客美对 <2 秒是直接 400，不会替我们钳制。
func TestWan3GuanwangTjClampsDuration(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","duration":1}`)

	assert.Equal(t, float64(2), decodeBody(t, rec.body)["duration"], "1 秒要按官网口径钳到 2 秒")
}

// 官网认顶层 seconds（字符串），客美的 resolveDuration 不读它 —— 必须转成 duration。
func TestWan3GuanwangTjConvertsSecondsToDuration(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","seconds":"10"}`)

	assert.Equal(t, float64(10), decodeBody(t, rec.body)["duration"],
		"seconds=10 必须落成 duration=10，否则客美按缺省 5 秒出片")
}

// 不传时长时与官网缺省一致（5 秒）。
func TestWan3GuanwangTjDefaultsDuration(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat"}`)

	assert.Equal(t, float64(5), decodeBody(t, rec.body)["duration"], "缺省时长必须是 5 秒")
}

// 目标没渠道 → 放行原请求（客户完全无感，照常走官网）。
func TestWan3GuanwangTjSkipsWhenTargetUnavailable(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, false)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body, "目标没渠道时必须原样放行")
	assert.Empty(t, rec.originModel)
}

// adaptive 画幅客美没有对应档（会回落 16:9）→ 不替客户钉死画幅。
func TestWan3GuanwangTjSkipsAdaptiveRatio(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","ratio":"adaptive","duration":2}`
	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "adaptive 画幅必须留在官网链路")
	assert.Empty(t, rec.originModel)
}

// 其它模型（含同渠道的 720P 档）不能被顺手改派。
func TestWan3GuanwangTjLeavesOtherModelsUntouched(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	for _, model := range []string{"wan3.0-720p-官网", "wan3.0-720p-特惠", "wan3.0-video-tj", "wan3.0-video"} {
		body := `{"model":"` + model + `","prompt":"a cat","duration":2}`
		rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", body)
		assert.Equal(t, body, rec.body, model+" 不该被本开关改派")
		assert.Empty(t, rec.originModel, model+" 不该记计费原点")
	}
}

// /remix 的模型由原任务决定，改名会破坏语义。
func TestWan3GuanwangTjSkipsRemix(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/videos/video_123/remix", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body, "remix 不该被改派")
}

// 非 JSON 请求不解析（multipart 直传走另一条路径）。
func TestWan3GuanwangTjSkipsNonJSON(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "text/plain", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body)
}

// 促销族规则总开关关掉时，本开关照样生效（不连坐）。
func TestWan3GuanwangTjWorksWithRulesDisabled(t *testing.T) {
	setWan3GuanwangTj(t, true, map[string]interface{}{"enabled": false})
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, seedanceroute_setting.Wan3TjTargetModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel)
}

// 参考图必须原样带到客美（官网 adaptor 会丢弃它，客美会真的用上）。
func TestWan3GuanwangTjKeepsReferenceImages(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":2,` +
		`"images":["https://example.com/a.png"]}`
	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", body)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.Wan3TjTargetModel, decoded["model"])
	assert.Equal(t, []interface{}{"https://example.com/a.png"}, decoded["images"])
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel)
}

// 带参考视频的请求**不改派**到客美：客美会把参考视频秒数并入计费，
// 而官网只按生成秒数收 —— 改派等于给同一个模型名静默涨价。
// 这类请求归 wan3_ref_video_hk_route（hk-wan3，参考视频不计费）。
func TestWan3GuanwangTjSkipsReferenceVideo(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "带参考视频必须留在官网链路")
	assert.Empty(t, rec.originModel)
}

// 素材过不了客美校验（这里用 11 条 reference_video 之外的显式 media 参考图，
// 超过 10 张上限）→ 不改派：官网会静默丢素材照常出片，改派过去就是硬 400。
func TestWan3GuanwangTjSkipsMediaRejectedByTarget(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	entries := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		entries = append(entries, `{"type":"reference_image","url":"https://example.com/`+
			string(rune('a'+i))+`.png"}`)
	}
	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":2,"media":[` +
		strings.Join(entries, ",") + `]}`
	rec := runWan3GuanwangTj(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "超过客美图片上限时必须留在官网链路")
	assert.Empty(t, rec.originModel)
}

// 引擎里同时挂别名与促销规则时，本中间件仍然只处理官网模型（顺序无关）。
func TestWan3GuanwangTjCoexistsWithOtherRoutes(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3TehuiAliasRoute(), Seedance25TeHuiRoute(), Wan3GuanwangTjRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations",
		bytes.NewBufferString(`{"model":"wan3.0-720p-特惠","prompt":"a cat"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	assert.NotEqual(t, seedanceroute_setting.Wan3TjTargetModel, decodeBody(t, rec.body)["model"],
		"特惠别名路径不能被万相换货开关接管")
}

var _ = gin.SetMode

// 已被前置规则改派到官网的请求（规则三「首尾帧」的 frames_target_model 就是
// wan3.0-video-官网）**不二次改派**：否则计费原点会被覆盖成官网，
// 而客户真正买的是 seedance2.5-特惠。
func TestWan3GuanwangTjSkipsAlreadyRerouted(t *testing.T) {
	setWan3GuanwangTj(t, true, nil)
	stubChannelAvailable(t, true)

	// 模拟「前面的规则已把请求改派到官网并写下计费原点」。
	preRoute := func(c *gin.Context) {
		common.SetContextKey(c, constant.ContextKeyRouteOriginModel, "seedance2.5-特惠")
	}

	rec := &routeRecorder{}
	engine := newRouteEngine(rec, preRoute, Wan3GuanwangTjRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(wan3GuanwangBody))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code)

	assert.Equal(t, wan3GuanwangBody, rec.body, "已改派过的请求必须原样放行")
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点不能被覆盖成官网")
}
