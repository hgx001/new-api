package wan3

import (
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/pkg/errors"
)

// refVideoSecondsContextKey 保存 validate 阶段探测出的参考视频总秒数（向上取整），
// EstimateBilling 读取它把秒数并入计费（生成秒数 + 参考视频总秒数）。
const refVideoSecondsContextKey = "wan3_ref_video_seconds"

// probeDownloadCapBytes 流式探测的下载上限：参考视频普遍为短片段，
// moov 在头部时几十 KB 即可命中；仅 moov 在尾部的非 faststart 文件才需要拉到底。
const probeDownloadCapBytes = 64 << 20

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

// probeReferenceVideoSeconds 探测单个参考视频的时长并取整为整秒（四舍五入，与上游
// input_video_duration 口径一致，见 probedSeconds）。
// 支持 data URL 与公网 http(s) URL；解析不出时长时返回错误（fail-loud，防少收）。
func probeReferenceVideoSeconds(rawURL string) (int, error) {
	rawURL = strings.TrimSpace(rawURL)
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lower, "data:"):
		data, err := decodeDataURLPrefix(rawURL)
		if err != nil {
			return 0, errors.Wrap(err, "decode reference video data URL failed")
		}
		return probedSeconds(data)
	case strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://"):
		data, err := downloadReferenceVideoPrefix(rawURL)
		if err != nil {
			return 0, err
		}
		return probedSeconds(data)
	default:
		return 0, errors.New("wan3 reference video url must be http(s) or data URL")
	}
}

func probedSeconds(data []byte) (int, error) {
	seconds, ok := mp4Seconds(data)
	if !ok {
		return 0, errors.New("reference video duration unavailable: no mvhd box found (MP4/MOV only)")
	}
	// 与上游取整口径对齐：DashScope 自己的 usage 把 2.02 秒的参考视频记为
	// input_video_duration=2（2026-10-16 真机实证），所以这里四舍五入而非向上取整——
	// 上游生成的成片天然带约 20ms 尾巴，向上取整会让每个参考视频都多收 1 秒。
	return int(math.Round(seconds)), nil
}

// decodeDataURLPrefix 解码 data URL 的 base64 部分。数据本就随请求体驻留内存，全量解码不增加额外开销。
func decodeDataURLPrefix(rawURL string) ([]byte, error) {
	comma := strings.Index(rawURL, ",")
	if comma < 0 {
		return nil, errors.New("malformed data URL")
	}
	return base64.StdEncoding.DecodeString(rawURL[comma+1:])
}

// downloadReferenceVideoPrefix 流式下载并边拉边找 mvhd：moov 在头部时提前命中即断，
// 在尾部时最多拉 probeDownloadCapBytes。
func downloadReferenceVideoPrefix(rawURL string) ([]byte, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, errors.Wrap(err, "reference video URL blocked")
	}
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return nil, errors.Wrap(err, "new http client failed")
	}
	if client == nil {
		client = http.DefaultClient
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
