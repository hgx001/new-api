package youkou

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 查询/轮询路径拿到的 adaptor 未经过 Init，sub 为 nil。
// 回归：ConvertToOpenAIVideo / ParseTaskResult 在此路径下不得 panic，且正确委托给 sora 适配器。
func TestQueryPathWithoutInit(t *testing.T) {
	adaptor := &TaskAdaptor{}

	task := &model.Task{
		TaskID: "task_regress001",
		Data:   json.RawMessage(`{"id":"task_upstream001","status":"completed","model":"aliyun:wan-3.0"}`),
	}
	task.Properties.OriginModelName = "aliyun:wan-3.0"

	out, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	assert.Contains(t, string(out), "task_regress001")

	info, err := adaptor.ParseTaskResult([]byte(`{"id":"x","status":"queued","progress":10}`))
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, model.TaskStatusQueued, info.Status)
}
