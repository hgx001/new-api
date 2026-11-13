package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// h3-image（2026-11-13 上架）：GPU 服务器 MiniMax H3-Image 文生图，
// 走 OpenAI 兼容同步端点 POST /v1/images/generations，按张 ¥0.05/张。
//
// 上游兼容 id 仍是 qwen_image_21（网关 2026-11-12 起内部工作流换成 H3-Image，
// 对外 id 不变），由渠道 model_mapping 转发；公开名不得出现 qwen。
//
// 关键约束：
//   - ModelPrice 必须显式配价 —— 不配价时 ModelPriceHelperPerCall 的
//     「查不到价」分支会静默免单（¥0），比报错更难发现。
//   - 张数由 relay/image_handler.go 的 OtherRatios{"n":N} 乘上去，
//     n=2 扣 ¥0.10；这里只锁单价，不重复实现倍率。
func TestH3ImagePriceIsFiveFenPerImage(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("h3-image", false)
	require.True(t, ok, "h3-image must have an explicit price")
	require.InDelta(t, 0.05/USD2RMB, price, 1e-12,
		"h3-image must be ¥0.05 per image")
}

// 新增模型不得牵动同品类其它公开名（授权只及点名模型）。
func TestH3ImagePriceDoesNotChangeOtherImageModels(t *testing.T) {
	InitRatioSettings()

	for name, want := range map[string]float64{
		"gpt-image-2.5-官方":   0.15 / USD2RMB,
		"Nano Banana Pro":    0.3 / USD2RMB,
		"Seedream 4.5":       0.2 / USD2RMB,
		"Seedream 5.0 Flash": 0.2 / USD2RMB,
	} {
		price, ok := GetModelPrice(name, false)
		require.True(t, ok, "%s must keep an explicit price", name)
		require.InDelta(t, want, price, 1e-12, "%s price must be unchanged", name)
	}
}

// 上游兼容 id 只存在于网关与渠道 model_mapping，不作为公开计价模型；
// 否则客户可以绕过 h3-image 直接按 qwen 名字调用，绕开对外口径。
func TestH3ImageUpstreamIdIsNotPubliclyPriced(t *testing.T) {
	InitRatioSettings()

	_, ok := GetModelPrice("qwen_image_21", false)
	require.False(t, ok, "upstream compat id must not be a public priced model")
}
