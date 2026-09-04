package common_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

func TestAutoDLEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeAutoDL, "autodl:multiref-video-1"),
	)

	endpoint, ok := common.GetDefaultEndpointInfo(constant.EndpointTypeOpenAIVideo)
	require.True(t, ok)
	require.Equal(t, "/v1/videos", endpoint.Path)
	require.Equal(t, "POST", endpoint.Method)
}

func TestWan3EndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeWan3, "wan3.0-video"),
	)
}

func TestVolcEngineEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeVolcEngine, "hmseedance_v2.0"),
	)
}
