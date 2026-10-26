package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/task/manwu"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testTaskError(statusCode int, message string, localError bool) *dto.TaskError {
	return &dto.TaskError{
		Code:       "upstream_error",
		Message:    message,
		StatusCode: statusCode,
		LocalError: localError,
	}
}

func TestShouldRetryTaskRelayOnBalanceError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		taskErr    *dto.TaskError
		retryTimes int
		pinned     bool
		want       bool
	}{
		{name: "400 balance switches channel", taskErr: testTaskError(400, "账户余额不足，请充值", false), retryTimes: 1, want: true},
		{name: "402 always switches channel", taskErr: testTaskError(402, "payment required", false), retryTimes: 1, want: true},
		{name: "400 plain bad request stays", taskErr: testTaskError(400, "invalid resolution", false), retryTimes: 1, want: false},
		{name: "400 local validation stays", taskErr: testTaskError(400, "prompt is required", true), retryTimes: 1, want: false},
		{name: "no retry budget stays", taskErr: testTaskError(400, "账户余额不足", false), retryTimes: 0, want: false},
		// 提交结果未知（超时/要求对账）不能重试：上游可能已受理，重发会重复计费。
		{name: "408 unknown submit outcome stays", taskErr: testTaskError(408, "context deadline exceeded", false), retryTimes: 1, want: false},
		{name: "408 reconciliation stays", taskErr: testTaskError(408, "WAN3_SUBMISSION_RECONCILIATION_REQUIRED", false), retryTimes: 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			if tt.pinned {
				ctx.Set("specific_channel_id", 17)
			}
			got := shouldRetryTaskRelay(ctx, 17, tt.taskErr, tt.retryTimes)
			if tt.want {
				require.True(t, got, "should fail over to backup channel")
			} else {
				assert.False(t, got, "should not retry")
			}
		})
	}

	t.Run("pinned channel never switches even on balance error", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set("specific_channel_id", 17)
		assert.False(t, shouldRetryTaskRelay(ctx, 17, testTaskError(400, "账户余额不足", false), 1))
	})
}

// 幂等任务提交（manwu/ArcReel）：408/504/524 是「提交结果未知」，不重试会造成
// 用户无单无扣、上游白烧额度（gen-78754689fdc325f5ce17510c 事故）。幂等键已按
// 请求固定，ArcReel 靠唯一索引 + 幂等重放防重，此时重试是安全的。
func TestShouldRetryTaskRelayIdempotentSubmit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	newIdemCtx := func() *gin.Context {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ctx.Set(manwu.CtxKeyTaskSubmitIdempotent, true)
		return ctx
	}

	t.Run("idempotent 408 retries", func(t *testing.T) {
		assert.True(t, shouldRetryTaskRelay(newIdemCtx(), 17, testTaskError(408, "context deadline exceeded", false), 1))
	})
	t.Run("idempotent 504 retries", func(t *testing.T) {
		assert.True(t, shouldRetryTaskRelay(newIdemCtx(), 17, testTaskError(504, "<html>504 Gateway Time-out</html>", false), 1))
	})
	t.Run("idempotent 524 retries", func(t *testing.T) {
		assert.True(t, shouldRetryTaskRelay(newIdemCtx(), 17, testTaskError(524, "a timeout occurred", false), 1))
	})
	t.Run("idempotent 400 still never retries", func(t *testing.T) {
		assert.False(t, shouldRetryTaskRelay(newIdemCtx(), 17, testTaskError(400, "prompt is required", true), 1))
	})
	t.Run("non-idempotent 408 stays no-retry", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		assert.False(t, shouldRetryTaskRelay(ctx, 17, testTaskError(408, "azure timeout", false), 1))
	})
	t.Run("non-idempotent 504 stays no-retry", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		assert.False(t, shouldRetryTaskRelay(ctx, 17, testTaskError(504, "gateway timeout", false), 1))
	})
}
