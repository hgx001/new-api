package erchun

import (
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 审查新增。
//
// erchun = 二春渠道（当前生产无在售渠道，但代码仍在，渠道随时可能重建）。
// 此前 resolveAspectRatio 只读 metadata，客户端顶层比例被静默丢弃。
//
// 与其它渠道一致：顶层比例必须生效，非法比例必须报错。

func TestResolveAspectRatioReadsTopLevelFields(t *testing.T) {
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
			ratio, err := resolveAspectRatio(tc.req)
			require.NoError(t, err)
			require.Equal(t, tc.want, ratio)
		})
	}
}

// 非法比例必须报错（二春口径）。
func TestResolveAspectRatioInvalidTopLevelStillRejected(t *testing.T) {
	_, err := resolveAspectRatio(relaycommon.TaskSubmitReq{Ratio: "99:1"})
	require.Error(t, err, "非法顶层比例必须报错")
}
