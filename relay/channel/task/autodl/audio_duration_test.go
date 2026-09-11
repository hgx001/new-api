package autodl

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyRejectsOutOfRangeAudioDuration(t *testing.T) {
	adaptor := &TaskAdaptor{}
	info := autoDLRelayInfo("autodl:minimax-h3-lipsync")
	adaptor.Init(info)

	for _, duration := range []int{0, 16} {
		duration := duration
		_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
			AudioDuration: &duration,
			Images:        []string{"https://example.com/portrait.png"},
			Audios:        []string{"https://example.com/speech.mp3"},
		}), info)
		require.ErrorContains(t, err, "audio_duration must be between 1 and 15")
	}

	_, err := adaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Images: []string{"https://example.com/portrait.png"},
		Audios: []string{"https://example.com/speech.mp3"},
	}), info)
	require.NoError(t, err)

	duration := 5
	nonSyncInfo := autoDLRelayInfo("autodl:minimax-h3-u24")
	nonSyncAdaptor := &TaskAdaptor{}
	nonSyncAdaptor.Init(nonSyncInfo)
	_, err = nonSyncAdaptor.BuildRequestBody(autoDLTaskContext(relaycommon.TaskSubmitReq{
		Prompt:        "animate",
		AudioDuration: &duration,
		Images:        []string{"https://example.com/portrait.png"},
	}), nonSyncInfo)
	require.ErrorContains(t, err, "does not support audio_duration")
}
