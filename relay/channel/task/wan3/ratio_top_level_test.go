package wan3

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查新增。
//
// 首轮「顶层比例被静默丢弃」的修复只覆盖了 youzanwan3 与 megabyai，
// 但根因是**全站性**的：只要适配器的 resolveRatio 只读 metadata，
// 客户端把比例放在请求体顶层（OpenAI 风格）时，一样被丢掉。
//
// wan3 = DashScope 适配器，生产渠道 12「DashScope-Wan3」在售
// （wan3.0-video-官网 / wan3.0-720p-官网 / wan3.0-720p-特惠），
// 近 30 天 97 单。此前顶层比例会被丢成 adaptive，成片比例随机。

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
		{
			name: "非法顶层比例不生效（降级默认）",
			req:  relaycommon.TaskSubmitReq{Ratio: "not-a-ratio"},
			want: defaultRatio,
		},
		{
			name: "顶层空白视为未传",
			req:  relaycommon.TaskSubmitReq{Ratio: "  "},
			want: defaultRatio,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveRatio(tc.req))
		})
	}
}

// 回归护栏：metadata 路径不能被破坏（生产已在用）。
func TestResolveRatioMetadataStillWorks(t *testing.T) {
	require.Equal(t, "9:16", resolveRatio(relaycommon.TaskSubmitReq{
		Metadata: map[string]interface{}{"ratio": "9:16"},
	}))
}
