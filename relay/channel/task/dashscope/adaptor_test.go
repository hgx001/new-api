package dashscope

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

func TestResolveDurationUsesSecondsWhenDurationIsAbsent(t *testing.T) {
	adaptor := &TaskAdaptor{}

	require.Equal(t, 12, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "12"}))
	require.Equal(t, 2, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "1"}))
	require.Equal(t, 30, adaptor.resolveDuration(relaycommon.TaskSubmitReq{Seconds: "31"}))
}

func TestResolveResolutionAcceptsTopLevelResolution(t *testing.T) {
	adaptor := &TaskAdaptor{}

	require.Equal(t, "720P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{Resolution: "720p"}))
	require.Equal(t, "720P", adaptor.resolveResolution(relaycommon.TaskSubmitReq{
		Resolution: "invalid",
		Metadata:   map[string]interface{}{"resolution": "720P"},
	}))
}
