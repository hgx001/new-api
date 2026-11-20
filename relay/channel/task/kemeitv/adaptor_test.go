package kemeitv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain 先灌一遍代码内置的默认 ModelPrice。
//
// 计费断言依赖「基准价 × 倍率」的绝对价：测试环境没有 options 表，
// 不初始化就查不到 kemeitv 两个模型的基准价，倍率会回落标准 1:2:4，
// 断言就变成假通过/假失败。
func TestMain(m *testing.M) {
	ratio_setting.InitRatioSettings()
	os.Exit(m.Run())
}

func newTestContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", req)
	return c
}

func newAdaptor() *TaskAdaptor {
	return &TaskAdaptor{baseURL: "https://ai.kemeitv.cn", apiKey: "sk-test"}
}

// ────────────────────────────────────────────────────────────────────────────
// 时长解析
// ────────────────────────────────────────────────────────────────────────────

func TestResolveDuration(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr bool
	}{
		{name: "缺省 5 秒", req: relaycommon.TaskSubmitReq{}, want: defaultDuration},
		{name: "显式 2 秒", req: relaycommon.TaskSubmitReq{Duration: 2}, want: 2},
		{name: "显式 30 秒", req: relaycommon.TaskSubmitReq{Duration: 30}, want: 30},
		{name: "1 秒越界拒绝", req: relaycommon.TaskSubmitReq{Duration: 1}, wantErr: true},
		{name: "31 秒越界拒绝", req: relaycommon.TaskSubmitReq{Duration: 31}, wantErr: true},
		{
			name: "metadata.duration",
			req:  relaycommon.TaskSubmitReq{Metadata: map[string]any{"duration": 12}},
			want: 12,
		},
		{
			name: "metadata.seconds 数字字符串",
			req:  relaycommon.TaskSubmitReq{Metadata: map[string]any{"seconds": "8"}},
			want: 8,
		},
		{
			name:    "metadata.duration 越界拒绝",
			req:     relaycommon.TaskSubmitReq{Metadata: map[string]any{"duration": 99}},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDuration(tc.req)
			if tc.wantErr {
				require.Error(t, err, "越界时长必须本地拒绝，不能夹取")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 分辨率解析
// ────────────────────────────────────────────────────────────────────────────

func TestResolveResolution(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr bool
	}{
		{name: "缺省 720P", req: relaycommon.TaskSubmitReq{}, want: resolution720P},
		{name: "顶层 1080P", req: relaycommon.TaskSubmitReq{Resolution: "1080P"}, want: resolution1080P},
		{name: "顶层小写 480p", req: relaycommon.TaskSubmitReq{Resolution: "480p"}, want: resolution480P},
		{name: "纯数字 720", req: relaycommon.TaskSubmitReq{Resolution: "720"}, want: resolution720P},
		{
			// 站内口径：顶层 resolution 优先于 metadata.resolution
			// （与 wan3 / youzanwan3 的 resolveResolution 同序）。
			name: "顶层优先于 metadata",
			req: relaycommon.TaskSubmitReq{
				Resolution: "480P",
				Metadata:   map[string]any{"resolution": "1080P"},
			},
			want: resolution480P,
		},
		{name: "size 1280x720", req: relaycommon.TaskSubmitReq{Size: "1280x720"}, want: resolution720P},
		{name: "size 1920x1080", req: relaycommon.TaskSubmitReq{Size: "1920x1080"}, want: resolution1080P},
		{name: "size 640x480", req: relaycommon.TaskSubmitReq{Size: "640x480"}, want: resolution480P},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveResolution(tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 画幅比例解析
// ────────────────────────────────────────────────────────────────────────────

func TestResolveRatio(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{name: "缺省 16:9", req: relaycommon.TaskSubmitReq{}, want: defaultRatio},
		{name: "顶层 ratio", req: relaycommon.TaskSubmitReq{Ratio: "9:16"}, want: "9:16"},
		{name: "顶层 aspect_ratio", req: relaycommon.TaskSubmitReq{AspectRatio: "1:1"}, want: "1:1"},
		{
			name: "metadata.ratio 优先",
			req: relaycommon.TaskSubmitReq{
				Ratio:    "9:16",
				Metadata: map[string]any{"ratio": "4:3"},
			},
			want: "4:3",
		},
		{name: "size 反推", req: relaycommon.TaskSubmitReq{Size: "1280x720"}, want: "16:9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRatio(tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 上游模型名映射
// ────────────────────────────────────────────────────────────────────────────

func TestUpstreamModelFor(t *testing.T) {
	assert.Equal(t, upstreamModelTJ, upstreamModelFor(ModelNameTJ))
	assert.Equal(t, upstreamModelPrimeTJ, upstreamModelFor(ModelNamePrimeTJ))
	// 未登记的名字原样透传，不静默改名。
	assert.Equal(t, "some-other-model", upstreamModelFor("some-other-model"))
}

// 只接两个模型 —— 这是用户 2026-11-19 明确的口径，必须钉住。
func TestModelList只接两个模型(t *testing.T) {
	assert.Equal(t, []string{ModelNameTJ, ModelNamePrimeTJ}, ModelList)
	assert.Len(t, ModelList, 2)
}

// ────────────────────────────────────────────────────────────────────────────
// 素材归一与校验
// ────────────────────────────────────────────────────────────────────────────

func TestCollectMedia_老字段图片按首帧处理(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Images: []string{"https://example.com/a.png"},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Images, 1)
	assert.Equal(t, roleFirstFrame, bundle.Images[0].Role)
	assert.Equal(t, "https://example.com/a.png", bundle.Images[0].URL)
}

func TestCollectMedia_显式media保留role(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleFirstFrame, URL: "https://example.com/f.png"},
			{Type: roleLastFrame, URL: "https://example.com/l.png"},
		},
	})
	require.NoError(t, err)
	require.Len(t, bundle.Images, 2)
	assert.Equal(t, roleFirstFrame, bundle.Images[0].Role)
	assert.Equal(t, roleLastFrame, bundle.Images[1].Role)
}

// 首尾帧两张图是**合法组合**（上游的「首尾帧生视频」场景），
// 不能被「合计 > 1」的判据误伤。
func TestCollectMedia_首帧加尾帧合法(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleFirstFrame, URL: "https://example.com/f.png"},
			{Type: roleLastFrame, URL: "https://example.com/l.png"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, bundle.Images, 2)
}

// 多张参考图是**合法组合**（上游的「多图参考生视频」场景）。
func TestCollectMedia_多张参考图合法(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://example.com/1.png"},
			{Type: roleReferenceImage, URL: "https://example.com/2.png"},
			{Type: roleReferenceImage, URL: "https://example.com/3.png"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, bundle.Images, 3)
}

func TestCollectMedia_未知type报错(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{{Type: "bogus_type", URL: "https://example.com/a.png"}},
	})
	require.Error(t, err, "未知素材类型必须本地拒绝，不能静默丢弃")
}

func TestCollectMedia_首尾帧与参考图互斥(t *testing.T) {
	// 上游文档明确「场景互斥」，混用上游报 400 —— 本地必须先拦。
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleFirstFrame, URL: "https://example.com/f.png"},
			{Type: roleReferenceImage, URL: "https://example.com/r.png"},
		},
	})
	require.Error(t, err)
}

func TestCollectMedia_首尾帧与参考视频互斥(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleLastFrame, URL: "https://example.com/l.png"},
			{Type: roleReferenceVideo, URL: "https://example.com/v.mp4"},
		},
	})
	require.Error(t, err)
}

func TestCollectMedia_参考音频不能单独使用(t *testing.T) {
	// 上游文档：至少需一张参考图或一段参考视频。
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Audios: []string{"https://example.com/a.mp3"},
	})
	require.Error(t, err)
}

func TestCollectMedia_参考音频配参考图可用(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://example.com/r.png"},
		},
		Audios: []string{"https://example.com/a.mp3"},
	})
	require.NoError(t, err)
	assert.Len(t, bundle.Audios, 1)
	assert.Equal(t, roleReferenceAudio, bundle.Audios[0].Role)
}

func TestCollectMedia_两张首帧报错(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleFirstFrame, URL: "https://example.com/1.png"},
			{Type: roleFirstFrame, URL: "https://example.com/2.png"},
		},
	})
	require.Error(t, err)
}

func TestCollectMedia_图片超上限报错(t *testing.T) {
	media := make([]relaycommon.TaskMedia, 0, maxReferenceImages+1)
	for i := 0; i <= maxReferenceImages; i++ {
		media = append(media, relaycommon.TaskMedia{
			Type: roleReferenceImage,
			URL:  "https://example.com/" + string(rune('a'+i%26)) + ".png",
		})
	}
	_, err := collectMedia(relaycommon.TaskSubmitReq{Media: media})
	require.Error(t, err)
}

// ────────────────────────────────────────────────────────────────────────────
// 请求体构造
// ────────────────────────────────────────────────────────────────────────────

func TestBuildRequestBody(t *testing.T) {
	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:     "一只猫在雪地里跑",
		Duration:   8,
		Resolution: "1080P",
		Ratio:      "9:16",
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://example.com/r.png"},
		},
		Videos: []string{"https://example.com/v.mp4"},
	})
	a := newAdaptor()
	body, err := a.BuildRequestBody(c, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: 71},
		OriginModelName: ModelNameTJ,
	})
	require.NoError(t, err)

	decoded := submitRequest{}
	raw, err := readAll(body)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &decoded))

	assert.Equal(t, upstreamModelTJ, decoded.Model)
	assert.Equal(t, "一只猫在雪地里跑", decoded.Prompt)
	assert.Equal(t, 8, decoded.Duration)
	assert.Equal(t, resolution1080P, decoded.Resolution)
	assert.Equal(t, "9:16", decoded.Ratio)
	require.Len(t, decoded.Images, 1)
	assert.Equal(t, roleReferenceImage, decoded.Images[0].Role)
	require.Len(t, decoded.Videos, 1)
	assert.Equal(t, roleReferenceVideo, decoded.Videos[0].Role)
}

func TestBuildRequestBody_上游模型名锁定(t *testing.T) {
	// 渠道 model_mapping 配错时，代码锁死的映射要挡住降级。
	c := newTestContext(t, relaycommon.TaskSubmitReq{Prompt: "x"})
	a := newAdaptor()
	body, err := a.BuildRequestBody(c, &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{ChannelType: 71},
		OriginModelName: ModelNamePrimeTJ,
	})
	require.NoError(t, err)
	raw, err := readAll(body)
	require.NoError(t, err)
	decoded := submitRequest{}
	require.NoError(t, json.Unmarshal(raw, &decoded))
	assert.Equal(t, upstreamModelPrimeTJ, decoded.Model)
}

// ────────────────────────────────────────────────────────────────────────────
// 计费倍率
// ────────────────────────────────────────────────────────────────────────────

func TestEstimateBilling(t *testing.T) {
	a := newAdaptor()

	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Duration:   10,
		Resolution: resolution480P,
	})
	ratios := a.EstimateBilling(c, nil)
	assert.Equal(t, float64(10), ratios["seconds"])
	assert.Equal(t, 1.0, ratios["size"])

	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Duration:   10,
		Resolution: resolution720P,
	})
	ratios = a.EstimateBilling(c, nil)
	assert.Equal(t, float64(10), ratios["seconds"])
	assert.Equal(t, 2.0, ratios["size"])

	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Duration:   10,
		Resolution: resolution1080P,
	})
	ratios = a.EstimateBilling(c, nil)
	assert.Equal(t, float64(10), ratios["seconds"])
	assert.Equal(t, 4.0, ratios["size"])
}

// tj 的 720P 是人工特价 ¥0.25/秒：基准 ¥0.138 × 倍率(0.25/0.138) = ¥0.25。
// 该覆盖只作用于 tj 的 720P —— 480P/1080P 与 prime-tj 全档必须保持标准 1/2/4。
func TestEstimateBilling_模型覆盖倍率(t *testing.T) {
	a := newAdaptor()
	tjInfo := &relaycommon.RelayInfo{OriginModelName: ModelNameTJ}
	primeInfo := &relaycommon.RelayInfo{OriginModelName: ModelNamePrimeTJ}

	want720P := 0.25 / 0.138

	// tj 720P：走特价倍率。
	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 10, Resolution: resolution720P,
	})
	ratios := a.EstimateBilling(c, tjInfo)
	assert.Equal(t, float64(10), ratios["seconds"])
	assert.InDelta(t, want720P, ratios["size"], 1e-9)

	// tj 480P / 1080P：标准倍率，不受覆盖影响。
	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 10, Resolution: resolution480P,
	})
	assert.Equal(t, 1.0, a.EstimateBilling(c, tjInfo)["size"])
	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 10, Resolution: resolution1080P,
	})
	assert.InDelta(t, 4.0, a.EstimateBilling(c, tjInfo)["size"], 1e-9)

	// prime-tj 720P：¥0.48 / ¥0.24 = 2 倍，与 tj 的档位价互不影响。
	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 10, Resolution: resolution720P,
	})
	assert.InDelta(t, 2.0, a.EstimateBilling(c, primeInfo)["size"], 1e-9)
}

// 2026-11-20 老服务器调价：tj 480P ¥0.138→¥0.15、prime-tj ¥0.24→¥0.25，
// 但 720P / 1080P 的绝对价必须保持原样。
//
// 两台生产服务器共用同一份代码、基准价却不同（老 0.15/0.25、新 0.138/0.24），
// 所以倍率不能写死 —— 本用例把两种基准价都跑一遍，断言「基准 × 倍率」得到的
// 每档绝对价一致。
func TestEstimateBilling_绝对档位价随基准价反推(t *testing.T) {
	a := newAdaptor()

	original := ratio_setting.GetModelPriceMap()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(
			mapJSON(t, original)))
	})

	cases := []struct {
		name         string
		tjBaseCNY    float64
		primeBaseCNY float64
	}{
		{name: "老服务器调价后", tjBaseCNY: 0.15, primeBaseCNY: 0.25},
		{name: "新服务器维持原价", tjBaseCNY: 0.138, primeBaseCNY: 0.24},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(mapJSON(t, map[string]float64{
				ModelNameTJ:      tc.tjBaseCNY / ratio_setting.USD2RMB,
				ModelNamePrimeTJ: tc.primeBaseCNY / ratio_setting.USD2RMB,
			})))

			tjInfo := &relaycommon.RelayInfo{OriginModelName: ModelNameTJ}
			primeInfo := &relaycommon.RelayInfo{OriginModelName: ModelNamePrimeTJ}

			// (模型, 分辨率, 期望绝对价 CNY/秒)
			assertions := []struct {
				model    string
				info     *relaycommon.RelayInfo
				res      string
				baseCNY  float64
				wantCNY  float64
			}{
				{ModelNameTJ, tjInfo, resolution480P, tc.tjBaseCNY, tc.tjBaseCNY},
				{ModelNamePrimeTJ, primeInfo, resolution480P, tc.primeBaseCNY, tc.primeBaseCNY},
				{ModelNameTJ, tjInfo, resolution720P, tc.tjBaseCNY, 0.25},
				{ModelNameTJ, tjInfo, resolution1080P, tc.tjBaseCNY, 0.552},
				{ModelNamePrimeTJ, primeInfo, resolution720P, tc.primeBaseCNY, 0.48},
				{ModelNamePrimeTJ, primeInfo, resolution1080P, tc.primeBaseCNY, 0.96},
			}
			for _, want := range assertions {
				c := newTestContext(t, relaycommon.TaskSubmitReq{
					Prompt: "x", Duration: 1, Resolution: want.res,
				})
				ratio := a.EstimateBilling(c, want.info)["size"]
				gotCNY := want.baseCNY * ratio
				assert.InDelta(t, want.wantCNY, gotCNY, 1e-9,
					"%s %s 绝对价应为 ¥%.4f/秒", want.model, want.res, want.wantCNY)
			}
		})
	}
}

// mapJSON 把价格表序列化成 UpdateModelPriceByJSONString 需要的 JSON。
func mapJSON(t *testing.T, m map[string]float64) string {
	t.Helper()
	raw, err := json.Marshal(m)
	require.NoError(t, err)
	return string(raw)
}

// 缺省（context 里没有请求）时必须返回缺省秒数，绝不能返回 nil ——
// 返回 nil 会被当成 1 秒，30 秒的请求只收 1 秒的钱。
func TestEstimateBilling_缺省不返回nil(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{})
	// 故意不 Set task_request，模拟请求上下文缺失。
	c.Set("task_request", nil)
	ratios := a.EstimateBilling(c, nil)
	require.NotNil(t, ratios)
	assert.Equal(t, float64(defaultDuration), ratios["seconds"])
}

// ────────────────────────────────────────────────────────────────────────────
// 轮询响应解析
// ────────────────────────────────────────────────────────────────────────────

func TestParseTaskResult(t *testing.T) {
	a := newAdaptor()
	cases := []struct {
		name       string
		body       string
		wantStatus string
		wantURL    string
		wantReason bool
	}{
		{
			name:       "排队中",
			body:       `{"id":"t1","status":"pending"}`,
			wantStatus: model.TaskStatusQueued,
		},
		{
			name:       "生成中",
			body:       `{"id":"t1","status":"running"}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "完成取 data[0].url",
			body:       `{"id":"t1","status":"completed","data":[{"url":"https://example.com/v.mp4"}]}`,
			wantStatus: model.TaskStatusSuccess,
			wantURL:    "https://example.com/v.mp4",
		},
		{
			name:       "完成取顶层 url",
			body:       `{"id":"t1","status":"completed","url":"https://example.com/v.mp4"}`,
			wantStatus: model.TaskStatusSuccess,
			wantURL:    "https://example.com/v.mp4",
		},
		{
			name:       "失败带原因",
			body:       `{"id":"t1","status":"failed","error":{"message":"参数非法"}}`,
			wantStatus: model.TaskStatusFailure,
			wantReason: true,
		},
		{
			name:       "未知状态保守当处理中",
			body:       `{"id":"t1","status":"weird_state"}`,
			wantStatus: model.TaskStatusInProgress,
		},
		{
			name:       "终态但无成片地址保持处理中",
			body:       `{"id":"t1","status":"completed"}`,
			wantStatus: model.TaskStatusInProgress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := a.ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, string(result.Status))
			if tc.wantURL != "" {
				assert.Equal(t, tc.wantURL, result.Url)
			}
			if tc.wantReason {
				assert.NotEmpty(t, result.Reason)
			}
		})
	}
}

// ────────────────────────────────────────────────────────────────────────────
// URL 构造
// ────────────────────────────────────────────────────────────────────────────

func TestBuildRequestURL(t *testing.T) {
	a := newAdaptor()
	url, err := a.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://ai.kemeitv.cn/v1/video/generations", url)
}

// 渠道 base_url 末尾带斜杠时不能拼出 //。
func TestBuildRequestURL_去尾斜杠(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://ai.kemeitv.cn/"}
	url, err := a.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://ai.kemeitv.cn/v1/video/generations", url)
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			if err.Error() == "EOF" {
				return buf, nil
			}
			return buf, nil
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// ConvertToOpenAIVideo
//
// ⚠️ 这个方法缺失时 /v1/videos/{id} 会返回 not_implemented:71 ——
//	任务照跑照扣费，客户端却永远查不到结果。所以必须有回归守卫。
// ────────────────────────────────────────────────────────────────────────────

func TestConvertToOpenAIVideo_成功给代理地址(t *testing.T) {
	a := newAdaptor()
	task := &model.Task{
		TaskID:   "task_test",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Data:     []byte(`{"id":"up1","status":"completed","data":[{"url":"https://up.example/v.mp4?sig=abc"}]}`),
	}
	task.Properties.OriginModelName = ModelNameTJ

	body, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "task_test", out["id"])
	assert.Equal(t, "completed", out["status"])
	assert.Equal(t, "video", out["object"])
	assert.Equal(t, float64(100), out["progress"])
	assert.Equal(t, ModelNameTJ, out["model"])
	// 成片地址必须是本站代理，不能是上游的签名直链（会过期）。
	meta, ok := out["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, meta["url"], "/v1/videos/task_test/content")
}

func TestConvertToOpenAIVideo_未完成不给地址(t *testing.T) {
	a := newAdaptor()
	task := &model.Task{
		TaskID:   "task_test",
		Status:   model.TaskStatusInProgress,
		Progress: "30%",
		Data:     []byte(`{"id":"up1","status":"running"}`),
	}

	body, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "in_progress", out["status"])
	meta, _ := out["metadata"].(map[string]any)
	// 未完成时给地址等于给半成品/会过期的链接。
	if meta != nil {
		assert.NotContains(t, meta, "url")
	}
}

func TestConvertToOpenAIVideo_失败带上游原因(t *testing.T) {
	a := newAdaptor()
	task := &model.Task{
		TaskID:     "task_test",
		Status:     model.TaskStatusFailure,
		Progress:   "100%",
		FailReason: "Failed to download reference image",
		Data:       []byte(`{"id":"up1","status":"failed","error":{"message":"Failed to download reference image","code":"InvalidParameter"}}`),
	}

	body, err := a.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	assert.Equal(t, "failed", out["status"])
	errObj, ok := out["error"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "InvalidParameter", errObj["code"])
	assert.NotEmpty(t, errObj["message"])
}

// ────────────────────────────────────────────────────────────────────────────
// 素材后缀校验
//
// ⚠️ 上游按后缀判格式（2026-11-19 真机实测：.m4a 被拒，提示只收 wav/mp3），
//	且这个拒绝发生在建单之后 —— 本地必须提前拦。
// ────────────────────────────────────────────────────────────────────────────

func TestCollectMedia_音频m4a被拒(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://cdn.example.com/a.png"},
			{Type: roleReferenceAudio, URL: "https://cdn.example.com/a.m4a"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "m4a")
	assert.Contains(t, err.Error(), "wav")
}

func TestCollectMedia_音频mp3通过(t *testing.T) {
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://cdn.example.com/a.png"},
			{Type: roleReferenceAudio, URL: "https://cdn.example.com/a.mp3"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, bundle.Audios, 1)
}

func TestCollectMedia_带签名参数仍能判后缀(t *testing.T) {
	_, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://cdn.example.com/a.png?sig=xyz&expires=123"},
			{Type: roleReferenceVideo, URL: "https://cdn.example.com/b.avi?sig=xyz"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "avi")
}

func TestCollectMedia_无后缀不拦(t *testing.T) {
	// 无后缀的签名 URL 很常见（部分 CDN 用 /object/<id> 形态），
	// 本地过度拦截会误杀合法素材 —— 取不到后缀就放行。
	bundle, err := collectMedia(relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: roleReferenceImage, URL: "https://cdn.example.com/object/abc"},
			{Type: roleReferenceAudio, URL: "https://cdn.example.com/object/def"},
		},
	})
	require.NoError(t, err)
	assert.Len(t, bundle.Audios, 1)
}

// ────────────────────────────────────────────────────────────────────────────
// DoResponse 必须写出响应体
//
// ⚠️ 渠道框架不替适配器回写 body。缺 c.JSON 时客户端拿到 HTTP 200 + 空 body，
//	连 task id 都取不到 —— 2026-11-19 真机踩过。
// ────────────────────────────────────────────────────────────────────────────

func TestDoResponse_写出OpenAIVideo响应体(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	a := newAdaptor()
	respBody := []byte(`{"id":"up-1","task_id":"up-1","status":"pending"}`)
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(respBody))}

	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_1"},
		OriginModelName: ModelNameTJ,
	}

	upstreamID, _, taskErr := a.DoResponse(c, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "up-1", upstreamID)

	require.Equal(t, 200, recorder.Code)
	body := recorder.Body.String()
	require.NotEmpty(t, body, "响应体不能为空：客户端要靠它拿 task id")

	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out))
	assert.Equal(t, "task_public_1", out["id"])
	assert.Equal(t, "video", out["object"])
	// 对外不能出现上游单号。
	assert.NotContains(t, body, "up-1")
}
