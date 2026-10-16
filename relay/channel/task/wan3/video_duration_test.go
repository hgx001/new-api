package wan3

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ---- ISO-BMFF 测试数据构造 ----

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
	// rate(4) volume(2) reserved(2) reserved(8) matrix(36) pre_defined(24) next_track_ID(4)
	tail := make([]byte, 80)
	binary.BigEndian.PutUint32(tail[0:4], 0x00010000)
	tail[4] = 0x01
	binary.BigEndian.PutUint32(tail[16:20], 0x00010000)
	binary.BigEndian.PutUint32(tail[32:36], 0x00010000)
	binary.BigEndian.PutUint32(tail[48:52], 0x40000000)
	binary.BigEndian.PutUint32(tail[76:80], 1)
	return buildBox("mvhd", append(payload, tail...))
}

// buildMP4 构造最小 MP4：ftyp + mdat + moov{mvhd}。
func buildMP4(mvhd []byte) []byte {
	out := buildBox("ftyp", []byte("isom\x00\x00\x02\x00"))
	out = append(out, buildBox("mdat", make([]byte, 16))...)
	out = append(out, buildBox("moov", mvhd)...)
	return out
}

func buildMP4Seconds(seconds float64) []byte {
	return buildMP4(buildMvhd(0, 1000, uint64(seconds*1000)))
}

func buildDataURLVideo(seconds float64) string {
	return "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(buildMP4Seconds(seconds))
}

// ---- mp4Seconds 解析 ----

func TestMP4SecondsParsesMvhdV0(t *testing.T) {
	seconds, ok := mp4Seconds(buildMP4Seconds(7.3))
	require.True(t, ok)
	require.InDelta(t, 7.3, seconds, 0.0001)
}

func TestMP4SecondsParsesMvhdV1(t *testing.T) {
	seconds, ok := mp4Seconds(buildMP4(buildMvhd(1, 1000, 12400)))
	require.True(t, ok)
	require.InDelta(t, 12.4, seconds, 0.0001)
}

func TestMP4SecondsRejectsBrokenData(t *testing.T) {
	// 非 ISO-BMFF 内容
	_, ok := mp4Seconds([]byte("not an mp4 file at all"))
	require.False(t, ok)

	// 只有 ftyp、缺 moov
	_, ok = mp4Seconds(buildBox("ftyp", []byte("isom\x00\x00\x02\x00")))
	require.False(t, ok)

	// mvhd 被截断（ftyp 20 字节 + moov 头 8 字节 + 部分 mvhd）
	truncated := buildMP4Seconds(7.3)[:20+8+40]
	_, ok = mp4Seconds(truncated)
	require.False(t, ok)

	// timescale 为 0（防除零）
	_, ok = mp4Seconds(buildMP4(buildMvhd(0, 0, 7300)))
	require.False(t, ok)
}

// ---- probeReferenceVideoSeconds ----

func disableSSRF(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	orig := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() { *fetch = orig })
}

func TestProbeReferenceVideoSecondsFromDataURL(t *testing.T) {
	seconds, err := probeReferenceVideoSeconds(buildDataURLVideo(7.3))
	require.NoError(t, err)
	require.Equal(t, 8, seconds) // 7.3 → 向上取整

	seconds, err = probeReferenceVideoSeconds(buildDataURLVideo(10))
	require.NoError(t, err)
	require.Equal(t, 10, seconds)
}

func TestProbeReferenceVideoSecondsFromHTTP(t *testing.T) {
	disableSSRF(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(buildMP4Seconds(12.4))
	}))
	defer srv.Close()

	seconds, err := probeReferenceVideoSeconds(srv.URL)
	require.NoError(t, err)
	require.Equal(t, 13, seconds) // 12.4 → 向上取整
}

func TestProbeReferenceVideoSecondsRejectsBadSources(t *testing.T) {
	// 非 http/data 协议
	_, err := probeReferenceVideoSeconds("ftp://example.com/video.mp4")
	require.ErrorContains(t, err, "http")

	// data URL 非法 base64
	_, err = probeReferenceVideoSeconds("data:video/mp4;base64,@@@")
	require.Error(t, err)

	// data URL 非 MP4 内容（无 mvhd）
	_, err = probeReferenceVideoSeconds("data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("plain text")))
	require.ErrorContains(t, err, "mvhd")
}

func TestProbeReferenceVideoSecondsRejectsNonMP4OverHTTP(t *testing.T) {
	disableSSRF(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello world, definitely not a video"))
	}))
	defer srv.Close()

	_, err := probeReferenceVideoSeconds(srv.URL)
	require.ErrorContains(t, err, "mvhd")
}

func TestProbeReferenceVideoSecondsRejectsHTTPErrorStatus(t *testing.T) {
	disableSSRF(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := probeReferenceVideoSeconds(srv.URL)
	require.ErrorContains(t, err, "404")
}

// ---- ValidateRequestAndSetAction 集成 ----

func newTaskContext(t *testing.T, body string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	// Action 字段位于嵌入的 *TaskRelayInfo，须显式初始化避免空指针
	return c, &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}
}

func TestValidateRequestAndSetActionSetsReferenceVideoSeconds(t *testing.T) {
	adaptor := &TaskAdaptor{}
	c, info := newTaskContext(t, fmt.Sprintf(
		`{"model":"wan3.0-video-官网","prompt":"参考动作","media":[{"type":"reference_video","url":%q},{"type":"reference_video","url":%q}]}`,
		buildDataURLVideo(7.3), buildDataURLVideo(4),
	))

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	// 7.3(→8) + 4 = 12
	require.Equal(t, 12, c.MustGet(refVideoSecondsContextKey))
}

func TestValidateRequestAndSetActionRejectsUndeterminableVideo(t *testing.T) {
	adaptor := &TaskAdaptor{}
	garbageURL := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("garbage"))
	c, info := newTaskContext(t, fmt.Sprintf(
		`{"model":"wan3.0-video-官网","prompt":"参考动作","media":[{"type":"reference_video","url":%q}]}`,
		garbageURL,
	))

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_input_reference", taskErr.Code)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
}

func TestValidateRequestAndSetActionSkipsProbeWithoutVideo(t *testing.T) {
	adaptor := &TaskAdaptor{}
	c, info := newTaskContext(t, `{"model":"wan3.0-video-官网","prompt":"纯文字生视频"}`)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr)
	_, exists := c.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}

// ---- EstimateBilling 计费口径 ----

func TestEstimateBillingChargesGenerationPlusReferenceVideo(t *testing.T) {
	// 用户口径：10 秒参考视频 + 生成 10 秒 480P → 按 0.27 × (10 + 10) 计费
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Duration:   10,
		Resolution: "480P",
	})
	context.Set(refVideoSecondsContextKey, 10)

	require.Equal(t, map[string]float64{
		"seconds": 20,
		"size":    1.0,
	}, adaptor.EstimateBilling(context, nil))
}

func TestEstimateBillingIgnoresZeroReferenceVideoSeconds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Duration:   5,
		Resolution: "720P",
	})
	context.Set(refVideoSecondsContextKey, 0)

	require.Equal(t, map[string]float64{
		"seconds": 5,
		"size":    2.0,
	}, adaptor.EstimateBilling(context, nil))
}
