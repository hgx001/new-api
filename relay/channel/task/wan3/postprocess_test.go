package wan3

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/setting/tencentmps_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// wan3.0-720p-特惠（两跳链路：官网 480P → 云端提升 → 交付 720P）
//
// 这些用例锁死三件最容易出错的事：
//  1. 向上游只请求 480P（否则两跳链路形同虚设，用户多花一倍生成费）；
//  2. 计费倍率恒为 1.0（漏判会落进 1/2/4 表，用户传 720P 时价格翻倍）；
//  3. 对外文案不含「超分 / 增强 / 腾讯云」等内部实现词。
// ─────────────────────────────────────────────────────────────────────────────

// TestOfficial720PRequests480PFromUpstream 锁定「上游只生成 480P」。
//
// 为什么必须锁：两跳链路的成本优势来自「用便宜的 480P 生成再提升」；
// 如果哪次改动让上游直接生成 720P，生成成本会从 ¥0.20/秒涨到 ¥0.45/秒，
// 而售价还是 ¥0.25/秒 —— 直接变成亏本卖，且用户拿到的画面并无差别。
func TestOfficial720PRequests480PFromUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: Official720PModelName,
	}
	adaptor.Init(info)

	// 用户传 720P / 1080P / 480P，上游拿到的都必须是 480P。
	for _, requested := range []string{"720P", "1080P", "480P", ""} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Prompt:    "一只橘猫",
			Duration:  5,
			Resolution: requested,
		})

		body, err := adaptor.BuildRequestBody(context, info)
		require.NoError(t, err)
		encoded, err := io.ReadAll(body)
		require.NoError(t, err)

		var payload struct {
			Parameters struct {
				Resolution string `json:"resolution"`
			} `json:"parameters"`
		}
		require.NoError(t, json.Unmarshal(encoded, &payload))
		require.Equal(t, official720PGenerationResolution, payload.Parameters.Resolution,
			"requested=%q: upstream must always generate 480P", requested)
	}
}

// TestOfficial720PLegacyNameStillUsesTwoHopChain 锁定旧名（wan3.0-720p-官网，
// 2026-10-30 改名前的公开名）**仍然走两跳链路**。
//
// ⚠️ 这是改名时最容易漏的一步：如果判定函数只认新名，旧名调用方会
//	静默落到单跳链路 —— 上游直接生成用户请求的分辨率，
//	结果可能是**用户买了 720P 拿到 480P，而且照样按 720P 档扣费**。
//	这类缺陷不报错，只是货不对，比崩溃更难发现。
func TestOfficial720PLegacyNameStillUsesTwoHopChain(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// ① 强制 480P 生成
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{
		ChannelMeta:     &relaycommon.ChannelMeta{},
		OriginModelName: Official720PModelNameLegacy,
	}
	adaptor.Init(info)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:     "一只橘猫",
		Duration:   5,
		Resolution: "1080P",
	})
	body, err := adaptor.BuildRequestBody(context, info)
	require.NoError(t, err)
	encoded, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload struct {
		Parameters struct {
			Resolution string `json:"resolution"`
		} `json:"parameters"`
	}
	require.NoError(t, json.Unmarshal(encoded, &payload))
	require.Equal(t, official720PGenerationResolution, payload.Parameters.Resolution,
		"旧名也必须强制上游生成 480P（否则用户付 720P 的钱拿 480P 的片）")

	// ② 走成片提升
	require.True(t, needsSuperResolution(Official720PModelNameLegacy),
		"旧名必须仍然走成片提升链路")

	// ③ 计费倍率恒为 1.0（落进 1/2/4 表会翻倍）
	require.Equal(t, 1.0, resolutionSizeRatioFor("720P", info),
		"旧名的计费倍率也必须恒为 1.0")

	// ④ 向上游请求的模型名仍是标准版
	require.Equal(t, upstreamModel, upstreamModelFor(Official720PModelNameLegacy),
		"旧名向上游也必须兜底成标准版 wan3.0-video")
}

// TestOfficial720PGenerationResolutionOnlyAppliesToItsOwnModelName 确认
// 「强制 480P」只作用于 wan3.0-720p-特惠，不影响官网原生 720P/1080P 与标准版。
func TestOfficial720PGenerationResolutionOnlyAppliesToItsOwnModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	for _, modelName := range []string{OfficialModelName, "wan3.0-video", "wan3.0-video-prime"} {
		info := &relaycommon.RelayInfo{
			ChannelMeta:     &relaycommon.ChannelMeta{},
			OriginModelName: modelName,
		}
		adaptor.Init(info)
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Prompt:     "一只橘猫",
			Duration:   5,
			Resolution: "1080P",
		})

		body, err := adaptor.BuildRequestBody(context, info)
		require.NoError(t, err)
		encoded, err := io.ReadAll(body)
		require.NoError(t, err)

		var payload struct {
			Parameters struct {
				Resolution string `json:"resolution"`
			} `json:"parameters"`
		}
		require.NoError(t, json.Unmarshal(encoded, &payload))
		require.Equal(t, "1080P", payload.Parameters.Resolution,
			"%s must keep its own resolution tiers", modelName)
	}
}

// TestOfficial720PPriceRatioIsAlwaysOne 锁定分辨率倍率恒为 1.0。
//
// 回归背景：wan3.0-720p-特惠 不以 "wan3.0-video" 开头，早期实现会落进
// standardResolutionSizeRatio（1/2/4），用户传 720P 时算出 2.0 倍率 ——
// 价格从 ¥0.25/秒直接翻到 ¥0.50/秒。
func TestOfficial720PPriceRatioIsAlwaysOne(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 5})

	info := &relaycommon.RelayInfo{OriginModelName: Official720PModelName}
	for _, resolution := range []string{"480P", "720P", "1080P", "未知"} {
		context.Set("task_request", relaycommon.TaskSubmitReq{
			Duration:   5,
			Resolution: resolution,
		})
		ratios := adaptor.EstimateBilling(context, info)
		require.Equal(t, 1.0, ratios["size"],
			"resolution=%q must not apply a resolution multiplier", resolution)
		require.Equal(t, 5.0, ratios["seconds"])
	}
}

// TestOfficial720PPerSecondPrice 锁定对外价 ¥0.20/秒。
//
// 口径：quota = int(ModelPrice × QuotaPerUnit) × seconds × size。
// 10 秒 = ¥2.00；带 10 秒参考视频（秒数并入）= ¥4.00。
//
// ⚠️ 2026-10-30 由 ¥0.25/秒下调到 ¥0.20/秒（老板拍板，走「特惠」定位）。
// 改价时这一条和 setting/ratio_setting/model_ratio.go 的默认值、
// 生产 options.ModelPrice 的键值**必须三处一起改**，否则会出现
// 「广场显示一个价、实际扣另一个价」。
func TestOfficial720PPerSecondPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ratio_setting.InitRatioSettings()

	price, ok := ratio_setting.GetModelPrice(Official720PModelName, false)
	require.True(t, ok, "wan3.0-720p-特惠 must have an explicit per-second price")
	require.InDelta(t, 0.2/ratio_setting.USD2RMB, price, 1e-9, "must equal ¥0.20/sec in USD")

	// 旧名过渡期仍可调用，且**必须同价** —— 改名前后同货不同价会让
	// 已按旧价谈好的客户觉得被涨价。
	legacyPrice, ok := ratio_setting.GetModelPrice(Official720PModelNameLegacy, false)
	require.True(t, ok, "旧名 wan3.0-720p-官网 must still have a price（否则算不出价=静默免单）")
	require.InDelta(t, price, legacyPrice, 1e-9, "旧名与新名必须同价")

	baseQuota := int(price * common.QuotaPerUnit)
	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: Official720PModelName}

	cases := []struct {
		name       string
		refSeconds int
		wantCNY    float64
	}{
		{"10s", 0, 2.00},
		{"10s + 10s reference video", 10, 4.00},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Set("task_request", relaycommon.TaskSubmitReq{
				Duration:   10,
				Resolution: "720P",
			})
			if tc.refSeconds > 0 {
				context.Set(refVideoSecondsContextKey, tc.refSeconds)
			}

			ratios := adaptor.EstimateBilling(context, info)
			multiplier := 1.0
			for _, ratio := range ratios {
				multiplier *= ratio
			}
			quota := int(float64(baseQuota) * multiplier)
			cny := float64(quota) / common.QuotaPerUnit * ratio_setting.USD2RMB
			require.InDelta(t, tc.wantCNY, cny, 0.005, "charged CNY")
		})
	}
}

// TestSuperResOutputSizeReturnsLongAndShortEdges 锁定输出尺寸语义。
//
// MPS 的 ResolutionAdaptive=open 语义是「Width 代表长边，Height 表示短边」
// （SDK 原文），不是宽×高。写反了会把竖屏片压扁。
func TestSuperResOutputSizeReturnsLongAndShortEdges(t *testing.T) {
	long, short := superResOutputSize(official720POutputResolution)
	require.Equal(t, 1280, long, "720P long edge")
	require.Equal(t, 720, short, "720P short edge")

	long, short = superResOutputSize("1080P")
	require.Equal(t, 1920, long)
	require.Equal(t, 1080, short)

	// 未知档位必须返回 0/0，调用方据此报错而不是提交一个畸形的任务。
	long, short = superResOutputSize("4K")
	require.Equal(t, 0, long)
	require.Equal(t, 0, short)
}

// TestNeedsSuperResolutionOnlyForItsOwnModelName 确认后处理只对目标模型名生效。
func TestNeedsSuperResolutionOnlyForItsOwnModelName(t *testing.T) {
	require.True(t, needsSuperResolution(Official720PModelName))
	require.False(t, needsSuperResolution(OfficialModelName))
	require.False(t, needsSuperResolution("wan3.0-video"))
	require.False(t, needsSuperResolution(""))
}

// TestCOSProxyMarkerRoundTrip 锁定 COS 产物占位符的往返解析。
//
// 占位符存在的原因：COS 预签名 URL 有有效期，任务可能几小时后才被下载，
// 所以 ResultURL 存可解析的标记，取片时再实时签名。
func TestCOSProxyMarkerRoundTrip(t *testing.T) {
	marker := BuildCOSProxyMarker("my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task-1.mp4")
	require.Equal(t, "cos-proxy://my-bucket-1250000000/ap-guangzhou/mps_api_test/out/task-1.mp4", marker)

	bucket, region, objectPath, ok := ParseCOSProxyMarker(marker)
	require.True(t, ok)
	require.Equal(t, "my-bucket-1250000000", bucket)
	require.Equal(t, "ap-guangzhou", region)
	require.Equal(t, "mps_api_test/out/task-1.mp4", objectPath)

	// 非占位符（普通上游直链）必须被识别为「不是 COS 产物」。
	_, _, _, ok = ParseCOSProxyMarker("https://example.com/video.mp4")
	require.False(t, ok)
	_, _, _, ok = ParseCOSProxyMarker("cos-proxy://only-two-parts")
	require.False(t, ok)
}

// TestPostProcessStateMachineKeepsTaskAlive 锁定两跳链路的状态机。
//
// 关键点：后处理已到终态时，轮询应**立刻**返回「已完成」而不再打腾讯云
// （终态判断在函数最前面）；失败态的 ResultURL 保留原始成片 ——
// 任务照常成功，用户至少拿到片。
func TestPostProcessStateMachineKeepsTaskAlive(t *testing.T) {
	// 成功终态：不再请求上游，直接收敛。
	success := &model.Task{
		TaskID: "task-1",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	success.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:      postProcessStageSuccess,
		Provider:   postProcessProvider,
		TaskID:     "mps-task-1",
		SourceURL:  "https://example.com/480p.mp4",
		OutputPath: "mps_api_test/out/task-1.mp4",
		Bucket:     "my-bucket-1250000000",
		Region:     "ap-guangzhou",
	}
	success.PrivateData.ResultURL = BuildCOSProxyMarker(
		"my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task-1.mp4")

	done, err := pollPostProcess(context.Background(), success)
	require.NoError(t, err)
	require.True(t, done, "a terminal post-process must converge immediately")
	require.Equal(t,
		"cos-proxy://my-bucket-1250000000/ap-guangzhou/mps_api_test/out/task-1.mp4",
		success.PrivateData.ResultURL,
	)

	// 失败终态：ResultURL 回落到原始成片，任务照常成功（不判失败、不退款）。
	failed := &model.Task{
		TaskID: "task-2",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	failed.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:     postProcessStageFailed,
		SourceURL: "https://example.com/480p.mp4",
		Error:     "upstream failure",
	}
	failed.PrivateData.ResultURL = "https://example.com/480p.mp4"
	done, err = pollPostProcess(context.Background(), failed)
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, "https://example.com/480p.mp4", failed.PrivateData.ResultURL)

	// 没有后处理记录的任务（其它模型名 / 历史数据）直接放行。
	plain := &model.Task{TaskID: "task-3"}
	done, err = pollPostProcess(context.Background(), plain)
	require.NoError(t, err)
	require.True(t, done)
}

// TestPostProcessTimesOutToOriginalClip 锁定超时回落。
//
// 后处理服务卡住时不能让用户的任务永远挂在「处理中」——
// 到点就当失败处理，回落第一跳原片，任务照常 SUCCESS。
func TestPostProcessTimesOutToOriginalClip(t *testing.T) {
	task := &model.Task{
		TaskID: "task-timeout",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:       postProcessStageSubmitted,
		TaskID:      "mps-task-1",
		SubmittedAt: time.Now().Add(-mpsMaxWait - time.Minute).Unix(),
		SourceURL:   "https://example.com/480p.mp4",
	}
	task.PrivateData.ResultURL = "https://example.com/480p.mp4"

	done, err := pollPostProcess(context.Background(), task)
	require.NoError(t, err)
	require.True(t, done, "an expired post process must converge immediately")
	require.Equal(t, postProcessStageFailed, task.PrivateData.PostProcess.Stage)
	require.Equal(t, "https://example.com/480p.mp4", task.PrivateData.ResultURL)
}

// TestPostProcessFallsBackWhenConfigRemoved 锁定「配置被摘掉」时的回落。
//
// 场景：任务提交时 MPS 是配好的（后处理已提交），之后管理员把 enabled 关掉。
// 此时既查不到状态也交不了 —— 必须回落原片，而不是让任务永远停在「处理中」。
func TestPostProcessFallsBackWhenConfigRemoved(t *testing.T) {
	// 先把配置清成「不可用」。
	previous := tencentmps_setting.Get()
	require.NoError(t, tencentmps_setting.UpdateFromJSONString(`{"enabled":false}`))
	defer func() {
		restored, err := common.Marshal(previous)
		require.NoError(t, err)
		require.NoError(t, tencentmps_setting.UpdateFromJSONString(string(restored)))
	}()
	require.False(t, tencentmps_setting.IsConfigured())

	task := &model.Task{
		TaskID: "task-no-config",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.PrivateData.PostProcess = &model.TaskPostProcess{
		Stage:     postProcessStageSubmitted,
		TaskID:    "mps-task-1",
		SourceURL: "https://example.com/480p.mp4",
	}
	task.PrivateData.ResultURL = "https://example.com/480p.mp4"

	done, err := pollPostProcess(context.Background(), task)
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, postProcessStageFailed, task.PrivateData.PostProcess.Stage)
	require.Equal(t, "https://example.com/480p.mp4", task.PrivateData.ResultURL)
}

// TestHMACSHA256UsesMessageFirstArgumentOrder 锁定签名函数的参数顺序。
//
// ⚠️ 腾讯云官方的 hmacsha256 签名是 **message 在前、key 在后**，
// 与 Go 标准库的 hmac.New(key, message) 相反。写反了会一直报
// SignatureDoesNotMatch，而报错信息不提示顺序问题。
//
// 这里用 RFC 4231 的测试向量反查：HMAC-SHA256(key="key", data="The quick brown fox jumps over the lazy dog")
// = f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8。
func TestHMACSHA256UsesMessageFirstArgumentOrder(t *testing.T) {
	expected := "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8"
	got := hex.EncodeToString(hmacSHA256(
		[]byte("The quick brown fox jumps over the lazy dog"),
		[]byte("key"),
	))
	require.Equal(t, expected, got)
}
