package megabyai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// wan30ChainResult 是一次「客户端原始 JSON → 本站解析 → 上游提交体 + 计费倍率」的完整结果。
type wan30ChainResult struct {
	body    map[string]any
	ratios  map[string]float64
	taskErr *dto.TaskError
}

func init() {
	gin.SetMode(gin.TestMode)
}

// runWan30Chain 复刻生产真实链路（对外模型名固定为 wan-3.0）。
func runWan30Chain(t *testing.T, raw string) wan30ChainResult {
	t.Helper()
	return runChainWithModel(t, raw, wan30Model, wan30Model)
}

// runChainWithModel 复刻生产真实链路，可指定对外模型名与渠道映射后的上游模型名：
//
//	原始 JSON body → Sora 的 ValidateMultipartDirect（解析 + 落 context）
//	              → megabyai 的 ValidateRequestAndSetAction（分辨率归一 + 素材校验）
//	              → BuildRequestBody（翻译成上游统一参数）
//	              → EstimateBilling（计费倍率）
//
// 全部是本地计算，不发任何上游请求、不产生任何计费。
// upstreamModel 为空表示渠道没配 model_mapping（靠代码兜底表）。
func runChainWithModel(t *testing.T, raw, originModel, upstreamModel string) wan30ChainResult {
	t.Helper()

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.KeyRequestBody, []byte(raw))

	info := &relaycommon.RelayInfo{
		OriginModelName: originModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: upstreamModel},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}

	adaptor := &TaskAdaptor{}

	// 第一段：解析 + 落 context（megabyai 复用 Sora 的这段）
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return wan30ChainResult{taskErr: taskErr}
	}
	// 第二段：本渠道自己的分辨率归一 + 素材上限校验
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return wan30ChainResult{taskErr: taskErr}
	}
	// 第三段：翻译成上游统一参数
	reader, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(reader)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(encoded, &body))

	// 第四段：计费倍率（必须与下发的分辨率同档）
	return wan30ChainResult{body: body, ratios: adaptor.EstimateBilling(c, info)}
}

// TestWan30ParameterChainEndToEnd 逐个参数走完整链路，钉住「客户端写法 → 上游字段」的映射。
//
// 为什么要这条测试：原有测试手工构造 TaskSubmitReq，**绕过了 JSON 解析器**，
// 而解析器才是「客户端到底怎么写」的唯一真相（字段类型、别名、合并规则都在那里）。
func TestWan30ParameterChainEndToEnd(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		// 期望的上游提交体字段（逐字段断言，没出现在 expected 里的键一律不许存在）
		expected map[string]any
		// 期望的计费 size 倍率；nil 表示本次不该走到计费
		expectedSize *float64
		expectErrCode string
	}{
		{
			name: "最小请求：没给分辨率就落 480p 档并显式下发",
			raw:  `{"model":"wan-3.0","prompt":"一只橘猫"}`,
			expected: map[string]any{
				"model":      "wan-3.0",
				"prompt":     "一只橘猫",
				"resolution": "480p",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "duration 传数字",
			raw:  `{"model":"wan-3.0","prompt":"p","duration":30}`,
			expected: map[string]any{
				"duration":   float64(30),
				"resolution": "480p",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "duration 传字符串",
			raw:  `{"model":"wan-3.0","prompt":"p","duration":"30"}`,
			expected: map[string]any{
				"duration":   float64(30),
				"resolution": "480p",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "seconds 传字符串（Sora 风格）",
			raw:  `{"model":"wan-3.0","prompt":"p","seconds":"15"}`,
			expected: map[string]any{
				"duration":   float64(15),
				"resolution": "480p",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			// 数字型 seconds 以前会让整包反序列化失败 → 客户端拿到 400 invalid_json，
			// 表现为「参数没传进去」。现在必须和字符串写法等价。
			name: "seconds 传数字（第三方客户端常见写法）",
			raw:  `{"model":"wan-3.0","prompt":"p","seconds":15}`,
			expected: map[string]any{
				"duration":   float64(15),
				"resolution": "480p",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "resolution 720p：下发与计费同档",
			raw:  `{"model":"wan-3.0","prompt":"p","resolution":"720p"}`,
			expected: map[string]any{
				"resolution": "720p",
			},
			expectedSize: float64Ptr(3.5 / 3.0),
		},
		{
			name: "resolution 1080p：下发与计费同档",
			raw:  `{"model":"wan-3.0","prompt":"p","resolution":"1080p"}`,
			expected: map[string]any{
				"resolution": "1080p",
			},
			expectedSize: float64Ptr(4.0 / 3.0),
		},
		{
			name: "size 传 WxH：同时推出分辨率档与宽高比",
			raw:  `{"model":"wan-3.0","prompt":"p","size":"1920x1080"}`,
			expected: map[string]any{
				"resolution": "1080p",
				"ratio":      "16:9",
			},
			expectedSize: float64Ptr(4.0 / 3.0),
		},
		{
			name: "size 传竖屏 WxH：比例取竖屏",
			raw:  `{"model":"wan-3.0","prompt":"p","size":"720x1280"}`,
			expected: map[string]any{
				"resolution": "720p",
				"ratio":      "9:16",
			},
			expectedSize: float64Ptr(3.5 / 3.0),
		},
		{
			name: "metadata 里带分辨率与比例",
			raw:  `{"model":"wan-3.0","prompt":"p","metadata":{"resolution":"1080p","ratio":"9:16"}}`,
			expected: map[string]any{
				"resolution": "1080p",
				"ratio":      "9:16",
			},
			expectedSize: float64Ptr(4.0 / 3.0),
		},
		{
			name: "顶层 ratio 透传（通用入参里没有 ratio 字段）",
			raw:  `{"model":"wan-3.0","prompt":"p","ratio":"9:16"}`,
			expected: map[string]any{
				"ratio": "9:16",
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "images 转成 referenceImages",
			raw:  `{"model":"wan-3.0","prompt":"p","images":["https://e.com/a.png","https://e.com/b.png"]}`,
			expected: map[string]any{
				"referenceImages": []any{"https://e.com/a.png", "https://e.com/b.png"},
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "media 按类型拆成三组",
			raw: `{"model":"wan-3.0","prompt":"p","media":[` +
				`{"type":"reference_image","url":"https://e.com/a.png"},` +
				`{"type":"first_frame","url":"https://e.com/f.png"},` +
				`{"type":"reference_video","url":"https://e.com/v.mp4"},` +
				`{"type":"reference_audio","url":"https://e.com/a.mp3"}]}`,
			expected: map[string]any{
				"referenceImages": []any{"https://e.com/a.png", "https://e.com/f.png"},
				"referenceVideos": []any{"https://e.com/v.mp4"},
				"referenceAudios": []any{"https://e.com/a.mp3"},
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "audios 数组转成 referenceAudios",
			raw:  `{"model":"wan-3.0","prompt":"p","audios":["https://e.com/a.mp3"]}`,
			expected: map[string]any{
				"referenceAudios": []any{"https://e.com/a.mp3"},
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			// 平台的 Sora 语义是「input_reference 是单图字段，存在就覆盖整个 images 数组」。
			// wan-3.0 支持多参考图，两者并存时一张都不能丢。
			name: "images 与 input_reference 并存：两张都要带",
			raw:  `{"model":"wan-3.0","prompt":"p","images":["https://e.com/a.png"],"input_reference":"https://e.com/b.png"}`,
			expected: map[string]any{
				"referenceImages": []any{"https://e.com/a.png", "https://e.com/b.png"},
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "seed 传数字",
			raw:  `{"model":"wan-3.0","prompt":"p","seed":42}`,
			expected: map[string]any{
				"seed": float64(42),
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name: "seed 传字符串",
			raw:  `{"model":"wan-3.0","prompt":"p","seed":"42"}`,
			expected: map[string]any{
				"seed": float64(42),
			},
			expectedSize: float64Ptr(1.0),
		},
		{
			name:         "非法分辨率必须 400（不能猜档位）",
			raw:          `{"model":"wan-3.0","prompt":"p","resolution":"4k"}`,
			expectErrCode: "invalid_resolution",
		},
		{
			name:         "素材超上限必须 400",
			raw:          `{"model":"wan-3.0","prompt":"p","images":["https://e.com/1.png","https://e.com/2.png","https://e.com/3.png","https://e.com/4.png","https://e.com/5.png","https://e.com/6.png","https://e.com/7.png","https://e.com/8.png","https://e.com/9.png","https://e.com/10.png","https://e.com/11.png"]}`,
			expectErrCode: "invalid_input_reference",
		},
		{
			// 上游要自己抓素材，data: URI 它抓不到 → 必然失败，必须提前 400。
			name:         "data URI 素材必须 400（上游抓不到）",
			raw:          `{"model":"wan-3.0","prompt":"p","images":["data:image/png;base64,AAAA"]}`,
			expectErrCode: "invalid_input_reference",
		},
		{
			name:         "没有 host 的本地路径素材必须 400",
			raw:          `{"model":"wan-3.0","prompt":"p","images":["/tmp/local.png"]}`,
			expectErrCode: "invalid_input_reference",
		},
		{
			name:         "缺 prompt 必须 400",
			raw:          `{"model":"wan-3.0","duration":30}`,
			expectErrCode: "invalid_request",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result := runWan30Chain(t, testCase.raw)

			if testCase.expectErrCode != "" {
				require.NotNil(t, result.taskErr, "期望报错 %s，实际通过", testCase.expectErrCode)
				require.Equal(t, testCase.expectErrCode, result.taskErr.Code)
				return
			}

			require.Nil(t, result.taskErr)
			for key, value := range testCase.expected {
				require.Equal(t, value, result.body[key], "字段 %s", key)
			}
			// 未在 expected 里声明的键一律不许出现（防止把客户端原始字段透传给上游）
			// model/prompt/resolution 是必然存在的三个字段
			allowed := map[string]bool{"model": true, "prompt": true, "resolution": true}
			for key := range testCase.expected {
				allowed[key] = true
			}
			for key := range result.body {
				require.True(t, allowed[key], "上游提交体出现了未声明字段 %s", key)
			}
			// 上游统一参数契约：不得混入经典字段
			for _, forbidden := range []string{"seconds", "size", "images", "input_reference", "media"} {
				require.NotContains(t, result.body, forbidden)
			}
			// 计费必须与下发的分辨率同档，且不得出现 seconds 倍率
			require.NotNil(t, testCase.expectedSize)
			require.Equal(t, map[string]float64{"size": *testCase.expectedSize}, result.ratios)
		})
	}
}

// TestWan30UpstreamPayloadNeverLeaksClientFields 单独钉一条：上游提交体里
// 不允许出现 seconds / size / images 这类经典字段（上游文档明确不要混用），
// 且计费里不得出现 seconds 倍率（上游按次计费）。
func TestWan30UpstreamPayloadNeverLeaksClientFields(t *testing.T) {
	result := runWan30Chain(t,
		`{"model":"wan-3.0","prompt":"p","duration":30,"resolution":"720p","ratio":"16:9",`+
			`"size":"1280x720","seconds":"30","images":["https://e.com/a.png"],`+
			`"input_reference":"https://e.com/b.png","seed":7}`)
	require.Nil(t, result.taskErr)

	require.Equal(t, "720p", result.body["resolution"])
	require.Equal(t, "16:9", result.body["ratio"])
	require.Equal(t, float64(30), result.body["duration"])
	require.Equal(t, float64(7), result.body["seed"])
	require.NotContains(t, result.body, "seconds")
	require.NotContains(t, result.body, "size")
	require.NotContains(t, result.body, "images")
	require.NotContains(t, result.body, "input_reference")

	require.Equal(t, map[string]float64{"size": 3.5 / 3.0}, result.ratios)
	require.NotContains(t, result.ratios, "seconds")
}

func float64Ptr(value float64) *float64 {
	return &value
}
