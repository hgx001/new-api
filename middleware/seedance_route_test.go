package middleware

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
)

// 条件路由中间件的测试。
//
// 核心断言只有一条：**要么路由成功且请求体被正确改写，要么请求体逐字节不变**。
// 中间件永远不能「部分改写」—— 半改写是最危险的形态（素材丢了但模型换了）。
//
// ⚠️ 写这类测试时注意 gin 的一个坑：**engine.Use() 必须在路由注册之前调用**。
// gin 在注册路由那一刻就把 group.Handlers 快照进路由链，之后再加的中间件**不生效** ——
// 那样写的测试会「全部通过」但其实中间件从未执行，属于典型的假阳性。
// 本文件里每条「不路由」的用例，都靠同文件里的「应该路由」用例证明中间件确实挂上了。

const (
	testSourceModel = "seedance2.5-特惠"
	testTargetModel = "seedance-2-5"
)

// enableRoute 打开路由开关，测试结束时恢复原状。
func enableRoute(t *testing.T, source, target string, seconds int) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":         true,
		"source_model":    source,
		"target_model":    target,
		"trigger_seconds": seconds,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// routeRecorder 记录下游 handler 看到的请求体与路由来源模型名。
type routeRecorder struct {
	body        string
	originModel string
	// billingSeconds 是中间件记下的「源模型口径计费秒数」（未记录时为 0）。
	billingSeconds float64
	hasBillingSec  bool
}

// newRouteEngine 建一个挂上条件路由中间件的引擎。
//
// ⚠️ Use 必须在 POST 之前 —— gin 在注册路由时就快照了中间件链。
func newRouteEngine(rec *routeRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Seedance25TeHuiRoute())
	engine.POST("/v1/video/generations", func(c *gin.Context) {
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
		rec.billingSeconds, rec.hasBillingSec = common.GetContextKeyType[float64](c, constant.ContextKeyRouteBillingSeconds)
		c.String(200, "ok")
	})
	return engine
}

// stubChannelAvailable 覆盖「目标模型是否有可用渠道」的探针。
//
// 真实实现要读渠道缓存或查数据库，单测里没有这些环境。
// 默认让所有用例都走「渠道可用」，需要验预检行为的用例自己覆盖成 false。
func stubChannelAvailable(t *testing.T, available bool) {
	t.Helper()
	previous := channelAvailabilityProbe
	channelAvailabilityProbe = func(*gin.Context, string) bool { return available }
	t.Cleanup(func() { channelAvailabilityProbe = previous })
}

// runRoute 把一个请求体灌进引擎，返回下游 handler 看到的请求体与来源模型名。
//
// 默认按「目标渠道可用」跑 —— 绝大多数用例关心的是改写逻辑，
// 不是渠道预检。要验预检行为请用 runRouteRaw 并自行 stub。
func runRoute(t *testing.T, body string) (string, string) {
	t.Helper()
	stubChannelAvailable(t, true)
	return runRouteRaw(t, body)
}

// runRouteRaw 不做任何渠道探针假设，直接跑一遍。
func runRouteRaw(t *testing.T, body string) (string, string) {
	t.Helper()
	rec := &routeRecorder{}
	engine := newRouteEngine(rec)
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status = %d, want 200 (body=%s)", response.Code, response.Body.String())
	}
	return rec.body, rec.originModel
}

// runRouteFull 返回完整记录器（含中间件记下的计费秒数）。
//
// 与 runRoute 分开是为了不改动几十个既有用例的签名 —— 它们不关心计费口径。
func runRouteFull(t *testing.T, body string) *routeRecorder {
	t.Helper()
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newRouteEngine(rec)
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatalf("status = %d, want 200 (body=%s)", response.Code, response.Body.String())
	}
	return rec
}

// 规则一命中时必须记下「源模型口径的计费秒数」。
//
// 计费已改为按客户请求的源模型算（按秒 ¥0.20），而目标 adaptor（manwu）对按次
// 模型返回 nil 倍率。少了这个秒数，30 秒的请求会只按 1 秒收钱 —— 少收 29 秒。
func TestSeedanceRouteRecordsBillingSecondsForPerCallTarget(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`)
	if !rec.hasBillingSec {
		t.Fatalf("路由命中后没有记录计费秒数（key 缺失）")
	}
	if rec.billingSeconds != 30 {
		t.Fatalf("计费秒数 = %v, want 30", rec.billingSeconds)
	}
}

// 规则二（9:16 → wan3）不记计费秒数：目标 adaptor 自己会给 seconds，
// 记了反而可能覆盖它的判断（relay_task.go 只在缺失时补）。
func TestSeedanceRouteRatioRuleDoesNotForceBillingSeconds(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	if rec.hasBillingSec {
		t.Fatalf("比例规则不该记录计费秒数，却记成了 %v", rec.billingSeconds)
	}
}

// 规则一没命中（时长不是 30）时也不能留下计费秒数，
// 否则后续若因比例规则改派，会带着一个错的秒数去算钱。
func TestSeedanceRouteNoBillingSecondsWhenSecondsRuleMisses(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":"12","prompt":"a cat"}`)
	if rec.hasBillingSec {
		t.Fatalf("未命中时长规则却记录了计费秒数 %v", rec.billingSeconds)
	}
}

func TestSeedanceRouteDisabledKeepsBodyByteIdentical(t *testing.T) {
	// 开关关闭（默认）时，一个字节都不能动。
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	if err := seedanceroute_setting.UpdateFromJSONString(`{"enabled":false}`); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	body := `{"model":"seedance2.5-特惠","seconds":"30","prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed while disabled:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
	}
}

func TestSeedanceRouteRewritesModelAndSeconds(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","prompt":"a cat"}`
	seen, originModel := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v", err)
	}
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testTargetModel, seen)
	}
	if out["seconds"] != "30" {
		t.Fatalf("seconds = %v, want \"30\"", out["seconds"])
	}
	if out["prompt"] != "a cat" {
		t.Fatalf("prompt = %v, want \"a cat\"", out["prompt"])
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

func TestSeedanceRouteMatchesAllSecondsSpellings(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	cases := []struct {
		name string
		body string
	}{
		{"duration number", `{"model":"seedance2.5-特惠","duration":30}`},
		{"duration string", `{"model":"seedance2.5-特惠","duration":"30"}`},
		{"seconds number", `{"model":"seedance2.5-特惠","seconds":30}`},
		{"seconds string", `{"model":"seedance2.5-特惠","seconds":"30"}`},
		{"metadata duration", `{"model":"seedance2.5-特惠","metadata":{"duration":30}}`},
		{"metadata seconds", `{"model":"seedance2.5-特惠","metadata":{"seconds":"30"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(seen), &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out["model"] != testTargetModel {
				t.Fatalf("model = %v, want %s (body=%s)", out["model"], testTargetModel, tc.body)
			}
			if originModel != testSourceModel {
				t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
			}
		})
	}
}

func TestSeedanceRouteSkipsNonTriggerSeconds(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	cases := []struct {
		name string
		body string
	}{
		{"15 seconds", `{"model":"seedance2.5-特惠","seconds":"15"}`},
		{"no seconds", `{"model":"seedance2.5-特惠","prompt":"a cat"}`},
		{"other model", `{"model":"wan3.0-video","seconds":"30"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteNormalizesPaddedModelName(t *testing.T) {
	// 模型名带前后空格时，匹配阶段用 TrimSpace 后的值判断，
	// 但改写后必须把 model 换成**干净的目标名** ——
	// 下游 Distribute 是拿 body 里的 model 去选渠道的，
	// 留着带空格的名字会「找不到渠道」直接 404。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"  seedance2.5-特惠  ","seconds":"30"}`
	seen, originModel := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["model"] != testTargetModel {
		t.Fatalf("model = %q, want %q (下游要拿干净名字选渠道)", out["model"], testTargetModel)
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

func TestSeedanceRouteSkipsUnsupportedMedia(t *testing.T) {
	// 老板 2026-10-30 拍板：带参考视频/音频 → 不路由，走源模型继续跑（不是报错）。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	cases := []struct {
		name string
		body string
	}{
		{
			"reference video",
			`{"model":"seedance2.5-特惠","seconds":"30","metadata":{"media":[{"type":"reference_video","url":"https://x/v.mp4"}]}}`,
		},
		{
			"reference audio",
			`{"model":"seedance2.5-特惠","seconds":"30","metadata":{"media":[{"type":"reference_audio","url":"https://x/a.mp3"}]}}`,
		},
		{
			"video mixed with image",
			`{"model":"seedance2.5-特惠","seconds":"30","metadata":{"media":[{"type":"reference_image","url":"https://x/i.png"},{"type":"reference_video","url":"https://x/v.mp4"}]}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteSkipsExplicitResolution(t *testing.T) {
	// 豆包链路没有分辨率控件：带 resolution 的请求路由过去要么被 400，
	// 要么给一份分辨率不对的片 —— 两种都是交付问题，一律不路由。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	cases := []struct {
		name string
		body string
	}{
		{"top level resolution", `{"model":"seedance2.5-特惠","seconds":"30","resolution":"720P"}`},
		{"top level size", `{"model":"seedance2.5-特惠","seconds":"30","size":"1080P"}`},
		{"metadata resolution", `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"resolution":"720P"}}`},
		{"metadata size", `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"size":"1080P"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteAllowsRatioShapedSize(t *testing.T) {
	// 客户端习惯性传 size:"16:9"（比例语义）时，源链路本来就会忽略它
	// （normalizeResolution("16:9") 返回 ""）—— 不该因为这个把整单挡在路由外面。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","size":"16:9"}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testTargetModel, seen)
	}
	if _, exists := out["size"]; exists {
		t.Fatalf("size must be dropped (目标链路会把它当比例读), body = %s", seen)
	}
}

func TestSeedanceRouteDropsUnsupportedTopLevelFields(t *testing.T) {
	// 漫屋 rejectFields 见到顶层 resolution 会显式 400（"does not support resolution"），
	// 见到顶层 size 会把它当**比例**读 → "720P" 不在比例白名单里 → 同样 400。
	// 两道防线：
	//   ① 用户明确要了分辨率 → 整单不路由（见 TestSeedanceRouteSkipsExplicitResolution）；
	//   ② 万一路由了，改写时也必须把 resolution / size 摘掉（纵深防御）。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	// 用一个「不被第①条拦下」的形态来验证第②条：
	// 顶层 resolution 存在但为空串 → 视为「没指定分辨率」→ 允许路由。
	body := `{"model":"seedance2.5-特惠","seconds":"30","resolution":"","size":"","prompt":"x"}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testTargetModel, seen)
	}
	if _, exists := out["resolution"]; exists {
		t.Fatalf("resolution must not be forwarded to target chain, body = %s", seen)
	}
	if _, exists := out["size"]; exists {
		t.Fatalf("size must not be forwarded to target chain, body = %s", seen)
	}
}

func TestSeedanceRouteUsesTopLevelMediaBeforeMetadataMedia(t *testing.T) {
	// 源链路（youzanwan3 buildMedia）的素材优先级是：
	// 顶层 media > metadata.media > 顶层 images/image/input_reference。
	// 优先级错了就会丢图 —— 源链路认顶层 media 时我们只读 metadata.media，
	// 转换出来的图数就少了。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","media":[{"type":"reference_image","url":"https://x/top.png"}],"metadata":{"media":[{"type":"reference_image","url":"https://x/meta.png"}]}}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	images, ok := out["input_reference"].([]interface{})
	if !ok {
		t.Fatalf("input_reference missing, body = %s", seen)
	}
	if len(images) != 1 || images[0] != "https://x/top.png" {
		t.Fatalf("input_reference = %v, want only the top-level media image (源链路优先级)", images)
	}
}

func TestSeedanceRouteSkipsTopLevelAudio(t *testing.T) {
	// 顶层 audio / audios 是参考音频，目标链路不支持 → 不路由。
	enableRoute(t, testSourceModel, testTargetModel, 30)

	cases := []struct {
		name string
		body string
	}{
		{"top level audio", `{"model":"seedance2.5-特惠","seconds":"30","audio":"https://x/a.mp3"}`},
		{"top level audios", `{"model":"seedance2.5-特惠","seconds":"30","audios":["https://x/a.mp3"]}`},
		{"top level media video", `{"model":"seedance2.5-特惠","seconds":"30","media":[{"type":"reference_video","url":"https://x/v.mp4"}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteConvertsReferenceImages(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"media":[{"type":"reference_image","url":"https://x/1.png"},{"type":"reference_image","url":"https://x/2.png"}]}}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	images, ok := out["input_reference"].([]interface{})
	if !ok {
		t.Fatalf("input_reference missing, body = %s", seen)
	}
	if len(images) != 2 {
		t.Fatalf("input_reference len = %d, want 2 (图数必须一致，不能静默丢图)", len(images))
	}
	if images[0] != "https://x/1.png" || images[1] != "https://x/2.png" {
		t.Fatalf("input_reference = %v, want original order", images)
	}
	if _, exists := out["metadata"]; exists {
		t.Fatalf("metadata should be removed for target chain, body = %s", seen)
	}
}

func TestSeedanceRouteMapsAdaptiveRatioToAuto(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"ratio":"adaptive"}}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["ratio"] != "auto" {
		t.Fatalf("ratio = %v, want \"auto\"", out["ratio"])
	}
}

func TestSeedanceRouteKeepsExplicitRatio(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	body := `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"ratio":"16:9"}}`
	seen, _ := runRoute(t, body)

	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["ratio"] != "16:9" {
		t.Fatalf("ratio = %v, want \"16:9\"", out["ratio"])
	}
}

func TestSeedanceRouteSkipsRemixPath(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Seedance25TeHuiRoute())
	var seenBody string
	engine.POST("/v1/videos/:video_id/remix", func(c *gin.Context) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.String(500, "read body storage: %v", err)
			return
		}
		raw, _ := storage.Bytes()
		seenBody = string(raw)
		c.String(200, "ok")
	})

	body := `{"model":"seedance2.5-特惠","seconds":"30"}`
	request := httptest.NewRequest("POST", "/v1/videos/abc/remix", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if seenBody != body {
		t.Fatalf("remix body changed:\n got %s\nwant %s", seenBody, body)
	}
}

func TestSeedanceRouteSkipsNonJSONContentType(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 30)

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Seedance25TeHuiRoute())
	var seenBody string
	engine.POST("/v1/video/generations", func(c *gin.Context) {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			c.String(500, "read body storage: %v", err)
			return
		}
		raw, _ := storage.Bytes()
		seenBody = string(raw)
		c.String(200, "ok")
	})

	body := `{"model":"seedance2.5-特惠","seconds":"30"}`
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	if seenBody != body {
		t.Fatalf("non-JSON body changed:\n got %s\nwant %s", seenBody, body)
	}
}

func TestSeedanceRouteIncompletePolicyStaysDisabled(t *testing.T) {
	// 半开状态（缺 target_model）必须视为「没开」——
	// 否则会把请求改写成一个空模型名，线上直接全挂。
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	if err := seedanceroute_setting.UpdateFromJSONString(
		`{"enabled":true,"source_model":"seedance2.5-特惠","trigger_seconds":30}`); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if seedanceroute_setting.IsEnabled() {
		t.Fatal("IsEnabled() = true, want false when target_model missing")
	}

	body := `{"model":"seedance2.5-特惠","seconds":"30"}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed with incomplete policy:\n got %s\nwant %s", seen, body)
	}
}

// ─────────────────────────────────────────────────────────────
// 规则二：比例路由（seedance2.5-特惠 + 9:16 → wan3.0-720p-特惠）
// ─────────────────────────────────────────────────────────────

const testRatioTargetModel = "wan3.0-720p-特惠"

// enableBothRules 同时打开时长规则与比例规则，测试结束时恢复原状。
func enableBothRules(t *testing.T, secondsTarget string, seconds int, ratioTarget string, ratio string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":            true,
		"source_model":       testSourceModel,
		"target_model":       secondsTarget,
		"trigger_seconds":    seconds,
		"ratio_target_model": ratioTarget,
		"trigger_ratio":      ratio,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

func decodeBody(t *testing.T, body string) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	return out
}

func TestSeedanceRouteRatioRoutesNon30sToWan3(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	// 15 秒 + 9:16：时长规则不命中（15≠30），比例规则命中 → 走 wan3。
	body := `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testRatioTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testRatioTargetModel, seen)
	}
	if out["prompt"] != "a cat" {
		t.Fatalf("prompt = %v, want a cat", out["prompt"])
	}
	// 时长必须原样保留 —— wan3 支持 2~30 秒逐秒，不能被改成 30。
	if out["seconds"] != "15" {
		t.Fatalf("seconds = %v, want 15", out["seconds"])
	}
	metadata, ok := out["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing in %s", seen)
	}
	if metadata["ratio"] != "9:16" {
		t.Fatalf("metadata.ratio = %v, want 9:16", metadata["ratio"])
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

func TestSeedanceRouteSecondsRuleWinsOverRatio(t *testing.T) {
	// 老板 2026-10-30 拍板「30 秒优先，豆包仍接 9:16」：
	// 30 秒 + 9:16 必须走豆包，不能走 wan3。
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"ratio":"9:16"}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s（30 秒必须优先走豆包）", out["model"], testTargetModel)
	}
}

func TestSeedanceRouteRatioFromAspectRatioAndSize(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	cases := []struct {
		name string
		body string
	}{
		{"metadata aspect_ratio", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"aspect_ratio":"9:16"}}`},
		{"top level ratio", `{"model":"seedance2.5-特惠","seconds":"10","ratio":"9:16"}`},
		{"size 720x1280", `{"model":"seedance2.5-特惠","seconds":"10","size":"720x1280"}`},
		{"size 480x854 (480P 竖屏)", `{"model":"seedance2.5-特惠","seconds":"10","size":"480x854"}`},
		{"full-width colon", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9：16"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, _ := runRoute(t, tc.body)
			out := decodeBody(t, seen)
			if out["model"] != testRatioTargetModel {
				t.Fatalf("model = %v, want %s (body=%s)", out["model"], testRatioTargetModel, seen)
			}
		})
	}
}

func TestSeedanceRouteRatioSkipsOtherRatios(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	cases := []struct {
		name string
		body string
	}{
		{"16:9", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"16:9"}}`},
		{"1:1", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"1:1"}}`},
		{"adaptive", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"adaptive"}}`},
		{"no ratio", `{"model":"seedance2.5-特惠","seconds":"10"}`},
		{"other model", `{"model":"wan3.0-video","seconds":"10","metadata":{"ratio":"9:16"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteRatioSkipsExplicit1080P(t *testing.T) {
	// 目标模型固定交付 720P（480P 生成 + 提升），要 1080P 时路由过去等于降级交付。
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	cases := []struct {
		name string
		body string
	}{
		{"top level resolution", `{"model":"seedance2.5-特惠","seconds":"10","resolution":"1080P","metadata":{"ratio":"9:16"}}`},
		{"metadata resolution", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9:16","resolution":"1080P"}}`},
		{"size 1920x1080", `{"model":"seedance2.5-特惠","seconds":"10","size":"1920x1080","metadata":{"ratio":"9:16"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, _ := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but should not route:\n got %s\nwant %s", seen, tc.body)
			}
		})
	}
}

func TestSeedanceRouteRatioKeepsMediaUntouched(t *testing.T) {
	// wan3 渠道的 buildWan3Media 素材优先级与源链路一致（顶层 media > metadata.media），
	// 所以**不需要**转换格式 —— 原样保留即可，绝不能像豆包规则那样改写成 input_reference。
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9:16"},"media":[{"type":"reference_image","url":"https://example.com/a.png"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testRatioTargetModel {
		t.Fatalf("model = %v, want %s", out["model"], testRatioTargetModel)
	}
	if _, ok := out["input_reference"]; ok {
		t.Fatalf("must not convert media to input_reference for wan3 (body=%s)", seen)
	}
	media, ok := out["media"].([]interface{})
	if !ok || len(media) != 1 {
		t.Fatalf("media lost: %v (body=%s)", out["media"], seen)
	}
	entry, _ := media[0].(map[string]interface{})
	if entry["url"] != "https://example.com/a.png" {
		t.Fatalf("media url = %v", entry["url"])
	}
}

func TestSeedanceRouteSkipsWhenTargetChannelUnavailable(t *testing.T) {
	// 中间件改写 body 之后，重试只会用同一个模型名重选渠道，不会退回源模型。
	// 所以目标模型没渠道时必须**不路由**，让请求自然落回源链路 ——
	// 否则 Distribute 会直接 503，客户撞错误。
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")
	stubChannelAvailable(t, false)

	cases := []struct {
		name string
		body string
	}{
		{"seconds rule", `{"model":"seedance2.5-特惠","seconds":"30"}`},
		{"ratio rule", `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9:16"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRouteRaw(t, tc.body)
			if seen != tc.body {
				t.Fatalf("body changed but target channel unavailable:\n got %s\nwant %s", seen, tc.body)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteLegacyPolicyKeepsRatioRuleOff(t *testing.T) {
	// 旧的生产 option JSON 没有 ratio_target_model / trigger_ratio。
	// 解析后比例规则必须**不生效** —— 否则等于在没批准的情况下上线一条新路由。
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	legacy := `{"enabled":true,"source_model":"seedance2.5-特惠","target_model":"seedance-2-5","trigger_seconds":30}`
	if err := seedanceroute_setting.UpdateFromJSONString(legacy); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if seedanceroute_setting.IsRatioRouteEnabled() {
		t.Fatal("IsRatioRouteEnabled() = true, want false for legacy policy")
	}

	body := `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9:16"}}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed with legacy policy:\n got %s\nwant %s", seen, body)
	}
}

func TestSeedanceRouteRatioOnlyPolicyStillWorks(t *testing.T) {
	// 只配比例规则、不配时长规则时，中间件也必须正常工作。
	// 早退条件若写成 IsEnabled()，这里会直接跳过整个中间件（比例规则永不生效）。
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	ratioOnly := `{"enabled":true,"source_model":"seedance2.5-特惠","ratio_target_model":"wan3.0-720p-特惠","trigger_ratio":"9:16"}`
	if err := seedanceroute_setting.UpdateFromJSONString(ratioOnly); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	if !seedanceroute_setting.IsAnyRouteEnabled() {
		t.Fatal("IsAnyRouteEnabled() = false, want true when ratio rule configured")
	}
	if seedanceroute_setting.IsEnabled() {
		t.Fatal("IsEnabled() = true, want false when seconds rule not configured")
	}

	body := `{"model":"seedance2.5-特惠","seconds":"10","metadata":{"ratio":"9:16"}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testRatioTargetModel {
		t.Fatalf("model = %v, want %s", out["model"], testRatioTargetModel)
	}
}

// ─────────────────────────────────────────────────────────────
// 规则一的「带到期时间的暂停」（客户端「今日额度已用完」按钮的后端效果）
// ─────────────────────────────────────────────────────────────

// pauseSecondsRule 把规则一暂停到指定 Unix 秒。
//
// 这里不冻结时钟（setting 包内的测试已经精确覆盖了到期边界）：
// 中间件层只关心「暂停中 → 不改写」与「已过期 → 照常改写」两种可观测行为，
// 用真实的 now 加减偏移就够，反而少一层测试与时间的耦合。
func pauseSecondsRule(t *testing.T, until int64) {
	t.Helper()
	patched := seedanceroute_setting.WithSecondsRulePausedUntil(until)
	encoded, err := common.Marshal(patched)
	if err != nil {
		t.Fatalf("marshal patched policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update patched policy: %v", err)
	}
}

func TestSeedanceRouteSecondsRulePausedFallsBackToSource(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")
	pauseSecondsRule(t, time.Now().Unix()+3600)

	if seedanceroute_setting.IsEnabled() {
		t.Fatal("IsEnabled() = true, want false（暂停期内）")
	}

	// 30 秒请求在暂停期内必须**逐字节不变**地落到源链路（有赞）。
	body := `{"model":"seedance2.5-特惠","seconds":"30","prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed while paused:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty（没发生改派）", originModel)
	}
}

func TestSeedanceRoutePauseDoesNotAffectRatioRule(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")
	pauseSecondsRule(t, time.Now().Unix()+3600)

	// 暂停只针对「按时长改派到豆包」，比例规则必须照常生效 ——
	// 否则老板点一下「豆包没额度」，9:16 的优化也一起没了。
	body := `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testRatioTargetModel {
		t.Fatalf("model = %v, want %s（暂停不该影响比例规则）", out["model"], testRatioTargetModel)
	}
}

func TestSeedanceRouteExpiredPauseRoutesAgain(t *testing.T) {
	enableBothRules(t, testTargetModel, 30, testRatioTargetModel, "9:16")
	// 截止时间已过 1 秒 → 应视为已恢复，不需要任何人手动打开。
	pauseSecondsRule(t, time.Now().Unix()-1)

	if !seedanceroute_setting.IsEnabled() {
		t.Fatal("IsEnabled() = false, want true（暂停已过期）")
	}

	body := `{"model":"seedance2.5-特惠","seconds":"30","prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s after pause expired", out["model"], testTargetModel)
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}
