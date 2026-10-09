package erchun

import (
	"encoding/json"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParseTaskResult 的错误语义直接影响三件事：任务是否收敛、客户是否退款、
// 用户看到什么文案。今天 wan2.7-r2v task 249 卡死就是状态映射漏了一个取值。
// 这里把每个上游状态映射到本端状态的规则钉死。

func parse(t *testing.T, payload map[string]any) *relaycommon.TaskInfo {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	info, err := (&TaskAdaptor{}).ParseTaskResult(raw)
	require.NoError(t, err)
	require.NotNil(t, info)
	return info
}

func TestParseTaskResultMapsUpstreamStatusToTerminalStates(t *testing.T) {
	// 已完成且带 content_url → 成功。Url 必须留空：content_url 需要渠道密钥，
	// 透传给下游会 401；留空后由 task_polling 落成站内 content 代理地址。
	ok := parse(t, map[string]any{"id": "1", "status": "completed", "content_url": "/v1/tasks/1/content"})
	assert.Equal(t, model.TaskStatusSuccess, ok.Status)
	assert.Equal(t, taskcommon.ProgressComplete, ok.Progress)
	assert.Empty(t, ok.Url, "绝不能把需要渠道密钥的 content_url 透传给下游")

	// 未完成 → 非终态，轮询继续
	for _, s := range []string{"queued", "pending", "submitted"} {
		info := parse(t, map[string]any{"status": s})
		assert.Equal(t, model.TaskStatusQueued, info.Status, "status=%s", s)
		assert.Equal(t, taskcommon.ProgressQueued, info.Progress)
	}
	for _, s := range []string{"in_progress", "running", "processing"} {
		info := parse(t, map[string]any{"status": s})
		assert.Equal(t, model.TaskStatusInProgress, info.Status, "status=%s", s)
	}

	// 未知状态按「还在进行」处理，绝不能判成功或失败。
	// 尤其不能只做前缀/包含匹配：status="completed_pending_verify" 不是终态成功。
	for _, s := range []string{"", "weird", "completed_pending_verify", "failure"} {
		info := parse(t, map[string]any{"status": s})
		assert.Equal(t, model.TaskStatusInProgress, info.Status, "未知状态 %q 不能判终态", s)
	}
}

// completed 却没给 content_url：绝不能判成功。
// 判成功会让用户为一个打不开的地址付了钱，而且不再轮询、不会触发退款。
func TestParseTaskResultNeverReportsSuccessWithoutContentURL(t *testing.T) {
	for _, s := range []string{"completed", "succeeded", "success"} {
		info := parse(t, map[string]any{"status": s})
		assert.NotEqual(t, model.TaskStatusSuccess, info.Status,
			"status=%s 无 content_url 时绝不能判成功", s)
		assert.Equal(t, model.TaskStatusInProgress, info.Status)
		assert.Equal(t, taskcommon.ProgressInProgress, info.Progress)

		// 空白 content_url 同样不行
		info = parse(t, map[string]any{"status": s, "content_url": "   "})
		assert.NotEqual(t, model.TaskStatusSuccess, info.Status, "status=%s 空白 content_url", s)
	}
}

// 失败文案必须如实。兜底成「内容审核不通过」是 2026-10-08 manwu 豆包首单
// 实证过的坑（commit 332ecfb5）：上游报 url_unavailable 实为产物 URL 校验失败，
// 用户看到「内容审核不通过」会误判自己提示词违规。
func TestParseTaskResultDoesNotBlameContentModeration(t *testing.T) {
	for _, s := range []string{"failed"} {
		info := parse(t, map[string]any{"status": s})
		assert.Equal(t, model.TaskStatusFailure, info.Status)
		assert.Equal(t, reasonUpstreamFailed, info.Reason)
		assert.NotEqual(t, taskcommon.ReasonContentModeration, info.Reason,
			"status=%s 不能误报内容审核不通过", s)
	}
	for _, s := range []string{"canceled", "cancelled"} {
		info := parse(t, map[string]any{"status": s})
		assert.Equal(t, model.TaskStatusFailure, info.Status)
		assert.Equal(t, reasonCancelled, info.Reason)
		assert.NotEqual(t, taskcommon.ReasonContentModeration, info.Reason,
			"status=%s 是取消，不是内容审核", s)
	}

	// 带 error 对象时优先用上游给的原文，不覆盖
	info := parse(t, map[string]any{
		"status": "failed",
		"error":  map[string]any{"code": "content_filter", "message": "prompt blocked by upstream filter"},
	})
	assert.Equal(t, model.TaskStatusFailure, info.Status)
	assert.Equal(t, "prompt blocked by upstream filter", info.Reason)

	// error 对象存在但字段全空时，也不能落回「内容审核不通过」
	info = parse(t, map[string]any{"status": "failed", "error": map[string]any{}})
	assert.Equal(t, reasonUpstreamFailed, info.Reason)

	// 只有 code 没有 message 时用 code
	info = parse(t, map[string]any{
		"status": "failed",
		"error":  map[string]any{"code": "upstream_timeout"},
	})
	assert.Equal(t, "upstream_timeout", info.Reason)
}

// error 对象一旦存在就以它为准，哪怕 status 还写着 in_progress ——
// 否则上游「带错误信息地失败」会被当成还在跑，任务永远收敛不了（task 249 那类卡死）。
func TestParseTaskResultPrefersErrorObjectOverStatus(t *testing.T) {
	info := parse(t, map[string]any{
		"status": "in_progress",
		"error":  map[string]any{"message": "account quota exhausted"},
	})
	assert.Equal(t, model.TaskStatusFailure, info.Status)
	assert.Equal(t, "account quota exhausted", info.Reason)
}

// 解析失败必须返回 error，不能吞掉后当成成功。
func TestParseTaskResultRejectsMalformedPayload(t *testing.T) {
	info, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":`))
	require.Error(t, err, "畸形 JSON 必须报错，不能返回 nil info 假装成功")
	assert.Nil(t, info)
}

// upstreamError 要兼容嵌套 error 对象与扁平 error 字段两种形状，
// 并在两者皆空时保留原始报文（否则排障时看不到上游到底说了什么）。
func TestUpstreamErrorHandlesBothShapes(t *testing.T) {
	nested := upstreamError(429, []byte(`{"error":{"code":"rate_limited","message":"too many requests"}}`))
	require.NotNil(t, nested)
	assert.Equal(t, 429, nested.StatusCode)
	assert.Contains(t, nested.Message, "too many requests")
	assert.Contains(t, nested.Message, "rate_limited")

	flat := upstreamError(402, []byte(`{"code":"insufficient_credits","message":"no balance"}`))
	require.NotNil(t, flat)
	assert.Equal(t, 402, flat.StatusCode)
	assert.Contains(t, flat.Message, "no balance")

	// 非 JSON 报文要原样带出，不能只留一个空 code
	raw := upstreamError(503, []byte(`<html>502 Bad Gateway</html>`))
	require.NotNil(t, raw)
	assert.Equal(t, 503, raw.StatusCode)
	assert.Contains(t, raw.Message, "502 Bad Gateway")
}

// convertErchunStatus 供 OpenAI video 响应使用，终态取值必须与 dto 定义对齐。
func TestConvertErchunStatusMapsToOpenAIVideoStatuses(t *testing.T) {
	for _, in := range []string{"completed", "succeeded", "success", "COMPLETED", " completed "} {
		assert.Equal(t, dto.VideoStatusCompleted, convertErchunStatus(in), "in=%q", in)
	}
	for _, in := range []string{"failed", "canceled", "cancelled"} {
		assert.Equal(t, dto.VideoStatusFailed, convertErchunStatus(in), "in=%q", in)
	}
	for _, in := range []string{"in_progress", "running", "processing"} {
		assert.Equal(t, dto.VideoStatusInProgress, convertErchunStatus(in), "in=%q", in)
	}
	for _, in := range []string{"queued", "pending", "submitted"} {
		assert.Equal(t, dto.VideoStatusQueued, convertErchunStatus(in), "in=%q", in)
	}
	assert.Equal(t, dto.VideoStatusUnknown, convertErchunStatus("something_new"))
}