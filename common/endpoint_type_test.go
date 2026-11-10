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

// MegaAI 渠道（type=64）已于 2026-11-10 停售：生产库里没有任何 type=64 的渠道，
// 广场上也没有挂在它下面的卡片。退役后它**不再**声明 OpenAI Video 端点 ——
// 否则客户端会照着 supported_endpoint_types 去调 /v1/videos，而那条路已经没有
// 任务适配器承接（relay.GetTaskAdaptor("64") 返回 nil）。
//
// 退役细节见 relay/megabyai_channel_retired_test.go。
func TestMegaAIEndpointNoLongerDeclaresOpenAIVideoContract(t *testing.T) {
	endpointTypes := common.GetEndpointTypesByChannelType(constant.ChannelTypeMegaAI, "sd-2.5")

	require.NotContains(t, endpointTypes, constant.EndpointTypeOpenAIVideo,
		"type=64 已退役，不得再声明 OpenAI Video 端点")
}

func TestVolcEngineEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeVolcEngine, "hmseedance_v2.0"),
	)
}

// 心笔 Model Router（68）漏配过一次：广场把 wan3.0-720p-按次 标成了 openai 端点
// （/v1/chat/completions），客户端照着 supported_endpoint_types 选路就会打到聊天口。
func TestXinbiEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeXinbi, "wan3.0-720p-按次"),
	)
}
