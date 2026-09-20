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
