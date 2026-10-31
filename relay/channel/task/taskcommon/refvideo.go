package taskcommon

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/pkg/errors"
)

// MaxReferenceVideoSeconds 是所有「支持参考视频」渠道统一的**单条**参考视频时长上限（秒）。
//
// 依据（两条独立来源，口径一致）：
//   - 万相 3.0 官方《视频生成API参考》：reference_video 单个 [1, 15] 秒；
//   - 有赞链路上游实测原文（2026-10-31）：「参考素材时长过长：当前为 31.609 秒，
//     最长允许 15 秒」。
//
// 为什么要在**所有**渠道统一成同一个数：客户看到的应当是一份「参考视频最长
// 15 秒」的对外口径，不该因为走了哪个渠道、有没有被条件路由改派而变。
//
// 为什么必须在本地拦而不是等上游拒：超长素材提交后要走完「建单 → 预扣费 →
// 上游拒绝 → 等轮询退款」，客户白占一次余额、等几分钟，还看不出该怎么改。
// 本地 400（TaskErrorWrapperLocal）既不扣费也不触发渠道 AutoBan。
const MaxReferenceVideoSeconds = 15

// DurationEpsilon 是浮点比较容差：mvhd 的 duration/timescale 除法可能让
// 恰好 15 秒的片子算出 15.000000000000002，用严格大于会误拒合法请求。
const DurationEpsilon = 1e-6

// probeDownloadCapBytes 流式探测的下载上限：参考视频普遍是短片段，moov 在头部时
// 几十 KB 就能命中；只有 moov 在尾部的非 faststart 文件才需要拉到底。
const probeDownloadCapBytes = 64 << 20

// 探测预算。为什么必须有：service.GetHttpClient 在 RELAY_TIMEOUT=0（生产默认）
// 时**不设 Timeout**，一个「连得上但一直不吐数据」的地址会让 Body.Read 无限阻塞。
//
// 分两级而不是一个总预算：客户可以传最多 5 段参考视频，共用一个总超时会让
// 第 5 段几乎没时间可用（前面几段吃光了预算），把合规请求误判成探测失败。
const (
	// probePerReferenceVideoTimeout 是单条参考视频的下载预算。
	probePerReferenceVideoTimeout = 10 * time.Second
	// probeReferenceVideosTimeout 是整组的上限，防止 5 条各耗 10 秒。
	probeReferenceVideosTimeout = 30 * time.Second
)

// ParseMP4Seconds 从 ISO-BMFF（MP4/MOV）字节流解析 mvhd 时长（秒）。
//
// 只依赖 box 结构头，不加载 mdat 内容，可在不完整缓冲上安全调用（未命中返回 false）。
// 这是「参考视频到底多长」的唯一解析口径：wan3 / youzanwan3 / 其余渠道全部走这里，
// 避免每个渠道各写一份、改一处漏三处。
func ParseMP4Seconds(data []byte) (float64, bool) {
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

// ProbeReferenceVideoDuration 探测单个参考视频的真实时长（秒，未取整）。
//
// 支持 data URL 与公网 http(s) URL。client 由调用方注入：taskcommon 不能 import
// service（service 已经 import taskcommon，会形成循环），所以这里只接受一个
// 现成的 *http.Client；传 nil 时回落到 http.DefaultClient（仅适合测试）。
//
// 解析不出时长时返回错误（fail-loud）——「测不出来」和「没超限」是两件事，
// 悄悄放行等于这道闸门形同虚设。
func ProbeReferenceVideoDuration(ctx context.Context, rawURL string, client *http.Client) (float64, error) {
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
		data, err = decodeDataURLPrefix(rawURL)
		if err != nil {
			return 0, errors.Wrap(err, "decode reference video data URL failed")
		}
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		data, err = downloadReferenceVideoPrefix(ctx, rawURL, client)
		if err != nil {
			return 0, err
		}
	default:
		return 0, errors.Errorf("reference video url must be a public http(s) URL or a data URL, got %q", truncateForLog(rawURL))
	}

	seconds, ok := ParseMP4Seconds(data)
	if !ok {
		return 0, errors.New("reference video duration unavailable: no mvhd box found (MP4/MOV only)")
	}
	return seconds, nil
}

// EnforceReferenceVideoMaxSeconds 逐条探测参考视频时长，**实测**超过 maxSeconds 时返回错误。
//
// 返回值 unmeasured 列出「没能测出时长」的地址，由调用方记日志；err 只在实测超限时非空。
//
// 这是「所有支持参考视频的渠道」共用的那道闸门：任一视频实测超限即拒绝，
// 调用方转成 TaskErrorWrapperLocal（400，不扣费、不 AutoBan）。
//
// ⚠️ 探测失败**放行**，这是刻意的：
//   - 客户的素材地址可能只对上游可达（IP 白名单、私有签名），我们抓不到不代表超长；
//   - 一刀切拒掉会把本来能跑的请求打死，而这道闸门的初衷恰恰是**减少**失败；
//   - 与条件路由的既有口径一致（`wan3.ProbeReferenceVideos` 探测失败时保守地
//     放弃改派，而不是拒单）。
//
// 代价是「给一个我们抓不到、上游抓得到的地址」可以绕过限长。这个口子刻意留着：
// 它要求客户先能控制一个对上游可达的存储，且绕过的收益只是「省一次裁剪」，
// 而误伤合法请求的代价是真实业务中断。wan3 / 有赞这两条**计费依赖探测结果**的
// 链路不适用本函数，它们有自己的 fail-loud 探测（拿不到时长就无法算钱）。
func EnforceReferenceVideoMaxSeconds(ctx context.Context, urls []string, maxSeconds float64, client *http.Client) (unmeasured []string, err error) {
	if len(urls) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeReferenceVideosTimeout)
	defer cancel()

	for _, raw := range urls {
		itemCtx, itemCancel := context.WithTimeout(ctx, probePerReferenceVideoTimeout)
		seconds, probeErr := ProbeReferenceVideoDuration(itemCtx, raw, client)
		itemCancel()
		if probeErr != nil {
			unmeasured = append(unmeasured, truncateForLog(raw))
			continue
		}
		if seconds > maxSeconds+DurationEpsilon {
			return unmeasured, errors.Errorf("参考视频时长 %.2f 秒超过 %.0f 秒上限，请裁剪后重试", seconds, maxSeconds)
		}
	}
	return unmeasured, nil
}

// decodeDataURLPrefix 解码 data URL 的 base64 部分。
//
// 同时接受带 padding 的标准编码与无 padding 的 Raw 变体：客户端手工拼 data URL 时
// 两种都出现过，只认一种会把合规请求判成「解析失败」。
func decodeDataURLPrefix(rawURL string) ([]byte, error) {
	comma := strings.Index(rawURL, ",")
	if comma < 0 {
		return nil, errors.New("malformed data URL")
	}
	payload := rawURL[comma+1:]
	if data, err := base64.StdEncoding.DecodeString(payload); err == nil {
		return data, nil
	}
	return base64.RawStdEncoding.DecodeString(payload)
}

// downloadReferenceVideoPrefix 流式下载并边拉边找 mvhd：moov 在头部时提前命中即断，
// 在尾部时最多拉 probeDownloadCapBytes。走与素材上传同一套 SSRF 校验。
func downloadReferenceVideoPrefix(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
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
			if _, ok := ParseMP4Seconds(buf); ok {
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

// RoundSecondsToBilling 把探测出的真实秒数换成计费口径的整秒。
//
// 与上游取整口径对齐：上游生成的成片天然带约 20ms 尾巴，四舍五入而非向上取整，
// 否则每个参考视频都白多收 1 秒。
func RoundSecondsToBilling(seconds float64) int {
	return int(math.Round(seconds))
}

// truncateForLog 截断用户输入的 URL，避免把超长 data URL 整个写进错误信息。
func truncateForLog(raw string) string {
	const limit = 120
	if len(raw) <= limit {
		return raw
	}
	return raw[:limit] + "..."
}
