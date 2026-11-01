package wan3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	pkgerrors "github.com/pkg/errors"
)

const (
	// referenceImageProbeTimeout 是单张参考图的探测超时。
	// 上游拿到的是 URL 本体、在**生成阶段**才去下载，坏链接会让用户
	// 白等一两分钟再收到 InvalidParameter（还要走一遍预扣/退款）。
	// 这里在提交前用一次轻量探测把这类请求直接挡在门外。
	referenceImageProbeTimeout = 8 * time.Second

	// referenceImageProbeTotalTimeout 是一次请求里所有参考图的**总预算**。
	// 上限 10 张，若每张都等 8 秒，客户端要挂 80 秒才拿到 400 ——
	// 比上游失败还难受。总预算耗尽后剩余图片直接判失败（宁可拒单，不吊住连接）。
	referenceImageProbeTotalTimeout = 15 * time.Second

	// maxReferenceImageBytes 与上游对参考图的体积上限对齐（20MB）。
	maxReferenceImageBytes = 20 << 20

	// probeRangeBytes 是退化为 Range GET 时只读取的头部字节数。
	// 只用来判 Content-Type 与魔数，不需要整张图。
	probeRangeBytes = 1024
)

// validateReferenceImageURLs 在提交前确认每张参考图都抓得到。
//
// 为什么必须做（2026-10-30 线上实证）：
//   - wan3 渠道把客户端的 media.url **原样**塞进上游 input.media，网关不下载、
//     不校验，DashScope 在生成阶段才去拉图；
//   - 客户给的签名 URL 会短时失效/瞬时不可达，结果是一批任务在 80 秒后
//     报 `InvalidParameter: Failed to download ...`，用户看到的是「生成完了
//     却没有参考图」，还占了一次预扣与退款；
//   - 更隐蔽的情况：上游拉图失败但没有硬失败时，成片照样出，只是完全没有
//     参考图 —— 用户只能靠肉眼发现。
//
// 校验口径刻意宽松：只要能拿到 2xx + image/* + 合法图片头就放行，
// 不做尺寸/清晰度判断（那是上游与产品侧的事）。
func validateReferenceImageURLs(media []relaycommon.TaskMedia) error {
	ctx, cancel := context.WithTimeout(context.Background(), referenceImageProbeTotalTimeout)
	defer cancel()
	for index, item := range media {
		if !isImageMediaType(item.Type) {
			continue
		}
		if err := probeReferenceImage(ctx, item.URL); err != nil {
			return fmt.Errorf("%s #%d (%s) is not usable: %w",
				imageMediaLabel(item.Type), index+1, redactMediaURL(item.URL), err)
		}
	}
	return nil
}

// isImageMediaType 判断这一项是不是「图片」，即需要下载校验的那类。
//
// first_frame / last_frame 必须算进来：上游对它们的处理和参考图完全一样
// （直传 URL、生成阶段才下载）。2026-11-01 线上两单都是首帧指向境外图床，
// 因为这里只认 reference_image 而跳过了探测，坏链接直接进上游，
// 用户白等 21 秒到 3 分钟才收到 `Failed to download`。
func isImageMediaType(mediaType string) bool {
	switch mediaType {
	case "reference_image", "first_frame", "last_frame":
		return true
	default:
		return false
	}
}

// imageMediaLabel 让报错能直接指出是哪一种图，而不是一律说成参考图。
func imageMediaLabel(mediaType string) string {
	switch mediaType {
	case "first_frame":
		return "first frame"
	case "last_frame":
		return "last frame"
	default:
		return "reference image"
	}
}

// probeReferenceImage 探测单张参考图：data URL 本地校验，
// http(s) 先 HEAD，HEAD 被服务端拒绝时退化为 Range GET。
func probeReferenceImage(ctx context.Context, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return errors.New("url is required")
	}
	if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
		return probeReferenceImageDataURL(rawURL)
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("requires http(s) URL or data URL")
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode,
		fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain); err != nil {
		return pkgerrors.Wrap(err, "URL blocked")
	}

	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return pkgerrors.Wrap(err, "new http client failed")
	}
	if client == nil {
		client = http.DefaultClient
	}

	// 先 HEAD：拿到 Content-Type / Content-Length 就能判完，不浪费带宽。
	// 只有「服务端回了状态码」才退化 GET（403/405 常见于拒 HEAD 的 CDN）；
	// 连不上/超时直接判失败 —— 再发一次 GET 只会把等待时间翻倍。
	if err, retryWithGet := probeReferenceImageHead(ctx, client, rawURL); err == nil {
		return nil
	} else if !retryWithGet {
		return err
	}
	return probeReferenceImageRange(ctx, client, rawURL)
}

// probeReferenceImageHead 返回 (错误, 是否值得再用 GET 试一次)。
func probeReferenceImageHead(ctx context.Context, client *http.Client, rawURL string) (error, bool) {
	requestCtx, cancel := context.WithTimeout(ctx, referenceImageProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err, false
	}
	response, err := client.Do(request)
	if err != nil {
		return err, false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		// 服务端有应答但拒了 HEAD：换 GET 再确认一次，避免误杀。
		return fmt.Errorf("HEAD returns HTTP %d", response.StatusCode), true
	}
	if err := checkImageContentType(response.Header.Get("Content-Type")); err != nil {
		return err, false
	}
	if response.ContentLength > maxReferenceImageBytes {
		return fmt.Errorf("image is %d bytes, exceeds the %d byte limit",
			response.ContentLength, maxReferenceImageBytes), false
	}
	return nil, false
}

func probeReferenceImageRange(ctx context.Context, client *http.Client, rawURL string) error {
	requestCtx, cancel := context.WithTimeout(ctx, referenceImageProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeRangeBytes-1))
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("GET returns HTTP %d", response.StatusCode)
	}
	if err := checkImageContentType(response.Header.Get("Content-Type")); err != nil {
		return err
	}
	head := make([]byte, probeRangeBytes)
	n, err := io.ReadFull(response.Body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fmt.Errorf("read failed: %w", err)
	}
	if !looksLikeImage(head[:n]) {
		return errors.New("response body is not a JPEG/PNG/WebP/GIF/BMP image")
	}
	return nil
}

// probeReferenceImageDataURL 校验内联 data URL：体积与图片头都过一遍。
// 数据本就随请求体驻留内存，不额外下载。
func probeReferenceImageDataURL(rawURL string) error {
	comma := strings.Index(rawURL, ",")
	if comma < 0 {
		return errors.New("malformed data URL")
	}
	header := strings.ToLower(rawURL[:comma])
	if !strings.HasPrefix(header, "data:image/") {
		return errors.New("data URL is not an image")
	}
	payload := rawURL[comma+1:]
	if len(payload) > maxReferenceImageBytes*4/3+4 {
		return fmt.Errorf("image exceeds the %d byte limit", maxReferenceImageBytes)
	}
	return nil
}

func checkImageContentType(contentType string) error {
	value := strings.ToLower(strings.TrimSpace(contentType))
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	// 有些对象存储对图片回 application/octet-stream，此时靠魔数兜底，
	// 不能只看 Content-Type 就把请求拒掉。
	if value == "" || value == "application/octet-stream" || value == "binary/octet-stream" {
		return nil
	}
	if !strings.HasPrefix(value, "image/") {
		return fmt.Errorf("content type %q is not an image", value)
	}
	return nil
}

// looksLikeImage 用文件头判断是不是真的图片。
// 目的：拦住「200 + image/png 但正文是错误页/登录页」这类假图。
func looksLikeImage(head []byte) bool {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF: // JPEG
		return true
	case len(head) >= 8 && head[0] == 0x89 && head[1] == 'P' && head[2] == 'N' && head[3] == 'G': // PNG
		return true
	case len(head) >= 6 && head[0] == 'G' && head[1] == 'I' && head[2] == 'F': // GIF
		return true
	case len(head) >= 2 && head[0] == 'B' && head[1] == 'M': // BMP
		return true
	case len(head) >= 12 && head[0] == 'R' && head[1] == 'I' && head[2] == 'F' && head[3] == 'F' &&
		head[8] == 'W' && head[9] == 'E' && head[10] == 'B' && head[11] == 'P': // WebP
		return true
	default:
		return false
	}
}

// describeMedia 把 media 汇总成一行可检索的日志：类型计数 + 去签名的来源。
// 只记 host 与路径，不带查询串（签名凭证不落日志）。
func describeMedia(media []relaycommon.TaskMedia) string {
	counts := make(map[string]int)
	sources := make([]string, 0, len(media))
	for _, item := range media {
		counts[item.Type]++
		sources = append(sources, item.Type+"="+redactMediaURL(item.URL))
	}
	return fmt.Sprintf("images=%d videos=%d audios=%d sources=[%s]",
		counts["reference_image"], counts["reference_video"], counts["reference_audio"],
		strings.Join(sources, " "))
}

// redactMediaURL 去掉 URL 的查询串再写日志/报错。
// 客户的参考图链接普遍带签名（tk=...），原样落日志等于把取图凭证写进日志。
//
// data URL 是另一类敏感：整张图片（base64）都在字符串里。原实现只清 query string，
// 而 data URL 的 query 本来就是空，payload 一字不漏地透进日志（describeMedia 的
// "wan3 media accepted:" 那行）。base64 通道上线后首尾帧会走这条路，所以这里
// 对 data scheme 单独处理：只留 MIME 与长度摘要，不留任何 payload。
func redactMediaURL(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return redactDataURL(trimmed)
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "(unparsable url)"
	}
	if parsed.Scheme == "" {
		return "(relative url)"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// redactDataURL 把 data URL 压成 "data:<mime>;base64,<n bytes>" 的摘要。
// mime 从头段原样取（限长），长度按 payload 字节数给，便于排查时确认图有多大。
func redactDataURL(rawURL string) string {
	comma := strings.Index(rawURL, ",")
	if comma < 0 {
		return "(malformed data url)"
	}
	header := rawURL[:comma]
	payload := rawURL[comma+1:]
	// 头段只保留 mime（去掉 ;base64 之后的参数），并限长防畸形输入撑爆日志。
	mime := header
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = mime[:idx]
	}
	if len(mime) > 64 {
		mime = mime[:64]
	}
	if !strings.HasPrefix(strings.ToLower(mime), "data:") {
		return "(malformed data url)"
	}
	return fmt.Sprintf("%s;base64,<%d bytes>", mime, len(payload))
}
