package taskcommon

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---- 测试数据构造 ----

// buildWav 构造一个 PCM WAV 文件（44 字节标准头 + 数据段）。
//
// fmt 是 1=PCM、3=IEEE float，其余按 PCM 处理；byteRate 是每帧字节数，
// dataBytes 是数据段长度。构造出的时长 = dataBytes / byteRate。
func buildWav(sampleRate uint32, channels uint16, bitsPerSample uint16, dataBytes int) []byte {
	byteRate := sampleRate * uint32(channels) * uint32(bitsPerSample) / 8
	blockAlign := channels * bitsPerSample / 8

	data := make([]byte, 44+dataBytes)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+dataBytes))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16) // fmt chunk 大小
	binary.LittleEndian.PutUint16(data[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(data[22:24], channels)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	binary.LittleEndian.PutUint32(data[28:32], byteRate)
	binary.LittleEndian.PutUint16(data[32:34], blockAlign)
	binary.LittleEndian.PutUint16(data[34:36], bitsPerSample)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(dataBytes))
	return data
}

// buildMp3Frame 构造一个 MPEG-1 Layer III 帧头（不含帧数据）。
//
// MPEG-1 Layer III：采样率索引 0=44100/1=48000/2=32000，帧长 = 144×码率/采样率 + 填充。
// 码率索引 9 = 128kbps，采样率索引 0 = 44100Hz → 每帧 417.9 字节（取整 417）。
func buildMp3Frame() []byte {
	// 同步字 11 bit 全 1 → 0xFF 0xE0；版本 MPEG1(11)、Layer III(01)、保护关闭(1)
	// → 第二字节 0xFB 的高 5 位。
	frame := []byte{0xFF, 0xFB, 0x90, 0x00}
	return frame
}

// ---- WAV 时长解析 ----

func TestParseWavSeconds(t *testing.T) {
	t.Run("标准 PCM WAV", func(t *testing.T) {
		// 44100Hz 单声道 16bit：每秒 88200 字节，10 秒 = 882000 字节
		data := buildWav(44100, 1, 16, 882000)
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		require.InDelta(t, 10.0, seconds, 0.01)
	})

	t.Run("立体声 48kHz", func(t *testing.T) {
		// 48000Hz 双声道 16bit = 192000 字节/秒，5 秒
		data := buildWav(48000, 2, 16, 960000)
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		require.InDelta(t, 5.0, seconds, 0.01)
	})

	t.Run("时长为零的 WAV 也能解析出 0", func(t *testing.T) {
		data := buildWav(44100, 1, 16, 0)
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		require.InDelta(t, 0.0, seconds, 0.01)
	})

	t.Run("非 WAV 返回 false", func(t *testing.T) {
		_, ok := ParseAudioSeconds([]byte("not a wav file at all"))
		require.False(t, ok)
	})

	t.Run("RIFF 头被截断返回 false", func(t *testing.T) {
		_, ok := ParseAudioSeconds([]byte("RIFF"))
		require.False(t, ok)
	})
}

// ---- MP3 时长解析 ----

func TestParseMp3Seconds(t *testing.T) {
	t.Run("按帧累加", func(t *testing.T) {
		// 构造 10 个 MPEG-1 Layer III 帧，每帧 417 字节 @ 128kbps/44100Hz
		// → 实际时长 = 10 × 1152 / 44100 = 0.2612 秒/帧的累加由帧数×采样数决定。
		frame := buildMp3Frame()
		frameLen := 417
		data := make([]byte, 0, frameLen*10)
		for i := 0; i < 10; i++ {
			chunk := make([]byte, frameLen)
			copy(chunk, frame)
			data = append(data, chunk...)
		}
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		// 10 帧 × 1152 采样/帧 ÷ 44100Hz = 0.26122 秒
		require.InDelta(t, 10.0*1152.0/44100.0, seconds, 0.001)
	})

	t.Run("非 MP3 返回 false", func(t *testing.T) {
		_, ok := ParseAudioSeconds([]byte("this is definitely not an mp3 payload"))
		require.False(t, ok)
	})

	t.Run("空数据返回 false", func(t *testing.T) {
		_, ok := ParseAudioSeconds(nil)
		require.False(t, ok)
	})
}

// ---- 格式自动识别 ----

func TestParseAudioSecondsRecognizesFormat(t *testing.T) {
	t.Run("WAV 走 WAV 分支", func(t *testing.T) {
		data := buildWav(44100, 1, 16, 882000)
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		require.InDelta(t, 10.0, seconds, 0.01)
	})

	t.Run("MP3 走 MP3 分支", func(t *testing.T) {
		frame := buildMp3Frame()
		data := make([]byte, 417)
		copy(data, frame)
		seconds, ok := ParseAudioSeconds(data)
		require.True(t, ok)
		require.InDelta(t, 1152.0/44100.0, seconds, 0.001)
	})
}

// ---- 统一上限常量 ----

func TestMaxReferenceAudioSeconds(t *testing.T) {
	// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
	// 单个 [1, 15] 秒，总时长不大于 15 秒。
	require.Equal(t, 15, MaxReferenceAudioSeconds)
}

// ---- 真实素材暴露的坑：ID3 标签与 Xing 头 ----

// buildMp3WithID3AndXing 构造「带 ID3v2 标签 + Xing 头」的 MP3。
//
// 这两个结构都是真实客户素材里存在的（tasks id=1258~1262 的 mp3）：
//   - ID3v2 标签在文件开头（客户素材里还带 AIGC 元数据），
//     不跳过它就一个 MP3 都解析不出来；
//   - Xing/Info 头在第一个音频帧里，里面是编码器写的权威帧数，
//     比逐帧扫描准（扫描会把尾部填充也算成完整帧）。
//
// 参数：帧数 frames、采样率 sampleRate、标签长度 tagBytes。
func buildMp3WithID3AndXing(frames int, sampleRate int, tagBytes int) []byte {
	// ── ID3v2 标签 ──
	// 头 10 字节：'I','D','3',版本(4),flags(0),synchsafe 长度(4)
	tag := make([]byte, 10+tagBytes)
	copy(tag[0:3], "ID3")
	tag[3] = 4 // 版本 2.4
	tag[4] = 0 // flags
	tag[6] = byte((tagBytes >> 21) & 0x7F)
	tag[7] = byte((tagBytes >> 14) & 0x7F)
	tag[8] = byte((tagBytes >> 7) & 0x7F)
	tag[9] = byte(tagBytes & 0x7F)

	// ── 第一个帧：帧头 + Xing 头 ──
	// MPEG-1 Layer III。第 3 字节布局：bit7-4=码率索引、bit3-2=采样率索引、
	// bit1=padding、bit0=私有位。码率索引 9 = 128kbps。
	// ⚠️ 采样率索引：0=44100、1=48000、2=32000 —— 必须落在 bit3-2，
	// 所以 32000 是 0x90 | 0x08 = 0x98，不是 0x90。
	frameHeader := []byte{0xFF, 0xFB, 0x90, 0x00}
	switch sampleRate {
	case 44100:
		frameHeader[2] = 0x90 // 码率 9 | 采样率索引 0
	case 32000:
		frameHeader[2] = 0x98 // 码率 9 | 采样率索引 2
	case 48000:
		frameHeader[2] = 0x94 // 码率 9 | 采样率索引 1
	}

	// Xing 头：'Xing' + flags(0x1=有帧数) + 帧数(4) + 字节数(4)
	xing := make([]byte, 16)
	copy(xing[0:4], "Xing")
	xing[7] = 0x01 // flags bit0
	xing[8] = byte(frames >> 24)
	xing[9] = byte(frames >> 16)
	xing[10] = byte(frames >> 8)
	xing[11] = byte(frames)

	out := append(tag, frameHeader...)
	out = append(out, xing...)
	// 再补几帧填充，让逐帧扫描路径也有内容可扫。
	for i := 0; i < 3; i++ {
		out = append(out, frameHeader...)
		out = append(out, make([]byte, 413)...)
	}
	return out
}

// 带 ID3v2 标签的 MP3 必须能解析出时长。
//
// 回归来源：客户真实素材 e72d1276…mp3 开头是 ID3v2（带 AIGC 元数据），
// 第一版实现没跳标签，直接找 0xFF 同步字 → 一个 MP3 都解析不出来。
func TestParseMp3WithID3Tag(t *testing.T) {
	// 100 帧 @ 44100Hz MPEG-1 → 100×1152/44100 = 2.6122 秒
	data := buildMp3WithID3AndXing(100, 44100, 100)
	seconds, ok := ParseAudioSeconds(data)
	require.True(t, ok, "带 ID3 标签的 MP3 必须能解析")
	require.InDelta(t, 100.0*1152.0/44100.0, seconds, 0.01)
}

// Xing 头的帧数必须被采信（比逐帧扫描准）。
//
// 回归来源：客户真实素材逐帧扫描得 8.208 秒，Xing 头得 8.172 秒，
// ffprobe 得 8.104 秒（它还扣了 encoder delay）。
func TestParseMp3PrefersXingFrameCount(t *testing.T) {
	data := buildMp3WithID3AndXing(227, 32000, 0)
	seconds, ok := ParseAudioSeconds(data)
	require.True(t, ok)
	// 227 帧 @ 32000Hz → 227×1152/32000 = 8.172 秒
	require.InDelta(t, 8.172, seconds, 0.01)
}

// 采样率 32000（MPEG-1 的第三档）必须正确识别 ——
// 客户真实素材 e72d1276…mp3 就是 32000Hz。
func TestParseMp3Handles32kHz(t *testing.T) {
	data := buildMp3WithID3AndXing(50, 32000, 0)
	seconds, ok := ParseAudioSeconds(data)
	require.True(t, ok)
	require.InDelta(t, 50.0*1152.0/32000.0, seconds, 0.01)
}

// ID3 标签长度用 synchsafe 编码，解出来后必须能正确跳过。
func TestID3v2SizeUsesSynchsafe(t *testing.T) {
	data := []byte{'I', 'D', '3', 4, 0, 0,
		0x00, 0x00, 0x01, 0x00, // synchsafe = 128
	}
	offset, ok := id3v2Size(data)
	require.True(t, ok)
	require.Equal(t, 10+128, offset)
}
