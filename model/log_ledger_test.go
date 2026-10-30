package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 运营内部成本台账不能出现在客户日志里。
//
// 为什么必须滤掉而不是脱敏：这些记录是运营手工写库的自由文本，
// 正文里塞着上游 task_id（UUID）、上游内部 vid、上游成本与渠道名 ——
// 词表遮不干净（UUID 没有对应规则）。
//
// 两种形态都要拦：
//   · token_name = OPERATOR-COST-ACCOUNTING（运营用 SQL 写的独立台账）
//   · 正文以 OPERATOR-COST-ACCOUNTING 开头（跟着退款日志一起写的备注）
func TestGetUserLogs_过滤运营内部成本台账(t *testing.T) {
	require.NoError(t, DB.Create(&Log{
		UserId:    30,
		Username:  "customer",
		Type:      LogTypeError,
		ModelName: "wan-3.0",
		Content:   "[OPERATOR-COST-ACCOUNTING] 调价探针上游净损 810 有赞积分（task 306-314）",
		TokenName: "OPERATOR-COST-ACCOUNTING",
	}).Error)
	require.NoError(t, DB.Create(&Log{
		UserId:    30,
		Username:  "customer",
		Type:      LogTypeRefund,
		ModelName: "wan-3.0",
		Content:   "OPERATOR-COST-ACCOUNTING 用户明确允许的真实任务测试：MegaAI 渠道 wan-3.0 30 秒 720p（上游 task_id=task_36be6a…）",
		TokenName: "auto",
	}).Error)
	// 普通消费日志必须留着
	require.NoError(t, DB.Create(&Log{
		UserId:    30,
		Username:  "customer",
		Type:      LogTypeConsume,
		ModelName: "seedance2.5-特惠",
		Content:   "操作 generate, 计算参数：seconds: 2.00",
		TokenName: "auto",
	}).Error)

	logs, total, err := GetUserLogs(30, LogTypeUnknown, 0, 0, "", "", 0, 50, "", "", "")
	require.NoError(t, err)

	require.Equal(t, int64(1), total, "内部台账两条都要被滤掉，只剩客户自己的消费日志")
	require.Len(t, logs, 1)
	assert.Equal(t, LogTypeConsume, logs[0].Type)
	assert.Contains(t, logs[0].Content, "操作 generate", "客户自己的日志要留着")
}
