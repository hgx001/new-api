package youzanwan3

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

// 提交上游前的**素材预热**（2026-11-21）。
//
// ════════════════════════════════════════════════════════════════════════
// 为什么需要预热：这不是「上游慢」，是 CDN **404 负缓存毒化**
// ════════════════════════════════════════════════════════════════════════
//
// 上游的报错是 asset_download_failed（连接素材服务器超时），现场四次：
//
//	11-20 19:26:39  req_8524e7ff…  5 张  耗时 88s
//	11-20 19:40:29  req_f94b226a…  5 张  耗时 33s
//	11-21 09:53:11  req_077eb751…  7 张  耗时 114s
//	11-21 10:02:39  req_1a8afa46…  7 张  耗时 71s
//
// 逐条排除了所有「素材本身有问题」的假设（2026-11-21 实测）：
//
//   - 9 个 CDN 边缘节点逐个探测 → 全 200；
//   - HEAD / Range / HTTP1.0 → 全正常；
//   - 阿里云（与上游同在杭州）侧解析到的 5 个节点 → 全 200；
//   - 上游视角并发冷拉 7 张 × 12 轮 = **84 次全 200**；
//   - 素材在 COS 里 HEAD 得到的长度与本地完全一致（891238 / 495207 …）。
//
// 也就是说**素材是好的**。真正的原因是 arcmedia 侧的一个竞态：
//
// 源站 lib/media_cdn.py 的 upload_ref_media_sync() 按内容 sha256 生成幂等键
//
//	projects/video-studio/refs/image/{sha256[:16]}/{name}
//
// 这个 key **内容相同就永远是同一个 key**。于是当同一个素材内容被第二次
// 引用时：
//
//  1. 第一次（更早）该 key 在 COS 里还不存在的那个瞬间，有人访问过它
//     （源站自己的预检、上游的探测、别处的引用……）；
//  2. CDN 把这个 **404 缓存住了**（实测第 2 次起就是 X-Cache-Lookup: Cache Hit）；
//  3. 后来素材 put 进 COS 了，但 CDN 那条 404 负缓存**还没过期**；
//  4. 上游此刻来拉 → 拿到 404 → 上游把它当成「素材服务器连不上」→
//     asset_download_failed。
//
// 完整复现（2026-11-21 源站实测，用真实 870KB jpg）：
//
//	建 404 负缓存 → put 到 COS → put 后立刻拉 → **404**（0.04s 秒回，缓存命中）
//	→ +5s 仍 404 → +10s 仍 404 → **+15s 才恢复 200**
//
// 负缓存 TTL 实测 **10.4 秒**。而上游的失败耗时是 33~114 秒 —— 它在
// 负缓存窗口内并发拉 5~7 张，每张都只等几秒就判超时，整单作废。
//
// 为什么源站已有的「300 秒冷却期」救不了这个场景：
//
// 源站 lib/media_cdn.py 的 CDN_REDIRECT_MIN_AGE_SECONDS = 300 保护的是
// **源站自己的 files 路由**（刚上传的媒体不走 302，继续本地直出）。但上游
// 拿到的从来不是源站地址，而是 CDN 直链 —— 那 300 秒冷却对上游完全不生效。
//
// ════════════════════════════════════════════════════════════════════════
// 修法：提交上游之前，先用**完全相同的 URL** 把 CDN 填热
// ════════════════════════════════════════════════════════════════════════
//
// 关键点是「URL 必须与上游将要请求的那个一模一样」：
//
//   - 带 CI 参数的地址与裸地址是**两个不同的缓存键**（实测带 ?imageMogr2 的
//     请求是 Cache Miss 重新回源）。所以预热必须带上 CI 参数，否则预热的是
//     另一条缓存键，上游照样撞上负缓存。
//   - 预热请求本身会**重新回源并拿到 200**，把这条缓存键从「负缓存」改成
//     「正缓存」，上游随后拉就是 Cache Hit（实测 0.05s）。
//
// 为什么预热能成功而不是被负缓存挡住：带 CI 参数的 URL 是一条**新的缓存键**，
// 它的第一次请求必然回源；而此时 COS 里对象已经在了（源站是 put 完才把 URL
// 交给上游的），所以回源直接拿到 200。
//
// 代价：每张图多一次回源（国内 0.3~1.1s），并发做，总耗时约等于最慢那张。
// 换来的是「上游一次拉成」，比失败后重试（上游已经超时 88 秒）便宜得多。
//
// 安全性：预热只 GET/HEAD 我们自己域名下的素材地址，不产生任何上游任务、
// 不产生任何计费。失败一律忽略 —— 预热是「尽力而为」的加速，不是正确性
// 前提：真预热失败还有上游重试兜底，绝不能因为预热失败就拒绝客户请求。

const (
	// referenceWarmupTimeout 是单张素材预热的超时。
	//
	// 不能太长：预热是并发的，总耗时取最慢那张；太长会把提交预算耗光。
	// 国内回源实测 0.3~1.1s，10 秒足够覆盖冷启动与偶发抖动。
	referenceWarmupTimeout = 10 * time.Second

	// referenceWarmupConcurrency 限制并发预热数。
	//
	// 上限 8 是因为一次请求最多 10 张参考图（promo 族），而且并发拉太多
	// 会把 CDN 回源打满、反而拖慢每一张。7 张实测并发全 200。
	referenceWarmupConcurrency = 8
)

// referenceWarmupClient 是预热用的 HTTP 客户端。
//
// 刻意不复用渠道的客户端：预热不是上游请求，不该带渠道的鉴权头、超时设置
// 和重试策略，也绝不能因为预热失败影响上游提交。
var referenceWarmupClient = &http.Client{
	Timeout: referenceWarmupTimeout,
	// 不跟随重定向：素材地址不应重定向，真重定向了说明地址不对，
	// 跟随过去只会浪费带宽并把别的对象拉一遍。
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// warmupReferenceMedia 在提交上游前预热参考图，让 CDN 边缘把素材填热。
//
// 只处理 reference_image：视频/音频的体积与回源特征完全不同（音频常被
// 拉成整段），实测的问题也只出在图片上。
//
// 返回值是**预热后**的素材列表（URL 与入参一致，只是顺序可能变）。
// 预热失败不返回错误 —— 见文件头「预热失败一律忽略」。
func warmupReferenceMedia(ctx context.Context, media []relaycommon.TaskMedia) []relaycommon.TaskMedia {
	out := append([]relaycommon.TaskMedia(nil), media...)
	if len(out) == 0 {
		return out
	}

	// 只收集需要预热的 URL（去重：同一张图在一次请求里出现多次很常见）。
	targets := make([]string, 0, len(out))
	seen := make(map[string]struct{}, len(out))
	for i := range out {
		if out[i].Type != "reference_image" {
			continue
		}
		u := strings.TrimSpace(out[i].URL)
		if u == "" {
			continue
		}
		if !isWarmupableReferenceURL(u) {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		targets = append(targets, u)
	}
	if len(targets) == 0 {
		return out
	}

	// 并发预热，全部完成再返回：必须等齐，因为上游请求紧接着就发出去。
	sem := make(chan struct{}, referenceWarmupConcurrency)
	var wg sync.WaitGroup
	for _, u := range targets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			warmupOne(ctx, target)
		}(u)
	}
	wg.Wait()
	return out
}

// warmupReferenceMediaFromBody 从**已构建好的上游请求体**里取出素材 URL 并预热。
//
// 为什么从请求体取而不是从 req.Media 取：
//
// 上游将要请求的是**加工后**的地址（促销族会追加 CI 参数）。CDN 的缓存键
// **包含 query 字符串**，所以：
//
//	裸地址        → 缓存键 A
//	带 CI 参数地址 → 缓存键 B
//
// 两条键互相独立。用裸地址预热填热的是键 A，上游请求的是键 B —— 上游
// 照样撞上键 B 的 404 负缓存。所以预热必须用请求体里的那个确切地址。
//
// 请求体的素材有两种形态（上游契约不同）：
//
//   - {"media":[{"type":"reference_image","url":"..."}]}   ← media 契约
//   - {"images":["...","..."],"videos":[...],"audios":[...]} ← 顶层数组契约
//
// 两种都要认：promo 族走前者，wan-3.0 / wan-3.0-pro 走后者。
//
// 解析失败一律忽略：预热是加速手段，不是正确性前提，绝不能因为请求体
// 解析不了就拒绝客户请求。
func warmupReferenceMediaFromBody(ctx context.Context, bodyBytes []byte) {
	urls := collectReferenceImageURLsFromBody(bodyBytes)
	if len(urls) == 0 {
		return
	}
	media := make([]relaycommon.TaskMedia, 0, len(urls))
	for _, u := range urls {
		media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: u})
	}
	warmupReferenceMedia(ctx, media)
}

// addReferenceCacheBusterToBody 给请求体里所有参考图 URL 追加 cache-buster 参数。
//
// 返回改写后的请求体与是否成功改写：
//   - 解析失败 / 没有参考图 / 没有任何地址可改 → 原样返回、false。
//
// 改写的是**已经构建好的上游请求体**，所以改完的 URL 就是上游即将请求的
// URL —— 随后的预热用同一份 body，两边必然是同一条 CDN 缓存键。
//
// 同时支持两种素材形态（media 契约 / 顶层 images 数组），与
// collectReferenceImageURLsFromBody 的口径一致。
func addReferenceCacheBusterToBody(bodyBytes []byte, stamp int64) ([]byte, bool) {
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return bodyBytes, false
	}
	changed := false

	// 形态一：media 数组。
	if raw, ok := payload["media"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := obj["type"].(string); t != "" && t != "reference_image" {
					continue
				}
				u, ok := obj["url"].(string)
				if !ok || strings.TrimSpace(u) == "" {
					continue
				}
				if newURL := addReferenceCacheBuster(u, stamp); newURL != u {
					obj["url"] = newURL
					changed = true
				}
			}
		}
	}

	// 形态二：顶层 images 数组。
	if raw, ok := payload["images"]; ok {
		if list, ok := raw.([]any); ok {
			for i, item := range list {
				u, ok := item.(string)
				if !ok || strings.TrimSpace(u) == "" {
					continue
				}
				if newURL := addReferenceCacheBuster(u, stamp); newURL != u {
					list[i] = newURL
					changed = true
				}
			}
		}
	}

	if !changed {
		return bodyBytes, false
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return bodyBytes, false
	}
	return out, true
}

// collectReferenceImageURLsFromBody 解析请求体，收集所有参考图 URL。
//
// 只收「看起来像图片 URL」的条目：media 契约里靠 type == "reference_image"
// 判定，顶层数组契约里 images 数组天然就是图片，videos/audios 一律不收。
func collectReferenceImageURLsFromBody(bodyBytes []byte) []string {
	var payload map[string]any
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		return nil
	}
	var urls []string

	// 形态一：media 数组（type 判定）。
	if raw, ok := payload["media"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				obj, ok := item.(map[string]any)
				if !ok {
					continue
				}
				if t, _ := obj["type"].(string); t != "" && t != "reference_image" {
					continue
				}
				if u, ok := obj["url"].(string); ok && strings.TrimSpace(u) != "" {
					urls = append(urls, u)
				}
			}
		}
	}

	// 形态二：顶层 images 数组（只有图片，没有类型标记）。
	if raw, ok := payload["images"]; ok {
		if list, ok := raw.([]any); ok {
			for _, item := range list {
				if u, ok := item.(string); ok && strings.TrimSpace(u) != "" {
					urls = append(urls, u)
				}
			}
		}
	}
	return urls
}

// warmupOne 预热单个素材地址。
//
// 只发 HEAD：HEAD 就足以让 CDN 回源并缓存这条 URL（实测 HEAD 返回 200
// 后，随后的 GET 是 Cache Hit），比 GET 省掉整个响应体的传输。
func warmupOne(ctx context.Context, target string) {
	ctx, cancel := context.WithTimeout(ctx, referenceWarmupTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, target, nil)
	if err != nil {
		return
	}
	resp, err := referenceWarmupClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	// 把 body 读掉丢弃，让连接能立刻复用（HEAD 的 body 通常是空的，
	// 但 Go 仍要求消费或关闭后才算完整）。
	_, _ = io.Copy(io.Discard, resp.Body)
}
