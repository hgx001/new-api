package relay

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// h3-image 两档计费（2026-11-13 用户定稿）：
//   - 普通尺寸 ¥0.05/张（基础 ModelPrice 不变）
//   - 只有**显式**传 size=4k 或 4096x4096 才按 4K 档 ¥0.10/张
//   - 其余一律普通档，包括 3840x2160、2048x2048、2k / 1k token
//
// 最危险的不是「漏收」，是「收了 4K 的钱却出 2K 的图」：GPU 网关的
// resolve_size() 不认识 "4k" token，会静默退回默认 2048x2048。所以命中
// 4K 档时必须在转发前把 size 归一化成 4096x4096（网关认裸数字尺寸）。

func TestIsH3Image4KSize(t *testing.T) {
	for _, size := range []string{
		"4k", "4K", " 4k ", "4096x4096", "4096X4096", "4096×4096", " 4096×4096 ",
	} {
		assert.Truef(t, isH3Image4KSize(size), "size=%q 应命中 4K 档", size)
	}
	for _, size := range []string{
		"", "1k", "2k", "1024x1024", "2048x2048", "3840x2160",
		"16:9", "4096x2160", "4k-ultra", "4096x4097",
	} {
		assert.Falsef(t, isH3Image4KSize(size), "size=%q 不该命中 4K 档（仅显式 4K 参数）", size)
	}
}

func TestPrepareH3ImageSizeNormalizes4KToken(t *testing.T) {
	for _, size := range []string{"4k", "4K", "4096×4096", "4096X4096"} {
		got, is4K := prepareH3ImageSize("h3-image", size)
		assert.True(t, is4K, "size=%q 应命中 4K 档", size)
		assert.Equal(t, "4096x4096", got,
			"size=%q 必须归一化成网关认识的裸数字尺寸，否则客户付 4K 的钱拿 2K 的图", size)
	}
}

func TestPrepareH3ImageSizeLeavesNormalRequestsAlone(t *testing.T) {
	for _, size := range []string{"", "1024x1024", "2048x2048", "3840x2160", "1:1"} {
		got, is4K := prepareH3ImageSize("h3-image", size)
		assert.False(t, is4K, "size=%q 不该命中 4K 档", size)
		assert.Equal(t, size, got, "普通档 size 必须原样透传")
	}
}

// 守卫：这段逻辑只服务于 h3-image。别的图片模型传 "4k" 不能被改写，
// 也不能被误判成 4K 加价（gpt-image / qwen / seedream 各有自己的价表）。
func TestPrepareH3ImageSizeDoesNotTouchOtherModels(t *testing.T) {
	for _, model := range []string{
		"gpt-image-2.5-官方", "dall-e-3", "Nano Banana Pro",
		"qwen_image_21", "h3-image-v2", "",
	} {
		got, is4K := prepareH3ImageSize(model, "4k")
		assert.False(t, is4K, "model=%q 不该命中 h3-image 的 4K 档", model)
		assert.Equal(t, "4k", got, "model=%q 的 size 不能被改写", model)
	}
	got, is4K := prepareH3ImageSize("gpt-image-2.5-官方", "4096x4096")
	assert.False(t, is4K)
	assert.Equal(t, "4096x4096", got)
}

func TestApplyH3Image4KBilling(t *testing.T) {
	t.Run("按次价 + 4K → 2 倍", func(t *testing.T) {
		info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: true}}
		applyH3Image4KBilling(info, true)
		require.Contains(t, info.PriceData.OtherRatios, h3Image4KRatioKey)
		assert.Equal(t, 2.0, info.PriceData.OtherRatios[h3Image4KRatioKey])
	})

	t.Run("按次价 + 普通档 → 不额外加价", func(t *testing.T) {
		info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: true}}
		applyH3Image4KBilling(info, false)
		assert.NotContains(t, info.PriceData.OtherRatios, h3Image4KRatioKey)
	})

	t.Run("非按次价 → 不加价，避免把倍率乘进按量计费", func(t *testing.T) {
		info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: false}}
		applyH3Image4KBilling(info, true)
		assert.NotContains(t, info.PriceData.OtherRatios, h3Image4KRatioKey)
	})
}

// n 倍率与 4K 倍率必须共存：service/text_quota.go 会遍历 OtherRatios 逐个相乘，
// n=2 且 4K 应扣 2 × 2 = 4 倍（¥0.20），任何一个被覆盖都会少收钱。
func TestH3Image4KBillingCoexistsWithNRatio(t *testing.T) {
	info := &relaycommon.RelayInfo{PriceData: types.PriceData{UsePrice: true}}
	info.PriceData.AddOtherRatio("n", 2)
	applyH3Image4KBilling(info, true)

	require.Equal(t, 2.0, info.PriceData.OtherRatios["n"])
	require.Equal(t, 2.0, info.PriceData.OtherRatios[h3Image4KRatioKey])
	product := 1.0
	for _, r := range info.PriceData.OtherRatios {
		product *= r
	}
	assert.Equal(t, 4.0, product)
}
