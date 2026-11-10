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
