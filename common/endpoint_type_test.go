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

// MegaAI 渠道（type=64）2026-11-23 重新接入（同上上游 newapi.megabyai.cc，
// 新上架 T3-seedance-2.5），因此必须重新声明 OpenAI Video 端点：
// 客户端就是照 supported_endpoint_types 去调 /v1/videos 的。
//
// 退役期间这条断言曾是 NotContains；反转回来是因为渠道真的在售了，
// 而不是为了绕测试。细节见 relay/megabyai_channel_retired_test.go。
func TestMegaAIEndpointDeclaresOpenAIVideoContract(t *testing.T) {
	endpointTypes := common.GetEndpointTypesByChannelType(constant.ChannelTypeMegaAI, "T3-seedance-2.5")

	require.Contains(t, endpointTypes, constant.EndpointTypeOpenAIVideo,
		"type=64 已重新接入，必须声明 OpenAI Video 端点")
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

// 二春 Erchun（65）与心笔是同一类漏配：渠道是异步视频，但没登记进视频渠道列表
// 就会回落到 openai 端点 —— 广场把 wan3-新大陆 归到文本分类，视频页签里看不到。
func TestErchunEndpointUsesOpenAIVideoContract(t *testing.T) {
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeErchun, "wan3-新大陆"),
	)
}
