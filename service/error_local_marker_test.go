package service

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本端预扣费/额度错误带 ErrOptionWithSkipRetry，转成 dto.TaskError 后必须保留
// LocalError 标记。丢失它会造成两起生产事故（详见 TaskErrorFromAPIError 注释）：
//   - shouldRetryTaskRelay 的余额关键词分支 → 白白换渠道重试；
//   - controller/relay.go 的 processChannelError → AutoBan 下线整条渠道。
func TestTaskErrorFromAPIErrorPreservesLocalMarker(t *testing.T) {
	local := types.NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: %s", "¥0.000000"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
	taskErr := TaskErrorFromAPIError(local)
	require.NotNil(t, taskErr)
	assert.True(t, taskErr.LocalError, "skip-retry 语义必须透传为 LocalError")
	assert.Equal(t, http.StatusForbidden, taskErr.StatusCode)
	assert.Equal(t, string(types.ErrorCodeInsufficientUserQuota), taskErr.Code)
	assert.Contains(t, taskErr.Message, "剩余额度")

	// 上游真实错误不带 skipRetry → 不得被标成 local，否则渠道错误会被当成本端错误吞掉。
	upstream := types.NewErrorWithStatusCode(
		fmt.Errorf("上游返回 500"),
		types.ErrorCodeDoRequestFailed,
		http.StatusInternalServerError,
	)
	upstreamTaskErr := TaskErrorFromAPIError(upstream)
	require.NotNil(t, upstreamTaskErr)
	assert.False(t, upstreamTaskErr.LocalError)

	assert.Nil(t, TaskErrorFromAPIError(nil))
}

// 直接锁死事故根因：即便错误文案里带「剩余额度」，只要它是本端 skip-retry 错误，
// 就绝不能被判为「上游余额耗尽」而禁用渠道。
func TestShouldDisableChannelIgnoresLocalQuotaError(t *testing.T) {
	// AutomaticDisableChannelEnabled 默认 false，ShouldDisableChannel 会直接返回 false；
	// 这里显式打开，否则断言的是开关而不是本次修复的逻辑。
	prev := common.AutomaticDisableChannelEnabled
	common.AutomaticDisableChannelEnabled = true
	t.Cleanup(func() { common.AutomaticDisableChannelEnabled = prev })
	localQuota := types.NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: %s", "¥0.000000"),
		types.ErrorCodeInsufficientUserQuota,
		http.StatusForbidden,
		types.ErrOptionWithSkipRetry(),
		types.ErrOptionWithNoRecordErrorLog(),
	)
	assert.False(t, ShouldDisableChannel(localQuota),
		"本端额度错误不得禁用渠道（曾导致一个欠费客户端下线整条漫屋渠道）")

	// 反向对照：同样是「剩余额度」文案，但来自上游且没带 skipRetry → 仍应禁用。
	upstreamExhausted := types.NewErrorWithStatusCode(
		fmt.Errorf("用户额度不足, 剩余额度: ¥0.000000"),
		types.ErrorCodeDoRequestFailed,
		http.StatusForbidden,
	)
	assert.True(t, ShouldDisableChannel(upstreamExhausted),
		"上游账户余额耗尽仍需下线渠道（保留原语义）")
}

// 上游余额类错误的关键词分类保持不变（本次修复不应削弱它）。
func TestUpstreamBalanceClassificationUnchanged(t *testing.T) {
	assert.True(t, IsUpstreamAccountBalanceError(http.StatusForbidden, "用户额度不足, 剩余额度: ¥0.000000"))
	assert.True(t, IsUpstreamAccountBalanceError(http.StatusPaymentRequired, ""))
	assert.True(t, IsUpstreamAccountBalanceError(http.StatusBadRequest, "insufficient balance"))
	assert.False(t, IsUpstreamAccountBalanceError(http.StatusBadRequest, "prompt is required"))
	assert.False(t, IsUpstreamAccountBalanceError(http.StatusBadRequest, ""))
}
