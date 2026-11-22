package middleware

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hk-wan3 → 客美 tj 手动改派（2026-11-22）的测试。
//
// 锁住四类性质：
//  1. 打开时只动 model / duration，其余字段原样；
//  2. 带参考视频、auto/adaptive 画幅、越界时长、素材校验不过、目标无渠道
//     这五种情况一律不改派（保持源链路行为）；
//  3. 计费倍率按 hk-wan3 口径覆盖（客美的 1/2/4 不能拿来收钱）；
//  4. 关闭时请求逐字节不变。

const hkWan3TestModel = "hk-wan3"

func enableHkWan3Tj(t *testing.T, enabled bool) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{"wan3_hk_wan3_tj_enabled": enabled}
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// runHkRoute 用指定渠道可用性跑一遍 HkWan3TjRoute。
func runHkRoute(t *testing.T, body string, available bool) *routeRecorder {
	t.Helper()
	stubChannelAvailable(t, available)
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, HkWan3TjRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, "body=%s", response.Body.String())
	return rec
}

func TestHkWan3TjRouteRewritesModelAndKeepsFields(t *testing.T) {
	enableHkWan3Tj(t, true)

	rec := runHkRoute(t,
		`{"model":"hk-wan3","prompt":"一只猫","resolution":"1080P","duration":10,"ratio":"3:4"}`,
		true)

	rewritten := decodeBody(t, rec.body)
	assert.Equal(t, "wan3.0-video-tj", rewritten["model"])
	assert.Equal(t, "一只猫", rewritten["prompt"])
	assert.Equal(t, "1080P", rewritten["resolution"])
	assert.Equal(t, "3:4", rewritten["ratio"])
	assert.EqualValues(t, 10, rewritten["duration"])
	assert.Equal(t, hkWan3TestModel, rec.originModel, "计费原点必须是客户请求的 hk-wan3")

	require.True(t, rec.hasBillingSize, "改派后必须写源模型口径的档位倍率")
	// hk-wan3：480P 0.18 / 720P 0.25 / 1080P 0.48（元/秒），1080P 倍率 = 0.48/0.18。
	assert.InDelta(t, 0.48/0.18, rec.billingSizeRatio, 1e-9,
		"1080P 必须按 hk-wan3 的 0.48 倍率口径，而不是客美的 4 倍")
}

func TestHkWan3TjRouteBillingRatioByResolution(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantRatio  float64
		wantOrigin string
	}{
		{"480P", `{"model":"hk-wan3","resolution":"480P"}`, 1.0, hkWan3TestModel},
		{"720P", `{"model":"hk-wan3","resolution":"720P"}`, 0.25 / 0.18, hkWan3TestModel},
		{"缺省 720P", `{"model":"hk-wan3"}`, 0.25 / 0.18, hkWan3TestModel},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableHkWan3Tj(t, true)
			rec := runHkRoute(t, tc.body, true)
			assert.Equal(t, tc.wantOrigin, rec.originModel)
			assert.InDelta(t, tc.wantRatio, rec.billingSizeRatio, 1e-9)
		})
	}
}

func TestHkWan3TjRouteWritesExplicitDuration(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"duration", `{"model":"hk-wan3","duration":12}`, 12},
		{"秒数字符串", `{"model":"hk-wan3","seconds":"20"}`, 20},
		{"秒数数字", `{"model":"hk-wan3","seconds":8}`, 8},
		{"缺省 5 秒", `{"model":"hk-wan3"}`, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableHkWan3Tj(t, true)
			rec := runHkRoute(t, tc.body, true)
			rewritten := decodeBody(t, rec.body)
			assert.EqualValues(t, tc.want, rewritten["duration"],
				"客美只认 duration，必须显式写死 hk-wan3 口径的秒数")
		})
	}
}

func TestHkWan3TjRouteSkipsRequestsWithReferenceVideo(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"顶层 videos", `{"model":"hk-wan3","prompt":"猫","videos":["https://cdn.example.com/a.mp4"]}`},
		{"第一条数集合 media", `{"model":"hk-wan3","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`},
		{"metadata media", `{"model":"hk-wan3","metadata":{"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enableHkWan3Tj(t, true)
			rec := runHkRoute(t, tc.body, true)
			assert.Equal(t, tc.body, rec.body, "带参考视频的请求必须原样留在 hk-wan3")
			assert.Empty(t, rec.originModel)
		})
	}
}

func TestHkWan3TjRouteSkipsAutoRatio(t *testing.T) {
	for _, ratio := range []string{"auto", "adaptive", "AUTO"} {
		t.Run(ratio, func(t *testing.T) {
			enableHkWan3Tj(t, true)
			body := `{"model":"hk-wan3","metadata":{"ratio":"` + ratio + `"}}`
			rec := runHkRoute(t, body, true)
			assert.Equal(t, body, rec.body, "客美没有 auto/adaptive 档位，不能静默回落 16:9")
			assert.Empty(t, rec.originModel)
		})
	}
}

func TestHkWan3TjRouteSkipsOutOfRangeDuration(t *testing.T) {
	for _, body := range []string{
		`{"model":"hk-wan3","duration":3}`,
		`{"model":"hk-wan3","duration":31}`,
	} {
		t.Run(body, func(t *testing.T) {
			enableHkWan3Tj(t, true)
			rec := runHkRoute(t, body, true)
			assert.Equal(t, body, rec.body, "hk-wan3 自己会 400 的时长，不该因改派到客美而被放行")
			assert.Empty(t, rec.originModel)
		})
	}
}

func TestHkWan3TjRouteSkipsInvalidMediaForTarget(t *testing.T) {
	// 客美参考图上限 10 张；11 张在 hk-wan3 可能照常出片，改派过去是硬 400。
	images := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		images = append(images, `{"type":"reference_image","url":"https://cdn.example.com/a.jpg"}`)
	}
	body := `{"model":"hk-wan3","media":[` + strings.Join(images, ",") + `]}`

	enableHkWan3Tj(t, true)
	rec := runHkRoute(t, body, true)
	assert.Equal(t, body, rec.body)
	assert.Empty(t, rec.originModel)
}

func TestHkWan3TjRouteSkipsWhenTargetUnavailable(t *testing.T) {
	enableHkWan3Tj(t, true)
	stubChannelAvailableByModel(t, map[string]bool{
		seedanceroute_setting.Wan3TjTargetModel: false,
	})

	rec := &routeRecorder{}
	engine := newRouteEngine(rec, HkWan3TjRoute())
	request := httptest.NewRequest("POST", "/v1/video/generations",
		bytes.NewBufferString(`{"model":"hk-wan3","prompt":"猫"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code)
	assert.Equal(t, `{"model":"hk-wan3","prompt":"猫"}`, rec.body)
	assert.Empty(t, rec.originModel)
}

func TestHkWan3TjRouteSkipsOtherModels(t *testing.T) {
	enableHkWan3Tj(t, true)
	for _, model := range []string{
		"wan3.0-video-官网",
		"wan3.0-video-tj",
		"seedance2.5-特惠",
		"hk-wan3-pro",
	} {
		t.Run(model, func(t *testing.T) {
			body := `{"model":"` + model + `","prompt":"猫"}`
			rec := runHkRoute(t, body, true)
			assert.Equal(t, body, rec.body)
			assert.Empty(t, rec.originModel)
		})
	}
}

func TestHkWan3TjRouteSkipsRemix(t *testing.T) {
	enableHkWan3Tj(t, true)
	stubChannelAvailable(t, true)

	rec := &routeRecorder{}
	engine := newRouteEngine(rec, HkWan3TjRoute())
	request := httptest.NewRequest("POST", "/v1/videos/video_1/remix",
		bytes.NewBufferString(`{"model":"hk-wan3","prompt":"猫"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code)
	assert.Equal(t, `{"model":"hk-wan3","prompt":"猫"}`, rec.body)
	assert.Empty(t, rec.originModel)
}

func TestHkWan3TjRouteSkipsWhenAlreadyRerouted(t *testing.T) {
	enableHkWan3Tj(t, true)
	stubChannelAvailable(t, true)

	rec := &routeRecorder{}
	engine := newRouteEngine(rec,
		func(c *gin.Context) {
			c.Set(routeOriginModelKey, "seedance2.5-特惠")
		},
		HkWan3TjRoute(),
	)
	body := `{"model":"hk-wan3","prompt":"猫"}`
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code)
	assert.Equal(t, body, rec.body, "已由前置规则改派过的请求不能二次改名")
	assert.Equal(t, "seedance2.5-特惠", rec.originModel, "计费原点必须保持前置规则写入的值")
}

func TestHkWan3TjRouteDisabledKeepsRequestByteForByte(t *testing.T) {
	enableHkWan3Tj(t, false)
	body := `{"model":"hk-wan3","prompt":"猫","duration":10}`
	rec := runHkRoute(t, body, true)
	assert.Equal(t, body, rec.body)
	assert.Empty(t, rec.originModel)
	assert.False(t, rec.hasBillingSize)
}
