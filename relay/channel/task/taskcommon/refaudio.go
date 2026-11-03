package taskcommon

import (
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/pkg/errors"
)

// MaxReferenceAudioSeconds 是所有「支持参考音频」渠道统一的参考音频时长上限（秒）。
//
// 依据：万相 3.0 官方《视频生成API参考》——reference_audio 最多 5 段，
// 单个 [1, 15] 秒，**总时长不大于 15 秒**。
//
// 为什么这个数与视频一致：两者是同一套万相 3.0 后端，参考素材共用一条
// 「总时长 ≤ 15 秒」的硬约束。
//
// 为什么必须在本地拦：上游对音频超限的报错是笼统的「参考素材时长不符合要求，
// 请调整素材或生成时长后重试」，客户看不出该裁哪一条素材。真实失败样本
// （tasks id=1258~1262，2026-11-02）4 条音频累计 19.20 秒被拒，客户连续
// 重试 5 次每次都白占余额、等 1~3 分钟退款。本地 400（TaskErrorWrapperLocal）
// 既不扣费也不触发渠道 AutoBan。
const MaxReferenceAudioSeconds = 15

// 音频探测的下载上限。
//
// 参考音频是短录音（客户实测素材 50KB~640KB），但客户也可能塞长音频进来，
// 所以给一个明确的封顶，避免大文件拖住校验阶段。
const probeAudioDownloadCapBytes = 32 << 20

// 探测预算。与参考视频同一套分级思路：单条有上限、整组有总上限，
// 避免 5 条音频各耗满单条预算把整组拖到超时。
const (
	// probePerReferenceAudioTimeout 是单条参考音频的下载预算。
	probePerReferenceAudioTimeout = 10 * time.Second
	// probeReferenceAudiosTimeout 是整组的上限，防止 5 条各耗 10 秒。
	probeReferenceAudiosTimeout = 30 * time.Second
)

// ParseAudioSeconds 从音频字节流解析时长（秒）。
//
// 自动识别两种格式：
//   - WAV（RIFF 容器）：直接读 fmt chunk 的采样率/字节率与 data chunk 长度，
//     不需要解码任何采样数据。
//   - MP3（MPEG-1/2 Layer III）：逐帧读帧头，按每帧采样数累加。
//
// 为什么必须支持这两个格式：客户实测素材里 mp3 与 wav 混用
// （tasks id=1258 那批是 3 个 mp3 + 1 个 wav），只支持一种会漏掉一半。
//
// 解析不出时长时返回 false，**不放行也不报错** —— 由调用方决定是拒单还是跳过。
func ParseAudioSeconds(data []byte) (float64, bool) {
	if len(data) < 4 {
		return 0, false
	}
	if strings.EqualFold(string(data[0:4]), "RIFF") {
		return parseWavSeconds(data)
	}
	if strings.EqualFold(string(data[0:3]), "ID3") {
		// MP3 常带 ID3v2 标签（客户素材里就有，还带 AIGC 元数据）。
		// 必须跳过标签再找帧同步，否则一个 MP3 都解析不出来。
		if offset, ok := id3v2Size(data); ok {
			data = data[min(offset, len(data)):]
		}
	}
	if len(data) >= 2 && data[0] == 0xFF && data[1]&0xE0 == 0xE0 {
		return parseMp3Seconds(data)
	}
	return 0, false
}

// id3v2Size 解析 ID3v2 标签长度，返回标签**之后**的偏移。
//
// 格式：头部 10 字节，第 6~9 字节是 synchsafe 整数（每字节只用低 7 位），
// 表示标签长度（不含这 10 字节头）。第 5 字节是扩展头长度（也用 synchsafe）。
func id3v2Size(data []byte) (int, bool) {
	if len(data) < 10 || !strings.EqualFold(string(data[0:3]), "ID3") {
		return 0, false
	}
	size := synchsafe(data[6:10])
	footer := 0
	if data[5]&0x10 != 0 { // 带 footer 的 ID3v2.4
		footer = 10
	}
	return 10 + size + footer, true
}

// synchsafe 把 4 字节 synchsafe 整数解出来（每字节只用低 7 位）。
func synchsafe(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return int(b[0]&0x7F)<<21 | int(b[1]&0x7F)<<14 | int(b[2]&0x7F)<<7 | int(b[3]&0x7F)
}

// parseWavSeconds 解析 RIFF/WAVE 时长。
//
// 只读 chunk 头，不碰采样数据：时长 = data chunk 字节数 ÷ byteRate。
func parseWavSeconds(data []byte) (float64, bool) {
	if len(data) < 12 || !strings.EqualFold(string(data[8:12]), "WAVE") {
		return 0, false
	}
	var (
		byteRate    uint32
		dataBytes   uint32
		haveFmt     bool
		haveData    bool
		sampleRate  uint32
		blockAlign  uint16
		channels    uint16
		bitsPerSamp uint16
	)
	off := 12
	for off+8 <= len(data) {
		chunkID := string(data[off : off+4])
		chunkSize := binary.LittleEndian.Uint32(data[off+4 : off+8])
		body := off + 8
		switch chunkID {
		case "fmt ":
			if body+16 > len(data) {
				return 0, false
			}
			channels = binary.LittleEndian.Uint16(data[body+2 : body+4])
			sampleRate = binary.LittleEndian.Uint32(data[body+4 : body+8])
			byteRate = binary.LittleEndian.Uint32(data[body+8 : body+12])
			blockAlign = binary.LittleEndian.Uint16(data[body+12 : body+14])
			bitsPerSamp = binary.LittleEndian.Uint16(data[body+14 : body+16])
			haveFmt = true
		case "data":
			// data chunk 长度可能是 0xFFFFFFFF（流式写入时未知长度），
			// 这时按实际可用字节数算。
			if chunkSize == 0xFFFFFFFF {
				dataBytes = uint32(len(data) - body)
			} else {
				dataBytes = chunkSize
			}
			haveData = true
		}
		off = body + int(chunkSize)
		if chunkSize == 0xFFFFFFFF {
			break
		}
		// chunk 长度是奇数时有 1 字节填充。
		if chunkSize%2 == 1 {
			off++
		}
	}
	if !haveFmt || !haveData {
		return 0, false
	}
	if byteRate == 0 {
		// 有些 WAV 的 byteRate 写 0，用采样率 × 声道 × 字节数兜底算。
		if sampleRate == 0 || channels == 0 || bitsPerSamp == 0 {
			return 0, false
		}
		byteRate = sampleRate * uint32(channels) * uint32(bitsPerSamp) / 8
	}
	if byteRate == 0 {
		return 0, false
	}
	_ = blockAlign
	return float64(dataBytes) / float64(byteRate), true
}

// mp3BitrateTable 是 MPEG-1 Layer III 的码率表（单位 kbps）。
//
// 下标是帧头的 4 bit 码率索引，-1 表示该索引无效。
var mp3BitrateTable = [16]int{
	-1, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, -1,
}

// mp3SampleRateTable 按版本分组：MPEG-1 用前三个，MPEG-2/2.5 用后三个。
var mp3SampleRateTable = map[int][3]int{
	1: {44100, 48000, 32000}, // MPEG-1
	2: {22050, 24000, 16000}, // MPEG-2
	3: {11025, 12000, 8000},  // MPEG-2.5
}

// parseMp3Seconds 解析 MPEG 音频时长。
//
// 优先读 Xing/Info 头里的帧数（最准），没有则逐帧扫描。
//
// 为什么优先读 Xing/Info：逐帧扫描会把**编码器尾部填充**也算成完整帧，
// 实测偏差 3 帧 ≈ 0.108 秒（客户素材 e72d1276…mp3：扫描得 8.208 秒，
// ffprobe 得 8.104 秒）。Xing/Info 头里的帧数字段是编码器自己写的权威值
// （实测与 ffprobe 的 nb_read_frames 一致：227 帧）。
//
// 为什么帧数换算出来是 8.172 而不是 ffprobe 的 8.104：ffprobe 还会扣掉
// MP3 编码器的 encoder delay（通常 576 或 1105 个采样）。这个差值在
// ±0.1 秒量级，对「15 秒上限」这个判定没有影响 —— 不追这个精度。
func parseMp3Seconds(data []byte) (float64, bool) {
	// ── 优先路径：Xing/Info 头 ──
	if frames, rate, ok := parseXingInfo(data); ok && frames > 0 && rate > 0 {
		return float64(frames) * float64(mp3SamplesPerFrame(rate)) / float64(rate), true
	}

	// ── 回退路径：逐帧扫描 ──
	off := 0
	totalSamples := 0
	sampleRate := 0
	frames := 0

	for off+4 <= len(data) {
		if data[off] != 0xFF || data[off+1]&0xE0 != 0xE0 {
			// 不是帧同步：向后找下一个同步字。
			next := findMp3Sync(data, off+1)
			if next < 0 {
				break
			}
			off = next
			continue
		}
		h1 := data[off+1]
		h2 := data[off+2]

		versionBits := (h1 >> 3) & 0x03 // 00=MPEG2.5, 01=reserved, 10=MPEG2, 11=MPEG1
		layerBits := (h1 >> 1) & 0x03   // 01=Layer III, 10=Layer II, 11=Layer I
		if versionBits == 0x01 || layerBits == 0x00 {
			off++ // 保留位或 Layer I：不是 Layer III，跳过
			continue
		}
		bitrateIndex := (h2 >> 4) & 0x0F
		sampleRateIndex := (h2 >> 2) & 0x03
		padding := (h2 >> 1) & 0x01

		if bitrateIndex == 0 || bitrateIndex == 15 || sampleRateIndex == 3 {
			off++
			continue // 非法/自由格式，本解析器不支持
		}
		bitrate := mp3BitrateTable[bitrateIndex]
		if bitrate <= 0 {
			off++
			continue
		}
		version := 3
		switch versionBits {
		case 0x03:
			version = 1
		case 0x02:
			version = 2
		default:
			version = 3 // MPEG-2.5 复用 MPEG-2 的采样率表下标偏移
		}
		rates, ok := mp3SampleRateTable[version]
		if !ok {
			off++
			continue
		}
		rate := rates[sampleRateIndex]
		if rate == 0 {
			off++
			continue
		}
		if sampleRate == 0 {
			sampleRate = rate
		}

		samplesPerFrame := mp3SamplesPerFrame(rate)
		// 标准帧长（字节）= 采样数/8 × 码率(kbps)×1000 ÷ 采样率 + 填充。
		frameLen := samplesPerFrame/8*bitrate*1000/rate + int(padding)
		if frameLen <= 4 {
			off++
			continue
		}
		totalSamples += samplesPerFrame
		frames++
		off += frameLen
	}
	if frames == 0 || sampleRate == 0 {
		return 0, false
	}
	return float64(totalSamples) / float64(sampleRate), true
}

// mp3SamplesPerFrame 按采样率返回 Layer III 的每帧采样数。
//
// MPEG-1 是 1152，MPEG-2/2.5 是 576。
func mp3SamplesPerFrame(sampleRate int) int {
	switch {
	case sampleRate >= 32000:
		return 1152
	case sampleRate > 0:
		return 576
	default:
		return 1152
	}
}

// parseXingInfo 解析 Xing/Info 头，返回帧数与采样率。
//
// 结构（位于第一个有效帧内）：4 字节 "Xing"/"Info" + 4 字节 flags，
// 然后按 flags 的位依次出现：帧数(4)、字节数(4)、TOC(100)、vbr scale(4)。
//
// 只有 flags 的 bit0（帧数字段存在）置位时才返回帧数。
func parseXingInfo(data []byte) (frames int, sampleRate int, ok bool) {
	idx := indexXingInfo(data)
	if idx < 0 {
		return 0, 0, false
	}
	// "Xing"/"Info" 后面 4 字节是 flags。
	if idx+12 > len(data) {
		return 0, 0, false
	}
	flags := binary.BigEndian.Uint32(data[idx+4 : idx+8])
	if flags&0x1 == 0 {
		return 0, 0, false // 没有帧数字段
	}
	frames = int(binary.BigEndian.Uint32(data[idx+8 : idx+12]))
	if frames <= 0 {
		return 0, 0, false
	}
	// 采样率要从**帧头**取（Xing 头本身不带采样率）。
	sampleRate = mp3SampleRateNear(data, idx)
	if sampleRate <= 0 {
		return 0, 0, false
	}
	return frames, sampleRate, true
}

// indexXingInfo 找到 "Xing"/"Info" 标签的位置，找不到返回 -1。
//
// 必须在音频数据范围内找（跳过 ID3 头），否则可能命中元数据里的同名文本。
func indexXingInfo(data []byte) int {
	start := 0
	if len(data) >= 3 && strings.EqualFold(string(data[0:3]), "ID3") {
		if offset, ok := id3v2Size(data); ok {
			start = min(offset, len(data))
		}
	}
	idx := strings.Index(string(data[start:]), "Xing")
	if idx < 0 {
		idx = strings.Index(string(data[start:]), "Info")
	}
	if idx < 0 {
		return -1
	}
	return start + idx
}

// mp3SampleRateNear 从 Xing/Info 标签**之前**最近的帧头取采样率。
func mp3SampleRateNear(data []byte, xingIdx int) int {
	// 从标签往前找帧同步（标签就在第一个帧里，所以帧头在标签之前不远处）。
	start := max(0, xingIdx-64)
	for off := xingIdx - 4; off >= start; off-- {
		if off < 0 || off+4 > len(data) {
			continue
		}
		if data[off] != 0xFF || data[off+1]&0xE0 != 0xE0 {
			continue
		}
		h1, h2 := data[off+1], data[off+2]
		versionBits := (h1 >> 3) & 0x03
		layerBits := (h1 >> 1) & 0x03
		if versionBits == 0x01 || layerBits != 0x01 {
			continue
		}
		sampleRateIndex := (h2 >> 2) & 0x03
		if sampleRateIndex == 3 {
			continue
		}
		version := 3
		if versionBits == 0x03 {
			version = 1
		} else if versionBits == 0x02 {
			version = 2
		}
		rates, ok := mp3SampleRateTable[version]
		if !ok {
			continue
		}
		return rates[sampleRateIndex]
	}
	return 0
}

// findMp3Sync 从 from 开始找下一个帧同步位置，找不到返回 -1。
func findMp3Sync(data []byte, from int) int {
	for i := from; i+1 < len(data); i++ {
		if data[i] == 0xFF && data[i+1]&0xE0 == 0xE0 {
			return i
		}
	}
	return -1
}

// ProbeReferenceAudioDuration 探测单个参考音频的真实时长（秒，未取整）。
//
// 支持 data URL 与公网 http(s) URL。client 由调用方注入（taskcommon 不能 import
// service，会形成循环依赖）。
//
// 解析不出时长时返回错误（fail-loud）：「测不出来」和「没超限」是两件事。
func ProbeReferenceAudioDuration(ctx context.Context, rawURL string, client *http.Client) (float64, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, errors.New("reference audio url is required")
	}
	lower := strings.ToLower(rawURL)
	var (
		data []byte
		err  error
	)
	switch {
	case strings.HasPrefix(lower, "data:"):
		data, err = decodeDataURLPrefix(rawURL)
		if err != nil {
			return 0, errors.Wrap(err, "decode reference audio data URL failed")
		}
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		data, err = downloadReferenceAudioPrefix(ctx, rawURL, client)
		if err != nil {
			return 0, err
		}
	default:
		return 0, errors.Errorf("reference audio url must be a public http(s) URL or a data URL, got %s", RedactMaterialURL(rawURL))
	}

	seconds, ok := ParseAudioSeconds(data)
	if !ok {
		return 0, errors.New("reference audio duration unavailable: unsupported audio format (WAV/MP3 only)")
	}
	return seconds, nil
}

// ReferenceAudioLimitError 标记「参考音频实测超出上游硬限制」。
//
// 为什么要单独一个类型：同样是 400，两类错误的**文案归属**不同。
//   - 超限是客户端素材问题，文案本身是写给客户看的中文提示，必须原样透传；
//   - 探测失败（地址为空、下载不通、不是合法音频）是技术/参数错误，
//     需要保留技术信息便于运维定位。
//
// 调用方用 errors.As 判断，不要靠字符串匹配 —— 文案会随需求改。
type ReferenceAudioLimitError struct{ error }

func (e *ReferenceAudioLimitError) Unwrap() error { return e.error }

// EnforceReferenceAudioMaxSeconds 逐条探测参考音频时长并**累加**，
// 超过 maxSeconds 时返回错误。
//
// 与视频的关键差别：这里**必须累加**。上游对多条参考音频的约束是
// 「总时长不大于 15 秒」（官方《万相3.0-视频生成API参考》），
// 而不是「每条各 15 秒」。真实失败样本 tasks id=1258~1262：
// 4 条音频分别 8.10/3.11/5.00/2.98 秒，单条都不超，累计 19.20 秒被上游拒。
//
// 返回值 unmeasured 列出「没能测出时长」的地址，由调用方记日志；err 只在实测超限时非空。
//
// ⚠️ **探测不到就放行**，理由与视频那条一致：客户的素材地址可能只对上游可达
// （IP 白名单、私有签名），我们抓不到不代表超长；一刀切拒掉会把本来能跑的
// 请求打死。代价是「给一个我们抓不到、上游抓得到的地址」可以绕过限长 ——
// 这个口子刻意留着，绕过的收益只是省一次裁剪。
//
// 例外：**地址本身是空的**（不是「抓不到」，是「根本没给」）——
// 那是客户端把必填素材传成了空串，属于明确的参数错误，必须当场报出来，
// 不能等到上游用一句含糊的「参考素材时长不符合要求」把客户绕晕。
func EnforceReferenceAudioMaxSeconds(ctx context.Context, urls []string, maxSeconds float64, client *http.Client) (unmeasured []string, err error) {
	if len(urls) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, probeReferenceAudiosTimeout)
	defer cancel()

	total := 0.0
	for _, raw := range urls {
		if strings.TrimSpace(raw) == "" {
			return unmeasured, errors.New("参考音频地址为空")
		}
		itemCtx, itemCancel := context.WithTimeout(ctx, probePerReferenceAudioTimeout)
		seconds, probeErr := ProbeReferenceAudioDuration(itemCtx, raw, client)
		itemCancel()
		if probeErr != nil {
			unmeasured = append(unmeasured, truncateForLog(raw))
			continue
		}
		total += seconds
	}
	if total > maxSeconds+DurationEpsilon {
		return unmeasured, &ReferenceAudioLimitError{
			errors.Errorf("参考音频总时长 %.2f 秒超过 %.0f 秒上限，请裁剪后重试", total, maxSeconds),
		}
	}
	return unmeasured, nil
}

// downloadReferenceAudioPrefix 下载参考音频用于时长探测。
//
// 走与素材上传同一套 SSRF 校验；音频普遍很小，一次读完即可
// （不像视频需要流式找 moov）。
func downloadReferenceAudioPrefix(ctx context.Context, rawURL string, client *http.Client) ([]byte, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(rawURL, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, fetchSetting.ApplyIPFilterForDomain); err != nil {
		return nil, errors.Wrap(err, "reference audio URL blocked")
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
		return nil, errors.Wrap(err, "download reference audio failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, errors.Errorf("download reference audio failed: HTTP %d", resp.StatusCode)
	}

	var buf []byte
	chunk := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(chunk)
		if n > 0 {
			buf = append(buf, chunk[:n]...)
			if len(buf) > probeAudioDownloadCapBytes {
				return nil, errors.New("reference audio exceeds 32MB probing limit")
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, errors.Wrap(readErr, "download reference audio failed")
		}
	}
	return buf, nil
}
