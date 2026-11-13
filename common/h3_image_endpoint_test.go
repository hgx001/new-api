package common_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/require"
)

// h3-image 是同步图片模型：type 1 OpenAI 渠道的第一端点必须是
// /v1/images/generations，否则模型广场会把它显示成聊天模型。
func TestH3ImageEndpointIsImageGeneration(t *testing.T) {
	require.True(t, common.IsImageGenerationModel("h3-image"))

	types := common.GetEndpointTypesByChannelType(constant.ChannelTypeOpenAI, "h3-image")
	require.NotEmpty(t, types)
	require.Equal(t, constant.EndpointTypeImageGeneration, types[0],
		"h3-image must prefer the image generation endpoint")

	endpoint, ok := common.GetDefaultEndpointInfo(types[0])
	require.True(t, ok)
	require.Equal(t, "/v1/images/generations", endpoint.Path)
	require.Equal(t, "POST", endpoint.Method)
}

// 不能因为新增 "h3-image" 把 H3 视频模型误判成图片模型（Contains 匹配）。
func TestH3ImageDoesNotReclassifyH3VideoModel(t *testing.T) {
	require.False(t, common.IsImageGenerationModel("minimax_h3_omni"))
	require.Equal(t,
		[]constant.EndpointType{constant.EndpointTypeOpenAIVideo},
		common.GetEndpointTypesByChannelType(constant.ChannelTypeSeeta, "minimax_h3_omni"),
	)
}
