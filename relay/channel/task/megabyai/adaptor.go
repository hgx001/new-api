package megabyai

import (
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// TaskAdaptor 是一个 facade：megabyai 上游走标准 openai-video 协议
// （POST {base}/v1/videos 提交，GET {base}/v1/videos/{id} 轮询），
// 因此转发/轮询/转换全部复用 Sora 适配器，仅覆盖两处：
//
//  1. EstimateBilling 返回 nil —— sd-2.5 上游按次计费（¥6/次固定），
//     禁止带入 Sora 的 seconds/size 倍率，否则会按秒多扣费。
//  2. ParseTaskResult 在成功时回填 video_url —— Sora 上游走自有
//     /content 端点所以故意留空 Url，而 megabyai 轮询直接返回成片
//     URL，必须写入 ResultURL，否则本站 /content 代理无地址可抓。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	sub channel.TaskAdaptor
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.sub = &tasksora.TaskAdaptor{}
	a.sub.Init(info)
}

func (a *TaskAdaptor) ensureSub() {
	if a.sub != nil {
		return
	}
	a.sub = &tasksora.TaskAdaptor{}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return a.sub.ValidateRequestAndSetAction(c, info)
}

func (a *TaskAdaptor) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return a.sub.AdjustBillingOnSubmit(info, taskData)
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return a.sub.AdjustBillingOnComplete(task, taskResult)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.sub.BuildRequestURL(info)
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	return a.sub.BuildRequestHeader(c, req, info)
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	return a.sub.BuildRequestBody(c, info)
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return a.sub.DoRequest(c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, err *dto.TaskError) {
	return a.sub.DoResponse(c, resp, info)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	a.ensureSub()
	return a.sub.FetchTask(baseURL, key, body, proxy)
}

type megaVideoResult struct {
	VideoURL string `json:"video_url"`
	Data     struct {
		URL string `json:"url"`
	} `json:"data"`
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	a.ensureSub()
	result, err := a.sub.ParseTaskResult(respBody)
	if err != nil {
		return nil, err
	}
	if result.Status == model.TaskStatusSuccess && result.Url == "" {
		var parsed megaVideoResult
		if err := common.Unmarshal(respBody, &parsed); err == nil {
			if parsed.VideoURL != "" {
				result.Url = parsed.VideoURL
			} else if parsed.Data.URL != "" {
				result.Url = parsed.Data.URL
			}
		}
	}
	return result, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	a.ensureSub()
	return a.sub.(channel.OpenAIVideoConverter).ConvertToOpenAIVideo(originTask)
}
