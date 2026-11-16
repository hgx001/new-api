package youzanwan3

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/require"
)

// TestFailureRecheckDelayContract 锁定「上游失败复核窗口」的渠道契约。
//
// 2026-11-16 生产实锤：上游余额耗尽时把任务标成 failed 并非真终态，充值后
// 会把同一个任务复活继续执行并计费。轮询层据此窗口延迟退款 —— 窗口语义由
// service.TaskFailureRechecker 定义，本渠道必须参与（非零延迟）。
func TestFailureRecheckDelayContract(t *testing.T) {
	var adaptor TaskAdaptor

	// 必须实现可选接口（编译期断言在 adaptor.go，这里再锁运行时语义）。
	var rechecker service.TaskFailureRechecker = &adaptor
	require.Equal(t, 10*time.Minute, rechecker.FailureRecheckDelay(),
		"复核窗口必须是 10 分钟：更短挡不住「充值后复活」，更长拖慢客户退款")
}
