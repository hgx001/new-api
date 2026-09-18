package controller

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
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
