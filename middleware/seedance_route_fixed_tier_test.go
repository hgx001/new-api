package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// enableFixedTierRoute 打开多源路由 + 比例规则 + 首尾帧规则，测试结束恢复原状。
//
// 为什么三条规则都要配：
//   - 规则一（threshold）：按次档未传时长归一后要靠它命中，验证「改派后仍是 30 秒」；
//   - 规则二（9:16）：验证「按次档显式传越界时长 + 9:16」不得被改派；
//   - 规则三（首尾帧）：验证「按次档显式传越界时长 + 首尾帧」也不得被改派。
//
// 只配规则一时，「显式传越界时长」本来就不命中任何规则 —— 那种用例是假绿。
func enableFixedTierRoute(t *testing.T, sources []string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":             true,
		"source_models":       sources,
		"target_model":        testTargetModel,
		"trigger_seconds":     20,
		"ratio_target_model":  testRatioTargetModel,
		"trigger_ratio":       "9:16",
		"frames_target_model": testFramesTargetModel,
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal policy: %v", err)
	}
	if err := seedanceroute_setting.UpdateFromJSONString(string(encoded)); err != nil {
		t.Fatalf("update policy: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 按次档（seedance2.5-活动）在条件路由中的**时长区间保持**。
//
// 背景（2026-11-02 上架 seedance2.5-活动 时查出来的绕过路径）：
//
//	活动版是**按次固定价**档：¥4.5/次，时长 2026-11-11 起放开为 2–30 秒。
//	「越界时长 → 400」这个 fail-loud 门控写在**源链路**
//	（youzanwan3 / xinbiapi 的按次校验）里。
//
//	但条件路由中间件发生在**源 adaptor 校验之前**：
//	middleware → 改写请求体的 model → 选渠道/选 adaptor → 源 adaptor 校验
//
//	改派后 req.Model 已经是**目标模型**，源链路的按次校验
//	（它按 req.Model 判断是不是按次档）**根本不会执行**。于是：
//
//	场景 A：客户传 seconds=1（越界）
//	  → 不命中规则一（触发值是 20 秒），但可能命中规则二/三 → 被改派
//	  → 源校验没跑 → 越界请求通过 → 由目标链路的缺省时长决定出片
//	  → **「越界报错」的承诺被静默打破**，且没有任何报错。
//
//	场景 B：客户不传时长
//	  → 源链路本应归一到 30 秒（按次档默认值）
//	  → 被改派到 wan3 链路 → wan3 的 resolveDuration 缺省是 **5 秒**
//	  → **按 30 秒卖、实际出 5 秒** —— 错交付，客户白等一轮。
//
// 所以按次档的时长语义必须在**路由层**（改派之前）就锁定，不能只依赖源 adaptor。

// 按次档显式传了越界时长（1 秒）→ **不得改派**。
//
// ⚠️ 用例必须带一个**会命中其它规则**的条件（这里用 9:16 → 规则二）：
// 只传 seconds=1 的话，它本来就不命中任何规则（规则一的触发值是 20 秒），
// 无论按次档逻辑在不在都「不改派」—— 那种用例是假绿，抓不住任何变异。
//
// 真实绕过形态：客户传 seconds=1 + 9:16 → 命中规则二 → 被改派到 wan3 链路
// → 源链路的按次校验根本没执行（req.Model 已变成目标模型）
// → 「越界报错」的 fail-loud 门控被静默绕过。
//
// 不改派 = 请求原样走到源链路 = 源链路报 400。这正是 fail-loud 的口径。
func TestFixedTierRoute_RejectsOutOfRangeDurationBeforeRouting(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	seen, origin := runRoute(t,
		`{"model":"`+constant.Seedance25HuoDongModel+`","seconds":1,"ratio":"9:16","prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("按次档显式传 1 秒仍被改派了（model=%v）—— 源链路的 400 门控被绕过", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串（不该发生改派）", origin)
	}
}

// 按次档显式传越界时长（31 秒）+ 带首尾帧 → 同样不得改派。
//
// 规则三（首尾帧）是**能力兜底**型改派：源链路不收首尾帧，带首尾帧的请求
// 不改派就必然 400。所以规则三看起来「必须改派」—— 但对按次档来说，
// 时长区间的承诺优先：客户要 31 秒 + 首尾帧，本来就是非法组合，
// 让它走到源链路报 400（时长越界）才是正确口径。
func TestFixedTierRoute_RejectsOutOfRangeDurationWithFrames(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	seen, origin := runRoute(t,
		`{"model":"`+constant.Seedance25HuoDongModel+`","seconds":31,"prompt":"a cat",`+
			`"media":[{"type":"first_frame","url":"https://x/a.png"}]}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("按次档显式传 31 秒仍被首尾帧规则改派了（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串（不该发生改派）", origin)
	}
}

// 按次档没传时长 → 改派后目标请求体必须**保持 30 秒**。
//
// 这是错交付防线：wan3 链路的 resolveDuration 缺省是 5 秒，
// 改派后不写死 30 秒，客户就会「按 30 秒的价拿到 5 秒的片」。
func TestFixedTierRoute_KeepsThirtySecondsAfterRouting(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	// 客户没传时长 → 规则一（30 秒）不该命中（没传就不是 30）。
	// 但按次档的固定语义就是 30 秒，所以路由层必须把它**归一成 30** 再判规则。
	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","prompt":"a cat"}`)

	body := decodeBody(t, seen)
	if body["model"] != testTargetModel {
		t.Fatalf("按次档未传时长应按固定 30 秒走规则一改派（model=%v）", body["model"])
	}
	if got := body["seconds"]; got != "30" {
		t.Fatalf("改派后的 seconds = %v, want \"30\" —— 目标链路缺省是 5 秒，不写死就是错交付", got)
	}
}

// 母本（按秒档）不受固定档逻辑影响：显式传 5 秒**照常路由**
// （它本来就支持 2~30 秒逐秒可选）。
//
// 这是**回归守卫**：不能把「按次档不路由」的实现写成「所有源模型都不路由」。
func TestFixedTierRoute_MotherModelStillRoutesWithExplicitSeconds(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	// 母本传 5 秒不命中规则一，但规则二（9:16）会命中 —— 用 9:16 验证母本仍可被改派。
	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","seconds":5,"ratio":"9:16","prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got == constant.Seedance25TeHuiModel {
		t.Fatalf("母本（按秒档）不该被固定档逻辑拦住（model=%v）", got)
	}
	if origin == "" {
		t.Fatalf("母本的 9:16 请求应当仍能命中规则二改派")
	}
}

// 母本没传时长时**不得**被归一成 30 秒 —— 它的缺省是 5 秒（按秒档，2~30 可选）。
//
// 不归一 = 母本未传时长不命中规则一 → 走源链路（有赞），
// 那正是母本的既有行为。
func TestFixedTierRoute_MotherModelWithoutSecondsIsNotForcedToThirty(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25TeHuiModel {
		t.Fatalf("母本未传时长不该被改派（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串", origin)
	}
}

// 按次档显式传 30 秒 → 必须照常命中规则一（这是活动版的主路径）。
func TestFixedTierRoute_ExplicitThirtySecondsStillRoutes(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("按次档显式传 30 秒没有改派（model=%v）", got)
	}
	if origin != constant.Seedance25HuoDongModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25HuoDongModel)
	}
}

// 族外的普通模型不受固定档逻辑影响（按次档归一不能误伤别人）。
func TestFixedTierRoute_DoesNotAffectNonFamilyModels(t *testing.T) {
	enableFixedTierRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	})

	// 源模型列表外的模型本来就不该被路由。
	seen, origin := runRoute(t, `{"model":"wan3.0-video","prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != "wan3.0-video" {
		t.Fatalf("族外模型被改派了（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串", origin)
	}
}
