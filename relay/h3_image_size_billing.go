package relay

import (
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// h3-image 两档计费（2026-11-13 用户定稿，同日追加 3840×2160）。
//
// 同一个模型按尺寸分两段：普通尺寸沿用基础价 ¥0.05/张，只有**显式**指定 4K
// （size=4k、4096x4096 或 3840x2160）才按 ¥0.10/张。基础 ModelPrice 保持不变，
// 4K 档靠 OtherRatios 的 2 倍倍率实现 —— service/text_quota.go 的按次价分支
// 会把 OtherRatios 逐个乘进 quota，所以 n=2 且 4K 会正确扣 4 倍（¥0.20）。
//
// ⚠️ 为什么必须归一化 size：GPU 网关的 resolve_size() 不认识 "4k" token，
// 也不认中文乘号 "×"（它只按 ASCII "x" 切分），两种情况都会静默退回默认
// 2048x2048。只加计费判定而不改请求体，就会出现「客户付了 4K 的钱，拿到的
// 却是 2K 的图」——比漏收更难发现。
//
// ⚠️ 归一化不能把所有 4K 都写成 4096x4096：3840x2160 是 16:9，硬改成 1:1
// 会把画面压变形。规则是——"4k" token 映射 4096x4096，显式像素尺寸只做
// 乘号/大小写归一，宽高比原样保留。
//
// ⚠️ 只服务于 h3-image：别的图片模型各有自己的价表与尺寸语义，
// 传 "4k" 既不能被改写，也不能被误判加价。
const (
	h3ImageModelName  = "h3-image"
	h3Image4KSize     = "4096x4096"
	h3Image4KSize169  = "3840x2160"
	h3Image4KRatioKey = "size_4k"
	h3Image4KRatio    = 2.0
)

func normalizeImageSizeToken(size string) string {
	// 兼容中文/全角乘号 "4096×4096" 与大小写、首尾空格。
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(size)), "×", "x")
}

// isH3Image4KSize 判定是否命中「显式 4K 参数」档。
// 刻意**不**把 4096x2160、2160x3840 等算进来：只认标准 4K 尺寸，
// 宁可少收也不能误扣。
func isH3Image4KSize(size string) bool {
	s := normalizeImageSizeToken(size)
	return s == "4k" || s == h3Image4KSize || s == h3Image4KSize169
}

// prepareH3ImageSize 返回（转发给上游的 size, 是否 4K 档）。
// 必须在 ModelMappedHelper 之前调用：之后 request.Model 会被改写成
// 上游兼容 id qwen_image_21，就认不出这是 h3-image 了。
func prepareH3ImageSize(model, size string) (string, bool) {
	if model != h3ImageModelName || !isH3Image4KSize(size) {
		return size, false
	}
	s := normalizeImageSizeToken(size)
	if s == "4k" {
		// 网关不认 token，换成裸数字尺寸。
		return h3Image4KSize, true
	}
	// 显式像素尺寸：只把乘号/大小写归一，宽高比必须保留。
	return s, true
}

// applyH3Image4KBilling 只在按次价（UsePrice）路径加 2 倍倍率。
// 按量计费路径不碰，避免把倍率乘进按 token 计算的 quota。
func applyH3Image4KBilling(info *relaycommon.RelayInfo, is4K bool) {
	if !is4K || !info.PriceData.UsePrice {
		return
	}
	info.PriceData.AddOtherRatio(h3Image4KRatioKey, h3Image4KRatio)
}
