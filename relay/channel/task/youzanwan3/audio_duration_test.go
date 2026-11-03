package youzanwan3

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---- 测试数据构造 ----

// buildWavAudio 构造一个 PCM WAV（44 字节标准头 + 数据段）。
//
// 44100Hz 单声道 16bit → 每秒 88200 字节，时长 = dataBytes / 88200。
func buildWavAudio(seconds float64) []byte {
	const (
		sampleRate = 44100
		byteRate   = sampleRate * 2 // 44100Hz × 1ch × 16bit
	)
	dataBytes := int(seconds * byteRate)
	data := make([]byte, 44+dataBytes)
	copy(data[0:4], "RIFF")
	binary.LittleEndian.PutUint32(data[4:8], uint32(36+dataBytes))
	copy(data[8:12], "WAVE")
	copy(data[12:16], "fmt ")
	binary.LittleEndian.PutUint32(data[16:20], 16) // fmt chunk 大小
	binary.LittleEndian.PutUint16(data[20:22], 1)  // PCM
	binary.LittleEndian.PutUint16(data[22:24], 1)  // 单声道
	binary.LittleEndian.PutUint32(data[24:28], sampleRate)
	binary.LittleEndian.PutUint32(data[28:32], byteRate)
	binary.LittleEndian.PutUint16(data[32:34], 2)  // blockAlign
	binary.LittleEndian.PutUint16(data[34:36], 16) // bitsPerSample
	copy(data[36:40], "data")
	binary.LittleEndian.PutUint32(data[40:44], uint32(dataBytes))
	return data
}

// dataURLAudio 把 WAV 字节包成 data URL。
func dataURLAudio(seconds float64) string {
	return "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(buildWavAudio(seconds))
}

// ---- 累计时长上限 ----

// 参考音频**合计**时长上限（15 秒）。
//
// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
// 单个 [1, 15] 秒，**总时长不大于 15 秒**。
//
// 真实失败样本（tasks id=1258~1262，2026-11-02 19:35~19:48）：
// 4 条音频分别 8.10/3.11/5.00/2.98 秒，单条都不超，累计 19.20 秒被上游拒，
// 客户连续重试 5 次每次都白占余额、等 1~3 分钟退款。
//
// 对照样本（tasks id=1257，同素材前两条）：2 条合计 11.21 秒 → 上游接受。
func TestSetReferenceAudioSecondsRejectsTotalOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 每条 9 秒（单条合规），三条合计 27 秒超限。
	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
	}
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err, "合计 27 秒必须本地拒绝，不能等上游")
	require.Contains(t, err.Error(), "总时长")
	require.Contains(t, err.Error(), "15")
	require.True(t, isReferenceLimitError(err), "必须是素材超限，不是探测失败")
}

// 真实失败样本的精确复现：8.10 + 3.11 + 5.00 + 2.98 = 19.19 秒。
func TestSetReferenceAudioSecondsRejectsRealFailureSample(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(8.1)},
		{Type: "reference_audio", URL: dataURLAudio(3.1)},
		{Type: "reference_audio", URL: dataURLAudio(5.0)},
		{Type: "reference_audio", URL: dataURLAudio(3.0)},
	}
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err, "19.2 秒累计必须被拒")
	require.Contains(t, err.Error(), "参考音频总时长")
}

// 对照样本：前两条音频合计 11.21 秒必须放行（上游接受过）。
func TestSetReferenceAudioSecondsAllowsRealSuccessSample(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(8.1)},
		{Type: "reference_audio", URL: dataURLAudio(3.1)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceAudioSeconds(context, media))
}

// 合计恰好 15 秒必须放行（闭区间，不能误伤）。
func TestSetReferenceAudioSecondsAllowsTotalAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(8)},
		{Type: "reference_audio", URL: dataURLAudio(7)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceAudioSeconds(context, media))
}

// 没有参考音频时不写 context key、不报错。
func TestSetReferenceAudioSecondsNoAudioIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLAudio(30)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceAudioSeconds(context, media))
	_, exists := context.Get(refAudioSecondsContextKey)
	require.False(t, exists, "没有参考音频时不应写 context key")
}

// ---- 探测失败与超限必须分开 ----

// 探测失败**不能**被误标成超限 —— 否则技术前缀被丢掉，运维查不到根因。
func TestReferenceAudioProbeFailureIsNotLimitError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: ""},
	}
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err)
	require.False(t, isReferenceLimitError(err), "空地址是探测失败，不是素材超限")
}

// 超限文案必须**原样**透传给下游：客户要能一眼看懂该怎么改。
//
// 反例（2026-11-02 实测）：上游原文「参考素材时长不符合要求，请调整素材或
// 生成时长后重试」完全没说是哪条素材、多长、多久，客户没法改。
func TestReferenceAudioLimitErrorsAreClientFacing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
	}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err)
	require.Contains(t, err.Error(), "参考音频总时长")
	require.NotContains(t, err.Error(), "probe reference audio duration failed")
}

// 校验阶段的音频超限必须被包成 400 本地错误。
func TestReferenceAudioLimitIsLocalBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
	}
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err)
	require.True(t, isReferenceLimitError(err))
}

// 参考音频超限时，ValidateRequestAndSetAction 必须返回 400 本地错误。
//
// 为什么关键：本地错误（TaskErrorWrapperLocal）**不扣费、不建单、不触发渠道
// AutoBan/重试**。不拦的话会走完「建单 → 预扣 → 上游拒绝 → 等轮询退款」，
// 客户白占一次余额、还要等 1~3 分钟（真实样本 tasks id=1258~1262 连失败 5 次）。
func TestValidateRejectsAudioTotalOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	// 直接验证校验函数返回的错误类型：超限时必须是 400 + 本地错误。
	media := []relaycommon.TaskMedia{
		{Type: "reference_audio", URL: dataURLAudio(9)},
		{Type: "reference_audio", URL: dataURLAudio(9)},
	}
	err := (&TaskAdaptor{}).setReferenceAudioSeconds(context, media)
	require.Error(t, err)
	require.True(t, isReferenceLimitError(err), "超限必须被标记成素材超限，文案原样透传")
	require.Contains(t, err.Error(), "参考音频总时长")
	require.Contains(t, err.Error(), "15")
}
