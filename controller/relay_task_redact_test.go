package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// 别名模型提交失败时，上游原文会带着真实模型名（例如 "model aliyun:wan-3.0 ..."）。
// 直接回给客户端就等于告诉用户真实上游是谁，输出前必须脱敏。
func TestRespondTaskErrorRedactsAliasModelUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "seedance2.5-特惠")

	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    `{"error":{"message":"model aliyun:wan-3.0 does not support the requested output combination"}}`,
		StatusCode: 400,
	}
	respondTaskError(ctx, taskErr)

	assert.NotContains(t, taskErr.Message, "aliyun")
	assert.NotContains(t, taskErr.Message, "wan-3.0")
	assert.Contains(t, taskErr.Message, "上游")
	assert.NotContains(t, w.Body.String(), "aliyun")
}

// 别名模型提交失败时，错误正文里可能夹带上游地址（例如渠道域名）。
// 上游地址本身就是"真实上游是谁"的证据，一并摘掉 —— 只遮字样会留下
// "https://newapi.上游.cc/v1/videos" 这种半遮半露的地址。
func TestRespondTaskErrorDropsAliasModelUpstreamAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "seedance2.5-特惠")

	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    `Post "https://newapi.megabyai.cc/v1/videos": 401 invalid token`,
		StatusCode: 400,
	}
	respondTaskError(ctx, taskErr)

	assert.NotContains(t, taskErr.Message, "megabyai", "上游站点域名不能给下游")
	assert.NotContains(t, taskErr.Message, "https://", "上游地址整体摘掉")
	assert.Contains(t, taskErr.Message, "invalid token", "错误原因本身要保留")
}

// 非别名模型：供应商词照样换成「上游」（有渠道的直接说上游），
// 但上游模型名保留 —— 那是客户自己买的模型族，替换了报错就看不明白了。
func TestRespondTaskErrorKeepsNonAliasMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "wan-3.0")

	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    `{"error":{"message":"model aliyun:wan-3.0 does not support"}}`,
		StatusCode: 400,
	}
	respondTaskError(ctx, taskErr)

	assert.NotContains(t, taskErr.Message, "aliyun", "供应商名不能给客户")
	assert.Contains(t, taskErr.Message, "上游")
	assert.Contains(t, taskErr.Message, "wan-3.0", "非别名模型：模型名保留")
	assert.Contains(t, taskErr.Message, "does not support", "错误原因本身要保留")
}

// 2026-11-08 真机验证：omni 多参考档缺设定参考图时，上游 400 的正文里
// 带着内部工作流 ID「纯文生视频请用 minimax_h3_lightx2v_no_pic」。
// 这不是客户买的模型、也不该出现在任何对客响应里；统一出口必须遮掉。
func TestRespondTaskErrorHidesSeetaInternalWorkflowID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "minimax_h3_omni")

	taskErr := &dto.TaskError{
		Code: "fail_to_fetch_task",
		Message: `{"error":{"message":"多参考档需要至少一张参考图（images 或 ref_image_0）；` +
			`纯文生视频请用 minimax_h3_lightx2v_no_pic","type":"invalid_request_error"}}`,
		StatusCode: 400,
	}
	respondTaskError(ctx, taskErr)

	assert.NotContains(t, taskErr.Message, "lightx2v")
	assert.NotContains(t, w.Body.String(), "lightx2v", "响应体里也不能残留上游工作流 ID")
	assert.Contains(t, w.Body.String(), "上游")
	assert.Contains(t, w.Body.String(), "至少一张参考图", "失败原因本身要保留")
}
