package middleware

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
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
}

// newRouteEngine 建一个挂上条件路由中间件的引擎。
//
// ⚠️ Use 必须在 POST 之前 —— gin 在注册路由时就快照了中间件链。
func newRouteEngine(rec *routeRecorder) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(Seedance25TeHuiRoute30s())
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
		c.String(200, "ok")
	})
	return engine
}

// runRoute 把一个请求体灌进引擎，返回下游 handler 看到的请求体与来源模型名。
func runRoute(t *testing.T, body string) (string, string) {
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
	engine.Use(Seedance25TeHuiRoute30s())
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
	engine.Use(Seedance25TeHuiRoute30s())
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
