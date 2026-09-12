package megabyai

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestModelListContainsSD25(t *testing.T) {
	require.Equal(t, []string{"sd-2.5"}, (&TaskAdaptor{}).GetModelList())
	require.Equal(t, "MegaAI", (&TaskAdaptor{}).GetChannelName())
}

// sd-2.5 上游按次计费（¥6/次固定），EstimateBilling 必须返回 nil，
// 禁止带入 seconds/size 倍率，否则会按秒多扣费。
func TestEstimateBillingIsFixedPerUse(t *testing.T) {
	require.Nil(t, (&TaskAdaptor{}).EstimateBilling(nil, nil))
}

func TestParseTaskResultCapturesVideoURL(t *testing.T) {
	adaptor := &TaskAdaptor{}

	completed, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"video_url":"https://cdn.example/v.mp4"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, completed.Status)
	require.Equal(t, "https://cdn.example/v.mp4", completed.Url)

	nested, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"completed","progress":100,"data":{"url":"https://cdn.example/n.mp4"}}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusSuccess, nested.Status)
	require.Equal(t, "https://cdn.example/n.mp4", nested.Url)

	queued, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","task_id":"video_1","status":"queued"}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusQueued, queued.Status)

	running, err := adaptor.ParseTaskResult([]byte(`{"id":"video_1","status":"in_progress","progress":30}`))
	require.NoError(t, err)
	require.Equal(t, model.TaskStatusInProgress, running.Status)
}
