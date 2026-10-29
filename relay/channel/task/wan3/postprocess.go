package wan3

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
)

// ─────────────────────────────────────────────────────────────────────────────
// 成片后处理：官网 480P 成片 → 云端增强 → 720P → 回给用户。
//
// 这是一条**两跳**链路：
//
//	第一跳：官网 wan3.0-video 生成 480P 成片（OSS 签名直链）
//	第二跳：把成片交给腾讯云 MPS 做 2 倍超分，产物落 COS
//	回源：客户端从 COS 取产物（本站签临时链接后 302 直连，字节不经本机出口）
//
// 为什么后处理失败**不判任务失败**：
//	用户已经为「720P」付了钱，但拿不到 720P 就是我们的问题；可如果直接判失败退款，
//	第一跳的钱退不回来、第二跳的钱白花，账目更乱。所以：超分失败 → 回落成 480P 原片，
//	任务照常 SUCCESS。**退款与否由计费层决定，不在这里决定。**
//
// ⚠️ 这里的实现细节**不对外暴露**：模型卡片、教程文档、错误文案里一律不写
//	「超分 / 增强 / MPS / 腾讯云」等内部词，用户只需要知道「这个模型出 720P」。
// ─────────────────────────────────────────────────────────────────────────────

// postProcessProvider 标识后处理服务。
const postProcessProvider = "tencent-mps"

// 后处理阶段常量。
const (
	postProcessStageSubmitted = "submitted"
	postProcessStageSuccess   = "success"
	postProcessStageFailed    = "failed"
)

// mpsMaxWait 单个任务后处理的最长等待时间。
//
// 超时后回落原片 —— 不能让用户的任务永远挂在「处理中」。
const mpsMaxWait = 20 * time.Minute

// needsSuperResolution 判断这个对外模型名是否需要走 480P→720P 的后处理链路。
//
// 只认 Official720PModelName 这一个名字：
//   · wan3.0-video-官网 是原生的 720P（上游直接出 720P），不走后处理；
//   · 标准版 wan3.0-video 走自己的分辨率档，也不走后处理。
func needsSuperResolution(originModelName string) bool {
	return strings.TrimSpace(originModelName) == Official720PModelName
}

// submitPostProcess 在第一跳成功后提交超分任务。
//
// resolution 是**目标输出档位**（不是输入档位）：输入恒为 480P（见
// official720PGenerationResolution），输出恒为 720P（见 official720POutputResolution）。
//
// 返回值：
//   - pp != nil 且 err == nil：已提交，pp 里有后处理任务 ID，轮询时继续跟进；
//   - pp == nil：不需要后处理（或配置不可用），调用方直接按原流程置成功。
func submitPostProcess(ctx context.Context, sourceURL string, resolution string) (*model.TaskPostProcess, error) {
	cfg := tencentmps_setting.Get()
	if !tencentmps_setting.IsConfigured() {
		// 没配 MPS 就不做后处理 —— 行为与改造前完全一致。
		return nil, nil
	}
	if strings.TrimSpace(sourceURL) == "" {
		return nil, fmt.Errorf("empty source url")
	}

	// 按目标档位推算输出尺寸（超分 Size 只支持 2 倍：480P → 720P）。
	width, height := superResOutputSize(resolution)
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("unsupported resolution %q for super resolution", resolution)
	}
	// 配置里显式指定了尺寸就以配置为准（同样是长边/短边）。
	if cfg.OutputWidth > 0 && cfg.OutputHeight > 0 {
		width, height = cfg.OutputWidth, cfg.OutputHeight
	}

	client := newMPSClient(cfg.SecretId, cfg.SecretKey, cfg.Region)
	sessionID := fmt.Sprintf("%s-%d", cfg.SessionIdPrefix, time.Now().UnixNano())

	taskID, err := client.submitSuperRes(ctx, mpsSuperResRequest{
		InputURL:             sourceURL,
		Bucket:               cfg.Bucket,
		Region:               cfg.Region,
		OutputDir:            cfg.OutputDir,
		Width:                width,
		Height:               height,
		BitrateKbps:          cfg.OutputBitrateKbps,
		SuperResolutionType: cfg.SuperResolutionType,
		SessionID:            sessionID,
	})
	if err != nil {
		return nil, err
	}

	return &model.TaskPostProcess{
		Stage:       postProcessStageSubmitted,
		Provider:    postProcessProvider,
		TaskID:      taskID,
		SubmittedAt: time.Now().Unix(),
		SourceURL:   sourceURL,
		Bucket:      cfg.Bucket,
		Region:      cfg.Region,
	}, nil
}

// superResOutputSize 按**目标输出档位**推算输出尺寸。
//
// 返回的是 **(长边, 短边)** —— 因为 MPS 的 ResolutionAdaptive=open 语义是
// 「Width 代表长边，Height 表示短边」（SDK 原文），不是宽×高。
//
// ⚠️ 必须用长边/短边而不是固定宽高：官网成片的画面比例随请求的 ratio 变
// （16:9 / 9:16 / 1:1 / 4:3 …），固定宽高 + FillType=stretch 会把
// 竖屏片压成胖画面。用长边/短边 + FillType=black，任何比例都正确：
// 长边短边都按目标档位给，超出部分留黑边。
func superResOutputSize(resolution string) (int, int) {
	switch strings.ToUpper(strings.TrimSpace(resolution)) {
	case "480P", "480":
		// 目标 480P 档：长边 854、短边 480。
		return 854, 480
	case "720P", "720":
		// 目标 720P 档：长边 1280、短边 720。
		return 1280, 720
	case "1080P", "1080":
		// 目标 1080P 档：长边 1920、短边 1080。
		return 1920, 1080
	default:
		return 0, 0
	}
}

// pollPostProcess 轮询后处理任务，把结果写回 task。
//
// 返回值：
//   - done == true：后处理已到终态（成功或失败回落），task 的 ResultURL 已就绪；
//   - done == false：还在处理中，调用方保持任务为「处理中」状态；
//   - err != nil：轮询本身出错（网络等），调用方保持现状、下一轮再试。
//
// ⚠️ 失败时**不返回错误**，而是把 Stage 置为 failed 并保留原片地址 ——
//	任务照常成功，用户拿到 480P，而不是一个失败的任务。
func pollPostProcess(ctx context.Context, task *model.Task) (bool, error) {
	pp := task.PrivateData.PostProcess
	if pp == nil {
		return true, nil
	}
	if pp.Stage == postProcessStageSuccess || pp.Stage == postProcessStageFailed {
		return true, nil
	}

	cfg := tencentmps_setting.Get()

	// 超时回落：后处理服务卡住时不能让用户的任务永远挂在「处理中」。
	//
	// 到点就当失败处理 —— 回落第一跳原片，任务照常 SUCCESS。
	// 用户拿到的是 480P 而不是拿不到片，这是可接受的降级。
	if pp.SubmittedAt > 0 && time.Now().Unix()-pp.SubmittedAt > int64(mpsMaxWait/time.Second) {
		pp.Stage = postProcessStageFailed
		pp.Error = "post process timed out"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	// 配置被摘掉（enabled=false 或密钥清空）时无法再查后处理状态。
	// 直接回落原片 —— 继续轮询只会一直失败，任务永远停在「处理中」。
	if !tencentmps_setting.IsConfigured() {
		pp.Stage = postProcessStageFailed
		pp.Error = "post process configuration is no longer available"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	client := newMPSClient(cfg.SecretId, cfg.SecretKey, cfg.Region)

	result, err := client.describeSuperRes(ctx, pp.TaskID)
	if err != nil {
		return false, err
	}

	switch strings.ToUpper(strings.TrimSpace(result.Status)) {
	case "FINISH", "SUCCESS":
		if result.ObjectPath == "" {
			// 状态是完成但没拿到产物路径 —— 当作失败回落，避免把空地址写进 ResultURL。
			pp.Stage = postProcessStageFailed
			pp.Error = "post process finished without output path"
			task.PrivateData.ResultURL = pp.SourceURL
			return true, nil
		}
		pp.Stage = postProcessStageSuccess
		pp.OutputPath = result.ObjectPath
		// ResultURL 换成「本站代理 + COS 预签名」的形式，由 video_proxy 解析。
		task.PrivateData.ResultURL = BuildCOSProxyMarker(pp.Bucket, pp.Region, pp.OutputPath)
		return true, nil
	case "FAIL", "FAILED":
		pp.Stage = postProcessStageFailed
		pp.Error = result.ErrCode
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	default:
		// 还在处理中。
		return false, nil
	}
}

// BuildCOSProxyMarker 构造一个「本站代理占位符」，供 video_proxy 识别并换成 COS 预签名直链。
//
// 为什么不直接把 COS URL 写进 ResultURL：
//   · COS 预签名 URL 有有效期（默认 1 小时），任务可能几小时后才被下载；
//   · 预签名需要 SecretId/SecretKey，现算现签才能保证用户什么时候来取都能拿到。
//
// 所以 ResultURL 里存一个可解析的标记，取片时再实时签名。
//
// ⚠️ 实现在 taskcommon（对外出口也要识别这个标记），这里只做转发。
func BuildCOSProxyMarker(bucket, region, objectPath string) string {
	return taskcommon.BuildCOSProxyMarker(bucket, region, objectPath)
}

// ParseCOSProxyMarker 解析占位符，返回（bucket, region, objectPath, ok）。
func ParseCOSProxyMarker(raw string) (string, string, string, bool) {
	return taskcommon.ParseCOSProxyMarker(raw)
}

// LogPostProcessFallback 记录后处理回落原片的日志。
//
// ⚠️ 日志里写「成片增强」而不是具体服务名 —— 避免把内部实现暴露到用户可见的日志里。
func LogPostProcessFallback(ctx context.Context, taskID string, reason string) {
	logger.LogWarn(ctx, fmt.Sprintf(
		"Task %s 成片增强未生效，回落到原始画质: %s", taskID, reason))
}
