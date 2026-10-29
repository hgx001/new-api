package youzanwan3

import (
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/pkg/errors"
)

// 参考视频按秒加价：计费秒数 = 生成秒数 + 参考视频总秒数，再乘所选分辨率单价。
// 口径与 wan3.0-video-官网（relay/channel/task/wan3）一致 —— 不是另设一个
// multiplier，而是把参考视频的秒数当成「要付钱的秒数」加到一起。
//
// 为什么必须在 validate 阶段探测：
// EstimateBilling 没有错误返回值，解析失败时无法拒单。拿不到时长就会按纯生成
// 时长收费 —— 不报错、不少一单，只是每条带参考视频的单都少收钱。所以探测放在
// ValidateRequestAndSetAction（预扣之前）里，解析失败直接 400，不建单不扣费。
const refVideoSecondsContextKey = "youzanwan3_ref_video_seconds"

// probeDownloadCapBytes 流式探测的下载上限：参考视频普遍是短片段，moov 在头部时
// 几十 KB 就能命中；只有 moov 在尾部的非 faststart 文件才需要拉到底。
const probeDownloadCapBytes = 64 << 20

// durationEpsilon 是浮点比较容差，避免 mvhd 的 duration/timescale 除法误差。
const durationEpsilon = 1e-6

// probeReferenceVideoSeconds 探测单个参考视频的时长，返回四舍五入后的整秒。
//
// 支持 data URL 与公网 http(s) URL（与 readAsset 的上传路径一致）。
// 解析不出时长时返回错误（fail-loud）：宁可拒单，也不能按 0 秒收钱。
func probeReferenceVideoSeconds(rawURL, proxy string) (int, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, errors.New("reference video url is required")
	}
	lower := strings.ToLower(rawURL)
	var (
		data []byte
		err  error
	)
	switch {
	case strings.HasPrefix(lower, "data:"):
		// 数据本就随请求体驻留内存，全量解码不增加额外开销。
		data, _, err = parseDataURL(rawURL)
		if err != nil {
			return 0, errors.Wrap(err, "decode reference video data URL failed")
		}
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		data, err = downloadReferenceVideoPrefix(rawURL, proxy)
		if err != nil {
			return 0, err
		}
	default:
		return 0, errors.New("youzan wan3 reference video url must be http(s) or data URL")
	}

	seconds, ok := mp4Seconds(data)
	if !ok {
		return 0, errors.New("reference video duration unavailable: no mvhd box found (MP4/MOV only)")
	}
	if seconds <= durationEpsilon {
		return 0, errors.Errorf("reference video duration %.3fs is not billable", seconds)
	}
	// 与上游取整口径对齐：上游生成的成片天然带约 20ms 尾巴，四舍五入而非向上
	// 取整，否则每个参考视频都白多收 1 秒。
	return int(math.Round(seconds)), nil
}

// mp4Seconds 从 ISO-BMFF（MP4/MOV）字节流解析 mvhd 时长（秒）。
// 只依赖 box 结构头，不加载 mdat 内容，可在不完整缓冲上安全调用（未命中返回 false）。
func mp4Seconds(data []byte) (float64, bool) {
	off := 0
	for off+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[off : off+4]))
		boxType := string(data[off+4 : off+8])
		if size == 0 {
			size = len(data) - off // box 延伸到文件末尾
		}
		if size < 8 {
			return 0, false
		}
		if boxType == "moov" {
			if off+size > len(data) {
				return 0, false // moov 未完整到达
			}
			return mvhdSeconds(data[off+8 : off+size])
		}
		off += size
	}
	return 0, false
}

func mvhdSeconds(moovBody []byte) (float64, bool) {
	off := 0
	for off+8 <= len(moovBody) {
		size := int(binary.BigEndian.Uint32(moovBody[off : off+4]))
		boxType := string(moovBody[off+4 : off+8])
		if size == 0 {
			size = len(moovBody) - off
		}
		if size < 8 {
			return 0, false
		}
		if boxType == "mvhd" {
			if off+size > len(moovBody) {
				return 0, false
			}
			return parseMvhd(moovBody[off+8 : off+size])
		}
		off += size
	}
	return 0, false
}

// parseMvhd 解析 mvhd 载荷：version 0 为 4 字节时长，version 1 为 8 字节时长。
func parseMvhd(payload []byte) (float64, bool) {
	if len(payload) < 4 {
		return 0, false
	}
	body := payload[4:]
	var timescale, duration uint64
	switch payload[0] {
	case 1:
		if len(body) < 28 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(body[16:20]))
		duration = binary.BigEndian.Uint64(body[20:28])
	default:
		if len(body) < 16 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(body[8:12]))
		duration = uint64(binary.BigEndian.Uint32(body[12:16]))
	}
	if timescale == 0 {
		return 0, false
	}
	return float64(duration) / float64(timescale), true
}

// downloadReferenceVideoPrefix 流式下载并边拉边找 mvhd：moov 在头部时提前命中即断，
// 在尾部时最多拉 probeDownloadCapBytes。走与素材上传同一套 SSRF 校验。
func downloadReferenceVideoPrefix(rawURL, proxy string) ([]byte, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, errors.Wrap(err, "reference video URL blocked")
	}
	client, err := getHTTPClient(proxy)
	if err != nil {
		return nil, errors.Wrap(err, "new http client failed")
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
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
			if _, ok := mp4Seconds(buf); ok {
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
