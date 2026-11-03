package middleware

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/seedanceroute_setting"
)

// 「seedance2.5-活动 → 心笔按次链路」的按源模型改派（2026-11-03）。
//
// 背景：促销族的两个成员需要**不同的兜底链路**：
//
//	母本 seedance2.5-特惠（按秒 ¥0.20/秒）→ 30 秒走豆包 seedance-2-5（成本 ≈ ¥0）
//	活动版 seedance2.5-活动（按次 ¥4.5/次）→ 30 秒走心笔 wan3.0-720p-按次
//	                                  （上游 wan3.0-video 480P ¥0.135/秒 × 30 = ¥4.05 < ¥4.50）
//
// 两者计费口径不同、目标链路能力不同，**不能共用一套目标模型**。
// 所以配置层加了 source_overrides：按源模型分别指定目标模型。
//
// 本文件锁三件事：
//  1. 活动版走心笔、母本仍走豆包 —— 互不影响；
//  2. 心笔链路的素材改写正确（参考图落顶层 reference_images、比例落 metadata.ratio）；
//  3. 心笔链路扛不住的请求**留在源链路**（参考视频/音频、首尾帧、seed、1080P、超限图数）。

// enableSplitRoute 打开「母本走豆包、活动版走心笔」的拆分配置。
func enableSplitRoute(t *testing.T, target string) {
	t.Helper()
	previous := seedanceroute_setting.Get()
	payload := map[string]interface{}{
		"enabled":         true,
		"source_models":   []string{constant.Seedance25TeHuiModel, constant.Seedance25HuoDongModel},
		"target_model":    doubaoTargetModel,
		"trigger_seconds": 30,
		"source_overrides": map[string]interface{}{
			constant.Seedance25HuoDongModel: map[string]interface{}{
				"target_model": target,
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

// 活动版的 30 秒请求必须改派到心笔按次链路。
func TestHuoDongRouteToXinbiPerCall(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != xinbiPerCallTargetModel {
		t.Fatalf("活动版 30 秒请求没有改派到心笔（model=%v）", got)
	}
	if origin != constant.Seedance25HuoDongModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25HuoDongModel)
	}
}

// 母本必须**不受影响**：仍然改派到豆包，不能被活动版的覆盖带走。
//
// 这是本次改造最容易出的错：source_models 是共用列表，
// 一旦目标模型取错来源，母本会被一起改派到心笔 —— 母本是按秒计价，
// 目标链路是按次计价，计费链会直接算错。
func TestTeHuiRouteUnaffectedByHuoDongOverride(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25TeHuiModel+`","seconds":30,"prompt":"a cat"}`)
	if got := decodeBody(t, seen)["model"]; got != doubaoTargetModel {
		t.Fatalf("母本的 30 秒请求被改派到了 %v，应该是 %v（母本不能被活动版的覆盖带走）",
			got, doubaoTargetModel)
	}
	if origin != constant.Seedance25TeHuiModel {
		t.Fatalf("RouteOriginModel() = %q, want %q", origin, constant.Seedance25TeHuiModel)
	}
}

// 参考图必须落到**顶层** reference_images[]。
//
// 心笔的 collectReferenceImages 只读顶层 image/images/media[image]/input_reference ——
// 留在 metadata.media 会被**静默丢弃**（客户要的图根本没进去，任务照跑照扣费）。
func TestXinbiRoutePutsReferenceImagesOnTopLevel(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,`+
		`"prompt":"a cat","metadata":{"media":[{"type":"reference_image","url":"https://example.com/a.png"}]}}`)

	decoded := decodeBody(t, seen)
	images, ok := decoded["reference_images"].([]interface{})
	if !ok || len(images) != 1 {
		t.Fatalf("参考图没有落到顶层 reference_images（decoded=%v）", decoded)
	}
	if images[0] != "https://example.com/a.png" {
		t.Fatalf("reference_images[0] = %v，地址不对", images[0])
	}
	// metadata.media 不能残留 —— 留着会让上游收到一份没声明过的字段。
	if metadata, ok := decoded["metadata"].(map[string]interface{}); ok {
		if _, hasMedia := metadata["media"]; hasMedia {
			t.Fatalf("metadata.media 没有被清掉（decoded=%v）", decoded)
		}
	}
}

// 比例必须落到 metadata.ratio —— 心笔的 resolveRatio 只读 metadata。
func TestXinbiRoutePutsRatioInMetadata(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,`+
		`"prompt":"a cat","metadata":{"ratio":"9:16"}}`)

	decoded := decodeBody(t, seen)
	metadata, ok := decoded["metadata"].(map[string]interface{})
	if !ok {
		t.Fatalf("metadata 缺失（decoded=%v）", decoded)
	}
	if metadata["ratio"] != "9:16" {
		t.Fatalf("metadata.ratio = %v, want 9:16", metadata["ratio"])
	}
}

// 时长必须显式写成 30 秒 —— 心笔是固定 30 秒按次档。
func TestXinbiRouteWritesFixedDuration(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)

	decoded := decodeBody(t, seen)
	if decoded["seconds"] != "30" {
		t.Fatalf("seconds = %v, want \"30\"（心笔固定 30 秒按次）", decoded["seconds"])
	}
}

// 带参考视频/参考音频的请求**不能**改派到心笔。
//
// 心笔兼容口对 reference_videos / reference_audios 返回 UNSUPPORTED_FIELD，
// 路由过去必然 400。留在源链路反而能跑。
func TestXinbiRouteRejectsUnsupportedMedia(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

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

// 带首尾帧的请求不能改派到心笔（兼容口只收参考图）。
func TestXinbiRouteRejectsFrames(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, origin := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat",`+
		`"media":[{"type":"first_frame","url":"https://example.com/a.png"}]}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("带首尾帧的请求被改派了（model=%v），应该留在源链路", got)
	}
	if origin != "" {
		t.Fatalf("带首尾帧的请求记录了来源模型 %q，应该为空", origin)
	}
}

// 带 seed 的请求不能改派到心笔（上游 parameterSchema 不认 seed，会 422）。
func TestXinbiRouteRejectsSeed(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat","seed":42}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("带 seed 的请求被改派了（model=%v），应该留在源链路", got)
	}
}

// 明确要 1080P 的请求不能改派到心笔（它只交付 720P）。
func TestXinbiRouteRejects1080P(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat","resolution":"1080P"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("要 1080P 的请求被改派了（model=%v），应该留在源链路", got)
	}
}

// 参考图超过心笔上限（9 张）时不改派 —— 路由过去必然被上游 400。
//
// 源链路（有赞）限 10 张，心笔限 9 张，所以 10 张的请求改派过去必挂。
func TestXinbiRouteRejectsTooManyImages(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	images := make([]string, 0, xinbiMaxReferenceImages+1)
	for i := 0; i <= xinbiMaxReferenceImages; i++ {
		images = append(images, `"https://example.com/`+string(rune('a'+i))+`.png"`)
	}
	body := `{"model":"` + constant.Seedance25HuoDongModel + `","seconds":30,"prompt":"a cat","images":[` +
		joinStrings(images) + `]}`

	seen, _ := runRoute(t, body)
	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("参考图超限的请求被改派了（model=%v），应该留在源链路", got)
	}
}

// 目标渠道不可用时必须留在源链路。
//
// 中间件改写 body 之后，重试只会用同一个模型名重选渠道，不会退回源模型 ——
// 所以预检是唯一的机会。渠道挂掉时不能让请求撞 503。
func TestXinbiRouteSkipsWhenTargetChannelUnavailable(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)
	stubChannelAvailable(t, false)

	seen, _ := runRouteRaw(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":30,"prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("目标渠道不可用时仍然改派了（model=%v），应该留在源链路", got)
	}
}

// 非 30 秒的请求不改派到心笔（心笔只卖固定 30 秒）。
func TestXinbiRouteIgnoresNonFixedDuration(t *testing.T) {
	enableSplitRoute(t, xinbiPerCallTargetModel)

	seen, _ := runRoute(t, `{"model":"`+constant.Seedance25HuoDongModel+`","seconds":10,"prompt":"a cat"}`)

	if got := decodeBody(t, seen)["model"]; got != constant.Seedance25HuoDongModel {
		t.Fatalf("10 秒的请求被改派了（model=%v），应该留在源链路", got)
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
