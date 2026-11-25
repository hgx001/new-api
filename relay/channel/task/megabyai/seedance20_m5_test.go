package megabyai

// ════════════════════════════════════════════════════════════════════════════
// seedance-2.0-fast-m5 / seedance-2.0-m5（2026-11-24 上架）
// ════════════════════════════════════════════════════════════════════════════
//
// 上游目录原文（两条一致）：「9图3视频3音频，4-15秒，不卡脸，支持16:9、9:16、1:1、
// 4:3、3:4、21:9。参考视频不计费。字符上限50000…视频最大支持总共15s、音频总共15s」。
//
// 上游价目（按秒、分辨率档）：
//
//	seedance-2.0-fast-m5：480p ¥0.16/秒、720p ¥0.35/秒
//	seedance-2.0-m5     ：480p ¥0.21/秒、720p ¥0.44/秒
//
// 本站价 = 上游各档 + ¥0.04/秒（老板口径「各个分辨率加 4 分钱」）：
//
//	fast：480p ¥0.20/秒、720p ¥0.39/秒（倍率 1.95）
//	标准：480p ¥0.25/秒、720p ¥0.48/秒（倍率 1.92）
//
// 本文件守四件会直接伤钱 / 伤客户预期的事：
//
//  1. **计费必须同时给 seconds 与 size** —— 只给 size 就按 1 秒收钱（少收 3/4）；
//     反过来给按次档 seconds 会把一口价乘成 N 倍（超收）。
//  2. **没有 1080p 档** —— 上游价目只有 480p/720p，传 1080p 必须本地 400。
//  3. **时长 4–15 秒且必填** —— 按秒计费没有默认值：猜一个就会错价。
//  4. **素材上限 9/3/3** —— 与 T3（30/10/10）不同，沿用旧上限会放进上游必拒的请求。

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 能力表：两档分辨率、两档倍率、素材 9/3/3、时长 4–15。
func TestM5Capabilities(t *testing.T) {
	for _, model := range []string{Seedance20FastM5Model, Seedance20M5Model} {
		caps := capabilityFor(model)
		assert.Equal(t, []string{"480p", "720p"}, caps.Resolutions, "%s 只有两档分辨率", model)
		assert.InDelta(t, 1.0, caps.SizeRatios["480p"], 1e-9, "%s 的 480p 是基准档", model)
		assert.Equal(t, 9, caps.MaxImages, model)
		assert.Equal(t, 3, caps.MaxVideos, model)
		assert.Equal(t, 3, caps.MaxAudios, model)
		assert.Equal(t, 4, caps.MinSeconds, model)
		assert.Equal(t, 15, caps.MaxSeconds, model)
	}

	// 两档不同价：720p 倍率必须各自对齐「上游价 +0.04」。
	assert.InDelta(t, 1.95, capabilityFor(Seedance20FastM5Model).SizeRatios["720p"], 1e-9,
		"fast: 720p ¥0.39 / 480p ¥0.20 = 1.95")
	assert.InDelta(t, 1.92, capabilityFor(Seedance20M5Model).SizeRatios["720p"], 1e-9,
		"标准: 720p ¥0.48 / 480p ¥0.25 = 1.92")
}

// 计费：按秒 + 分辨率档，两者必须同时出现。
func TestM5EstimateBillingHasSecondsAndSize(t *testing.T) {
	adaptor := &TaskAdaptor{}

	cases := []struct {
		model    string
		seconds  int
		res      string
		wantSize float64
	}{
		{Seedance20FastM5Model, 4, "480p", 1.0},
		{Seedance20FastM5Model, 4, "720p", 1.95},
		{Seedance20M5Model, 4, "480p", 1.0},
		{Seedance20M5Model, 15, "720p", 1.92},
	}
	for _, tc := range cases {
		info := &relaycommon.RelayInfo{OriginModelName: tc.model}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set(wan30ResolutionContextKey, tc.res)
		c.Set(wan30DurationContextKey, tc.seconds)

		got := adaptor.EstimateBilling(c, info)
		require.Len(t, got, 2, "%s：按秒档必须同时给 seconds 与 size", tc.model)
		assert.Equal(t, float64(tc.seconds), got["seconds"],
			"%s：漏给 seconds 就会按 1 秒收费", tc.model)
		assert.InDelta(t, tc.wantSize, got["size"], 1e-9, tc.model)
	}
}

// 缺时长（context 里没有）→ 不给任何倍率，宁可让调用方按缺省处理，
// 也不能凭空给一个 seconds=1 或 seconds=N。
func TestM5EstimateBillingWithoutDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: Seedance20M5Model}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(wan30ResolutionContextKey, "480p")
	got := adaptor.EstimateBilling(c, info)

	require.Len(t, got, 1)
	assert.NotContains(t, got, "seconds")
	assert.InDelta(t, 1.0, got["size"], 1e-9)
}

// 按次档（wan-3.0）不得被 m5 的改动带出 seconds。
func TestPerUseModelsKeepSizeOnly(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: wan30Model}

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set(wan30ResolutionContextKey, "720p")
	c.Set(wan30DurationContextKey, 10) // 即使 context 里有秒数也不能带出去
	got := adaptor.EstimateBilling(c, info)

	require.Len(t, got, 1, "wan-3.0 是按次价，带 seconds 会超收 10 倍")
	assert.NotContains(t, got, "seconds")
}

// 分辨率：480p/720p 通过，1080p 显式 400（上游根本没有这一档）。
func TestM5ResolutionWhitelist(t *testing.T) {
	for _, model := range []string{Seedance20FastM5Model, Seedance20M5Model} {
		caps := capabilityFor(model)
		for _, ok := range []string{"480p", "720p", "720P"} {
			_, err := resolveMegabyaiResolution(relaycommon.TaskSubmitReq{Resolution: ok}, caps)
			require.NoError(t, err, "%s 分辨率 %q 应通过", model, ok)
		}
		_, err := resolveMegabyaiResolution(relaycommon.TaskSubmitReq{Resolution: "1080p"}, caps)
		require.Error(t, err, "%s 没有 1080p 档，必须 400 而不是静默降到 720p", model)
		assert.Contains(t, err.Error(), "1080p")
	}
}

// 时长 4–15 秒：区间内通过，越界 400，未给 → nil（由 adaptor 在入口处拦「必填」）。
func TestM5DurationRange(t *testing.T) {
	caps := capabilityFor(Seedance20M5Model)
	for _, seconds := range []int{4, 8, 15} {
		duration, err := resolveMegabyaiDuration(relaycommon.TaskSubmitReq{Duration: seconds}, caps)
		require.NoError(t, err)
		require.NotNil(t, duration)
		assert.Equal(t, seconds, *duration)
	}
	for _, seconds := range []int{1, 3, 16, 30} {
		_, err := resolveMegabyaiDuration(relaycommon.TaskSubmitReq{Duration: seconds}, caps)
		require.Error(t, err, "%d 秒越界必须 400（不能截断成 4/15 秒）", seconds)
		assert.Contains(t, err.Error(), "4-15")
	}
}

// 入口校验：按秒档缺时长必须 400（不是静默按 1 秒计费）。
func TestM5RequiresDurationAtValidate(t *testing.T) {
	for _, model := range []string{Seedance20FastM5Model, Seedance20M5Model} {
		result := runChainWithModel(t, `{"model":"`+model+`","prompt":"x","resolution":"480p"}`, model, model)
		require.NotNil(t, result.taskErr, "%s 未给时长必须拒", model)
		assert.Equal(t, "invalid_duration", result.taskErr.Code)
		assert.Contains(t, result.taskErr.Message, "duration is required")
	}
}

// 入口校验：4 秒 480p 全参（图 + 视频 + 音频）必须通过，且把解析结果写进 context、
// 上游提交体与计费倍率三处一致。
func TestM5ValidateFullParams(t *testing.T) {
	for _, model := range []string{Seedance20FastM5Model, Seedance20M5Model} {
		raw := `{"model":"` + model + `","prompt":"x","duration":4,"resolution":"480p","ratio":"9:16",` +
			`"images":["https://e.com/a.png"],` +
			`"media":[{"type":"reference_video","url":"https://e.com/v.mp4"},` +
			`{"type":"reference_audio","url":"https://e.com/a.mp3"}]}`
		result := runChainWithModel(t, raw, model, model)
		require.Nil(t, result.taskErr, "%s 全参 4 秒 480p 应通过", model)

		// 上游提交体：参数真的都下发了，且模型名原样（不是别名）。
		assert.Equal(t, model, result.body["model"])
		assert.Equal(t, float64(4), result.body["duration"])
		assert.Equal(t, "480p", result.body["resolution"])
		assert.Equal(t, "9:16", result.body["ratio"])
		assert.Len(t, result.body["referenceImages"], 1)
		assert.Len(t, result.body["referenceVideos"], 1)
		assert.Len(t, result.body["referenceAudios"], 1)

		// 计费：4 秒 + 480p 基档（漏 seconds 就只收 1 秒的钱）。
		require.Len(t, result.ratios, 2)
		assert.Equal(t, float64(4), result.ratios["seconds"])
		assert.InDelta(t, 1.0, result.ratios["size"], 1e-9)
	}
}

// 素材上限 9/3/3：与 T3 的 30/10/10 不同，不能共用一份上限。
func TestM5MediaLimits(t *testing.T) {
	caps := capabilityFor(Seedance20FastM5Model)

	build := func(n int, kind string) relaycommon.TaskSubmitReq {
		items := make([]relaycommon.TaskMedia, 0, n)
		for i := 0; i < n; i++ {
			items = append(items, relaycommon.TaskMedia{Type: kind, URL: "https://e.com/" + itoaTest(i) + ".bin"})
		}
		return relaycommon.TaskSubmitReq{Media: items}
	}

	if _, _, _, err := buildMegabyaiMedia(build(9, "reference_image"), caps); err != nil {
		t.Fatalf("9 张参考图是上限，应通过: %v", err)
	}
	_, _, _, err := buildMegabyaiMedia(build(10, "reference_image"), caps)
	require.Error(t, err, "10 张参考图超出 m5 上限 9，必须 400")
	assert.Contains(t, err.Error(), "9")

	if _, _, _, err := buildMegabyaiMedia(build(3, "reference_video"), caps); err != nil {
		t.Fatalf("3 个参考视频是上限，应通过: %v", err)
	}
	_, _, _, err = buildMegabyaiMedia(build(4, "reference_video"), caps)
	require.Error(t, err, "4 个参考视频超出 m5 上限 3")

	_, _, _, err = buildMegabyaiMedia(build(4, "reference_audio"), caps)
	require.Error(t, err, "4 段参考音频超出 m5 上限 3")
}

// 公开名到上游名：两条都不是别名，必须原样下发（上游就是这个名字）。
func TestM5UpstreamNameIsIdentity(t *testing.T) {
	for _, model := range []string{Seedance20FastM5Model, Seedance20M5Model} {
		require.True(t, megabyaiPerSecondModels[model], "%s 必须登记为按秒档", model)
		require.True(t, megabyaiUnifiedContractModels[model], "%s 必须走统一参数契约", model)
		assert.Equal(t, model, megabyaiUpstreamModels[model])
	}
}

// T3 已下架：不再出现在渠道模型表，但代码能力表仍保留（在途单兼容）。
func TestT3RetiredFromModelList(t *testing.T) {
	for _, model := range (&TaskAdaptor{}).GetModelList() {
		require.NotEqual(t, T3Seedance25Model, model, "T3-seedance-2.5 已于 2026-11-24 下架")
	}
	require.True(t, megabyaiUnifiedContractModels[T3Seedance25Model],
		"已下架的模型仍要走统一参数契约：在途单的轮询/取片不能因为下架改道")
}

// ── 对客响应体不得泄露上游货源 / 成本（2026-11-24 真机发现）──
//
// 上游（newapi.megabyai.cc）的轮询回执里带着：
//
//	url / video_url / metadata.final_video_url / metadata.origin_video_url —— R2 直链，
//	  路径里带 hailuo-video，直接报出真实货源；
//	billing_amount / billing_quota / billing_currency —— 我们的**进货价**；
//	metadata.upstream_task_id —— 上游任务号。
//
// 未覆盖 ConvertToOpenAIVideo 时这些会经 sora 的「透传上游回执」原样发给客户。
func TestConvertToOpenAIVideoHidesUpstream(t *testing.T) {
	raw := `{"billing_amount":0.64,"billing_currency":"CNY","billing_quota":320000,"billing_status":"settled",` +
		`"completed_at":1795571684,"created_at":1795571334,"generated_seconds":4,"reference_video_seconds":2,` +
		`"id":"task_x","task_id":"task_x","model":"seedance-2.0-fast-m5","object":"video","progress":100,` +
		`"status":"completed","seconds":"4","size":"480p",` +
		`"url":"https://abc.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_1.m4v?X-Amz-Signature=x",` +
		`"video_url":"https://abc.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_1.m4v?X-Amz-Signature=x",` +
		`"metadata":{"final_video_url":"https://abc.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_1.m4v",` +
		`"origin_video_url":"https://abc.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_1.m4v",` +
		`"upstream_task_id":"vid_1"}}`

	task := &model.Task{
		TaskID: "task_x",
		Status: model.TaskStatusSuccess,
		Data:   []byte(raw),
	}
	task.Properties.OriginModelName = Seedance20FastM5Model
	task.PrivateData.ResultURL = "https://our.example/v1/videos/task_x/content"

	adaptor := &TaskAdaptor{}
	encoded, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	body := string(encoded)

	for _, leak := range []string{
		"r2.cloudflarestorage.com", "hailuo-video", // 上游存储域名 + 货源目录
		"billing_amount", "billing_quota", "billing_currency", // 上游成本
		"upstream_task_id", "vid_1", // 上游任务号
	} {
		assert.NotContains(t, body, leak, "对客响应体不得出现 %q", leak)
	}

	var video map[string]any
	require.NoError(t, common.Unmarshal(encoded, &video))
	assert.Equal(t, "task_x", video["id"])
	assert.Equal(t, Seedance20FastM5Model, video["model"])
	assert.Equal(t, "completed", video["status"])
	assert.Equal(t, "4", video["seconds"], "seconds/size 是客户可见规格，可以保留")
	meta, ok := video["metadata"].(map[string]any)
	require.True(t, ok, "成功任务要给 metadata.url（下载地址）")
	assert.Contains(t, meta["url"], "/v1/videos/task_x/content",
		"下载地址必须是本站代理（前缀取 ServerAddress，测试环境是 localhost）")
	assert.NotContains(t, meta["url"], "r2.cloudflarestorage.com")
}

// 失败任务：不得在响应体里回显上游原文（含上游地址），只在 error.message 里给出脱敏原因。
func TestConvertToOpenAIVideoFailureRedacted(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_y",
		Status:     model.TaskStatusFailure,
		FailReason: "上游 megabyai 拒绝了请求：https://abc.r2.cloudflarestorage.com/hailuo-video/x.m4v",
	}
	task.Properties.OriginModelName = Seedance20M5Model

	adaptor := &TaskAdaptor{}
	encoded, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	body := string(encoded)
	assert.NotContains(t, body, "r2.cloudflarestorage.com")
	assert.NotContains(t, body, "megabyai")
	assert.NotContains(t, body, "hailuo-video")
	assert.Contains(t, body, "failed")
}
