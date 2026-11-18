package taskcommon

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/pkg/errors"
)

// ReferenceMediaMinBounds 是三类参考素材的**下限**（由调用方按平台官网标定传入）。
//
// 为什么下限值不写在 taskcommon：共享层只提供「探测 + 比对」能力，具体数值属于
// 平台契约（CapCut 的 2s / 300px / 720px 都在 manwu 侧声明，与 Worker 端
// CAPCUT_REFERENCE_*_BOUNDS 同口径）。0 表示该类不校验。
//
// 为什么要有这道闸门：官网对越界素材是**静默处理**（过短音频静默丢弃、低清视频
// 静默收下但生成阶段判「出错了」、低清参考图静默不填槽），错误要等到任务进到
// Worker 开页上传才暴露，那时额度已扣、页面已白等。在网关 validate 阶段
// （预扣费之前）实测拦截，400 不扣费、不建单、不 AutoBan。
type ReferenceMediaMinBounds struct {
	// AudioMinSeconds 单条参考音频的时长下限（秒）。
	AudioMinSeconds float64
	// ImageMinShortSide 单张参考图的短边下限（px，min(宽,高)，竖屏不误杀）。
	ImageMinShortSide int
	// VideoMinShortSide 单条参考视频的短边下限（px，口径同图片）。
	VideoMinShortSide int
}

// referenceMediaImageProbeBytes 只读参考图头部：image.DecodeConfig 只需文件头。
const referenceMediaImageProbeBytes = 2 << 20

// EnforceReferenceMediaMinBounds 逐条探测三类参考素材，实测低于下限即返回中文错误。
//
// 返回值 unmeasured 列出「没能探测出尺寸/时长」的地址，由调用方记日志（与
// EnforceReferenceAudioMaxSeconds 同款留痕纪律：校验部分失明必须看得见）。
//
// ⚠️ **探测失败一律放行**（fail-open），与 Worker 端 referenceMediaGuard 一致：
//   - 客户素材地址可能只对上游可达（IP 白名单 / 私有签名），我们抓不到不代表越界；
//   - 误杀会把本来能跑的单直接打死，代价高于「漏放到 Worker 端再拦」；
//   - 漏放不是失控——素材进到 Worker 后本地 ffprobe 还会按同一口径再拦一次。
//
// 只有**实测**低于下限才报错：用户照文案换素材即可，换号无效。
func EnforceReferenceMediaMinBounds(
	ctx context.Context, images, videos, audios []string, bounds ReferenceMediaMinBounds, client *http.Client,
) (unmeasured []string, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if bounds.ImageMinShortSide > 0 {
		for i, raw := range images {
			width, height, probeErr := ProbeReferenceImageDimensions(ctx, raw, client)
			if probeErr != nil {
				unmeasured = append(unmeasured, truncateForLog(raw))
				continue
			}
			short := width
			if height < short {
				short = height
			}
			if short < bounds.ImageMinShortSide {
				return unmeasured, errors.Errorf(
					"参考图 #%d 分辨率 %dx%d 的短边 %dpx 低于 %dpx 下限，请换更清晰的图片后重试",
					i+1, width, height, short, bounds.ImageMinShortSide)
			}
		}
	}

	if bounds.VideoMinShortSide > 0 {
		for i, raw := range videos {
			width, height, probeErr := ProbeReferenceVideoDimensions(ctx, raw, client)
			if probeErr != nil {
				unmeasured = append(unmeasured, truncateForLog(raw))
				continue
			}
			short := width
			if height < short {
				short = height
			}
			if short < bounds.VideoMinShortSide {
				return unmeasured, errors.Errorf(
					"参考视频 #%d 分辨率 %dx%d 的短边 %dpx 低于 %dpx 下限（官网会静默收下但生成失败），请换 ≥%dp 的素材后重试",
					i+1, width, height, short, bounds.VideoMinShortSide, bounds.VideoMinShortSide)
			}
		}
	}

	if bounds.AudioMinSeconds > 0 {
		for i, raw := range audios {
			seconds, probeErr := ProbeReferenceAudioDuration(ctx, raw, client)
			if probeErr != nil {
				unmeasured = append(unmeasured, truncateForLog(raw))
				continue
			}
			if seconds < bounds.AudioMinSeconds-DurationEpsilon {
				return unmeasured, errors.Errorf(
					"参考音频 #%d 时长 %.2f 秒低于 %.0f 秒下限（官网会静默丢弃），请换更长的音频后重试",
					i+1, seconds, bounds.AudioMinSeconds)
			}
		}
	}

	return unmeasured, nil
}

// ProbeReferenceImageDimensions 探测参考图的宽高（只读头部）。
//
// 支持 data URL 与公网 http(s) URL；data URL 直接解码，不经过 SSRF 校验
// （数据本就随请求体驻留内存，与 refvideo.go / refaudio.go 同款处理）。
func ProbeReferenceImageDimensions(ctx context.Context, rawURL string, client *http.Client) (int, int, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return 0, 0, errors.New("reference image url is required")
	}
	var (
		data []byte
		err  error
	)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		data, err = decodeDataURLPrefix(trimmed)
	} else if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		data, err = downloadReferenceImagePrefix(ctx, trimmed, client)
	} else {
		return 0, 0, errors.Errorf("reference image url must be a public http(s) URL or a data URL, got %s", RedactMaterialURL(trimmed))
	}
	if err != nil {
		return 0, 0, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return 0, 0, errors.Errorf("reference image dimensions unavailable: %s", http.DetectContentType(data))
	}
	return config.Width, config.Height, nil
}

// downloadReferenceImagePrefix 下载参考图头部用于尺寸探测，走与素材上传同一套 SSRF 校验。
func downloadReferenceImagePrefix(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, errors.Wrap(err, "reference image URL blocked")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "download reference image failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("download reference image failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, referenceMediaImageProbeBytes))
	if err != nil {
		return nil, errors.Wrap(err, "read reference image failed")
	}
	return data, nil
}

// ProbeReferenceVideoDimensions 探测参考视频的宽高（解析 ISO-BMFF 的 moov/trak/tkhd）。
//
// 复用 downloadReferenceVideoPrefix 拿到的字节：它边拉边找 mvhd，返回时已包含完整 moov，
// 而 tkhd 与 mvhd 同在 moov 内，不需要为尺寸再下载一遍。
func ProbeReferenceVideoDimensions(ctx context.Context, rawURL string, client *http.Client) (int, int, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return 0, 0, errors.New("reference video url is required")
	}
	var (
		data []byte
		err  error
	)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		data, err = decodeDataURLPrefix(trimmed)
	} else if strings.HasPrefix(strings.ToLower(trimmed), "http://") || strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		data, err = downloadReferenceVideoForDimensions(ctx, trimmed, client)
	} else {
		return 0, 0, errors.Errorf("reference video url must be a public http(s) URL or a data URL, got %s", RedactMaterialURL(trimmed))
	}
	if err != nil {
		return 0, 0, err
	}
	width, height, ok := ParseMP4Dimensions(data)
	if !ok {
		return 0, 0, errors.New("reference video dimensions unavailable: no readable tkhd box found (MP4/MOV only)")
	}
	return width, height, nil
}

// downloadReferenceVideoForDimensions 流式下载，直到拿到完整 moov（含 tkhd 宽高）才停。
//
// 为什么不复用 downloadReferenceVideoPrefix：那个函数以「ParseMP4Seconds 命中」
// （即找到 mvhd）为提前返回条件，而 tkhd 在 mvhd **之后**的 trak 里 —— 第一次
// 64KB chunk 若只切到 mvhd 就返回，尺寸解析必然失败、这道闸门会静默失效。
// 这里改用 ParseMP4Dimensions 作提前返回条件，确保 moov 里的视频轨 tkhd 到齐。
// 走与素材上传同一套 SSRF 校验，下载上限与时长探测一致（64MB）。
func downloadReferenceVideoForDimensions(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, errors.Wrap(err, "reference video URL blocked")
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "download reference video failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("download reference video failed: HTTP %d", resp.StatusCode)
	}

	var buf []byte
	chunk := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if _, _, ok := ParseMP4Dimensions(buf); ok {
				return buf, nil
			}
			if len(buf) > probeDownloadCapBytes {
				return nil, errors.New("reference video exceeds 64MB probing limit")
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, errors.Wrap(readErr, "download reference video failed")
		}
	}
	return buf, nil
}

// ParseMP4Dimensions 从 ISO-BMFF 字节流解析视频轨的宽高。
//
// 遍历 moov 下的 trak → tkhd；width/height 是 16.16 定点数（高 16 位为整数像素）。
// 音频轨的 tkhd 宽高为 0，跳过，取第一个宽高均 > 0 的轨（即视频轨）。
// 只依赖 box 结构头，可在不完整缓冲上安全调用（未命中返回 false）。
func ParseMP4Dimensions(data []byte) (int, int, bool) {
	moov, ok := findBox(data, "moov")
	if !ok {
		return 0, 0, false
	}
	for off := 0; off+8 <= len(moov); {
		size := int(binary.BigEndian.Uint32(moov[off : off+4]))
		boxType := string(moov[off+4 : off+8])
		if size == 0 {
			size = len(moov) - off
		}
		if size < 8 || off+size > len(moov) {
			return 0, 0, false
		}
		if boxType == "trak" {
			if tkhd, found := findBox(moov[off+8:off+size], "tkhd"); found {
				if width, height, valid := parseTkhdDimensions(tkhd); valid {
					return width, height, true
				}
				// 音频轨的 tkhd 宽高为 0（parseTkhdDimensions 返回 false），
				// 不能因此中断整个遍历 —— 继续找下一个 trak（视频轨）。
			}
		}
		off += size
	}
	return 0, 0, false
}

// findBox 返回第一个指定类型 box 的**载荷**（去掉 8 字节 size+type）。
func findBox(data []byte, want string) ([]byte, bool) {
	for off := 0; off+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		boxType := string(data[off+4 : off+8])
		if size == 0 {
			size = len(data) - off
		}
		if size < 8 || off+size > len(data) {
			return nil, false
		}
		if boxType == want {
			return data[off+8 : off+size], true
		}
		off += size
	}
	return nil, false
}

// parseTkhdDimensions 解析 tkhd 载荷里的 width/height（16.16 定点）。
// version 0 的宽高偏移为 76/80，version 1 为 88/92（多 8 字节的创建/修改时间）。
func parseTkhdDimensions(payload []byte) (int, int, bool) {
	if len(payload) < 1 {
		return 0, 0, false
	}
	base := 84 // version 0：payload[76:80]=width, payload[80:84]=height
	if payload[0] == 1 {
		base = 96
	}
	if len(payload) < base {
		return 0, 0, false
	}
	width := int(binary.BigEndian.Uint32(payload[base-8:base-4]) >> 16)
	height := int(binary.BigEndian.Uint32(payload[base-4:base]) >> 16)
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	return width, height, true
}
