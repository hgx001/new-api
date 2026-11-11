package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedance2.5-活动 带参考视频 → 有赞 prime（seedance2.5-特惠，上游 ¥0.15/秒）。
//
// 锁的是「改派边界」：
//   - 该改派却没改派 → 心笔把参考视频秒数并入上游计费，按次售价可能倒挂；
//   - 不该改派却改派 → 有赞对有参考视频的任务硬限制生成 ≤15 秒，客户必然吃 400。

// newHuoDongRefVideoEngine 只挂这条改派规则，不带规则一~五。
func newHuoDongRefVideoEngine(rec *routeRecorder, presetOrigin string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if presetOrigin != "" {
		engine.Use(func(c *gin.Context) {
			c.Set(routeOriginModelKey, presetOrigin)
			c.Next()
		})
	}
	engine.Use(Seedance25HuoDongRefVideoRoute())
	engine.POST("/v1/video/generations", recordRouteBody(rec))
	return engine
}

func runHuoDongRefVideo(t *testing.T, payload string) (map[string]interface{}, string) {
	t.Helper()
	return runHuoDongRefVideoWithOrigin(t, payload, "")
}

func runHuoDongRefVideoWithOrigin(t *testing.T, payload, presetOrigin string) (map[string]interface{}, string) {
	t.Helper()
	stubChannelAvailable(t, true)
	rec := &routeRecorder{}
	engine := newHuoDongRefVideoEngine(rec, presetOrigin)
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return decodeBody(t, rec.body), rec.originModel
}

// mediaEntries 取出请求体顶层 media 数组里的对象条目。
func mediaEntries(t *testing.T, body map[string]interface{}) []map[string]interface{} {
	t.Helper()
	raw, ok := body["media"].([]interface{})
	require.True(t, ok, "media 必须是数组，实际：%#v", body["media"])
	entries := make([]map[string]interface{}, 0, len(raw))
	for _, item := range raw {
		entry, ok := item.(map[string]interface{})
		require.True(t, ok, "media 条目必须是对象，实际：%#v", item)
		entries = append(entries, entry)
	}
	return entries
}

func metadataOf(t *testing.T, body map[string]interface{}) map[string]interface{} {
	t.Helper()
	metadata, ok := body["metadata"].(map[string]interface{})
	require.True(t, ok, "metadata 必须是对象，实际：%#v", body["metadata"])
	return metadata
}

func TestHuoDongRefVideoRouteRewritesToTehui(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","prompt":"a cat","seconds":"8","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"], "应改派到有赞 prime 的对外别名")
	assert.Equal(t, constant.Seedance25HuoDongModel, origin, "计费原点必须保持活动版（¥4.5/条）")
	assert.Equal(t, "8", body["seconds"], "生成秒数必须原样带过去")
	assert.Equal(t, "720P", metadataOf(t, body)["resolution"], "活动版固定 720P，改派后不能静默升档")
}

// 顶层 video / videos 是兼容口字段：有赞 adaptor 不读，必须在改派时并入 media，
// 否则参考视频会被静默丢掉（任务照建、照扣费、成片没有参考素材）。
func TestHuoDongRefVideoRouteNormalizesTopLevelVideos(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","prompt":"a cat","seconds":"8","videos":["https://cdn.example.com/a.mp4"]}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"])
	assert.NotContains(t, body, "videos", "顶层 videos 已并入 media，不能留给有赞 adaptor 忽略")
	entries := mediaEntries(t, body)
	require.Len(t, entries, 1)
	assert.Equal(t, "reference_video", entries[0]["type"])
	assert.Equal(t, "https://cdn.example.com/a.mp4", entries[0]["url"])
}

// 同一地址既在 media 又在顶层 videos：不能重复投递。
func TestHuoDongRefVideoRouteDeduplicatesNormalizedVideos(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","videos":["https://cdn.example.com/a.mp4"],"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	entries := mediaEntries(t, body)
	require.Len(t, entries, 1, "同一参考视频不能投递两次")
}

func TestHuoDongRefVideoRouteHandlesMetadataInputMedia(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"6","metadata":{"input":{"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}}}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"])
	entries := mediaEntries(t, body)
	require.Len(t, entries, 1, "metadata.input.media 必须搬到顶层 media：有赞只在顶层为空时才看 metadata")
	assert.Equal(t, "reference_video", entries[0]["type"])
	assert.NotContains(t, metadataOf(t, body), "input", "只剩 media 的 input 对象要整个删掉")
}

// 三类 media 容器在源链路里是**并行**关系（心笔全部读取），有赞只在顶层
// media 为空时才回退读 metadata。原样透传会让 metadata 里的素材被静默丢掉。
func TestHuoDongRefVideoRouteMergesAllMediaContainers(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8",`+
			`"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}],`+
			`"metadata":{"ratio":"16:9",`+
			`"media":[{"type":"reference_image","url":"https://cdn.example.com/m.png"}],`+
			`"input":{"media":[{"type":"reference_audio","url":"https://cdn.example.com/m.mp3"}]}}}`)

	entries := mediaEntries(t, body)
	require.Len(t, entries, 3, "三个容器的素材必须全部保留")
	assert.Equal(t, "reference_video", entries[0]["type"])
	assert.Equal(t, "reference_image", entries[1]["type"])
	assert.Equal(t, "reference_audio", entries[2]["type"])
	metadata := metadataOf(t, body)
	assert.NotContains(t, metadata, "media", "已并入顶层 media，不能留一份重复投递")
	assert.NotContains(t, metadata, "input")
}

// 顶层 audio / audios 与 video / videos 一样是有赞不读的兼容口字段，
// 不并入 media 就是静默丢音频（任务照建、照扣费、成片没有音频参考）。
func TestHuoDongRefVideoRouteMergesTopLevelAudio(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8",`+
			`"videos":["https://cdn.example.com/a.mp4"],`+
			`"audios":["https://cdn.example.com/a.mp3"]}`)

	assert.NotContains(t, body, "audios")
	entries := mediaEntries(t, body)
	require.Len(t, entries, 2)
	assert.Equal(t, "reference_video", entries[0]["type"])
	assert.Equal(t, "reference_audio", entries[1]["type"])
	assert.Equal(t, "https://cdn.example.com/a.mp3", entries[1]["url"])
}

// 有赞 adaptor 只认 reference_* 三种 type；兼容口的旧写法（video / image / audio）
// 原样透传会被 400，而它们在源链路是能跑的。必须按源链路的分类口径归一。
func TestHuoDongRefVideoRouteNormalizesLegacyMediaTypes(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","media":[`+
			`{"type":"video","url":"https://cdn.example.com/a.mp4"},`+
			`{"type":"image","url":"https://cdn.example.com/a.png"},`+
			`{"type":"audio","url":"https://cdn.example.com/a.mp3"}]}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"], "旧写法本身就带参考视频，应当改派")
	entries := mediaEntries(t, body)
	require.Len(t, entries, 3)
	assert.Equal(t, "reference_video", entries[0]["type"])
	assert.Equal(t, "reference_image", entries[1]["type"])
	assert.Equal(t, "reference_audio", entries[2]["type"])
}

// @图片N 的序号依赖同类型素材的相对顺序：源链路把 image / images 排在 media 之前，
// 改写后必须保持，否则混用两种写法的请求会引用错图。
func TestHuoDongRefVideoRoutePreservesLegacyImageOrder(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","images":["https://cdn.example.com/legacy.png"],`+
			`"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"},`+
			`{"type":"reference_image","url":"https://cdn.example.com/media.png"}]}`)

	assert.NotContains(t, body, "images", "已并入 media，再留着会被有赞 adaptor 追加一遍")
	entries := mediaEntries(t, body)
	require.Len(t, entries, 3)
	assert.Equal(t, "https://cdn.example.com/legacy.png", entries[0]["url"], "legacy 图必须排第一")
	assert.Equal(t, "https://cdn.example.com/media.png", entries[2]["url"], "media 里的图排在 legacy 图之后")
}

// 空 URL 素材在源链路被忽略，有赞会直接 400。改派时一并丢掉。
func TestHuoDongRefVideoRouteDropsEmptyURLMedia(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","media":[`+
			`{"type":"reference_video","url":"https://cdn.example.com/a.mp4"},`+
			`{"type":"reference_image","url":"  "}]}`)

	entries := mediaEntries(t, body)
	require.Len(t, entries, 1)
}

// 未知素材类型（含首尾帧）：源链路会给出自己更准确的报错，改派只会换成有赞的报错。
func TestHuoDongRefVideoRouteSkipsUnknownMediaType(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","media":[`+
			`{"type":"reference_video","url":"https://cdn.example.com/a.mp4"},`+
			`{"type":"scene","url":"https://cdn.example.com/x.png"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
	assert.Empty(t, origin)
}

// 客户显式要非 720P：源链路（心笔按次档）本来会明确 400，改派不能变成
// 「静默按 720P 交付」，否则客户以为买到了 1080P。
func TestHuoDongRefVideoRouteKeepsExplicitNonFixedResolution(t *testing.T) {
	for _, resolution := range []string{"1080P", "480P"} {
		t.Run(resolution, func(t *testing.T) {
			body, origin := runHuoDongRefVideo(t,
				`{"model":"seedance2.5-活动","seconds":"8","resolution":"`+resolution+`","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)
			assert.Equal(t, constant.Seedance25HuoDongModel, body["model"], "应留给源链路报错")
			assert.Empty(t, origin)
		})
	}
	routed, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","resolution":"720p","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)
	assert.Equal(t, constant.Seedance25TeHuiModel, routed["model"], "720p 小写等价，应照常改派")
}

// seed 在源链路是明确 400（上游 parameterSchema 拒绝），改派后被有赞静默忽略，
// 客户会拿到「seed 没生效」的成片。
func TestHuoDongRefVideoRouteSkipsSeed(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","seed":42,"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
	assert.Empty(t, origin)
}

// input_reference 在源链路被忽略，但有赞会把它当参考图 —— 改派等于凭空多一张素材。
func TestHuoDongRefVideoRouteSkipsInputReference(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","input_reference":"https://cdn.example.com/first.png","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
	assert.Empty(t, origin)
}

// 有赞硬限制：有参考视频时生成 ≤15 秒。16 秒及以上留在心笔。
func TestHuoDongRefVideoRouteKeepsLongGenerationOnSource(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"16","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"], "超过有赞上限的请求不能改派")
	assert.Empty(t, origin)
}

func TestHuoDongRefVideoRouteDurationBoundary(t *testing.T) {
	routed, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"15","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)
	assert.Equal(t, constant.Seedance25TeHuiModel, routed["model"], "15 秒正好是有赞上限，应改派")
}

// 不传时长时按次档默认 30 秒 → 超过有赞上限，不能改派。
func TestHuoDongRefVideoRouteKeepsDefaultDurationOnSource(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
}

// 1 秒应由源链路按自己的规则拒绝，路由层不能替它改成有赞的下限 2 秒。
func TestHuoDongRefVideoRouteKeepsTooShortOnSource(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"1","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
}

func TestHuoDongRefVideoRouteIgnoresRequestsWithoutVideo(t *testing.T) {
	body, origin := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","images":["https://cdn.example.com/a.png"]}`)

	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
	assert.Empty(t, origin)
}

func TestHuoDongRefVideoRouteIgnoresOtherModels(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-特惠","seconds":"8","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"], "特惠本来就在有赞，不应被再次改写")
}

// 活动版固定 720P：metadata 里的分辨率源链路不校验，改派后必须落回 720P，
// 否则 1080P 的上游成本（¥0.20/秒）会侵蚀按次售价。
func TestHuoDongRefVideoRouteForces720P(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","size":"1920x1080","metadata":{"ratio":"16:9","resolution":"1080P"},"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.NotContains(t, body, "size", "像素尺寸摘掉前必须把比例搬进 metadata.ratio")
	assert.Equal(t, "720P", metadataOf(t, body)["resolution"])
	assert.Equal(t, "16:9", metadataOf(t, body)["ratio"], "比例不在本规则职责内，必须原样保留")
}

// 前面的规则已经记过计费原点时不能被覆盖（同 Wan3TehuiXinbiRoute 的约束）。
func TestHuoDongRefVideoRouteKeepsPreexistingOrigin(t *testing.T) {
	_, origin := runHuoDongRefVideoWithOrigin(t,
		`{"model":"seedance2.5-活动","seconds":"8","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`,
		constant.Seedance25HuoDongModel)

	assert.Equal(t, constant.Seedance25HuoDongModel, origin)
}

// size 里写的比例在摘掉 size 后必须保留，否则客户选的画幅会变成默认 adaptive。
func TestHuoDongRefVideoRouteKeepsSizeRatio(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","size":"9:16","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.Equal(t, constant.Seedance25TeHuiModel, body["model"])
	assert.Equal(t, "9:16", metadataOf(t, body)["ratio"])
}

// 目标模型没有可用渠道时不能改派，否则客户会因为调度策略直接失败。
func TestHuoDongRefVideoRouteKeepsSourceWhenTargetUnavailable(t *testing.T) {
	stubChannelAvailable(t, false)
	rec := &routeRecorder{}
	engine := newHuoDongRefVideoEngine(rec, "")
	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(
		`{"model":"seedance2.5-活动","seconds":"8","media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())

	body := decodeBody(t, rec.body)
	assert.Equal(t, constant.Seedance25HuoDongModel, body["model"])
}
