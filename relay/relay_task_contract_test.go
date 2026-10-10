package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TaskModel2Dto 承载了文本型任务的交付链路。
// AGENTS.md 记的契约：relaycommon.TaskInfo.ResultText → model.TaskPrivateData.ResultText
// → dto.TaskDto.ResultText，OpenAI video 响应里最终落在 metadata.prompt。
// 任一环断了，jimeng-video-reverse 会「任务成功但结果为空」——不报错、不退款、用户只看到空。
func TestTaskModel2DtoCarriesTextResultAndProxyURL(t *testing.T) {
	task := &model.Task{
		ID:       7,
		TaskID:   "task_abc",
		UserId:   36,
		Status:   model.TaskStatusSuccess,
		FailReason: "",
		PrivateData: model.TaskPrivateData{
			ResultURL:  "/v1/videos/task_abc/content",
			ResultText: "分镜1：晨光中的山谷，镜头缓推",
		},
	}

	out := TaskModel2Dto(task)
	require.NotNil(t, out)
	assert.Equal(t, "分镜1：晨光中的山谷，镜头缓推", out.ResultText,
		"文本结果必须透传，否则文本型任务交付为空")
	assert.Equal(t, "/v1/videos/task_abc/content", out.ResultURL,
		"托管产物的相对路径要原样保留，由 /content 代理回源")
	assert.Equal(t, "task_abc", out.TaskID)
	assert.Equal(t, string(model.TaskStatusSuccess), out.Status)
}

// ResultURL 的实际取值走 GetResultURL（PrivateData 优先、FailReason 兜底旧数据），
// DTO 必须用同一个函数而不是直读字段，否则历史任务的产物地址会丢。
func TestTaskModel2DtoUsesGetResultURLNotRawField(t *testing.T) {
	task := &model.Task{TaskID: "task_old"}
	task.PrivateData.ResultURL = ""

	// 旧任务没有 ResultURL，FailReason 里存着地址
	task.FailReason = "https://cdn.example.com/legacy.mp4"
	assert.Equal(t, task.GetResultURL(), TaskModel2Dto(task).ResultURL)
	assert.Equal(t, "https://cdn.example.com/legacy.mp4", TaskModel2Dto(task).ResultURL)

	// 两者都有时以 ResultURL 为准
	task.PrivateData.ResultURL = "/v1/videos/task_old/content"
	assert.Equal(t, "/v1/videos/task_old/content", TaskModel2Dto(task).ResultURL)
}

// 对外状态取值是客户端轮询时看到的契约：SUCCESS/failed 必须明确区分，
// 未终结的状态统一落在 processing，不能出现空串。
func TestMapTaskStatusToSimpleIsAClosedContract(t *testing.T) {
	for _, tt := range []struct {
		in   model.TaskStatus
		want string
	}{
		{model.TaskStatusSuccess, "succeeded"},
		{model.TaskStatusFailure, "failed"},
		{model.TaskStatusQueued, "queued"},
		{model.TaskStatusSubmitted, "queued"},
		{model.TaskStatusInProgress, "processing"},
		{model.TaskStatusUnknown, "processing"},
	} {
		got := mapTaskStatusToSimple(tt.in)
		assert.Equal(t, tt.want, got, "status=%v", tt.in)
		assert.NotEmpty(t, got, "对外状态不能是空串，客户端会陷入未知态")
	}
}

// detectVideoFormat 的兜底必须是 mp4（兼容性最好的容器），
// 畸形报文、空 videos、缺 mimeType 都不能返回空串或垃圾值。
func TestDetectVideoFormatFallsBackToMP4(t *testing.T) {
	for _, name := range []string{
		"畸形 JSON", "无 response", "videos 非数组", "videos 为空", "首元素非对象", "mimeType 非字符串", "mimeType 为空",
	} {
		t.Run(name, func(t *testing.T) {
			cases := map[string]string{
				"畸形 JSON":      `{`,
				"无 response":    `{"foo":1}`,
				"videos 非数组":   `{"response":{"videos":"x"}}`,
				"videos 为空":     `{"response":{"videos":[]}}`,
				"首元素非对象":     `{"response":{"videos":[1,2]}}`,
				"mimeType 非字符串": `{"response":{"videos":[{"mimeType":123}]}}`,
				"mimeType 为空":   `{"response":{"videos":[{"mimeType":""}]}}`,
			}
			assert.Equal(t, "mp4", detectVideoFormat([]byte(cases[name])))
		})
	}
	// 正常形态
	assert.Equal(t, "mp4", detectVideoFormat([]byte(`{"response":{"videos":[{"mimeType":"video/mp4"}]}}`)))
	// 非 mp4 的真实容器要如实返回，不能一律吞成 mp4
	assert.Equal(t, "video/quicktime",
		detectVideoFormat([]byte(`{"response":{"videos":[{"mimeType":"video/quicktime"}]}}`)))
}

var _ = dto.TaskDto{}