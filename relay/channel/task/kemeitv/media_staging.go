package kemeitv

// data URI 图片转存（2026-11-21）。
//
// 背景：客户买的是 wan3.0-video-官网，ArcReel 按官网契约把首帧编码成 data URI
// 放在 metadata.input.media。网关的「官网 → 客美 tj」改派把请求发给客美后，
// 客美只收公网 URL（文档的 images[].url 是 https 地址），data URI 会被它当成
// URL 去抓取而失败。
//
// 解法与 wan3 的 relocateMediaForUpstream 同源：转存到 ArcReel 暂存图床，
// 再把公网地址交给上游。区别只在**失败策略**：
//   - wan3（官网）：暂存图床不可用时原样放行 —— DashScope 自己能读 data URI；
//   - 客美：data URI 必然失败，转存失败必须拒单，否则就是「建单扣费后交付
//     一个没有首帧的成片」。

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	"github.com/QuantumNous/new-api/service"
)

// stagedMediaContextKey 保存校验阶段转存后的素材集合。
//
// 放在校验阶段（预扣费之前）而不是 BuildRequestBody：转存失败属于
// 「必然失败就该拒单」的校验动作，放到 Build 里失败就等于先扣钱再退款。
const stagedMediaContextKey = "kemeitv_staged_media"

// maxStagedImageBytes 单张图片转存上限 —— 与 ArcReel 暂存图床的 25MB 上限一致。
const maxStagedImageBytes = 25 << 20

// stagingUploadFunc 抽象「图片字节 → 公网 URL」，便于测试注入。
type stagingUploadFunc func(ctx context.Context, data []byte, filename, contentType string) (string, error)

// uploadStagedImage 把图片字节转存到 ArcReel 暂存图床并返回公网 URL。
//
// 包级变量是为了测试可替换（与 wan3 的 relocateClient 注入同一思路）。
var uploadStagedImage stagingUploadFunc = func(ctx context.Context, data []byte, filename, contentType string) (string, error) {
	channel, err := resolveStagingChannel()
	if err != nil {
		return "", err
	}
	client, err := service.GetHttpClientWithProxy(channel.GetSetting().Proxy)
	if err != nil {
		return "", err
	}
	stagingPath, err := manwu.StagingUpload(ctx, client, channel.GetBaseURL(), channel.Key, filename, contentType, data)
	if err != nil {
		return "", err
	}
	return manwu.StagingURL(channel.GetBaseURL(), stagingPath), nil
}

// resolveStagingChannel 取用于转存的漫屋渠道（暂存图床所在处）。
//
// 与 wan3 的同名变量一致：列表接口返回的是脱敏记录（Key 为空），
// 选中后必须按 id 回查带 key 的全量记录，否则会以空令牌调用暂存图床。
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

// stageDataURIImages 把图片素材里的 data URI 换成暂存图床公网 URL。
//
// 已经是公网 URL 的素材原样保留；没有任何 data URI 时直接返回原 bundle
// （调用方据此判断要不要写 context）。
func stageDataURIImages(ctx context.Context, bundle *mediaBundle) (*mediaBundle, error) {
	if bundle == nil {
		return bundle, nil
	}

	needsStaging := false
	for _, item := range bundle.Images {
		if isDataURI(item.URL) {
			needsStaging = true
			break
		}
	}
	if !needsStaging {
		return bundle, nil
	}

	out := &mediaBundle{
		Images: append([]mediaItem(nil), bundle.Images...),
		Videos: bundle.Videos,
		Audios: bundle.Audios,
		Files:  bundle.Files,
		Links:  bundle.Links,
	}
	for index, item := range out.Images {
		if !isDataURI(item.URL) {
			continue
		}
		data, mimeType, err := decodeImageDataURL(item.URL)
		if err != nil {
			return nil, err
		}
		url, err := uploadStagedImage(ctx, data, stagedImageFilename(mimeType), mimeType)
		if err != nil {
			// ⚠️ 错误里不能带原始 data URI：整张图的 base64 会进 400 响应与日志。
			return nil, fmt.Errorf("%s 素材转存失败: %w", item.Role, err)
		}
		out.Images[index].URL = strings.TrimSpace(url)
	}
	return out, nil
}

// isDataURI 判断素材地址是不是 data URI（大小写不敏感）。
func isDataURI(raw string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(raw)), "data:")
}

// decodeImageDataURL 解析 base64 编码的图片 data URI，返回字节与 MIME。
func decodeImageDataURL(raw string) ([]byte, string, error) {
	trimmed := strings.TrimSpace(raw)
	comma := strings.IndexByte(trimmed, ',')
	if comma < 0 {
		return nil, "", fmt.Errorf("data URI 缺少逗号分隔符")
	}
	header := trimmed[len("data:"):comma]
	parts := strings.Split(header, ";")
	mimeType := strings.ToLower(strings.TrimSpace(parts[0]))
	if mimeType == "" {
		mimeType = "image/png"
	}
	if !strings.HasPrefix(mimeType, "image/") {
		return nil, "", fmt.Errorf("data URI 仅支持图片素材，收到 %s", mimeType)
	}
	isBase64 := false
	for _, part := range parts[1:] {
		if strings.EqualFold(strings.TrimSpace(part), "base64") {
			isBase64 = true
			break
		}
	}
	if !isBase64 {
		return nil, "", fmt.Errorf("data URI 必须是 base64 编码")
	}
	data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(trimmed[comma+1:]))
	if err != nil {
		return nil, "", fmt.Errorf("data URI base64 解码失败: %w", err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("data URI 内容为空")
	}
	if len(data) > maxStagedImageBytes {
		return nil, "", fmt.Errorf("data URI 图片超过 %d 字节上限", maxStagedImageBytes)
	}
	return data, mimeType, nil
}

// stagedImageFilename 给暂存图床的文件名带上扩展名：
// ArcReel 侧按魔数复核扩展名，两边口径必须一致。
func stagedImageFilename(mimeType string) string {
	switch strings.ToLower(strings.TrimSpace(mimeType)) {
	case "image/jpeg":
		return "image.jpg"
	case "image/webp":
		return "image.webp"
	case "image/bmp":
		return "image.bmp"
	default:
		return "image.png"
	}
}
