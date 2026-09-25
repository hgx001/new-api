package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAllowedPricingModelWanVideo(t *testing.T) {
	allowed := []string{
		"wan3.0-video",
		"wan3.0-video-官网",
		"wan3.0-smart",
		"wan2.7-r2v",
	}
	for _, name := range allowed {
		require.True(t, isAllowedPricingModel(name), "model %q must show in pricing square", name)
	}
	assert.False(t, isAllowedPricingModel("some-random-model"), "unlisted model must stay hidden")
	// 注：已下线的错误 id（wan3.0-video-smart）仍命中 wan3.0-video 前缀规则，
	// 但它已无任何 ability 指向，不会出现在广场上，故不在此断言。
}

// GPT 文本模型只放行 gpt-6 系列；5.6 全家族下线后必须从广场隐藏。
func TestIsAllowedPricingModelGpt6(t *testing.T) {
	for _, name := range []string{"gpt-6-luna", "gpt-6-sol"} {
		require.True(t, isAllowedPricingModel(name), "model %q must show in pricing square", name)
	}
	for _, retired := range []string{"gpt-5.6", "gpt-5.6-luna", "gpt-5.6-terra", "gpt-5.6-sol"} {
		assert.False(t, isAllowedPricingModel(retired), "%s must be hidden after retirement", retired)
	}
}

// 官方渠道图片模型 gpt-image-2.5-官方 需与二手渠道的 gpt-image-2/2.5 同时在广场展示。
func TestIsAllowedPricingModelGptImageOfficial(t *testing.T) {
	for _, name := range []string{"gpt-image-2", "gpt-image-2.5", "gpt-image-2.5-官方"} {
		require.True(t, isAllowedPricingModel(name), "model %q must show in pricing square", name)
	}
}

// 漫屋 ArcReel dola 视频模型必须在广场展示。
func TestIsAllowedPricingModelDolaSeedance(t *testing.T) {
	require.True(t, isAllowedPricingModel("dola-seedance-2.5"), "dola-seedance-2.5 must show in pricing square")
}
