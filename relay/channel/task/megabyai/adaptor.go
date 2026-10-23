package megabyai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	tasksora "github.com/QuantumNous/new-api/relay/channel/task/sora"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// TaskAdaptor 是一个 facade：megabyai 上游走 openai-video 协议
// （POST {base}/v1/videos 提交，GET {base}/v1/videos/{id} 轮询），
// 因此转发 / 轮询 / 转换大部分复用 Sora 适配器，本文件只覆盖三处：
//
//  1. EstimateBilling 按模型分档 —— sd-2.5 是固定按次价（返回 nil，禁止带入 Sora 的
//     seconds/size 倍率，否则会按秒多扣费）；wan-3.0 是「按次 + 分辨率档」，
//     只带 size 倍率（上游按次计费，时长不参与计价）。
//  2. BuildRequestBody 对 wan-3.0 走上游「统一参数」契约（duration / ratio / resolution /
//     referenceImages ...），不把原始 JSON 透传过去。
//  3. ParseTaskResult 做成容错的 —— 上游的成片地址与错误字段形状都不固定，
//     严格解析一旦失败，任务会永远停在非终态：既看不到失败原因，也永远退不了款。
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

// usesUnifiedContract 判断本次请求是不是走上游「统一参数」契约的模型。
//
// wan-3.0 与它的对外别名 seedance2.5-特惠 都走这套契约：入参翻译成
// duration / ratio / resolution / referenceImages ...，不透传原始 JSON。
//
// ⚠️ 用**精确集合**判断而不是前缀：站内相近的 seedance 模型名很多，
// 前缀匹配会误命中别家渠道的枚举表。
func usesUnifiedContract(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	return megabyaiUnifiedContractModels[info.OriginModelName]
}

// isResolutionTiered 判断本次请求是不是**按分辨率档计价**。
//
// wan-3.0 是按次 + 分辨率档（480p ¥2.4 / 720p ¥3.0 / 1080p ¥4.0）；
// seedance2.5-特惠 是固定 ¥3.0/条，与分辨率、时长都无关。
func isResolutionTiered(info *relaycommon.RelayInfo) bool {
	if info == nil {
		return false
	}
	return megabyaiResolutionTierModels[info.OriginModelName]
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	a.ensureSub()
	if !usesUnifiedContract(info) {
		return a.sub.ValidateRequestAndSetAction(c, info)
	}
	if taskErr := a.sub.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	// 平台 Sora 语义会让 input_reference 覆盖整个 images 数组；wan-3.0 支持多参考图，
	// 两者并存时必须把 images 合并回来，否则用户付了钱却丢了自己的参考图。
	if restored, ok := wan30RestoreImages(c, req); ok {
		req = restored
		relaycommon.StoreTaskRequest(c, info, info.Action, req)
	}
	// 分辨率必须在本地归一：wan-3.0 按分辨率档计价，猜错档位就是少收钱；
	// 锁定档位的模型（seedance2.5-特惠）只认自己那一档，给别的档位直接 400。
	tier, locked := megabyaiLockedTierModels[info.OriginModelName]
	var resolution string
	if locked {
		resolution, err = resolveLockedResolution(req, tier)
	} else {
		resolution, err = resolveWan30Resolution(req)
	}
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_resolution", http.StatusBadRequest)
	}
	// 时长同理：锁定时长只认自己那一档。
	if locked {
		if _, err := wan30LockedDuration(req, tier); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
	}
	if _, _, _, err := buildWan30Media(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	// 解析出的档位写进 context：EstimateBilling 复用它，
	// 保证「计费档位」与「实际下发档位」永远是同一个值。
	c.Set(wan30ResolutionContextKey, resolution)
	return nil
}

// EstimateBilling 返回计费倍率。
//
//   - sd-2.5：固定按次价 → nil（禁止带入 seconds/size，否则会按秒多扣费）。
//   - wan-3.0：上游按次计价（时长不参与），只有分辨率档参与 → 只给 size 倍率。
//   - seedance2.5-特惠：固定 ¥3.0/条 → nil（不按分辨率分档，也不乘时长）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !isResolutionTiered(info) {
		return nil
	}
	resolution := ""
	if c != nil {
		if value, ok := c.Get(wan30ResolutionContextKey); ok {
			resolution, _ = value.(string)
		}
	}
	ratio := wan30ResolutionSizeRatio[resolution]
	if ratio == 0 {
		ratio = wan30ResolutionSizeRatio[wan30DefaultResolution]
	}
	return map[string]float64{"size": ratio}
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	a.ensureSub()
	return a.sub.AdjustBillingOnSubmit(info, taskData)
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	a.ensureSub()
	return a.sub.AdjustBillingOnComplete(task, taskResult)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	a.ensureSub()
	return a.sub.BuildRequestURL(info)
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	a.ensureSub()
	return a.sub.BuildRequestHeader(c, req, info)
}

// BuildRequestBody 构建提交体。
//
// wan-3.0 走上游「统一参数」契约：把本站通用入参（duration/seconds、size/resolution、
// images/media、audios、seed、ratio）翻译成上游那套字段；
// 其余模型沿用 Sora 适配器的透传行为。
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	a.ensureSub()
	if !usesUnifiedContract(info) {
		return a.sub.BuildRequestBody(c, info)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	tier, locked := megabyaiLockedTierModels[info.OriginModelName]
	var resolution string
	if locked {
		resolution, err = resolveLockedResolution(req, tier)
	} else {
		resolution, err = resolveWan30Resolution(req)
	}
	if err != nil {
		return nil, err
	}
	var duration *int
	if locked {
		duration, err = wan30LockedDuration(req, tier)
		if err != nil {
			return nil, err
		}
	} else {
		duration = wan30Duration(req)
	}
	images, videos, audios, err := buildWan30Media(req)
	if err != nil {
		return nil, err
	}
	modelName := resolveMegabyaiUpstreamModel(info)
	body := wan30Request{
		Model:           modelName,
		Prompt:          strings.TrimSpace(req.Prompt),
		Duration:        duration,
		Ratio:           resolveWan30Ratio(rawWan30Ratio(c), req),
		Resolution:      resolution,
		Seed:            req.Seed,
		ReferenceImages: images,
		ReferenceVideos: videos,
		ReferenceAudios: audios,
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(encoded), nil
}

// resolveMegabyaiUpstreamModel 决定下发给上游的 model 名。
//
// 顺序：
//  1. 渠道 model_mapping 生效时以它为准（管理员可以改上游指向）；
//  2. 没有映射、或映射回自己时用兜底表 —— **别名模型绝不能把对外名原样发给上游**
//     （上游不认识「seedance2.5-特惠」，会直接 400）；
//  3. 兜底到 wan-3.0。
func resolveMegabyaiUpstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return wan30Model
	}
	upstream := ""
	if info.ChannelMeta != nil {
		upstream = strings.TrimSpace(info.UpstreamModelName)
	}
	if upstream != "" && upstream != info.OriginModelName {
		return upstream
	}
	if mapped, ok := megabyaiUpstreamModels[info.OriginModelName]; ok {
		return mapped
	}
	if upstream != "" {
		return upstream
	}
	return wan30Model
}

// rawWan30Ratio 从原始请求体里取 ratio（本站通用入参里没有 ratio 字段，
// 但客户端习惯直接传，透传给上游时不能丢）。
func rawWan30Ratio(c *gin.Context) string {
	if c == nil {
		return ""
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return ""
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) == 0 {
		return ""
	}
	var bodyMap map[string]any
	if err := common.Unmarshal(raw, &bodyMap); err != nil {
		return ""
	}
	ratio, _ := bodyMap["ratio"].(string)
	return strings.TrimSpace(ratio)
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	a.ensureSub()
	return a.sub.DoRequest(c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, err *dto.TaskError) {
	a.ensureSub()
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

// megaVideoResult 兼容上游多种成片地址字段。
//
// 上游是 new-api 实例，轮询响应里成片地址可能出现在顶层 video_url / url / result_url，
// 也可能嵌在 data / output 里；不逐个兼容就会把「成片已出」判成「没有产物」。
type megaVideoResult struct {
	Status     string          `json:"status"`
	TaskStatus string          `json:"task_status"`
	Progress   int             `json:"progress"`
	VideoURL   string          `json:"video_url"`
	URL        string          `json:"url"`
	ResultURL  string          `json:"result_url"`
	FailReason string          `json:"fail_reason"`
	Error      json.RawMessage `json:"error"`
	Data       struct {
		URL       string `json:"url"`
		VideoURL  string `json:"video_url"`
		ResultURL string `json:"result_url"`
		Output    struct {
			VideoURL string `json:"video_url"`
		} `json:"output"`
	} `json:"data"`
	Output struct {
		VideoURL string `json:"video_url"`
	} `json:"output"`
}

// ParseTaskResult 解析轮询结果。
//
// 不用 Sora 适配器的严格解析当唯一来源：上游错误字段可能是对象、字符串或自定义结构，
// 严格解析失败会直接抛错，任务就永远停在非终态（看不到失败原因，也退不了款）。
// 这里以 Sora 的解析为底稿，再用容错解析补齐状态、成片地址与失败原因。
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	a.ensureSub()
	result, err := a.sub.ParseTaskResult(respBody)
	if err != nil || result == nil {
		result = &relaycommon.TaskInfo{}
	}
	var parsed megaVideoResult
	if unmarshalErr := common.Unmarshal(respBody, &parsed); unmarshalErr != nil {
		return result, nil
	}
	status := strings.TrimSpace(parsed.Status)
	if status == "" {
		status = strings.TrimSpace(parsed.TaskStatus)
	}
	if mapped := mapMegaStatus(status); mapped != "" {
		result.Status = mapped
	}
	if result.Status == model.TaskStatusSuccess && result.Url == "" {
		result.Url = megaResultURL(parsed)
	}
	if result.Status == model.TaskStatusFailure {
		if reason := megaFailureReason(parsed); reason != "" {
			result.Reason = reason
		}
	}
	if parsed.Progress > 0 && parsed.Progress < 100 {
		result.Progress = fmt.Sprintf("%d%%", parsed.Progress)
	}
	return result, nil
}

// mapMegaStatus 把上游状态映射成本站任务状态。
//
// 上游文档明确「部分兼容渠道可能返回 succeeded 等状态」，所以 succeeded 也要算成功。
func mapMegaStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "queued", "pending", "submitted", "created":
		return model.TaskStatusQueued
	case "processing", "in_progress", "running", "generating":
		return model.TaskStatusInProgress
	case "completed", "succeeded", "success":
		return model.TaskStatusSuccess
	case "failed", "cancelled", "canceled", "error":
		return model.TaskStatusFailure
	case "":
		return ""
	default:
		// 认不出的非空状态按「还在跑」处理：误判成失败会让用户白等一次生成，
		// 误判成成功会把没出的片子当成功交付。轮询超时有兜底退款，宁可等超时。
		return model.TaskStatusInProgress
	}
}

// megaResultURL 按优先级取出成片地址。
func megaResultURL(parsed megaVideoResult) string {
	candidates := []string{
		parsed.VideoURL,
		parsed.URL,
		parsed.ResultURL,
		parsed.Data.VideoURL,
		parsed.Data.URL,
		parsed.Data.ResultURL,
		parsed.Output.VideoURL,
		parsed.Data.Output.VideoURL,
	}
	for _, candidate := range candidates {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			return candidate
		}
	}
	return ""
}

// megaFailureReason 提取失败原因，兼容对象 / 字符串两种错误形态。
func megaFailureReason(parsed megaVideoResult) string {
	trimmed := strings.TrimSpace(string(parsed.Error))
	if trimmed != "" && trimmed != "null" {
		if strings.HasPrefix(trimmed, "\"") {
			var text string
			if err := common.Unmarshal(parsed.Error, &text); err == nil && strings.TrimSpace(text) != "" {
				return strings.TrimSpace(text)
			}
		} else {
			var object struct {
				Code    string `json:"code"`
				Message string `json:"message"`
				Reason  string `json:"reason"`
			}
			if err := common.Unmarshal(parsed.Error, &object); err == nil {
				switch {
				case strings.TrimSpace(object.Message) != "":
					return strings.TrimSpace(object.Message)
				case strings.TrimSpace(object.Code) != "":
					return strings.TrimSpace(object.Code)
				case strings.TrimSpace(object.Reason) != "":
					return strings.TrimSpace(object.Reason)
				}
			}
		}
	}
	return strings.TrimSpace(parsed.FailReason)
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	a.ensureSub()
	return a.sub.(channel.OpenAIVideoConverter).ConvertToOpenAIVideo(originTask)
}
