package megabyai

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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
	if raw := strings.TrimSpace(req.Resolution); raw != "" {
		if normalized := normalizeWan30Resolution(raw); normalized != "" {
			return normalized, nil
		}
		return "", fmt.Errorf("resolution %q is not supported, use one of %s", raw, strings.Join(wan30Resolutions, ", "))
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["resolution"]; ok {
			text, isString := raw.(string)
			if !isString {
				return "", fmt.Errorf("metadata.resolution must be a string")
			}
			if strings.TrimSpace(text) != "" {
				if normalized := normalizeWan30Resolution(text); normalized != "" {
					return normalized, nil
				}
				return "", fmt.Errorf("metadata.resolution %q is not supported, use one of %s", text, strings.Join(wan30Resolutions, ", "))
			}
		}
	}
	if raw := strings.TrimSpace(req.Size); raw != "" {
		if resolution, ok := resolutionFromWan30Size(raw); ok {
			return resolution, nil
		}
		return "", fmt.Errorf("size %q is not supported, use WxH (e.g. 1280x720) or one of %s", raw, strings.Join(wan30Resolutions, ", "))
	}
	return wan30DefaultResolution, nil
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

	if len(images) > wan30MaxReferenceImages {
		return nil, nil, nil, fmt.Errorf("wan-3.0 accepts at most %d reference images", wan30MaxReferenceImages)
	}
	if len(videos) > wan30MaxReferenceVideos {
		return nil, nil, nil, fmt.Errorf("wan-3.0 accepts at most %d reference videos", wan30MaxReferenceVideos)
	}
	if len(audios) > wan30MaxReferenceAudios {
		return nil, nil, nil, fmt.Errorf("wan-3.0 accepts at most %d reference audios", wan30MaxReferenceAudios)
	}
	return images, videos, audios, nil
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
