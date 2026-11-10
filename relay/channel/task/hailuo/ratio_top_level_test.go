package hailuo

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查新增。
//
// hailuo = MiniMax 适配器，生产渠道 1「MiniMax-M3」与 26 在售。
//
// 此前 resolveV2Ratio 只读 metadata，客户端顶层比例被丢弃 →
// 落 "adaptive"，成片比例不对但任务成功、照扣费。

func TestResolveV2RatioReadsTopLevelFields(t *testing.T) {
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
			req := tc.req
			ratio, err := resolveV2Ratio(&req)
			require.NoError(t, err)
			require.Equal(t, tc.want, ratio)
		})
	}
}
