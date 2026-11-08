package middleware

// 规则五（故障转移）的测试。
//
// 与其它规则测试的核心断言同一条：**要么路由成功且请求体被正确改写，
// 要么请求体逐字节不变**。中间件永远不能「部分改写」。
//
// 但规则五多一组断言维度：它有**两段降级**（源→备胎、促销族→豆包→备胎），
// 每段都有各自的「源不可用 / 备胎不可用 / 能力不兼容」分支，
// 组合起来是一张判定矩阵。所以这里的用例按矩阵逐格覆盖。

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

const (
	// failoverSourceModel 是规则五的源模型（豆包官网链路）。
	failoverSourceModel = "seedance-2-5"
	// failoverTargetModel 常量在 video_route_failover.go 里定义，这里只做断言。
	failoverBackupModel = "wan3.0-班车"
)

// enableFailover 打开规则五（故障转移）的开关与源/目标模型。
func enableFailover(t *testing.T, source, target string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":               true,
		"failover_enabled":      true,
		"failover_source_model": source,
		"failover_target_model": target,
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

// stubChannelAvailableByModel 按模型名分别控制「是否有可用渠道」。
//
// 规则五的判定矩阵需要区分「源链路可用」与「备胎可用」两种情况，
// 所以不能用只返回单一 bool 的 stubChannelAvailable。
func stubChannelAvailableByModel(t *testing.T, available map[string]bool) {
	t.Helper()
	previous := channelAvailabilityProbe
	channelAvailabilityProbe = func(_ *gin.Context, modelName string) bool {
		return available[modelName]
	}
	t.Cleanup(func() { channelAvailabilityProbe = previous })
}

// runFailover 跑一遍路由，返回下游看到的请求体与 RouteOriginModel。
func runFailover(t *testing.T, body string) (string, string) {
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

// ───────────────────────── 基础判定 ─────────────────────────

// 规则五未配置（开关关闭）时，一个字节都不能动。
func TestFailoverDisabledKeepsBodyByteIdentical(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
	if err := seedanceroute_setting.UpdateFromJSONString(`{"enabled":false}`); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`
	seen, originModel := runFailover(t, body)
	if seen != body {
		t.Fatalf("body changed while failover disabled:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("RouteOriginModel() = %q, want empty", originModel)
	}
}

// 源模型不是规则五的源时，不改派（哪怕源链路「不可用」）。
func TestFailoverIgnoresNonSourceModels(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
		"gpt-4o":            false,
	})

	body := `{"model":"gpt-4o","seconds":10,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("非源模型不该被改派:\n got %s\nwant %s", seen, body)
	}
}

// 源链路可用时，不改派（故障转移只在不可用时触发）。
func TestFailoverKeepsBodyWhenSourceIsAvailable(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("源链路可用时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 源链路不可用 + 备胎有渠道 → 改派到备胎。
func TestFailoverRoutesToBackupWhenSourceUnavailable(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, originModel := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`)
	if originModel != failoverSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, failoverSourceModel)
	}
	assertModelRouted(t, seen, failoverBackupModel)
}

// 源链路不可用 + 备胎也无可用渠道 → 不改派（放行原请求）。
//
// 改派过去只是把「额度用完」换成「没有可用渠道」，而且重试不会退回源模型。
func TestFailoverSkipsWhenBackupHasNoChannel(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: false,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`
	seen, originModel := runFailover(t, body)
	if seen != body {
		t.Fatalf("备胎没渠道时不该改派:\n got %s\nwant %s", seen, body)
	}
	if originModel != "" {
		t.Fatalf("备胎没渠道时不该写 RouteOriginModel，却写了 %q", originModel)
	}
}

// ───────────────────────── 额度探针触发的不可用 ─────────────────────────

// 额度探针标记「源链路额度已用完」→ 改派到备胎。
//
// 这是故障转移最主要的使用场景：豆包额度按天计，用完之后必须自动降级，
// 否则「整个 seedance-2-5 档位全挂」。
func TestFailoverRoutesWhenQuotaProbeMarksSourceUnavailable(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true, // 渠道还活着，只是额度用完
		failoverBackupModel: true,
	})

	// 写入「额度已用完」的标记（截止到 1 小时后）。
	until := timeNowUnix() + 3600
	marked := seedanceroute_setting.WithFailoverUnavailable(until, seedanceroute_setting.ReasonQuotaExhausted)
	encoded, err := common.Marshal(marked)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	seen, originModel := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`)
	if originModel != failoverSourceModel {
		t.Fatalf("额度标记期间应改派，RouteOriginModel() = %q", originModel)
	}
	assertModelRouted(t, seen, failoverBackupModel)
}

// 标记已过期 → 不改派（额度已恢复）。
func TestFailoverDoesNotRouteAfterQuotaMarkerExpires(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true,
		failoverBackupModel: true,
	})

	// 截止时间在过去 = 已过期。
	marked := seedanceroute_setting.WithFailoverUnavailable(timeNowUnix()-60, seedanceroute_setting.ReasonQuotaExhausted)
	encoded, err := common.Marshal(marked)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("标记过期后不该改派:\n got %s\nwant %s", seen, body)
	}
}

// ───────────────────────── 能力挡板 ─────────────────────────

// 时长超出备胎范围（2–30 秒）→ 不改派。
func TestFailoverSkipsWhenSecondsOutOfBackupRange(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":60,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("时长超范围时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 提示词短于备胎下限（5 字符）→ 不改派。
func TestFailoverSkipsWhenPromptTooShort(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"ab"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("提示词过短时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 参考图超过备胎上限（10 张）→ 不改派。
func TestFailoverSkipsWhenTooManyReferenceImages(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	images := `["http://a/1.png","http://a/2.png","http://a/3.png","http://a/4.png","http://a/5.png","http://a/6.png","http://a/7.png","http://a/8.png","http://a/9.png","http://a/10.png","http://a/11.png"]`
	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","images":` + images + `}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("参考图超上限时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 比例不是源链路支持的档位 → 不改派。
//
// 不改派而不是「随便给个档位」：源链路会 400，改派过去若不写比例，
// 备胎链路按缺省 16:9 出片 = 静默改了客户要的画幅。
func TestFailoverSkipsWhenRatioNotSupportedBySource(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"5:4"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("非法比例时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// ───────────────────────── 请求体改写 ─────────────────────────

// 改派后的请求体必须符合备胎链路的契约：
// 模型换掉、参考图进 media[]、比例进 metadata.ratio、时长写成 seconds 字符串，
// 且**源链路不读的那些字段不能带过去**（media/audio/videos 等）。
func TestFailoverRewritesBodyForBackupContract(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat",`+
		`"images":["http://a/1.png","http://a/2.png"],"ratio":"9:16"}`)

	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	if out["model"] != failoverBackupModel {
		t.Fatalf("model = %v, want %q", out["model"], failoverBackupModel)
	}
	if out["seconds"] != "10" {
		t.Fatalf("seconds = %v (%T), want \"10\"（字符串）", out["seconds"], out["seconds"])
	}
	if out["prompt"] != "a cat" {
		t.Fatalf("prompt = %v, want \"a cat\"", out["prompt"])
	}
	// 源链路的参考图别名字段不能留在改写后的 body 里（备胎会把它们当未知字段）。
	if _, exists := out["images"]; exists {
		t.Fatalf("改写后的 body 仍带 images 字段: %s", seen)
	}
	// 媒体必须转成 media[]。
	media, ok := out["media"].([]interface{})
	if !ok || len(media) != 2 {
		t.Fatalf("media = %v, want 2 项", out["media"])
	}
	first, _ := media[0].(map[string]interface{})
	if first["type"] != "reference_image" || first["url"] != "http://a/1.png" {
		t.Fatalf("media[0] = %v", first)
	}
	// 比例进 metadata.ratio。
	metadata, ok := out["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata 缺失: %s", seen)
	}
	if metadata["ratio"] != "9:16" {
		t.Fatalf("metadata.ratio = %v, want \"9:16\"", metadata["ratio"])
	}
}

// 源链路「不读」的字段（media / audio / audios / videos / video）不能被搬过去。
//
// 搬过去等于替客户加需求：备胎链路会把它们收成参考音频/参考视频，
// 而「参考视频时长 + 生成时长 ≤ 30 秒」会让请求直接失败。
func TestFailoverDropsFieldsSourceChainIgnores(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat",`+
		`"audio":"http://a/s.mp3","audios":["http://a/s2.mp3"],`+
		`"videos":["http://a/v.mp4"],"video":"http://a/v2.mp4",`+
		`"media":[{"type":"reference_audio","url":"http://a/m.mp3"}]}`)

	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	for _, key := range []string{"audio", "audios", "videos", "video"} {
		if _, exists := out[key]; exists {
			t.Fatalf("改写后的 body 仍带 %s 字段: %s", key, seen)
		}
	}
	// media 里不该出现 reference_audio（源链路不收它）。
	media, _ := out["media"].([]interface{})
	for _, item := range media {
		entry, _ := item.(map[string]interface{})
		if entry["type"] == "reference_audio" {
			t.Fatalf("改写后的 media 仍带参考音频: %s", seen)
		}
	}
}

// 参考图只搬「第一个有值的字段」，不合并所有别名。
//
// 与豆包链路的 resolveReferenceImages 同口径：豆包只认第一个有值的字段，
// 合并所有别名等于替客户多传图。
func TestFailoverTakesOnlyFirstNonEmptyImageField(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat",`+
		`"input_reference":"http://a/first.png","images":["http://a/second.png"]}`)

	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	media, _ := out["media"].([]interface{})
	if len(media) != 1 {
		t.Fatalf("media = %v, want 1 项（只搬第一个有值的字段）", out["media"])
	}
	entry, _ := media[0].(map[string]interface{})
	if entry["url"] != "http://a/first.png" {
		t.Fatalf("media[0].url = %v, want http://a/first.png", entry["url"])
	}
}

// 豆包的 auto 档要映射成班车的 adaptive。
func TestFailoverMapsAutoRatioToAdaptive(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","ratio":"auto"}`)

	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	metadata, _ := out["metadata"].(map[string]interface{})
	if metadata["ratio"] != "adaptive" {
		t.Fatalf("metadata.ratio = %v, want \"adaptive\"", metadata["ratio"])
	}
}

// 时长取源链路口径：seconds 优先于 duration，缺省 5 秒。
func TestFailoverSecondsPriorityMatchesSourceChain(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	// seconds 与 duration 同时给：豆包取 seconds。
	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":15,"duration":30,"prompt":"a cat"}`)
	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	if out["seconds"] != "15" {
		t.Fatalf("seconds = %v, want \"15\"（seconds 优先于 duration）", out["seconds"])
	}
}

// 客户明确要的分辨率要透传给备胎（备胎支持 480P/720P/1080P 且三档同价）。
func TestFailoverPassesThroughExplicitResolution(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"a cat","resolution":"1080P"}`)
	var out map[string]interface{}
	if err := common.Unmarshal([]byte(seen), &out); err != nil {
		t.Fatalf("unmarshal rewritten body: %v (%s)", err, seen)
	}
	if out["resolution"] != "1080P" {
		t.Fatalf("resolution = %v, want \"1080P\"", out["resolution"])
	}
}

// ───────────────────────── 与规则一的串联（促销族 → 豆包 → 班车）─────────

// 豆包额度用完时，规则一改派到豆包后必须**继续降级**到班车。
//
// 这是规则五与规则一串联的核心场景：seedance-2-5 既是规则五的源、
// 又是规则一的目标。不做第二段降级的话，豆包额度一用完，
// 「整个 30 秒档位全挂」。
func TestFailoverDegradesDoubaoRoutedRequestToBackup(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})

	payload := map[string]interface{}{
		"enabled":               true,
		"source_model":          testSourceModel,
		"target_model":          failoverSourceModel,
		"trigger_seconds":       20,
		"failover_enabled":      true,
		"failover_source_model": failoverSourceModel,
		"failover_target_model": failoverBackupModel,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	// 豆包渠道还活着（只是额度用完），备胎有渠道。
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true,
		failoverBackupModel: true,
	})

	until := timeNowUnix() + 3600
	marked := seedanceroute_setting.WithFailoverUnavailable(until, seedanceroute_setting.ReasonQuotaExhausted)
	markedEncoded, err := common.Marshal(marked)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(markedEncoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	seen, originModel := runFailover(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`)

	// 最终执行模型必须是备胎。
	assertModelRouted(t, seen, failoverBackupModel)
	// ⚠️ 计费原点必须是**客户最初请求的促销族模型**，不能是豆包。
	// 客户买的是 seedance2.5-特惠（¥0.20/秒），降级到班车后仍按它计费。
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q（计费原点必须是客户原始请求的模型）",
			originModel, testSourceModel)
	}
}

// 豆包可用时，规则一改派到豆包就结束（不该再降级到班车）。
func TestFailoverDoesNotDegradeWhenDoubaoAvailable(t *testing.T) {
	previous := seedanceroute_setting.Get()
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})

	payload := map[string]interface{}{
		"enabled":               true,
		"source_model":          testSourceModel,
		"target_model":          failoverSourceModel,
		"trigger_seconds":       20,
		"failover_enabled":      true,
		"failover_source_model": failoverSourceModel,
		"failover_target_model": failoverBackupModel,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}

	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: true,
		failoverBackupModel: true,
	})

	seen, originModel := runFailover(t, `{"model":"seedance2.5-特惠","seconds":30,"prompt":"a cat"}`)
	assertModelRouted(t, seen, failoverSourceModel)
	if originModel != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", originModel, testSourceModel)
	}
}

// ───────────────────────── 边界：提示词长度按字节口径 ─────────────────────────

// 提示词长度必须按**字节**判，与班车 adaptor 的 len(prompt) 完全一致。
//
// 为什么这条重要：班车 adaptor 用 len(prompt)（Go 字符串字节数）判 5～20000，
// 而改派侧此前用 len([]rune(prompt))（字符数）。中文提示词每字 3 字节，
// 字符数口径会放行 3 倍长的提示词 —— 改派过去必被上游 400，
// 而留在源链路至少能拿到源链路的明确报错。
func TestFailoverPromptLengthUsesByteSemantics(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	// 7000 个中文字 = 21000 字节 > 20000（班车上限），
	// 但只有 7000 个字符 < 20000（字符口径会误放行）。
	prompt := strings.Repeat("猫", 7000)
	body := `{"model":"seedance-2-5","seconds":10,"prompt":"` + prompt + `"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("提示词超字节上限时不该改派（字符口径会误放行）:\n got %s\nwant %s", seen[:80], body[:80])
	}
}

// 反向：字节数没超上限时**必须**改派（不能因为字符数换算而误拦）。
func TestFailoverRoutesWhenPromptWithinByteLimit(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	// 3000 个中文字 = 9000 字节，落在 5～20000 区间内。
	prompt := strings.Repeat("猫", 3000)
	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":10,"prompt":"`+prompt+`"}`)
	assertModelRouted(t, seen, failoverBackupModel)
}

// ───────────────────────── 边界：非法时长 fail-loud ─────────────────────────

// 时长是小数时**不改派**：源链路 manwu 对非法值是 fail-loud 的
// （parseFlexibleInt 会报 "is not a valid integer"），改派侧若把 7.5 截断成 7
// 搬过去，就把「源链路会报错的请求」变成了「备胎按 7 秒跑」的一单 ——
// 静默改了客户要的时长。
func TestFailoverSkipsWhenSecondsIsFractional(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":7.5,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("时长为小数时不该改派（源链路会直接报错）:\n got %s\nwant %s", seen, body)
	}
}

// 时长是非数字字符串时同样不改派。
func TestFailoverSkipsWhenSecondsIsNotNumeric(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":"abc","prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("时长非法时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// duration 字段带小数时同样不改派（seconds 缺省时回退读 duration）。
func TestFailoverSkipsWhenDurationIsFractional(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","duration":12.5,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("duration 为小数时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// ───────────────────────── 边界：时长白名单按源链路口径 ─────────────────────────

// 时长必须先过**源链路**（豆包）的白名单 5/10/15/30，再过备胎的 2–30 区间。
//
// 为什么这条重要：客户传 seconds=7 时，
//   - 源链路可用 → 走 manwu → checkDoubaoDuration 报「must be one of 5/10/15/30」
//   - 源链路挂了 → 若改派侧只判 2–30 区间 → 班车按 7 秒真跑出片
//
// 后者就是「源链路本该拒绝、改派后却悄悄接受」的静默语义改写。
func TestFailoverSkipsWhenSecondsNotInSourceChainTiers(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	// 7 秒落在班车能吃的区间内，但不在豆包白名单里。
	body := `{"model":"seedance-2-5","seconds":7,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("时长不在源链路档位内时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// 白名单里的时长必须能正常改派（防止上面的闸口把合法单子也拦了）。
func TestFailoverRoutesWhenSecondsInSourceChainTiers(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	for _, seconds := range []string{"5", "10", "15", "30"} {
		seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":`+seconds+`,"prompt":"a cat"}`)
		assertModelRouted(t, seen, failoverBackupModel)
	}
}

// duration 字段同样受白名单约束（seconds 缺省时回退读 duration）。
func TestFailoverSkipsWhenDurationNotInSourceChainTiers(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","duration":7,"prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("duration 不在源链路档位内时不该改派:\n got %s\nwant %s", seen, body)
	}
}

// ───────────────────────── 边界：空值字段按 hasValue 同口径 ─────────────────────────

// seconds:null 时，源链路把 null 当「没传」→ 回退 duration → 再回退默认值 5。
// 改派侧必须同口径，否则会比源链路更严（源链路能跑的单子不敢接）。
func TestFailoverTreatsNullSecondsAsAbsent(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	// seconds 是 null、duration 是合法的 10 → 应按 10 秒改派。
	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":null,"duration":10,"prompt":"a cat"}`)
	assertModelRouted(t, seen, failoverBackupModel)
	if !strings.Contains(seen, `"seconds":"10"`) {
		t.Fatalf("seconds:null 时应回退到 duration=10:\n got %s", seen)
	}
}

// seconds:null 且 duration 也缺省 → 用源链路的默认 5 秒。
func TestFailoverTreatsNullSecondsAsDefault(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":null,"prompt":"a cat"}`)
	assertModelRouted(t, seen, failoverBackupModel)
	if !strings.Contains(seen, `"seconds":"5"`) {
		t.Fatalf("seconds:null 且无 duration 时应取默认 5 秒:\n got %s", seen)
	}
}

// 空串同样算「没传」（源链路 hasValue 把 "" 视为无值）。
func TestFailoverTreatsEmptySecondsAsAbsent(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	seen, _ := runFailover(t, `{"model":"seedance-2-5","seconds":"","duration":15,"prompt":"a cat"}`)
	assertModelRouted(t, seen, failoverBackupModel)
	if !strings.Contains(seen, `"seconds":"15"`) {
		t.Fatalf("seconds 为空串时应回退到 duration=15:\n got %s", seen)
	}
}

// 空白字符串**算有值**：源链路 hasValue 只匹配 "" 字面量，
// "  " 会进 parseFlexibleInt → Atoi 失败 → 报「不是合法整数」。
// 改派侧若把 "  " 当「没传」，就比源链路宽松了 —— 同样要拦。
func TestFailoverSkipsWhenSecondsIsBlankString(t *testing.T) {
	enableFailover(t, failoverSourceModel, failoverBackupModel)
	stubChannelAvailableByModel(t, map[string]bool{
		failoverSourceModel: false,
		failoverBackupModel: true,
	})

	body := `{"model":"seedance-2-5","seconds":"  ","prompt":"a cat"}`
	seen, _ := runFailover(t, body)
	if seen != body {
		t.Fatalf("时长是空白字符串时不该改派:\n got %s\nwant %s", seen, body)
	}
}
