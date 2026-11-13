package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/xinbiapi"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// wan3.0-720p-特惠 的分流：能力允许时走心笔（480P 生成 + 交付 720P），
// 带首尾帧 / 参考视频 / 参考音频 / seed 等心笔不支持的请求留在原 Wan3 链路。
//
// 这些用例锁的是**分流边界**，不是改写细节：
//   - 能分流却没分流 → 白付官网 480P 的钱（成本优化失效）；
//   - 不能分流却分流 → 上游拒单或素材被静默丢掉（比多花钱更糟）。

const tehuiModel = "wan3.0-720p-特惠"

// newTehuiXinbiEngine 只挂分流中间件（不带规则一~五）。
func newTehuiXinbiEngine(rec *routeRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Wan3TehuiXinbiRoute())
	engine.POST("/v1/video/generations", recordRouteBody(rec))
	return engine
}

// newTehuiXinbiEngineWithOrigin 预置一个「上游规则已经改派过」的源模型名。
//
// 规则二/三会先把 seedance2.5-特惠 改写成 特惠；分流不能再把计费原点
// 覆写成 特惠 —— 否则客户买的是 ¥0.30/秒 的促销档，账单却按 ¥0.20/秒 走。
func newTehuiXinbiEngineWithOrigin(rec *routeRecorder, origin string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set(routeOriginModelKey, origin)
		c.Next()
	})
	engine.Use(Wan3TehuiXinbiRoute())
	engine.POST("/v1/video/generations", recordRouteBody(rec))
	return engine
}

// recordRouteBody 记录下游看到的请求体与计费原点。
func recordRouteBody(rec *routeRecorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.String(500, "read body storage: %v", err)
			return
		}
		raw, err := storage.Bytes()
		if err != nil {
			c.String(500, "read body bytes: %v", err)
			return
		}
		rec.body = string(raw)
		rec.originModel = RouteOriginModel(c)
		c.String(200, "ok")
	}
}

func runTehuiXinbi(t *testing.T, body string) *routeRecorder {
	t.Helper()
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newTehuiXinbiEngine(rec)
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return rec
}

// 最基础的一条：纯文本 + 10 秒 → 必须分流。
func TestWan3TehuiXinbiRoute_RoutesPlainRequest(t *testing.T) {
	rec := runTehuiXinbi(t, `{"model":"`+tehuiModel+`","prompt":"a cat","seconds":"10"}`)

	body := decodeBody(t, rec.body)
	assert.Equal(t, xinbiapi.ModelNameTehuiRoute, body["model"], "应改写到内部目标模型")
	assert.Equal(t, "10", body["seconds"], "计费/生成秒数必须原样带过去")
	assert.Equal(t, tehuiModel, rec.originModel, "对客模型与计费原点必须仍是特惠")
}

// 时长：2–30 透传，缺省 5，越界按 Wan3 同口径夹取。
func TestWan3TehuiXinbiRoute_DurationPolicy(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "5 秒", body: `{"model":"` + tehuiModel + `","prompt":"x","seconds":"5"}`, want: "5"},
		{name: "10 秒", body: `{"model":"` + tehuiModel + `","prompt":"x","seconds":"10"}`, want: "10"},
		{name: "30 秒", body: `{"model":"` + tehuiModel + `","prompt":"x","seconds":"30"}`, want: "30"},
		{name: "不传默认 5 秒", body: `{"model":"` + tehuiModel + `","prompt":"x"}`, want: "5"},
		{name: "1 秒按 Wan3 口径夹到 2", body: `{"model":"` + tehuiModel + `","prompt":"x","seconds":"1"}`, want: "2"},
		{name: "31 秒按 Wan3 口径夹到 30", body: `{"model":"` + tehuiModel + `","prompt":"x","seconds":"31"}`, want: "30"},
		{name: "duration 数字", body: `{"model":"` + tehuiModel + `","prompt":"x","duration":7}`, want: "7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := runTehuiXinbi(t, tc.body)
			body := decodeBody(t, rec.body)
			assert.Equal(t, xinbiapi.ModelNameTehuiRoute, body["model"])
			assert.Equal(t, tc.want, body["seconds"])
			_, hasDuration := body["duration"]
			assert.False(t, hasDuration, "改写后只保留 seconds，避免两个时长字段打架")
		})
	}
}

// 心笔吃不住的请求必须留在原链路 —— 分流过去只会 400 或丢素材。
func TestWan3TehuiXinbiRoute_KeepsUnsupportedRequestsOnWan3(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "首帧",
			body: `{"model":"` + tehuiModel + `","prompt":"x","media":[{"type":"first_frame","url":"https://a.example/1.png"}]}`,
		},
		{
			name: "尾帧",
			body: `{"model":"` + tehuiModel + `","prompt":"x","media":[{"type":"last_frame","url":"https://a.example/1.png"}]}`,
		},
		{
			name: "参考视频",
			body: `{"model":"` + tehuiModel + `","prompt":"x","media":[{"type":"reference_video","url":"https://a.example/1.mp4"}]}`,
		},
		{
			name: "参考音频",
			body: `{"model":"` + tehuiModel + `","prompt":"x","media":[{"type":"reference_audio","url":"https://a.example/1.mp3"}]}`,
		},
		{
			name: "顶层 audio 参考音频",
			body: `{"model":"` + tehuiModel + `","prompt":"x","audio":"https://a.example/1.mp3"}`,
		},
		{
			name: "seed",
			body: `{"model":"` + tehuiModel + `","prompt":"x","seed":42}`,
		},
		{
			name: "11 张参考图",
			body: `{"model":"` + tehuiModel + `","prompt":"x","images":["https://a/1.png","https://a/2.png","https://a/3.png","https://a/4.png","https://a/5.png","https://a/6.png","https://a/7.png","https://a/8.png","https://a/9.png","https://a/10.png","https://a/11.png"]}`,
		},
		{
			name: "http 参考图",
			body: `{"model":"` + tehuiModel + `","prompt":"x","images":["http://a/1.png"]}`,
		},
		{
			name: "非标准比例",
			body: `{"model":"` + tehuiModel + `","prompt":"x","metadata":{"ratio":"21:9"}}`,
		},
		{
			name: "小数时长",
			body: `{"model":"` + tehuiModel + `","prompt":"x","duration":7.5}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := runTehuiXinbi(t, tc.body)
			body := decodeBody(t, rec.body)
			assert.Equal(t, tehuiModel, body["model"], "不支持的心笔请求必须保持原模型")
			assert.Empty(t, rec.originModel, "没分流就不该写计费原点")
		})
	}
}

// 比例与参考图要按心笔契约重写：ratio 进 metadata，图进顶层 images。
func TestWan3TehuiXinbiRoute_RewritesRatioAndImages(t *testing.T) {
	rec := runTehuiXinbi(t, `{"model":"`+tehuiModel+`","prompt":"x","seconds":"5",`+
		`"metadata":{"ratio":"9:16","media":[{"type":"reference_image","url":"https://a.example/1.png"}]}}`)

	body := decodeBody(t, rec.body)
	metadata, ok := body["metadata"].(map[string]interface{})
	require.True(t, ok, "metadata 必须是对象")
	assert.Equal(t, "9:16", metadata["ratio"])
	assert.NotContains(t, metadata, "media", "metadata.media 必须丢掉，心笔不认这个字段")

	images, ok := body["images"].([]interface{})
	require.True(t, ok, "参考图必须收到顶层 images（reference_images 会被 TaskSubmitReq 静默丢弃）")
	require.Len(t, images, 1)
	assert.Equal(t, "https://a.example/1.png", images[0])
}

// 分辨率：特惠只有一个交付档位（720P），客户端传 480P/1080P 本来就不会改变产物 ——
// 原 Wan3 链路是**无条件 480P 生成 + 提升到 720P**，参数直接忽略。
//
// 分流必须保持同一语义：不能因为参数不是 720P 就退回贵链路（白花钱），
// 也不能把分辨率原样透给心笔（顶层像素尺寸会被它当成比例误读）。
func TestWan3TehuiXinbiRoute_IgnoresRequestedResolution(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantRatio string
	}{
		{
			name: "顶层 1080P",
			body: `{"model":"` + tehuiModel + `","prompt":"x","resolution":"1080P"}`,
		},
		{
			name: "顶层 480P",
			body: `{"model":"` + tehuiModel + `","prompt":"x","resolution":"480P"}`,
		},
		{
			name: "顶层 720P",
			body: `{"model":"` + tehuiModel + `","prompt":"x","resolution":"720P"}`,
		},
		{
			name:      "像素尺寸 1920x1080",
			body:      `{"model":"` + tehuiModel + `","prompt":"x","size":"1920x1080"}`,
			wantRatio: "16:9",
		},
		{
			name:      "像素尺寸 720x1280",
			body:      `{"model":"` + tehuiModel + `","prompt":"x","size":"720x1280"}`,
			wantRatio: "9:16",
		},
		{
			name: "metadata.resolution 1080P",
			body: `{"model":"` + tehuiModel + `","prompt":"x","metadata":{"resolution":"1080P"}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := runTehuiXinbi(t, tc.body)
			body := decodeBody(t, rec.body)

			assert.Equal(t, xinbiapi.ModelNameTehuiRoute, body["model"],
				"非 720P 参数不能挡住分流：原链路同样只会交付 720P")
			assert.Equal(t, tehuiModel, rec.originModel, "计费原点仍是特惠")

			_, hasResolution := body["resolution"]
			_, hasSize := body["size"]
			assert.False(t, hasResolution, "分辨率不透传：心笔固定 480P 生成 + 720P 交付")
			assert.False(t, hasSize, "顶层像素尺寸会被心笔当成比例误读，必须剥掉")

			metadata, ok := body["metadata"].(map[string]interface{})
			require.True(t, ok, "metadata 必须是对象")
			assert.NotContains(t, metadata, "resolution", "metadata 里的分辨率同样要剥掉")
			if tc.wantRatio != "" {
				assert.Equal(t, tc.wantRatio, metadata["ratio"],
					"比例仍要从像素尺寸推断后保留，不能连画幅一起丢掉")
			}
		})
	}
}

// 规则二/三已经把计费原点设成促销族时，分流不能把它覆写成特惠。
func TestWan3TehuiXinbiRoute_PreservesExistingOriginModel(t *testing.T) {
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newTehuiXinbiEngineWithOrigin(rec, constant.Seedance25TeHuiModel)
	request := httptest.NewRequest("POST", "/v1/video/generations",
		bytes.NewBufferString(`{"model":"`+tehuiModel+`","prompt":"x","seconds":"5"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code, response.Body.String())
	body := decodeBody(t, rec.body)
	assert.Equal(t, xinbiapi.ModelNameTehuiRoute, body["model"])
	assert.Equal(t, constant.Seedance25TeHuiModel, rec.originModel,
		"计费原点必须保持客户最初请求的促销族模型")
}

// 心笔没有可用渠道时不分流 —— 改写后重试只会用同一个模型名重选渠道。
func TestWan3TehuiXinbiRoute_SkipsWhenTargetUnavailable(t *testing.T) {
	stubChannelAvailableByModel(t, map[string]bool{})
	rec := &routeRecorder{}
	engine := newTehuiXinbiEngine(rec)
	request := httptest.NewRequest("POST", "/v1/video/generations",
		bytes.NewBufferString(`{"model":"`+tehuiModel+`","prompt":"x","seconds":"5"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code, response.Body.String())
	assert.Equal(t, tehuiModel, decodeBody(t, rec.body)["model"])
	assert.Empty(t, rec.originModel)
}

// 规则二先把 9:16 改派到特惠，分流层必须接着把它分到心笔，
// 且计费原点仍是客户请求的 seedance2.5-特惠。
func TestWan3TehuiXinbiRoute_AfterRatioRuleKeepsPromoOrigin(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")
	stubChannelAvailable(t, true)

	rec := &routeRecorder{}
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Seedance25TeHuiRoute(), Wan3TehuiXinbiRoute())
	engine.POST("/v1/video/generations", recordRouteBody(rec))

	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(
		`{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code, response.Body.String())
	body := decodeBody(t, rec.body)
	assert.Equal(t, xinbiapi.ModelNameTehuiRoute, body["model"], "规则二改派后应继续分流到心笔")
	assert.Equal(t, constant.Seedance25TeHuiModel, rec.originModel,
		"计费原点必须仍是客户请求的促销族模型")
}

// 其它模型一律不碰。
func TestWan3TehuiXinbiRoute_IgnoresOtherModels(t *testing.T) {
	rec := runTehuiXinbi(t, `{"model":"wan3.0-720p-按次","prompt":"x","seconds":"5"}`)
	assert.Equal(t, "wan3.0-720p-按次", decodeBody(t, rec.body)["model"])
	assert.Empty(t, rec.originModel)
}

// 中间件里的目标模型名必须与渠道包一致 —— 否则改写出来的名字没有渠道能接。
func TestWan3TehuiXinbiRoute_TargetModelMatchesAdaptor(t *testing.T) {
	assert.Equal(t, xinbiapi.ModelNameTehuiRoute, xinbiTehuiTargetModel)
	assert.Contains(t, xinbiapi.ModelList, xinbiapi.ModelNameTehuiRoute)
}
