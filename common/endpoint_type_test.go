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
		common.GetEndpointTypesByChannelType(constant.ChannelTypeAutoDL, "autodl:minimax-h3-lightx2v-v5"),
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

func TestYouzanWan3EndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeYouzanWan3, "wan3.0-video-prime"),
	)
}

func TestMegaAIEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeMegaAI, "sd-2.5"),
	)
}

func TestVolcEngineEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeVolcEngine, "hmseedance_v2.0"),
	)
}
