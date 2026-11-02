package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 生产实测：用**生产库 option 的真实 JSON** 走完整条条件路由中间件，
// 验证 seedance2.5-活动 真的能被改派（而不只是配置层判定命中）。
//
// 背景（2026-11-02 上线实测）：活动版上架后要确认它**真的有路由能力**。
// 配置层判定（IsSourceModel）通过 ≠ 中间件真的会改派 —— 中间件里还有
// 按次固定档的时长归一分支，逻辑更复杂，必须端到端跑一遍。
//
// ⚠️ 这份 JSON 是**生产实测值**，改动生产 option 时必须同步改这里。
const prodRoutePolicyJSON = `{
  "enabled": true,
  "updated_at": 1793515704,
  "source_model": "seedance2.5-特惠",
  "target_model": "seedance-2-5",
  "source_models": ["seedance2.5-特惠", "seedance2.5-活动"],
  "trigger_ratio": "9:16",
  "trigger_seconds": 30,
  "ratio_target_model": "wan3.0-720p-特惠",
  "frames_target_model": "wan3.0-720p-特惠"
}`

func enableProdRoutePolicy(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	if err := seedanceroute_setting.UpdateFromJSONString(prodRoutePolicyJSON); err != nil {
		t.Fatalf("载入生产路由配置失败: %v", err)
	}
	t.Cleanup(func() {
		restored, _ := common.Marshal(previous)
		_ = seedanceroute_setting.UpdateFromJSONString(string(restored))
	})
}

// 生产配置下，活动版的 30 秒请求必须被改派到 seedance-2-5（规则一）。
//
// 这是最核心的一条：活动版走有赞成本 ¥4.50、卖 ¥4.50 = 零毛利，
// 不路由就是白干；路由到豆包后成本 ≈ ¥0，毛利 ¥4.50。
func TestProdPolicy_活动版30秒请求被改派(t *testing.T) {
	enableProdRoutePolicy(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != "seedance-2-5" {
		t.Fatalf("活动版的 30 秒请求没有被改派（model=%v）", got)
	}
	if origin != constant.Seedance25HuoDongModel {
		t.Fatalf("RouteOriginModel() = %q, want %q —— 记的必须是实际命中的源模型",
			origin, constant.Seedance25HuoDongModel)
	}
}

// 母本在同一份配置下也必须照常改派（不能因为加了活动版把母本弄坏）。
func TestProdPolicy_母本30秒请求仍被改派(t *testing.T) {
	enableProdRoutePolicy(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != "seedance-2-5" {
		t.Fatalf("母本的 30 秒请求没有被改派（model=%v）", got)
	}
	if origin != constant.Seedance25TeHuiModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25TeHuiModel)
	}
}

// 活动版**不传时长**时必须归一成 30 秒再命中规则一 ——
// 「按 30 秒卖」的前提是确实出 30 秒。
func TestProdPolicy_活动版不传时长归一成30秒并命中(t *testing.T) {
	enableProdRoutePolicy(t)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","prompt":"a cat"}`)
	body := decodeBody(t, seen)
	if got := body["model"]; got != "seedance-2-5" {
		t.Fatalf("活动版不传时长时没有被改派（model=%v）", got)
	}
	// 归一后的 seconds 是**字符串**（strconv.Itoa 写入），不是数字 ——
	// 这是有意为之：请求体里的 seconds 字段在不同 adaptor 协议里类型不统一，
	// 写成字符串最安全（详见 normalizeFixedTierSeconds 的注释）。
	if secs, ok := body["seconds"]; !ok || secs != "30" {
		t.Fatalf("归一后的 seconds = %#v, want \"30\"（字符串）", body["seconds"])
	}
}

// 活动版显式传非 30 秒 → **放弃路由**，原样放行到源链路，
// 由源链路的 fail-loud 门控返回 400。
//
// 绝不能静默改档 —— 客户要 5 秒却出了 30 秒，是错交付。
func TestProdPolicy_活动版显式传非30秒放弃路由(t *testing.T) {
	enableProdRoutePolicy(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":5,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("显式传 5 秒时不应改派（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串（没发生改派）", origin)
	}
}

// 非源模型不得被路由 —— 否则会把别的商品也改派走。
func TestProdPolicy_非源模型不被改派(t *testing.T) {
	enableProdRoutePolicy(t)

	seen, origin := runRoute(t, `{"model":"seedance-2-5","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != "seedance-2-5" {
		t.Fatalf("不在源列表里的模型被改派了（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串（没发生改派）", origin)
	}
}
