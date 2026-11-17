package wan3

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// httptestNewRequest 造一个带 JSON body 的 POST 请求。
func httptestNewRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ─────────────────────────────────────────────────────────────────────────────
// wan3.0-720p-特惠 只支持 9:16（2026-11-17 老板口径）
//
// 背景：该模型是「两跳」链路 —— 官网按 480P 生成，成片再交给云端超分到 720P。
// 超分输出尺寸按**长边/短边**给（1280×720），所以 9:16 的 480P 原片（540×960）
// 会被超分成 720×1280 —— 竖屏成片。而 16:9 的原片（854×480）超分后是 1280×720，
// 横屏成片。也就是说：**同一个模型名，比例不同 → 交付尺寸完全不同**，
// 而对外模型名只有一个「720P」标签，客户无法预期自己会拿到哪一档像素。
//
// 老板要求收窄到单一比例（9:16）：这个模型的真实使用场景是竖屏带货视频
// （生产库 2026-11-14 之前渠道 12 的 21/21 条订单全是 9:16），
// 收窄后「模型名 → 交付尺寸」是一一对应的，不再有歧义。
//
// ⚠️ 为什么不静默改成 9:16：客户传 16:9 是有明确诉求的，
// 拿到 9:16 的片属于**静默降级**（钱按 16:9 付、片是 9:16）。
// 必须 400 明确拒单。
// ─────────────────────────────────────────────────────────────────────────────

// 特惠模型只放行 9:16。
func TestOfficial720PModelAcceptsOnly9x16(t *testing.T) {
	require.True(t, isRatioAllowedForOfficial720P("9:16"), "9:16 必须放行")
	require.False(t, isRatioAllowedForOfficial720P("16:9"), "16:9 必须拒单")
	require.False(t, isRatioAllowedForOfficial720P("1:1"), "1:1 必须拒单")
	require.False(t, isRatioAllowedForOfficial720P("4:3"), "4:3 必须拒单")
	require.False(t, isRatioAllowedForOfficial720P("3:4"), "3:4 必须拒单")
	require.False(t, isRatioAllowedForOfficial720P("adaptive"), "adaptive 必须拒单")
}

// 其它模型（官网原生、标准版）**不受**这条收窄影响 —— 它们仍支持全比例。
func TestRatioRestrictionOnlyAppliesToOfficial720PModel(t *testing.T) {
	require.False(t, isOfficial720PModel(OfficialModelName), "官网原生不是特惠档")
	require.False(t, isOfficial720PModel("wan3.0-video"), "标准版不是特惠档")
	require.True(t, isOfficial720PModel(Official720PModelName))
	require.True(t, isOfficial720PModel(Official720PModelNameLegacy), "旧名同样受限")
}

// 请求 16:9 → 400 拒单，且**不建单不扣费**（校验发生在 validate 阶段）。
func TestOfficial720PModelRejectsNon9x16Request(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptestNewRequest("POST", "/v1/videos", `{"model":"wan3.0-720p-特惠","prompt":"test","metadata":{"ratio":"16:9"}}`)

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: Official720PModelName, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.NotNil(t, taskErr, "16:9 必须被拒")
	require.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	require.Contains(t, taskErr.Message, "9:16", "报错要明确告诉客户只支持 9:16")
}

// 请求 9:16 → 放行，不报错。
func TestOfficial720PModelAccepts9x16Request(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptestNewRequest("POST", "/v1/videos", `{"model":"wan3.0-720p-特惠","prompt":"test","metadata":{"ratio":"9:16"}}`)

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: Official720PModelName, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr, "9:16 必须放行")
}

// 官网原生模型传 16:9 → 不受影响，照旧放行。
func TestOfficialModelKeepsAcceptingNon9x16(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptestNewRequest("POST", "/v1/videos", `{"model":"wan3.0-video-官网","prompt":"test","metadata":{"ratio":"16:9"}}`)

	adaptor := &TaskAdaptor{}
	info := &relaycommon.RelayInfo{OriginModelName: OfficialModelName, TaskRelayInfo: &relaycommon.TaskRelayInfo{}}

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)
	require.Nil(t, taskErr, "官网原生不受特惠的比例收窄影响")
}

// ── 规则三（首尾帧）目标的回归守卫 ────────────────────────────────────────
//
// 首尾帧请求的画幅由用户定（16:9 / 9:16 / 1:1 都可能），规则三不改比例。
// 所以规则三的目标模型必须支持全比例 —— 否则首尾帧的 16:9 请求
// 会被目标链路的比例闸门 400 拒掉，首尾帧能力直接废掉。
//
// 生产曾把 frames_target_model 配成 wan3.0-720p-特惠（比例受限），
// 2026-11-17 已改为 wan3.0-video-官网。这条用例锁住这个事实。
func TestSupportsFullRatioGuardsFramesRouteTarget(t *testing.T) {
	require.True(t, SupportsFullRatio(OfficialModelName),
		"官网原生支持全比例 —— 规则三的首尾帧目标用它")
	require.True(t, SupportsFullRatio("wan3.0-video"))
	require.True(t, SupportsFullRatio("wan3.0-video-prime"))
	require.True(t, SupportsFullRatio("wan3.0-smart"))

	require.False(t, SupportsFullRatio(Official720PModelName),
		"特惠档只放行 9:16 —— 不能作为规则三（首尾帧）的目标")
	require.False(t, SupportsFullRatio(Official720PModelNameLegacy),
		"特惠档旧名同样只放行 9:16")
}
