package middleware

// 万相官网 → wan3-新大陆 的手动改派（2026-11-27）。
//
// 本文件锁五件事：
//  1. 开关关着时一个字都不改；
//  2. 打开且目标有渠道：模型名换成 wan3-新大陆，计费原点仍是官网；
//  3. 档位倍率按**官网口径**覆盖（1/2.25/4），不是新大陆自己的 1/1.739/3.623；
//  4. 带参考视频 / 素材过不了目标校验 / 目标没渠道 → 留在官网；
//  5. 与客美开关互斥：客美开着时本中间件不抢。

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

func setWan3GuanwangXindalu(t *testing.T, enabled bool, extra map[string]interface{}) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{}
	for key, value := range extra {
		payload[key] = value
	}
	payload["wan3_guanwang_xindalu_enabled"] = enabled
	encoded, err := common.Marshal(payload)
	require.NoError(t, err)
	require.NoError(t, seedanceroute_setting.UpdateFromJSONString(string(encoded)))
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

func runWan3GuanwangXindalu(t *testing.T, path, contentType, body string) *routeRecorder {
	t.Helper()
	rec := &routeRecorder{}
	engine := newRouteEngine(rec, Wan3GuanwangXindaluRoute())
	request := httptest.NewRequest("POST", path, bytes.NewBufferString(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	return rec
}

func TestWan3GuanwangXindaluOffKeepsRequest(t *testing.T) {
	setWan3GuanwangXindalu(t, false, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body, "开关关着时请求体必须逐字节不变")
	assert.Empty(t, rec.originModel, "开关关着时不该记计费原点")
}

func TestWan3GuanwangXindaluRewritesToTarget(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	decoded := decodeBody(t, rec.body)
	assert.Equal(t, seedanceroute_setting.Wan3XindaluTargetModel, decoded["model"])
	assert.Equal(t, "a cat", decoded["prompt"], "提示词不能被动")
	assert.Equal(t, "480P", decoded["resolution"], "分辨率档原样保留")
	assert.Equal(t, "16:9", decoded["ratio"], "画幅原样保留")
	assert.Equal(t, seedanceroute_setting.Wan3GuanwangModel, rec.originModel,
		"计费原点必须是客户请求的官网模型")
}

// 计费按官网价：1080P 必须是 4.0 倍，不能落到新大陆自己的 3.6232。
func TestWan3GuanwangXindaluUsesSourceSizeRatio(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","resolution":"1080P","duration":5}`)

	require.True(t, rec.hasBillingSize, "必须写源模型口径的档位倍率")
	assert.InDelta(t, 4.0, rec.billingSizeRatio, 1e-6, "1080P 必须按官网的 ¥0.80/¥0.20 = 4 倍收")
}

// 新大陆支持 adaptive 画幅，不像客美会在 adaptive 时放弃改派。
func TestWan3GuanwangXindaluRoutesAdaptiveRatio(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json",
		`{"model":"wan3.0-video-官网","prompt":"a cat","ratio":"adaptive","duration":5}`)

	assert.Equal(t, seedanceroute_setting.Wan3XindaluTargetModel, decodeBody(t, rec.body)["model"])
	assert.Equal(t, "adaptive", decodeBody(t, rec.body)["ratio"])
}

func TestWan3GuanwangXindaluSkipsWhenTargetUnavailable(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, false)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body, "目标没渠道时必须原样放行")
	assert.Empty(t, rec.originModel)
}

// 带参考视频的请求不改派：新大陆会把参考视频秒数并入计费，官网只按生成秒数收。
func TestWan3GuanwangXindaluSkipsReferenceVideo(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, true)

	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,` +
		`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`
	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "带参考视频必须留在官网链路")
	assert.Empty(t, rec.originModel)
}

// 素材过不了新大陆校验（>10 张参考图）→ 留在官网。
func TestWan3GuanwangXindaluSkipsMediaRejectedByTarget(t *testing.T) {
	setWan3GuanwangXindalu(t, true, nil)
	stubChannelAvailable(t, true)

	entries := make([]string, 0, 11)
	for i := 0; i < 11; i++ {
		entries = append(entries, `{"type":"reference_image","url":"https://example.com/`+
			string(rune('a'+i))+`.png"}`)
	}
	body := `{"model":"wan3.0-video-官网","prompt":"a cat","duration":5,"media":[` +
		strings.Join(entries, ",") + `]}`
	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", body)

	assert.Equal(t, body, rec.body, "超过新大陆图片上限时必须留在官网链路")
	assert.Empty(t, rec.originModel)
}

// 客美开关开着时（历史 option 双 true），本中间件不抢：请求交给客美链路。
func TestWan3GuanwangXindaluYieldsToKemeitv(t *testing.T) {
	setWan3GuanwangXindalu(t, true, map[string]interface{}{"wan3_guanwang_tj_enabled": true})
	stubChannelAvailable(t, true)

	rec := runWan3GuanwangXindalu(t, "/v1/video/generations", "application/json", wan3GuanwangBody)

	assert.Equal(t, wan3GuanwangBody, rec.body,
		"两个开关同时为 true 时本中间件必须让位给客美，不能二次改写")
	assert.Empty(t, rec.originModel)
}

var _ = gin.SetMode
