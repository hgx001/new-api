package youkou

import (
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskdoubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// ChannelName 是后台展示名
const ChannelName = "Youkou"

// ModelList 只保留 h3 + wan3.0
var ModelList = []string{
	"aliyun:wan-3.0",
	"hailuo-h3-cankaosheng-fast",
	"hailuo-h3-cankaosheng-night",
}

// volcengineModels 已清空（seedance 系列已下线，仅保留 h3/wan3.0 走 sora 适配器）
var volcengineModels = map[string]bool{}

// TaskAdaptor 是一个 facade：根据模型名把请求分发到现成的 Sora / Doubao 适配器，
// 从而复用 new-api 完整的视频任务链路（提交 / 轮询 / OpenAI 视频格式转换 / 结果代理）。
//
//   - openai-video 端点 (aliyun:wan-3.0, hailuo-*)            -> Sora 适配器  (POST {base}/v1/videos)
//   - volcengine-ark-video 已下线（原 seedance/sd2.5 已移除）
//
// 两种端点的请求/响应字段均已对 youkou.cc 实测兼容，无需新增转发逻辑。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	sub channel.TaskAdaptor
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.selectSub(info.OriginModelName)
	a.sub.Init(info)
}

// selectSub 按模型名选择子适配器。
func (a *TaskAdaptor) selectSub(modelName string) {
	if volcengineModels[modelName] {
		a.sub = &taskdoubao.TaskAdaptor{}
	} else {
		a.sub = &tasksora.TaskAdaptor{}
	}
}

// ensureSub 保证 sub 已初始化。提交路径会先调 Init，但查询/轮询路径
// （FetchTask / ParseTaskResult / ConvertToOpenAIVideo）拿到的 adaptor 未经过
// Init，sub 为 nil，直接使用会空指针 panic。sora/doubao 的查询与转换方法是
// 无状态的，未 Init 的 sub 在这些路径下可安全使用。
func (a *TaskAdaptor) ensureSub(modelName string) {
	if a.sub != nil {
		return
	}
	a.selectSub(modelName)
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return a.sub.ValidateRequestAndSetAction(c, info)
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	return a.sub.EstimateBilling(c, info)
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

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	a.ensureSub("")
	return a.sub.FetchTask(baseUrl, key, body, proxy)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	a.ensureSub("")
	return a.sub.ParseTaskResult(respBody)
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	a.ensureSub(originTask.Properties.OriginModelName)
	conv, ok := a.sub.(channel.OpenAIVideoConverter)
	if !ok {
		return nil, fmt.Errorf("adaptor %s does not support OpenAI video conversion", a.sub.GetChannelName())
	}
	return conv.ConvertToOpenAIVideo(originTask)
}
