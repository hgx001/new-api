package middleware

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 条件路由的**多源**支持（2026-11-02，seedance2.5-活动 上架）。
//
// 活动版 seedance2.5-活动 必须继承母本 seedance2.5-特惠 的整套路由规则，
// 尤其 30 秒规则 —— 活动版走有赞的成本是 ¥4.50、卖 ¥4.50（零毛利），
// 路由到豆包后成本 ≈ ¥0，毛利 ¥4.50。
//
// 所以中间件必须能按**多个**源模型名匹配。
//
// ⚠️ 另一条硬要求：改派日志必须打**实际命中的那个源模型名**。
// 多源化之后如果日志还打写死的那一个，排查时会看到「seedance2.5-特惠 → seedance-2-5」
// 但客户明明请求的是活动版 —— 完全对不上账。

// enableMultiSourceRoute 打开多源路由配置，测试结束恢复原状。
func enableMultiSourceRoute(t *testing.T, sources []string, target string, seconds int) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":         true,
		"source_models":   sources,
		"target_model":    target,
		"trigger_seconds": seconds,
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

// 多源配置下，两个源模型都必须能命中规则一。
func TestMultiSourceRouteMatchesBothSources(t *testing.T) {
	enableMultiSourceRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	}, testTargetModel, 20)

	for _, source := range []string{constant.Seedance25TeHuiModel, constant.Seedance25HuoDongModel} {
		seen, origin := runRoute(t, `{"model":"`+source+`","seconds":30,"prompt":"a cat"}`)
		if got := decodeBody(t, seen)["model"]; got != testTargetModel {
			t.Fatalf("源模型 %q 的 30 秒请求没有改派（model=%v）", source, got)
		}
		if origin != source {
			t.Fatalf("RouteOriginModel() = %q, want %q —— 记的必须是实际命中的源模型", origin, source)
		}
	}
}

// 不在源列表里的模型**不得**被路由 —— 否则会把别的商品也改派走。
func TestMultiSourceRouteIgnoresOtherModels(t *testing.T) {
	enableMultiSourceRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	}, testTargetModel, 20)

	seen, origin := runRoute(t, `{"model":"seedance-2-5","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != "seedance-2-5" {
		t.Fatalf("不在源列表里的模型被改派了（model=%v）", got)
	}
	if origin != "" {
		t.Fatalf("RouteOriginModel() = %q, want 空串（没发生改派）", origin)
	}
}

// 改派日志必须打**实际命中的源模型名**，不能打写死的某一个。
//
// 这是多源化后最容易出的错：日志显示「seedance2.5-特惠 → seedance-2-5」，
// 客户请求的却是活动版 —— 账单与日志对不上，排查时完全找不到头绪。
func TestMultiSourceRouteLogUsesMatchedSource(t *testing.T) {
	enableMultiSourceRoute(t, []string{
		constant.Seedance25TeHuiModel,
		constant.Seedance25HuoDongModel,
	}, testTargetModel, 20)
	readLogs := captureRouteLogs(t)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("前置条件失败：活动版的 30 秒请求没有改派")
	}

	logs := readLogs()
	if !strings.Contains(logs, constant.Seedance25HuoDongModel) {
		t.Fatalf("改派日志没有写实际命中的源模型名（%s），日志内容=%q",
			constant.Seedance25HuoDongModel, logs)
	}
}

// 老配置（只配 source_model）必须继续工作 —— 生产 options 至今是单值格式，
// 升级代码不能把现有路由弄失效。
func TestSingleSourceRouteStillWorks(t *testing.T) {
	enableRoute(t, testSourceModel, testTargetModel, 20)

	seen, origin := runRoute(t, `{"model":"`+testSourceModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != testTargetModel {
		t.Fatalf("老配置下 30 秒请求没有改派（model=%v）", got)
	}
	if origin != testSourceModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, testSourceModel)
	}
}
