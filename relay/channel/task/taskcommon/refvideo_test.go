package taskcommon

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
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
	tail := make([]byte, 80)
	binary.BigEndian.PutUint32(tail[0:4], 0x00010000)
	tail[4] = 0x01
	binary.BigEndian.PutUint32(tail[16:20], 0x00010000)
	binary.BigEndian.PutUint32(tail[32:36], 0x00010000)
	binary.BigEndian.PutUint32(tail[48:52], 0x40000000)
	binary.BigEndian.PutUint32(tail[76:80], 1)
	return buildBox("mvhd", append(payload, tail...))
}

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

// ---- ParseMP4Seconds ----

func TestParseMP4SecondsReadsMvhdV0AndV1(t *testing.T) {
	seconds, ok := ParseMP4Seconds(buildMP4Seconds(7.3))
	require.True(t, ok)
	require.InDelta(t, 7.3, seconds, 1e-6)

	seconds, ok = ParseMP4Seconds(buildMP4(buildMvhd(1, 1000, 12400)))
	require.True(t, ok)
	require.InDelta(t, 12.4, seconds, 1e-6)
}

func TestParseMP4SecondsRejectsBrokenData(t *testing.T) {
	_, ok := ParseMP4Seconds([]byte("not an mp4 file at all"))
	require.False(t, ok)

	_, ok = ParseMP4Seconds(buildBox("ftyp", []byte("isom\x00\x00\x02\x00")))
	require.False(t, ok, "只有 ftyp、没有 moov")

	full := buildMP4Seconds(5)
	_, ok = ParseMP4Seconds(full[:len(full)-10])
	require.False(t, ok, "moov 被截断")

	_, ok = ParseMP4Seconds(buildMP4(buildMvhd(0, 0, 7300)))
	require.False(t, ok, "timescale 为 0")
}

// ---- ProbeReferenceVideoDuration ----

func TestProbeReferenceVideoDurationFromDataURL(t *testing.T) {
	seconds, err := ProbeReferenceVideoDuration(t.Context(), dataURLVideo(6.5), nil)
	require.NoError(t, err)
	require.InDelta(t, 6.5, seconds, 1e-6)
}

// 无 padding 的 Raw base64 也要认：客户端手拼 data URL 时两种都出现过。
func TestProbeReferenceVideoDurationAcceptsRawBase64(t *testing.T) {
	raw := buildMP4Seconds(4.0)
	encoded := base64.RawStdEncoding.EncodeToString(raw)
	require.NotContains(t, encoded, "=", "构造出的样本必须是无 padding 的，否则这条测试没意义")

	seconds, err := ProbeReferenceVideoDuration(t.Context(), "data:video/mp4;base64,"+encoded, nil)
	require.NoError(t, err)
	require.InDelta(t, 4.0, seconds, 1e-6)
}

func TestProbeReferenceVideoDurationRejectsBadSources(t *testing.T) {
	_, err := ProbeReferenceVideoDuration(t.Context(), "ftp://example.com/v.mp4", nil)
	require.Error(t, err)

	_, err = ProbeReferenceVideoDuration(t.Context(), "", nil)
	require.Error(t, err)

	_, err = ProbeReferenceVideoDuration(t.Context(), "data:video/mp4;base64,@@@", nil)
	require.Error(t, err)

	// 不是 MP4：解析不出时长必须报错，不能静默当成 0 秒
	_, err = ProbeReferenceVideoDuration(t.Context(), "data:video/mp4;base64,"+
		base64.StdEncoding.EncodeToString([]byte("plain text")), nil)
	require.Error(t, err)
}

func TestProbeReferenceVideoDurationFromHTTP(t *testing.T) {
	disableSSRF(t)
	body := buildMP4Seconds(9.2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	seconds, err := ProbeReferenceVideoDuration(t.Context(), srv.URL, srv.Client())
	require.NoError(t, err)
	require.InDelta(t, 9.2, seconds, 1e-6)
}

func TestProbeReferenceVideoDurationRejectsHTTPErrorStatus(t *testing.T) {
	disableSSRF(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := ProbeReferenceVideoDuration(t.Context(), srv.URL, srv.Client())
	require.Error(t, err)
	require.Contains(t, err.Error(), "HTTP 404")
}

// ---- EnforceReferenceVideoMaxSeconds：所有渠道共用的一道闸门 ----

func TestEnforceReferenceVideoMaxSecondsAllowsExactly15s(t *testing.T) {
	unmeasured, err := EnforceReferenceVideoMaxSeconds(t.Context(),
		[]string{dataURLVideo(15)}, MaxReferenceVideoSeconds, nil)
	require.NoError(t, err, "15 秒整是允许的，闭区间不能误伤")
	require.Empty(t, unmeasured)
}

func TestEnforceReferenceVideoMaxSecondsRejectsOver15s(t *testing.T) {
	_, err := EnforceReferenceVideoMaxSeconds(t.Context(),
		[]string{dataURLVideo(15.02)}, MaxReferenceVideoSeconds, nil)
	require.Error(t, err, "15.02 秒必须拒")

	_, err = EnforceReferenceVideoMaxSeconds(t.Context(),
		[]string{dataURLVideo(31.609)}, MaxReferenceVideoSeconds, nil)
	require.Error(t, err, "31.609 秒必须拒（有赞上游 2026-10-31 实测被拒的那条素材）")
	require.Contains(t, err.Error(), "15")
}

// 多条里只要有一条超限就拒：限长是「逐条」口径，不是总量。
func TestEnforceReferenceVideoMaxSecondsChecksEveryVideo(t *testing.T) {
	_, err := EnforceReferenceVideoMaxSeconds(t.Context(),
		[]string{dataURLVideo(5), dataURLVideo(20), dataURLVideo(3)},
		MaxReferenceVideoSeconds, nil)
	require.Error(t, err)
}

// 探测失败必须**放行**并如实报告「没测出来」。
//
// 这是刻意的：素材地址可能只对上游可达（IP 白名单、私有签名），我们抓不到
// 不代表超长；一刀切拒掉会把本来能跑的请求打死，而这道闸门的初衷恰恰是减少失败。
func TestEnforceReferenceVideoMaxSecondsPassesThroughUnmeasurable(t *testing.T) {
	unmeasured, err := EnforceReferenceVideoMaxSeconds(t.Context(),
		[]string{"https://a.example/not-reachable.mp4"}, MaxReferenceVideoSeconds, nil)
	require.NoError(t, err, "探测失败不能阻断请求")
	require.Len(t, unmeasured, 1, "必须如实报告哪几条没测出来")
	require.Contains(t, unmeasured[0], "a.example")
}

// 空列表直接放行，不能因为「没有视频」就去建 client 或起超时。
func TestEnforceReferenceVideoMaxSecondsNoVideos(t *testing.T) {
	unmeasured, err := EnforceReferenceVideoMaxSeconds(t.Context(), nil, MaxReferenceVideoSeconds, nil)
	require.NoError(t, err)
	require.Empty(t, unmeasured)
}

func TestRoundSecondsToBilling(t *testing.T) {
	// 上游生成的成片天然带约 20ms 尾巴：2.02 秒按 2 秒算，向上取整会白多收 1 秒。
	require.Equal(t, 2, RoundSecondsToBilling(2.02))
	require.Equal(t, 7, RoundSecondsToBilling(6.5))
	require.Equal(t, 15, RoundSecondsToBilling(15))
}
