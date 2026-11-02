package youzanwan3

// ════════════════════════════════════════════════════════════════════════════
// seedance2.5-活动（按次档）的契约测试
// ════════════════════════════════════════════════════════════════════════════
//
// 母本（seedance2.5-特惠）按秒 ¥0.20/秒、活动版（seedance2.5-活动）按次 ¥4.5/次
// 固定 30 秒。两者**能力完全相同**（同一上游 wan3.0-video-prime、同样的素材上限、
// 同样的路由规则），**只有计费口径不同**。
//
// 本文件守三件事：
//   1. 活动版确实由 prime 执行、确实按次结算；
//   2. 固定 30 秒 / 固定 720P 是 **fail-loud**（传别的值报错，不静默改档）；
//   3. EstimateBilling 返回 nil —— 否则 30 秒会被**乘进** ¥4.5，收成 ¥135。

import (
	"net/http/httptest"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// huoDongModel 是活动版（按次档）的对外模型名。
const huoDongModel = "seedance2.5-活动"

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：模型归属与上游映射
// ────────────────────────────────────────────────────────────────────────────

// TestHuoDong_上游仍是prime 守的是「不能把中文别名发给上游」。
//
// 上游只认 wan3.0-video-prime / wan3.0-video。发别的名字上游直接 404。
// 这条对活动版尤其重要 —— 它是**新加**的别名，最容易漏掉兜底映射。
func TestHuoDong_上游仍是prime(t *testing.T) {
	got, err := upstreamModelFor(huoDongModel)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got,
		"活动版上游必须是 wan3.0-video-prime —— 发中文别名上游会 404")

	// 母本同样如此（回归守卫）。
	got, err = upstreamModelFor(tehuiModel)
	require.NoError(t, err)
	require.Equal(t, "wan3.0-video-prime", got)
}

// TestHuoDong_走prime档位校验 守的是「活动版与母本共用同一套能力上限」。
//
// 素材上限（参考图 10 / 视频 5 / 音频 5）、无首尾帧这些约束在
// validateMedia 里按 isPrimeModel 分支走 —— 漏判就会让上游拒单。
func TestHuoDong_走prime档位校验(t *testing.T) {
	assert.True(t, isPrimeModel(huoDongModel),
		"活动版必须被 isPrimeModel 认成 prime —— 否则素材上限校验走错分支")
	assert.True(t, isPrimeModel(tehuiModel))
	assert.True(t, isPerCallModel(huoDongModel))
	assert.False(t, isPerCallModel(tehuiModel),
		"母本是按秒档，不该被当成按次 —— 否则会少收 ¥0.20×秒数")
}

// TestHuoDong_母本仍是按秒 守的是「新增活动版不能改变母本行为」。
//
// 这是**回归守卫**：本次改动动了 isPrimeModel / isTehuiModel /
// resolutionRatioForModel / EstimateBilling 四处，全都是母本也走的路径。
// 母本的任何行为变化都是回归（会直接影响线上计费）。
func TestHuoDong_母本仍是按秒(t *testing.T) {
	assert.True(t, isTehuiModel(tehuiModel))
	assert.False(t, isPerCallModel(tehuiModel))

	// 母本的分辨率倍率：720P 基准 1.0、1080P 是 1.5 倍（独立定价）。
	assert.Equal(t, 1.0, resolutionRatioForModel(tehuiModel, "720P"))
	assert.Equal(t, 1.5, resolutionRatioForModel(tehuiModel, "1080P"),
		"母本 1080P 倍率是 1.5 —— 变了就是改价事故")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：固定 30 秒 / 固定 720P（fail-loud）
// ────────────────────────────────────────────────────────────────────────────

// TestHuoDong_时长固定30秒 守的是「按次档只有一种时长规格」。
//
// ⚠️ 最危险的表现是「静默吸附」：客户要 5 秒、我们跑 5 秒、却按 30 秒收钱
// （反过来更糟：客户要 30 秒、我们跑 5 秒）。两种都必须用 400 挡住。
func TestHuoDong_时长固定30秒(t *testing.T) {
	// 不传时长 → 30 秒。
	assert.Equal(t, 30, resolveDurationForModel(relaycommon.TaskSubmitReq{Model: huoDongModel}, huoDongModel))
	// 显式传 30 → 30。
	assert.Equal(t, 30, resolveDurationForModel(relaycommon.TaskSubmitReq{Model: huoDongModel, Duration: 30}, huoDongModel))
	// 显式传 30（字符串形态）→ 30。
	assert.Equal(t, 30, resolveDurationForModel(relaycommon.TaskSubmitReq{Model: huoDongModel, Seconds: "30"}, huoDongModel))

	// ⚠️ 传别的值必须 400，不能吸附。
	for _, bad := range []int{5, 10, 15, 29, 31, 60} {
		req := relaycommon.TaskSubmitReq{Model: huoDongModel, Duration: bad}
		taskErr := validatePerCallFixedTier(req)
		require.NotNil(t, taskErr, "时长 %d 秒必须被拒 —— 静默吸附会让客户为不想要的时长付全价", bad)
	}
	// 母本不受影响：5 秒是合法的（按秒档 2—30 秒逐秒可选）。
	require.Nil(t, validatePerCallFixedTier(relaycommon.TaskSubmitReq{Model: tehuiModel, Duration: 5}),
		"母本必须仍然允许 5 秒 —— 按秒档本来就可以逐秒选")
}

// TestHuoDong_分辨率固定720P 守的是「按次只有一档价」。
//
// 1080P 的上游成本是 720P 的 1.33 倍（¥0.20/秒 vs ¥0.15/秒），
// 30 秒 1080P 成本 ¥6.00 > 售价 ¥4.50 —— 直接倒挂。
// 而静默降到 720P 也不行：客户付了 1080P 的钱拿到 720P 的片。
func TestHuoDong_分辨率固定720P(t *testing.T) {
	// 不传 → 720P。
	assert.Equal(t, "720P", resolveResolutionForModel(relaycommon.TaskSubmitReq{Model: huoDongModel}, huoDongModel))
	// 显式传 720P → 720P。
	assert.Equal(t, "720P", resolveResolutionForModel(relaycommon.TaskSubmitReq{Model: huoDongModel, Resolution: "720P"}, huoDongModel))

	// 显式传 1080P / 480P 必须 400。
	for _, bad := range []string{"1080P", "1080p", "480P"} {
		req := relaycommon.TaskSubmitReq{Model: huoDongModel, Resolution: bad}
		taskErr := validatePerCallFixedTier(req)
		require.NotNil(t, taskErr, "分辨率 %s 必须被拒 —— 1080P 成本是 720P 的 1.33 倍，卖同一价就倒挂", bad)
	}
	// 母本不受影响：母本是按秒分档，1080P 合法。
	require.Nil(t, validatePerCallFixedTier(relaycommon.TaskSubmitReq{Model: tehuiModel, Resolution: "1080P"}),
		"母本必须仍然允许 1080P —— 按秒分档定价")
}

// TestHuoDong_错误文案不带模型名 守的是「脱敏后不出现碎词」。
//
// 本模型在脱敏名单里（model/upstream_redact.go），报错里的模型名会被
// 替换成「上游」——「上游-活动」这种碎词客户读不懂。
func TestHuoDong_错误文案不带模型名(t *testing.T) {
	req := relaycommon.TaskSubmitReq{Model: huoDongModel, Duration: 5}
	taskErr := validatePerCallFixedTier(req)
	require.NotNil(t, taskErr)
	assert.NotContains(t, taskErr.Message, huoDongModel,
		"报错文案不能带模型名 —— 脱敏后会变成「上游-活动」这种碎词")
	assert.NotContains(t, taskErr.Message, "seedance",
		"报错文案不能带 seedance 字样 —— 同上")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 3：计费倍率（**最关键**）
// ────────────────────────────────────────────────────────────────────────────

// TestHuoDong_倍率恒为1 守的是「按次档不乘分辨率倍率」。
//
// 按次只有一档价，1080P 那档的 1.33 倍成本已经在「锁 720P」里解决了。
// 若这里还乘倍率，就会出现「¥4.5 × 1.33」这种按次价被放大的错价。
func TestHuoDong_倍率恒为1(t *testing.T) {
	assert.Equal(t, 1.0, resolutionRatioForModel(huoDongModel, "720P"))
	assert.Equal(t, 1.0, resolutionRatioForModel(huoDongModel, "1080P"),
		"按次档的分辨率倍率必须是 1 —— 一口价就是一口价")
}

// TestHuoDong_EstimateBilling返回nil 守的是**最贵的那个错价**。
//
// 按次档的 EstimateBilling 必须返回 nil（= 按次结算，不乘任何倍率）。
//
// 为什么这条最要命：relay/relay_task.go 的 applyTaskOtherRatios 会把
// OtherRatios 里的每个倍率**乘进**预扣额度。若按次档返回 {"seconds": 30}，
// 预扣额度就从 ¥4.5 变成 ¥4.5 × 30 = **¥135** ——
// 客户收到一条 30 秒的片，被扣 ¥135，而且**不报错、不告警**。
//
// 母本必须继续返回 seconds/size（按秒计费）—— 这是回归守卫。
func TestHuoDong_EstimateBilling返回nil(t *testing.T) {
	gin.SetMode(gin.TestMode)
	adaptor := &TaskAdaptor{}

	// 按次档：无论传什么参数，倍率都必须是 nil。
	for _, req := range []relaycommon.TaskSubmitReq{
		{Model: huoDongModel, Duration: 30, Resolution: "720P"},
		{Model: huoDongModel, Duration: 30},
		{Model: huoDongModel, Seconds: "30"},
		{Model: huoDongModel, Duration: 5, Resolution: "720P"},
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Set("task_request", req)
		info := &relaycommon.RelayInfo{OriginModelName: huoDongModel}
		assert.Nil(t, adaptor.EstimateBilling(context, info),
			"按次档的 EstimateBilling 必须返回 nil —— 返回 seconds 会把 30 秒乘进 ¥4.5 变成 ¥135")
	}

	// 母本：必须继续返回 seconds/size（回归守卫）。
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Set("task_request", relaycommon.TaskSubmitReq{Duration: 30, Resolution: "720P"})
	info := &relaycommon.RelayInfo{OriginModelName: tehuiModel}
	require.Equal(t, map[string]float64{"seconds": 30, "size": 1.0},
		adaptor.EstimateBilling(context, info),
		"母本必须继续按秒计费 —— 返回 nil 会让 30 秒的请求只收 ¥0.20")
}

// TestHuoDong_上游模型名单不含别名 守的是「别名不进 ModelList」。
//
// ModelList 必须与上游 /v1/models 逐字一致，否则「同步上游模型」会把别名
// 当成真实上游模型写回渠道配置。
func TestHuoDong_上游模型名单不含别名(t *testing.T) {
	for _, name := range ModelList {
		assert.NotEqual(t, huoDongModel, name,
			"活动版是对外别名，不能写进 ModelList")
		assert.NotEqual(t, tehuiModel, name,
			"母本也是对外别名，不能写进 ModelList")
	}
	// 反证：真实上游模型在名单里。
	assert.Contains(t, ModelList, primeModel)
	assert.Contains(t, ModelList, videoModel)
}
