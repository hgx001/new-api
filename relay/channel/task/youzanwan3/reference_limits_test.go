package youzanwan3

import (
	"net/http"
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 参考视频**合计**时长上限（15 秒）。
//
// 与 wan3.0-video-官网同源同模型，官方《万相3.0-视频生成API参考》写明
// 「参考视频最多 5 段、总时长不大于 15 秒」。
//
// 只拦单条不够：「5 条各 14 秒」单条都合规，但合计 70 秒必然被上游拒，
// 走完「建单 → 预扣 → 上游拒绝 → 退款」还是白跑。
func TestSetReferenceVideoSecondsRejectsTotalOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	// 每条 9 秒（合规），三条合计 27 秒超限。
	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: dataURLVideo(9)},
		{Type: "reference_video", URL: dataURLVideo(9)},
		{Type: "reference_video", URL: dataURLVideo(9)},
	}
	err := (&TaskAdaptor{}).setReferenceVideoSeconds(context, media)
	require.Error(t, err, "合计 27 秒必须本地拒绝，不能等上游")
	require.Contains(t, err.Error(), "总时长")
	require.Contains(t, err.Error(), "15")

	// 拒绝时不能写 context key —— 否则 EstimateBilling 会把秒数算进去。
	_, exists := context.Get(refVideoSecondsContextKey)
	require.False(t, exists, "拒绝时不应写 context key")
}

// 合计恰好 15 秒必须放行（闭区间，不能误伤）。
func TestSetReferenceVideoSecondsAllowsTotalAtLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: dataURLVideo(8)},
		{Type: "reference_video", URL: dataURLVideo(7)},
	}
	require.NoError(t, (&TaskAdaptor{}).setReferenceVideoSeconds(context, media))
	require.Equal(t, 15, context.MustGet(refVideoSecondsContextKey))
}

// 有参考视频时生成时长上限（15 秒）。
//
// 老板 2026-10-31 定：「有参考视频的时候，生成的视频也要小于 15 秒」。
//
// 真实失败样本：tasks id=967~982，30 秒生成 + 31.609 秒参考，
// 上游 MEDIA_DURATION_INVALID，连续失败 14 次。
func TestValidateRejectsLongGenerationWithReferenceVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: dataURLVideo(10)},
	}
	// 有参考视频 + 生成 30 秒 → 拒绝。
	taskErr := validateGenerationSecondsWithReferenceVideo(context, media, 30)
	require.NotNil(t, taskErr, "有参考视频时 30 秒生成必须本地拒绝")
	require.Contains(t, taskErr.Message, "15")

	// 无参考视频 + 生成 30 秒 → 放行（渠道 17 的 wan3.0-video 支持 30 秒，不能一刀切）。
	taskErr = validateGenerationSecondsWithReferenceVideo(context, []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLVideo(10)},
	}, 30)
	require.Nil(t, taskErr, "无参考视频时 30 秒生成必须放行")
}

// 有参考视频 + 生成 15 秒整必须放行（闭区间）。
func TestValidateAllowsGenerationAtLimitWithReferenceVideo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: dataURLVideo(10)},
	}
	require.Nil(t, validateGenerationSecondsWithReferenceVideo(context, media, 15))
}

// 参考图/参考音频不算「有参考视频」—— 只有 reference_video 才触发生成时长限制。
func TestValidateIgnoresNonVideoReferenceForGenerationLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_image", URL: dataURLVideo(10)},
		{Type: "reference_audio", URL: dataURLVideo(10)},
	}
	require.Nil(t, validateGenerationSecondsWithReferenceVideo(context, media, 30))
}

// validateGenerationSecondsWithReferenceVideo 的错误必须被包成 400 本地错误，
// 避免被当成 5xx 触发渠道重试（重试只会再撞一次同样的错）。
func TestValidateGenerationLimitIsLocalBadRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())

	media := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: dataURLVideo(10)},
	}
	taskErr := validateGenerationSecondsWithReferenceVideo(context, media, 30)
	require.NotNil(t, taskErr)
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode, "必须是 400，否则会被当成 5xx 触发渠道重试")
	require.True(t, taskErr.LocalError, "必须是本地错误，不扣费、不建单、不触发 AutoBan")
	require.Equal(t, "invalid_input_reference", taskErr.Code)
}
