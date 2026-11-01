package wan3

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 产物名必须按任务唯一 —— 否则不同任务的 720P 会写到同一个 COS 对象上互相覆盖。
//
// 2026-11-01 真实事故：心笔渠道的成片地址是 .../v1/media/files/{id}/content，
// basename 恒为 "content"，腾讯云默认拿它当产物名，于是每个任务都产出
// /mps_api_test/content_transcode_0.mp4。两个不同用户的任务确实指向了同一个对象，
// 先完成的那个被后完成的覆盖掉，且**全程没有任何报错**。
func TestSuperResObjectNameIsUniquePerTask(t *testing.T) {
	a := superResObjectName("task_MJhf7cD1JYunhTXQXRswr904betpqri5")
	b := superResObjectName("task_9McV9BdsBH8UHXzBJ2GgrSTE729J53ZJ")

	assert.Equal(t, "sr_task_MJhf7cD1JYunhTXQXRswr904betpqri5.mp4", a)
	assert.NotEqual(t, a, b, "不同任务必须得到不同的产物名")
	assert.True(t, strings.HasSuffix(a, ".mp4"))
}

func TestSuperResObjectNameSanitizesAndFallsBack(t *testing.T) {
	// 非法字符一律替换成下划线：COS 对象名要能直接拼进路径。
	assert.Equal(t, "sr_task____1.mp4", superResObjectName("task/中文:1"))

	// 空 hint 也要能跑 —— 退化成时间戳，至少不会互相覆盖。
	empty := superResObjectName("")
	assert.True(t, strings.HasPrefix(empty, "sr_"))
	assert.True(t, strings.HasSuffix(empty, ".mp4"))
	assert.Greater(t, len(empty), len("sr_.mp4"))

	// 超长 hint 截断，保证整个对象名在 64 字符内。
	long := superResObjectName(strings.Repeat("a", 200))
	assert.LessOrEqual(t, len(long), 64)
}

// OutputObjectPath 必须真的进到 TranscodeTaskSet 的那一项里。
// 放在别的层级腾讯云会直接忽略（不报错），等于没修。
func TestBuildSuperResPayloadCarriesOutputObjectPath(t *testing.T) {
	payload := buildSuperResPayload(mpsSuperResRequest{
		InputURL:            "https://cdn.example.com/v1/media/files/x/content",
		Bucket:              "mps-test-1397292858",
		Region:              "ap-guangzhou",
		OutputDir:           "/mps_api_test/",
		OutputObjectPath:    "sr_task_abc.mp4",
		Width:               1280,
		Height:              720,
		BitrateKbps:         5000,
		SuperResolutionType: "hq",
		SessionID:           "heibaidao-1",
	})

	mediaProcessTask, ok := payload["MediaProcessTask"].(map[string]any)
	require.True(t, ok)
	taskSet, ok := mediaProcessTask["TranscodeTaskSet"].([]any)
	require.True(t, ok)
	require.Len(t, taskSet, 1)
	task, ok := taskSet[0].(map[string]any)
	require.True(t, ok)

	assert.Equal(t, "sr_task_abc.mp4", task["OutputObjectPath"])

	// 顺手锁住几个容易配错的项，避免以后改坏。
	raw, ok := task["RawParameter"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "mp4", raw["Container"])
	video := raw["VideoTemplate"].(map[string]any)
	assert.Equal(t, "black", video["FillType"], "stretch 会把竖屏片压扁")
	assert.Equal(t, "open", video["ResolutionAdaptive"])
	assert.Equal(t, 1280, video["Width"])
	assert.Equal(t, 720, video["Height"])
	enhance := raw["EnhanceConfig"].(map[string]any)["VideoEnhance"].(map[string]any)
	sr := enhance["SuperResolution"].(map[string]any)
	assert.Equal(t, "ON", sr["Switch"])
	assert.Equal(t, 2, sr["Size"])
}

func TestBuildSuperResPayloadOmitsEmptyOutputObjectPath(t *testing.T) {
	payload := buildSuperResPayload(mpsSuperResRequest{
		InputURL:  "https://cdn.example.com/a.mp4",
		OutputDir: "/mps_api_test/",
		Width:     1280,
		Height:    720,
	})
	task := payload["MediaProcessTask"].(map[string]any)["TranscodeTaskSet"].([]any)[0].(map[string]any)
	_, exists := task["OutputObjectPath"]
	assert.False(t, exists, "留空时不下发该字段，保持腾讯云默认命名")
}
