package autodl

import (
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func autoDLTaskContext(req relaycommon.TaskSubmitReq) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", req)
	return c
}

func autoDLRelayInfo(model string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta:     &relaycommon.ChannelMeta{},
	}
}

func TestBuildRequestBodyExpandsAllAutoDLReferenceImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:multiref-video-1")
	adaptor.Init(info)

	images := make([]string, 9)
	for i := range images {
		images[i] = " https://example.com/reference-" + strconv.Itoa(i+1) + ".png "
	}

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "animate the references",
		Images: images,
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, "animate the references", payload["prompt"])
	require.Equal(t, "768p竖", payload["resolution"])
	for i := range images {
		key := "ref_image_" + strconv.Itoa(i)
		require.Equal(t, "https://example.com/reference-"+strconv.Itoa(i+1)+".png", payload[key])
	}
}

func TestBuildRequestBodyForwardsAutoDLOptionalParameters(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:multiref-video-1")
	adaptor.Init(info)
	seed := int64(999999999999999)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:     "animate the references",
		Duration:   10,
		Resolution: "768p横",
		Seed:       &seed,
		Images:     []string{"https://example.com/reference.png"},
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, float64(10), payload["duration"])
	require.Equal(t, "768p横", payload["resolution"])
	require.Equal(t, float64(seed), payload["seed"])
}

func TestEstimateBillingChargesAutoDLResolutionRatio(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:multiref-video-1")
	adaptor.Init(info)

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    1.0,
	}, adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "768p竖",
	}), info))
}

func TestBuildRequestBodyUsesAutoDLWorkflowSpecificLimits(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:h3-video")
	adaptor.Init(info)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:   "text only",
		Duration: 15,
	}), info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, float64(15), payload["duration"])
	require.Equal(t, "768p竖", payload["resolution"])

	seed := int64(1)
	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "text only",
		Seed:   &seed,
	}), info)
	require.ErrorContains(t, err, "does not support seed")

	info = autoDLRelayInfo("autodl:multiref-video-3")
	adaptor.Init(info)
	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:     "reference",
		Resolution: "1080p竖",
		Images:     []string{"https://example.com/reference.png"},
	}), info)
	require.ErrorContains(t, err, "does not support resolution")

	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: strings.Repeat("a", 10001),
		Images: []string{"https://example.com/reference.png"},
	}), info)
	require.ErrorContains(t, err, "supports prompts up to 10000 characters")
}

func TestBuildRequestBodyRejectsTooManyAutoDLReferenceImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:multiref-video-1")
	adaptor.Init(info)

	_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "too many references",
		Images: []string{
			"https://example.com/1.png",
			"https://example.com/2.png",
			"https://example.com/3.png",
			"https://example.com/4.png",
			"https://example.com/5.png",
			"https://example.com/6.png",
			"https://example.com/7.png",
			"https://example.com/8.png",
			"https://example.com/9.png",
			"https://example.com/10.png",
		},
	}), info)
	require.ErrorContains(t, err, "supports at most 9 reference images")
}

func TestBuildRequestBodyRejectsReferenceImageForAutoDLTextModel(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:h3-video")
	adaptor.Init(info)

	_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "text only",
		Images: []string{"https://example.com/reference.png"},
	}), info)
	require.ErrorContains(t, err, "does not support reference images")
}

func TestDeriveResolutionFromSize(t *testing.T) {
	tests := []struct {
		name        string
		size        string
		allowed     []string
		want        string
		wantOk      bool
	}{
		{"vertical 768p", "768x1280", []string{"480p竖", "768p竖", "480p横", "768p横"}, "768p竖", true},
		{"horizontal 768p", "1280x768", []string{"480p竖", "768p竖", "480p横", "768p横"}, "768p横", true},
		{"vertical 480p", "480x854", []string{"480p竖", "768p竖", "480p横", "768p横"}, "480p竖", true},
		{"square 768p", "768x768", []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"}, "768p(1:1)", true},
		{"1080p vertical falls back to nearest 768p", "1080x1920", []string{"480p竖", "768p竖", "480p横", "768p横"}, "768p竖", true},
		{"720p short edge maps to nearest 768p", "720x1280", []string{"480p竖", "768p竖", "480p横", "768p横"}, "768p竖", true},
		{"736p only tier", "736x1280", []string{"736p竖", "736p横", "736p(1:1)"}, "736p竖", true},
		{"736p square", "736x736", []string{"736p竖", "736p横", "736p(1:1)"}, "736p(1:1)", true},
		{"no matching suffix falls back to any", "768x768", []string{"768p竖", "768p横"}, "768p竖", true},
		{"invalid size returns false", "invalid", []string{"768p竖"}, "", false},
		{"empty allowed returns false", "768x1280", []string{}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := deriveResolutionFromSize(tt.size, tt.allowed)
			require.Equal(t, tt.wantOk, ok)
			if ok {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func TestBuildRequestBodyDerivesResolutionFromSize(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:h3-video")
	adaptor.Init(info)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "text only",
		Size:   "768x1280",
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, "768p竖", payload["resolution"])
}

func TestEstimateBillingDerivesResolutionFromSize(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:multiref-video-1")
	adaptor.Init(info)

	billing := adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration: 5,
		Size:     "720x1280",
	}), info)
	require.Equal(t, 1.0, billing["size"])
}
