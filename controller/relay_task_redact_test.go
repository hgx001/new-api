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

// 非别名模型：上游原文照常回给客户端（错误信息要可读，遮了只会更难排查）。
func TestRespondTaskErrorKeepsNonAliasMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "wan-3.0")

	raw := `{"error":{"message":"model aliyun:wan-3.0 does not support"}}`
	taskErr := &dto.TaskError{Code: "fail_to_fetch_task", Message: raw, StatusCode: 400}
	respondTaskError(ctx, taskErr)

	assert.Equal(t, raw, taskErr.Message)
}
