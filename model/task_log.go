package model

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
)

// TaskLog 是任务执行流水：提交快照、上游回执、状态迁移、失败原因、后处理阶段。
//
// 为什么需要（2026-10-30）：
//
//	tasks 表只存**最终快照**（status/progress/fail_reason/data），中间过程全丢。
//	线上出现「任务成功但产物不对」时，连「请求里到底有没有带参考图」都查不出来 ——
//	只能靠猜。这张表把关键节点按时间串起来，事后按 task_id 一查就知道发生了什么。
//
// 设计约束：
//   - 只记**状态与决策**，不记每次轮询的原文（否则一天几十万行）；
//   - Payload 存 JSON 文本（type:text），不依赖各数据库的 JSON 类型；
//   - 写入失败**不影响主流程**：日志是辅助证据，不能反过来打断下单/结算。
type TaskLog struct {
	Id             int64  `json:"id" gorm:"primary_key;AUTO_INCREMENT"`
	CreatedAt      int64  `json:"created_at" gorm:"index"`
	TaskId         string `json:"task_id" gorm:"type:varchar(191);index"`                    // 对外 task id
	UpstreamTaskId string `json:"upstream_task_id,omitempty" gorm:"type:varchar(191);index"` // 上游 task id
	Stage          string `json:"stage" gorm:"type:varchar(40);index"`                       // submit / poll / post_process / billing
	Level          string `json:"level" gorm:"type:varchar(10)"`                             // info / warn / error
	Message        string `json:"message" gorm:"type:text"`
	Payload        string `json:"payload,omitempty" gorm:"type:text"`
}

const (
	TaskLogStageSubmit      = "submit"
	TaskLogStagePoll        = "poll"
	TaskLogStagePostProcess = "post_process"
	TaskLogStageBilling     = "billing"

	TaskLogLevelInfo  = "info"
	TaskLogLevelWarn  = "warn"
	TaskLogLevelError = "error"
)

const (
	// taskLogMessageLimit 防止上游把整段 HTML 错误页塞进 message。
	taskLogMessageLimit = 1000
	// taskLogPayloadLimit 单条快照上限。提交快照含完整提示词与媒体 URL，
	// 长剧本 30 秒任务实测 6~10KB，16KB 足够且不会把表撑爆。
	taskLogPayloadLimit = 16 << 10
	// TaskLogRetentionDays 保留天数：出问题基本在一周内发现，30 天够用。
	TaskLogRetentionDays = 30
)

// AddTaskLog 写一条任务流水。任何失败只记系统日志，绝不向上返回错误 ——
// 调用点都在下单/轮询主链路上，不能让日志把业务打断。
func AddTaskLog(taskId, upstreamTaskId, stage, level, message string, payload any) {
	if DB == nil || taskId == "" {
		return
	}
	entry := &TaskLog{
		CreatedAt:      time.Now().Unix(),
		TaskId:         taskId,
		UpstreamTaskId: upstreamTaskId,
		Stage:          stage,
		Level:          level,
		Message:        truncateText(message, taskLogMessageLimit),
	}
	if payload != nil {
		entry.Payload = encodeTaskLogPayload(payload)
	}
	if err := DB.Create(entry).Error; err != nil {
		common.SysError("add task log failed: " + err.Error())
	}
}

// GetTaskLogs 按 task id（对外或上游）查流水，新的在前。
func GetTaskLogs(taskId, upstreamTaskId string, limit int) ([]TaskLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	query := DB.Model(&TaskLog{})
	switch {
	case taskId != "":
		query = query.Where("task_id = ?", taskId)
	case upstreamTaskId != "":
		query = query.Where("upstream_task_id = ?", upstreamTaskId)
	default:
		return nil, fmt.Errorf("task_id or upstream_task_id is required")
	}
	var logs []TaskLog
	err := query.Order("id desc").Limit(limit).Find(&logs).Error
	return logs, err
}

// DeleteTaskLogsBefore 分批删除过期流水，返回删除行数。
//
// 先查 id 再按 id 删，不用 `DELETE ... LIMIT`：
// Postgres 不支持 DELETE 的 LIMIT，MySQL 不支持 `IN (SELECT ... LIMIT n)` 子查询，
// 三种库都能跑的就只有「先取一批 id、再按主键删」。
func DeleteTaskLogsBefore(beforeTimestamp int64, batchSize int) (int64, error) {
	if beforeTimestamp <= 0 {
		return 0, nil
	}
	if batchSize <= 0 || batchSize > 5000 {
		batchSize = 1000
	}
	var total int64
	for {
		var ids []int64
		if err := DB.Model(&TaskLog{}).
			Where("created_at < ?", beforeTimestamp).
			Order("id").
			Limit(batchSize).
			Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		result := DB.Where("id IN ?", ids).Delete(&TaskLog{})
		if result.Error != nil {
			return total, result.Error
		}
		total += result.RowsAffected
		if len(ids) < batchSize {
			return total, nil
		}
	}
}

// encodeTaskLogPayload 把快照序列化成**合法 JSON** 字符串。
//
// 不能直接对序列化结果按长度截断：JSON 是结构化文本，从中间切一刀就成了
// 非法 JSON，`payload::jsonb` 之类的查询会直接报错（生产实测 381 条提交快照里
// 有 18 条因此不可解析）。超限时逐级降级，但**每一级输出都仍是合法 JSON**。
func encodeTaskLogPayload(payload any) string {
	encoded, err := common.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"marshal_error":%q}`, err.Error())
	}
	if len(encoded) <= taskLogPayloadLimit {
		return string(encoded)
	}
	// 一级降级：提示词是快照里最大的字段，先砍它，其余（media/渠道/计费）全留。
	if fields, ok := payload.(map[string]any); ok {
		trimmed := make(map[string]any, len(fields)+1)
		for key, value := range fields {
			trimmed[key] = value
		}
		if prompt, ok := trimmed["prompt"].(string); ok {
			trimmed["prompt"] = truncateText(prompt, 200)
			trimmed["prompt_truncated"] = true
		}
		if slim, marshalErr := common.Marshal(trimmed); marshalErr == nil && len(slim) <= taskLogPayloadLimit {
			return string(slim)
		}
	}
	// 二级降级：仍然超限（多半是内联 base64 媒体），只留体积信息，保证可解析。
	fallback, marshalErr := common.Marshal(map[string]any{
		"truncated":      true,
		"original_bytes": len(encoded),
	})
	if marshalErr != nil {
		return `{"truncated":true}`
	}
	return string(fallback)
}

func truncateText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	// 按 rune 截断，避免把 UTF-8 汉字切一半写进库。
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…(truncated)"
}
