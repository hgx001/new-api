package youzanwan3

import (
	"context"
	"encoding/json"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 排障：任务 task_cnJZv65dU4WljjaMoW6nmzNxTEOA3N1q 用户要 9:16，
// 上游实际收到 ratio=adaptive，成片 1280x720（横屏）。
//
// 根因：TaskSubmitReq 没有顶层 ratio/aspect_ratio 字段，客户端把比例放在顶层时
// 被 Go 解码器**静默丢弃**（不报错），resolveRatio() 只读 metadata，
// 于是落到 fallback=adaptive。
//
// 这里锁死「顶层比例必须生效」，并锁死「顶层优先于 metadata 之外的降级」。

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
			name: "顶层 ratio 空白视为未传",
			req:  relaycommon.TaskSubmitReq{Ratio: "   "},
			want: defaultRatio,
		},
		{
			name: "顶层非法比例不生效（降级 adaptive）",
			req:  relaycommon.TaskSubmitReq{Ratio: "not-a-ratio"},
			want: defaultRatio,
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
			name: "metadata.aspect_ratio 次于 metadata.ratio",
			req: relaycommon.TaskSubmitReq{
				Metadata: map[string]interface{}{
					"ratio":        "16:9",
					"aspect_ratio": "9:16",
				},
			},
			want: "16:9",
		},
		{
			name: "顶层 ratio 优先于顶层 aspect_ratio",
			req: relaycommon.TaskSubmitReq{
				Ratio:       "16:9",
				AspectRatio: "9:16",
			},
			want: "16:9",
		},
		{
			name: "size 可推断比例（720x1280 -> 9:16）",
			req:  relaycommon.TaskSubmitReq{Size: "720x1280"},
			want: "9:16",
		},
		{
			name: "size 竖屏 1280x720 方向相反时按自身推断",
			req:  relaycommon.TaskSubmitReq{Size: "1280x720"},
			want: "16:9",
		},
		{
			name: "size 无法推断时降级 adaptive",
			req:  relaycommon.TaskSubmitReq{Size: "720P"},
			want: defaultRatio,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, resolveRatio(tc.req))
		})
	}
}

// 顶层比例必须真的落到上游请求体里，而不是只在单元函数里正确。
func TestTopLevelRatioReachesUpstreamBody(t *testing.T) {
	body, err := (&TaskAdaptor{}).buildRequestBody(context.Background(), relaycommon.TaskSubmitReq{
		Model:  "seedance2.5-特惠",
		Prompt: "test prompt",
		Ratio:  "9:16",
	})
	require.NoError(t, err)

	decoded := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "9:16", decoded["ratio"])
}

// 回归护栏：metadata 路径不能被这次改动破坏（此前生产 38 条 9:16 依赖它）。
func TestMetadataRatioStillWorks(t *testing.T) {
	body, err := (&TaskAdaptor{}).buildRequestBody(context.Background(), relaycommon.TaskSubmitReq{
		Model:    "seedance2.5-特惠",
		Prompt:   "test prompt",
		Metadata: map[string]interface{}{"ratio": "9:16"},
	})
	require.NoError(t, err)

	decoded := map[string]interface{}{}
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "9:16", decoded["ratio"])
}
