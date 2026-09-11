package autodl

import (
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
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
	info := autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
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
	info := autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
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

func TestBuildRequestBodyExpandsAutoDLReferenceImagesAndAudios(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-u24")
	adaptor.Init(info)
	seed := int64(0)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:   "animate with the soundtrack",
		Duration: 15,
		Images:   []string{"https://example.com/first.png", "https://example.com/second.png"},
		Audios:   []string{"https://example.com/voice.mp3", "https://example.com/music.wav"},
		Seed:     &seed,
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, "animate with the soundtrack", payload["prompt"])
	require.Equal(t, float64(15), payload["duration"])
	require.Equal(t, float64(0), payload["seed"])
	require.Equal(t, "https://example.com/first.png", payload["ref_image_0"])
	require.Equal(t, "https://example.com/second.png", payload["ref_image_1"])
	require.Equal(t, "https://example.com/voice.mp3", payload["ref_audio_0"])
	require.Equal(t, "https://example.com/music.wav", payload["ref_audio_1"])
}

func TestBuildRequestBodySupportsOptionalMediaForAutoDLV2(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-image-audio-10s")
	adaptor.Init(info)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:     "animate the portrait",
		Resolution: "1080p横",
		Images:     []string{"https://example.com/portrait.png"},
		Audios:     []string{"https://example.com/dialogue.flac"},
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, "1080p横", payload["resolution"])
	require.Equal(t, "https://example.com/portrait.png", payload["ref_image_0"])
	require.Equal(t, "https://example.com/dialogue.flac", payload["ref_audio_0"])
}

func TestBuildRequestBodyUsesAutoDLAudioDurationForSyncWorkflow(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-lipsync")
	adaptor.Init(info)

	body, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		AudioDuration: common.GetPointer(12),
		Images:        []string{"https://example.com/portrait.png"},
		Audios:        []string{"https://example.com/speech.mp3"},
	}), info)
	require.NoError(t, err)

	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, float64(12), payload["audio_duration"])
	require.NotContains(t, payload, "duration")
	require.NotContains(t, payload, "prompt")
	require.Equal(t, "https://example.com/portrait.png", payload["ref_image_0"])
	require.Equal(t, "https://example.com/speech.mp3", payload["ref_audio_0"])
}

func TestBuildRequestBodyPassesOfficialParametersForEveryAutoDLWorkflow(t *testing.T) {
	seedZero := int64(0)
	seedOne := int64(1)
	seedMax := int64(999999999999999)
	allImages := []string{
		"https://example.com/image-1.png",
		"https://example.com/image-2.png",
		"https://example.com/image-3.png",
		"https://example.com/image-4.png",
		"https://example.com/image-5.png",
		"https://example.com/image-6.png",
		"https://example.com/image-7.png",
		"https://example.com/image-8.png",
		"https://example.com/image-9.png",
	}
	allAudios := []string{
		"https://example.com/audio-1.mp3",
		"https://example.com/audio-2.wav",
		"https://example.com/audio-3.flac",
	}

	tests := []struct {
		name       string
		model      string
		request    relaycommon.TaskSubmitReq
		want       map[string]any
		wantAbsent []string
	}{
		{
			name:  "u24 quality priority",
			model: "autodl:minimax-h3-u24",
			request: relaycommon.TaskSubmitReq{
				Prompt:     "combine all references",
				Duration:   15,
				Resolution: "768p(1:1)",
				Images:     allImages,
				Audios:     allAudios,
				Seed:       &seedZero,
			},
			want: map[string]any{
				"prompt":     "combine all references",
				"duration":   float64(15),
				"resolution": "768p(1:1)",
				"seed":       float64(0),
			},
		},
		{
			name:  "u08 speed priority",
			model: "autodl:minimax-h3-u08",
			request: relaycommon.TaskSubmitReq{
				Prompt:     "generate the fast variant",
				Duration:   1,
				Resolution: "480p横",
				Images:     []string{allImages[0]},
				Audios:     []string{allAudios[0]},
				Seed:       &seedZero,
			},
			want: map[string]any{
				"prompt":     "generate the fast variant",
				"duration":   float64(1),
				"resolution": "480p横",
				"seed":       float64(0),
			},
		},
		{
			name:  "v2 optional media and 1080p",
			model: "autodl:minimax-h3-image-audio-10s",
			request: relaycommon.TaskSubmitReq{
				Prompt:     "animate the optional references",
				Duration:   10,
				Resolution: "1080p横",
				Images:     allImages,
				Audios:     allAudios,
				Seed:       &seedOne,
			},
			want: map[string]any{
				"prompt":     "animate the optional references",
				"duration":   float64(10),
				"resolution": "1080p横",
				"seed":       float64(1),
			},
		},
		{
			name:  "v2 15s without 1080p",
			model: "autodl:minimax-h3-image-audio-15s",
			request: relaycommon.TaskSubmitReq{
				Prompt:     "animate the 15 second scene",
				Duration:   15,
				Resolution: "480p横",
				Images:     []string{allImages[0]},
				Audios:     []string{allAudios[0]},
				Seed:       &seedMax,
			},
			want: map[string]any{
				"prompt":     "animate the 15 second scene",
				"duration":   float64(15),
				"resolution": "480p横",
				"seed":       float64(seedMax),
			},
		},
		{
			name:  "audio sync",
			model: "autodl:minimax-h3-lipsync",
			request: relaycommon.TaskSubmitReq{
				AudioDuration: common.GetPointer(15),
				Resolution:    "1080p横",
				Images:        []string{allImages[0]},
				Audios:        []string{allAudios[0]},
			},
			want: map[string]any{
				"audio_duration": float64(15),
				"resolution":     "1080p横",
			},
			wantAbsent: []string{"prompt", "duration", "seed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			info := autoDLRelayInfo(tt.model)
			adaptor.Init(info)

			body, err := adaptor.BuildRequestBody(autoDLTaskContext(tt.request), info)
			require.NoError(t, err)
			encoded, err := io.ReadAll(body)
			require.NoError(t, err)

			var payload map[string]any
			require.NoError(t, common.Unmarshal(encoded, &payload))
			require.Len(t, payload, len(tt.want)+len(tt.request.Images)+len(tt.request.Audios))
			for key, value := range tt.want {
				require.Equal(t, value, payload[key], key)
			}
			for i, image := range tt.request.Images {
				require.Equal(t, image, payload["ref_image_"+strconv.Itoa(i)])
			}
			for i, audio := range tt.request.Audios {
				require.Equal(t, audio, payload["ref_audio_"+strconv.Itoa(i)])
			}
			for _, key := range tt.wantAbsent {
				require.NotContains(t, payload, key)
			}
		})
	}
}

func TestBuildRequestBodyRejectsUnsupportedOrExcessAutoDLAudio(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-text-to-video")
	adaptor.Init(info)

	_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "text only",
		Audio:  "https://example.com/audio.mp3",
	}), info)
	require.ErrorContains(t, err, "does not support reference audio")

	info = autoDLRelayInfo("autodl:minimax-h3-lipsync")
	adaptor.Init(info)
	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Images: []string{"https://example.com/portrait.png"},
	}), info)
	require.ErrorContains(t, err, "requires at least one reference audio")

	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Audios: []string{"https://example.com/speech.mp3"},
	}), info)
	require.ErrorContains(t, err, "requires at least one reference image")

	_, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Images: []string{"https://example.com/portrait.png"},
		Audios: []string{
			"https://example.com/1.mp3",
			"https://example.com/2.mp3",
		},
	}), info)
	require.ErrorContains(t, err, "supports at most 1 reference audios")
}

func TestOldAutoDLModelNamesAreRejectedAfterRename(t *testing.T) {
	oldNames := []string{
		"autodl:h3-video",
		"autodl:multiref-video-1",
		"autodl:multiref-video-2",
		"autodl:multiref-video-3",
		"autodl:multiaudio-video-1",
		"autodl:multiaudio-video-2",
		"autodl:multiaudio-video-3",
		"autodl:multiaudio-video-4",
		"autodl:audio-sync-video",
	}

	for _, modelName := range oldNames {
		adaptor := &TaskAdaptor{}
		info := autoDLRelayInfo(modelName)
		adaptor.Init(info)

		_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
			Prompt: "old alias must not be routed",
			Images: []string{"https://example.com/reference.png"},
			Audios: []string{"https://example.com/reference.mp3"},
		}), info)
		require.ErrorContains(t, err, "not mapped to an AutoDL workflow_id", modelName)
	}
}

func TestAutoDLConfiguredAudioWorkflowsMatchOfficialSchemas(t *testing.T) {
	expected := map[string]struct {
		workflowID        string
		maxDuration       int
		maxPromptLength   int
		minSeed           int64
		maxSeed           int64
		maxImages         int
		maxAudios         int
		supportsImages    bool
		requiresImages    bool
		requiresAudios    bool
		promptSupported   bool
		promptRequired    bool
		resolutions       []string
		resolutionCount   int
		usesAudioDuration bool
	}{
		"autodl:minimax-h3-u24": {
			workflowID: "minimax_h3_zm_u24", maxDuration: 15, maxPromptLength: 10000,
			minSeed: 0, maxSeed: 999999999999999, maxImages: 9, maxAudios: 3,
			supportsImages: true, requiresImages: true, promptSupported: true, promptRequired: true,
			resolutions: []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"}, resolutionCount: 6,
		},
		"autodl:minimax-h3-u08": {
			workflowID: "minimax_h3_zm_u08", maxDuration: 15, maxPromptLength: 10000,
			minSeed: 0, maxSeed: 999999999999999, maxImages: 9, maxAudios: 3,
			supportsImages: true, requiresImages: true, promptSupported: true, promptRequired: true,
			resolutions: []string{"480p竖", "768p竖", "480p横", "768p横", "480p(1:1)", "768p(1:1)"}, resolutionCount: 6,
		},
		"autodl:minimax-h3-image-audio-10s": {
			workflowID: "minimax_h3_image_audio_to_video_v2", maxDuration: 10, maxPromptLength: 10000,
			minSeed: 1, maxSeed: 999999999999999, maxImages: 9, maxAudios: 3,
			supportsImages: true, promptSupported: true, promptRequired: true,
			resolutions: []string{"480p竖", "768p竖", "1080p竖", "480p横", "768p横", "1080p横"}, resolutionCount: 6,
		},
		"autodl:minimax-h3-image-audio-15s": {
			workflowID: "minimax_h3_image_audio_to_video_v2_15s", maxDuration: 15, maxPromptLength: 10000,
			minSeed: 1, maxSeed: 999999999999999, maxImages: 9, maxAudios: 3,
			supportsImages: true, promptSupported: true, promptRequired: true,
			resolutions: []string{"480p竖", "768p竖", "480p横", "768p横"}, resolutionCount: 4,
		},
		"autodl:minimax-h3-lipsync": {
			workflowID: "minimax_h3_image_audio_to_video", maxDuration: 15, maxImages: 1, maxAudios: 1,
			supportsImages: true, requiresImages: true, requiresAudios: true, resolutionCount: 6,
			resolutions:       []string{"480p竖", "768p竖", "1080p竖", "480p横", "768p横", "1080p横"},
			usesAudioDuration: true,
		},
	}

	for modelName, want := range expected {
		cfg, ok := workflowByModel[modelName]
		require.True(t, ok, modelName)
		require.Equal(t, want.workflowID, cfg.WorkflowID, modelName)
		require.Equal(t, want.maxDuration, cfg.MaxDuration, modelName)
		require.Equal(t, want.maxPromptLength, cfg.MaxPromptLength, modelName)
		require.Equal(t, want.minSeed, cfg.MinSeed, modelName)
		require.Equal(t, want.maxSeed, cfg.MaxSeed, modelName)
		require.Equal(t, want.maxImages, cfg.MaxImages, modelName)
		require.Equal(t, want.maxAudios, cfg.MaxAudios, modelName)
		require.Equal(t, want.supportsImages, cfg.SupportsImages, modelName)
		require.Equal(t, want.requiresImages, cfg.RequiresImages, modelName)
		require.Equal(t, want.requiresAudios, cfg.RequiresAudios, modelName)
		require.Equal(t, want.promptSupported, cfg.PromptSupported, modelName)
		require.Equal(t, want.promptRequired, cfg.PromptRequired, modelName)
		require.Equal(t, want.resolutions, cfg.Resolutions, modelName)
		require.Len(t, cfg.Resolutions, want.resolutionCount, modelName)
		require.Equal(t, want.usesAudioDuration, cfg.UsesAudioDuration, modelName)
		require.True(t, cfg.SupportsAudios, modelName)
	}
}

func TestEstimateBillingChargesAutoDLResolutionRatio(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
	adaptor.Init(info)

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    1.2,
	}, adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "768p竖",
	}), info))

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    1.0,
	}, adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "480p竖",
	}), info))

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    2.0,
	}, adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "1080p竖",
	}), info))
}

func TestAutoDLResolutionPriceTiers(t *testing.T) {
	want := map[string]float64{
		"480p竖":  0.10,
		"768p竖":  0.12, // AutoDL 的 720p 档使用 768p 标识
		"1080p竖": 0.20,
	}

	for _, ratios := range []map[string]float64{h3ResolutionRatios, h3V2ResolutionRatios} {
		for resolution, wantCNY := range want {
			ratio, ok := ratios[resolution]
			require.True(t, ok, "resolution tier must be configured: %s", resolution)
			require.InDelta(t, wantCNY, pricePerSecondCNY*ratio, 1e-12, "resolution price: %s", resolution)
		}
	}
}

func TestBuildRequestBodyUsesAutoDLWorkflowSpecificLimits(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-text-to-video")
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

	info = autoDLRelayInfo("autodl:minimax-h3-b99-12s")
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

	info = autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
	adaptor.Init(info)
	body, err = adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:     "reference",
		Resolution: "1080p竖",
		Images:     []string{"https://example.com/reference.png"},
	}), info)
	require.NoError(t, err)
	encoded, err = io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(encoded, &payload))
	require.Equal(t, "1080p竖", payload["resolution"])
}

func TestBuildRequestBodyRejectsTooManyAutoDLReferenceImages(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
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
	info := autoDLRelayInfo("autodl:minimax-h3-text-to-video")
	adaptor.Init(info)

	_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt: "text only",
		Images: []string{"https://example.com/reference.png"},
	}), info)
	require.ErrorContains(t, err, "does not support reference images")
}

func TestDeriveResolutionFromSize(t *testing.T) {
	tests := []struct {
		name    string
		size    string
		allowed []string
		want    string
		wantOk  bool
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
	info := autoDLRelayInfo("autodl:minimax-h3-text-to-video")
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
	info := autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5")
	adaptor.Init(info)

	billing := adaptor.EstimateBilling(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Duration: 5,
		Size:     "720x1280",
	}), info)
	require.Equal(t, 1.2, billing["size"])
}

func TestConvertToOpenAIVideoReflectsTaskStatus(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(autoDLRelayInfo("autodl:minimax-h3-lightx2v-v5"))

	// 轮询同步后 task.Data = redactVideoResponseBody(pollResponse)
	// {"code":"Success","data":{"status":"SUCCEEDED","results":[{"type":"video","url":"https://...","file_type":"mp4"}]}}
	task := &model.Task{
		TaskID:   "public-task-abc",
		Status:   model.TaskStatusSuccess,
		Progress: taskcommon.ProgressComplete,
		Data:     []byte(`{"code":"Success","data":{"status":"SUCCEEDED","results":[{"type":"video","url":"https://upstream.example.com/v1.mp4","file_type":"mp4"}]}}`),
		Properties: model.Properties{
			OriginModelName: "autodl:minimax-h3-lightx2v-v5",
		},
		CreatedAt: 1788400000,
		UpdatedAt: 1788400100,
	}
	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var out dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &out))
	require.Equal(t, "public-task-abc", out.ID)
	require.Equal(t, dto.VideoStatusCompleted, out.Status)
	require.Equal(t, "autodl:minimax-h3-lightx2v-v5", out.Model)
	metadata := out.Metadata
	url, _ := metadata["url"].(string)
	require.Equal(t, "https://upstream.example.com/v1.mp4", url)
}

func TestConvertToOpenAIVideoMapsFailure(t *testing.T) {
	adaptor := &TaskAdaptor{}
	adaptor.Init(autoDLRelayInfo("autodl:minimax-h3-text-to-video"))

	task := &model.Task{
		TaskID: "public-task-fail",
		Status: model.TaskStatusFailure,
		Data:   []byte(`{"code":"Fail","msg":"quota exceeded","data":{"status":"FAILED","message":"quota exceeded"}}`),
	}
	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var out dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &out))
	require.Equal(t, dto.VideoStatusFailed, out.Status)
	require.NotNil(t, out.Error)
}
