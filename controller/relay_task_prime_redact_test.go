package controller

// 出口 1：提交失败时的对客响应。
//
// 生产真实样本（2026-11-12 17:12:17，渠道 17，seedance2.5-特惠）：
//
//	status_code=400, {"ok":false,"error":{"code":"PRIME_DURATION_TOO_SHORT",
//	"message":"由于服务器保护机制，暂时不受理低于 10 秒以下的prime任务"}}
//
// respondTaskError 是这条原文回给客户端前的**最后一道**。上游正文整段塞在
// TaskError.Message 里，Code 字段是本站自己的错误码（不受影响）。
// 所以这里按「响应体」断言，而不只是断言内存里的 taskErr —— 客户真正
// 收到的是 w.Body，不是结构体。

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const primeDurationTooShortBody = `status_code=400, {"ok":false,"error":{"code":"PRIME_DURATION_TOO_SHORT","message":"由于服务器保护机制，暂时不受理低于 10 秒以下的prime任务"}}`

func TestRespondTaskError_DropsPrimeTierFromClientResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "seedance2.5-特惠")

	taskErr := &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    primeDurationTooShortBody,
		StatusCode: 400,
	}
	respondTaskError(ctx, taskErr)

	assert.NotContains(t, strings.ToLower(w.Body.String()), "prime",
		"响应体里不能出现上游档位名 prime（含错误码前缀）")
	assert.NotContains(t, strings.ToLower(taskErr.Message), "prime",
		"改写后的 Message 也不能残留 prime")
	// 本站自己的错误码不受影响
	assert.Contains(t, w.Body.String(), "fail_to_fetch_task")
	// 失败原因要留着：客户得知道是时长低于 10 秒被拒，才能自己改请求
	assert.Contains(t, w.Body.String(), "低于 10 秒以下")
	assert.Contains(t, w.Body.String(), "DURATION_TOO_SHORT", "错误码主体仍是机器可读的")
}

// 活动版与特惠版同货源，上游同样会返回 prime 报错，必须一并拦住。
func TestRespondTaskError_DropsPrimeForHuodongAlias(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "seedance2.5-活动")

	respondTaskError(ctx, &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    primeDurationTooShortBody,
		StatusCode: 400,
	})

	assert.NotContains(t, strings.ToLower(w.Body.String()), "prime")
	assert.Contains(t, w.Body.String(), "低于 10 秒以下")
}

// 非别名模型直接调 wan3.0-video-prime：prime 是他自己买的模型名，
// 全局过滤会让他看不懂报错。这条守住脱敏的边界。
func TestRespondTaskError_KeepsPrimeForNonAliasModel(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Set("original_model", "wan3.0-video-prime")

	respondTaskError(ctx, &dto.TaskError{
		Code:       "fail_to_fetch_task",
		Message:    primeDurationTooShortBody,
		StatusCode: 400,
	})

	assert.Contains(t, w.Body.String(), "PRIME_DURATION_TOO_SHORT",
		"客户自己买的就是 prime，上游错误码原样保留才看得懂")
	assert.Contains(t, w.Body.String(), "prime任务")
}
