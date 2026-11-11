package common

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustParseTaskRequest(t *testing.T, body string) *TaskSubmitReq {
	t.Helper()
	var req TaskSubmitReq
	require.NoError(t, common.Unmarshal([]byte(body), &req))
	return &req
}

func mediaCounts(summary map[string]any) map[string]int {
	counts, _ := summary["media_counts"].(map[string]int)
	return counts
}

func fieldCounts(summary map[string]any) map[string]int {
	fields, _ := summary["media_fields"].(map[string]int)
	return fields
}

// 日志详情要回答的是「客户到底发了什么」，而不是「改写/降级之后变成了什么」。
func TestBuildTaskRequestParamsSummary_RecordsCoreFields(t *testing.T) {
	req := mustParseTaskRequest(t, `{
		"model":"seedance2.5-活动",
		"prompt":"一只猫在雨里奔跑",
		"seconds":"10",
		"resolution":"720P",
		"size":"1280x720",
		"seed":42,
		"metadata":{"ratio":"9:16"}
	}`)

	summary := BuildTaskRequestParamsSummary(req)

	require.NotNil(t, summary)
	assert.Equal(t, "seedance2.5-活动", summary["model"])
	assert.Equal(t, "一只猫在雨里奔跑", summary["prompt"])
	assert.Equal(t, "10", summary["seconds"])
	assert.Equal(t, "720P", summary["resolution"])
	assert.Equal(t, "1280x720", summary["size"])
	assert.Equal(t, "9:16", summary["ratio"], "metadata.ratio 要优先于顶层空值")
	assert.Equal(t, int64(42), summary["seed"])
}

// 参考视频可能藏在五种容器里，日志必须与「客户实际发过的字段」一致。
//
// 少收一处，排查时就会得出「他没传参考视频」的错误结论 —— 这正是
// 2026-11-11 误判 422 的根因。
func TestBuildTaskRequestParamsSummary_MergesMediaFromAllContainers(t *testing.T) {
	req := mustParseTaskRequest(t, `{
		"model":"seedance2.5-活动",
		"prompt":"x",
		"media":[
			{"type":"reference_video","url":"https://a.example/v1.mp4"},
			{"type":"reference_image","url":"https://a.example/i1.png"}
		],
		"images":["https://a.example/l1.png","https://a.example/l2.png"],
		"videos":["https://a.example/v2.mp4"],
		"audios":["https://a.example/a1.mp3"],
		"metadata":{
			"media":[{"type":"reference_audio","url":"https://a.example/m1.mp3"}],
			"input":{"media":[{"type":"reference_image","url":"https://a.example/mi.png"}]}
		}
	}`)

	summary := BuildTaskRequestParamsSummary(req)

	media, ok := summary["media"].([]map[string]string)
	require.True(t, ok, "media 必须是可直接渲染的 type/url 列表")
	assert.Len(t, media, 8, "五种容器里的素材都要收进摘要")

	counts := mediaCounts(summary)
	assert.Equal(t, 4, counts["reference_image"], "media + images + metadata.input.media 的图都要算")
	assert.Equal(t, 2, counts["reference_video"])
	assert.Equal(t, 2, counts["reference_audio"])

	fields := fieldCounts(summary)
	assert.Equal(t, 2, fields["media"])
	assert.Equal(t, 2, fields["images"])
	assert.Equal(t, 1, fields["videos"])
	assert.Equal(t, 1, fields["audios"])
	assert.Equal(t, 1, fields["metadata_media"])
	assert.Equal(t, 1, fields["metadata_input_media"])
}

// 单值兼容字段（image / video / audio / input_reference）同样会进上游。
func TestBuildTaskRequestParamsSummary_CollectsLegacySingleFields(t *testing.T) {
	req := mustParseTaskRequest(t, `{
		"model":"m",
		"image":"https://a.example/i.png",
		"video":"https://a.example/v.mp4",
		"audio":"https://a.example/a.mp3",
		"input_reference":"https://a.example/ir.png"
	}`)

	summary := BuildTaskRequestParamsSummary(req)

	counts := mediaCounts(summary)
	assert.Equal(t, 2, counts["reference_image"], "image 与 input_reference 都属于图片素材")
	assert.Equal(t, 1, counts["reference_video"])
	assert.Equal(t, 1, counts["reference_audio"])

	// input_reference 会被并进 Images，字段统计不能因此谎报「用了 images」。
	fields := fieldCounts(summary)
	assert.NotContains(t, fields, "images")
	assert.Equal(t, 1, fields["image"])
	assert.Equal(t, 1, fields["input_reference"])
}

// data URI 如果原样落库，单条可达数 MB，会把日志行撑爆。
func TestBuildTaskRequestParamsSummary_CompactsDataURI(t *testing.T) {
	req := mustParseTaskRequest(t, `{
		"model":"m",
		"images":["data:image/png;base64,AAAA"]
	}`)

	summary := BuildTaskRequestParamsSummary(req)

	media, ok := summary["media"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, media, 1)
	assert.Equal(t, "data:image/png;base64,<3 bytes>", media[0]["url"])
}

// 长剧本可能上万字，日志只留可辨认的前 200 字。
func TestBuildTaskRequestParamsSummary_TruncatesPrompt(t *testing.T) {
	prompt := strings.Repeat("字", 300)
	req := mustParseTaskRequest(t, `{"model":"m","prompt":"`+prompt+`"}`)

	summary := BuildTaskRequestParamsSummary(req)

	got, _ := summary["prompt"].(string)
	assert.True(t, strings.HasSuffix(got, "…(truncated)"))
	assert.Equal(t, 200, len([]rune(strings.TrimSuffix(got, "…(truncated)"))), "只保留前 200 字")
}

// 空请求没有任何可记录的信息，不要往日志里塞一个空对象。
func TestBuildTaskRequestParamsSummary_EmptyReturnsNil(t *testing.T) {
	req := mustParseTaskRequest(t, `{}`)
	assert.Nil(t, BuildTaskRequestParamsSummary(req))
	assert.Nil(t, BuildTaskRequestParamsSummary(nil))
}

// 比例可能写在四个位置，取值优先级要与适配器一致：
// metadata.ratio > metadata.aspect_ratio > 顶层 ratio > 顶层 aspect_ratio。
func TestBuildTaskRequestParamsSummary_RatioPriority(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "metadata.ratio 最高",
			body: `{"model":"m","ratio":"16:9","aspect_ratio":"4:3","metadata":{"ratio":"9:16","aspect_ratio":"1:1"}}`,
			want: "9:16",
		},
		{
			name: "metadata.aspect_ratio 次之",
			body: `{"model":"m","ratio":"16:9","aspect_ratio":"4:3","metadata":{"aspect_ratio":"1:1"}}`,
			want: "1:1",
		},
		{
			name: "顶层 ratio 再次",
			body: `{"model":"m","ratio":"16:9","aspect_ratio":"4:3"}`,
			want: "16:9",
		},
		{
			name: "顶层 aspect_ratio 兜底",
			body: `{"model":"m","aspect_ratio":"4:3"}`,
			want: "4:3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			summary := BuildTaskRequestParamsSummary(mustParseTaskRequest(t, tc.body))
			assert.Equal(t, tc.want, summary["ratio"])
		})
	}
}
