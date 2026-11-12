package wan3

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/cosdirect_setting"

	"github.com/tencentyun/cos-go-sdk-v5"
)

// ─────────────────────────────────────────────────────────────────────────────
// COS 直存转存 —— 拉片直传，不做任何转码（2026-11-12 新增）。
//
// 为什么不用 MPS 转存（SubmitRelocate）：
//
//	2026-11-12 生产实测：MPS 以「URL 直链转存」模式跑两次（output_bitrate_kbps
//	分别配 5000 / 12000），产物码率都固定在 1.81Mbps（源片 10.75Mbps），
//	40.8MB 压成 7.07MB —— 配置码率无效，画质不可逆损失。
//	新业务走「服务器拉片 → 同地域内网 PUT 到 COS」：字节级无损。
//
// 数据面：
//
//	任务成功 → SubmitDirectRelocate 记 submitted（ResultURL 先保持上游原片）
//	轮询周期 → PollDirectRelocate 拉片 + PUT COS
//	  · 成功：ResultURL = cos-proxy:// 占位符（取片时实时预签名 302）
//	  · 失败/超时：ResultURL = 上游原片，任务照常 SUCCESS —— 不让用户挂起
//
// 与 SubmitRelocate 的关键差异：这里是同步动作（拉片+上传在本轮询周期完成），
// 没有「远端任务 ID」要查；服务侧失败语义与 MPS 链路一致（回落原片）。
// ─────────────────────────────────────────────────────────────────────────────

// cosDirectProvider 标识直存链路（与 tencent-mps-relocate 区分）。
const cosDirectProvider = "cos-direct"

// cosBucketURL 测试注入点：非 nil 时用它做 COS endpoint（单测指向 httptest）。
// 生产代码不要碰它。
var cosBucketURL atomic.Pointer[url.URL]

// SubmitDirectRelocate 接管一次「COS 直存」转存。
//
// 返回 handled=true 表示已接管：service 层保持任务「处理中」，
// 等 PollDirectRelocate 收敛后再置 SUCCESS。
//
// 返回 handled=false 表示「不接管」：未配置直存、源地址为空。
func SubmitDirectRelocate(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil {
		return false, nil
	}
	if !cosdirect_setting.IsConfigured() {
		return false, nil
	}
	upstreamURL = strings.TrimSpace(upstreamURL)
	if upstreamURL == "" {
		return false, nil
	}

	cfg := cosdirect_setting.Get()
	task.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:       postProcessStageSubmitted,
		Provider:    cosDirectProvider,
		SubmittedAt: time.Now().Unix(),
		SourceURL:   upstreamURL,
		Bucket:      cfg.Bucket,
		Region:      cfg.Region,
	}
	// ResultURL 先保持上游原片：转存失败回落时用户仍拿得到片。
	task.PrivateData.ResultURL = upstreamURL
	logger.LogInfo(ctx, "Task "+task.TaskID+" COS 直存转存已接管")
	return true, nil
}

// PollDirectRelocate 推进直存转存；done=true 表示已到终态。
//
// 终态只有两种，与 MPS 链路语义一致：
//   · 成功 —— ResultURL 换成 cos-proxy:// 占位符；
//   · 失败 —— ResultURL 回落上游原片，任务照常 SUCCESS。
func PollDirectRelocate(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	pp := task.PrivateData.PostProcess
	if pp.Stage == postProcessStageSuccess || pp.Stage == postProcessStageFailed {
		return true, nil
	}

	cfg := cosdirect_setting.Get()

	// 超时回落：不能让用户的任务永远挂在「处理中」。
	if pp.SubmittedAt > 0 && time.Now().Unix()-pp.SubmittedAt > int64(cfg.TimeoutSec) {
		pp.Stage = postProcessStageFailed
		pp.Error = "direct relocate timed out"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	// 配置被摘掉时无法再上传，回落原片。
	if !cosdirect_setting.IsConfigured() {
		pp.Stage = postProcessStageFailed
		pp.Error = "direct relocate configuration is no longer available"
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	objectKey, err := buildDirectObjectKey(cfg.OutputDir, RelocateObjectName(task.TaskID))
	if err != nil {
		pp.Stage = postProcessStageFailed
		pp.Error = err.Error()
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	uploadCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutSec)*time.Second)
	defer cancel()
	if err := fetchAndUploadDirect(uploadCtx, cfg, pp.SourceURL, objectKey); err != nil {
		// 失败回落：上游 404/超限/COS 拒绝都算。任务不失败，用户拿原片。
		logger.LogWarn(ctx, fmt.Sprintf("Task %s COS 直存失败，回落上游原片: %s", task.TaskID, err.Error()))
		pp.Stage = postProcessStageFailed
		pp.Error = err.Error()
		task.PrivateData.ResultURL = pp.SourceURL
		return true, nil
	}

	pp.Stage = postProcessStageSuccess
	pp.OutputPath = objectKey
	task.PrivateData.ResultURL = BuildCOSProxyMarker(pp.Bucket, pp.Region, objectKey)
	logger.LogInfo(ctx, "Task "+task.TaskID+" COS 直存完成: "+objectKey)
	return true, nil
}

// buildDirectObjectKey 拼出完整对象键。
//
// COS 对象键规范不带前导斜杠（SDK 会自己拼 "/"），带前导 "/" 会产生
// 双斜杠对象名，且与 presign 的 TrimPrefix 口径不一致。
func buildDirectObjectKey(outputDir, objectName string) (string, error) {
	dir := strings.Trim(strings.TrimSpace(outputDir), "/")
	if dir == "" {
		return "", fmt.Errorf("direct relocate output_dir is empty")
	}
	return dir + "/" + strings.Trim(objectName, "/"), nil
}

// fetchAndUploadDirect 从 sourceURL 拉片并 PUT 到 COS。
//
// 大小保护：读满 MaxBytes+1 即判失败，防止上游异常响应灌爆内存。
func fetchAndUploadDirect(ctx context.Context, cfg cosdirect_setting.Config, sourceURL, objectKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return fmt.Errorf("build fetch request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch upstream video: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upstream video status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, cfg.MaxBytes+1)
	if err := putCOSObject(ctx, cfg, objectKey, limited); err != nil {
		return err
	}

	// 上传管道已经把 MaxBytes+1 读进来了；多出的 1 字节说明源片超限。
	if n, _ := io.Copy(io.Discard, resp.Body); n > 0 {
		return fmt.Errorf("upstream video exceeds max size %d bytes", cfg.MaxBytes)
	}
	return nil
}

// putCOSObject 用官方 SDK 上传（与 cos_presign.go 同一理由：不手写 COS 签名）。
func putCOSObject(ctx context.Context, cfg cosdirect_setting.Config, objectKey string, body io.Reader) error {
	bucketURL := cosBucketURL.Load()
	if bucketURL == nil {
		var err error
		bucketURL, err = cos.NewBucketURL(cfg.Bucket, cfg.Region, true)
		if err != nil {
			return fmt.Errorf("build cos bucket url: %w", err)
		}
	}
	client := cos.NewClient(
		&cos.BaseURL{BucketURL: bucketURL},
		&http.Client{Timeout: 0}, // 超时由 ctx 控制
	)
	_, err := client.Object.Put(ctx, objectKey, body, nil)
	if err != nil {
		return fmt.Errorf("put cos object %s: %w", objectKey, err)
	}
	return nil
}
