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
		"wan3.0-video-prime-1080p",
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

func TestIsAllowedPricingModelMiniMaxM3Family(t *testing.T) {
	for _, name := range []string{"MiniMax-M3", "MiniMax-M3.1"} {
		require.True(t, isAllowedPricingModel(name), "model %q must show in pricing square", name)
	}
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

// opus-5.5 已下架（以 Claude 之名售卖 gpt-6-luna），广场不得再展示。
func TestIsAllowedPricingModelOpus55Removed(t *testing.T) {
	require.False(t, isAllowedPricingModel("opus-5.5"), "opus-5.5 must be hidden after removal")
}

// 官方渠道图片模型 gpt-image-2.5-官方 需与二手渠道的 gpt-image-2/2.5 同时在广场展示。
func TestIsAllowedPricingModelGptImageOfficial(t *testing.T) {
	for _, name := range []string{"gpt-image-2", "gpt-image-2.5", "gpt-image-2.5-官方"} {
		require.True(t, isAllowedPricingModel(name), "model %q must show in pricing square", name)
	}
}

// 漫屋 ArcReel Seedance 2.0/2.5 视频模型必须在广场展示。
func TestIsAllowedPricingModelDolaSeedance(t *testing.T) {
	for _, name := range []string{"seedance-2.0", "seedance-2.5"} {
		require.True(t, isAllowedPricingModel(name), "%s must show in pricing square", name)
	}
}

// 漫屋 ArcReel 豆包官网视频上广场（2026-10-12 公开名去掉 db- 前缀，旧名退役）。
func TestIsAllowedPricingModelDoubaoSeedance(t *testing.T) {
	for _, name := range []string{"seedance-2-0-fast", "seedance-2-5"} {
		require.True(t, isAllowedPricingModel(name), "%s must show in pricing square", name)
	}
	for _, retired := range []string{"db-seedance-2-0", "db-seedance-2-5"} {
		assert.False(t, isAllowedPricingModel(retired), "%s must be hidden after renaming", retired)
	}
}

// MegaAI 渠道的 wan-3.0 上广场：与官网 wan3.0-video 系列并存，按次 + 分辨率档计价。
func TestIsAllowedPricingModelMegaAIWan30(t *testing.T) {
	require.True(t, isAllowedPricingModel("wan-3.0"), "wan-3.0 must show in pricing square")
}

// MegaAI 渠道 wan-3.0 的对外别名 seedance2.5-特惠 上广场：固定 ¥5.0/条。
func TestIsAllowedPricingModelMegaAISeedance25TeHui(t *testing.T) {
	require.True(t, isAllowedPricingModel("seedance2.5-特惠"), "seedance2.5-特惠 must show in pricing square")
}

// 漫屋新增两个模型上广场；视频反解在价格核定前**不**上（占位价不得对外发布）。
func TestIsAllowedPricingModelManwuRemote(t *testing.T) {
	for _, name := range []string{"gemini-web-video", "Nano Banana Pro"} {
		require.True(t, isAllowedPricingModel(name), "%s must show in pricing square", name)
	}
	assert.False(t, isAllowedPricingModel("jimeng-video-reverse"),
		"jimeng-video-reverse 定价未核定，不得出现在广场")
}

// 漫屋 ArcReel 通义万相 wan2.7 免灵感值双模型上广场（2026-10-16）。
// 两个公开名共用 platformId=tongyiwan，靠素材类型分流图生 / 编辑两条 DOM 路线。
func TestIsAllowedPricingModelManwuWan27(t *testing.T) {
	for _, name := range []string{"wan2.7-图生", "wan2.7-编辑"} {
		require.True(t, isAllowedPricingModel(name), "%s must show in pricing square", name)
	}
	// 上游真名 / 旧名一律不对外：公开名只存在于 adaptor 的 modelSpecs 里。
	for _, hidden := range []string{"wan2.7", "Wan2_7", "wan2.7-i2v", "wan2.7-edit"} {
		assert.False(t, isAllowedPricingModel(hidden), "%s must not show in pricing square", hidden)
	}
}
