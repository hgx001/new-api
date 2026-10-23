package relay

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

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

// 别名模型（对外 seedance2.5-特惠、上游 wan-3.0）的任务 DTO 不得暴露真实上游：
// properties.upstream_model_name / data（上游回执）/ fail_reason / result_url 四处都要处理。
//
// 成片直链 result_url 改成指向本站 /content 代理：直链里带着上游存储域名与货源目录
// （R2 域名 + hailuo-video 路径），原样给下游等于直接报出真实上游是谁。
// 代理回源的是库里的原始地址，交付行为不变（管理员仍看得到原始直链）。
func TestTaskModel2DtoAliasModelHidesUpstream(t *testing.T) {
	task := &model.Task{
		ID:     619,
		TaskID: "task_W6qbKbmn4LuExWRTapnwnvS08ukY9uDx",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://d4c280ea2c37b44805c584296cb271a0.r2.cloudflarestorage.com/hailuo-video/vid_1.m4v",
		},
		FailReason: "任务失败：model aliyun:wan-3.0 does not support the requested output combination",
		Data:       json.RawMessage(`{"id":"task_36be6a9c56a14eebbeb1573c43abbf7e","billing_amount":1.8,"model":"wan-3.0"}`),
	}

	out := TaskModel2Dto(task)
	require.NotNil(t, out)

	props, ok := out.Properties.(model.Properties)
	require.True(t, ok, "properties 结构不能变")
	assert.Equal(t, "seedance2.5-特惠", props.UpstreamModelName,
		"上游模型名必须换成对外别名，否则下游一眼看出真实上游")
	assert.Equal(t, "seedance2.5-特惠", props.OriginModelName)

	assert.NotContains(t, out.FailReason, "aliyun")
	assert.NotContains(t, out.FailReason, "wan-3.0")
	assert.Contains(t, out.FailReason, "上游")

	assert.Nil(t, out.Data, "上游原始回执（含上游任务号与上游成本）不给下游")

	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), out.ResultURL,
		"成片直链必须改指向本站 /content 代理")
	assert.NotContains(t, out.ResultURL, "r2.cloudflarestorage.com", "上游存储域名不能给下游")
	assert.NotContains(t, out.ResultURL, "hailuo-video", "上游货源目录不能给下游")
}

// 管理员界面保留上游原文：管理员要能一眼看到真实货源（下游用户看不到）。
func TestTaskModel2DtoForAdminKeepsUpstream(t *testing.T) {
	task := &model.Task{
		TaskID: "task_alias_admin",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
		FailReason: "model aliyun:wan-3.0 does not support",
		Data:       json.RawMessage(`{"billing_amount":1.8}`),
	}

	out := TaskModel2DtoForAdmin(task)
	props, ok := out.Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, "wan-3.0", props.UpstreamModelName, "管理员要能看到真实上游")
	assert.Contains(t, out.FailReason, "aliyun")
	assert.NotNil(t, out.Data, "上游回执留给管理员")
}

// 非别名模型：一切照旧，遮字样只会让报错更难读。
func TestTaskModel2DtoNonAliasModelKeepsEverything(t *testing.T) {
	task := &model.Task{
		TaskID: "task_wan30",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "wan-3.0",
		},
		FailReason: "model aliyun:wan-3.0 does not support",
		Data:       json.RawMessage(`{"billing_amount":1.8}`),
	}

	out := TaskModel2Dto(task)
	props, ok := out.Properties.(model.Properties)
	require.True(t, ok)
	assert.Equal(t, "wan-3.0", props.UpstreamModelName)
	assert.Contains(t, out.FailReason, "aliyun")
	assert.NotNil(t, out.Data)
}

// 上游成片直链的形状（真机回执里的原样）：R2 域名 + hailuo-video 货源目录 + 签名参数。
const aliasUpstreamResultURL = "https://d4c280ea2c37b44805c584296cb271a0.r2.cloudflarestorage.com/hailuo-video/aggregate/videos/vid_28c43b66c6495219c5bd01.m4v?X-Amz-Signature=8ca74d62d7fb9d90eff6882d04e060176f6c940bab998cbfbe517d99bf281a03"

// 别名模型的成片地址：整条 DTO 序列化后都不能出现上游存储域名与货源目录。
// 只断言 result_url 不够 —— 下游是从整个响应体里翻的，任何一个字段漏出去都算泄露。
func TestTaskModel2DtoAliasModelSerializedBodyHasNoUpstreamAddress(t *testing.T) {
	task := &model.Task{
		TaskID: "task_alias_addr",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
		PrivateData: model.TaskPrivateData{ResultURL: aliasUpstreamResultURL},
	}

	encoded, err := common.Marshal(TaskModel2Dto(task))
	require.NoError(t, err)
	body := string(encoded)
	assert.NotContains(t, body, "r2.cloudflarestorage.com", "上游存储域名不能出现在下游响应体里")
	assert.NotContains(t, body, "hailuo-video", "上游货源目录不能出现在下游响应体里")
}

// 别名模型的任务没跑完时 result_url 必须为空：指向 /content 代理会让客户端去拉
// 一个还不存在的文件（代理会回 400 "Task is not completed yet"）。
func TestTaskModel2DtoAliasModelUnfinishedTaskHasNoResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_pending",
		Status: model.TaskStatusInProgress,
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
	}

	assert.Empty(t, TaskModel2Dto(task).ResultURL, "任务还没出片，不能给下载地址")
}

// 失败任务的 result_url 也不能是失败文案。
//
// GetResultURL() 在没有产物时会回退到 FailReason —— 失败文案里带的是上游原文
// （"model aliyun:wan-3.0 does not support ..."），原样留在 result_url 里等于
// 在「下载地址」这个字段里报出真实上游。失败原因由 fail_reason 承载，result_url
// 只放下载地址，没有产物就是空。
func TestTaskModel2DtoAliasModelFailedTaskHasNoResultURL(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_failed",
		Status:     model.TaskStatusFailure,
		FailReason: "model aliyun:wan-3.0 does not support the requested output combination",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
	}

	out := TaskModel2Dto(task)
	assert.Empty(t, out.ResultURL, "失败任务没有产物，result_url 必须为空")
	assert.NotContains(t, out.FailReason, "aliyun", "失败原因脱敏后留给下游")
	assert.Contains(t, out.FailReason, "上游")
}

// 非别名模型：result_url 逐字不变（别的模型一个字不动）。
func TestTaskModel2DtoNonAliasModelKeepsResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_wan30",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "wan-3.0",
		},
		PrivateData: model.TaskPrivateData{ResultURL: aliasUpstreamResultURL},
	}

	assert.Equal(t, aliasUpstreamResultURL, TaskModel2Dto(task).ResultURL,
		"非别名模型逐字不变")
}

// 管理员：保留原始直链。管理员要能一眼看到成片的真实地址（排查、转存、对账都用得上）。
func TestTaskModel2DtoForAdminKeepsResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_alias_admin",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
		PrivateData: model.TaskPrivateData{ResultURL: aliasUpstreamResultURL},
	}

	assert.Equal(t, aliasUpstreamResultURL, TaskModel2DtoForAdmin(task).ResultURL,
		"管理员要看到原始直链")
}

// OpenAI Video API 的取任务响应（GET /v1/videos/{task_id}）走各渠道的
// ConvertToOpenAIVideo —— megabyai 那里返回的是**上游原始回执**，里面同时有
// 上游模型名（wan-3.0）、上游成片直链（R2 + hailuo-video）与上游成本（billing_*）。
// 这三样任何一样漏出去，别名就白起了，所以别名模型必须按对外口径重建响应体。
func TestRedactAliasOpenAIVideoHidesUpstream(t *testing.T) {
	upstreamReceipt := []byte(`{"id":"task_36be6a9c56a14eebbeb1573c43abbf7e","task_id":"task_36be6a9c56a14eebbeb1573c43abbf7e","object":"video","model":"wan-3.0","status":"completed","progress":100,"created_at":1792669696,"completed_at":1792670651,"url":"` + aliasUpstreamResultURL + `","metadata":{"final_video_url":"` + aliasUpstreamResultURL + `","video_url":"` + aliasUpstreamResultURL + `"},"billing_amount":1.8,"billing_currency":"CNY","billing_status":"settled"}`)

	task := &model.Task{
		TaskID:   "task_alias_video",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
		PrivateData: model.TaskPrivateData{ResultURL: aliasUpstreamResultURL},
	}

	got := redactAliasOpenAIVideo(task, upstreamReceipt)
	require.NotEmpty(t, got)
	body := string(got)
	assert.NotContains(t, body, "wan-3.0", "上游模型名不能出现在下游响应里")
	assert.NotContains(t, body, "r2.cloudflarestorage.com", "上游成片直链不能出现在下游响应里")
	assert.NotContains(t, body, "hailuo-video", "上游货源目录不能出现在下游响应里")
	assert.NotContains(t, body, "billing_amount", "上游成本不能出现在下游响应里")
	assert.NotContains(t, body, "task_36be6a9c56a14eebbeb1573c43abbf7e", "上游任务号不能出现在下游响应里")

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(got, &video))
	assert.Equal(t, "seedance2.5-特惠", video.Model, "模型名必须是对外别名")
	assert.Equal(t, "task_alias_video", video.ID)
	assert.Equal(t, dto.VideoStatusCompleted, video.Status)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), video.Metadata["url"],
		"成片地址必须是本站代理，下游拿不到上游直链")
}

// 失败任务：上游回执里只有提交时的 queued 快照（真正的原因在 fail_reason），
// 重建后必须按本站的任务状态报 failed，并把失败原因脱敏后带出去。
func TestRedactAliasOpenAIVideoFailedTaskCarriesRedactedReason(t *testing.T) {
	upstreamReceipt := []byte(`{"id":"task_36be6a9c56a14eebbeb1573c43abbf7e","model":"wan-3.0","status":"queued","progress":0}`)

	task := &model.Task{
		TaskID:     "task_alias_failed",
		Status:     model.TaskStatusFailure,
		FailReason: "model aliyun:wan-3.0 does not support the requested output combination",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "seedance2.5-特惠",
		},
	}

	got := redactAliasOpenAIVideo(task, upstreamReceipt)
	body := string(got)
	assert.NotContains(t, body, "wan-3.0")
	assert.NotContains(t, body, "aliyun")
	assert.Contains(t, body, "上游")

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(got, &video))
	assert.Equal(t, dto.VideoStatusFailed, video.Status)
	require.NotNil(t, video.Error)
	assert.Contains(t, video.Error.Message, "上游")
}

// 非别名模型：OpenAI Video 响应逐字不变（别的模型一个字不动）。
func TestRedactAliasOpenAIVideoNonAliasKeepsRaw(t *testing.T) {
	raw := []byte(`{"id":"task_x","model":"wan-3.0","url":"` + aliasUpstreamResultURL + `"}`)
	task := &model.Task{
		TaskID: "task_x",
		Properties: model.Properties{
			UpstreamModelName: "wan-3.0",
			OriginModelName:   "wan-3.0",
		},
	}

	assert.Equal(t, raw, redactAliasOpenAIVideo(task, raw), "非别名模型逐字不变")
}
