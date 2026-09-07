package youzanwan3

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func onePixelPNG(t *testing.T) string {
	t.Helper()
	var buffer bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&buffer, img))
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buffer.Bytes())
}

func TestBuildRequestBodyUploadsWan3AssetsAndMapsMentions(t *testing.T) {
	assetIndex := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/api/conversations":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"id":"conversation-1"}`))
		case "/api/multimodal-assets":
			assetIndex++
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"asset":{"id":"asset-` + string(rune('0'+assetIndex)) + `","displayAlias":"asset` + string(rune('0'+assetIndex)) + `"}}`))
		default:
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
	}))
	defer server.Close()

	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "test-key"}
	body, err := adaptor.buildRequestBody(relaycommon.TaskSubmitReq{
		Model:  "wan3.0-video-prime",
		Prompt: "让@图片1配合@音频1运动",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: onePixelPNG(t)},
			{Type: "reference_audio", URL: "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString([]byte("audio"))},
		},
		Duration: 5,
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Equal(t, "wan3.0-video-prime", payload["model"])
	require.Equal(t, "conversation-1", payload["conversationId"])
	require.Contains(t, payload["prompt"], "@asset1")
	require.Contains(t, payload["prompt"], "@asset2")
	require.Len(t, payload["mentions"], 2)
}

func TestGetModelListExposesOnlyRequestedWan3Models(t *testing.T) {
	adaptor := &TaskAdaptor{}
	require.Equal(t, []string{"wan3.0-video", "wan3.0-video-prime"}, adaptor.GetModelList())
}

func TestParseTaskResultReadsYouzanResult(t *testing.T) {
	adaptor := &TaskAdaptor{}
	result, err := adaptor.ParseTaskResult([]byte(`{"status":"succeeded","result":{"url":"https://cdn.example/video.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/video.mp4", result.Url)
	require.Equal(t, "100%", result.Progress)
}
