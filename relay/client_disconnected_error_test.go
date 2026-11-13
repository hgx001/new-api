package relay

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRelayTestContext(t *testing.T) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	return c
}

// 客户断开时返回 499 且标记 LocalError，是本文件的核心约束：
//   - 499 不是 5xx → shouldRetryTaskRelay 不会换渠道重发（一次超时不再放大成两次下载）；
//   - LocalError → controller/relay.go 跳过 processChannelError，不归咎渠道、不 AutoBan；
//   - 预扣费仍由 RelayTask 的 defer 走 Refund，客户不会被扣钱。
func TestBuildRequestErrorMapsClientGoneToLocal499(t *testing.T) {
	c := newRelayTestContext(t)
	err := fmt.Errorf("包一层: %w", taskcommon.ErrClientDisconnected)

	taskErr := buildRequestErrorToTaskError(c, err)

	require.NotNil(t, taskErr)
	assert.Equal(t, 499, taskErr.StatusCode, "客户端已断开必须是 499，不能是 5xx（5xx 会重试 + AutoBan）")
	assert.True(t, taskErr.LocalError, "断线是本端事件，不能归咎渠道")
	assert.Equal(t, "client_disconnected", taskErr.Code)
}

// ctx 被取消（net/http 在连接关闭时取消 request ctx）同样算客户走了，
// 即使适配器没把 sentinel 包出来。
func TestBuildRequestErrorTreatsCanceledRequestContextAsClientGone(t *testing.T) {
	c := newRelayTestContext(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	c.Request = c.Request.WithContext(ctx)
	cancel()

	taskErr := buildRequestErrorToTaskError(c, fmt.Errorf("读素材失败: %w", context.Canceled))

	require.NotNil(t, taskErr)
	assert.Equal(t, 499, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
}

// 入参错误仍然是 400（既有行为不能退化）。
func TestBuildRequestErrorKeepsUserErrorAs400Local(t *testing.T) {
	c := newRelayTestContext(t)
	userErr := taskcommon.NewUserError(fmt.Errorf("参考图分辨率过低：204x360"), "invalid_request")

	taskErr := buildRequestErrorToTaskError(c, userErr)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, "invalid_request", taskErr.Code)
}

// 真正的本端 bug 还是 500：不能被断线判定吞成 499，否则渠道故障再也不会被发现。
func TestBuildRequestErrorKeepsUnknownErrorsAs500(t *testing.T) {
	c := newRelayTestContext(t)
	taskErr := buildRequestErrorToTaskError(c, fmt.Errorf("json: unsupported type"))

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusInternalServerError, taskErr.StatusCode)
	assert.False(t, taskErr.LocalError)
	assert.Equal(t, "build_request_failed", taskErr.Code)
}

// DoRequest 阶段断线原本会走 IsSubmitOutcomeUnknown → 408 非 LocalError，
// 于是 processChannelError 照样把渠道记一次错（断线的客户端能把渠道打掉）。
func TestDoRequestErrorMapsClientGoneToLocal499(t *testing.T) {
	c := newRelayTestContext(t)

	taskErr := doRequestErrorToTaskError(c, fmt.Errorf("submit: %w", context.Canceled))

	require.NotNil(t, taskErr)
	assert.Equal(t, 499, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
	assert.Equal(t, "client_disconnected", taskErr.Code)
}

// 提交超时（上游没回）仍是 408「结果未知」，语义不能被断线改动带偏。
func TestDoRequestErrorKeepsSubmitOutcomeUnknownAs408(t *testing.T) {
	c := newRelayTestContext(t)

	taskErr := doRequestErrorToTaskError(c, context.DeadlineExceeded)

	require.NotNil(t, taskErr)
	assert.Equal(t, http.StatusRequestTimeout, taskErr.StatusCode)
	assert.False(t, taskErr.LocalError)
}
