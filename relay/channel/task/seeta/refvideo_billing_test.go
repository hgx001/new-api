package seeta

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── 最小 MP4 fixture ────────────────────────────────────────────────────────
//
// 计费依赖 taskcommon.ParseMP4Seconds 读 moov/mvhd 的 duration/timescale，
// 所以测试素材必须是结构完整的 ISO-BMFF，不能拿一串假字节糊弄。
// 与 wan3 包用的是同一套解析口径，这里独立构造以免跨测试包耦合。

func refVideoTestBox(boxType string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], boxType)
	return append(out, payload...)
}

func refVideoTestMvhd(timescale uint32, duration uint64) []byte {
	// version(1)+flags(3) + creation(4)+modification(4) + timescale(4)+duration(4)
	payload := []byte{0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 2}
	var ts, d [4]byte
	binary.BigEndian.PutUint32(ts[:], timescale)
	binary.BigEndian.PutUint32(d[:], uint32(duration))
	payload = append(payload, ts[:]...)
	payload = append(payload, d[:]...)
	return refVideoTestBox("mvhd", append(payload, make([]byte, 80)...))
}

func refVideoTestDataURI(seconds float64) string {
	out := refVideoTestBox("ftyp", []byte("isom\x00\x00\x02\x00"))
	out = append(out, refVideoTestBox("mdat", make([]byte, 16))...)
	out = append(out, refVideoTestBox("moov", refVideoTestMvhd(1000, uint64(seconds*1000)))...)
	return "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(out)
}

// seetaValidateContext 构造一次真实提交所需的 gin.Context / RelayInfo。
// StoreTaskRequest 会写 info.Action，TaskRelayInfo 必须显式初始化。
func seetaValidateContext(t *testing.T, model, body string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: model,
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	return c, info
}

// seetaRefVideoBody 构造一条**合法的** Omni 多参考请求。
//
// 2026-11-08 真机验证：上游多参考档强制至少一张设定参考图，只带参考视频
// 会在提交阶段 400。所以计费探测的 fixture 必须自带一张 reference_image，
// 否则测的是一个上游根本不会受理的请求。
func seetaRefVideoBody(t *testing.T, model string, duration int, videos ...string) string {
	t.Helper()
	items := []string{fmt.Sprintf(`{"type":"reference_image","url":%q}`, seetaTestDataURI)}
	for _, video := range videos {
		items = append(items, fmt.Sprintf(`{"type":"reference_video","url":%q}`, video))
	}
	return fmt.Sprintf(`{"model":%q,"prompt":"参考动作","duration":%d,"media":[%s]}`,
		model, duration, strings.Join(items, ","))
}

// ── ValidateRequestAndSetAction：探测并暂存参考视频秒数 ─────────────────────

// TestValidateOmniProbesReferenceVideoSeconds 逐条四舍五入后相加：
// 7.3s→7、4s→4，合计 11 秒。这个数必须存进 context，供 EstimateBilling 用。
func TestValidateOmniProbesReferenceVideoSeconds(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		seetaRefVideoBody(t, "minimax_h3_omni", 5,
			refVideoTestDataURI(7.3), refVideoTestDataURI(4)))

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Equal(t, 11, c.MustGet(refVideoSecondsContextKey))
}

// TestValidateOmniRoundsPerClip 单条四舍五入后再相加（与 wan3 口径一致）：
// 0.4s→0、2.6s→3，合计 3 秒。
func TestValidateOmniRoundsPerClip(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		seetaRefVideoBody(t, "minimax_h3_omni", 5,
			refVideoTestDataURI(0.4), refVideoTestDataURI(2.6)))

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Equal(t, 3, c.MustGet(refVideoSecondsContextKey))
}

// TestValidateOmniWithoutReferenceVideoLeavesNoKey 纯文生不探测、不写 key。
func TestValidateOmniWithoutReferenceVideoLeavesNoKey(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		`{"model":"minimax_h3_omni","prompt":"纯文生","duration":5}`)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	_, exists := c.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}

// TestValidateOmniRejectsUnmeasurableReferenceVideo 计费依赖实测时长：
// 解析不出秒数必须 400 LocalError 拒单，不能按 0 秒放行（会少收），
// 也不能因为这个客户素材问题触发渠道重试/AutoBan。
func TestValidateOmniRejectsUnmeasurableReferenceVideo(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	garbage := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("not an mp4"))
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		seetaRefVideoBody(t, "minimax_h3_omni", 5, garbage))

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError, "客户素材问题必须是 LocalError，不能触发 AutoBan")
	require.Equal(t, "invalid_reference_video", taskErr.Code)
}

// TestValidateV2SkipsReferenceVideoProbe V2（2026-11-08 退役、仅保留隐藏路由）
// 没有视频槽位：不探测、不计费，由 BuildRequestBody 给出明确的 400。
// 这里验证 validate 阶段不会为一个注定被拒的请求白下载一次素材。
func TestValidateV2SkipsReferenceVideoProbe(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_v2")
	c, info := seetaValidateContext(t, "minimax_h3_v2",
		seetaRefVideoBody(t, "minimax_h3_v2", 5,
			"https://example.invalid/unreachable.mp4"))

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	_, exists := c.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}

// ── EstimateBilling：生成秒数 + 参考视频秒数 ────────────────────────────────

// TestEstimateBillingAddsReferenceVideoSeconds 768p 生成 5 秒 + 11 秒参考视频
// = 16 秒计费，分辨率倍率仍是 2.0（参考视频与生成同价）。
func TestEstimateBillingAddsReferenceVideoSeconds(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c := seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Duration: 5, Resolution: "768p横",
	})
	c.Set(refVideoSecondsContextKey, 11)

	billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))
	require.Equal(t, 16.0, billing["seconds"])
	require.InDelta(t, 2.0, billing["size"], 0.001)
	// 对客 ¥0.04/秒 × 16 秒 = ¥0.64
	require.InDelta(t, 0.64, 0.02*billing["size"]*billing["seconds"], 0.0001)
}

// TestEstimateBillingReferenceVideoUsesResolutionRatio 参考视频秒数同样乘
// 分辨率倍率：1080p 生成 10 秒 + 5 秒参考视频 → 15 × 3.0 × ¥0.02 = ¥0.90。
func TestEstimateBillingReferenceVideoUsesResolutionRatio(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c := seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Duration: 10, Resolution: "1080p竖",
	})
	c.Set(refVideoSecondsContextKey, 5)

	billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))
	require.Equal(t, 15.0, billing["seconds"])
	require.InDelta(t, 3.0, billing["size"], 0.001)
	require.InDelta(t, 0.90, 0.02*billing["size"]*billing["seconds"], 0.0001)
}

// TestEstimateBillingWithoutProbeKey 没有参考视频时保持原口径（只按生成秒数）。
func TestEstimateBillingWithoutProbeKey(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c := seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Duration: 5, Resolution: "480p横",
	})

	billing := adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))
	require.Equal(t, 5.0, billing["seconds"])
	require.InDelta(t, 1.0, billing["size"], 0.001)
}

// TestEstimateBillingIgnoresNonPositiveReferenceSeconds 0 或脏类型都不能影响计费。
func TestEstimateBillingIgnoresNonPositiveReferenceSeconds(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c := seetaTaskContext(relaycommon.TaskSubmitReq{
		Model: "minimax_h3_omni", Prompt: "p", Duration: 5,
	})
	c.Set(refVideoSecondsContextKey, 0)
	require.Equal(t, 5.0, adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))["seconds"])

	c.Set(refVideoSecondsContextKey, "11")
	require.Equal(t, 5.0, adaptor.EstimateBilling(c, seetaRelayInfo("minimax_h3_omni"))["seconds"])
}

// TestProbeReferenceVideosSecondsRedactsMaterialURL 探测失败的错误不能把整段
// base64 素材（或带签名的素材 URL）原样回显 —— 日志里等于落了一份客户素材。
func TestProbeReferenceVideosSecondsRedactsMaterialURL(t *testing.T) {
	payload := strings.Repeat("A", 4096)
	raw := "data:video/mp4;base64," + payload

	_, err := probeReferenceVideosSeconds(nil, []string{raw})
	require.Error(t, err)
	require.NotContains(t, err.Error(), payload)
	require.Contains(t, err.Error(), "base64,<")

	// 同一错误经 validate 转成对客 400 后也必须保持脱敏。
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		seetaRefVideoBody(t, "minimax_h3_omni", 5, raw))
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.NotContains(t, taskErr.Message, payload)
	require.Contains(t, taskErr.Message, "base64,<")
}

// TestValidateToEstimateBillingEndToEnd 走完整链路（validate → EstimateBilling
// 用同一个 context），确认探测值真的进了计费，而不是只写了个没用的 key。
func TestValidateToEstimateBillingEndToEnd(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni",
		seetaRefVideoBody(t, "minimax_h3_omni", 5,
			refVideoTestDataURI(6), refVideoTestDataURI(3)))

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	billing := adaptor.EstimateBilling(c, info)
	// 5 秒生成 + 9 秒参考视频 = 14 秒；480p 基准 ¥0.02 → ¥0.28
	assert.Equal(t, 14.0, billing["seconds"])
	assert.InDelta(t, 1.0, billing["size"], 0.001)
	assert.InDelta(t, 0.28, 0.02*billing["size"]*billing["seconds"], 0.0001)
}
