package model

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 提示词压缩的可追溯性（2026-11-03）。
//
// 压缩改写的是客户的创作描述，而任务照样成功、账单照收 —— 客户反馈
// 「成片与提示词不符」时，支持侧只能靠任务里的证据回答「这条压过没有」。
//
// 本文件锁两件事：
//  1. 压缩过的请求把「原长度 → 压缩后长度」写进 PrivateData（管理员可见）；
//  2. 没压缩的请求不能凭空多出这个字段（否则每一单都像是被压过）。

func newRelayInfoForCompress(from, to int) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		ChannelMeta:        &relaycommon.ChannelMeta{ChannelId: 17, ChannelType: 63},
		PromptCompressFrom: from,
		PromptCompressTo:   to,
	}
}

func TestInitTaskStoresPromptCompress(t *testing.T) {
	task := InitTask(constant.TaskPlatform("63"), newRelayInfoForCompress(6120, 4600))

	require.NotNil(t, task, "InitTask 不该返回 nil")
	require.NotNil(t, task.PrivateData.PromptCompress, "压缩过的请求必须记下长度变化")
	assert.Equal(t, 6120, task.PrivateData.PromptCompress.From)
	assert.Equal(t, 4600, task.PrivateData.PromptCompress.To)
}

func TestInitTaskOmitsPromptCompressWhenNotCompressed(t *testing.T) {
	task := InitTask(constant.TaskPlatform("63"), newRelayInfoForCompress(0, 0))

	require.NotNil(t, task, "InitTask 不该返回 nil")
	assert.Nil(t, task.PrivateData.PromptCompress, "没压缩的请求不该带 prompt_compress")
}
