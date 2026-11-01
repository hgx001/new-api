package taskcommon

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ReasonContentModeration 是无理由失败的统一口径：视频任务已运行一段时间
// 才失败、且上游未返回任何原因时，归因为内容审核不通过，方便下游对接时定位。
const ReasonContentModeration = "内容审核不通过"

// ReasonUpstreamFailure 是「上游内部失败、且没给出可行动原因」时的统一对客文案。
//
// 为什么不用 ReasonContentModeration：那是「无理由失败一律归内容审核」的旧口径，
// 会把上游内部故障谎报成内容违规，客户拿着错的方向去改提示词，白折腾一轮。
//
// 为什么要统一在 taskcommon：多个渠道都会碰到这种兜底错误（漫屋的 reasonUnknownFailure、
// 有赞的 TASK_ERROR），客户在同一个平台里看到的文案必须一致，否则同一个问题在不同
// 模型上表现成两种说法，客服和客户都要重新对一遍口径。
//
// 2026-10-31 有赞实证：三单相同 prompt/参数/时长，3.5 分钟前和 3 分钟后都成功，
// 只有这一批返回 TASK_ERROR「错误请重试」——上游的兜底码，没有细粒度原因。
// 原样透传「错误请重试」会让客户以为是自己参数写错，反复改参数重试。
const ReasonUpstreamFailure = "上游生成失败（未提供具体原因），请稍后重试或联系客服"

// NormalizeFailureReason 将空原因或上游未给出的通用占位原因统一为内容审核不通过。
// 保留上游返回的任何具体错误信息不变。
func NormalizeFailureReason(reason string) string {
	trimmed := strings.TrimSpace(reason)
	if trimmed == "" {
		return ReasonContentModeration
	}
	switch strings.ToLower(trimmed) {
	case "task failed", "autodl task failed", "wan3 task failed",
		"upstream returned error", "upstream returned empty status",
		"upstream returned unrecognized message":
		return ReasonContentModeration
	}
	return trimmed
}

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
