package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactUpstreamIdentity_AliasModel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "组合写法整体替换，不留 上游:上游 碎片",
			in:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support the requested output combination"}}`,
			want: `status_code=400, {"error":{"message":"model 上游 does not support the requested output combination"}}`,
		},
		{
			name: "供应商 + 产品线连续命中时收敛成一个中性词",
			in:   `status_code=500, youzan wan3 asset upload failed: HTTP 422`,
			want: `status_code=500, 上游 asset upload failed: HTTP 422`,
		},
		{
			name: "独立的 ali 被替换",
			in:   `status_code=400, ali rejected the request`,
			want: `status_code=400, 上游 rejected the request`,
		},
		{
			name: "validate / invalid 里的 ali 不能被误伤",
			in:   `status_code=400, Failed to validate API key (invalid_request_error)`,
			want: `status_code=400, Failed to validate API key (invalid_request_error)`,
		},
		{
			name: "对外在售的 wan3.0 变体整体替换",
			in:   `status_code=400, model wan3.0-480p not available`,
			want: `status_code=400, model 上游 not available`,
		},
		{
			name: "渠道编号 LK888 被替换",
			in:   `status_code=500, LK888 image task failed: 上游没有返回预期的图像`,
			want: `status_code=500, 上游 image task failed: 上游没有返回预期的图像`,
		},
		{
			name: "腾讯云原文被替换",
			in:   `status_code=429, ... contact Tencent Cloud support to request a higher limit`,
			want: `status_code=429, ... contact 上游 support to request a higher limit`,
		},
		{
			name: "别名模型自身的名字必须原样保留",
			in:   `status_code=500, model seedance2.5-特惠 is busy`,
			want: `status_code=500, model seedance2.5-特惠 is busy`,
		},
		{
			name: "wan2 系列是另外在售的模型，不该被牵连",
			in:   `status_code=500, model wan2.7-r2v is busy`,
			want: `status_code=500, model wan2.7-r2v is busy`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, RedactUpstreamIdentity(tc.in, "seedance2.5-特惠"))
		})
	}
}

func TestRedactUpstreamIdentity_历史旧名同样生效(t *testing.T) {
	assert.Equal(t,
		`status_code=400, model 上游 does not support`,
		RedactUpstreamIdentity(`status_code=400, model aliyun:wan-3.0 does not support`, "seedance2.5特惠版"))
}

func TestRedactUpstreamIdentity_非别名模型原样返回(t *testing.T) {
	raw := `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`
	for _, modelName := range []string{"wan-3.0", "wan3.0-smart", "gpt-6-luna", "seedance-2.5", ""} {
		assert.Equal(t, raw, RedactUpstreamIdentity(raw, modelName), "模型 %s 的文案不该被改写", modelName)
	}
}

func TestRedactUpstreamIdentity_空内容(t *testing.T) {
	assert.Equal(t, "", RedactUpstreamIdentity("", "seedance2.5-特惠"))
}

func TestRedactUpstreamIdentityKeepURL_URL区段不能动(t *testing.T) {
	raw := `成片地址 https://cdn.example.com/wan3.0-720p/final.mp4 拉取失败：model aliyun:wan-3.0 rejected`
	got := RedactUpstreamIdentityKeepURL(raw, "seedance2.5-特惠")
	assert.Contains(t, got, "https://cdn.example.com/wan3.0-720p/final.mp4",
		"交付链接改一个字就下载不了，URL 区段必须原样保留")
	assert.NotContains(t, got, "aliyun")
	assert.Contains(t, got, "上游")
}

func TestRedactUpstreamIdentityJSON_保持合法JSON且遮掉模型名(t *testing.T) {
	raw := []byte(`{"id":"task_36be6a9c56a14eebbeb1573c43abbf7e","model":"wan-3.0","metadata":{"final_video_url":"https://x.r2.cloudflarestorage.com/hailuo-video/v.mp4"}}`)
	got := RedactUpstreamIdentityJSON(raw, "seedance2.5-特惠")
	require.True(t, json.Valid(got), "脱敏后必须仍是合法 JSON")
	assert.NotContains(t, string(got), "wan-3.0")
	assert.Contains(t, string(got), "https://x.r2.cloudflarestorage.com/hailuo-video/v.mp4")
}

func TestRedactUpstreamIdentityJSON_非别名模型与非法JSON原样返回(t *testing.T) {
	raw := []byte(`{"model":"wan-3.0"}`)
	assert.Equal(t, raw, RedactUpstreamIdentityJSON(raw, "wan-3.0"))

	broken := []byte(`{"model":`)
	assert.Equal(t, broken, RedactUpstreamIdentityJSON(broken, "seedance2.5-特惠"),
		"脱敏后不是合法 JSON 就原样返回，绝不能把下游响应体搞坏")
}

func TestFormatUserLogs_错误日志对普通用户隐藏渠道名与上游字样(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeError,
			ModelName: "seedance2.5-特惠",
			Content:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3","channel_type":64,"admin_info":{"use_channel":["24"]}}`,
		},
		{
			Type:      LogTypeError,
			ModelName: "wan-3.0",
			Content:   `status_code=400, {"error":{"message":"model aliyun:wan-3.0 does not support"}}`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3"}`,
		},
		{
			Type:      LogTypeConsume,
			ModelName: "seedance2.5-特惠",
			Content:   `aliyun:wan-3.0`,
			Other:     `{"channel_id":24,"channel_name":"MegaAI-Wan3"}`,
		},
	}

	formatUserLogs(logs, 0)

	// 别名模型的错误日志：上游字样被替换、渠道名被清掉
	assert.NotContains(t, logs[0].Content, "aliyun")
	assert.NotContains(t, logs[0].Content, "wan-3.0")
	assert.NotContains(t, logs[0].Other, "channel_name")
	assert.NotContains(t, logs[0].Other, "admin_info")

	// 其它模型：正文原样保留，但渠道名同样不能给普通用户看
	assert.Contains(t, logs[1].Content, "aliyun")
	assert.NotContains(t, logs[1].Other, "channel_name")

	// 非错误日志：正文不在脱敏范围
	assert.Equal(t, "aliyun:wan-3.0", logs[2].Content)
}

func TestFormatUserLogs_别名模型的消费日志不暴露上游模型名(t *testing.T) {
	logs := []*Log{
		{
			Type:      LogTypeConsume,
			ModelName: "seedance2.5-特惠",
			Content:   "模型倍率 1",
			Other:     `{"is_model_mapped":true,"upstream_model_name":"wan-3.0","channel_id":24}`,
		},
		{
			Type:      LogTypeConsume,
			ModelName: "gpt-5.6-luna",
			Content:   "模型倍率 1",
			Other:     `{"is_model_mapped":true,"upstream_model_name":"gpt-6-luna"}`,
		},
	}

	formatUserLogs(logs, 0)

	// 别名模型：前端的 Model Mapping 面板会把 upstream_model_name 显示给用户，必须抹掉
	assert.NotContains(t, logs[0].Other, "upstream_model_name")
	assert.NotContains(t, logs[0].Other, "is_model_mapped")

	// 其它模型：映射信息照常展示（本来就对外公开）
	assert.Contains(t, logs[1].Other, "upstream_model_name")
}
