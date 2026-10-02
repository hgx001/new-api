package hailuo

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
)

// https://platform.minimaxi.com/docs/api-reference/video-generation-intro
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	// v2（H3 家族）走自己的能力矩阵：时长/分辨率/比例/素材都有硬边界，
	// 越界必须在本地 400，不能指望上游报错。
	if _, ok := lookupV2Spec(info.UpstreamModelName); ok {
		var req relaycommon.TaskSubmitReq
		if err := common.UnmarshalBodyReusable(c, &req); err != nil {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("invalid request body: %s", err.Error()),
				"invalid_request", http.StatusBadRequest)
		}
		// 与 ValidateBasicTaskRequest 对齐的单图写法。
		if len(req.Images) == 0 && strings.TrimSpace(req.Image) != "" {
			req.Images = []string{req.Image}
		}
		spec, _ := lookupV2Spec(info.UpstreamModelName)
		if taskErr := validateV2Request(&req, spec); taskErr != nil {
			return taskErr
		}
		// ⚠️ 必须写回 context：BuildRequestBody 读的是 "task_request"，而
		// EstimateBilling 也从同一个 key 取 duration。漏了这一步的表现是
		// BuildRequestBody 报 "request not found in context"（500），并且
		// **计费静默退化成按 1 秒收**（EstimateBilling 拿不到请求就返回 nil）。
		relaycommon.StoreTaskRequest(c, info, constant.TaskActionGenerate, req)
		return nil
	}
	return relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionGenerate)
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if spec, ok := lookupV2Spec(info.UpstreamModelName); ok {
		if spec.ContextIR {
			return fmt.Sprintf("%s%s", a.baseURL, V2ContextIREndpoint), nil
		}
		return fmt.Sprintf("%s%s", a.baseURL, V2VideoEndpoint), nil
	}
	return fmt.Sprintf("%s%s", a.baseURL, TextToVideoEndpoint), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(relaycommon.TaskSubmitReq)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}

	var payload any
	var err error
	if _, ok := lookupV2Spec(info.UpstreamModelName); ok {
		payload, err = a.convertToV2Payload(&req, info)
	} else {
		payload, err = a.convertToRequestPayload(&req, info)
	}
	if err != nil {
		return nil, errors.Wrap(err, "convert request payload failed")
	}

	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}

	return bytes.NewReader(data), nil
}

// convertToV2Payload 组装 v2 建单载荷。注意 model 字段用 **UpstreamName**：
// 对外模型名是 MiniMax-H3-Context-IR，但 Context-IR 端点只接受 MiniMax-H3。
func (a *TaskAdaptor) convertToV2Payload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*V2VideoRequest, error) {
	spec, ok := lookupV2Spec(info.UpstreamModelName)
	if !ok {
		return nil, fmt.Errorf("unsupported v2 model: %s", info.UpstreamModelName)
	}
	resolution, err := resolveV2Resolution(req, spec)
	if err != nil {
		return nil, err
	}
	ratio, err := resolveV2Ratio(req)
	if err != nil {
		return nil, err
	}
	duration := spec.DefaultDur
	if req.Duration > 0 {
		duration = req.Duration
	}
	content := buildV2Content(req)
	if len(content) == 0 {
		return nil, fmt.Errorf("prompt is required")
	}
	// 纯文生视频（只有 text 项）时上游不接受 adaptive。
	if spec.RequireRatio && len(content) == 1 && ratio == "adaptive" {
		return nil, fmt.Errorf(
			"ratio is required for text-to-video %s: pass metadata.ratio (one of %s) or size",
			spec.Name, strings.Join(nonAdaptiveV2Ratios(), "/"))
	}
	return &V2VideoRequest{
		Model:      spec.UpstreamName,
		Content:    content,
		Duration:   duration,
		Resolution: resolution,
		Ratio:      ratio,
	}, nil
}

func nonAdaptiveV2Ratios() []string {
	out := make([]string, 0, len(v2Ratios))
	for _, r := range v2Ratios {
		if r != "adaptive" {
			out = append(out, r)
		}
	}
	return out
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	// v2 的失败形状与 v1 完全不同（没有 base_resp），先单独处理。
	if _, isV2 := lookupV2Spec(info.UpstreamModelName); isV2 {
		return a.doResponseV2(c, responseBody, info)
	}

	var hResp VideoResponse
	if err := common.Unmarshal(responseBody, &hResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if hResp.BaseResp.StatusCode != StatusSuccess {
		taskErr = service.TaskErrorWrapper(
			fmt.Errorf("hailuo api error: %s", hResp.BaseResp.StatusMsg),
			strconv.Itoa(hResp.BaseResp.StatusCode),
			http.StatusBadRequest,
		)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName

	c.JSON(http.StatusOK, ov)
	return hResp.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}

	// v2 轮询是**路径参数**（/v2/query/video_generation/{task_id}），不是 query。
	if isV2UpstreamModel(body) {
		uri := fmt.Sprintf("%s%s", baseUrl, fmt.Sprintf(V2QueryTaskEndpointFmt, taskID))
		return a.doTaskQuery(uri, key, proxy)
	}

	uri := fmt.Sprintf("%s%s?task_id=%s", baseUrl, QueryTaskEndpoint, taskID)
	return a.doTaskQuery(uri, key, proxy)
}

func (a *TaskAdaptor) doTaskQuery(uri, key, proxy string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// isV2UpstreamModel 从落库的任务载荷反推是否 v2。轮询阶段拿不到 RelayInfo，
// 只能靠 CreateResponse 落库的 task_id 旁边那个 model 标记。
func isV2UpstreamModel(body map[string]any) bool {
	m, _ := body["model"].(string)
	if isV2Model(m) {
		return true
	}
	// 老数据可能没存 model，但存了 v2 端点标记。
	if ep, _ := body["_minimax_endpoint"].(string); ep != "" {
		return true
	}
	return false
}

// EstimateBilling 让 v2（H3 家族）按「秒数 × 分辨率档位」计费。
//
// v2 上游是按秒计价（$0.05–0.13/秒随模型与分辨率变化），而 BaseBilling 返回 nil
// 意味着只按模型基础价收一次——对 4s 和 15s 收一样的钱，长片就亏本。
// 做法与 wan3 一致：基础价定义在**最低分辨率档**（H3=768P、H3-Max=480P），
// 其余档位用 size 倍率上浮；这样 ModelRatio 里的数字直接读作「每秒单价」。
//
// Context-IR 不产视频、按时长计费无意义，回落到 BaseBilling（按次）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if info == nil || info.ChannelMeta == nil {
		return nil
	}
	spec, ok := lookupV2Spec(info.UpstreamModelName)
	if !ok || spec.ContextIR {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	duration := spec.DefaultDur
	if req.Duration > 0 {
		duration = req.Duration
	}
	resolution, err := resolveV2Resolution(&req, spec)
	if err != nil {
		resolution = spec.DefaultRes
	}
	return map[string]float64{
		"seconds": float64(duration),
		"size":    v2ResolutionRatio(spec, resolution),
	}
}

// v2ResolutionRatio 把分辨率档位折成相对基础档的倍率。缺配置回 1.0（按基础档收），
// 不猜倍数——猜错就是系统性错价。
func v2ResolutionRatio(spec V2ModelSpec, resolution string) float64 {
	if ratio, ok := spec.ResolutionRatios[strings.ToUpper(strings.TrimSpace(resolution))]; ok {
		return ratio
	}
	return 1.0
}

func (a *TaskAdaptor) GetModelList() []string {
	return append(append([]string{}, ModelList...), ModelH3, ModelH3Max, ModelH3ContextIR)
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

func (a *TaskAdaptor) convertToRequestPayload(req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*VideoRequest, error) {
	modelConfig := GetModelConfig(info.UpstreamModelName)
	duration := DefaultDuration
	if req.Duration > 0 {
		duration = req.Duration
	}
	resolution := modelConfig.DefaultResolution
	if req.Size != "" {
		resolution = a.parseResolutionFromSize(req.Size, modelConfig)
	}

	videoRequest := &VideoRequest{
		Model:      info.UpstreamModelName,
		Prompt:     req.Prompt,
		Duration:   &duration,
		Resolution: resolution,
	}
	if err := req.UnmarshalMetadata(&videoRequest); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata to video request failed")
	}

	return videoRequest, nil
}

func (a *TaskAdaptor) parseResolutionFromSize(size string, modelConfig ModelConfig) string {
	switch {
	case strings.Contains(size, "1080"):
		return Resolution1080P
	case strings.Contains(size, "768"):
		return Resolution768P
	case strings.Contains(size, "720"):
		return Resolution720P
	case strings.Contains(size, "512"):
		return Resolution512P
	default:
		return modelConfig.DefaultResolution
	}
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	// v2 形状不同，先分流。
	if looksLikeV2Query(respBody) {
		return parseV2TaskResult(respBody)
	}

	resTask := QueryTaskResponse{}
	if err := common.Unmarshal(respBody, &resTask); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := relaycommon.TaskInfo{}

	if resTask.BaseResp.StatusCode == StatusSuccess {
		taskResult.Code = 0
	} else {
		taskResult.Code = resTask.BaseResp.StatusCode
		taskResult.Reason = resTask.BaseResp.StatusMsg
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
	}

	switch resTask.Status {
	case TaskStatusPreparing, TaskStatusQueueing, TaskStatusProcessing:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
		if resTask.Status == TaskStatusProcessing {
			taskResult.Progress = "50%"
		}
	case TaskStatusSuccess:
		taskResult.Status = model.TaskStatusSuccess
		taskResult.Progress = "100%"
		taskResult.Url = a.buildVideoURL(resTask.TaskID, resTask.FileID)
	case TaskStatusFailed:
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = "100%"
		if taskResult.Reason == "" {
			taskResult.Reason = taskcommon.ReasonContentModeration
		}
	default:
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = "30%"
	}

	return &taskResult, nil
}

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	// v2：产物可能是视频 URL，也可能是 Context-IR 的提示词文本。
	if looksLikeV2Query(originTask.Data) {
		return convertV2ToOpenAIVideo(originTask)
	}

	var hailuoResp QueryTaskResponse
	if err := common.Unmarshal(originTask.Data, &hailuoResp); err != nil {
		return nil, errors.Wrap(err, "unmarshal hailuo task data failed")
	}

	openAIVideo := originTask.ToOpenAIVideo()
	if hailuoResp.BaseResp.StatusCode != StatusSuccess {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: taskcommon.NormalizeFailureReason(hailuoResp.BaseResp.StatusMsg),
			Code:    strconv.Itoa(hailuoResp.BaseResp.StatusCode),
		}
	} else if originTask.Status == model.TaskStatusFailure {
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: taskcommon.NormalizeFailureReason(originTask.FailReason),
		}
	}

	jsonData, err := common.Marshal(openAIVideo)
	if err != nil {
		return nil, errors.Wrap(err, "marshal openai video failed")
	}

	return jsonData, nil
}

func (a *TaskAdaptor) buildVideoURL(_, fileID string) string {
	if a.apiKey == "" || a.baseURL == "" {
		return ""
	}

	url := fmt.Sprintf("%s/v1/files/retrieve?file_id=%s", a.baseURL, fileID)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return ""
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	resp, err := service.GetHttpClient().Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}

	var retrieveResp RetrieveFileResponse
	if err := common.Unmarshal(responseBody, &retrieveResp); err != nil {
		return ""
	}

	if retrieveResp.BaseResp.StatusCode != StatusSuccess {
		return ""
	}

	return retrieveResp.File.DownloadURL
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsInt(slice []int, item int) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
