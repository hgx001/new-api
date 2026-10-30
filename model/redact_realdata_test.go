package model

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 生产库真实数据的脱敏验证（2026-10-30）。
//
// 为什么需要这个测试：单元测试用的是人造样本，而真实库里存着**各种形状**的上游原文
// （有赞的错误文案、阿里云的回执、ArcReel 的 jobResponse、腾讯云的提升日志…）。
// 拿真实数据过一遍脱敏函数，才能确认「有渠道的一律说上游」这条口径真的覆盖到了。
//
// 数据来源：scripts/_verify_redact_db.py → /tmp/redact_db.json
// 跑法：
//
//	cd /c/work/new-api-src && python scripts/_verify_redact_db.py
//	go test ./model/ -run TestRealProductionDataRedaction -v
//
// 数据文件不存在时自动跳过（本地/CI 不一定连着生产库）。
func TestRealProductionDataRedaction(t *testing.T) {
	raw, err := os.ReadFile("/tmp/redact_db.json")
	if err != nil {
		t.Skipf("生产数据快照不存在（先跑 scripts/_verify_redact_db.py）：%v", err)
	}

	var payload struct {
		Tasks []struct {
			TaskID      string          `json:"task_id"`
			Status      string          `json:"status"`
			Platform    string          `json:"platform"`
			ChannelID   int             `json:"channel_id"`
			FailReason  string          `json:"fail_reason"`
			Properties  json.RawMessage `json:"properties"`
			PrivateData json.RawMessage `json:"private_data"`
			Data        json.RawMessage `json:"data"`
		} `json:"tasks"`
		Logs []struct {
			Type             int             `json:"type"`
			ModelName        string          `json:"model_name"`
			Content          string          `json:"content"`
			ChannelID        int             `json:"channel_id"`
			ChannelName      string          `json:"channel_name"`
			UpstreamRequestID string         `json:"upstream_request_id"`
			Other            string          `json:"other"`
		} `json:"logs"`
	}
	require.NoError(t, json.Unmarshal(raw, &payload))
	require.NotEmpty(t, payload.Tasks, "任务快照为空")
	require.NotEmpty(t, payload.Logs, "日志快照为空")

	// 客户侧不该出现的字样（与 scripts/_verify_redact_live.py 保持一致）
	//
	// ⚠️ 不包含 "wan3"：客户可以直接买 wan3.0-* 系列，
	// 他在报错里看到 "wan3 media url is required" 属于正常 —— 遮了反而看不懂。
	// 要遮的是**上游渠道身份**（有赞/阿里云/漫屋…）与上游地址。
	banned := []string{
		"youzan", "有赞", "aliyun", "阿里", "dashscope", "百炼",
		"tencent", "腾讯", "volcengine", "火山",
		"doubao", "豆包", "manwu", "漫屋", "arcreel", "巡狩", "三元",
		"erchun", "二春", "aliyuncs.com", "r2.cloudflarestorage.com",
		"hailuo-video", "cos-proxy://", "megaai", "megabyai",
	}

	// ---------- 出口 1：错误日志文案 ----------
	for _, entry := range payload.Logs {
		if entry.Type != LogTypeError {
			continue
		}
		got := RedactUpstreamText(entry.Content, entry.ModelName)
		for _, token := range banned {
			assert.NotContains(t, strings.ToLower(got), token,
				"错误日志文案仍含渠道痕迹")
		}
	}

	// ---------- 出口 2：客户侧日志的其他字段 ----------
	logs := make([]*Log, 0, len(payload.Logs))
	for _, entry := range payload.Logs {
		logs = append(logs, &Log{
			Type:              entry.Type,
			ModelName:         entry.ModelName,
			Content:           entry.Content,
			ChannelId:         entry.ChannelID,
			ChannelName:       entry.ChannelName,
			UpstreamRequestId: entry.UpstreamRequestID,
			Other:             entry.Other,
		})
	}
	formatUserLogs(logs, 0)

	for i, entry := range logs {
		assert.Equal(t, 0, entry.ChannelId, "客户侧日志渠道 ID 必须置 0")
		assert.Empty(t, entry.ChannelName, "客户侧日志渠道名必须清空")
		assert.Empty(t, entry.UpstreamRequestId, "客户侧日志上游任务号必须清空")
		for _, token := range banned {
			assert.NotContains(t, strings.ToLower(entry.Content), token,
				"客户侧日志正文含渠道痕迹")
			assert.NotContains(t, strings.ToLower(entry.Other), token,
				"客户侧日志 other 含渠道痕迹")
		}
		_ = i
	}

	// ---------- 出口 3：任务 DTO ----------
	for _, entry := range payload.Tasks {
		var props Properties
		if len(entry.Properties) > 0 {
			_ = json.Unmarshal(entry.Properties, &props)
		}
		var private TaskPrivateData
		if len(entry.PrivateData) > 0 {
			_ = json.Unmarshal(entry.PrivateData, &private)
		}

		task := &Task{
			TaskID:      entry.TaskID,
			Status:      TaskStatus(entry.Status),
			Platform:    constant.TaskPlatform(entry.Platform),
			ChannelId:   entry.ChannelID,
			FailReason:  entry.FailReason,
			Properties:  props,
			PrivateData: private,
			Data:        entry.Data,
		}

		// 脱敏后的任务 DTO（与 relay.TaskModel2Dto 同一套口径的等价实现：
		// 这里只验「文案与 properties」，DTO 的字段级断言在 relay 包里）
		origin := task.PublicModelName()
		got := RedactUpstreamText(task.FailReason, origin)
		for _, token := range banned {
			assert.NotContains(t, strings.ToLower(got), token,
				"任务失败文案含渠道痕迹")
		}
	}
}
