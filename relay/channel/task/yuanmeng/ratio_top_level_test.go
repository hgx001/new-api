package yuanmeng

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查新增。
//
// yuanmeng = 视频生成车站，生产渠道 27 在售（wan3.0-班车）。
//
// 此前 resolveRatio 只读 metadata，客户端顶层比例被丢弃 →
// 落 defaultRatio(16:9)，成片比例不对但任务成功、照扣费。
//
// ⚠️ 视频生成车站的口径：非法比例**报错**（unsupported ratio）。

func TestResolveRatioReadsTopLevelFields(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want string
	}{
		{
			name: "顶层 ratio 生效",
			req:  relaycommon.TaskSubmitReq{Ratio: "9:16"},
			want: "9:16",
		},
		{
			name: "顶层 aspect_ratio 生效",
			req:  relaycommon.TaskSubmitReq{AspectRatio: "9:16"},
			want: "9:16",
		},
		{
			name: "顶层 ratio 优先于顶层 aspect_ratio",
			req:  relaycommon.TaskSubmitReq{Ratio: "16:9", AspectRatio: "9:16"},
			want: "16:9",
		},
		{
			name: "metadata 优先于顶层",
			req: relaycommon.TaskSubmitReq{
				Ratio:    "16:9",
				Metadata: map[string]interface{}{"ratio": "9:16"},
			},
			want: "9:16",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ratio, err := resolveRatio(tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.want, ratio)
		})
	}
}

// 非法比例仍必须报错（视频生成车站口径）。
func TestResolveRatioInvalidTopLevelStillRejected(t *testing.T) {
	_, err := resolveRatio(relaycommon.TaskSubmitReq{Ratio: "not-a-ratio"})
	require.Error(t, err, "非法顶层比例必须报错")
}

// 2026-11-12 seedance-2-5 常驻迁移：该模型此前常驻豆包官网链路（manwu），
// 自适应档的档位名叫 "auto"；迁到视频生成车站（yuanmeng）后上游档位名是
// "adaptive"。同一语义的档位别名，老客户端写法必须继续可用 —— 拒掉会让
// 迁移当天所有选「自动」档的请求 100% 400。
func TestResolveRatioAutoAliasMapsToAdaptive(t *testing.T) {
	cases := []struct {
		name string
		req  relaycommon.TaskSubmitReq
	}{
		{name: "顶层 auto", req: relaycommon.TaskSubmitReq{Ratio: "auto"}},
		{name: "顶层大小写变体", req: relaycommon.TaskSubmitReq{Ratio: "Auto"}},
		{name: "metadata auto", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "auto"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRatio(tc.req)
			require.NoError(t, err)
			require.Equal(t, "adaptive", got)
		})
	}
}

// 别名只认 auto 本身：带杂质的写法仍要 fail-loud，不能被归一化顺手放行。
func TestResolveRatioAutoAliasDoesNotWiden(t *testing.T) {
	_, err := resolveRatio(relaycommon.TaskSubmitReq{Ratio: "auto16"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported ratio")
}
