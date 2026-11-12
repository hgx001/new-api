package wan3

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting/cosdirect_setting"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// ─────────────────────────────────────────────────────────────────────────────
// COS 产物回源：生成临时签名直链。
//
// 为什么不用手写 COS 签名：
//   · COS 是 XML API + sha1 签名体系，与腾讯云其它服务的 TC3-HMAC-SHA256 **完全不同**；
//   · 2026-10-29 手写连错三次（StringToSign 结构、canonical headers/query 拼接），
//     最后改用官方 SDK 才通。手写一遍纯属重复踩坑。
//
// 为什么要签名直链而不是本站转发：
//   · 本机公网出口只有 3Mbps，30MB 的 720P 成片要走 ~87 秒；
//   · 客户端直连 COS/CDN 只要 1.55 秒（边缘命中）。
//   所以签好临时链接后 302 透传，字节根本不经过本机。
//
// ─────────────────────────────────────────────────────────────────────────────

// cosPresignTTL 预签名链接有效期。
//
// ⚠️ 不能太短：任务完成后用户可能过一会儿才来取片（App 缓存、用户稍后下载）。
// 也不能太长：链接一旦泄露，在有效期内任何人都能拿到成片。
// 2 小时是个折中 —— 覆盖绝大多数「生成完再来看」的场景。
const cosPresignTTL = 2 * time.Hour

// PresignCOSURL 为 COS 对象生成一个临时下载直链。
//
// 凭据按桶匹配：桶属于 cosdirect_setting（直存链路）时用直存密钥；
// 其余桶走 tencentmps_setting（MPS 超分/转存链路）。两套链路分属
// 不同腾讯云账号，用错凭据必然 403。
//
// 返回错误时调用方应回落到「本站代理下载」或直接报错 ——
// 绝不返回空字符串让客户端去请求一个空地址。
func PresignCOSURL(ctx context.Context, bucket, region, objectPath string) (string, error) {
	key := strings.TrimPrefix(objectPath, "/")
	if key == "" {
		return "", fmt.Errorf("empty cos object path")
	}

	secretId, secretKey := "", ""
	if direct := cosdirect_setting.Get(); direct.Enabled &&
		direct.Bucket == bucket && direct.Region == region {
		secretId, secretKey = direct.SecretId, direct.SecretKey
	} else {
		cfg := tencentmps_setting.Get()
		secretId, secretKey = cfg.SecretId, cfg.SecretKey
	}
	if secretId == "" || secretKey == "" {
		return "", fmt.Errorf("cos credentials for bucket %s are not configured", bucket)
	}

	bucketURL, err := cos.NewBucketURL(bucket, region, true)
	if err != nil {
		return "", fmt.Errorf("build cos bucket url failed: %w", err)
	}

	client := cos.NewClient(
		&cos.BaseURL{BucketURL: bucketURL},
		&http.Client{Timeout: 30 * time.Second},
	)

	// GetPresignedURL 直接接受 ak/sk，不需要额外装配 credential。
	signedURL, err := client.Object.GetPresignedURL(ctx, http.MethodGet, key, secretId, secretKey, cosPresignTTL, nil)
	if err != nil {
		return "", fmt.Errorf("presign cos url failed: %w", err)
	}
	return signedURL.String(), nil
}
