package hailuo

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UpstreamModelName 挂在 RelayInfo.ChannelMeta 上，不是顶层字段。
// TaskRelayInfo 是**内嵌指针**，生产路径由 relay_task.go 初始化；测试夹具必须
// 手动补上，否则 info.Action=... 会穿透 nil 指针 panic。
func relayInfoWithModel(m string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: m},
		OriginModelName: m,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
}

func specFor(t *testing.T, model string) V2ModelSpec {
	t.Helper()
	spec, ok := lookupV2Spec(model)
	require.True(t, ok, "model %s should be registered as v2", model)
	return spec
}

// v2 与 v1 的字段形状完全不同，端点选错就是 404/400。锁死分派。
func TestV2EndpointRouting(t *testing.T) {
	a := &TaskAdaptor{baseURL: "https://api.minimax.io"}

	got, err := a.BuildRequestURL(relayInfoWithModel(ModelH3))
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.io/v2/video_generation", got)

	got, err = a.BuildRequestURL(relayInfoWithModel(ModelH3Max))
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.io/v2/video_generation", got)

	// Context-IR 走自己的端点，但载荷 model 必须是 MiniMax-H3。
	got, err = a.BuildRequestURL(relayInfoWithModel(ModelH3ContextIR))
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.io/v2/h3_context_ir", got)

	// v1 模型不能被误分到 v2。
	got, err = a.BuildRequestURL(relayInfoWithModel("MiniMax-Hailuo-2.3"))
	require.NoError(t, err)
	assert.Equal(t, "https://api.minimax.io/v1/video_generation", got)
}

// Context-IR 的载荷 model 与对外模型名不同，这是最容易写错的一处。
func TestContextIRPayloadUsesUpstreamModelName(t *testing.T) {
	a := &TaskAdaptor{}
	req := relaycommon.TaskSubmitReq{
		Prompt:         "描述这个视频",
		InputReference: "https://cdn.example.com/clip.mp4",
		Duration:       5,
		Metadata:       map[string]interface{}{"ratio": "16:9"},
	}
	payload, err := a.convertToV2Payload(&req, relayInfoWithModel(ModelH3ContextIR))
	require.NoError(t, err)
	assert.Equal(t, ModelH3, payload.Model, "Context-IR 端点只接受 MiniMax-H3")
	require.Len(t, payload.Content, 2)
	assert.Equal(t, "text", payload.Content[0].Type)
	assert.Equal(t, "video_url", payload.Content[1].Type)
	assert.Equal(t, roleReferenceVideo, payload.Content[1].Role)
	assert.Equal(t, 5, payload.Duration)
}

// content[] 组装：首尾帧 role、参考图、参考视频/音频。
func TestBuildV2ContentRoles(t *testing.T) {
	t.Run("单图默认当首帧", func(t *testing.T) {
		items := buildV2Content(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Images: []string{"https://cdn.example.com/a.jpg"},
		})
		require.Len(t, items, 2)
		assert.Equal(t, "image_url", items[1].Type)
		assert.Equal(t, roleFirstFrame, items[1].Role)
	})

	t.Run("多图当参考图", func(t *testing.T) {
		items := buildV2Content(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Images: []string{"https://cdn.example.com/a.jpg", "https://cdn.example.com/b.jpg"},
		})
		require.Len(t, items, 3)
		assert.Equal(t, roleReferenceImage, items[1].Role)
		assert.Equal(t, roleReferenceImage, items[2].Role)
	})

	t.Run("media 显式标注尾帧优先于启发式", func(t *testing.T) {
		items := buildV2Content(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Media: []relaycommon.TaskMedia{
				{Type: "first_frame", URL: "https://cdn.example.com/a.jpg"},
				{Type: "last_frame", URL: "https://cdn.example.com/b.jpg"},
			},
		})
		require.Len(t, items, 3)
		assert.Equal(t, roleFirstFrame, items[1].Role)
		assert.Equal(t, roleLastFrame, items[2].Role)
	})

	t.Run("URL 片段标注也生效", func(t *testing.T) {
		items := buildV2Content(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Images: []string{"https://cdn.example.com/a.jpg#last_frame"},
		})
		require.Len(t, items, 2)
		assert.Equal(t, roleLastFrame, items[1].Role)
	})

	t.Run("音频按扩展名识别为 reference_audio", func(t *testing.T) {
		items := buildV2Content(&relaycommon.TaskSubmitReq{
			Prompt:         "p",
			InputReference: "https://cdn.example.com/voice.mp3",
		})
		require.Len(t, items, 2)
		assert.Equal(t, "audio_url", items[1].Type)
		assert.Equal(t, roleReferenceAudio, items[1].Role)
	})
}

// 上游明确拒绝首尾帧与参考素材混用，必须在本地拦下并说清原因。
func TestValidateV2MediaRejectsMixedFrameAndReference(t *testing.T) {
	err := validateV2Media(&relaycommon.TaskSubmitReq{
		Prompt: "p",
		Media: []relaycommon.TaskMedia{
			{Type: "first_frame", URL: "https://cdn.example.com/a.jpg"},
			{Type: "reference_image", URL: "https://cdn.example.com/b.jpg"},
		},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestValidateV2MediaLimits(t *testing.T) {
	t.Run("参考图超过 9 张", func(t *testing.T) {
		images := make([]string, 0, 10)
		for i := 0; i < 10; i++ {
			images = append(images, "https://cdn.example.com/a.jpg")
		}
		err := validateV2Media(&relaycommon.TaskSubmitReq{Prompt: "p", Images: images})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 9 reference images")
	})

	t.Run("首帧超过 1 张", func(t *testing.T) {
		err := validateV2Media(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Media: []relaycommon.TaskMedia{
				{Type: "first_frame", URL: "https://cdn.example.com/a.jpg"},
				{Type: "first_frame", URL: "https://cdn.example.com/b.jpg"},
			},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 1 first_frame")
	})

	t.Run("非 http(s) 素材被拒（远端上游取不到 data: URI）", func(t *testing.T) {
		err := validateV2Media(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Images: []string{"data:image/png;base64,AAAA"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "public http(s) URL")
	})

	t.Run("合法组合通过", func(t *testing.T) {
		require.NoError(t, validateV2Media(&relaycommon.TaskSubmitReq{
			Prompt: "p",
			Media: []relaycommon.TaskMedia{
				{Type: "first_frame", URL: "https://cdn.example.com/a.jpg"},
				{Type: "last_frame", URL: "https://cdn.example.com/b.jpg"},
			},
		}))
	})
}

// 时长/分辨率越界必须 fail-loud，绝不静默改档（否则交付一个参数不符的成片）。
func TestValidateV2RequestCapabilityGates(t *testing.T) {
	spec := specFor(t, ModelH3)

	t.Run("时长越界", func(t *testing.T) {
		taskErr := validateV2Request(&relaycommon.TaskSubmitReq{
			Prompt: "p", Duration: 20, Metadata: map[string]interface{}{"ratio": "16:9"},
		}, spec)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
		assert.Contains(t, taskErr.Message, "duration must be between 4 and 15")
		assert.True(t, taskErr.LocalError)
	})

	t.Run("H3 不支持 480P", func(t *testing.T) {
		taskErr := validateV2Request(&relaycommon.TaskSubmitReq{
			Prompt: "p", Resolution: "480P", Metadata: map[string]interface{}{"ratio": "16:9"},
		}, spec)
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "resolution must be one of 768P/2K")
	})

	t.Run("H3-Max 不支持 2K", func(t *testing.T) {
		taskErr := validateV2Request(&relaycommon.TaskSubmitReq{
			Prompt: "p", Resolution: "2K", Metadata: map[string]interface{}{"ratio": "16:9"},
		}, specFor(t, ModelH3Max))
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "resolution must be one of 480P/768P")
	})

	t.Run("H3-Max 最短 5 秒", func(t *testing.T) {
		taskErr := validateV2Request(&relaycommon.TaskSubmitReq{
			Prompt: "p", Duration: 4, Metadata: map[string]interface{}{"ratio": "16:9"},
		}, specFor(t, ModelH3Max))
		require.NotNil(t, taskErr)
		assert.Contains(t, taskErr.Message, "duration must be between 5 and 15")
	})

	t.Run("合法请求通过", func(t *testing.T) {
		require.Nil(t, validateV2Request(&relaycommon.TaskSubmitReq{
			Prompt: "p", Duration: 8, Resolution: "768P",
			Metadata: map[string]interface{}{"ratio": "16:9"},
		}, spec))
	})
}

// 文生视频必须显式给比例：上游不接受 adaptive 的 t2va。
func TestTextToVideoRequiresExplicitRatio(t *testing.T) {
	a := &TaskAdaptor{}
	_, err := a.convertToV2Payload(
		&relaycommon.TaskSubmitReq{Prompt: "p"}, relayInfoWithModel(ModelH3))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ratio is required for text-to-video")

	// 有图时可以用 adaptive（跟随首帧比例）。
	payload, err := a.convertToV2Payload(&relaycommon.TaskSubmitReq{
		Prompt: "p", Images: []string{"https://cdn.example.com/a.jpg"},
	}, relayInfoWithModel(ModelH3))
	require.NoError(t, err)
	assert.Equal(t, "adaptive", payload.Ratio)
}

func TestResolveV2RatioFromSizeAndMetadata(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{"metadata ratio", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "9:16"}}, "9:16"},
		{"metadata aspect_ratio", relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"aspect_ratio": "1:1"}}, "1:1"},
		{"size 竖版", relaycommon.TaskSubmitReq{Size: "1080x1920"}, "9:16"},
		{"size 横版", relaycommon.TaskSubmitReq{Size: "1920x1080"}, "16:9"},
		{"size 方形", relaycommon.TaskSubmitReq{Size: "1024x1024"}, "1:1"},
		{"缺省", relaycommon.TaskSubmitReq{}, "adaptive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveV2Ratio(&tc.req)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveV2Resolution(t *testing.T) {
	spec := specFor(t, ModelH3)
	t.Run("缺省用默认", func(t *testing.T) {
		got, err := resolveV2Resolution(&relaycommon.TaskSubmitReq{}, spec)
		require.NoError(t, err)
		assert.Equal(t, Resolution768P, got)
	})
	t.Run("小写也认", func(t *testing.T) {
		got, err := resolveV2Resolution(&relaycommon.TaskSubmitReq{Resolution: "2k"}, spec)
		require.NoError(t, err)
		assert.Equal(t, Resolution2K, got)
	})
	t.Run("越界报错不回落到默认", func(t *testing.T) {
		_, err := resolveV2Resolution(&relaycommon.TaskSubmitReq{Resolution: "4K"}, spec)
		require.Error(t, err)
	})
}

func TestGetModelListIncludesH3Family(t *testing.T) {
	a := &TaskAdaptor{}
	list := a.GetModelList()
	assert.Contains(t, list, ModelH3)
	assert.Contains(t, list, ModelH3Max)
	assert.Contains(t, list, ModelH3ContextIR)
	assert.Contains(t, list, "MiniMax-Hailuo-2.3", "v1 模型不能被移除")
}

// v2 按秒计价：4s 和 15s 不能收一样的钱，2K 必须比 768P 贵。
func TestEstimateBillingV2PerSecondAndResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)
	billingCtx := func(req relaycommon.TaskSubmitReq) *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Set("task_request", req)
		return c
	}
	ratioMeta := map[string]interface{}{"ratio": "16:9"}

	t.Run("按秒 + 基础档倍率 1.0", func(t *testing.T) {
		got := (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 10, Metadata: ratioMeta}),
			relayInfoWithModel(ModelH3))
		assert.Equal(t, 10.0, got["seconds"])
		assert.Equal(t, 1.0, got["size"])
	})

	t.Run("2K 走 1.625 倍率", func(t *testing.T) {
		got := (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 5, Resolution: "2K", Metadata: ratioMeta}),
			relayInfoWithModel(ModelH3))
		assert.Equal(t, 5.0, got["seconds"])
		assert.Equal(t, 1.625, got["size"])
	})

	t.Run("H3-Max 480P 是基础档，768P 上浮", func(t *testing.T) {
		base := (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 8, Resolution: "480P", Metadata: ratioMeta}),
			relayInfoWithModel(ModelH3Max))
		assert.Equal(t, 1.0, base["size"])
		higher := (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 8, Resolution: "768P", Metadata: ratioMeta}),
			relayInfoWithModel(ModelH3Max))
		assert.Equal(t, 1.6, higher["size"])
	})

	t.Run("Context-IR 不按时长计费", func(t *testing.T) {
		assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 15, Metadata: ratioMeta}),
			relayInfoWithModel(ModelH3ContextIR)))
	})

	t.Run("v1 模型保持原行为（按次）", func(t *testing.T) {
		assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(
			billingCtx(relaycommon.TaskSubmitReq{Duration: 10}),
			relayInfoWithModel("MiniMax-Hailuo-2.3")))
	})

	t.Run("info 为 nil 不 panic", func(t *testing.T) {
		assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(billingCtx(relaycommon.TaskSubmitReq{}), nil))
	})
}

// 回归：v2 校验必须把请求写回 context。
// 漏掉的症状有两个，而且第二个是静默的：
//  1. BuildRequestBody 报 "request not found in context"（500）；
//  2. EstimateBilling 拿不到 duration → 返回 nil → 10 秒的片子按 1 秒计费。
func TestValidateV2StoresTaskRequestInContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"prompt":"a cat","duration":10,"resolution":"2K","metadata":{"ratio":"16:9"}}`

	newCtx := func() *gin.Context {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
		c.Request.Header.Set("Content-Type", "application/json")
		return c
	}

	c := newCtx()
	info := relayInfoWithModel(ModelH3)
	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info))

	stored, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err, "校验后必须能从 context 取回请求")
	assert.Equal(t, 10, stored.Duration)
	assert.Equal(t, "2K", stored.Resolution)

	// 计费能拿到 duration → 按秒倍率生效
	billing := (&TaskAdaptor{}).EstimateBilling(c, info)
	require.NotNil(t, billing)
	assert.Equal(t, 10.0, billing["seconds"])
	assert.Equal(t, 1.625, billing["size"])
}

// 单图写法（image 而非 images）要与共享路径一致地归一。
func TestValidateV2NormalizesSingleImageField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := `{"prompt":"a cat","image":"https://x.example/a.jpg","metadata":{"ratio":"16:9"}}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	require.Nil(t, (&TaskAdaptor{}).ValidateRequestAndSetAction(c, relayInfoWithModel(ModelH3)))
	stored, err := relaycommon.GetTaskRequest(c)
	require.NoError(t, err)
	assert.Equal(t, []string{"https://x.example/a.jpg"}, stored.Images)
}
