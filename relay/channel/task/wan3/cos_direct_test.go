package wan3

import (
	"bytes"
	"context"
	"hash/crc64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/cosdirect_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// COS 直存转存（拉片 → 内网 PUT → cos-proxy:// 占位符）。
//
// 与 MPS 转存的区别：不经 MPS 转码，字节级无损。
// 失败一律回落上游原片，任务不失败。
// ─────────────────────────────────────────────────────────────────────────────

// setupDirect 配置 cosdirect_setting（全量 JSON，避免部分更新语义干扰）。
func setupDirect(t *testing.T, cfg cosdirect_setting.Config) {
	t.Helper()
	b := &strings.Builder{}
	b.WriteString(`{"enabled":`)
	b.WriteString(strconv.FormatBool(cfg.Enabled))
	b.WriteString(`,"secret_id":` + strconv.Quote(cfg.SecretId))
	b.WriteString(`,"secret_key":` + strconv.Quote(cfg.SecretKey))
	b.WriteString(`,"region":` + strconv.Quote(cfg.Region))
	b.WriteString(`,"bucket":` + strconv.Quote(cfg.Bucket))
	b.WriteString(`,"output_dir":` + strconv.Quote(cfg.OutputDir))
	b.WriteString(`,"max_bytes":` + strconv.FormatInt(cfg.MaxBytes, 10))
	b.WriteString(`,"timeout_sec":` + strconv.Itoa(cfg.TimeoutSec))
	b.WriteString(`}`)
	require.NoError(t, cosdirect_setting.UpdateFromJSONString(b.String()))
	t.Cleanup(func() {
		require.NoError(t, cosdirect_setting.UpdateFromJSONString("{}"))
	})
}

// newTask 构造一个处于转存 submitted 阶段的任务。
func newTask(stage, provider, sourceURL string) *model.Task {
	task := &model.Task{
		TaskID:    "task_utCOSDirect",
		ChannelId: 17,
		Status:    "IN_PROGRESS",
		PrivateData: model.TaskPrivateData{
			ResultURL: sourceURL,
		},
	}
	if stage != "" {
		task.PrivateData.PostProcess = &model.TaskPostProcess{
			Stage:       stage,
			Provider:    provider,
			SourceURL:   sourceURL,
			SubmittedAt: time.Now().Unix(),
			// 与 setupDirect 常用配置一致：成功后 marker 断言直接用这组值。
			Bucket: "hbd-video-1256715892",
			Region: "ap-guangzhou",
		}
	}
	return task
}

func TestSubmitDirectRelocateNotConfigured(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{Enabled: false})
	task := newTask("", "", "https://upstream.example/video.mp4")

	handled, err := SubmitDirectRelocate(context.Background(), task, task.PrivateData.ResultURL)
	require.NoError(t, err)
	assert.False(t, handled, "未配置必须不接管，行为与改造前一致")
	assert.Nil(t, task.PrivateData.PostProcess)
}

func TestSubmitDirectRelocateEmptyURL(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	task := newTask("", "", "")

	handled, err := SubmitDirectRelocate(context.Background(), task, "")
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, task.PrivateData.PostProcess)
}

func TestSubmitDirectRelocateTakesOver(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	task := newTask("", "", "https://upstream.example/video.mp4")

	handled, err := SubmitDirectRelocate(context.Background(), task, task.PrivateData.ResultURL)
	require.NoError(t, err)
	assert.True(t, handled)
	require.NotNil(t, task.PrivateData.PostProcess)
	pp := task.PrivateData.PostProcess
	assert.Equal(t, postProcessStageSubmitted, pp.Stage)
	assert.Equal(t, cosDirectProvider, pp.Provider)
	assert.Equal(t, "https://upstream.example/video.mp4", pp.SourceURL)
	assert.Equal(t, "hbd-video-1256715892", pp.Bucket)
	// 接管期间 ResultURL 先保持上游原片：转存失败回落时用户仍拿得到片。
	assert.Equal(t, "https://upstream.example/video.mp4", task.PrivateData.ResultURL)
}

// fakeCOS 启一个 httptest server 模拟 COS PUT（校验路径与 body），返回桶 URL。
//
// 两个仿真点（缺一个 SDK 都会判失败）：
//   · SDK 把对象键拼在桶 URL 后，r.URL.Path 形如 "/"+key（带前导斜杠）；
//   · COS SDK 上传后校验响应头 x-cos-hash-crc64ecma，真 COS 会返回该头。
func fakeCOS(t *testing.T, expectedKey string, body *bytes.Buffer) *url.URL {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if expectedKey != "" && r.URL.Path != "/"+expectedKey {
			http.Error(w, "path mismatch: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		n, _ := io.Copy(body, r.Body)
		bodyLen.Store(n)
		w.Header().Set("x-cos-hash-crc64ecma",
			strconv.FormatUint(crc64.Checksum(body.Bytes(), crc64.MakeTable(crc64.ECMA)), 10))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	u, err := url.Parse(srv.URL)
	require.NoError(t, err)
	return u
}

var bodyLen atomic.Int64

func TestPollDirectRelocateSuccess(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
		OutputDir: "/videos/",
	})

	content := bytes.Repeat([]byte("FAKE-VIDEO"), 1024)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(content)
	}))
	t.Cleanup(upstream.Close)

	cosBody := &bytes.Buffer{}
	cosURL := fakeCOS(t, "videos/task_utCOSDirect.mp4", cosBody)
	cosBucketURL.Store(cosURL)
	t.Cleanup(func() { cosBucketURL.Store(nil) })

	task := newTask(postProcessStageSubmitted, cosDirectProvider, upstream.URL+"/video.mp4")

	done, err := PollDirectRelocate(context.Background(), task)
	require.NoError(t, err)
	assert.True(t, done)

	pp := task.PrivateData.PostProcess
	assert.Equal(t, postProcessStageSuccess, pp.Stage)
	assert.Equal(t, int64(len(content)), bodyLen.Load(), "COS 收到的字节必须与上游一致（无损转存）")
	assert.Equal(t, "cos-proxy://hbd-video-1256715892/ap-guangzhou/videos/task_utCOSDirect.mp4", task.PrivateData.ResultURL)
}

func TestPollDirectRelocateUpstream404FallsBack(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	t.Cleanup(upstream.Close)

	cosBucketURL.Store(nil) // 不应触达 COS
	source := upstream.URL + "/missing.mp4"
	task := newTask(postProcessStageSubmitted, cosDirectProvider, source)

	done, err := PollDirectRelocate(context.Background(), task)
	require.NoError(t, err)
	assert.True(t, done, "失败也是终态：回落原片，任务不挂起")

	pp := task.PrivateData.PostProcess
	assert.Equal(t, postProcessStageFailed, pp.Stage)
	assert.Equal(t, source, task.PrivateData.ResultURL)
}

func TestPollDirectRelocateOversizeFallsBack(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
		MaxBytes: 1024,
	})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2048))
	}))
	t.Cleanup(upstream.Close)

	// 上传目标也用假 endpoint：超限判定发生在「上传后还有剩余字节」，
	// 所以 PUT 会成功，判定靠剩余字节，不能打真 COS。
	cosBody := &bytes.Buffer{}
	cosURL := fakeCOS(t, "", cosBody)
	cosBucketURL.Store(cosURL)
	t.Cleanup(func() { cosBucketURL.Store(nil) })

	task := newTask(postProcessStageSubmitted, cosDirectProvider, upstream.URL+"/big.mp4")

	done, err := PollDirectRelocate(context.Background(), task)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, postProcessStageFailed, task.PrivateData.PostProcess.Stage)
	assert.Equal(t, task.PrivateData.PostProcess.SourceURL, task.PrivateData.ResultURL)
}

func TestPollDirectRelocateTerminalStageIsNoop(t *testing.T) {
	task := newTask(postProcessStageSuccess, cosDirectProvider, "https://up.example/a.mp4")
	task.PrivateData.PostProcess.OutputPath = "/videos/a.mp4"
	task.PrivateData.ResultURL = "cos-proxy://b/r/videos/a.mp4"

	done, err := PollDirectRelocate(context.Background(), task)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, "cos-proxy://b/r/videos/a.mp4", task.PrivateData.ResultURL, "终态不得被改写")
}

func TestPollDirectRelocateTimeoutFallsBack(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	task := newTask(postProcessStageSubmitted, cosDirectProvider, "https://slow.example/a.mp4")
	task.PrivateData.PostProcess.SubmittedAt = time.Now().Unix() - 3600

	done, err := PollDirectRelocate(context.Background(), task)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, postProcessStageFailed, task.PrivateData.PostProcess.Stage)
	assert.Equal(t, "https://slow.example/a.mp4", task.PrivateData.ResultURL)
}

// ── adaptor 接线：AfterUpstreamSuccess / PollPostProcess 分派 ────────────────

func TestAdaptorAfterUpstreamSuccessTakesDirectRelocate(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_utWireDirect",
		Properties: model.Properties{OriginModelName: "wan3.0-video"},
	}

	handled, err := a.AfterUpstreamSuccess(context.Background(), task, "https://up.example/v.mp4")
	require.NoError(t, err)
	assert.True(t, handled, "配置了直存且模型不需要超分：adaptor 必须接管")
	require.NotNil(t, task.PrivateData.PostProcess)
	assert.Equal(t, cosDirectProvider, task.PrivateData.PostProcess.Provider)
	assert.Equal(t, "https://up.example/v.mp4", task.PrivateData.ResultURL)
}

func TestAdaptorAfterUpstreamSuccessKeepsSuperResolutionPath(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "ak", SecretKey: "sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID:     "task_utWireSR",
		Properties: model.Properties{OriginModelName: Official720PModelName},
	}

	// 720P 特惠承诺的是画质提升：MPS 未配置时超分链路拒绝接管，
	// 直存也不得抢走这条语义（回落 480P 原片，维持改造前行为）。
	handled, err := a.AfterUpstreamSuccess(context.Background(), task, "https://up.example/v.mp4")
	require.NoError(t, err)
	assert.False(t, handled)
	assert.Nil(t, task.PrivateData.PostProcess)
}

func TestAdaptorPollPostProcessDispatchesByProvider(t *testing.T) {
	a := &TaskAdaptor{}

	// cos-direct 提供者：模型名不是超分目标也要走直存轮询（超时回落）。
	direct := &model.Task{
		TaskID:     "task_utWirePollDirect",
		Properties: model.Properties{OriginModelName: "wan3.0-video"},
	}
	direct.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:       postProcessStageSubmitted,
		Provider:    cosDirectProvider,
		SourceURL:   "https://up.example/v.mp4",
		SubmittedAt: time.Now().Unix() - 100000,
	}
	done, err := a.PollPostProcess(context.Background(), direct)
	require.NoError(t, err)
	assert.True(t, done)
	assert.Equal(t, postProcessStageFailed, direct.PrivateData.PostProcess.Stage)
	assert.Equal(t, "https://up.example/v.mp4", direct.PrivateData.ResultURL)

	// MPS 超分提供者：维持原分派，终态立即收敛且不打腾讯云。
	superRes := &model.Task{
		TaskID:     "task_utWirePollSR",
		Properties: model.Properties{OriginModelName: Official720PModelName},
	}
	superRes.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:      postProcessStageSuccess,
		Provider:   postProcessProvider,
		OutputPath: "mps_api_test/out/x.mp4",
		Bucket:     "b", Region: "r",
	}
	done, err = a.PollPostProcess(context.Background(), superRes)
	require.NoError(t, err)
	assert.True(t, done)

	// 历史/异常数据：Provider 为空但模型名是超分目标 → 仍走超分轮询。
	legacy := &model.Task{
		TaskID:     "task_utWirePollLegacy",
		Properties: model.Properties{OriginModelName: Official720PModelName},
	}
	legacy.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:      postProcessStageSuccess,
		OutputPath: "mps_api_test/out/y.mp4",
		Bucket:     "b", Region: "r",
	}
	done, err = a.PollPostProcess(context.Background(), legacy)
	require.NoError(t, err)
	assert.True(t, done)
}

// ── presign 凭据按桶匹配 ─────────────────────────────────────────────────────

func TestPresignCOSURLOverridesWithDirectCreds(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "direct-ak", SecretKey: "direct-sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	// 未配置 MPS 凭据：只有 cosdirect 凭据可用时也必须能签名（新桶场景）。
	signed, err := PresignCOSURL(context.Background(), "hbd-video-1256715892", "ap-guangzhou", "/videos/a.mp4")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(signed, "https://hbd-video-1256715892.cos.ap-guangzhou.myqcloud.com/"), signed)
	assert.Contains(t, signed, "q-sign-algorithm=sha1")
}

func TestPresignCOSURLRejectsUnmatchedBucketWithoutMPSCreds(t *testing.T) {
	setupDirect(t, cosdirect_setting.Config{
		Enabled: true, SecretId: "direct-ak", SecretKey: "direct-sk",
		Region: "ap-guangzhou", Bucket: "hbd-video-1256715892",
	})
	// 桶不匹配且无 MPS 凭据：必须报错而不是用错凭据签名。
	_, err := PresignCOSURL(context.Background(), "mps-bucket", "ap-guangzhou", "/x.mp4")
	require.Error(t, err)
}
