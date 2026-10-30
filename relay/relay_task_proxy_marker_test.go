package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// 对外出口的成片地址：内部占位符必须换掉
//
// 库里存的是内部协议占位符（cos-proxy://bucket/region/path）——
// 因为对象存储的直链是带签名的、有有效期的，任务完成后用户可能过一会儿
// 才来取片，把签名直链直接落库，到期后用户就拿不到成片。
//
// 但占位符是**内部协议**，客户端拿到没法下载。它出现在「下载地址」字段里
// 等于交付失败 —— 而用户已经为这条成片付过钱了。
// ─────────────────────────────────────────────────────────────────────────────

// TestTaskModel2DtoTranslatesProxyMarker 锁定「占位符 → 本站代理地址」。
func TestTaskModel2DtoTranslatesProxyMarker(t *testing.T) {
	task := &model.Task{
		TaskID: "task_cos",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "wan3.0-720p-特惠",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: taskcommon.BuildCOSProxyMarker(
				"my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task_cos.mp4"),
		},
	}

	out := TaskModel2Dto(task)
	require.NotNil(t, out)
	assert.Equal(t, taskcommon.BuildProxyURL(task.TaskID), out.ResultURL,
		"占位符必须换成本站代理地址")
	assert.NotContains(t, out.ResultURL, "cos-proxy://",
		"内部标记不能出现在给客户端的响应里")
	assert.NotContains(t, out.ResultURL, "my-bucket-1250000000",
		"桶名属于内部实现，不该给客户端")
}

// TestTaskModel2DtoForAdminKeepsProxyMarker 锁定管理员界面看到库内原文。
//
// 管理员要能一眼看到产物落在哪个桶/哪个路径（排查、对账用得上）。
func TestTaskModel2DtoForAdminKeepsProxyMarker(t *testing.T) {
	marker := taskcommon.BuildCOSProxyMarker(
		"my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task_cos.mp4")
	task := &model.Task{
		TaskID: "task_cos_admin",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "wan3.0-720p-特惠",
		},
		PrivateData: model.TaskPrivateData{ResultURL: marker},
	}

	assert.Equal(t, marker, TaskModel2DtoForAdmin(task).ResultURL,
		"管理员界面保留库内原文")
}

// TestTaskModel2DtoSerializedBodyHasNoProxyMarker 锁定整个响应体里都不含内部标记。
//
// 只断言 result_url 不够 —— 下游是从整个响应体里翻的，任何一个字段漏出去都算。
func TestTaskModel2DtoSerializedBodyHasNoProxyMarker(t *testing.T) {
	task := &model.Task{
		TaskID: "task_cos_body",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "wan3.0-720p-特惠",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: taskcommon.BuildCOSProxyMarker(
				"my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task_cos.mp4"),
		},
	}

	encoded, err := common.Marshal(TaskModel2Dto(task))
	require.NoError(t, err)
	body := string(encoded)
	assert.NotContains(t, body, "cos-proxy://", "内部标记不能出现在给客户端的响应体里")
	assert.NotContains(t, body, "my-bucket-1250000000", "桶名不能出现在给客户端的响应体里")
}
