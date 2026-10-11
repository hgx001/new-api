package manwu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strings"
	"time"
)

// staging 暂存图床客户端（2026-10-11）。
//
// 工作台本地参考图走本渠道时，adaptor 的 assertHTTPURL 只放行公网 http(s) URL，
// 本机文件/data URI 注定 400。闭环方案：new-api 开 POST /v1/uploads 薄转发——
// 用户 token 鉴权后把 multipart 原样转投 ArcReel 暂存图床（service token 鉴权），
// 拿回站内路径拼成公网绝对 URL 交给调用方。文件字节不在 new-api 落盘。

const stagingUploadPath = "/api/v1/remote-generation/staging/uploads"

// stagingUploadTimeout：参考图单张通常 1-5MB，30s 已远超慢链路需求。
const stagingUploadTimeout = 30 * time.Second

// StagingUpload 把一张图片转发到 ArcReel 暂存图床，返回站内可回源路径
// （形如 /api/v1/remote-generation/staging/<32hex>/image.png）。
//
// baseURL/serviceToken 来自 manwu 渠道配置（与建单同源）；client 由调用方注入
// （生产走渠道代理配置，测试注入 httptest client）。非 2xx 一律报错并携带
// 状态码与响应摘要，让 App 端能给用户可读提示（413 超限 / 400 非图片等）。
func StagingUpload(ctx context.Context, client *http.Client, baseURL, serviceToken, filename, contentType string, data []byte) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = baseURLDefault
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	// 不用 CreateFormFile：它写死 application/octet-stream，下游只能靠魔数兜底。
	// 用 CreatePart 带上声明类型（ArcReel 仍按魔数复核，声明值只是第一道信号）。
	disposition := mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": filename})
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", disposition)
	if contentType != "" {
		header.Set("Content-Type", contentType)
	} else {
		header.Set("Content-Type", "application/octet-stream")
	}
	part, err := writer.CreatePart(header)
	if err != nil {
		return "", fmt.Errorf("staging multipart 构造失败: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("staging multipart 写入失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("staging multipart 收尾失败: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, stagingUploadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+stagingUploadPath, &buf)
	if err != nil {
		return "", fmt.Errorf("staging 请求构造失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+serviceToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", writer.FormDataContentType())

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("staging 上传失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if err != nil {
		return "", fmt.Errorf("staging 上传响应读取失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("staging 上传失败: HTTP %d: %s", resp.StatusCode, truncateBody(body))
	}

	stagingPath := extractJSONString(body, "path")
	if stagingPath == "" {
		return "", fmt.Errorf("staging 上传响应缺 path 字段: %s", truncateBody(body))
	}
	return stagingPath, nil
}

// StagingURL 把站内暂存路径拼成公网绝对 URL（调用方塞进 input_reference）。
func StagingURL(baseURL, stagingPath string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = baseURLDefault
	}
	return baseURL + "/" + strings.TrimLeft(strings.TrimSpace(stagingPath), "/")
}

// truncateBody 限制错误里的响应摘要长度，避免把整页反代 HTML 塞进错误链。
func truncateBody(body []byte) string {
	const limit = 200
	text := strings.TrimSpace(string(body))
	if len(text) > limit {
		text = text[:limit]
	}
	return text
}

// extractJSONString 极简 JSON 字符串字段提取：只为读 path，不引解码依赖树
// （adaptor 现有解析同样走轻量手写，保持同风格）。
func extractJSONString(body []byte, key string) string {
	needle := `"` + key + `"`
	idx := bytes.Index(body, []byte(needle))
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(needle):]
	rest = bytes.TrimLeft(rest, " \t\r\n:")
	if len(rest) == 0 || rest[0] != '"' {
		return ""
	}
	rest = rest[1:]
	end := bytes.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	value := string(rest[:end])
	// path 只允许 ASCII 路径段，反斜杠转义不在预期内；出现即视为脏响应拒绝。
	if strings.Contains(value, "\\") {
		return ""
	}
	return path.Clean(value)
}
