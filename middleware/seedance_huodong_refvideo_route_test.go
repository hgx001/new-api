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

// 活动版固定 720P：客户传 1080P 时改派后也必须落回 720P，
// 否则 1080P 的上游成本（¥0.20/秒）会侵蚀按次售价。
func TestHuoDongRefVideoRouteForces720P(t *testing.T) {
	body, _ := runHuoDongRefVideo(t,
		`{"model":"seedance2.5-活动","seconds":"8","resolution":"1080P","size":"1080P","metadata":{"ratio":"16:9","resolution":"1080P"},"media":[{"type":"reference_video","url":"https://cdn.example.com/a.mp4"}]}`)

	assert.NotContains(t, body, "resolution", "顶层分辨率必须摘掉，否则会盖过 metadata 里的 720P")
	assert.NotContains(t, body, "size")
	metadata := metadataOf(t, body)
	assert.Equal(t, "720P", metadata["resolution"])
	assert.Equal(t, "16:9", metadata["ratio"], "比例不在本规则职责内，必须原样保留")
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
