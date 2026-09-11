package common

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskSubmitReqPreservesAudioDurationPresenceAndRejectsInvalidValues(t *testing.T) {
	var zeroDuration TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{"audio_duration":0}`), &zeroDuration))
	require.NotNil(t, zeroDuration.AudioDuration)
	assert.Equal(t, 0, *zeroDuration.AudioDuration)

	var omittedDuration TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(`{}`), &omittedDuration))
	assert.Nil(t, omittedDuration.AudioDuration)

	var invalidDuration TaskSubmitReq
	require.ErrorContains(t, common.Unmarshal([]byte(`{"audio_duration":"abc"}`), &invalidDuration), "audio_duration must be an integer")
}
