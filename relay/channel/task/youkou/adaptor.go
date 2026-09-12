package youkou

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskdoubao "github.com/QuantumNous/new-api/relay/channel/task/doubao"
	taskmegabyai "github.com/QuantumNous/new-api/relay/channel/task/megabyai"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ChannelName 是后台展示名
const ChannelName = "Youkou"

// ModelList 只保留 h3 + wan3.0 + 按次计费的 seedance2.5（openai-video 协议）
var ModelList = []string{
	"aliyun:wan-3.0",
	"hailuo-h3-cankaosheng-fast",
	"hailuo-h3-cankaosheng-night",
	"seedance2.5特惠版",
}

// fixedPriceModels 按次计费的 openai-video 模型：目录 pricing.mode=fixed/unit=request。
// 出处：GET /v1/catalog（catalog-v1-a4b7eaff2a16f6ee02e4209d），
// seedance2.5 family hm-seedance-2-5-discount，capability_status available。
// 这些模型复用 sora 协议子适配器做传输（POST {base}/v1/videos），但必须用
// megabyai 式按次计费覆盖（EstimateBilling=nil），禁止带入 sora 的
// seconds/size 倍率，否则多秒任务会被扣到上百元。
var fixedPriceModels = map[string]bool{
	"seedance2.5特惠版": true,
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

// selectSub 按模型名选择子适配器：按次计费模型优先，其次 ark 模型，最后默认 sora。
func (a *TaskAdaptor) selectSub(modelName string) {
	if fixedPriceModels[modelName] {
		a.sub = &taskmegabyai.TaskAdaptor{}
		return
	}
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
	if err := a.sub.ValidateRequestAndSetAction(c, info); err != nil {
		return err
	}
	if fixedPriceModels[info.OriginModelName] {
		return validateFixedPriceRequest(c, info)
	}
	return nil
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
	if err := a.sub.BuildRequestHeader(c, req, info); err != nil {
		return err
	}
	// 上游要求每次业务提交带新的 Idempotency-Key；调用方自带的 Key 予以保留，
	// 网络超时重试时框架会复用同一请求（同 Key 同正文），避免重复建单。
	if req.Header.Get("Idempotency-Key") == "" {
		req.Header.Set("Idempotency-Key", uuid.NewString())
	}
	return nil
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

// seedance 2.5 目录约束（GET /v1/catalog，catalog-v1-a4b7eaff2a16f6ee02e4209d）：
// 720p 单分辨率、4~30 秒、images≤30/videos≤10/audios≤10。
// 平台约定每个提示词最多 5,000,000 个 Unicode 字符，超限本地拦截。
const (
	seedance25MinDuration = 4
	seedance25MaxDuration = 30
	seedance25MaxImages   = 30
	seedance25MaxVideos   = 10
	seedance25MaxAudios   = 10
	maxPromptRunes        = 5000000
)

var seedance25AspectRatios = map[string]bool{
	"21:9": true, "16:9": true, "3:2": true, "4:3": true,
	"1:1": true, "3:4": true, "2:3": true, "9:16": true,
}

// validateFixedPriceRequest 按目录校验按次计费模型的提交参数，非法组合在
// 本地拦截（目录 parameters/capabilities 为准，未声明的不猜测、不转发）。
func validateFixedPriceRequest(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	reject := func(format string, args ...any) *dto.TaskError {
		return service.TaskErrorWrapperLocal(fmt.Errorf(format, args...), "invalid_request", http.StatusBadRequest)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return reject("read_task_request_failed")
	}
	if utf8.RuneCountInString(req.Prompt) > maxPromptRunes {
		return reject("prompt exceeds %d unicode characters", maxPromptRunes)
	}
	if req.Duration != 0 && (req.Duration < seedance25MinDuration || req.Duration > seedance25MaxDuration) {
		return reject("duration must be between %d and %d seconds", seedance25MinDuration, seedance25MaxDuration)
	}
	if req.Seconds != "" {
		seconds, err := strconv.Atoi(req.Seconds)
		if err != nil || seconds < seedance25MinDuration || seconds > seedance25MaxDuration {
			return reject("seconds must be between %d and %d", seedance25MinDuration, seedance25MaxDuration)
		}
	}
	if len(req.Images) > seedance25MaxImages {
		return reject("at most %d reference images allowed", seedance25MaxImages)
	}
	if len(req.Audios) > seedance25MaxAudios {
		return reject("at most %d reference audios allowed", seedance25MaxAudios)
	}
	// videos[] 与 ratio 不在 TaskSubmitReq 结构里（sora 透传原 JSON 上游），
	// 从原始请求体读取校验，避免静默转发非法组合。
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return reject("read_request_body_failed")
	}
	raw, err := storage.Bytes()
	if err != nil {
		return reject("read_request_body_failed")
	}
	var bodyMap map[string]any
	if err := common.Unmarshal(raw, &bodyMap); err != nil {
		return reject("invalid_json")
	}
	if v, ok := bodyMap["videos"]; ok && v != nil {
		arr, ok := v.([]any)
		if !ok {
			return reject("videos must be an array of urls")
		}
		if len(arr) > seedance25MaxVideos {
			return reject("at most %d reference videos allowed", seedance25MaxVideos)
		}
	}
	if v, ok := bodyMap["ratio"]; ok && v != nil {
		s, ok := v.(string)
		if !ok || !seedance25AspectRatios[s] {
			return reject("ratio must be one of 21:9, 16:9, 3:2, 4:3, 1:1, 3:4, 2:3, 9:16")
		}
	}
	return nil
}
