package megabyai

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// wan30Request 是上游「统一参数」契约的提交体。
//
// 契约出处：上游接入文档（https://newapi.megabyai.cc/docs）——视频任务统一用
// duration / ratio / resolution / referenceImages / referenceVideos / referenceAudios，
// 文档同时明确「不要与 seconds / size / image / input_reference 混用」。
// 所以本文件负责把本站的通用入参翻译成上游这套字段，而不是把原始 JSON 透传过去。
type wan30Request struct {
	Model           string   `json:"model"`
	Prompt          string   `json:"prompt,omitempty"`
	Duration        *int     `json:"duration,omitempty"`
	Ratio           string   `json:"ratio,omitempty"`
	Resolution      string   `json:"resolution,omitempty"`
	Seed            *int64   `json:"seed,omitempty"`
	ReferenceImages []string `json:"referenceImages,omitempty"`
	ReferenceVideos []string `json:"referenceVideos,omitempty"`
	ReferenceAudios []string `json:"referenceAudios,omitempty"`
}

// resolveWan30Resolution 把客户端的分辨率表达归一到上游的三档（480p / 720p / 1080p）。
//
// 归一不出来时必须让请求失败，不能默默落到默认档：wan-3.0 是**按次 + 分辨率档**计费
// （上游 480p ¥1.6 / 720p ¥1.8 / 1080p ¥2.0），猜错档位就是直接少收钱。
// 客户端完全没给分辨率时返回默认档，并把这个档位**显式下发**给上游，
// 保证「我们收的价」与「上游收的价」永远是同一档。
func resolveWan30Resolution(req relaycommon.TaskSubmitReq) (string, error) {
	resolution, _, err := resolveWan30ResolutionWithSource(req)
	return resolution, err
}

// resolveWan30ResolutionWithSource 除了档位，还返回「客户端是否给过分辨率」。
//
// 区分「没给」与「给了但不是这一档」是锁定档位模型的前提：
// 没给就用锁定档位（默认），给了别的档位必须报错而不是静默改档。
func resolveWan30ResolutionWithSource(req relaycommon.TaskSubmitReq) (string, bool, error) {
	if raw := strings.TrimSpace(req.Resolution); raw != "" {
		if normalized := normalizeWan30Resolution(raw); normalized != "" {
			return normalized, true, nil
		}
		return "", true, fmt.Errorf("resolution %q is not supported, use one of %s", raw, strings.Join(wan30Resolutions, ", "))
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["resolution"]; ok {
			text, isString := raw.(string)
			if !isString {
				return "", true, fmt.Errorf("metadata.resolution must be a string")
			}
			if strings.TrimSpace(text) != "" {
				if normalized := normalizeWan30Resolution(text); normalized != "" {
					return normalized, true, nil
				}
				return "", true, fmt.Errorf("metadata.resolution %q is not supported, use one of %s", text, strings.Join(wan30Resolutions, ", "))
			}
		}
	}
	if raw := strings.TrimSpace(req.Size); raw != "" {
		if resolution, ok := resolutionFromWan30Size(raw); ok {
			return resolution, true, nil
		}
		return "", true, fmt.Errorf("size %q is not supported, use WxH (e.g. 1280x720) or one of %s", raw, strings.Join(wan30Resolutions, ", "))
	}
	return wan30DefaultResolution, false, nil
}

// resolveLockedResolution 处理「档位被锁死」的模型（如 seedance2.5-特惠 只出 720p）。
//
// 客户端没给分辨率 → 用锁定档位；给了别的档位 → 报错（fail-loud，不静默降档）。
func resolveLockedResolution(req relaycommon.TaskSubmitReq, tier wan30LockedTier) (string, error) {
	resolution, provided, err := resolveWan30ResolutionWithSource(req)
	if err != nil {
		return "", err
	}
	if !provided {
		return tier.Resolution, nil
	}
	if resolution != tier.Resolution {
		return "", fmt.Errorf("this model only supports %s, got %s", tier.Resolution, resolution)
	}
	return resolution, nil
}

// normalizeWan30Resolution 把 "480" / "480P" / "480p" 之类的写法统一成上游档位。
func normalizeWan30Resolution(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "480", "480p":
		return "480p"
	case "720", "720p":
		return "720p"
	case "1080", "1080p":
		return "1080p"
	default:
		return ""
	}
}

// resolutionFromWan30Size 从 size 推断档位：直接写档位名，或按短边映射 WxH。
func resolutionFromWan30Size(size string) (string, bool) {
	if normalized := normalizeWan30Resolution(size); normalized != "" {
		return normalized, true
	}
	width, height, ok := parseWan30Size(size)
	if !ok {
		return "", false
	}
	short := width
	if height < short {
		short = height
	}
	switch {
	case short <= 600:
		return "480p", true
	case short <= 900:
		return "720p", true
	default:
		return "1080p", true
	}
}

// parseWan30Size 解析 "1280x720" / "1280*720" 形式的尺寸。
func parseWan30Size(size string) (int, int, bool) {
	size = strings.ReplaceAll(strings.TrimSpace(size), "*", "x")
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	width, errWidth := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, errHeight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}

// resolveWan30Ratio 决定下发给上游的宽高比。
//
// 优先级：原始请求体的 ratio → metadata.ratio / metadata.aspect_ratio → 由 size 推断。
// 推断不出来就不下发（用上游自己的默认值），不编造比例。
func resolveWan30Ratio(rawRatio string, req relaycommon.TaskSubmitReq) string {
	if ratio := strings.TrimSpace(rawRatio); ratio != "" {
		return ratio
	}
	if req.Metadata != nil {
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if value, ok := req.Metadata[key].(string); ok {
				if value = strings.TrimSpace(value); value != "" {
					return value
				}
			}
		}
	}
	if ratio, ok := ratioFromWan30Size(req.Size); ok {
		return ratio
	}
	return ""
}

// ratioFromWan30Size 把 "1280x720" 之类的尺寸映射成宽高比；差距过大时返回 adaptive。
func ratioFromWan30Size(size string) (string, bool) {
	width, height, ok := parseWan30Size(size)
	if !ok {
		return "", false
	}
	candidates := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
		"4:3":  4.0 / 3.0,
		"3:4":  3.0 / 4.0,
		"1:1":  1.0,
	}
	ratio := float64(width) / float64(height)
	best := ""
	bestDiff := math.MaxFloat64
	for name, value := range candidates {
		if diff := math.Abs(ratio - value); diff < bestDiff {
			best = name
			bestDiff = diff
		}
	}
	if bestDiff > 0.25 {
		return "adaptive", true
	}
	return best, true
}

// buildWan30Media 收集参考素材，按上游统一参数契约分成图片 / 视频 / 音频三组。
//
// 上游 wan-3.0 的素材上限（上游价目说明）：参考图 ≤10、参考视频 ≤5、参考音频 ≤5。
// 数量超限在本地 400 拦下：上游拒单要等它自己发现，而我们已经知道上限。
//
// first_frame / last_frame 也归入 referenceImages：上游统一参数里没有单独的首尾帧字段，
// 它的语义是「首张图即首帧」，与本站 wan3 渠道的口径一致。
func buildWan30Media(req relaycommon.TaskSubmitReq) ([]string, []string, []string, error) {
	images := make([]string, 0, 4)
	videos := make([]string, 0, 2)
	audios := make([]string, 0, 2)

	appendItem := func(kind, raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		if err := validateWan30MaterialURL(kind, raw); err != nil {
			return err
		}
		switch strings.TrimSpace(kind) {
		case "reference_image", "first_frame", "last_frame", "image":
			images = append(images, raw)
		case "reference_video", "video":
			videos = append(videos, raw)
		case "reference_audio", "audio":
			audios = append(audios, raw)
		default:
			return fmt.Errorf("media type %q is not supported", kind)
		}
		return nil
	}

	media := req.Media
	if len(media) == 0 {
		metadataMedia, err := wan30MetadataMedia(req)
		if err != nil {
			return nil, nil, nil, err
		}
		media = metadataMedia
	}
	for _, item := range media {
		if err := appendItem(item.Type, item.URL); err != nil {
			return nil, nil, nil, err
		}
	}
	// req.Images 已由 TaskSubmitReq.UnmarshalJSON 合并了 input_reference。
	for _, url := range req.Images {
		if err := appendItem("reference_image", url); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := appendItem("reference_image", req.Image); err != nil {
		return nil, nil, nil, err
	}
	for _, url := range req.Audios {
		if err := appendItem("reference_audio", url); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := appendItem("reference_audio", req.Audio); err != nil {
		return nil, nil, nil, err
	}

	// 报错文案不能点名上游模型：别名模型（seedance2.5-特惠）走的是同一段校验，
	// 写死 "wan-3.0" 等于把真实上游告诉下游；脱敏层也会把它变成误导性的「上游 accepts...」。
	// 「this model」对两个对外名都成立，也和锁定档位的文案口径一致。
	if len(images) > wan30MaxReferenceImages {
		return nil, nil, nil, fmt.Errorf("this model accepts at most %d reference images", wan30MaxReferenceImages)
	}
	if len(videos) > wan30MaxReferenceVideos {
		return nil, nil, nil, fmt.Errorf("this model accepts at most %d reference videos", wan30MaxReferenceVideos)
	}
	if len(audios) > wan30MaxReferenceAudios {
		return nil, nil, nil, fmt.Errorf("this model accepts at most %d reference audios", wan30MaxReferenceAudios)
	}
	return images, videos, audios, nil
}

// checkWan30ReferenceVideoSeconds 校验参考视频单条时长不超过统一上限（15 秒）。
//
// 上游 wan-3.0 本身就是万相 3.0 系，官方限制是「单个 [1,15] 秒」；本站在提交前
// 探测一次，避免客户白等一轮「建单 → 预扣 → 上游拒绝 → 退款」。
// 上限口径统一由 taskcommon.MaxReferenceVideoSeconds 定义，与 wan3 / 有赞链路同一条线。
func checkWan30ReferenceVideoSeconds(videos []string) error {
	if len(videos) == 0 {
		return nil
	}
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return fmt.Errorf("new http client failed: %s", err.Error())
	}
	unmeasured, err := taskcommon.EnforceReferenceVideoMaxSeconds(context.Background(), videos, taskcommon.MaxReferenceVideoSeconds, client)
	if len(unmeasured) > 0 {
		common.SysLog(fmt.Sprintf("megabyai 参考视频时长无法探测，放行（无法判断是否超限）：%v", unmeasured))
	}
	return err
}

// validateWan30MaterialURL 参考素材必须是上游能自己抓取的公网 http(s) URL。
//
// 上游文档写的是「参考图 URL / 参考视频 URL / 参考音频 URL」——上游要自己去拉这些地址，
// data: / blob: / 相对路径 / 没有 host 的地址对它都不可达，提交上去必然失败：
// 用户付了钱、任务跑完才报「素材取不到」，是最差的一种失败。
// 与 manwu / wan3 通道的既有约定保持一致：非 http(s) 一律本地 400。
func validateWan30MaterialURL(kind, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must be a public http(s) URL, got %q", kind, raw)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%s must be a public http(s) URL, got %q", kind, raw)
	}
	return nil
}

// wan30RestoreImages 把被平台语义覆盖掉的 images 数组找回来。
//
// 平台的 Sora 语义规定：input_reference 是「单图字段」，一旦存在就把整个 images 数组
// 替换成它自己（见 relay/common/relay_utils.go 的 validateMultipartDirect）。
// wan-3.0 支持多参考图（图 ≤10），两者并存时静默丢图 = 用户付了 ¥3 却没带参考图，
// 成片里什么也没有。所以这里从原始请求体里把 images 取回来，与 input_reference 合并。
//
// 只在「input_reference 是字符串（触发覆盖）」且「原始 body 里确有 images」时生效；
// 拿不到原始 body 时保持平台原样（宁可少合也不要凭空编素材）。
func wan30RestoreImages(c *gin.Context, req relaycommon.TaskSubmitReq) (relaycommon.TaskSubmitReq, bool) {
	if strings.TrimSpace(req.InputReference) == "" {
		return req, false
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return req, false
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return req, false
	}
	var body struct {
		Images json.RawMessage `json:"images"`
	}
	if err := common.Unmarshal(raw, &body); err != nil || len(body.Images) == 0 {
		return req, false
	}
	images, err := parseWan30URLList(body.Images)
	if err != nil || len(images) == 0 {
		return req, false
	}
	restored := make([]string, 0, len(images)+1)
	restored = append(restored, images...)
	restored = append(restored, strings.TrimSpace(req.InputReference))
	req.Images = restored
	return req, true
}

// parseWan30URLList 解析「字符串或字符串数组」两种写法（与平台解析 images 的口径一致）。
func parseWan30URLList(raw json.RawMessage) ([]string, error) {
	var list []string
	if err := common.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var single string
	if err := common.Unmarshal(raw, &single); err == nil {
		return []string{single}, nil
	}
	return nil, fmt.Errorf("images must be a string or an array of strings")
}

// wan30MetadataMedia 兼容把素材放在 metadata.media 里的客户端。
func wan30MetadataMedia(req relaycommon.TaskSubmitReq) ([]relaycommon.TaskMedia, error) {
	if req.Metadata == nil {
		return nil, nil
	}
	raw, ok := req.Metadata["media"]
	if !ok {
		if input, inputOK := req.Metadata["input"].(map[string]interface{}); inputOK {
			raw, ok = input["media"]
		}
	}
	if !ok || raw == nil {
		return nil, nil
	}
	encoded, err := common.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var media []relaycommon.TaskMedia
	if err := common.Unmarshal(encoded, &media); err != nil {
		return nil, err
	}
	return media, nil
}

// wan30Duration 解析时长：客户端给 duration 或 seconds 都认，都没给就不下发
// （用上游自己的默认时长，不替客户编一个）。
func wan30Duration(req relaycommon.TaskSubmitReq) *int {
	if req.Duration > 0 {
		duration := req.Duration
		return &duration
	}
	if raw := strings.TrimSpace(req.Seconds); raw != "" {
		if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
			return &seconds
		}
	}
	return nil
}

// wan30LockedDuration 处理「时长被锁死」的模型（如 seedance2.5-特惠 只出 30 秒）。
//
// 客户端没给时长 → 用锁定时长并显式下发；给了别的时长 → 报错（fail-loud）。
// duration 与 seconds 都认，两个字段都校验：给了一致的值没问题，
// 给了互相矛盾的值（duration=30 + seconds=5）也要报错，不能只看一个字段。
func wan30LockedDuration(req relaycommon.TaskSubmitReq, tier wan30LockedTier) (*int, error) {
	if req.Duration != 0 && req.Duration != tier.Duration {
		return nil, fmt.Errorf("this model only supports %d seconds, got duration=%d", tier.Duration, req.Duration)
	}
	if raw := strings.TrimSpace(req.Seconds); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return nil, fmt.Errorf("seconds must be an integer")
		}
		if seconds != tier.Duration {
			return nil, fmt.Errorf("this model only supports %d seconds, got seconds=%s", tier.Duration, raw)
		}
	}
	duration := tier.Duration
	return &duration, nil
}
