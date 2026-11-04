package wan3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

// ─────────────────────────────────────────────────────────────────────────────
// 成片转存（不做画质增强）—— 2026-11-04 新增。
//
// 为什么需要它：
//
//	有些上游的成片直链**只有短期有效期**（如「视频生成车站」的排队通道直链
//	约 24 小时过期，站点不留副本）。不转存的话，用户过了有效期再取片必然 404，
//	而任务在我们库里是「成功」状态 —— 用户看到的是「成功但打不开」，
//	这是最坏的一种失败。
//
//	转存 = 用 MPS 以 URL 直链为输入、产物落 COS，库里存 cos-proxy:// 占位符，
//	取片时实时预签名。**与超分链路共用同一套 MPS 凭据与 COS 桶**，
//	只是请求体里不配 EnhanceConfig —— 于是**原分辨率、原画质**转存，
//	不会有任何画质损失。
//
//	与 SubmitSuperResolution 的区别：
//	  · SubmitSuperResolution —— 配 EnhanceConfig.SuperResolution，2 倍放大；
//	  · SubmitRelocate       —— 不配 EnhanceConfig，纯转存，尺寸不变。
//	两者产物都落 COS、都用 cos-proxy:// 占位符，取片路径完全一致。
//
//	为什么不直接把上游直链透传给客户端：
//	透传最省事，但客户端若隔天才取片就会拿到 404。转存多花一次 MPS 转码
//	（几十秒 + 几分钱），换来的是「成片永久可取」。
// ─────────────────────────────────────────────────────────────────────────────

// relocateProvider 标识转存任务的服务（与超分区分，便于日志排查）。
const relocateProvider = "tencent-mps-relocate"

// relocateMaxWait 单个任务转存的最长等待时间。
//
// 超时后回落上游原片 —— 不能让用户的任务永远挂在「处理中」。
const relocateMaxWait = 20 * time.Minute

// SubmitRelocate 提交一个**纯转存**任务（不增强画质），并在 task 上记下状态。
//
// 调用方负责决定何时转存（一般是在上游返回成功后、置 SUCCESS 之前）。
//
// 返回 handled=true 表示已接管：service 层会保持任务为「处理中」，
// 等 PollRelocate 收敛后再置 SUCCESS。
//
// 返回 handled=false 表示「不接管，按原流程走」：没有配置 MPS、或源地址为空。
// 此时用户拿到的仍是上游原片（短期有效的直链）。
func SubmitRelocate(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil {
		return false, nil
	}
	// 没配 MPS 就不接管 —— 行为与改造前完全一致。
	if !tencentmps_setting.IsConfigured() {
		return false, nil
	}
	if strings.TrimSpace(upstreamURL) == "" {
		return false, nil
	}

	cfg := tencentmps_setting.Get()
	client := newMPSClient(cfg.SecretId, cfg.SecretKey, cfg.Region)
	sessionID := fmt.Sprintf("%s-relocate-%d", cfg.SessionIdPrefix, time.Now().UnixNano())

	taskID, err := client.submitRelocate(ctx, mpsRelocateRequest{
		InputURL:         upstreamURL,
		Bucket:           cfg.Bucket,
		Region:           cfg.Region,
		OutputDir:        cfg.OutputDir,
		OutputObjectPath: RelocateObjectName(task.TaskID),
		BitrateKbps:      cfg.OutputBitrateKbps,
		SessionID:        sessionID,
	})
	if err != nil {
		// 提交失败：回落上游原片，**不让用户任务失败**。
		//
		// 这里返回 (false, nil) 而不是 error：service 层收到 error 会保持「处理中」
		// 并无限重试，而提交失败往往是配置问题（密钥无效、桶不存在），
		// 重试也不会好，只会拖到超时。回落原片让用户至少拿到片。
		logger.LogWarn(ctx, fmt.Sprintf("Task %s 成片转存提交失败，回落上游原片: %s",
			task.TaskID, err.Error()))
		return false, nil
	}

	task.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:       postProcessStageSubmitted,
		Provider:    relocateProvider,
		TaskID:      taskID,
		SubmittedAt: time.Now().Unix(),
		SourceURL:   upstreamURL,
		Bucket:      cfg.Bucket,
		Region:      cfg.Region,
	}
	task.PrivateData.ResultURL = upstreamURL
	logger.LogInfo(ctx, "Task "+task.TaskID+" 成片转存任务已提交")
	return true, nil
}

// PollRelocate 推进转存任务；done=true 表示已到终态。
//
// 终态有两种：
//   · 成功 —— ResultURL 换成 cos-proxy:// 占位符，取片时实时签名；
//   · 失败/超时 —— ResultURL 保留上游原片，任务照常 SUCCESS。
func PollRelocate(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	pp := task.PrivateData.PostProcess
	if pp.Stage == postProcessStageSuccess || pp.Stage == postProcessStageFailed {
		return true, nil
	}

	cfg := tencentmps_setting.Get()

	// 超时回落：转存服务卡住时不能让用户的任务永远挂在「处理中」。
	if pp.SubmittedAt > 0 && time.Now().Unix()-pp.SubmittedAt > int64(relocateMaxWait/time.Second) {
		pp.Stage = postProcessStageFailed
		pp.Error = "relocate timed out"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	// 配置被摘掉时无法再查状态，直接回落原片。
	if !tencentmps_setting.IsConfigured() {
		pp.Stage = postProcessStageFailed
		pp.Error = "relocate configuration is no longer available"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	client := newMPSClient(cfg.SecretId, cfg.SecretKey, cfg.Region)
	result, err := client.describeRelocate(ctx, pp.TaskID)
	if err != nil {
		return false, err
	}

	switch strings.ToUpper(strings.TrimSpace(result.Status)) {
	case "FINISH", "SUCCESS":
		if result.ObjectPath == "" {
			// 状态是完成但没拿到产物路径 —— 当作失败回落，避免把空地址写进 ResultURL。
			pp.Stage = postProcessStageFailed
			pp.Error = "relocate finished without output path"
			task.PrivateData.ResultURL = pp.SourceURL
			return true, nil
		}
		pp.Stage = postProcessStageSuccess
		pp.OutputPath = result.ObjectPath
		task.PrivateData.ResultURL = BuildCOSProxyMarker(pp.Bucket, pp.Region, pp.OutputPath)
		return true, nil
	case "FAIL", "FAILED":
		pp.Stage = postProcessStageFailed
		pp.Error = result.ErrCode
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	default:
		return false, nil
	}
}

// RelocateObjectName 生成**按任务唯一**的产物文件名（相对 OutputDir）。
//
// 为什么必须唯一：MPS 不指定文件名时拿**输入 URL 的 basename** 命名产物。
// 本渠道的成片地址形如 .../vr/api/pub/xxx.mp4，basename 随上游生成规则变化，
// 一旦撞名就会静默覆盖 —— 后完成任务把先完成的片子覆盖掉，
// 而且**不报错**，用户拿到的是别人的片子。
//
// hint 传任务的公开 ID（task_xxx）。为空或含非法字符时退化成时间戳，
// 保证「至少不会互相覆盖」，不追求可读性。
func RelocateObjectName(hint string) string {
	safe := sanitizeObjectName(hint)
	if safe == "" {
		return fmt.Sprintf("relocate_%d.mp4", time.Now().UnixNano())
	}
	return safe + ".mp4"
}

// sanitizeObjectName 把任务 ID 里的非法字符替换成下划线。
//
// COS 对象名允许的字符比 URL 宽松，但为了跨存储兼容，这里只保留
// 字母/数字/下划线/连字符 —— 与 superResObjectName 同一套规则。
func sanitizeObjectName(hint string) string {
	cleaned := make([]rune, 0, len(hint))
	for _, r := range hint {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			cleaned = append(cleaned, r)
		default:
			cleaned = append(cleaned, '_')
		}
	}
	name := strings.Trim(string(cleaned), "_")
	// 留出 "relocate_" 前缀与 ".mp4" 后缀的余量，控制在 64 字符内。
	if len(name) > 48 {
		name = name[:48]
	}
	return name
}
