package common

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// 单元测试
// ---------------------------------------------------------------------------

func TestResolveTaskRatioPrecedence(t *testing.T) {
	all := TaskRatioCandidates{
		"adaptive": true, "16:9": true, "9:16": true,
		"4:3": true, "3:4": true, "1:1": true,
	}
	noSize := func(string) (string, bool) { return "", false }

	cases := []struct {
		name       string
		req        TaskSubmitReq
		wantRatio  string
		wantSource TaskRatioSource
	}{
		{
			name:       "metadata.ratio 最高优先",
			req:        TaskSubmitReq{Ratio: "16:9", Metadata: map[string]interface{}{"ratio": "9:16"}},
			wantRatio:  "9:16",
			wantSource: TaskRatioSourceMetadataRatio,
		},
		{
			name:       "metadata.aspect_ratio 次于 metadata.ratio",
			req:        TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "16:9", "aspect_ratio": "9:16"}},
			wantRatio:  "16:9",
			wantSource: TaskRatioSourceMetadataRatio,
		},
		{
			name:       "metadata.aspect_ratio 单独存在时生效",
			req:        TaskSubmitReq{Metadata: map[string]interface{}{"aspect_ratio": "9:16"}},
			wantRatio:  "9:16",
			wantSource: TaskRatioSourceMetadataAspect,
		},
		{
			name:       "顶层 ratio 生效",
			req:        TaskSubmitReq{Ratio: "9:16"},
			wantRatio:  "9:16",
			wantSource: TaskRatioSourceTopLevelRatio,
		},
		{
			name:       "顶层 aspect_ratio 生效",
			req:        TaskSubmitReq{AspectRatio: "9:16"},
			wantRatio:  "9:16",
			wantSource: TaskRatioSourceTopLevelAspect,
		},
		{
			name:       "顶层 ratio 优先于顶层 aspect_ratio",
			req:        TaskSubmitReq{Ratio: "16:9", AspectRatio: "9:16"},
			wantRatio:  "16:9",
			wantSource: TaskRatioSourceTopLevelRatio,
		},
		{
			name:       "metadata 优先于顶层",
			req:        TaskSubmitReq{Ratio: "16:9", Metadata: map[string]interface{}{"aspect_ratio": "9:16"}},
			wantRatio:  "9:16",
			wantSource: TaskRatioSourceMetadataAspect,
		},
		{
			name:       "都没给时用默认值",
			req:        TaskSubmitReq{},
			wantRatio:  "adaptive",
			wantSource: TaskRatioSourceDefault,
		},
		{
			name:       "顶层空白视为未传",
			req:        TaskSubmitReq{Ratio: "   "},
			wantRatio:  "adaptive",
			wantSource: TaskRatioSourceDefault,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ResolveTaskRatio(tc.req, TaskRatioOptions{
				Candidates:   all,
				DefaultRatio: "adaptive",
				SizeToRatio:  noSize,
			})
			require.Equal(t, tc.wantRatio, got.Ratio)
			require.Equal(t, tc.wantSource, got.Source)
		})
	}
}

func TestResolveTaskRatioSizeFallback(t *testing.T) {
	all := TaskRatioCandidates{"16:9": true, "9:16": true, "1:1": true}
	sizeToRatio := func(size string) (string, bool) {
		switch size {
		case "1280x720":
			return "16:9", true
		case "720x1280":
			return "9:16", true
		}
		return "", false
	}

	got := ResolveTaskRatio(TaskSubmitReq{Size: "720x1280"}, TaskRatioOptions{
		Candidates:   all,
		DefaultRatio: "adaptive",
		SizeToRatio:  sizeToRatio,
	})
	require.Equal(t, "9:16", got.Ratio)
	require.Equal(t, TaskRatioSourceSize, got.Source)

	// 推不出时用默认值，不编造
	got = ResolveTaskRatio(TaskSubmitReq{Size: "720P"}, TaskRatioOptions{
		Candidates:   all,
		DefaultRatio: "adaptive",
		SizeToRatio:  sizeToRatio,
	})
	require.Equal(t, "adaptive", got.Ratio)
	require.Equal(t, TaskRatioSourceDefault, got.Source)
}

func TestResolveTaskRatioInvalidIsRejectedNotApplied(t *testing.T) {
	all := TaskRatioCandidates{"16:9": true, "9:16": true}
	noSize := func(string) (string, bool) { return "", false }

	got := ResolveTaskRatio(TaskSubmitReq{Ratio: "not-a-ratio"}, TaskRatioOptions{
		Candidates:   all,
		DefaultRatio: "adaptive",
		SizeToRatio:  noSize,
	})
	require.Equal(t, "adaptive", got.Ratio)
	require.Equal(t, TaskRatioSourceInvalidRejected, got.Source)
	require.Equal(t, "not-a-ratio", got.Rejected)
}

// SizeFirst：size 的原始比例写法优先于顶层 ratio。
// 唯一使用场景是 manwu（type=66）—— 官网以 size 为准，是已上线的既定契约。
func TestResolveTaskRatioSizeFirst(t *testing.T) {
	result := ResolveTaskRatio(TaskSubmitReq{
		Ratio: "9:16",
		Size:  "1:1",
	}, TaskRatioOptions{
		DefaultRatio: "16:9",
		SizeFirst:    true,
	})
	require.Equal(t, "1:1", result.Ratio)
	require.Equal(t, TaskRatioSourceSize, result.Source)
}

// 不开 SizeFirst 时（默认全站口径），顶层 ratio 优先于 size。
func TestResolveTaskRatioTopLevelWinsOverSizeByDefault(t *testing.T) {
	result := ResolveTaskRatio(TaskSubmitReq{
		Ratio: "9:16",
		Size:  "1:1",
	}, TaskRatioOptions{
		DefaultRatio: "16:9",
	})
	require.Equal(t, "9:16", result.Ratio)
	require.Equal(t, TaskRatioSourceTopLevelRatio, result.Source)
}

// AllowEmptyRatio：没指定比例时返回空串而不是默认值。
// 用于官网有「自动/不指定」档位的场景 —— 补默认值等于把「用户没选」改成参数变更。
func TestResolveTaskRatioAllowEmpty(t *testing.T) {
	result := ResolveTaskRatio(TaskSubmitReq{}, TaskRatioOptions{
		DefaultRatio:    "16:9",
		AllowEmptyRatio: true,
	})
	require.Equal(t, "", result.Ratio)
	require.Equal(t, TaskRatioSourceDefault, result.Source)
}

// AllowEmptyRatio 不影响显式传值：客户端给了就必须生效。
func TestResolveTaskRatioAllowEmptyStillRespectsExplicitValue(t *testing.T) {
	result := ResolveTaskRatio(TaskSubmitReq{Ratio: "9:16"}, TaskRatioOptions{
		DefaultRatio:    "16:9",
		AllowEmptyRatio: true,
	})
	require.Equal(t, "9:16", result.Ratio)
}
