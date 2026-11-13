package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// h3-image 必须在模型广场展示（isAllowedPricingModel 是硬编码逐个放行的，
// 漏一行就「可调用但广场看不到，也算不出展示价」）。
func TestIsAllowedPricingModelH3Image(t *testing.T) {
	require.True(t, isAllowedPricingModel("h3-image"),
		"h3-image must show in pricing square")
}

// 网关上游兼容 id 不得上广场 —— 客户侧只能看到 h3-image。
func TestIsAllowedPricingModelH3ImageUpstreamIdHidden(t *testing.T) {
	for _, upstream := range []string{"qwen_image_21", "qwen_image_21_i2i"} {
		require.False(t, isAllowedPricingModel(upstream),
			"upstream compat id %s must never be exposed in pricing square", upstream)
	}
}
