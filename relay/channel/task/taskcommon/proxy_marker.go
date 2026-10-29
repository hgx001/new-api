package taskcommon

import (
	"fmt"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// 成片地址占位符（内部标记 → 对外代理地址）
//
// 为什么需要占位符：
//
//	有些成片不在本站手里（在对象存储里），而对象存储的直链是**带签名的、有有效期的**。
//	任务完成后用户可能过一会儿才来取片，把签名直链直接存进库，到期后用户就拿不到成片。
//	所以库里只存一个可解析的标记，取片时再实时签名。
//
// 为什么放在 taskcommon 而不是某个渠道包里：
//
//	这个标记有**三个**消费方，分别在不同层：
//	  · 生产者（渠道包）：后处理成功时写入 ResultURL；
//	  · 取片代理（controller）：解析标记 → 换成临时直链；
//	  · 对外出口（relay）：把标记换成「本站 /content 代理地址」再给客户端。
//	放在 taskcommon 是为了避免 relay → 渠道包 的导入环（渠道包最终依赖 relay/common）。
//
// ⚠️ 占位符是**内部协议**，绝不能原样出现在给客户端的响应里 ——
//	客户端拿到 "cos-proxy://..." 是没法下载的。
// ─────────────────────────────────────────────────────────────────────────────

// ResultURLProxyMarkerPrefix 是成片地址占位符的前缀。
const ResultURLProxyMarkerPrefix = "cos-proxy://"

// BuildCOSProxyMarker 构造成片地址占位符。
//
// 形如：cos-proxy://<bucket>/<region>/<objectPath>
// bucket 与 region 里不含 "/"，objectPath 可以含（多级目录）。
func BuildCOSProxyMarker(bucket, region, objectPath string) string {
	return fmt.Sprintf("%s%s/%s/%s", ResultURLProxyMarkerPrefix, bucket, region, strings.TrimPrefix(objectPath, "/"))
}

// ParseCOSProxyMarker 解析占位符，返回（bucket, region, objectPath, ok）。
// 不是占位符时 ok=false。
func ParseCOSProxyMarker(raw string) (string, string, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(strings.ToLower(trimmed), ResultURLProxyMarkerPrefix) {
		return "", "", "", false
	}
	rest := trimmed[len(ResultURLProxyMarkerPrefix):]
	// bucket/region/objectPath —— bucket 与 region 里不含 "/"，objectPath 可以含。
	parts := strings.SplitN(rest, "/", 3)
	if len(parts) != 3 {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

// IsResultURLProxyMarker 判断一个地址是不是内部占位符。
//
// 对外出口用这个判断「该换成本站代理地址」。
func IsResultURLProxyMarker(raw string) bool {
	_, _, _, ok := ParseCOSProxyMarker(raw)
	return ok
}

// PublicResultURL 把库里的成片地址翻译成**可以给客户端**的下载地址。
//
// 返回值有三种情况：
//
//  1. 库里是内部占位符（cos-proxy://…）→ 返回本站 /content 代理地址。
//     占位符是内部协议，客户端拿到没法下载，必须换掉。
//  2. 库里是绝对地址（http/https）→ 原样返回。
//     客户端直连上游存储/CDN 最快；走本站代理反而多经一次本机出口
//     （本机公网出口 3Mbps，30MB 成片要走 ~87 秒）。
//  3. 库里是站内相对路径（/v1/videos/{id}/content）→ 原样返回。
//     它本来就是代理地址。
//
// taskID 为空时无法构造代理地址，此时占位符一律返回空串 ——
// 宁可不给地址，也不能给一个客户端下不动的内部标记。
func PublicResultURL(taskID, stored string) string {
	trimmed := strings.TrimSpace(stored)
	if trimmed == "" {
		return ""
	}
	if IsResultURLProxyMarker(trimmed) {
		if strings.TrimSpace(taskID) == "" {
			return ""
		}
		return BuildProxyURL(taskID)
	}
	return trimmed
}
