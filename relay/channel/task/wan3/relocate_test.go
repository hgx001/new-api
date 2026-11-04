package wan3

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 转存请求体**不能**配 EnhanceConfig —— 那是超分的开关。
// 纯转存要求原分辨率、原画质，所以既不配超分也不配大模型增强。
func TestBuildRelocatePayloadHasNoEnhanceConfig(t *testing.T) {
	payload := buildRelocatePayload(mpsRelocateRequest{
		InputURL:         "https://upstream/expire-in-24h.mp4",
		Bucket:           "my-bucket-1250000000",
		Region:           "ap-guangzhou",
		OutputDir:        "/mps_api_test/",
		OutputObjectPath: "task_abc.mp4",
		SessionID:        "test-session",
	})

	rawParameter := payload["MediaProcessTask"].(map[string]any)["TranscodeTaskSet"].([]any)[0].(map[string]any)["RawParameter"].(map[string]any)
	_, hasEnhance := rawParameter["EnhanceConfig"]
	assert.False(t, hasEnhance, "纯转存不能配 EnhanceConfig（那是超分/大模型增强的开关）")
}

// ⚠️ 转存**不能**传 Width/Height —— 那两个值在超分链路里表示「目标输出尺寸」，
// 传了会把 1080P 缩成配置里的尺寸。纯转存必须保持输入原始分辨率。
func TestBuildRelocatePayloadKeepsSourceResolution(t *testing.T) {
	payload := buildRelocatePayload(mpsRelocateRequest{
		InputURL:  "https://upstream/expire-in-24h.mp4",
		Bucket:    "my-bucket-1250000000",
		Region:    "ap-guangzhou",
		OutputDir: "/mps_api_test/",
	})

	transcodeTask := payload["MediaProcessTask"].(map[string]any)["TranscodeTaskSet"].([]any)[0].(map[string]any)
	rawParameter := transcodeTask["RawParameter"].(map[string]any)
	videoTemplate := rawParameter["VideoTemplate"].(map[string]any)

	_, hasWidth := videoTemplate["Width"]
	_, hasHeight := videoTemplate["Height"]
	assert.False(t, hasWidth, "纯转存不能传 Width（会覆盖输入分辨率）")
	assert.False(t, hasHeight, "纯转存不能传 Height（会覆盖输入分辨率）")
	assert.Equal(t, "open", videoTemplate["ResolutionAdaptive"], "必须用 open 按输入自适应")
	assert.Equal(t, "black", videoTemplate["FillType"], "不能用 stretch —— 会把竖屏片压扁")
}

// 产物名必须按任务唯一，否则不同任务会写到同一个 COS 对象上互相覆盖
// （不报错，静默覆盖，用户拿到别人的片子）。
func TestRelocateObjectNameIsUniquePerTask(t *testing.T) {
	name1 := RelocateObjectName("task_abc123")
	name2 := RelocateObjectName("task_xyz789")
	assert.NotEqual(t, name1, name2)
	assert.Equal(t, "task_abc123.mp4", name1)
}

// 非法字符要清洗掉 —— 任务 ID 里出现特殊字符时不能生成非法对象名。
func TestRelocateObjectNameSanitizesIllegalChars(t *testing.T) {
	name := RelocateObjectName("task/abc 123")
	assert.NotContains(t, name, "/")
	assert.NotContains(t, name, " ")
}

func TestRelocateObjectNameFallsBackToTimestamp(t *testing.T) {
	name := RelocateObjectName("///")
	require.NotEmpty(t, name)
	assert.Contains(t, name, ".mp4")
}

// 转存与超分共用同一套 MPS 凭据与 COS 桶，但请求体不同：
// 超分配 EnhanceConfig 且写死 2 倍尺寸，转存不配且不写尺寸。
func TestRelocatePayloadDiffersFromSuperResPayload(t *testing.T) {
	relocate := buildRelocatePayload(mpsRelocateRequest{
		InputURL: "https://x/1.mp4", Bucket: "b", Region: "r", OutputDir: "/d/",
	})
	superRes := buildSuperResPayload(mpsSuperResRequest{
		InputURL: "https://x/1.mp4", Bucket: "b", Region: "r", OutputDir: "/d/",
		Width: 1280, Height: 720, SuperResolutionType: "hq",
	})

	relocateRaw := relocate["MediaProcessTask"].(map[string]any)["TranscodeTaskSet"].([]any)[0].(map[string]any)["RawParameter"].(map[string]any)
	superRaw := superRes["MediaProcessTask"].(map[string]any)["TranscodeTaskSet"].([]any)[0].(map[string]any)["RawParameter"].(map[string]any)

	_, relocateHasEnhance := relocateRaw["EnhanceConfig"]
	_, superHasEnhance := superRaw["EnhanceConfig"]
	assert.False(t, relocateHasEnhance)
	assert.True(t, superHasEnhance, "超分链路必须配 EnhanceConfig")
}
