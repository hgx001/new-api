package xinbiapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	taskwan3 "github.com/QuantumNous/new-api/relay/channel/task/wan3"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────────────
// 心笔 Model Router（xinbiapi.com）视频任务适配器。
//
// 契约（2026-11-01 实测 + 官方文档核对）：
//
//	提交   POST {base}/newapi/v1/video/generations
//	       请求体是「new-api 风格」：model / prompt / duration / metadata{resolution,ratio}
//	       / reference_images[] / seed，**没有 parameters 桶**。
//	轮询   GET  {base}/v1/media/tasks/{taskId}          ← 原生口，状态枚举有完整文档
//	取件   GET  {base}/v1/media/files/{fileId}/download ← 返回 3 天有效的稳定直链
//
// 为什么提交走兼容口、轮询走原生口：
//   · 兼容口接受 https 直链参考图（原生口只收平台 fileId），省掉整套分片上传；
//   · 兼容口的响应体没有公开文档，原生口的 status / resultFileIds 有明确枚举；
//   · 官方文档明确「三套风格返回的是同一个任务 id，用原生 GET /media/tasks/{id} 查详情」。
//
// 实测确认的坑（零成本 400 探针，未建单）：
//   · negative_prompt / negativePrompt **都不被兼容口接受**（UNSUPPORTED_FIELD），
//     所以本适配器不向下游承诺负向提示词，传了就本地 400，而不是静默丢弃。
//   · ratio / resolution 会按 parameterSchema 做枚举校验；
//   · 参考图必须是 https 直链（http / 内网地址被 FILE_3000037 拒绝）。
// ─────────────────────────────────────────────────────────────────────────────

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.apiKey = info.ApiKey
}

// ────────────────────────────── 校验 ──────────────────────────────

// ValidateRequestAndSetAction 在本地把参数收敛到上游合同允许的范围内。
//
// 所有拒绝都在**建单之前**返回 400：上游一建单就开始计费，
// 参数错误不该以「建单后失败」的形式出现。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) (taskErr *dto.TaskError) {
	if taskErr = relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if _, err := resolveDuration(req, executedModelName(info)); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	if _, err := resolveRatio(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_ratio", http.StatusBadRequest)
	}
	// 素材：参考图 / 参考视频 / 参考音频三类都收（上限与上游一致），
	// 首尾帧与未知类型在这里明确报错 —— 不能静默丢掉后照建单照扣费。
	if _, err := collectReferenceMedia(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	// 档位锁定：按次档只卖 720P。
	// 客户端显式要 480P/1080P 必须报错，不能静默改档 —— 那是「收 720P 的钱给 1080P 的承诺」。
	//
	// 特惠分流目标（按秒档）是**例外**：原 Wan3 特惠链路对分辨率参数的处理是
	// 「无条件 480P 生成、统一提升到 720P」，传 480P/1080P 既不报错也不改变产物。
	// 分流层如果在这里改成 400，同一条请求会因为「走了便宜链路」而行为突变 ——
	// 客户看到的是同一个模型时好时坏。所以按秒档一律忽略该参数。
	if err := rejectResolutionMismatch(req, executedModelName(info)); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_resolution", http.StatusBadRequest)
	}
	// seed 上游不支持（parameterSchema 只有 ratio/duration/resolution，
	// 且 additionalProperties: false）。本地先拒：上游的 422 只说「传了未声明的参数」，
	// 不点名字段，客户端拿到只会一脸问号。
	if err := rejectUnsupportedFields(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "unsupported_seed", http.StatusBadRequest)
	}
	return nil
}

// rejectUnsupportedFields 拦住上游 parameterSchema 没声明、但客户端会习惯性带的字段。
//
// 目前只有 seed。做成独立函数是为了能脱离 gin 上下文单测。
func rejectUnsupportedFields(req relaycommon.TaskSubmitReq) error {
	if req.Seed != nil {
		return fmt.Errorf("field seed is not supported by this model, remove it and retry")
	}
	return nil
}

// rejectResolutionMismatch 校验客户端显式指定的分辨率档位。
//
// 按次档（wan3.0-720p-按次 / seedance2.5-活动）只卖 720P：传别的档位必须报错，
// 不能静默改档 —— 那是「收 720P 的钱给 1080P 的承诺」。
//
// 按秒档（特惠分流目标）**不校验**：原 Wan3 特惠链路对分辨率参数是静默忽略
// （无条件 480P 生成 + 提升到 720P），分流后改成 400 会让同一条请求
// 「走官网能跑、走心笔报错」，客户看到的是同一个模型时好时坏。
//
// 错误文案刻意不带模型名：它会被脱敏成「上游-720p-按次」这种碎词。
func rejectResolutionMismatch(req relaycommon.TaskSubmitReq, modelName string) error {
	if isPerSecondRouteModel(modelName) {
		return nil
	}
	raw := strings.TrimSpace(req.Resolution)
	if raw == "" || strings.EqualFold(raw, deliveredResolution) {
		return nil
	}
	return fmt.Errorf("resolution %q is not supported, this model always delivers %s", raw, deliveredResolution)
}

// EstimateBilling 返回计费倍率。
//
// 按次档（ModelNamePayPerCall / seedance2.5-活动）固定一口价，返回 nil：
// 不能带入 seconds / size，否则会在按次价上再乘一次时长，等于按秒多扣。
//
// 按秒档（ModelNameTehuiRoute = 特惠分流目标）必须返回 seconds：
// 少了它，10 秒的请求只按 1 秒收钱。**不返回 size** ——
// 特惠的 720P 是「480P 生成 + 交付处理」的复合价，已经含在 ModelPrice 里，
// 再乘分辨率倍率会把价格翻倍。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !isPerSecondRouteModel(executedModelName(info)) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		// 请求上下文缺失时按缺省秒数计费，绝不能返回 nil ——
		// 按秒档返回 nil 会被当成 1 秒，10 秒的请求少收 9 秒。
		return map[string]float64{"seconds": float64(perSecondDefaultDuration)}
	}
	duration, err := resolveDuration(req, ModelNameTehuiRoute)
	if err != nil {
		// 时长已经在 Validate 阶段拦过；这里拿不到就交给上层按缺省秒数处理，
		// 绝不能返回一个「0 秒」把账单清零。
		duration = perSecondDefaultDuration
	}
	return map[string]float64{"seconds": float64(duration)}
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

// ────────────────────────────── 请求 ──────────────────────────────

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// 渠道 base_url 可能带结尾斜杠（管理员手填），这里再兜一次，避免拼出 //newapi。
	return strings.TrimRight(a.baseURL, "/") + "/newapi/v1/video/generations", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// 幂等键用本站的公开任务 ID：同一任务重试时上游返回原单，不会重复计费。
	// 上游文档明确「同一键 + 同一请求体返回原任务；不带则每次都是新任务」。
	if id := strings.TrimSpace(info.PublicTaskID); id != "" {
		req.Header.Set("Idempotency-Key", id)
	}
	return nil
}

// submitRequest 是兼容口的提交体。
//
// 字段名按 2026-11-01 实测锁定：snake_case 的 reference_images 被接受，
// negative_prompt 被 UNSUPPORTED_FIELD 拒绝，故不设该字段。
//
// ⚠️ 没有 seed：上游 wan3.0-video 的 parameterSchema 是
// {"properties":{ratio,duration,resolution}, "additionalProperties":false}，
// 带 seed 会吃 UNKNOWN_MEDIA_PARAMETER（422，且报错不点名字段，很难自查）。
// 调用方传 seed 在 ValidateRequestAndSetAction 里就本地 400。
type submitRequest struct {
	Model           string            `json:"model"`
	Prompt          string            `json:"prompt"`
	Duration        int               `json:"duration"`
	Metadata        map[string]string `json:"metadata"`
	ReferenceImages []string          `json:"reference_images,omitempty"`
	Videos          []string          `json:"videos,omitempty"`
	Audios          []string          `json:"audios,omitempty"`
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	duration, err := resolveDuration(req, executedModelName(info))
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_duration"}
	}
	ratio, err := resolveRatio(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_ratio"}
	}
	media, err := collectReferenceMedia(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_input_reference"}
	}

	body := submitRequest{
		// 固定上游模型名，不信任渠道映射（见 constants.go 的 upstreamModel 注释）。
		Model:    upstreamModel,
		Prompt:   req.Prompt,
		Duration: duration,
		Metadata: map[string]string{
			"resolution": generationResolution,
			"ratio":      ratio,
		},
		ReferenceImages: media.Images,
		Videos:          media.Videos,
		Audios:          media.Audios,
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return strings.NewReader(string(encoded)), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

// submitEnvelope 兼容口的响应信封。
//
// 实测错误体是 {"code","message","data"}，成功体没有公开文档，因此
// 同时兼容「信封里套 data」与「平铺 id」两种形状 —— 宁可多认几种，
// 也不要因为形状猜错把一个已经计费成功的任务判成提交失败。
type submitEnvelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	ID      string          `json:"id"`
	TaskID  string          `json:"task_id"`
}

type submitData struct {
	ID        string `json:"id"`
	TaskID    string `json:"taskId"`
	TaskIDAlt string `json:"task_id"`
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var env submitEnvelope
	if err := common.Unmarshal(responseBody, &env); err != nil {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("unmarshal response failed: %w, body: %s", err, truncate(responseBody)),
			"unmarshal_response_body_failed", http.StatusInternalServerError)
	}

	upstreamID := firstNonEmpty(env.ID, env.TaskID)
	if upstreamID == "" && len(env.Data) > 0 {
		var data submitData
		if err := common.Unmarshal(env.Data, &data); err == nil {
			upstreamID = firstNonEmpty(data.ID, data.TaskID, data.TaskIDAlt)
		}
	}
	if upstreamID == "" {
		// 上游回了 200 却没给任务 ID：既不能当成功（下游会拿到一个查不到的任务），
		// 也不能重试（可能已经建单计费）。按「结果未知」处理，交给上层判定。
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("task id is empty, body: %s", truncate(responseBody)),
			"invalid_response", http.StatusInternalServerError)
	}

	// 回给客户端的 ID 一律是本站公开 ID，不能泄露上游 taskId。
	//
	// resolution 报的是**实际交付档位**，不是客户请求值：本渠道所有模型
	// 都以 480P 生成、交付 720P，客户端传 1080P 也只会拿到 720P。
	// 回执再回一个 "1080P"，等于把「参数被忽略」包装成承诺。
	seconds := perSecondDefaultDuration
	if req, err := relaycommon.GetTaskRequest(c); err == nil {
		if resolved, durationErr := resolveDuration(req, executedModelName(info)); durationErr == nil {
			seconds = resolved
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"id":         info.PublicTaskID,
		"object":     "video",
		"model":      info.PublicModelName(),
		"status":     "queued",
		"resolution": deliveredResolution,
		"seconds":    strconv.Itoa(seconds),
	})
	return upstreamID, responseBody, nil
}

// ────────────────────────────── 轮询 ──────────────────────────────

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	base := strings.TrimRight(baseUrl, "/")
	uri := fmt.Sprintf("%s/v1/media/tasks/%s", base, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// taskDetail 是原生口 GET /v1/media/tasks/{id} 的任务详情。
//
// 只取用得到的字段：状态、结果文件、失败原因。
// progress 的形状不稳定（数字 / 字符串都见过），刻意不解析。
type taskDetail struct {
	TaskID         string          `json:"taskId"`
	TaskIDAlt      string          `json:"task_id"`
	ID             string          `json:"id"`
	Status         string          `json:"status"`
	ResultFileIDs  []string        `json:"resultFileIds"`
	ResultURLs     []string        `json:"resultUrls"`
	URL            string          `json:"url"`
	DownloadURL    string          `json:"downloadUrl"`
	FailureCode    string          `json:"failureCode"`
	FailureMessage string          `json:"failureMessage"`
	Code           string          `json:"code"`
	Message        string          `json:"message"`
	Data           json.RawMessage `json:"data"`
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	detail := taskDetail{}
	if err := common.Unmarshal(respBody, &detail); err != nil {
		return nil, fmt.Errorf("unmarshal task result failed: %w", err)
	}
	// 万一上游把这个端点也包了信封，就解一层。
	if detail.Status == "" && len(detail.Data) > 0 {
		var inner taskDetail
		if err := common.Unmarshal(detail.Data, &inner); err == nil && inner.Status != "" {
			detail = inner
		}
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToUpper(strings.TrimSpace(detail.Status)) {
	case "QUEUED", "PENDING", "SUBMITTED", "NOT_START":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "RUNNING", "IN_PROGRESS", "PROCESSING":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED", "PARTIAL":
		url, err := a.resolveResultURL(detail)
		if err != nil {
			// 终态却拿不到成片地址：不能判成功（客户端会拿到一个打不开的链接），
			// 也不能判失败（钱已经花了、片其实已经出了）。保持处理中，下轮重试。
			return nil, fmt.Errorf("resolve result url failed: %w", err)
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		result.Url = url
	case "FAILED", "REJECTED", "CANCELED", "CANCELLED", "EXPIRED":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(detail.FailureMessage, detail.FailureCode, detail.Message)
	default:
		// 未知状态一律当「处理中」：判失败会误退款，判成功会交付空产物。
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

// resolveResultURL 把任务详情换成一条**可直接抓取**的成片地址。
//
// 必须返回公网可达的直链而不是「带 Bearer 的端点」：这条地址后面还要交给
// 交付链路的下游处理（腾讯 MPS），那边没有我们的渠道密钥。
// 上游的 downloadUrl 是 3 天有效的稳定链接（token 在 URL 里），正好满足。
func (a *TaskAdaptor) resolveResultURL(detail taskDetail) (string, error) {
	// 详情里直接带地址就用（形状不确定，先认几种常见键）。
	for _, candidate := range []string{detail.URL, detail.DownloadURL} {
		if strings.TrimSpace(candidate) != "" {
			return candidate, nil
		}
	}
	for _, candidate := range detail.ResultURLs {
		if strings.TrimSpace(candidate) != "" {
			return candidate, nil
		}
	}
	if len(detail.ResultFileIDs) == 0 {
		return "", fmt.Errorf("task succeeded but returned no result file")
	}
	if a.baseURL == "" || a.apiKey == "" {
		return "", fmt.Errorf("adaptor not initialized, cannot resolve download url")
	}
	return a.fetchDownloadURL(detail.ResultFileIDs[0])
}

type downloadEnvelope struct {
	DownloadURL string          `json:"downloadUrl"`
	Data        json.RawMessage `json:"data"`
	Code        string          `json:"code"`
	Message     string          `json:"message"`
}

// fetchDownloadURL 调取件接口换稳定直链。
func (a *TaskAdaptor) fetchDownloadURL(fileID string) (string, error) {
	uri := fmt.Sprintf("%s/v1/media/files/%s/download", a.baseURL, fileID)
	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+a.apiKey)

	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return "", err
	}
	if client == nil {
		// 单测 / 未初始化 RelayHttpClient 时兜底，避免 nil 指针 panic。
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download api http %d: %s", resp.StatusCode, truncate(body))
	}
	var env downloadEnvelope
	if err := common.Unmarshal(body, &env); err != nil {
		return "", fmt.Errorf("unmarshal download response failed: %w", err)
	}
	if url := strings.TrimSpace(env.DownloadURL); url != "" {
		return url, nil
	}
	if len(env.Data) > 0 {
		var inner downloadEnvelope
		if err := common.Unmarshal(env.Data, &inner); err == nil && strings.TrimSpace(inner.DownloadURL) != "" {
			return inner.DownloadURL, nil
		}
	}
	return "", fmt.Errorf("download api returned no url: %s", truncate(body))
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ConvertToOpenAIVideo 让客户端能用 GET /v1/videos/{id} 查任务。
//
// 不实现它，relay_task.go 的 OpenAI Video 查询分支会落到
// 「not_implemented:68」—— 任务照跑照扣费，但客户端永远查不到结果，
// 30 秒这种长任务尤其致命（上架首测就踩到了）。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	video := originTask.ToOpenAIVideo()
	// ToOpenAIVideo 预置的 url 是 GetResultURL()：可能是 cos-proxy:// 内部标记，
	// 失败任务上还可能回退成上游原文。统一换成本站代理地址。
	delete(video.Metadata, "url")
	if originTask.Status == model.TaskStatusSuccess && strings.TrimSpace(originTask.GetResultURL()) != "" {
		video.SetMetadata("url", taskcommon.BuildProxyURL(originTask.TaskID))
	}
	if originTask.Status == model.TaskStatusFailure {
		video.Error = &dto.OpenAIVideoError{
			Message: taskcommon.NormalizeFailureReason(originTask.FailReason),
		}
	}
	return common.Marshal(video)
}

var _ channel.OpenAIVideoConverter = (*TaskAdaptor)(nil)

// ────────────────────────────── 交付前画质增强 ──────────────────────────────
//
// ⚠️ 内部实现细节，对外一律表述为「原生 720P」（见 constants.go 的
//    ModelNamePayPerCall 注释）。不要在模型描述、渠道备注、错误信息里提到它。

// AfterUpstreamSuccess 在成片出来后接管，提交交付前的画质增强。
//
// 复用 wan3 的实现：这一步与上游是谁无关，只要有一条可抓取的源片地址。
// 模型名白名单在本渠道内判，避免把别家渠道的模型卷进来。
func (a *TaskAdaptor) AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error) {
	if task == nil || !needsSuperResolution(task.Properties.OriginModelName) {
		return false, nil
	}
	return taskwan3.SubmitSuperResolution(ctx, task, upstreamURL)
}

func (a *TaskAdaptor) PollPostProcess(ctx context.Context, task *model.Task) (bool, error) {
	if task == nil || task.PrivateData.PostProcess == nil {
		return true, nil
	}
	if !needsSuperResolution(task.Properties.OriginModelName) {
		return true, nil
	}
	return taskwan3.PollSuperResolution(ctx, task)
}

var _ interface {
	AfterUpstreamSuccess(ctx context.Context, task *model.Task, upstreamURL string) (bool, error)
	PollPostProcess(ctx context.Context, task *model.Task) (bool, error)
} = (*TaskAdaptor)(nil)

// ────────────────────────────── 工具 ──────────────────────────────

// resolveDuration 统一 duration / seconds 两种写法。
//
// 两个口径，按**实际执行的模型名**区分：
//
//   - 按次档（ModelNamePayPerCall / seedance2.5-活动）：2–30 秒逐秒可选。
//     不传就按默认 30 秒；越界直接报错。不做「就近吸附」——
//     用户要越界时长时静默改档，是比报错更糟的体验。
//
//   - 按秒档（ModelNameTehuiRoute）：2–30 秒透传，缺省 5 秒，越界夹取。
//     与源链路 Wan3 的 resolveDuration 同口径（duration 优先、再 seconds、
//     再默认 5；<2 夹到 2、>30 夹到 30）—— 同一份请求分流前后结果必须一致。
func resolveDuration(req relaycommon.TaskSubmitReq, modelName string) (int, error) {
	if isPerSecondRouteModel(modelName) {
		return resolvePerSecondDuration(req), nil
	}
	duration := req.Duration
	if duration == 0 {
		// seconds 是字符串，可能是 "5" 也可能是 "5.0"，统一按整数秒处理。
		if raw := strings.TrimSpace(req.Seconds); raw != "" {
			var parsed int
			if _, err := fmt.Sscanf(raw, "%d", &parsed); err == nil {
				duration = parsed
			} else {
				return 0, fmt.Errorf("invalid seconds %q", raw)
			}
		}
	}
	if duration == 0 {
		return fixedDuration, nil
	}
	if duration < perCallMinDuration || duration > perCallMaxDuration {
		// 刻意不带模型名：本模型在脱敏名单里（model/upstream_redact.go），
		// 报错里的 "wan3.0" 会被替换成「上游」，出来是「上游-720p-按次」这种碎词。
		// 用户本来就知道自己调的是哪个模型，不需要在这里重复。
		return 0, fmt.Errorf("duration must be between %d and %d seconds, got %d", perCallMinDuration, perCallMaxDuration, duration)
	}
	return duration, nil
}

// resolvePerSecondDuration 复刻 Wan3 的时长归一：duration 优先、再 seconds、
// 再默认 5，最后夹到 [2,30]。
//
// 为什么不报错：源链路就是夹取（wan3.resolveDuration），分流后报错会让
// 同一份请求「走官网能跑、走心笔 400」—— 客户看到的是随机的行为差异。
func resolvePerSecondDuration(req relaycommon.TaskSubmitReq) int {
	duration := req.Duration
	if duration <= 0 {
		duration, _ = strconv.Atoi(strings.TrimSpace(req.Seconds))
	}
	if duration <= 0 {
		return perSecondDefaultDuration
	}
	if duration < perSecondMinDuration {
		return perSecondMinDuration
	}
	if duration > perSecondMaxDuration {
		return perSecondMaxDuration
	}
	return duration
}

// executedModelName 取本次实际执行的模型名（= 路由改写后的目标模型）。
//
// 不能用 BillingModelName：分流到本渠道后，计费口径仍是客户请求的特惠，
// 而时长口径要看**执行**的是不是按秒档。
func executedModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.OriginModelName)
}

// resolveRatio 取画面比例。
//
// 统一走 relaycommon.ResolveTaskRatio（全站唯一入口），优先级：
//
//	metadata.ratio → metadata.aspect_ratio → 顶层 ratio → 顶层 aspect_ratio
//	→ size（恰好是比例写法时）→ adaptive
//
// ⚠️ 2026-11-10：此前本函数**只读 metadata**，客户端按 OpenAI 风格把比例
// 放在请求体顶层（{"ratio":"9:16"}）时会被静默丢弃 → 落 adaptive，
// 成片比例不对但任务成功、照扣费。首轮修复漏了本包 —— 根因是全站性的。
//
// ⚠️ 心笔的口径与其它渠道不同：非法比例**报错**（invalid_ratio），
// 不静默降级 —— 心笔的档位是硬约束（它会直接拒单），提前报错能给客户
// 可读原因，而不是建单后失败再退款。
func resolveRatio(req relaycommon.TaskSubmitReq) (string, error) {
	result := relaycommon.ResolveTaskRatio(req, relaycommon.TaskRatioOptions{
		Candidates:    relaycommon.TaskRatioCandidates(ratios),
		DefaultRatio:  defaultRatio,
		RejectInvalid: true,
		// 心笔只认「size 恰好是比例写法」的情况（"16:9"）；
		// 像素写法（"1280x720"）不认，保持原有行为不变。
		SizeToRatio: func(size string) (string, bool) {
			if strings.Contains(size, ":") && !strings.Contains(strings.ToLower(size), "x") {
				return size, true
			}
			return "", false
		},
	})
	if result.Rejected != "" {
		return "", fmt.Errorf("ratio %q is not supported, use one of adaptive, 16:9, 9:16, 1:1, 4:3, 3:4", result.Rejected)
	}
	return result.Ratio, nil
}

// referenceMedia 是一次请求里收集到的三类参考素材。
//
// ⚠️ 2026-11-03 的教训仍然有效：取值必须覆盖 metadata.media / metadata.input.media。
// seedance2.5-活动 常驻前走渠道 17（有赞 Wan3），那个 adaptor 会读 metadata.media，
// 客户端已经形成这个用法。只读顶层 image/images/media[] 会让素材**静默丢掉** ——
// 任务照建单、照扣费、状态照 SUCCESS，成片却没有参考素材。
//
// 兼容口只收 https 直链 / data URL（实测 http 与内网地址被 FILE_3000037 拒），
// 本地先拦，用户拿到可读原因，而不是建单后失败再退款。
type referenceMedia struct {
	Images []string
	Videos []string
	Audios []string
}

// collectReferenceMedia 收集参考图 / 参考视频 / 参考音频。
//
// 上游 wan3.0-video 的 mediaConstraints 声明 VIDEO_REFERENCE 可带
// reference_image ≤10、reference_video ≤5、reference_audio ≤5，
// 兼容口一律收 https 直链（平台代取入库）或 data: URL。
//
// 首尾帧继续拒绝：兼容口没有声明尾帧字段，原生接口里首尾帧与参考件也互斥。
func collectReferenceMedia(req relaycommon.TaskSubmitReq) (referenceMedia, error) {
	var out referenceMedia
	if image := strings.TrimSpace(req.Image); image != "" {
		out.Images = append(out.Images, image)
	}
	for _, image := range req.Images {
		if trimmed := strings.TrimSpace(image); trimmed != "" {
			out.Images = append(out.Images, trimmed)
		}
	}
	if audio := strings.TrimSpace(req.Audio); audio != "" {
		out.Audios = append(out.Audios, audio)
	}
	for _, audio := range req.Audios {
		if trimmed := strings.TrimSpace(audio); trimmed != "" {
			out.Audios = append(out.Audios, trimmed)
		}
	}
	// 心笔兼容口的视频参考在顶层 video / videos（与 audio / audios 同形）。
	if video := strings.TrimSpace(req.Video); video != "" {
		out.Videos = append(out.Videos, video)
	}
	for _, video := range req.Videos {
		if trimmed := strings.TrimSpace(video); trimmed != "" {
			out.Videos = append(out.Videos, trimmed)
		}
	}
	// media 与 metadata.media / metadata.input.media 是并行关系，不是备选。
	for _, media := range req.Media {
		if err := classifyMedia(&out, media.Type, media.URL); err != nil {
			return referenceMedia{}, err
		}
	}
	metadataMedia, err := metadataReferenceMedia(req)
	if err != nil {
		return referenceMedia{}, err
	}
	out.Images = append(out.Images, metadataMedia.Images...)
	out.Videos = append(out.Videos, metadataMedia.Videos...)
	out.Audios = append(out.Audios, metadataMedia.Audios...)

	if len(out.Images) > maxReferenceImages {
		return referenceMedia{}, fmt.Errorf("too many reference images: %d, max %d", len(out.Images), maxReferenceImages)
	}
	if len(out.Videos) > maxReferenceVideos {
		return referenceMedia{}, fmt.Errorf("too many reference videos: %d, max %d", len(out.Videos), maxReferenceVideos)
	}
	if len(out.Audios) > maxReferenceAudios {
		return referenceMedia{}, fmt.Errorf("too many reference audios: %d, max %d", len(out.Audios), maxReferenceAudios)
	}
	for _, media := range out.all() {
		if strings.HasPrefix(media, "data:") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(media), "https://") {
			return referenceMedia{}, fmt.Errorf("reference media must be an https url, got %q", truncate([]byte(media)))
		}
	}
	return out, nil
}

// all 按图、视频、音频顺序返回全部素材，便于统一做 URL 校验。
func (m referenceMedia) all() []string {
	merged := make([]string, 0, len(m.Images)+len(m.Videos)+len(m.Audios))
	merged = append(merged, m.Images...)
	merged = append(merged, m.Videos...)
	merged = append(merged, m.Audios...)
	return merged
}

// classifyMedia 按 media.type 把素材归入对应类别。
//
// 空 type 视作图片（兼容老客户端），未知类型直接报错 —— 不能静默丢弃，
// 否则客户传的素材没进上游，任务却照建照扣费。
func classifyMedia(out *referenceMedia, mediaType, url string) error {
	trimmedURL := strings.TrimSpace(url)
	if trimmedURL == "" {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(mediaType))
	switch {
	case strings.Contains(normalized, "first_frame"), strings.Contains(normalized, "last_frame"):
		return fmt.Errorf("this model does not support first frame or last frame; use reference_image instead")
	case normalized == "" || strings.Contains(normalized, "image"):
		out.Images = append(out.Images, trimmedURL)
	case strings.Contains(normalized, "video"):
		out.Videos = append(out.Videos, trimmedURL)
	case strings.Contains(normalized, "audio"):
		out.Audios = append(out.Audios, trimmedURL)
	default:
		return fmt.Errorf("unsupported media type %q", mediaType)
	}
	return nil
}

// metadataReferenceMedia 从 metadata.media / metadata.input.media 里取三类素材。
//
// 返回错误只表示**能解析出但类型不认识**（由调用方拒单，避免静默丢弃）；
// 字段读不懂（不是数组、marshal 失败）一律当没有，不能因为一个残缺字段拒单。
func metadataReferenceMedia(req relaycommon.TaskSubmitReq) (referenceMedia, error) {
	var out referenceMedia
	if req.Metadata == nil {
		return out, nil
	}
	raw, ok := req.Metadata["media"]
	if !ok {
		if input, inputOK := req.Metadata["input"].(map[string]interface{}); inputOK {
			raw, ok = input["media"]
		}
	}
	if !ok || raw == nil {
		return out, nil
	}
	encoded, err := common.Marshal(raw)
	if err != nil {
		return out, nil
	}
	var media []relaycommon.TaskMedia
	if err := common.Unmarshal(encoded, &media); err != nil {
		return out, nil
	}
	for _, item := range media {
		if err := classifyMedia(&out, item.Type, item.URL); err != nil {
			return out, err
		}
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// truncate 截断日志里的响应体，避免把整段 base64 打进日志。
func truncate(body []byte) string {
	const max = 512
	if len(body) <= max {
		return string(body)
	}
	return string(body[:max]) + "..."
}
