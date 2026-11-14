package manwu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// COS 直传预签名客户端（2026-11-14）。
//
// 为什么要有这条链路：
//
//	客户参考素材常放在境外服务器（实测 media.threefatai.com 挂 Cloudflare
//	免费版，从广州腾讯云回源仅 67~79 KB/s）。现有两条上传路都让字节**经过**
//	我们的服务器：
//	  · POST /v1/uploads 薄转发 staging 图床 → 走老机 3Mbps 出口；
//	  · 建单时 _download_url_inputs 代下载境外 URL → 同样吃出口，且受
//	    ArcReel 侧 60s 硬总预算约束 —— 按 67 KB/s 反推，**超过 4MB 必定 400**。
//
//	直传方案：网关只负责**签发**一个 COS 预签名 PUT URL，客户端把文件直接
//	PUT 到对象存储（境内内网，字节不经我们的服务器），拿回境内 CDN URL 建单。
//	建单时 ArcReel 从 CDN 拉取，毫秒级完成。
//
// 密钥只在 ArcReel 一侧（ARCREEL_COS_SECRET_*），网关不持有、也不需要。

const cosPresignPath = "/api/v1/remote-generation/cos-upload/presign"

// cosPresignTimeout：签发是纯签名计算（不传字节），10s 足够。
const cosPresignTimeout = 10 * time.Second

// CosPresignResult 是 ArcReel 返回的预签名结果。
type CosPresignResult struct {
	// URL 是预签名 PUT 地址，客户端直接 PUT 到这里。
	URL string `json:"url"`
	// ObjectURL 是对象的公网只读地址（COS 或 CDN），建单时用它作为参考图 URL。
	ObjectURL string `json:"object_url"`
	// Key 是对象键。
	Key string `json:"key"`
	// ExpiresIn 是预签名有效期（秒）。
	ExpiresIn int `json:"expires_in"`
	// MaxBytes 是单文件上限（字节）。
	MaxBytes int64 `json:"max_bytes"`
	// ContentType 是客户端 PUT 时必须带的 Content-Type（已签入签名）。
	ContentType string `json:"content_type"`
}

// PresignCosUpload 向 ArcReel 请求一个 COS 直传预签名。
//
// baseURL/serviceToken 来自 manwu 渠道配置（与建单同源）。filename 只用来
// 决定格式（扩展名），对象键由服务端生成。
//
// 非 2xx 一律报错并携带状态码与响应摘要，让客户端能给出可读提示。
func PresignCosUpload(ctx context.Context, client *http.Client, baseURL, serviceToken, filename string) (*CosPresignResult, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = baseURLDefault
	}

	payload, err := json.Marshal(map[string]string{"filename": filename})
	if err != nil {
		return nil, fmt.Errorf("cos 预签名请求构造失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, cosPresignTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+cosPresignPath, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("cos 预签名请求构造失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+serviceToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cos 预签名请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<10))
	if err != nil {
		return nil, fmt.Errorf("cos 预签名响应读取失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("cos 预签名失败: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	var result CosPresignResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("cos 预签名响应解析失败: %w", err)
	}
	if result.URL == "" || result.ObjectURL == "" {
		return nil, fmt.Errorf("cos 预签名响应缺 url/object_url 字段: %s", truncateBody(body))
	}
	return &result, nil
}
