package middleware

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 「seedance2.5-活动 → 豆包兜底」的按源模型改派（2026-11-03 改造）。
//
// 背景：这一版把促销族的两个成员**对调**了 ——
//
//	改造前：活动版常驻渠道 17 有赞，30 秒改派心笔 wan3.0-720p-按次
//	改造后：活动版常驻渠道 25 心笔（= wan3.0-720p-按次 的同一条链路），
//	        **豆包 seedance-2-5 变成兜底路由**（仅时长超过阈值时命中）
//
// 为什么兜底是豆包而不是心笔：
//
//	豆包账号每天刷新额度，**用不完就亏本**（额度是买来的、过期作废）。
//	把 30 秒的量导过去，既能赚毛利（豆包成本 ¥2.00 vs 活动版售价 ¥4.50），
//	又能把每天刷新的额度消耗掉 —— 一举两得。
//
//	母本 seedance2.5-特惠（按秒 ¥0.20/秒）**完全不受影响**：
//	常驻仍是有赞，30 秒仍改派豆包。
//
// 本文件锁四件事：
//  1. 活动版的 30 秒请求改派豆包，母本同样改派豆包但计费口径互不干扰；
//  2. 超长提示词**不再**触发改派（2026-11-11 删除规则四）；
//  3. 改派后计费与展示仍按「客户请求的模型」——源模型名不变；
//  4. 豆包扛不住的请求（参考视频/音频、明确分辨率）**留在源链路**。

// enableDoubaoFallbackRoute 打开「两个源模型的兜底目标都是豆包」的拆分配置。
//
// 注意这里仍然保留 source_overrides：活动版显式写一遍 target_model=豆包，
// 是为了让配置**自带意图**（不依赖公共字段的隐式回落）——
// 万一以后有人把公共 target_model 改成别的，活动版不会被带着走。
func enableDoubaoFallbackRoute(t *testing.T) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{constant.Seedance25TeHuiModel, constant.Seedance25HuoDongModel},
		"target_model":    doubaoTargetModel,
		"trigger_seconds": 20,
		"source_overrides": map[string]interface{}{
			constant.Seedance25HuoDongModel: map[string]interface{}{
				"target_model": doubaoTargetModel,
			},
		},
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

// 活动版的 30 秒请求必须改派到豆包（兜底消耗每天刷新的豆包额度）。
func TestHuoDongRouteToDoubaoFallback(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != doubaoTargetModel {
		t.Fatalf("活动版 30 秒请求没有改派到豆包（model=%v）", got)
	}
	if origin != constant.Seedance25HuoDongModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25HuoDongModel)
	}
}

// 母本必须**不受影响**：仍然改派到豆包。
//
// 母本是按秒计价、活动版是按次计价，计费链靠 BillingModelName() 区分，
// 两者不能互相影响。母本的兜底目标没被活动版的覆盖带走。
func TestTeHuiRouteUnaffectedByHuoDongOverride(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != doubaoTargetModel {
		t.Fatalf("母本的 30 秒请求被改派到了 %v，应该是 %v", got, doubaoTargetModel)
	}
	if origin != constant.Seedance25TeHuiModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25TeHuiModel)
	}
}

// 规则四已删除：活动版的超长提示词（非 30 秒）不再改派豆包。
//
// 心笔常驻链路自己吃 20000 字符提示词；超长提示词统一交给后面的压缩中间件，
// 不再走「悄悄换一条链路」这条路。
func TestHuoDongLongPromptDoesNotRerouteToDoubao(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	longPrompt := strings.Repeat("字", 6000)
	body := `{"model":"` + constant.Seedance25HuoDongModel + `","seconds":10,"prompt":"` + longPrompt + `"}`
	seen, origin := runRoute(t, body)
	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("超长提示词不该改派到豆包，实际 model=%v", got)
	}
	if origin != "" {
		t.Fatalf("没改派却记下了计费原点 %q", origin)
	}
	if seen != body {
		t.Fatalf("超长提示词不该改写请求体")
	}
}

// 母本同理：10 秒 + 超长提示词留在有赞源链路，由压缩中间件处理。
func TestTeHuiLongPromptDoesNotRerouteToDoubao(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	longPrompt := strings.Repeat("字", 6000)
	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","seconds":10,"prompt":"`+longPrompt+`"}`)
	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25TeHuiModel {
		t.Fatalf("超长提示词不该改派到豆包，实际 model=%v", got)
	}
	if origin != "" {
		t.Fatalf("没改派却记下了计费原点 %q", origin)
	}
}

// 改派豆包后，参考图必须落到**顶层 input_reference[]**。
//
// 豆包链路只认顶层 input_reference，留在 metadata.media 会被静默丢弃
// （客户要的图根本没进去，任务照跑照扣费）。
func TestDoubaoRoutePutsReferenceImagesOnTopLevel(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,`+
		`"prompt":"a cat","metadata":{"media":[{"type":"reference_image","url":"https://example.com/a.png"}]}}`)

	decoded := decodeBody(t, seen)
	images, ok := decoded["input_reference"].([]interface{})
	if !ok || len(images) != 1 {
		t.Fatalf("参考图没有落到顶层 input_reference（decoded=%v）", decoded)
	}
	if images[0] != "https://example.com/a.png" {
		t.Fatalf("input_reference[0] = %v，地址不对", images[0])
	}
	// metadata 整块必须丢掉 —— 留着会让上游收到一份没声明过的字段。
	if _, hasMetadata := decoded["metadata"]; hasMetadata {
		t.Fatalf("metadata 没有被清掉（decoded=%v）", decoded)
	}
}

// 时长必须显式写成 30 秒 —— 触发规则一的就是 30 秒。
func TestDoubaoRouteWritesTriggerDuration(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)

	decoded := decodeBody(t, seen)
	if decoded["seconds"] != "30" {
		t.Fatalf("seconds = %v, want \"30\"", decoded["seconds"])
	}
}

// 带参考视频/参考音频的请求**不能**改派到豆包。
//
// 豆包链路同样只支持参考图，路由过去必然 400。留在源链路（心笔）还能给出
// 可读的错误原因。
func TestDoubaoRouteRejectsUnsupportedMedia(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	cases := []struct {
		name string
		body string
	}{
		{
			name: "参考视频",
			body: `{"model":"` + constant.Seedance25HuoDongModel + `","seconds":30,"prompt":"a cat",` +
				`"media":[{"type":"reference_video","url":"https://example.com/a.mp4"}]}`,
		},
		{
			name: "参考音频",
			body: `{"model":"` + constant.Seedance25HuoDongModel + `","seconds":30,"prompt":"a cat",` +
				`"media":[{"type":"reference_audio","url":"https://example.com/a.mp3"}]}`,
		},
		{
			name: "顶层 audio",
			body: `{"model":"` + constant.Seedance25HuoDongModel + `","seconds":30,"prompt":"a cat",` +
				`"audio":"https://example.com/a.mp3"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seen, origin := runRoute(t, tc.body)
			if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
				t.Fatalf("%s 的请求被改派了（model=%v），应该留在源链路", tc.name, got)
			}
			if origin != "" {
				t.Fatalf("%s 的请求记录了来源模型 %q，应该为空（没发生改派）", tc.name, origin)
			}
		})
	}
}

// 480P / 720P / 1080P 都是豆包的合法档位：活动版长时长请求照常改派。
func TestDoubaoRouteAcceptsSupportedResolution(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	for _, resolution := range []string{"480P", "720P", "1080P"} {
		t.Run(resolution, func(t *testing.T) {
			seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat","resolution":"`+resolution+`"}`)

			body := decodeBody(t, seen)
			if got := body["model"]; got != testTargetModel {
				t.Fatalf("%s 请求未改派（model=%v）", resolution, got)
			}
			if got := body["resolution"]; got != strings.ToLower(resolution) {
				t.Fatalf("resolution = %v, want %s", got, strings.ToLower(resolution))
			}
		})
	}
}

// 目标渠道不可用时必须留在源链路。
//
// 中间件改写 body 之后，重试只会用同一个模型名重选渠道，不会退回源模型 ——
// 所以预检是唯一的机会。渠道挂掉时不能让请求撞 503。
func TestDoubaoRouteSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableDoubaoFallbackRoute(t)
	stubChannelAvailable(t, false)

	seen, _ := runRouteRaw(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("目标渠道不可用时仍然改派了（model=%v），应该留在源链路", got)
	}
}

// 活动版**不传时长**也要改派豆包 —— 归一成固定 30 秒后命中规则一。
//
// 这条锁的是「按次固定档归一」与「规则一」的配合：活动版不传时长时，
// 源链路（心笔）的语义是「不传 → 30 秒」，所以路由层必须先归一，
// 否则这类请求就走不到 30 秒规则，白白错过消耗豆包额度的机会。
func TestHuoDongRouteToDoubaoWhenDurationOmitted(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != doubaoTargetModel {
		t.Fatalf("活动版未传时长的请求没有改派到豆包（model=%v）", got)
	}
	if origin != constant.Seedance25HuoDongModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25HuoDongModel)
	}
	// 归一后必须显式写出 30 秒，否则豆包侧缺省是 5 秒 —— 按 30 秒的价出 5 秒的片。
	if got := decodeBody(t, seen)["seconds"]; got != "30" {
		t.Fatalf("seconds = %v, want \"30\"（不写回就是按 30 秒收钱出 5 秒的片）", got)
	}
}

// 活动版传**非 30 秒**时必须放弃路由、留在源链路（心笔会 fail-loud 报 400）。
//
// 这条锁的是「不做就近吸附」：客户要 5 秒却按 30 秒计费，比报错更糟。
func TestHuoDongRouteAbandonsOnNonFixedDuration(t *testing.T) {
	enableDoubaoFallbackRoute(t)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":10,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("10 秒的请求被改派了（model=%v），应该留在源链路", got)
	}
	if origin != "" {
		t.Fatalf("10 秒的请求记录了来源模型 %q，应该为空（没发生改派）", origin)
	}
}

// joinStrings 用逗号连接字符串列表（测试辅助，避免引入额外 import）。
func joinStrings(items []string) string {
	out := ""
	for i, item := range items {
		if i > 0 {
			out += ","
		}
		out += item
	}
	return out
}
