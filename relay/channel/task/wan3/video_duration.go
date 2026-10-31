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

// probedVideo 是一次参考视频探测的结果。
type probedVideo struct {
	// seconds 是计费口径：四舍五入后的整秒（与上游 input_video_duration 对齐）。
	seconds int
	// realSeconds 是真实时长，用于官方范围校验（单个 [1,15] 秒、总计 ≤15 秒）。
	realSeconds float64
}

// probeReferenceVideoSeconds 探测单个参考视频的时长。
// 支持 data URL 与公网 http(s) URL；解析不出时长时返回错误（fail-loud，防少收）。
//
// 单个时长必须落在官方区间 [1, 15] 秒 —— 超限或过短都在本地拒：
// 上游会拒，但那时本站已经按超限秒数预扣了钱。
func probeReferenceVideoSeconds(rawURL string) (probedVideo, error) {
	rawURL = strings.TrimSpace(rawURL)
	lower := strings.ToLower(rawURL)
	switch {
	case strings.HasPrefix(lower, "data:"):
		data, err := decodeDataURLPrefix(rawURL)
		if err != nil {
			return probedVideo{}, errors.Wrap(err, "decode reference video data URL failed")
		}
		return probedSeconds(data)
	case strings.HasPrefix(rawURL, "http://") || strings.HasPrefix(rawURL, "https://"):
		data, err := downloadReferenceVideoPrefix(rawURL)
		if err != nil {
			return probedVideo{}, err
		}
		return probedSeconds(data)
	default:
		return probedVideo{}, errors.New("wan3 reference video url must be http(s) or data URL")
	}
}

// durationEpsilon 是浮点比较容差：mvhd 的 duration/timescale 除法可能让
// 恰好 15 秒的片子算出 15.000000000000002，用严格大于会误拒合法请求。
const durationEpsilon = 1e-6

func probedSeconds(data []byte) (probedVideo, error) {
	seconds, ok := mp4Seconds(data)
	if !ok {
		return probedVideo{}, errors.New("reference video duration unavailable: no mvhd box found (MP4/MOV only)")
	}
	// 官方《万相3.0-视频生成API参考》：参考视频单个时长 [1, 15] 秒。
	if seconds < 1-durationEpsilon {
		return probedVideo{}, errors.Errorf("reference video duration %.2fs is below the 1s minimum", seconds)
	}
	if seconds > maxReferenceVideoTotalSeconds+durationEpsilon {
		return probedVideo{}, errors.Errorf("reference video duration %.2fs exceeds the %ds per-video limit", seconds, maxReferenceVideoTotalSeconds)
	}
	// 与上游取整口径对齐：DashScope 自己的 usage 把 2.02 秒的参考视频记为
	// input_video_duration=2（2026-10-16 真机实证），所以这里四舍五入而非向上取整——
	// 上游生成的成片天然带约 20ms 尾巴，向上取整会让每个参考视频都多收 1 秒。
	return probedVideo{seconds: int(math.Round(seconds)), realSeconds: seconds}, nil
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

// MaxReferenceVideoTotalSeconds 是官方允许的**参考视频总时长**上限（秒）。
//
// 官方《万相3.0-视频生成API参考》：reference_video 最多 5 段，
// **单个 [1, 15] 秒，总时长不大于 15 秒**。
const MaxReferenceVideoTotalSeconds = maxReferenceVideoTotalSeconds

// MaxTotalDurationWithVideo 是「输入视频总时长 + 输出视频时长」的上限（秒）。
//
// 官方同一条约束：**有视频输入时，输入视频总时长 + 输出视频时长不超过 30 秒**。
// 本站计费秒数（生成 + 参考视频）与上游 usage.duration 是同一个数。
const MaxTotalDurationWithVideo = maxTotalDurationWithVideo

// ProbeReferenceVideos 探测一组参考视频的总时长，供条件路由中间件在**改派前**预检。
//
// 为什么中间件需要它：wan3 对参考视频有两条硬限制（单条 ≤15 秒、总量 ≤15 秒、
// 输入+输出 ≤30 秒），超限会在提交阶段直接 400。而改派发生在**选渠道之前** ——
// 中间件不知道这些限制，就会把「源链路本来能跑」的请求扔给 wan3，客户白等一次
// 还看不懂失败原因。生产实例（2026-10-31 01:10，logs id=9379）：
//
//	status_code=400, probe reference video duration failed:
//	reference video duration 32.39s exceeds the 15s per-video limit
//
// 同一条请求走 16:9（不改派）本来是可以成功的 —— 这是路由造成的功能退化。
//
// 返回值：totalSeconds 是计费口径（四舍五入整秒）的合计，realSeconds 是真实合计
// （用于 15 秒总量校验）。**任一视频探测失败都返回 error**：调用方必须保守地
// 放弃改派 —— 探测不出时长时无法判断是否会超限，宁可走贵一点但能跑的源链路。
func ProbeReferenceVideos(urls []string) (totalSeconds int, realSeconds float64, err error) {
	for _, raw := range urls {
		probed, probeErr := probeReferenceVideoSeconds(raw)
		if probeErr != nil {
			return 0, 0, probeErr
		}
		totalSeconds += probed.seconds
		realSeconds += probed.realSeconds
	}
	return totalSeconds, realSeconds, nil
}
