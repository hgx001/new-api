/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package erchun

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 本渠道只对外定价 480P。上游目录其实支持 720P/1080P（默认 1080P），若不在校验层
// 拦死，非 480P 的请求会按 480P 收费却建出高分辨率单 —— 静默少收，且无法事后追回。
func TestResolveResolutionRejectsNon480P(t *testing.T) {
	tests := []struct {
		name       string
		resolution string
		want       string
		wantErr    string
	}{
		{name: "缺省走 480P", resolution: "", want: onlyResolution},
		{name: "小写 480p", resolution: "480p", want: onlyResolution},
		{name: "大写 480P", resolution: "480P", want: onlyResolution},
		{name: "裸 480", resolution: "480", want: onlyResolution},
		{name: "720P 拒绝", resolution: "720P", wantErr: "only supports 480P"},
		{name: "1080P 拒绝", resolution: "1080p", wantErr: "only supports 480P"},
		{name: "未知档位拒绝", resolution: "4k", wantErr: "only supports 480P"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveResolution(relaycommon.TaskSubmitReq{Resolution: tt.resolution})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResolveDurationClampsToCatalogRange(t *testing.T) {
	// catalog durations: enum 2..30, default 5
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{}), "缺省用上游默认 5 秒")
	require.Equal(t, 2, resolveDuration(relaycommon.TaskSubmitReq{Duration: 2}))
	require.Equal(t, 2, resolveDuration(relaycommon.TaskSubmitReq{Duration: 1}), "低于下界抬到 2")
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Duration: 30}))
	require.Equal(t, 30, resolveDuration(relaycommon.TaskSubmitReq{Duration: 99}), "高于上界压到 30")

	// 下游按 OpenAI video 形状传的是 {"seconds":"2"} —— 字符串，落进 Seconds 而非 Duration。
	// 漏读 Seconds 会静默按默认 5 秒计费（2026-10-06 首次真机验证就是这样多收了 2.5 倍）。
	require.Equal(t, 2, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "2"}))
	require.Equal(t, 7, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "7"}))
	// Duration 优先于 Seconds
	require.Equal(t, 3, resolveDuration(relaycommon.TaskSubmitReq{Duration: 3, Seconds: "9"}))
	// 非法/空 Seconds 回落默认值，不报错
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "abc"}))
	require.Equal(t, 5, resolveDuration(relaycommon.TaskSubmitReq{Seconds: "  "}))
}

func TestResolveAspectRatio(t *testing.T) {
	// catalog aspect_ratios: adaptive/16:9/4:3/1:1/3:4/9:16, default adaptive
	got, err := resolveAspectRatio(relaycommon.TaskSubmitReq{})
	require.NoError(t, err)
	require.Equal(t, "adaptive", got, "缺省必须是上游默认 adaptive，不能瞎给一个具体比例")

	got, err = resolveAspectRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"ratio": "9:16"},
	})
	require.NoError(t, err)
	require.Equal(t, "9:16", got)

	got, err = resolveAspectRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"aspect_ratio": "1:1"},
	})
	require.NoError(t, err)
	require.Equal(t, "1:1", got)

	// size 反解
	got, err = resolveAspectRatio(relaycommon.TaskSubmitReq{Size: "1280x720"})
	require.NoError(t, err)
	require.Equal(t, "16:9", got)

	_, err = resolveAspectRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"ratio": "21:9"},
	})
	require.ErrorContains(t, err, "does not support aspect_ratio")
}

func TestRatioFromSize(t *testing.T) {
	for _, tt := range []struct{ size, want string }{
		{"1280x720", "16:9"},
		{"1920x1080", "16:9"},
		{"720x1280", "9:16"},
		{"1024x1024", "1:1"},
	} {
		got, ok := ratioFromSize(tt.size)
		require.True(t, ok, tt.size)
		require.Equal(t, tt.want, got, tt.size)
	}
	_, ok := ratioFromSize("not-a-size")
	require.False(t, ok)
}

// 上游只认 catalog 的稳定 ID；display_name 和我们自己的对外名都不能发过去。
func TestUpstreamModelUsesStableID(t *testing.T) {
	adaptor := &TaskAdaptor{}
	// 没配模型映射时回落到内置稳定 ID
	require.Equal(t, UpstreamModelWan3, adaptor.upstreamModel(nil))
	// 配了映射就用映射结果
	require.Equal(t, "mdl_custom", adaptor.upstreamModel(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "mdl_custom"},
	}))
	// 映射没生效（仍是我们的对外名）时不能把 wan3.0-480p 原样发给上游
	require.Equal(t, UpstreamModelWan3, adaptor.upstreamModel(&relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: PublicModel},
	}))
}

// 计费只乘 seconds：本渠道只允许 480P，分辨率维度恒为 1，不该再叠一个 size 倍率
// （多一次浮点乘+截断只会引入不必要的不确定性）。
func TestEstimateBillingOnlyMultipliesSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		duration int
		want     float64
	}{{2, 2}, {5, 5}, {30, 30}} {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set("task_request", relaycommon.TaskSubmitReq{Duration: tt.duration, Resolution: "480P"})
		billing := (&TaskAdaptor{}).EstimateBilling(ctx, &relaycommon.RelayInfo{OriginModelName: PublicModel})
		require.Equal(t, tt.want, billing["seconds"], "duration=%d", tt.duration)
		require.NotContains(t, billing, "size", "只允许 480P，不应下发 size 倍率")
	}

	// {"seconds":"2"} 这条真实入参形状：预扣必须是 2 秒而不是默认的 5 秒。
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("task_request", relaycommon.TaskSubmitReq{Seconds: "2", Resolution: "480P"})
	billing := (&TaskAdaptor{}).EstimateBilling(ctx, &relaycommon.RelayInfo{OriginModelName: PublicModel})
	require.Equal(t, float64(2), billing["seconds"], "seconds 字符串形状必须被采纳")
}

// 素材数量上限取自 catalog capabilities（不是文档里的全局 upload 限制）。
func TestValidateMediaEnforcesCatalogCounts(t *testing.T) {
	images := make([]relaycommon.TaskMedia, 0, 11)
	for i := 0; i < 11; i++ {
		images = append(images, relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/a.png"})
	}
	require.ErrorContains(t, validateMedia(images), "at most 10 reference images")

	videos := make([]relaycommon.TaskMedia, 0, 6)
	for i := 0; i < 6; i++ {
		videos = append(videos, relaycommon.TaskMedia{Type: "reference_video", URL: "https://cdn.example/v.mp4"})
	}
	require.ErrorContains(t, validateMedia(videos), "at most 5 reference videos")

	audios := make([]relaycommon.TaskMedia, 0, 6)
	for i := 0; i < 6; i++ {
		audios = append(audios, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://cdn.example/a.mp3"})
	}
	require.ErrorContains(t, validateMedia(audios), "at most 5 reference audios")

	// 逐类型上限之和（10+5+5）已经等于 catalog 的 max_total=20，所以混合上限
	// 永远不会被先触发 —— 这里只守住逐类型边界，不假装能测到混合上限。
	mixed := make([]relaycommon.TaskMedia, 0, 20)
	for i := 0; i < 10; i++ {
		mixed = append(mixed, relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/a.png"})
	}
	for i := 0; i < 5; i++ {
		mixed = append(mixed, relaycommon.TaskMedia{Type: "reference_video", URL: "https://cdn.example/v.mp4"})
	}
	for i := 0; i < 5; i++ {
		mixed = append(mixed, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://cdn.example/a.mp3"})
	}
	require.NoError(t, validateMedia(mixed), "10+5+5=20 正好在 catalog 的 mix.max_total 内")
	// first_frame / last_frame 与参考图共享 image 名额，不能绕过上限
	frames := make([]relaycommon.TaskMedia, 0, 11)
	for i := 0; i < 9; i++ {
		frames = append(frames, relaycommon.TaskMedia{Type: "reference_image", URL: "https://cdn.example/a.png"})
	}
	frames = append(frames,
		relaycommon.TaskMedia{Type: "first_frame", URL: "https://cdn.example/f.png"},
		relaycommon.TaskMedia{Type: "last_frame", URL: "https://cdn.example/l.png"},
	)
	require.ErrorContains(t, validateMedia(frames), "at most 10 reference images",
		"首尾帧占的是 image 名额，多加两张就破 10 的上限")

	require.ErrorContains(t, validateMedia([]relaycommon.TaskMedia{
		{Type: "bogus", URL: "https://cdn.example/a.png"},
	}), "is unsupported")
}

// 202 只代表受理；completed 但没有 content_url 不能当成功，否则客户端拿到打不开的地址。
func TestParseTaskResultStatusMapping(t *testing.T) {
	for _, tt := range []struct {
		body       string
		wantStatus string
	}{
		{`{"id":"t1","status":"queued","content_url":"/v1/tasks/t1/content"}`, model.TaskStatusQueued},
		{`{"id":"t1","status":"in_progress","content_url":"/v1/tasks/t1/content"}`, model.TaskStatusInProgress},
		{`{"id":"t1","status":"completed","content_url":"/v1/tasks/t1/content"}`, model.TaskStatusSuccess},
		{`{"id":"t1","status":"failed","error":{"code":"x","message":"审核不通过"}}`, model.TaskStatusFailure},
		{`{"id":"t1","status":"completed"}`, model.TaskStatusInProgress},
		{`{"id":"t1","status":"weird"}`, model.TaskStatusInProgress},
	} {
		info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(tt.body))
		require.NoError(t, err, tt.body)
		require.Equal(t, tt.wantStatus, info.Status, tt.body)
	}
}

// 成功时必须留空 Url，让 service/task_polling.go 落成站内 content 代理地址。
// 若在这里吐上游地址，下游会拿一个没有 Bearer 的 /v1/tasks/{id}/content，必然 401。
func TestParseTaskResultLeavesURLEmptyForProxy(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t1","status":"completed","content_url":"/v1/tasks/t1/content"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, info.Status)
	require.Empty(t, info.Url, "content 需渠道密钥回源，不能把上游地址直接给下游")
}

func TestModelListExposesOnlyPublicModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Equal(t, []string{"wan3.0-480p"}, adaptor.GetModelList())
	require.Equal(t, "二春 Erchun", adaptor.GetChannelName())
}
