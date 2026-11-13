package middleware

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	enableRouteWithGate(t, source, target, seconds, "")
}

// enableRouteWithGate 额外配置规则一的比例闸门（空串 = 不限比例）。
func enableRouteWithGate(t *testing.T, source, target string, seconds int, ratioGate string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":         true,
		"source_model":    source,
		"target_model":    target,
		"trigger_seconds": seconds,
	}
	if ratioGate != "" {
		payload["seconds_rule_ratio_gate"] = ratioGate
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
	// billingSizeRatio 是中间件记下的「源模型口径 size 倍率」（未记录时为 0）。
	//
	// 规则六（短时长 → 标准版）需要它：目标 wan3.0-video 三档同价、adaptor 给 1.0，
	// 而源模型 seedance2.5-特惠 的 1080P 是 1.5 倍。不覆盖就会少收。
	billingSizeRatio float64
	hasBillingSize   bool
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
		rec.billingSizeRatio, rec.hasBillingSize = common.GetContextKeyType[float64](c, constant.ContextKeyRouteBillingSizeRatio)
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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

	rec := runRouteFull(t, `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`)
	if rec.hasBillingSec {
		t.Fatalf("比例规则不该记录计费秒数，却记成了 %v", rec.billingSeconds)
	}
}

// 规则一没命中（时长不是 30）时也不能留下计费秒数，
// 否则后续若因比例规则改派，会带着一个错的秒数去算钱。
func TestSeedanceRouteNoBillingSecondsWhenSecondsRuleMisses(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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

func TestSeedanceRouteAllowsRatioShapedSize(t *testing.T) {
	// 客户端习惯性传 size:"16:9"（比例语义）时，源链路本来就会忽略它
	// （normalizeResolution("16:9") 返回 ""）—— 不该因为这个把整单挡在路由外面。
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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

// 回归 2026-11-09 gen-7cdab0938eba1d6229b01a5e：ArcReel 非豆包形状把参考图
// 编码在 metadata.input.media；网关改派豆包时会整块丢弃 metadata，必须先把图
// 抬到顶层 input_reference，否则远端 job refs=0，成片完全没有参考图。
func TestSeedanceRouteReadsWan3InputMediaReferenceImages(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":"26","metadata":{"input":{"media":[{"type":"reference_image","url":"https://x/1.png"},{"type":"reference_image","url":"https://x/2.png"}]}}}`
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
		t.Fatalf("input_reference len = %d, want 2 (metadata.input.media 的图不能丢)", len(images))
	}
	if images[0] != "https://x/1.png" || images[1] != "https://x/2.png" {
		t.Fatalf("input_reference = %v, want original order", images)
	}
	if _, exists := out["metadata"]; exists {
		t.Fatalf("metadata should be removed for target chain, body = %s", seen)
	}
	if out["seconds"] != "26" {
		t.Fatalf("seconds = %v, want \"26\"", out["seconds"])
	}
}

// metadata.input.media 里是 data URI 时不能改派：漫屋只收公网 http(s)。
// 正确做法是放弃改派、保留原请求走源链路（源链路能处理 data URI），绝不静默丢图。
func TestSeedanceRouteSkipsRerouteWhenWan3InputMediaIsNotHTTP(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":"26","metadata":{"input":{"media":[{"type":"reference_image","url":"data:image/png;base64,AAAA"}]}}}`
	seen, origin := runRoute(t, body)

	if seen != body {
		t.Fatalf("带 data URI 参考图时应放弃改派、保留原请求，got %s", seen)
	}
	if origin != "" {
		t.Fatalf("originModel = %q, want empty (not routed)", origin)
	}
}

// metadata.input.media 里的参考视频同样不能改派到豆包（豆包链路不收）。
func TestSeedanceRouteSkipsRerouteWhenWan3InputMediaHasVideo(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":"26","metadata":{"input":{"media":[{"type":"reference_video","url":"https://x/1.mp4"}]}}}`
	seen, origin := runRoute(t, body)

	if seen != body {
		t.Fatalf("带参考视频时应放弃改派、保留原请求，got %s", seen)
	}
	if origin != "" {
		t.Fatalf("originModel = %q, want empty (not routed)", origin)
	}
}

// metadata.media 与 metadata.input.media 同时出现时只抬前者：同一批图被抬两遍
// 会让 input_reference 出现重复项，参考图编号（图1..图N）整体错位。
func TestSeedanceRoutePrefersMetadataMediaOverWan3InputMedia(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":"26","metadata":{"media":[{"type":"reference_image","url":"https://x/a.png"}],"input":{"media":[{"type":"reference_image","url":"https://x/b.png"}]}}}`
	seen, _ := runRoute(t, body)

	out := decodeBody(t, seen)
	images, ok := out["input_reference"].([]interface{})
	if !ok {
		t.Fatalf("input_reference missing, body = %s", seen)
	}
	if len(images) != 1 || images[0] != "https://x/a.png" {
		t.Fatalf("input_reference = %v, want [https://x/a.png]（metadata.media 优先，不能重复抬图）", images)
	}
}

// metadata.input.media 里图与视频混排时同样放弃改派：豆包链路只收图，
// 丢掉视频等于换掉用户的参考素材，宁可留在源链路。
func TestSeedanceRouteSkipsRerouteWhenWan3InputMediaMixesImageAndVideo(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":"26","metadata":{"input":{"media":[{"type":"reference_image","url":"https://x/1.png"},{"type":"reference_video","url":"https://x/1.mp4"}]}}}`
	seen, origin := runRoute(t, body)

	if seen != body {
		t.Fatalf("图+视频混排时应放弃改派、保留原请求，got %s", seen)
	}
	if origin != "" {
		t.Fatalf("originModel = %q, want empty (not routed)", origin)
	}
}

func TestSeedanceRouteMapsAdaptiveRatioToAuto(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableRoute(t, testSourceModel, testTargetModel, 20)

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":"30","metadata":{"ratio":"9:16"}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testTargetModel {
		t.Fatalf("model = %v, want %s（30 秒必须优先走豆包）", out["model"], testTargetModel)
	}
}

func TestSeedanceRouteRatioFromAspectRatioAndSize(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")
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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")
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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")
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
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")
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

// ─────────────────────────────────────────────────────────────
// 规则三：首尾帧路由（seedance2.5-特惠 + first_frame/last_frame → wan3.0-720p-特惠）
//
// 背景（2026-11-01）：源链路 youzan wan3 已彻底不收首尾帧
// （2026-10-28 上游取消 firstFrameAssetId/lastFrameAssetId），带首尾帧的请求
// 打到源模型必然 400；规则一的目标（漫屋）也不支持首尾帧。所以带首尾帧的请求
// 只能走 wan3 直连。
// ─────────────────────────────────────────────────────────────

const testFramesTargetModel = "wan3.0-720p-特惠"

// enableAllRules 打开全部三条规则，测试结束时恢复原状。
func enableAllRules(t *testing.T, secondsTarget string, seconds int, ratioTarget, ratio, framesTarget string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":             true,
		"source_model":        testSourceModel,
		"target_model":        secondsTarget,
		"trigger_seconds":     seconds,
		"ratio_target_model":  ratioTarget,
		"trigger_ratio":       ratio,
		"frames_target_model": framesTarget,
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

func TestSeedanceRouteFramesGoToWan3(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 16:9 + 首帧：比例规则不命中（9:16≠16:9），首尾帧规则必须接住。
	body := `{"model":"seedance2.5-特惠","seconds":"10","prompt":"a cat","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testFramesTargetModel, seen)
	}
	if out["seconds"] != "10" {
		t.Fatalf("seconds = %v, want 10（首尾帧不改时长）", out["seconds"])
	}
	if out["prompt"] != "a cat" {
		t.Fatalf("prompt = %v, want a cat", out["prompt"])
	}
	// 首尾帧必须原样带过去 —— 这正是改派的目的。
	media, ok := out["media"].([]interface{})
	if !ok || len(media) != 1 {
		t.Fatalf("media missing in %s", seen)
	}
	first, _ := media[0].(map[string]interface{})
	if first["type"] != "first_frame" {
		t.Fatalf("media[0].type = %v, want first_frame", first["type"])
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q（计费按源模型）", originModel, testSourceModel)
	}
}

func TestSeedanceRouteFramesBeatSecondsRule(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 30 秒 + 首帧：时长规则本来会命中（改派到漫屋），但漫屋不支持首尾帧，
	// 必须让首尾帧规则先接住 —— 否则这一单必然失败。
	body := `{"model":"seedance2.5-特惠","seconds":"30","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s（首尾帧优先于时长规则）", out["model"], testFramesTargetModel)
	}
}

func TestSeedanceRouteFramesKeepsUserRatio(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 顶层 16:9 + 首帧：改派后比例必须仍是 16:9，绝不能被改成触发值 9:16。
	body := `{"model":"seedance2.5-特惠","ratio":"16:9","media":[{"type":"last_frame","url":"https://x/b.png"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s", out["model"], testFramesTargetModel)
	}
	metadata, ok := out["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata missing in %s", seen)
	}
	if metadata["ratio"] != "16:9" {
		t.Fatalf("metadata.ratio = %v, want 16:9（用户画幅不能被触发值覆盖）", metadata["ratio"])
	}
}

func TestSeedanceRouteFramesInMetadataMedia(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 首尾帧也可能写在 metadata.media 里（源链路同样认这个位置）。
	body := `{"model":"seedance2.5-特惠","metadata":{"media":[{"type":"first_frame","url":"https://x/a.png"}]}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testFramesTargetModel, seen)
	}
}

func TestSeedanceRouteFramesInMetadataInputMedia(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 第三种位置：metadata.input.media（youzanwan3 的 metadataMedia 也读这里）。
	body := `{"model":"seedance2.5-特惠","metadata":{"input":{"media":[{"type":"last_frame","url":"https://x/b.png"}]}}}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s (body=%s)", out["model"], testFramesTargetModel, seen)
	}
}

func TestSeedanceRouteReferenceImageNotTreatedAsFrames(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// 纯参考图不是首尾帧：16:9 的参考图请求不该被规则三接走，
	// 也不该被规则二接走（9:16 不匹配）→ 逐字节不变。
	body := `{"model":"seedance2.5-特惠","ratio":"16:9","media":[{"type":"reference_image","url":"https://x/a.png"}]}`
	seen, originModel := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed for reference_image:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
	}
}

func TestSeedanceRouteFramesDetectedInMixedMedia(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)

	// media 数组里允许混入裸字符串（等价 reference_image）。整体反序列化成结构体
	// 切片会因为那条字符串失败 → 漏判首尾帧。必须逐条解析才认得出首帧。
	body := `{"model":"seedance2.5-特惠","media":["https://x/ref.png",{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s（混合 media 里的首帧必须被识别）", out["model"], testFramesTargetModel)
	}
}

func TestSeedanceRouteFramesRuleOffWhenTargetEmpty(t *testing.T) {
	// 只配时长+比例规则（旧的生产配置）→ 首尾帧请求必须逐字节不变，
	// 行为与改造前完全一致。这是「加字段不影响存量配置」的硬要求。
	enableBothRules(t, testTargetModel, 20, testRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","ratio":"16:9","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("body changed with frames rule off:\n got %s\nwant %s", seen, body)
	}
}

func TestSeedanceRouteFramesSkipWhenNoChannel(t *testing.T) {
	enableAllRules(t, testTargetModel, 20, testRatioTargetModel, "9:16", testFramesTargetModel)
	stubChannelAvailable(t, false)

	// 目标渠道不可用 → 不路由（放行原请求）。中间件永远不挡客户。
	body := `{"model":"seedance2.5-特惠","ratio":"16:9","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRouteRaw(t, body)
	if seen != body {
		t.Fatalf("body changed when target channel unavailable:\n got %s\nwant %s", seen, body)
	}
}

func TestSeedanceRouteFramesOnlyConfigEnablesMiddleware(t *testing.T) {
	// 只配首尾帧规则（时长/比例都没配全）→ IsAnyRouteEnabled 必须为 true，
	// 否则中间件在入口就早退，规则三永远不生效。
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":             true,
		"source_model":        testSourceModel,
		"frames_target_model": testFramesTargetModel,
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

	if !seedanceroute_setting.IsAnyRouteEnabled() {
		t.Fatal("IsAnyRouteEnabled() = false，中间件会早退，规则三永远不生效")
	}
	if !seedanceroute_setting.IsFramesRouteEnabled() {
		t.Fatal("IsFramesRouteEnabled() = false, want true")
	}
	if seedanceroute_setting.IsEnabled() {
		t.Fatal("IsEnabled() = true，时长规则没配全不该生效")
	}

	body := `{"model":"seedance2.5-特惠","media":[{"type":"first_frame","url":"https://x/a.png"}]}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)
	if out["model"] != testFramesTargetModel {
		t.Fatalf("model = %v, want %s", out["model"], testFramesTargetModel)
	}
}

// ─────────────────────────────────────────────────────────────
// 规则一阈值：大于 TriggerSeconds（生产配 20）即改派，不再要求恰好 30 秒
//
// 2026-11-08 需求：豆包按次成本固定（¥2.00/次），源链路按秒计价（¥0.20/秒），
// 21 秒以后源链路已经比豆包更贵，没必要非等到 30 秒整档才改派。
//
// 2026-11-09 用户口径修正：豆包 2.5 支持任意正整数秒，下发值必须与客户请求
// **完全一致**，禁止向上吸附（以前 21–29 秒会被改成 30 秒出片）。
// ─────────────────────────────────────────────────────────────

func TestDoubaoSecondsForPassesThrough(t *testing.T) {
	cases := []struct {
		requested int
		want      int
		ok        bool
	}{
		{requested: 5, want: 5, ok: true},
		{requested: 7, want: 7, ok: true},
		{requested: 15, want: 15, ok: true},
		{requested: 21, want: 21, ok: true},
		{requested: 23, want: 23, ok: true},
		{requested: 25, want: 25, ok: true},
		{requested: 30, want: 30, ok: true},
		{requested: 31, want: 31, ok: true},
		{requested: 99, want: 99, ok: true},
		{requested: 0, ok: false},
		{requested: -3, ok: false},
	}
	for _, tc := range cases {
		got, ok := doubaoSecondsFor(tc.requested)
		if ok != tc.ok {
			t.Fatalf("doubaoSecondsFor(%d) ok = %v, want %v", tc.requested, ok, tc.ok)
		}
		if ok && got != tc.want {
			t.Fatalf("doubaoSecondsFor(%d) = %d, want %d（必须原样透传）", tc.requested, got, tc.want)
		}
	}
}

func TestSeedanceRoutePassesRequestedSecondsThrough(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	cases := []struct {
		requested   int
		wantTierSec string
	}{
		{requested: 21, wantTierSec: "21"},
		{requested: 23, wantTierSec: "23"},
		{requested: 25, wantTierSec: "25"},
		{requested: 29, wantTierSec: "29"},
		{requested: 30, wantTierSec: "30"},
		{requested: 45, wantTierSec: "45"},
	}

	for _, tc := range cases {
		body := `{"model":"seedance2.5-特惠","seconds":"` + strconv.Itoa(tc.requested) + `","prompt":"a cat"}`
		rec := runRouteFull(t, body)

		out := decodeBody(t, rec.body)
		if out["model"] != testTargetModel {
			t.Fatalf("%d 秒：model = %v, want %s（超过阈值必须改派豆包）",
				tc.requested, out["model"], testTargetModel)
		}
		if out["seconds"] != tc.wantTierSec {
			t.Fatalf("%d 秒：下发 seconds = %v, want %s（必须原样透传，禁止吸附档位）",
				tc.requested, out["seconds"], tc.wantTierSec)
		}
		// 计费与下发值都必须是客户请求的秒数。
		if !rec.hasBillingSec {
			t.Fatalf("%d 秒：没有记录 ContextKeyRouteBillingSeconds", tc.requested)
		}
		if rec.billingSeconds != float64(tc.requested) {
			t.Fatalf("%d 秒：计费秒数 = %v, want %d（不能改值）",
				tc.requested, rec.billingSeconds, tc.requested)
		}
	}
}

func TestSeedanceRouteKeepsSourceAtOrBelowThreshold(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	for _, requested := range []int{5, 10, 15, 20} {
		body := `{"model":"seedance2.5-特惠","seconds":"` + strconv.Itoa(requested) + `","prompt":"a cat"}`
		seen, _ := runRoute(t, body)
		if seen != body {
			t.Fatalf("%d 秒：不应改派（阈值是严格大于 20），body 被改成了 %s", requested, seen)
		}
	}
}

// ─────────────────────────────────────────────────────────────
// 规则一比例闸门（2026-11-12）：只有 9:16 且超阈值才改派豆包。
//
// 2026-11-12 用户口径：豆包官网 9:16 出片效果最好，其它比例的长时长
// 请求留在常驻链路（有赞原生 720P）。非 9:16 的 21~30 秒请求**不再**改派
// （此前无条件改派）—— 这是主动收窄，不是回滚。
// ─────────────────────────────────────────────────────────────

func TestSeedanceRouteRatioGateBlocksNonNineSixteen(t *testing.T) {
	enableRouteWithGate(t, testSourceModel, testTargetModel, 20, "9:16")

	cases := []struct {
		name string
		body string
	}{
		{"16:9", `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"16:9"},"prompt":"a cat"}`},
		{"1:1", `{"model":"seedance2.5-特惠","seconds":25,"ratio":"1:1","prompt":"a cat"}`},
		{"无比例", `{"model":"seedance2.5-特惠","seconds":25,"prompt":"a cat"}`},
		{"adaptive 不算命中", `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"adaptive"},"prompt":"a cat"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			if seen != tc.body {
				t.Fatalf("有闸门时非 9:16 不应改派，body 被改成了 %s", seen)
			}
			if originModel != "" {
				t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
			}
		})
	}
}

func TestSeedanceRouteRatioGatePassesNineSixteen(t *testing.T) {
	enableRouteWithGate(t, testSourceModel, testTargetModel, 20, "9:16")

	cases := []struct {
		name string
		body string
	}{
		{"metadata.ratio 9:16", `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`},
		{"顶层 ratio 写法归一", `{"model":"seedance2.5-特惠","seconds":25,"ratio":"9/16","prompt":"a cat"}`},
		{"size 推断 720x1280", `{"model":"seedance2.5-特惠","seconds":25,"size":"720x1280","prompt":"a cat"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			var out map[string]interface{}
			if err := json.Unmarshal([]byte(seen), &out); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if out["model"] != testTargetModel {
				t.Fatalf("9:16 超阈值必须改派，model = %v（body=%s）", out["model"], tc.body)
			}
			if originModel != testSourceModel {
				t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
			}
		})
	}
}

// 9:16 但没超阈值：两条闸门都要过，缺一不改派。
func TestSeedanceRouteRatioGateStillRequiresThreshold(t *testing.T) {
	enableRouteWithGate(t, testSourceModel, testTargetModel, 20, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":20,"metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, _ := runRoute(t, body)
	if seen != body {
		t.Fatalf("9:16 但 20 秒未超阈值不应改派，body 被改成了 %s", seen)
	}
}

// 闸门为空 = 保持无条件改派（旧生产 JSON 没这个字段，行为不能变）。
func TestSeedanceRouteRatioGateEmptyKeepsLegacyBehavior(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	body := `{"model":"seedance2.5-特惠","seconds":25,"metadata":{"ratio":"16:9"},"prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	var out map[string]interface{}
	if err := json.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out["model"] != testTargetModel {
		t.Fatalf("空闸门时任何比例超阈值都要改派，model = %v", out["model"])
	}
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

// 带 480P/720P/1080P 分辨率的请求必须照常改派，并把档位抬到顶层 resolution 透传。
//
// 2026-11-09 口径：豆包官网三档全支持，带分辨率不再是不改派的理由。
func TestSeedanceRouteForwardsSupportedResolution(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	cases := []struct {
		name    string
		body    string
		wantRes string
	}{
		{
			"metadata.resolution 1080P",
			`{"model":"seedance2.5-特惠","seconds":25,"metadata":{"resolution":"1080P"},"prompt":"a cat"}`,
			"1080p",
		},
		{
			"顶层 resolution 720P",
			`{"model":"seedance2.5-特惠","seconds":25,"resolution":"720P","prompt":"a cat"}`,
			"720p",
		},
		{
			"顶层 size 当分辨率 720P",
			`{"model":"seedance2.5-特惠","seconds":25,"size":"720P","prompt":"a cat"}`,
			"720p",
		},
		{
			"metadata.resolution 480P",
			`{"model":"seedance2.5-特惠","seconds":25,"metadata":{"resolution":"480P"},"prompt":"a cat"}`,
			"480p",
		},
		{
			"像素尺寸 1920x1080",
			`{"model":"seedance2.5-特惠","seconds":25,"metadata":{"resolution":"1920x1080"},"prompt":"a cat"}`,
			"1080p",
		},
		{
			"像素尺寸 1280x720",
			`{"model":"seedance2.5-特惠","seconds":25,"metadata":{"resolution":"1280x720"},"prompt":"a cat"}`,
			"720p",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, originModel := runRoute(t, tc.body)
			out := decodeBody(t, seen)

			require.Equal(t, testTargetModel, out["model"])
			require.Equal(t, tc.wantRes, out["resolution"])
			// metadata / size 必须整块摘掉：漫屋不读 metadata，size 会被当比例 400。
			_, hasMetadata := out["metadata"]
			assert.False(t, hasMetadata, "metadata 应被摘掉")
			_, hasSize := out["size"]
			assert.False(t, hasSize, "size 应被摘掉（漫屋把它当比例读）")
			assert.Equal(t, testSourceModel, originModel)
		})
	}
}

// ─────────────────────────────────────────────────────────────
// 规则二：9:16 → 心笔 480P 内链（2026-11-11 生产配置）
//
// ratio_target_model = wan3.0-720p-特惠-内链 时，请求体必须按**心笔兼容口**
// 的契约改写，不能沿用 wan3 官网那套（metadata.media 会被静默丢图）。
// ─────────────────────────────────────────────────────────────

const testXinbiRatioTargetModel = "wan3.0-720p-特惠-内链"

func TestRatioRuleRoutesToXinbiTehuiTarget(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat",` +
		`"metadata":{"ratio":"9:16","media":[{"type":"reference_image","url":"https://example.com/a.png"}]}}`
	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)

	require.Equal(t, testXinbiRatioTargetModel, out["model"], "9:16 必须直连心笔 480P 内链")
	assert.Equal(t, testSourceModel, originModel, "计费原点仍是客户请求的模型")
	assert.Equal(t, "10", out["seconds"], "按秒链路必须保留客户请求时长，不能改成 30")
	metadata, ok := out["metadata"].(map[string]interface{})
	require.True(t, ok, "心笔只认 metadata.ratio")
	assert.Equal(t, "9:16", metadata["ratio"])
	images, ok := out["images"].([]interface{})
	require.True(t, ok, "参考图必须落到顶层 images[]（TaskSubmitReq 能解析；reference_images 会被静默丢弃）")
	assert.Equal(t, []interface{}{"https://example.com/a.png"}, images)
	_, hasMedia := out["media"]
	assert.False(t, hasMedia, "顶层 media 必须摘掉，否则参考图会被重复投递")
}

func TestRatioRuleXinbiKeepsNonTierSeconds(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":7,"prompt":"a cat","ratio":"9:16"}`
	seen, _ := runRoute(t, body)
	out := decodeBody(t, seen)

	require.Equal(t, testXinbiRatioTargetModel, out["model"])
	assert.Equal(t, "7", out["seconds"], "7 秒原样透传，不吸附档位")
}

func TestRatioRuleXinbiSkipsWhenChannelUnavailable(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")
	stubChannelAvailable(t, false)

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","metadata":{"ratio":"9:16"}}`
	seen, originModel := runRouteRaw(t, body)

	assert.Equal(t, body, seen, "目标链路没有可用渠道时不该改派")
	assert.Empty(t, originModel)
}

func TestRatioRuleXinbiSkipsFrameMedia(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","metadata":{"ratio":"9:16"},` +
		`"media":[{"type":"first_frame","url":"https://example.com/first.png"}]}`
	seen, originModel := runRoute(t, body)

	assert.Equal(t, body, seen, "心笔兼容口不收首尾帧，必须留在源链路")
	assert.Empty(t, originModel)
}

func TestRatioRuleXinbiSkipsReferenceVideo(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","metadata":{"ratio":"9:16"},` +
		`"media":[{"type":"reference_video","url":"https://example.com/ref.mp4"}]}`
	seen, _ := runRoute(t, body)

	assert.Equal(t, body, seen, "心笔兼容口只收参考图，带参考视频不改派")
}

func TestRatioRuleXinbiSkipsNonTriggerRatio(t *testing.T) {
	enableBothRules(t, testTargetModel, 20, testXinbiRatioTargetModel, "9:16")

	body := `{"model":"seedance2.5-特惠","seconds":10,"prompt":"a cat","metadata":{"ratio":"16:9"}}`
	seen, originModel := runRoute(t, body)

	assert.Equal(t, body, seen, "非触发比例不该改派")
	assert.Empty(t, originModel)
}

// ─────────────────────────────────────────────────────────────
// 按源模型的比例改派开关（2026-11-11）：只取消 seedance2.5-特惠 → 心笔
// ─────────────────────────────────────────────────────────────

// enableRatioRuleWithSwitch 打开「促销族双源 + 时长 + 比例（心笔内链）」，
// 并把 switchedOffSource 的比例改派单独关掉。
//
// trigger_seconds 抬到 40 而不是常规的 20：seedance2.5-活动 是按次固定 30 秒档，
// 没传时长会被归一成 30 —— 阈值必须高于 30，否则规则一（30 > 20）会抢在
// 规则二前面命中，活动版的「比例仍改派」就测不到了。
func enableRatioRuleWithSwitch(t *testing.T, switchedOffSource string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":            true,
		"source_models":      []string{testSourceModel, "seedance2.5-活动"},
		"target_model":       testTargetModel,
		"trigger_seconds":    40,
		"ratio_target_model": testXinbiRatioTargetModel,
		"trigger_ratio":      "9:16",
		"source_overrides": map[string]interface{}{
			switchedOffSource: map[string]interface{}{
				"ratio_rule_enabled": false,
			},
		},
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

// 母本的比例改派被关掉后，9:16 请求必须逐字节原样放行 —— 不改派心笔。
func TestRatioRuleSwitchSkipsRerouteForSwitchedOffSource(t *testing.T) {
	enableRatioRuleWithSwitch(t, testSourceModel)

	body := `{"model":"seedance2.5-特惠","seconds":"15","metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, originModel := runRoute(t, body)

	assert.Equal(t, body, seen,
		"seedance2.5-特惠 的比例改派已取消，请求体必须逐字节不变")
	assert.Empty(t, originModel, "没有发生改派，不该记录改派原点")
}

// 只关母本：活动版的 9:16 仍要改派到心笔内链（别的模型路由到心笔的不变）。
func TestRatioRuleSwitchKeepsRerouteForOtherSource(t *testing.T) {
	enableRatioRuleWithSwitch(t, testSourceModel)

	// 活动版是按次固定档：不传时长会被归一成 30 秒，再进入规则判定。
	body := `{"model":"seedance2.5-活动","metadata":{"ratio":"9:16"},"prompt":"a cat"}`
	seen, originModel := runRoute(t, body)
	out := decodeBody(t, seen)

	assert.Equal(t, testXinbiRatioTargetModel, out["model"],
		"活动版没被关开关，9:16 必须照旧改派到心笔内链")
	assert.Equal(t, "seedance2.5-活动", originModel)
}
