package manwu

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Constants
// ============================

const (
	// ModelName 是渠道默认模型（dola），保留导出供既有测试与文档引用。
	// 多模型路由请查 modelSpecs / SpecFor。
	ModelName       = ModelDola
	ChannelName     = "manwu"
	InputTypeImage  = "reference_image"
	InputTypeVideo  = "reference_video"
	OutputModeVideo = "video"
	OutputModeImage = "image"
	OutputModeText  = "prompt"

	// ArcReel 侧 platformId 标识（建单载荷 platformId 字段）。
	PlatformID       = "dola"
	PlatformIDGemini = "gemini"
	PlatformIDJimeng = "jimeng"
	// PlatformIDImage 是 ArcReel 的抽象图片平台（PUBLIC_REMOTE_IMAGE_PLATFORM）：
	// 它是内部建单协议值，与对外模型名 ModelImage 分离；服务端按
	// MANWU_REMOTE_IMAGE_PROVIDER 决定实际走 gemini 还是 jimeng。
	PlatformIDImage = "manwu-image"

	// baseURLDefault 为渠道未配置 BaseURL 时的兜底（ArcReel 生产地址）
	baseURLDefault = "https://arcreel.heibaidao.cn"

	submitPath = "/api/v1/remote-generation/jobs"

	defaultRatio = "16:9"
	// defaultDuration 与上游兜底口径对齐：ArcReel 对未指定/非法时长会回落 30 秒，
	// 这里缺省显式传 30，保证预扣与实际生成时长一致。
	defaultDuration = 30

	// 参考图上限按 dola 官网真机口径取 10（与 ArcReel openai 后端 manwu 分支同口径）。
	maxReferenceImages = 10
	// maxReferenceVideos 反解只认 1 个视频：ArcReel create_job 硬约束「视频反解
	// 任务必须且只能携带 1 个参考视频」，多传会被上游 400。
	maxReferenceVideos = 1
	// maxImageCount 与 ArcReel `_remote_image_count` 的 clamp 上限一致（1..10）。
	maxImageCount     = 10
	defaultImageCount = 1

	idempotencyKeyPrefix = "manwu-"

	normalizedRequestKey = "manwu_request"

	reasonCancelled = "任务已取消"
	// reasonEmptyResult：上游报 ready 却既没有产物 URL 也没有文本结果。判失败而不是
	// 成功——空交付必须触发退款，否则用户为「什么都没拿到」付了钱。
	reasonEmptyResult = "任务完成但上游未返回结果"
	// reasonDirectUploadUnsupported：全渠道都不收 multipart 文件。参考素材必须是
	// ArcReel 能回源的公网 URL（图片 URL、dola/gemini 的参考图 URL、反解的视频 URL），
	// 因此这条拒绝对四个模型都成立，不能写成只讲反解。
	reasonDirectUploadUnsupported = "暂不支持文件直传：本渠道全部模型都需要公网可访问的 http(s) 素材 URL（视频反解请传视频 URL）"
)

// 四个模型共用同一条 ArcReel 建单链路（POST /api/v1/remote-generation/jobs），
// 差异只在 platformId + outputMode + 参数白名单，故用一张路由表而不是四个 adaptor。
//
// 模型名口径：
//   - dola-seedance-2.5：dola 官网 Seedance 2.5（远端视频，按次计费）
//   - Nano Banana Pro：ArcReel 抽象图片平台（PUBLIC_REMOTE_IMAGE_PLATFORM）。服务端
//     MANWU_REMOTE_IMAGE_PROVIDER 决定实际走 gemini(nano_banana_2) 还是
//     jimeng(dreamina_image_5_0_lite)，客户端不感知，也不该猜
//   - gemini-web-video：Gemini 官网 Veo（远端视频，按次计费）。**故意不叫 veo-***：
//     官方 Gemini 渠道已占 veo-3.1-generate-preview 等模型名，重名会被路由到错渠道
//   - jimeng-video-reverse：即梦「视频反解」技能，单视频入参、提示词文本出参
const (
	ModelDola    = "dola-seedance-2.5"
	ModelImage   = "Nano Banana Pro"
	ModelVideo   = "gemini-web-video"
	ModelReverse = "jimeng-video-reverse"
)

// 任务形态：决定校验分支与建单载荷。
const (
	kindDolaVideo = "dola_video" // 时长/比例/参考图白名单最严
	kindVeoVideo  = "veo_video"  // 只有宽高比：官网无模型/时长/分辨率控件
	kindImage     = "image"      // 张数 + 宽高比 + 参考图
	kindReverse   = "reverse"    // 单视频入参，文本出参
)

type modelSpec struct {
	PlatformID string
	OutputMode string
	Kind       string
}

var modelSpecs = map[string]modelSpec{
	ModelDola:    {PlatformID: PlatformID, OutputMode: OutputModeVideo, Kind: kindDolaVideo},
	ModelVideo:   {PlatformID: PlatformIDGemini, OutputMode: OutputModeVideo, Kind: kindVeoVideo},
	ModelImage:   {PlatformID: PlatformIDImage, OutputMode: OutputModeImage, Kind: kindImage},
	ModelReverse: {PlatformID: PlatformIDJimeng, OutputMode: OutputModeText, Kind: kindReverse},
}

// SpecFor 返回模型路由；未知模型返回 false。调用方必须先拒绝，不能默认落到 dola
// （那会让 Veo 请求被当成 dola 提交，上游跑错平台还要用户付两次钱）。
func SpecFor(model string) (modelSpec, bool) {
	spec, ok := modelSpecs[strings.TrimSpace(model)]
	return spec, ok
}

// ModelList 返回本渠道支持的全部模型名（有序，供错误提示与 GetModelList 共用）。
func ModelList() []string {
	names := make([]string, 0, len(modelSpecs))
	for name := range modelSpecs {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

var allowedDurations = map[int]bool{
	5:  true,
	10: true,
	15: true,
	30: true,
}

var allowedRatios = map[string]bool{
	"16:9": true,
	"9:16": true,
	"1:1":  true,
	"4:3":  true,
	"3:4":  true,
	"21:9": true,
}

// ============================
// Request / Response structures
// ============================

// clientRequest 是厂商侧请求体。seconds/duration/input_reference/images 等用
// json.RawMessage 承接，以便宽容解析数字/字符串、单值/数组等别名形态。
type clientRequest struct {
	Model          string          `json:"model"`
	Prompt         string          `json:"prompt"`
	Seconds        json.RawMessage `json:"seconds"`
	Duration       json.RawMessage `json:"duration"`
	Size           string          `json:"size"`
	Ratio          string          `json:"ratio"`
	AspectRatio    string          `json:"aspect_ratio"`
	Resolution     string          `json:"resolution"`
	InputReference json.RawMessage `json:"input_reference"`
	Images         json.RawMessage `json:"images"`
	Videos         json.RawMessage `json:"videos"`
	Video          json.RawMessage `json:"video"`
	N              json.RawMessage `json:"n"`
	Count          json.RawMessage `json:"count"`
	IdempotencyKey string          `json:"idempotency_key"`
}

// normalizedRequest 是校验通过后缓存在 gin.Context 中的规范化请求，
// 供 EstimateBilling / BuildRequestBody 复用（避免重复解析与二次校验）。
type normalizedRequest struct {
	Model          string
	Kind           string
	Prompt         string
	Duration       int
	Ratio          string
	Count          int
	Images         []string
	Videos         []string
	IdempotencyKey string
}

// submitRequest 是 ArcReel 建单载荷。视频走 videoParams、图片走 imageParams、
// 反解两者都不带（ArcReel 对 outputMode=prompt 要求 inputs 恰好 1 个参考视频）。
type submitRequest struct {
	PlatformId     string       `json:"platformId"`
	OutputMode     string       `json:"outputMode"`
	Prompt         string       `json:"prompt"`
	VideoParams    *videoParams `json:"videoParams,omitempty"`
	ImageParams    *imageParams `json:"imageParams,omitempty"`
	Inputs         []jobInput   `json:"inputs,omitempty"`
	IdempotencyKey string       `json:"idempotencyKey"`
}

type videoParams struct {
	Ratio    string `json:"ratio"`
	Duration *int   `json:"duration,omitempty"`
}

type imageParams struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	Count       int    `json:"count"`
}

type jobInput struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// submitResponse 是 ArcReel 建单响应，宽容解析 jobId / id 两种字段名。
type submitResponse struct {
	JobID  string `json:"jobId"`
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// jobResponse 是 ArcReel 轮询响应，宽容解析 sourceUrl / source_url。
// ResultPrompt 是 outputMode=prompt（视频反解）的文本结果。
type jobResponse struct {
	JobID         string `json:"jobId"`
	Status        string `json:"status"`
	SourceURL     string `json:"sourceUrl"`
	SourceURLAlt  string `json:"source_url"`
	ResultPrompt  string `json:"resultPrompt"`
	ResultPrompt2 string `json:"result_prompt"`
	Error         string `json:"error"`
	CreatedAt     string `json:"createdAt"`
	UpdatedAt     string `json:"updatedAt"`
}

func (r jobResponse) resultURL() string {
	return firstNonEmpty(r.SourceURL, r.SourceURLAlt)
}

func (r jobResponse) resultText() string {
	return firstNonEmpty(r.ResultPrompt, r.ResultPrompt2)
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	if strings.TrimSpace(a.baseURL) == "" {
		a.baseURL = baseURLDefault
	}
}

// ValidateRequestAndSetAction 解析厂商请求，按模型形态分派校验。
// 注意：ArcReel 只接受 http(s) 参考素材 URL，不支持 multipart 文件上传，
// 因此不走 ValidateBasicTaskRequest 的共享解析（其 seconds 字段仅支持字符串形态）。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 反解的天然入参是本地 mp4，客户端很容易直接 multipart 上传。先把这种请求挡在
	// 入口给出可读原因，而不是让 JSON 解析报一个语法错误。
	if strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "multipart/form-data") {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("%s", reasonDirectUploadUnsupported),
			"invalid_input_reference",
			http.StatusBadRequest,
		)
	}

	var req clientRequest
	if err := common.UnmarshalBodyReusable(c, &req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}

	spec, ok := SpecFor(req.Model)
	if !ok {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("model must be one of %s, got %q", strings.Join(ModelList(), ", "), strings.TrimSpace(req.Model)),
			"invalid_model",
			http.StatusBadRequest,
		)
	}

	norm := &normalizedRequest{
		Model:          strings.TrimSpace(req.Model),
		Kind:           spec.Kind,
		IdempotencyKey: firstNonEmpty(req.IdempotencyKey, idempotencyKeyPrefix+common.GetUUID()),
	}

	if spec.Kind == kindReverse {
		// 反解建单允许空 prompt（ArcReel 对 outputMode=prompt 不要求 prompt）；
		// 带 prompt 也照传，当作给即梦助手的附加指令。
		norm.Prompt = strings.TrimSpace(req.Prompt)
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		videos, err := resolveVideos(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
		}
		norm.Videos = videos
		c.Set(normalizedRequestKey, norm)
		info.Action = constant.TaskActionGenerate
		return nil
	}

	// 其余形态（图片 / 两路视频）prompt 必填。
	norm.Prompt = strings.TrimSpace(req.Prompt)
	if norm.Prompt == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	ratio, err := resolveRatio(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_ratio", http.StatusBadRequest)
	}
	norm.Ratio = ratio

	images, err := resolveReferenceImages(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	norm.Images = images

	switch spec.Kind {
	case kindDolaVideo:
		duration, err := resolveDuration(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
		norm.Duration = duration
	case kindVeoVideo:
		// Veo 官网只有宽高比控件（2026-09-22 真机验证）：模型/时长/分辨率没有可实现
		// 的控件，Worker 侧会 fail-fast 拒单。这里提前 400，别让用户白付一次预扣。
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
	case kindImage:
		if err := rejectFields(req, spec.Kind, norm.Model); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		count, err := resolveImageCount(req)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_count", http.StatusBadRequest)
		}
		norm.Count = count
	}

	c.Set(normalizedRequestKey, norm)
	info.Action = constant.TaskActionGenerate
	return nil
}

// EstimateBilling 图片按张计费：把张数作为 n 倍率乘到 ¥0.3/张 的基础价上。
// 视频与反解按次计费，返回 nil（否则会被 relay_task.go 按倍率再乘一遍）。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	if c == nil {
		return nil
	}
	req, err := getNormalizedRequest(c)
	if err != nil {
		return nil
	}
	if req.Kind == kindImage && req.Count > 1 {
		return map[string]float64{"n": float64(req.Count)}
	}
	return nil
}

// BuildRequestURL constructs the upstream URL.
func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s%s", strings.TrimRight(a.baseURL, "/"), submitPath), nil
}

// BuildRequestHeader sets required headers.
func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

// BuildRequestBody converts the normalized request into the ArcReel job payload.
func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := getNormalizedRequest(c)
	if err != nil {
		return nil, err
	}
	spec, ok := SpecFor(req.Model)
	if !ok {
		return nil, fmt.Errorf("unknown manwu model: %s", req.Model)
	}

	body := submitRequest{
		PlatformId:     spec.PlatformID,
		OutputMode:     spec.OutputMode,
		Prompt:         req.Prompt,
		IdempotencyKey: req.IdempotencyKey,
	}

	switch spec.Kind {
	case kindReverse:
		// inputs 恰好 1 个 reference_video；不带 videoParams/imageParams
		// （ArcReel 对 outputMode=prompt 会拒绝其它参数形态）。
		body.Inputs = []jobInput{{Type: InputTypeVideo, URL: req.Videos[0]}}
	case kindImage:
		body.ImageParams = &imageParams{AspectRatio: req.Ratio, Count: req.Count}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	case kindVeoVideo:
		// 只传宽高比：官网没有时长/分辨率控件，传了也会被 ArcReel 剥掉，写进去只会
		// 让人误以为我们执行了用户请求的时长。
		body.VideoParams = &videoParams{Ratio: req.Ratio}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	default:
		body.VideoParams = &videoParams{Ratio: req.Ratio, Duration: &req.Duration}
		body.Inputs = toInputs(InputTypeImage, req.Images)
	}

	data, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(data), nil
}

func toInputs(inputType string, urls []string) []jobInput {
	if len(urls) == 0 {
		return nil
	}
	inputs := make([]jobInput, 0, len(urls))
	for _, u := range urls {
		inputs = append(inputs, jobInput{Type: inputType, URL: u})
	}
	return inputs
}

// DoRequest delegates to common helper.
func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// DoResponse handles upstream response, returns taskID etc.
func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var submitResp submitResponse
	if err := common.Unmarshal(responseBody, &submitResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	taskID = firstNonEmpty(submitResp.JobID, submitResp.ID)
	if taskID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("arcreel returned empty jobId, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)
	return taskID, responseBody, nil
}

// FetchTask fetch task status
func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s%s/%s", strings.TrimRight(baseUrl, "/"), submitPath, url.PathEscape(taskID))
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

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList()
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ParseTaskResult maps the ArcReel job status to the unified TaskInfo.
func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res jobResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "queued":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "assigned", "accepted", "submitting", "provider_running", "provider_succeeded", "url_validating":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "ready":
		taskResult.Progress = taskcommon.ProgressComplete
		raw := res.resultURL()
		taskResult.ResultText = res.resultText()
		switch {
		case directResultURL(raw) != "":
			// 公网直链：直接交付。
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Url = directResultURL(raw)
		case strings.TrimSpace(raw) != "":
			// ArcReel 自有托管路径（非公网、需渠道密钥）：交付有效，但由
			// new-api 的 content 代理回源，这里不吐地址。
			taskResult.Status = model.TaskStatusSuccess
		case taskResult.ResultText != "":
			// 视频反解：文本即交付物，没有媒体 URL。
			taskResult.Status = model.TaskStatusSuccess
		default:
			// ready 但什么都没给：按失败收敛，让轮询阶段退款。
			taskResult.Status = model.TaskStatusFailure
			taskResult.Reason = reasonEmptyResult
		}
	case "failed", "url_unavailable":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = res.Error
	case "cancelled":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Progress = taskcommon.ProgressComplete
		taskResult.Reason = reasonCancelled
	default:
		// "unknown" 及无法识别的状态
		taskResult.Status = model.TaskStatusUnknown
	}
	return taskResult, nil
}

// ConvertToOpenAIVideo 把轮询同步后的 task.Data（ArcReel jobResponse 原始 JSON）
// 映射成 OpenAI video 查询响应；否则客户端永远看到 queued 无法收敛。
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	var res jobResponse
	if err := common.Unmarshal(task.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal manwu task data failed")
	}

	openAIResp := dto.NewOpenAIVideo()
	openAIResp.ID = task.TaskID
	openAIResp.Model = task.Properties.OriginModelName
	openAIResp.SetProgressStr(task.Progress)
	openAIResp.CreatedAt = task.CreatedAt
	openAIResp.CompletedAt = task.UpdatedAt
	// 文本结果以落库的 PrivateData.ResultText 为准（权威副本）；task.Data 只是上游
	// 响应的快照，经脱敏/重写后可能缺字段。反解提示词很长，更不能只靠快照。
	text := strings.TrimSpace(task.PrivateData.ResultText)

	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "ready":
		raw := strings.TrimSpace(res.resultURL())
		if text == "" {
			text = res.resultText()
		}
		switch {
		case directResultURL(raw) != "":
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("url", directResultURL(raw))
			openAIResp.SetMetadata("media_type", inferMediaType(raw))
		case raw != "":
			// 自有托管路径：回落到 content 代理地址，客户端拿到的一定能打开。
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("url", taskcommon.BuildProxyURL(task.TaskID))
			openAIResp.SetMetadata("media_type", inferMediaType(raw))
		case text != "":
			// 反解：提示词就是全部交付物。**不给 url**——写一个指向 content 代理的
			// 占位地址会诱使客户端去拉一个根本不存在的媒体文件（代理只会 502）。
			openAIResp.Status = dto.VideoStatusCompleted
			openAIResp.SetMetadata("prompt", text)
			openAIResp.SetMetadata("media_type", "text")
		default:
			openAIResp.Status = dto.VideoStatusFailed
			openAIResp.Error = &dto.OpenAIVideoError{Code: "empty_result", Message: reasonEmptyResult}
		}
	case "failed", "url_unavailable":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{
			Code:    strings.ToLower(strings.TrimSpace(res.Status)),
			Message: firstNonEmpty(res.Error, taskcommon.ReasonContentModeration),
		}
	case "cancelled":
		openAIResp.Status = dto.VideoStatusFailed
		openAIResp.Error = &dto.OpenAIVideoError{Code: "cancelled", Message: reasonCancelled}
	case "queued":
		openAIResp.Status = dto.VideoStatusQueued
	case "assigned", "accepted", "submitting", "provider_running", "provider_succeeded", "url_validating":
		openAIResp.Status = dto.VideoStatusInProgress
	default:
		openAIResp.Status = dto.VideoStatusUnknown
	}

	return common.Marshal(openAIResp)
}

// ============================
// helpers
// ============================

func getNormalizedRequest(c *gin.Context) (*normalizedRequest, error) {
	v, exists := c.Get(normalizedRequestKey)
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req, ok := v.(*normalizedRequest)
	if !ok {
		return nil, fmt.Errorf("invalid request type in context")
	}
	return req, nil
}

// rejectFields 拒绝该任务形态不支持、且**无法诚实执行**的字段。
//
// 口径与 Worker 侧一致（RemoteTaskAdapter 对 Gemini/Veo 的 unsupported params 直接
// 拒单）：宁可入口 400，也不要「过滤后伪装成已按请求执行」——用户会为一个没跑的
// 参数付费。
func rejectFields(req clientRequest, kind, modelName string) error {
	switch kind {
	case kindVeoVideo:
		if hasValue(req.Seconds) {
			return fmt.Errorf("%s is fixed-duration web Veo: seconds/duration is not supported", modelName)
		}
		if hasValue(req.Duration) {
			return fmt.Errorf("%s is fixed-duration web Veo: seconds/duration is not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
	case kindImage:
		if hasValue(req.Seconds) || hasValue(req.Duration) {
			return fmt.Errorf("%s is an image model: seconds/duration is not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
	case kindReverse:
		if hasValue(req.Seconds) || hasValue(req.Duration) {
			return fmt.Errorf("%s duration is fixed by the skill and is not supported", modelName)
		}
		if strings.TrimSpace(req.Size) != "" || strings.TrimSpace(req.Ratio) != "" ||
			strings.TrimSpace(req.AspectRatio) != "" {
			return fmt.Errorf("%s produces text, so size/ratio/aspect_ratio are not supported", modelName)
		}
		if strings.TrimSpace(req.Resolution) != "" {
			return fmt.Errorf("%s does not support resolution", modelName)
		}
		if len(parseStringList(req.Images)) > 0 {
			return fmt.Errorf("%s accepts exactly one reference video, not images", modelName)
		}
	}
	return nil
}

// resolveDuration 解析时长：seconds（首选，数字或数字字符串）→ duration → 缺省 30。
// 白名单 5/10/15/30，缺省或 null 视为未指定；字段存在但非法（格式错误或不在白名单）
// 直接报错——上游 ArcReel 对非法值会静默回落 30，厂商侧必须显式拒绝，
// 避免计费时长与实际生成时长脱节。
func resolveDuration(req clientRequest) (int, error) {
	seconds, found, err := parseFlexibleInt(req.Seconds)
	if err != nil {
		return 0, fmt.Errorf("seconds %s: %w", string(req.Seconds), err)
	}
	if found {
		if !allowedDurations[seconds] {
			return 0, fmt.Errorf("seconds must be one of 5/10/15/30, got %d", seconds)
		}
		return seconds, nil
	}

	duration, found, err := parseFlexibleInt(req.Duration)
	if err != nil {
		return 0, fmt.Errorf("duration %s: %w", string(req.Duration), err)
	}
	if found {
		if !allowedDurations[duration] {
			return 0, fmt.Errorf("duration must be one of 5/10/15/30, got %d", duration)
		}
		return duration, nil
	}

	return defaultDuration, nil
}

// resolveRatio 解析画面比例：size（首选）→ ratio → aspect_ratio → 缺省 16:9。
func resolveRatio(req clientRequest) (string, error) {
	candidate := strings.TrimSpace(req.Size)
	if candidate == "" {
		candidate = strings.TrimSpace(req.Ratio)
	}
	if candidate == "" {
		candidate = strings.TrimSpace(req.AspectRatio)
	}
	if candidate == "" {
		return defaultRatio, nil
	}
	if !allowedRatios[candidate] {
		return "", fmt.Errorf("ratio must be one of 16:9/9:16/1:1/4:3/3:4/21:9, got %q", candidate)
	}
	return candidate, nil
}

// resolveReferenceImages 解析参考图：input_reference（首选）→ images。
// 每张必须是 http(s) URL（ArcReel 不接受 multipart/data URI）。
func resolveReferenceImages(req clientRequest) ([]string, error) {
	images := parseStringList(req.InputReference)
	if len(images) == 0 {
		images = parseStringList(req.Images)
	}
	if len(images) > maxReferenceImages {
		return nil, fmt.Errorf("manwu accepts at most %d reference images, got %d", maxReferenceImages, len(images))
	}
	for _, image := range images {
		if err := assertHTTPURL(image, "reference image"); err != nil {
			return nil, err
		}
	}
	return images, nil
}

// resolveVideos 解析反解的视频地址：input_reference（首选）→ videos → video。
// 上限恰好 1 个（ArcReel 硬约束）。
func resolveVideos(req clientRequest) ([]string, error) {
	videos := parseStringList(req.InputReference)
	if len(videos) == 0 {
		videos = parseStringList(req.Videos)
	}
	if len(videos) == 0 {
		videos = parseStringList(req.Video)
	}
	if len(videos) == 0 {
		return nil, fmt.Errorf("video reference is required: pass a public http(s) video URL via input_reference")
	}
	if len(videos) > maxReferenceVideos {
		return nil, fmt.Errorf("video reverse accepts exactly %d reference video, got %d", maxReferenceVideos, len(videos))
	}
	for _, video := range videos {
		if err := assertHTTPURL(video, "reference video"); err != nil {
			return nil, err
		}
	}
	return videos, nil
}

// resolveImageCount 解析出图张数：n（首选）→ count → 缺省 1，上限 10（与 ArcReel
// `_remote_image_count` 的 clamp 一致）。
func resolveImageCount(req clientRequest) (int, error) {
	count, found, err := parseFlexibleInt(req.N)
	if err != nil {
		return 0, fmt.Errorf("n %s: %w", string(req.N), err)
	}
	if !found {
		if count, found, err = parseFlexibleInt(req.Count); err != nil {
			return 0, fmt.Errorf("count %s: %w", string(req.Count), err)
		}
	}
	if !found {
		return defaultImageCount, nil
	}
	if count < 1 || count > maxImageCount {
		return 0, fmt.Errorf("n must be between 1 and %d, got %d", maxImageCount, count)
	}
	return count, nil
}

func assertHTTPURL(raw, label string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an http(s) URL, got %q", label, raw)
	}
	return nil
}

// parseFlexibleInt 兼容数字与数字字符串两种 JSON 形态。
// found=false 表示字段缺省或 null（视为未指定）；
// 字段存在但格式非法（非数字、浮点、非数字字符串）时返回错误。
func parseFlexibleInt(raw json.RawMessage) (value int, found bool, err error) {
	if !hasValue(raw) {
		return 0, false, nil
	}
	var num *int
	if uerr := common.Unmarshal(raw, &num); uerr == nil {
		if num != nil {
			return *num, true, nil
		}
		return 0, false, nil
	}
	var text string
	if uerr := common.Unmarshal(raw, &text); uerr == nil {
		if v, aerr := strconv.Atoi(strings.TrimSpace(text)); aerr == nil {
			return v, true, nil
		}
	}
	return 0, false, fmt.Errorf("is not a valid integer")
}

// parseStringList 兼容字符串与字符串数组两种 JSON 形态
// （与 relaycommon 对 input_reference/images 的宽容解析一致）。
func parseStringList(raw json.RawMessage) []string {
	if !hasValue(raw) {
		return nil
	}
	var values []string
	if err := common.Unmarshal(raw, &values); err == nil {
		return values
	}
	var value string
	if err := common.Unmarshal(raw, &value); err == nil && value != "" {
		return []string{value}
	}
	return nil
}

// hasValue 判断 JSON 字段是否「真的带了值」：缺省 / null / 空串 / 空数组都算未指定。
//
// 注意**不**把数字 0 当作未指定：`seconds: 0` / `n: 0` 是用户明确传了非法值，应该
// 走到白名单校验并 400，而不是静默回落到缺省值（计费时长/张数会与用户意图脱节）。
func hasValue(raw json.RawMessage) bool {
	switch strings.TrimSpace(string(raw)) {
	case "", "null", `""`, "[]":
		return false
	}
	return true
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

// directResultURL 只把「公网绝对地址」当作可直交付的结果。
//
// ArcReel 对 Worker 上传到本站托管目录的产物（Gemini 图片 blob、Veo 的 data: mp4）
// 存的是**相对路径** `/api/v1/remote-generation/jobs/{id}/outputs/{file}`——它不是
// 公网地址且需要渠道 token。透传给客户端等于交付一个打不开的链接，因此这里返回空串，
// 让 new-api 落成 `/v1/videos/{task_id}/content` 代理（task_polling.go 的既有分支），
// 由 VideoProxy 带渠道密钥回源。dola/tiktok 这类公网 CDN 地址照旧直传。
func directResultURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(strings.ToLower(trimmed), "http://") &&
		!strings.HasPrefix(strings.ToLower(trimmed), "https://") {
		return ""
	}
	return trimmed
}

// inferMediaType 按结果地址后缀推断交付物类型，供客户端区分图片 / 视频 / 文本。
func inferMediaType(rawURL string) string {
	switch strings.ToLower(path.Ext(strings.SplitN(rawURL, "?", 2)[0])) {
	case ".png", ".jpg", ".jpeg", ".webp", ".gif":
		return "image"
	case ".mp4", ".mov", ".webm", ".m4v":
		return "video"
	default:
		return "video"
	}
}
