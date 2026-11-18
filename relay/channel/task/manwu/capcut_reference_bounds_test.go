package manwu

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// disableSSRF 关掉 SSRF 保护，让 httptest 的 127.0.0.1 随机端口地址能通过
// 素材探测的 URL 校验（与 taskcommon/refvideo_test.go 同款做法）。
// 不关的话探测全部 fail-open 放行，越界用例测不出拦截行为。
func disableSSRF(t *testing.T) {
	t.Helper()
	fetch := system_setting.GetFetchSetting()
	orig := *fetch
	fetch.EnableSSRFProtection = false
	t.Cleanup(func() { *fetch = orig })
}

// 本地图片服务：CapCut 素材必须是公网 http(s) URL（data URI 会在更早的
// assertHTTPURL 被拒），所以尺寸下限的入口测试只能用 httptest 提供图片。
func imageServer(t *testing.T, w, h int) *httptest.Server {
	t.Helper()
	img := capcutTestJPEG(t, w, h)
	return httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "image/jpeg")
		_, _ = rw.Write(img)
	}))
}

// CapCut 三类素材下限是**三端契约**（new-api / Worker params.ts / ArcReel 说明）。
// 任何一个数字漂移都会让「网关放行、Worker 拒单」或「网关误杀官网能收的素材」。
// 这里锁死数值，改口径必须三仓同批改测试。
func TestCapcutReferenceMediaMinBoundsValues(t *testing.T) {
	assert.Equal(t, 2, capcutMinReferenceAudioSeconds, "参考音频下限 2s（2026-11-17 生产取证）")
	assert.Equal(t, 300, capcutMinReferenceImageShortSide, "参考图短边下限 300px（官网 image_length_out_of_range 下界）")
	assert.Equal(t, 720, capcutMinReferenceVideoShortSide, "参考视频短边下限 720px（2026-11-18 三单哈希对照）")
}

// 越界参考图（短边 200px）必须在 validate 阶段（预扣费之前）400 拒绝。
func TestCapcutRejectsLowResReferenceImageAtValidate(t *testing.T) {
	disableSSRF(t)
	srv := imageServer(t, 200, 400)
	defer srv.Close()
	body := `{"model":"cp-seedance-2.0","prompt":"hi","input_reference":["` + srv.URL + `/low.jpg"]}`
	c, info, a := postVideoCtx(t, body)
	err := a.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, err, "短边 200px 的参考图应在入口 400")
}

// 短边恰好 300px（竖屏 300×720）必须放行 —— 下限是含等号的下界，竖屏不误杀。
func TestCapcutAcceptsBoundaryReferenceImage(t *testing.T) {
	disableSSRF(t)
	srv := imageServer(t, 300, 720)
	defer srv.Close()
	body := `{"model":"cp-seedance-2.0","prompt":"hi","input_reference":["` + srv.URL + `/ok.jpg"]}`
	c, info, a := postVideoCtx(t, body)
	require.Nil(t, a.ValidateRequestAndSetAction(c, info), "短边 300px 恰好等于下限应放行")
}

func capcutTestJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	require.NoError(t, jpeg.Encode(&buf, img, nil))
	return buf.Bytes()
}
