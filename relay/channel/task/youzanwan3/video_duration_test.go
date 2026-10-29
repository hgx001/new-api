package youzanwan3

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// disableSSRF 关掉 SSRF 保护，让 httptest 的随机端口能通过端口白名单。
func disableSSRF(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	orig := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() { *fetch = orig })
}

// ---- ISO-BMFF 测试数据构造（与 wan3 包同一套口径） ----

func buildBox(boxType string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], boxType)
	return append(out, payload...)
}

// buildMvhd 构造结构完整的 mvhd box（v0 共 108 字节、v1 共 120 字节）。
func buildMvhd(version byte, timescale uint32, duration uint64) []byte {
	payload := []byte{version, 0, 0, 0}
	if version == 1 {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], 1)
		payload = append(payload, buf[:]...)
		binary.BigEndian.PutUint64(buf[:], 2)
		payload = append(payload, buf[:]...)
	} else {
		payload = append(payload, 0, 0, 0, 1, 0, 0, 0, 2)
	}
	var ts [4]byte
	binary.BigEndian.PutUint32(ts[:], timescale)
	payload = append(payload, ts[:]...)
	if version == 1 {
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], duration)
		payload = append(payload, buf[:]...)
	} else {
		var d [4]byte
		binary.BigEndian.PutUint32(d[:], uint32(duration))
		payload = append(payload, d[:]...)
	}
	tail := make([]byte, 80)
	binary.BigEndian.PutUint32(tail[0:4], 0x00010000)
	tail[4] = 0x01
	binary.BigEndian.PutUint32(tail[16:20], 0x00010000)
	binary.BigEndian.PutUint32(tail[32:36], 0x00010000)
	binary.BigEndian.PutUint32(tail[48:52], 0x40000000)
	binary.BigEndian.PutUint32(tail[76:80], 1)
	return buildBox("mvhd", append(payload, tail...))
}

// buildMP4 拼出 ftyp + moov(mvhd) 的最小可解析结构。
func buildMP4(mvhd []byte) []byte {
	out := buildBox("ftyp", []byte("isom\x00\x00\x02\x00isomiso2"))
	return append(out, buildBox("moov", mvhd)...)
}

func buildMP4Seconds(seconds float64) []byte {
	return buildMP4(buildMvhd(0, 1000, uint64(seconds*1000)))
}

func dataURLVideo(seconds float64) string {
	return "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(buildMP4Seconds(seconds))
}

// ---- mp4Seconds ----

func TestMP4SecondsParsesMvhdV0(t *testing.T) {
	seconds, ok := mp4Seconds(buildMP4Seconds(7.3))
	require.True(t, ok)
	require.InDelta(t, 7.3, seconds, 1e-6)
}

func TestMP4SecondsParsesMvhdV1(t *testing.T) {
	seconds, ok := mp4Seconds(buildMP4(buildMvhd(1, 1000, 12400)))
	require.True(t, ok)
	require.InDelta(t, 12.4, seconds, 1e-6)
}

func TestMP4SecondsRejectsBrokenData(t *testing.T) {
	_, ok := mp4Seconds([]byte("not an mp4 file at all"))
	require.False(t, ok)

	// 只有 ftyp、没有 moov
	_, ok = mp4Seconds(buildBox("ftyp", []byte("isom\x00\x00\x02\x00")))
	require.False(t, ok)

	// moov 被截断
	full := buildMP4Seconds(5)
	_, ok = mp4Seconds(full[:len(full)-10])
	require.False(t, ok)

	// timescale 为 0
	_, ok = mp4Seconds(buildMP4(buildMvhd(0, 0, 7300)))
	require.False(t, ok)
}

// ---- probeReferenceVideoSeconds ----

func TestProbeReferenceVideoSecondsFromDataURL(t *testing.T) {
	seconds, err := probeReferenceVideoSeconds(dataURLVideo(7.3), "")
	require.NoError(t, err)
	require.Equal(t, 7, seconds)

	seconds, err = probeReferenceVideoSeconds(dataURLVideo(10), "")
	require.NoError(t, err)
	require.Equal(t, 10, seconds)
}

// 上游生成的成片天然带约 20ms 尾巴：2.02 秒必须按 2 秒算，不能向上取整成 3。
func TestProbeReferenceVideoSecondsRoundsDownTinyTail(t *testing.T) {
	seconds, err := probeReferenceVideoSeconds(dataURLVideo(2.02), "")
	require.NoError(t, err)
	require.Equal(t, 2, seconds)
}

func TestProbeReferenceVideoSecondsFromHTTP(t *testing.T) {
	disableSSRF(t)
	body := buildMP4Seconds(6.5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	seconds, err := probeReferenceVideoSeconds(srv.URL, "")
	require.NoError(t, err)
	require.Equal(t, 7, seconds)
}

func TestProbeReferenceVideoSecondsRejectsBadSources(t *testing.T) {
	_, err := probeReferenceVideoSeconds("ftp://example.com/video.mp4", "")
	require.Error(t, err)

	_, err = probeReferenceVideoSeconds("", "")
	require.Error(t, err)

	_, err = probeReferenceVideoSeconds("data:video/mp4;base64,@@@", "")
	require.Error(t, err)

	// 不是 MP4：fail-loud，不能静默按 0 秒计费
	_, err = probeReferenceVideoSeconds("data:video/mp4;base64,"+base64.StdEncoding.EncodeToString([]byte("plain text")), "")
	require.Error(t, err)

	// 时长为 0 的片子不可计费
	_, err = probeReferenceVideoSeconds(dataURLVideo(0), "")
	require.Error(t, err)
}

func TestProbeReferenceVideoSecondsRejectsHTTPErrorStatus(t *testing.T) {
	disableSSRF(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := probeReferenceVideoSeconds(srv.URL, "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 404")
}

// ---- 计费：生成秒数 + 参考视频秒数 ----

func TestEstimateBillingAddsReferenceVideoSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 2, Resolution: "720P"})
	context.Set(refVideoSecondsContextKey, 3)

	info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
	// 2 秒生成 + 3 秒参考视频 = 按 5 秒收，单价仍是所选档位的单价（size 不变）。
	require.Equal(t, map[string]float64{"seconds": 5, "size": 1.0},
		(&TaskAdaptor{}).EstimateBilling(context, info))
}

func TestEstimateBillingIgnoresZeroReferenceVideoSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, value := range []interface{}{0, -1, "3", nil} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 2, Resolution: "1080P"})
		context.Set(refVideoSecondsContextKey, value)

		info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
		require.Equal(t, map[string]float64{"seconds": 2, "size": 1.5},
			(&TaskAdaptor{}).EstimateBilling(context, info), "value=%v", value)
	}
}

func TestEstimateBillingWithoutReferenceVideoUnchanged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 2, Resolution: "720P"})

	info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
	require.Equal(t, map[string]float64{"seconds": 2, "size": 1.0},
		(&TaskAdaptor{}).EstimateBilling(context, info))
}

// ---- validate 阶段写 context ----

func TestSetReferenceVideoSecondsSumsAllVideos(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLVideo(9)}, // 图不参与计秒
		{Type: "reference_video", URL: dataURLVideo(4)},
		{Type: "reference_audio", URL: dataURLVideo(9)}, // 音频不参与计秒
		{Type: "reference_video", URL: dataURLVideo(3.4)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceVideoSeconds(context, media))
	require.Equal(t, 7, context.MustGet(refVideoSecondsContextKey))
}

func TestSetReferenceVideoSecondsSkipsWhenNoVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLVideo(9)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceVideoSeconds(context, media))
	_, exists := context.Get(refVideoSecondsContextKey)
	require.False(t, exists, "无参考视频时不应写 context key")
}

func TestSetReferenceVideoSecondsFailsLoud(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("nope"))},
	}
	err := (&TaskAdaptor{}).setReferenceVideoSeconds(context, media)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "duration unavailable"))
	_, exists := context.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}
