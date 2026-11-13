package taskcommon

import (
	"context"
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

// 客户端断开必须能被**跨包**识别出来。
//
// 2026-11-13 生产现象：threeFatAi 工作台的提交超时写死 60 秒，客户端先断开，
// 适配器把 context.Canceled 包成「素材下载中止」返回，relay_task 认不出来，
// 于是降级成 500 build_request_failed —— 500 会触发换渠道重试（把一次超时放大成
// 两次完整下载）并走 processChannelError（一个断线的客户端就可能把渠道 AutoBan）。
//
// 判据放在 taskcommon，是为了让 relay_task 不必 import 任何具体渠道包。
func TestIsClientDisconnectedRecognizesWrappedSentinel(t *testing.T) {
	wrapped := errors.Wrapf(ErrClientDisconnected, "客户端已断开，素材%s中止", "下载")

	require.True(t, IsClientDisconnected(wrapped))
	require.True(t, IsClientDisconnected(fmt.Errorf("再包一层: %w", wrapped)))
}

// context.Canceled 是断开在标准库里的另一种形状：适配器直接返回 ctx.Err()
// 时也必须认出来，否则同一条链路会因为「哪个适配器写的」而行为不一致。
func TestIsClientDisconnectedTreatsContextCanceledAsClientGone(t *testing.T) {
	require.True(t, IsClientDisconnected(context.Canceled))
	require.True(t, IsClientDisconnected(errors.Wrap(context.Canceled, "下载")))
}

// 反过来不能误伤：deadline exceeded 是「我们等太久」，仍然要按上游/本端超时处理，
// 不能被当成「客户已经走了」而掩掉渠道故障。
func TestIsClientDisconnectedRejectsOtherErrors(t *testing.T) {
	require.False(t, IsClientDisconnected(nil))
	require.False(t, IsClientDisconnected(context.DeadlineExceeded))
	require.False(t, IsClientDisconnected(fmt.Errorf("源站返回 502")))
	require.False(t, IsClientDisconnected(NewUserError(fmt.Errorf("参考素材下载失败"), "asset_unavailable")))
}
