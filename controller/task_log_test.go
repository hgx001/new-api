package controller

import (
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 内联 base64 必须压成摘要：原样落库既撑表，又会把快照挤爆成非法 JSON。
func TestCompactMediaURLKeepsDataURLAsDigest(t *testing.T) {
	// 4 个 base64 字符还原 3 字节，1200 字符 = 900 字节。
	raw := "data:image/png;base64," + strings.Repeat("A", 1200)
	assert.Equal(t, "data:image/png;base64,<900 bytes>", compactMediaURL(raw))

	// 非 base64 的 data URL（少见，但协议允许）按字符数记。
	assert.Equal(t, "data:image/svg+xml,<10 chars>", compactMediaURL("data:image/svg+xml,0123456789"))

	// 普通 URL 原样保留 —— 它可能是复现问题的唯一线索。
	url := "https://media.example.com/refs/a.png?tk=secret"
	assert.Equal(t, url, compactMediaURL(url))

	assert.Equal(t, "data:<malformed>", compactMediaURL("data:image/png;base64"))
	assert.Equal(t, "", compactMediaURL("   "))
}

// 关键回归：media 与 images 同时出现时，两者都要记下来。
//
// 适配器按 media > images 取第一个非空的，images 会被静默忽略；快照若只记生效
// 的那份，事后就分不清「客户端没传图」和「传了但被 media 挡住」。
func TestRequestMediaFieldsRecordsEveryField(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media: []relaycommon.TaskMedia{
			{Type: "reference_audio", URL: "https://example.com/a.mp3"},
			{Type: "reference_audio", URL: "https://example.com/b.mp3"},
		},
		Images:         []string{"https://example.com/1.png", "https://example.com/2.png"},
		Image:          "https://example.com/3.png",
		InputReference: "https://example.com/4.png",
	}
	fields := requestMediaFields(req)
	assert.Equal(t, map[string]int{"media": 2, "images": 2, "image": 1, "input_reference": 1}, fields)

	assert.Empty(t, requestMediaFields(relaycommon.TaskSubmitReq{}), "纯文生视频不应产生该字段")
}

func TestMediaTypeSummaryCountsByType(t *testing.T) {
	media := []map[string]string{
		{"type": "image", "url": "a"},
		{"type": "image", "url": "b"},
		{"type": "reference_audio", "url": "c"},
	}
	assert.Equal(t, map[string]int{"image": 2, "reference_audio": 1}, mediaTypeSummary(media))
	assert.Empty(t, mediaTypeSummary(nil))
}

// 快照里的 media 必须与适配器实际送上游的那份一致：media 与 legacy 合并，
// 不再「media 非空就把 images 吞掉」。
func TestCollectRequestMediaMergesMediaAndImages(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media:  []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images: []string{"https://example.com/1.png", "https://example.com/2.png"},
	}
	media := collectRequestMedia(req)
	require.Len(t, media, 3)
	assert.Equal(t, "reference_audio", media[0]["type"])
	assert.Equal(t, "image", media[1]["type"])
	assert.Equal(t, "https://example.com/2.png", media[2]["url"])

	// media 为空时 legacy 照旧生效。
	fallback := collectRequestMedia(relaycommon.TaskSubmitReq{Images: []string{"https://example.com/1.png"}})
	require.Len(t, fallback, 1)
	assert.Equal(t, "image", fallback[0]["type"])
}

// legacy 三选一，不重复计入。
func TestCollectRequestMediaLegacyFieldsDoNotStack(t *testing.T) {
	req := relaycommon.TaskSubmitReq{
		Media:          []relaycommon.TaskMedia{{Type: "reference_audio", URL: "https://example.com/a.mp3"}},
		Images:         []string{"https://example.com/from-images.png"},
		Image:          "https://example.com/from-image.png",
		InputReference: "https://example.com/from-input-ref.png",
	}
	media := collectRequestMedia(req)
	require.Len(t, media, 2)
	assert.Equal(t, "https://example.com/from-images.png", media[1]["url"])
}
