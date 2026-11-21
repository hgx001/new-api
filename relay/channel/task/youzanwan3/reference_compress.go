package youzanwan3

import (
	"net/url"
	"strconv"
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

// isWarmupableReferenceURL 判断这个参考图地址值不值得预热。
//
// 只预热**自有 CDN 域名**下的地址：
//   - 别人的图床 / 对象存储签名链，预热等于替对方刷流量，且签名链多一次
//     请求就多一次过期风险，宁可不动；
//   - 带签名参数的地址一律不动：预热请求本身也是一次带签名的请求，可能
//     触发对方的风控或把签名提前用掉。
//
// 与 optimizeReferenceImageURL 的域名/签名判定保持同一口径 —— 两处都要
// 用同一份「哪些地址是我们自己的、哪些带签名」的判断，否则会出现
// 「压缩加了参数但预热没加」这种半吊子状态。
func isWarmupableReferenceURL(raw string) bool {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Host == "" {
		return false
	}
	if _, ok := referenceImageOptimizableHosts[strings.ToLower(parsed.Host)]; !ok {
		return false
	}
	lowerQuery := strings.ToLower(parsed.RawQuery)
	for _, marker := range signedQueryMarkers {
		if strings.Contains(lowerQuery, marker) {
			return false
		}
	}
	return true
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

// ════════════════════════════════════════════════════════════════════════
// cache-buster：让每次提交的素材 URL 都是一条**全新的缓存键**
// ════════════════════════════════════════════════════════════════════════
//
// 为什么需要（2026-11-21）：
//
// CDN 会把 404 缓存住（实测第 2 次起就是 X-Cache-Lookup: Cache Hit），
// 而 arcmedia 的参考素材 key 按**内容 sha256** 生成 —— 同一张图内容相同，
// key 就永远是同一个。于是「key 在对象还没落盘时被访问过 → CDN 记住 404
// → 之后 put 进去了也拉不到，要等负缓存过期（实测 10.4 秒）」。
//
// 实测三件事：
//
//	裸 URL put 后立刻拉   → 404（0.04s，命中负缓存）
//	带 ?v=xxx 的同一对象  → 200（0.81s，重新回源）
//	带 ?imageMogr2 的地址  → 200（同样是新缓存键）
//
// 也就是说**只有换缓存键才能穿透负缓存**。所以给每次提交追加一个
// cache-buster 参数，让这条 URL 对 CDN 而言是全新的：
//
//   - 负缓存打不到这条键上（它压根没见过这个 URL）；
//   - 回源时对象已经在 COS 里（源站是 put 完才把 URL 交给我们），直接 200；
//   - 顺带把这条键填成正缓存，上游随后拉是 Cache Hit（实测 0.04s）。
//
// 为什么不能只是「预热裸 URL」：负缓存还在裸 URL 那条键上，预热裸 URL
// 只会把 404 又刷新一遍。实测预热裸 URL 后立刻拉，仍然是 404。
//
// 参数值用**当前秒级时间戳**：素材内容不变时，同一秒内重复提交会复用
// 同一个键（那时对象早就落盘了，负缓存也过期了，无害）；跨秒则必然是新键。
//
// ⚠️ 只对自有 CDN 域名加。外部图床 / 带签名的地址一律不动（签名链多一个
// 参数就失效了）。

// referenceCacheBusterParam 是 cache-buster 的参数名。
const referenceCacheBusterParam = "wz"

// addReferenceCacheBuster 给素材地址追加 cache-buster 参数。
//
// 已有同名参数时**覆盖**而不是追加第二个 —— 避免 URL 里出现两个 wz=
// 导致缓存键行为不可预期。
func addReferenceCacheBuster(raw string, stamp int64) string {
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
	lowerQuery := strings.ToLower(parsed.RawQuery)
	for _, marker := range signedQueryMarkers {
		if strings.Contains(lowerQuery, marker) {
			return raw
		}
	}
	// 手工拼 query：url.Values.Encode 会把 path 风格的 CI 参数（/）转义成 %2F，
	// 腾讯云就不认那条指令了。所以这里只处理我们自己加的普通参数，
	// 保留其余 query 原样。
	//
	// 注意 base 必须是**去掉 query 后的地址**：trimmed 里已经带了原 query，
	// 直接在 trimmed 上拼 `?` 会得到 `...?a=1?wz=2` 这种坏 URL。
	base := trimmed
	if i := strings.Index(base, "?"); i >= 0 {
		base = base[:i]
	}
	q := parsed.RawQuery
	newParam := referenceCacheBusterParam + "=" + strconv.FormatInt(stamp, 10)
	if q == "" {
		return base + "?" + newParam
	}
	// 去掉已有的 wz=（可能重复），再追加新的。
	parts := strings.Split(q, "&")
	kept := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		key := p
		if i := strings.Index(p, "="); i >= 0 {
			key = p[:i]
		}
		if strings.EqualFold(key, referenceCacheBusterParam) {
			continue
		}
		kept = append(kept, p)
	}
	kept = append(kept, newParam)
	return base + "?" + strings.Join(kept, "&")
}
