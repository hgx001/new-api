package wan3

import (
	"encoding/base64"
	"encoding/binary"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// buildWavAudio 构造一个 PCM WAV（44 字节标准头 + 数据段）。
//
// 44100Hz 单声道 16bit → 每秒 88200 字节，时长 = dataBytes / 88200。
func buildWavAudio(seconds float64) []byte {
	const (
		sampleRate = 44100
		byteRate   = sampleRate * 2
	)
	dataBytes := int(seconds * byteRate)
	data := make([]byte, 44+dataBytes)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+dataBytes))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16)
	binary.LittleEndian.PutUint16(data[20:22], 1)
	binary.LittleEndian.PutUint16(data[22:24], 1)
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	binary.LittleEndian.PutUint32(data[28:32], byteRate)
	binary.LittleEndian.PutUint16(data[32:34], 2)
	binary.LittleEndian.PutUint16(data[34:36], 16)
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(dataBytes))
	return data
}

// dataURLAudio 把 WAV 字节包成 data URL。
func dataURLAudio(seconds float64) string {
	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(buildWavAudio(seconds))
}

// 参考音频**合计**时长上限（15 秒）。
//
// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
// 单个 [1, 15] 秒，**总时长不大于 15 秒**。
//
// ⚠️ 这条限制在常量里早就写好了（maxReferenceAudioTotalSeconds），
// 但一直没有代码去强制它 —— 常量文件里注释写着「本站不探测音频时长」。
// 现在 taskcommon 有了音频时长解析，把它接上。
//
// 真实失败样本（tasks id=1258~1262，2026-11-02，渠道 17 有赞 Wan3）：
// 4 条音频累计 19.20 秒被上游拒，客户连续重试 5 次每次白占余额。
func TestValidateReferenceAudioTotalOverLimit(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
	}
	err := validateReferenceAudioDurations(media)
	require.Error(t, err, "合计 18 秒必须本地拒绝，不能等上游")
	require.Contains(t, err.Error(), "参考音频总时长")
	require.Contains(t, err.Error(), "15")
}

// 合计恰好 15 秒必须放行（闭区间，不能误伤）。
func TestValidateReferenceAudioTotalAtLimit(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(8)},
		{Type: "reference_audio", URL: dataURLAudio(7)},
	}
	require.NoError(t, validateReferenceAudioDurations(media))
}

// 没有参考音频时是空操作。
func TestValidateReferenceAudioNoAudioIsNoop(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLAudio(30)},
	}
	require.NoError(t, validateReferenceAudioDurations(media))
}

// 探测失败**放行**，不能当成超限 ——
// 客户的素材地址可能只对上游可达（IP 白名单、私有签名），我们抓不到不代表它超长。
//
// 这与参考视频那条相反：视频的探测结果参与计费，测不出来就无法算钱，只能 fail-loud；
// 音频不参与计费，抓不到就先放过。
func TestValidateReferenceAudioProbeFailureIsAllowedThrough(t *testing.T) {
	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: "data:audio/wav;base64,bm90YXR2YWxpZA=="},
	}
	require.NoError(t, validateReferenceAudioDurations(media),
		"抓不到的素材必须放行，不能一刀切拒单")
}
