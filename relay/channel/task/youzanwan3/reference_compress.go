package youzanwan3

import (
	"net/url"
	"strings"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// 参考图体积优化（2026-11-20 首版，2026-11-21 加缩放）。
//
// 现场（logs id=18830 / 18833 / 18962，客户 arcreel，5~7 张参考图 2.6~3.6MB）：
// 图片刚写进 CDN 就提交，上游并发回源超时，回 400：
//
//	{"code":"asset_download_failed","message":"连接素材服务器超时。尚未提交上游生成。"}
//
// 上游（阿里云杭州）与素材（腾讯云 CDN）都在国内，不是跨境问题，但「刚上传 +
// 冷缓存 + 多张并发回源」把窗口拉长了，体积越小越容易一次成功。
//
// 做法：**只改 URL，不搬字节**。arcmedia 这个桶挂了腾讯云数据万象（CI），
// 在地址上追加缩放 + 质量指令，CI 会在回源时按需重编码（实测见下方常量）。
// 2026-11-21 现场：9c4513e0… 那张 891KB → 300KB，7 张合计 3.6MB → 1.2MB。
//
// 好处是链路里没有任何新增环节：不下载、不转存、不占网关出口（老机 3Mbps），
// 也不引入新的失败点 —— 参数不生效时 CI 原样返图，最坏情况等于没加。
//
// ⚠️ 有两条边界必须守住：
//  1. 只对 arcmedia 这个自有 CDN 域名加参数。客户给的外部图（对象存储签名链、
//     别人的图床）加参数可能直接 403/404，宁可不动。
//  2. 带签名参数的地址一律不加：拼一个 `&` 会破坏签名，图就彻底拉不到了。
const (
	// referenceImageCIParam 是腾讯云 CI 的重编码指令。
	//
	// 2026-11-21 迭代：原来只加 quality/90，对这批参考图几乎没效果 ——
	// 实测 2048×1152 的图只降 3%（461KB→446KB）。这批图的冗余不在量化，
	// 而在像素数：客户传的是 2K 图，促销族对客恒定交付 720P（1280×720），
	// 缩到 1280 长边已经跑在成片分辨率上，再大的像素对成片没有增量。
	//
	//	891KB(2048×1152) → 300KB(1280×720)　-66%
	//	633KB(2048×1152) → 216KB(1280×720)　-66%
	//	461KB(2048×1152) → 151KB(1280×720)　-67%
	//
	// `>` 是「仅缩不放」：比 1280 小的图原样返回，避免把小图放大变糊又变胖。
	// `%3E` 是 `>` 的百分号编码 —— 裸 `>` 写进 query 有客户端会拒发，
	// 编码后腾讯云 CI 一样认（2026-11-21 实测 200 / 1280×720）。
	referenceImageCIParam = "imageMogr2/thumbnail/1280x1280%3E/quality/85"
)

// referenceImageOptimizableHosts 是允许追加 CI 参数的域名（自有腾讯云 CDN）。
var referenceImageOptimizableHosts = map[string]struct{}{
	"arcmedia.heibaidao.cn": {},
}

// signedQueryMarkers 命中任一即视为「地址自带签名/鉴权」，不能追加任何查询参数。
var signedQueryMarkers = []string{
	"sign=", "signature=", "x-amz-", "x-cos-", "q-sign-", "access=", "token=",
}

// optimizeReferenceMedia 返回把参考图地址换成 CI 重编码版本后的素材列表。
//
// 只动 reference_image：视频/音频不在这里处理（体积与收益结构完全不同）。
// 顺序与类型保持不变，prompt 里的「@图片N」指认不会错位。
func optimizeReferenceMedia(media []relaycommon.TaskMedia) []relaycommon.TaskMedia {
	out := append([]relaycommon.TaskMedia(nil), media...)
	for i := range out {
		if out[i].Type != "reference_image" {
			continue
		}
		out[i].URL = optimizeReferenceImageURL(out[i].URL)
	}
	return out
}

// optimizeReferenceImageURL 给自有 CDN 上的参考图追加 CI 质量参数。
//
// 任何一条不满足就原样返回 —— 这是纯字符串变换，不联网、不失败。
func optimizeReferenceImageURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return raw
	}
	if _, ok := referenceImageOptimizableHosts[strings.ToLower(parsed.Host)]; !ok {
		return raw
	}
	// 已经带过 CI 指令（客户自己压过）就别叠加第二条。
	if strings.Contains(strings.ToLower(parsed.RawQuery), "imagemogr2") {
		return raw
	}
	lowerQuery := strings.ToLower(parsed.RawQuery)
	for _, marker := range signedQueryMarkers {
		if strings.Contains(lowerQuery, marker) {
			return raw
		}
	}
	// 手工拼接：CI 的 path 风格参数经 url.Values.Encode 会把 `/` 转义成 %2F，
	// 腾讯云就不认这条指令了。
	if parsed.RawQuery == "" {
		return trimmed + "?" + referenceImageCIParam
	}
	return trimmed + "&" + referenceImageCIParam
}
