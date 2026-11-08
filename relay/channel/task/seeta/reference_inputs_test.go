package seeta

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游 H3 网关 derive_video_kind() 的契约（2026-11-08 真机验证）：
// 请求里只要出现 ref_image_N / ref_video_N / ref_audio_N 之一即为「多参考档」，
// 该档强制至少一张设定参考图；只带参考视频/音频却没有设定参考图时，上游会
// 在提交阶段 400「多参考档需要至少一张参考图」并把内部工作流 ID 写进报错。
// 这类请求必须在**预扣费之前**本地拦掉，不能让客户白等一次上游往返。
//
// 与之相对：纯文生（零素材）与只带 metadata 首尾帧的请求走
// text / first_last 档，上游本来就允许 0 张设定参考图，不能被误伤。

func seetaOmniBody(media string, metadata string) string {
	body := fmt.Sprintf(`{"model":"minimax_h3_omni","prompt":"镜头缓慢推进","duration":5,"media":[%s]`, media)
	if metadata != "" {
		body += `,"metadata":` + metadata
	}
	return body + `}`
}

func seetaMedia(typ, url string) string {
	return fmt.Sprintf(`{"type":%q,"url":%q}`, typ, url)
}

func seetaRequireLocal400(t *testing.T, taskErrCode string, body string) {
	t.Helper()
	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError, "客户入参问题必须是 LocalError，不能触发渠道重试/AutoBan")
	assert.Equal(t, taskErrCode, taskErr.Code)
}

// TestValidateOmniRejectsReferenceVideoWithoutImage 只有参考视频、没有设定参考图
// 必须在**探测下载之前**被拦掉：用不可达地址，若先去探测就会变成下载超时错误。
func TestValidateOmniRejectsReferenceVideoWithoutImage(t *testing.T) {
	body := seetaOmniBody(
		seetaMedia("reference_video", "https://example.invalid/unreachable.mp4"), "")

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)
	assert.Equal(t, "invalid_reference_media", taskErr.Code)
	assert.Contains(t, taskErr.Message, "参考图")
	assert.NotContains(t, taskErr.Message, "download", "不能先去下载素材再报错")
}

// TestValidateOmniRejectsReferenceAudioWithoutImage 只带参考音频同样进多参考档，
// 上游同样要求至少一张设定参考图。
func TestValidateOmniRejectsReferenceAudioWithoutImage(t *testing.T) {
	body := seetaOmniBody(
		seetaMedia("reference_audio", "data:audio/wav;base64,AAAA"), "")
	seetaRequireLocal400(t, "invalid_reference_media", body)
}

// TestValidateOmniAcceptsReferenceVideoWithImage 补一张设定参考图后放行，
// 且参考视频计费探测仍然生效（不能被新校验吞掉）。
func TestValidateOmniAcceptsReferenceVideoWithImage(t *testing.T) {
	body := seetaOmniBody(
		seetaMedia("reference_image", seetaTestDataURI)+","+
			seetaMedia("reference_video", refVideoTestDataURI(3)), "")

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	require.Equal(t, 3, c.MustGet(refVideoSecondsContextKey))
}

// TestValidateOmniRejectsLastFrameWithoutFirstFrame 上游首尾帧锚定要求首帧先给；
// 只有 metadata.last_frame 会在提交阶段 400，本地先拦。
func TestValidateOmniRejectsLastFrameWithoutFirstFrame(t *testing.T) {
	body := seetaOmniBody("", fmt.Sprintf(`{"last_frame":%q}`, seetaTestDataURI))

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)
	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.True(t, taskErr.LocalError)
	assert.Equal(t, "invalid_first_frame", taskErr.Code)
	assert.Contains(t, taskErr.Message, "首帧")
}

// TestValidateOmniAcceptsFirstLastFrames 首尾帧齐全且没有设定参考/视频/音频时
// 走 first_last 档，必须放行（V2 退役后由 omni 承接图生/首尾帧能力）。
func TestValidateOmniAcceptsFirstLastFrames(t *testing.T) {
	body := seetaOmniBody("", fmt.Sprintf(`{"first_frame":%q,"last_frame":%q}`,
		seetaTestDataURI, seetaTestDataURI))

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	_, exists := c.Get(refVideoSecondsContextKey)
	require.False(t, exists)
}

// TestValidateOmniAcceptsPureTextToVideo 纯文生（零素材）走 text 档，不能误伤。
func TestValidateOmniAcceptsPureTextToVideo(t *testing.T) {
	body := `{"model":"minimax_h3_omni","prompt":"纯文生视频","duration":5}`

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
}

// TestValidateOmniAcceptsSetRefWithFirstLast 设定参考与首尾帧可共存（上游
// multi_reference 分支显式支持 AddGuide 锚定），不能被误判成缺图或尾帧错误。
func TestValidateOmniAcceptsSetRefWithFirstLast(t *testing.T) {
	body := seetaOmniBody(
		seetaMedia("reference_image", seetaTestDataURI),
		fmt.Sprintf(`{"first_frame":%q,"last_frame":%q}`, seetaTestDataURI, seetaTestDataURI))

	adaptor := seetaAdaptor(t, "minimax_h3_omni")
	c, info := seetaValidateContext(t, "minimax_h3_omni", body)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
}

// TestValidateV2KeepsOwnRules V2（已退役、仅保留隐藏路由）不套用 Omni 的多参考
// 档校验，仍由 BuildRequestBody 给出它自己的 400。
func TestValidateV2KeepsOwnRules(t *testing.T) {
	adaptor := seetaAdaptor(t, "minimax_h3_v2")
	c, info := seetaValidateContext(t, "minimax_h3_v2",
		`{"model":"minimax_h3_v2","prompt":"p","duration":5,"media":[`+
			seetaMedia("reference_video", "https://example.invalid/unreachable.mp4")+`]}`)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
}
