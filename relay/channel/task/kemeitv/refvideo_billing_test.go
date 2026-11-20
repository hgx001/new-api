package kemeitv

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

func refVideoTestBox(boxType string, payload []byte) []byte {
	out := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(8+len(payload)))
	copy(out[4:8], boxType)
	return append(out, payload...)
}

func refVideoTestMvhd(timescale uint32, duration uint64) []byte {
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

// kemeitvValidateContext 构造一次真实提交所需的 gin.Context / RelayInfo。
// ValidateMultipartDirect 会解析 body 并 StoreTaskRequest（写 info.Action），
// 所以 TaskRelayInfo 必须显式初始化。
func kemeitvValidateContext(t *testing.T, body string) (*gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: ModelNameTJ,
		ChannelMeta:     &relaycommon.ChannelMeta{},
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
	}
	return c, info
}

// kemeitvRefVideoBody 构造一条带参考视频的合法请求。
func kemeitvRefVideoBody(duration int, videos ...string) string {
	items := make([]string, 0, len(videos))
	for _, video := range videos {
		items = append(items, fmt.Sprintf(`{"type":"reference_video","url":%q}`, video))
	}
	return fmt.Sprintf(`{"model":%q,"prompt":"参考动作","duration":%d,"media":[%s]}`,
		ModelNameTJ, duration, strings.Join(items, ","))
}

// ── ValidateRequestAndSetAction：探测并暂存参考视频秒数 ─────────────────────

// 逐条四舍五入后相加：7.3s→7、4s→4，合计 11 秒，存入 context 供 EstimateBilling 使用。
func TestValidateProbesReferenceVideoSeconds(t *testing.T) {
	a := newAdaptor()
	c, info := kemeitvValidateContext(t, kemeitvRefVideoBody(5,
		refVideoTestDataURI(7.3), refVideoTestDataURI(4)))

	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	require.Equal(t, 11, c.MustGet(refVideoSecondsContextKey))
}

// 0.4s→0、2.6s→3，合计 3 秒（与站内 wan3 / 有赞同口径）。
func TestValidateRoundsPerClip(t *testing.T) {
	a := newAdaptor()
	c, info := kemeitvValidateContext(t, kemeitvRefVideoBody(5,
		refVideoTestDataURI(0.4), refVideoTestDataURI(2.6)))

	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	require.Equal(t, 3, c.MustGet(refVideoSecondsContextKey))
}

// 纯文生不探测、不写 key，计费保持原样。
func TestValidateWithoutReferenceVideoLeavesNoKey(t *testing.T) {
	a := newAdaptor()
	c, info := kemeitvValidateContext(t,
		fmt.Sprintf(`{"model":%q,"prompt":"纯文生","duration":5}`, ModelNameTJ))

	require.Nil(t, a.ValidateRequestAndSetAction(c, info))
	_, exists := c.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}

// 计费依赖实测时长：解析不出秒数必须 400 LocalError 拒单。
// 按 0 秒放行 = 每条带参考视频的单都少收钱，而 EstimateBilling 没有错误返回值，
// 等到计费阶段才发现就已经晚了。
func TestValidateRejectsUnmeasurableReferenceVideo(t *testing.T) {
	a := newAdaptor()
	broken := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("not an mp4"))
	c, info := kemeitvValidateContext(t, kemeitvRefVideoBody(5, broken))

	taskErr := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	_, exists := c.Get(refVideoSecondsContextKey)
	assert.False(t, exists)
}

// ── EstimateBilling：把参考视频秒数并入计费秒数 ────────────────────────────

func TestEstimateBillingAddsReferenceVideoSeconds(t *testing.T) {
	a := newAdaptor()

	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 5, Resolution: resolution720P,
	})
	c.Set(refVideoSecondsContextKey, 11)
	ratios := a.EstimateBilling(c, nil)
	assert.Equal(t, float64(16), ratios["seconds"])
	assert.Equal(t, 2.0, ratios["size"])

	// tj 720P 特价倍率同样作用于「生成 + 参考」的总秒数。
	tjInfo := &relaycommon.RelayInfo{OriginModelName: ModelNameTJ}
	c = newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt: "x", Duration: 5, Resolution: resolution720P,
	})
	c.Set(refVideoSecondsContextKey, 3)
	ratios = a.EstimateBilling(c, tjInfo)
	assert.Equal(t, float64(8), ratios["seconds"])
	assert.InDelta(t, 0.25/0.138, ratios["size"], 1e-9)
}

// 零值 / 类型不对时不能影响秒数（防御：context 被别处写坏也要按生成时长计费）。
func TestEstimateBillingIgnoresInvalidReferenceVideoSeconds(t *testing.T) {
	a := newAdaptor()

	for name, value := range map[string]any{
		"零值":  0,
		"负值":  -3,
		"字符串": "11",
	} {
		t.Run(name, func(t *testing.T) {
			c := newTestContext(t, relaycommon.TaskSubmitReq{
				Prompt: "x", Duration: 5, Resolution: resolution480P,
			})
			c.Set(refVideoSecondsContextKey, value)
			ratios := a.EstimateBilling(c, nil)
			assert.Equal(t, float64(5), ratios["seconds"])
		})
	}
}
