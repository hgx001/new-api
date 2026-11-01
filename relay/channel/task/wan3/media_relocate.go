package wan3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
)

// 首帧/尾帧/参考图的「代下载转存」（2026-11-01）。
//
// 背景：DashScope 是「直传 URL、生成阶段才下载」的模式，要求客户给的图片地址
// 在**上游机房**也能打开。实测客户的境外图床过不去：
//   - tmpfiles.org：TCP 层就断（http=000，0.52 秒）
//   - litter.catbox.moe：能连但单次耗时 88.54 秒
// 而 new-api 侧探测（media_probe.go）只是「我们能打开」，不代表上游也能打开 ——
// 探测通过后仍会在生成阶段以 Failed to download 失败，用户白等 1~3 分钟。
//
// 解法：由 new-api 主动把图片拉下来，转存到自家 staging 暂存图床，再把这个
// 必定公网可达的地址交给上游。这样上游不再碰客户的境外图床。
//
// 已有等价范式：youzanwan3 的 uploadAsset（代下载 → POST /v1/assets → 用签名 URL）。
// 这里复用 ArcReel 已有的 staging 暂存图床（POST /remote-generation/staging/uploads），
// 与 controller.UploadStagingFile 是同一套机制。

// relocateClient 抽象「代下载」与「转存」两个外部动作，便于测试注入。
type relocateClient struct {
	// download 取回图片字节与 MIME。
	download func(ctx context.Context, rawURL string) ([]byte, string, error)
	// staging 把字节转存到暂存图床，返回站内路径。
	staging func(ctx context.Context, data []byte, filename, contentType string) (string, error)
	// stagingURL 把站内路径拼成公网绝对 URL。
	stagingURL func(stagingPath string) string
}

// relocateMedia 把 media 里的图片 URL 换成自家 staging 地址。
//
// 只处理图片类素材（reference_image / first_frame / last_frame）：
//   - data URL 原样保留（数据已随请求体到达，无需转存）
//   - 非图片素材（视频/音频）不动 —— 上游对它们只收公网 URL，转存成图片没有意义
//
// 任一环节失败即整体报错，**不回退原 URL**：回退等于把客户的境外图床又交给上游，
// 注定在生成阶段失败，不如在提交前明确拒单。
func relocateMedia(ctx context.Context, media []relaycommon.TaskMedia, client *relocateClient) ([]relaycommon.TaskMedia, error) {
	if client == nil || len(media) == 0 {
		return media, nil
	}
	out := make([]relaycommon.TaskMedia, 0, len(media))
	for index, item := range media {
		if !isImageMediaType(item.Type) {
			out = append(out, item)
			continue
		}
		rawURL := strings.TrimSpace(item.URL)
		// data URL 已内联在请求体里，既不需要下载也不需要转存。
		if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
			out = append(out, item)
			continue
		}
		if !isHTTPURL(rawURL) {
			// 非法 URL 交给后面的探测去报错，这里不重复处理。
			out = append(out, item)
			continue
		}

		data, mimeType, err := client.download(ctx, rawURL)
		if err != nil {
			return nil, fmt.Errorf("%s #%d (%s) download failed: %w",
				imageMediaLabel(item.Type), index+1, redactMediaURL(rawURL), err)
		}
		if len(data) > maxReferenceImageBytes {
			return nil, fmt.Errorf("%s #%d (%s) exceeds the %d byte limit",
				imageMediaLabel(item.Type), index+1, redactMediaURL(rawURL), maxReferenceImageBytes)
		}
		// 「200 + image/png 但正文是错误页/登录页」这类假图要在转存前拦住，
		// 否则会污染暂存图床并让上游拿到一张坏图。
		if !looksLikeImage(data) {
			return nil, fmt.Errorf("%s #%d (%s) is not a valid image",
				imageMediaLabel(item.Type), index+1, redactMediaURL(rawURL))
		}
		if mimeType == "" {
			mimeType = "image/png"
		}

		stagingPath, err := client.staging(ctx, data, relocateFilename(rawURL, mimeType), mimeType)
		if err != nil {
			return nil, fmt.Errorf("%s #%d (%s) staging upload failed: %w",
				imageMediaLabel(item.Type), index+1, redactMediaURL(rawURL), err)
		}
		out = append(out, relaycommon.TaskMedia{
			Type: item.Type,
			URL:  client.stagingURL(stagingPath),
		})
	}
	return out, nil
}

// isHTTPURL 判断是不是 http(s) URL（data URL 另行处理）。
func isHTTPURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// relocatedMediaContextKey 保存 validate 阶段转存后的 media。
//
// 为什么放在 validate 阶段而不是 build：预扣费发生在两者之间
// （relay_task.go 的第 7 步）。转存是「必然失败就该拒单」的校验动作，
// 放在 build 里失败就等于先扣了钱再退款——用户能看到一次「余额被扣又退回」的
// 抖动，而提交失败本不该触发扣费。
const relocatedMediaContextKey = "wan3_relocated_media"

// relocateFilename 给暂存图床的文件名：取原 URL 的 basename，没有就用 mime 推一个。
// 带上扩展名，ArcReel 侧按魔数复核扩展名，两边口径一致。
func relocateFilename(rawURL, mimeType string) string {
	name := ""
	if parsed, err := url.Parse(rawURL); err == nil {
		name = path.Base(parsed.Path)
	}
	name = strings.TrimSpace(name)
	// 形如 ".png" 的空 basename 直接丢。
	if name == "" || name == "." || name == "/" {
		switch mimeType {
		case "image/jpeg":
			name = "image.jpg"
		case "image/webp":
			name = "image.webp"
		case "image/bmp":
			name = "image.bmp"
		default:
			name = "image.png"
		}
	}
	return name
}

// newRelocateClient 组装生产环境的 relocateClient：
// 下载走渠道代理配置，转存走 ArcReel staging 暂存图床。
//
// baseURL/serviceToken 来自 manwu 渠道（暂存图床所在处）——与 controller.UploadStagingFile
// 同源。之所以复用 manwu 渠道而不是给每个 wan3 渠道各配一份：暂存图床只有一处，
// 分散配置必然出现「A 渠道能转存、B 渠道不能」的诡异差异。
func newRelocateClient(baseURL, serviceToken, proxy string) *relocateClient {
	return &relocateClient{
		download: func(ctx context.Context, rawURL string) ([]byte, string, error) {
			return downloadImage(ctx, rawURL, proxy)
		},
		staging: func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
			client, err := service.GetHttpClientWithProxy(proxy)
			if err != nil {
				return "", err
			}
			return manwu.StagingUpload(ctx, client, baseURL, serviceToken, filename, contentType, data)
		},
		stagingURL: func(stagingPath string) string {
			return manwu.StagingURL(baseURL, stagingPath)
		},
	}
}

// resolveStagingChannel 取用于转存的 manwu 渠道（暂存图床所在处）。
//
// 与 controller.resolveManwuStagingChannel 同源：列表接口为脱敏 Omit("key")，
// 拿到的 Key 恒为空，直接当 serviceToken 会发出 "Bearer"（无令牌）被 ArcReel 拒，
// 所以选中后必须按 id 回查带 key 的全量记录。
//
// 包级变量是为了测试可替换。
var resolveStagingChannel = func() (*model.Channel, error) {
	channels, err := model.GetChannelsByType(0, 100, false, constant.ChannelTypeManwu)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch != nil && ch.Status == common.ChannelStatusEnabled {
			return model.GetChannelById(ch.Id, true)
		}
	}
	return nil, fmt.Errorf("没有已启用的漫屋渠道（暂存图床不可用）")
}

// relocateMediaForUpstream 把 media 里的图片 URL 换成自家 staging 地址。
//
// 降级策略：**暂存图床本身不可用**（没配漫屋渠道 / 渠道被禁用）时**不拒单**，
// 而是原样放行 —— 那时客户若给了可达的图床（比如自己公司的 CDN）仍能成功，
// 我们不该因为自己的辅助设施故障而阻断用户的正常请求。
//
// 但「转存过程中某张图拉不到」是**必须拒单**的：那说明客户给的这张图确实有问题，
// 放行等于把它交给上游继续失败，用户白等 1~3 分钟。
func (a *TaskAdaptor) relocateMediaForUpstream(c *gin.Context, media []relaycommon.TaskMedia) ([]relaycommon.TaskMedia, *dto.TaskError) {
	if !hasRelocatableImage(media) {
		return media, nil
	}

	channel, err := resolveStagingChannel()
	if err != nil {
		logger.LogWarn(c, fmt.Sprintf("wan3 media relocate skipped: %s", err.Error()))
		return media, nil
	}

	client := newRelocateClient(channel.GetBaseURL(), channel.Key, channel.GetSetting().Proxy)
	// 用 c 而不是 c.Request.Context()：gin.Context 自身实现了 context.Context，
	// 且在 Request 为 nil 时也不会 panic（测试构造的 Context 常无 Request）。
	relocated, err := relocateMedia(c, media, client)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "invalid_input_reference", http.StatusBadRequest)
	}
	c.Set(relocatedMediaContextKey, relocated)
	return relocated, nil
}

// hasRelocatableImage 判断 media 里有没有需要转存的图片 URL。
// 全是 data URL / 非图片素材时不必白跑一趟下载。
func hasRelocatableImage(media []relaycommon.TaskMedia) bool {
	for _, item := range media {
		if !isImageMediaType(item.Type) {
			continue
		}
		rawURL := strings.TrimSpace(item.URL)
		if strings.HasPrefix(strings.ToLower(rawURL), "data:") {
			continue
		}
		if isHTTPURL(rawURL) {
			return true
		}
	}
	return false
}

// relocateDownloadTimeout 是单张图片代下载的超时。
//
// 实测慢图床（litter.catbox.moe）单次拉取要 88 秒以上，而探测阶段只给 8 秒 ——
// 转存会因此比探测慢一个数量级，拖住整个建单请求。这里给一个独立上限：
// 正常图床秒级返回，慢图床超时即拒单（用户拿明确错误，而不是一直挂着）。
const relocateDownloadTimeout = 30 * time.Second

// downloadImage 代下载一张图片，返回字节与 MIME。
//
// 复用 probe 的 SSRF 校验（防内网探测），但走完整 GET —— 探测只读 1KB 头部，
// 转存需要完整字节。
func downloadImage(ctx context.Context, rawURL, proxy string) ([]byte, string, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode,
		fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, "", fmt.Errorf("URL blocked: %w", err)
	}

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, "", err
	}
	if client == nil {
		client = http.DefaultClient
	}

	// 单图独立超时：不能因为客户给了一个慢图床就无限期占着建单连接。
	downloadCtx, cancel := context.WithTimeout(ctx, relocateDownloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, "", err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "", fmt.Errorf("HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxReferenceImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > maxReferenceImageBytes {
		return nil, "", fmt.Errorf("image exceeds the %d byte limit", maxReferenceImageBytes)
	}

	mimeType := response.Header.Get("Content-Type")
	if index := strings.IndexByte(mimeType, ';'); index >= 0 {
		mimeType = mimeType[:index]
	}
	mimeType = strings.TrimSpace(mimeType)
	// 上游对透明通道 PNG 有明确限制（渲染会出问题），这里不转换格式，
	// 但内容类型要与魔数一致时以魔数为准——对象存储常返回 application/octet-stream。
	if mimeType == "" || mimeType == "application/octet-stream" || mimeType == "binary/octet-stream" {
		mimeType = detectImageMime(data)
	}
	return data, mimeType, nil
}

// detectImageMime 按文件头推断 MIME，识别不出时返回空串。
func detectImageMime(data []byte) string {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) >= 8 && data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G':
		return "image/png"
	case len(data) >= 6 && data[0] == 'G' && data[1] == 'I' && data[2] == 'F':
		return "image/gif"
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "image/bmp"
	case len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P':
		return "image/webp"
	default:
		return ""
	}
}
