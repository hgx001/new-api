package kemeitv

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ─────────────────────────────────────────────────────────────────────────────
// Kemeitv（ai.kemeitv.cn）视频任务适配器。
//
// 契约（2026-11-19 按上游文档核对，docs/quickstart/*）：
//
//	提交   POST {base}/v1/video/generations   （OpenAI 兼容）
//	       Body: model / prompt / resolution / ratio / duration
//	             images[] / videos[] / audios[] / files[] / links[]
//	       素材支持「纯 URL 字符串」或「{url, role} 对象」两种写法。
//	轮询   GET  {base}/v1/video/generations/{task_id}
//
// 鉴权：`Authorization: Bearer sk-...`（上游三种方式任选，此处用 Bearer）。
//
// 响应形状（OpenAI 兼容）：
//
//	提交 {"id":"video_task_001","task_id":"video_task_001","status":"pending"}
//	完成 {"id":"...","status":"completed","data":[{"url":"https://.../wan_video.mp4"}]}
//	失败 HTTP 200 也可能带 "status":"failed"（上游《WAN视频生成》第 5 节明确）。
//
// ⚠️ 只接 wan3.0-video-tj 与 wan3.0-video-prime-tj 两个模型（用户 2026-11-19 口径）。
// ─────────────────────────────────────────────────────────────────────────────

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	// proxy 渠道代理：探测参考视频时长要在本地下载素材，必须与提交走同一条出口，
	// 否则「客户素材只有代理可达」的正常请求会被判成探测失败而拒单。
	proxy string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = strings.TrimRight(info.ChannelBaseUrl, "/")
	a.apiKey = info.ApiKey
	a.proxy = info.ChannelSetting.Proxy
}

// ────────────────────────────── 校验 ──────────────────────────────

// ValidateRequestAndSetAction 在本地把参数收敛到上游合同允许的范围内。
//
// 所有拒绝都在**建单之前**返回 400：上游一建单就开始计费，
// 参数错误不该以「建单后失败」的形式出现。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateMultipartDirect(c, info); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("field prompt is required"), "invalid_request", http.StatusBadRequest)
	}
	if _, err := resolveDuration(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	if _, err := resolveResolution(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_resolution", http.StatusBadRequest)
	}
	if _, err := resolveRatio(req); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_ratio", http.StatusBadRequest)
	}
	// 素材：先做「首尾帧 vs 参考类互斥」的场景闸门，再做数量上限检查。
	//
	// ⚠️ 顺序不能反：互斥错误要优先报出来。客户端传了 1 张首帧 + 2 张参考图时，
	// 先报「图片超上限」会让人误以为加到 11 张就行 —— 真正的问题是场景混用。
	bundle, err := collectMedia(req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_input_reference", http.StatusBadRequest)
	}
	// data URI 是图片任务通道的上游必然拒收的形状（客美只认公网 URL）。
	// 在**预扣费之前**转存成暂存图床地址；转存失败直接拒单，不留「扣完钱再失败」的单。
	staged, stageErr := stageDataURIImages(c, bundle)
	if stageErr != nil {
		return service.TaskErrorWrapperLocal(stageErr, "invalid_input_reference", http.StatusBadRequest)
	}
	if staged != bundle {
		c.Set(stagedMediaContextKey, staged)
	}
	// 参考视频按秒加价（生成秒数 + 参考视频总秒数），秒数在这里探测并塞进 context，
	// 由 EstimateBilling 读出来并入计费。
	//
	// 必须在**预扣之前**做：EstimateBilling 没有错误返回值，等到计费阶段才发现
	// 解析不出时长，就只能按生成时长收钱 —— 不报错、不少一单，只是每条带参考视频
	// 的单都少收。所以探测放在校验阶段，拿不到时长直接 400（不建单、不扣费）。
	if refErr := a.setReferenceVideoSeconds(c, bundle); refErr != nil {
		return service.TaskErrorWrapperLocal(refErr, "invalid_input_reference", http.StatusBadRequest)
	}
	return nil
}

// ────────────────────────────── 计费 ──────────────────────────────

// refVideoSecondsContextKey 是「参考视频计费秒数」在校验阶段写给计费阶段用的键。
//
// 口径（2026-11-20 用户拍板）：本渠道两个模型都对参考视频收费 ——
// 计费秒数 = 生成秒数 + 各条参考视频四舍五入后的整秒之和。
// 全站模型广场里**只有 hk-wan3 对参考视频不额外收费**（youzanwan3 的
// isHkWan3Model 分支），其余按秒计费的模型一律并入。
const refVideoSecondsContextKey = "kemeitv_ref_video_seconds"

// setReferenceVideoSeconds 探测全部参考视频的真实时长，累加后写入 context。
//
// 只探测、不在这里拒「超长」：上游 kemeitv 的参考视频时长上限未在文档里给出，
// 而此前本站也没有这道闸门 —— 贸然加限长会误杀本来能跑的长素材请求。
// 计费只需要「多长就收多长」，超长与否交给上游判定。
//
// ⚠️ 探测失败必须 fail-loud：拿不到时长就没法算钱，放行等于按 0 秒白送。
func (a *TaskAdaptor) setReferenceVideoSeconds(c *gin.Context, bundle *mediaBundle) error {
	if bundle == nil || len(bundle.Videos) == 0 {
		return nil
	}
	urls := make([]string, 0, len(bundle.Videos))
	for _, item := range bundle.Videos {
		urls = append(urls, item.URL)
	}
	client, err := service.GetHttpClientWithProxy(a.proxy)
	if err != nil {
		return fmt.Errorf("new proxy http client failed: %w", err)
	}
	// maxSeconds 传 0 = 不限长，只算秒数（见 ProbeAndEnforceReferenceVideos）。
	seconds, err := taskcommon.ProbeAndEnforceReferenceVideos(c.Request.Context(), urls, 0, client)
	if err != nil {
		return err
	}
	if seconds > 0 {
		c.Set(refVideoSecondsContextKey, seconds)
	}
	return nil
}

// EstimateBilling 返回计费倍率。
//
// 返回 seconds + size：按秒计费（基准价是 480P 的每秒价），
// 分辨率倍率默认按 1 : 2 : 4 走；个别模型的档位是人工定价，
// 由 modelResolutionPriceCNY 声明绝对价、运行时按基准价反推倍率
// （如 tj 的 720P 特价 ¥0.25/秒、1080P ¥0.552/秒）。
//
// ⚠️ 不返回 nil：返回 nil 会被当成 1 秒，30 秒的请求只收 1 秒的钱。
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		// 请求上下文缺失时按缺省秒数计费，绝不能返回 nil。
		return map[string]float64{"seconds": float64(defaultDuration), "size": 1.0}
	}
	duration, err := resolveDuration(req)
	if err != nil {
		// 时长已经在 Validate 阶段拦过；这里拿不到就交给上层按缺省秒数处理，
		// 绝不能返回一个「0 秒」把账单清零。
		duration = defaultDuration
	}
	resolution, err := resolveResolution(req)
	if err != nil {
		resolution = defaultResolution
	}
	// 分辨率倍率：先取标准 1 : 2 : 4，再按模型的绝对档位价反推倍率
	// （modelResolutionPriceCNY）。info 可能为 nil（单测/缺省路径），
	// 此时没有模型名，直接走标准倍率。
	modelName := ""
	if info != nil {
		modelName = info.OriginModelName
	}
	ratio := resolutionRatioForModel(modelName, resolution)
	if ratio == 0 {
		ratio = 1.0
	}
	// 参考视频按秒并入：2 秒生成 + 11 秒参考 = 按 13 秒收费，再乘分辨率倍率。
	// 秒数由校验阶段探测写入，这里只读不算（探测失败已在那边 400 拒掉）。
	seconds := duration
	if value, ok := c.Get(refVideoSecondsContextKey); ok {
		if refSeconds, ok := value.(int); ok && refSeconds > 0 {
			seconds += refSeconds
		}
	}
	return map[string]float64{
		"seconds": float64(seconds),
		"size":    ratio,
	}
}

func (a *TaskAdaptor) AdjustBillingOnSubmit(info *relaycommon.RelayInfo, taskData []byte) map[string]float64 {
	return nil
}

func (a *TaskAdaptor) AdjustBillingOnComplete(task *model.Task, taskResult *relaycommon.TaskInfo) int {
	return 0
}

// ────────────────────────────── 请求 ──────────────────────────────

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	// 渠道 base_url 可能带结尾斜杠（管理员手填），这里再兜一次，避免拼出 //v1。
	return strings.TrimRight(a.baseURL, "/") + submitPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return nil
}

// mediaItem 是上游的素材写法：纯 URL 字符串或 {"url": ..., "role": ...}。
//
// ⚠️ 统一用对象写法（带 role）：上游的「按张数推断默认 role」规则
// （1 张=首帧、2 张=首尾帧、3+ 张=参考图）在本站点不可控 ——
// 客户传 2 张参考图本意是「参考」，上游却会当成首尾帧。
// 显式写 role 才能保证语义一致。
type mediaItem struct {
	URL  string `json:"url"`
	Role string `json:"role,omitempty"`
}

// submitRequest 是上游 OpenAI 兼容提交体。
//
// ⚠️ 不设 negative_prompt / seed 等上游文档未列的字段 ——
// 上游是严格参数集校验（additionalProperties 风格），传未声明字段会 400。
type submitRequest struct {
	Model      string      `json:"model"`
	Prompt     string      `json:"prompt"`
	Resolution string      `json:"resolution,omitempty"`
	Ratio      string      `json:"ratio,omitempty"`
	Duration   int         `json:"duration,omitempty"`
	Images     []mediaItem `json:"images,omitempty"`
	Videos     []mediaItem `json:"videos,omitempty"`
	Audios     []mediaItem `json:"audios,omitempty"`
	Files      []mediaItem `json:"files,omitempty"`
	Links      []mediaItem `json:"links,omitempty"`
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	duration, err := resolveDuration(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_duration"}
	}
	resolution, err := resolveResolution(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_resolution"}
	}
	ratio, err := resolveRatio(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_ratio"}
	}
	media, err := collectMedia(req)
	if err != nil {
		return nil, &taskcommon.UserError{Err: err, Code: "invalid_input_reference"}
	}
	// 校验阶段已把 data URI 转存成公网 URL；命中就用转存结果，
	// 否则这里重新 collect 会拿到原始 data URI，转存等于白做。
	if staged, ok := c.Get(stagedMediaContextKey); ok {
		if bundle, ok := staged.(*mediaBundle); ok {
			media = bundle
		}
	}

	body := submitRequest{
		// 固定上游模型名，不信任渠道映射（见 constants.go 的 upstreamModelFor）。
		Model:      upstreamModelFor(info.OriginModelName),
		Prompt:     req.Prompt,
		Duration:   duration,
		Resolution: resolution,
		Ratio:      ratio,
		Images:     media.Images,
		Videos:     media.Videos,
		Audios:     media.Audios,
		Files:      media.Files,
		Links:      media.Links,
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

// submitResponse 是提交响应。上游文档给的形状是
// {"id":"video_task_001","task_id":"video_task_001","status":"pending"}。
//
// 两个字段都收：不同版本上游可能只回其中一个。
type submitResponse struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Error  *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	var parsed submitResponse
	if err := common.Unmarshal(responseBody, &parsed); err != nil {
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("unmarshal response failed: %w, body: %s", err, truncate(responseBody)),
			"unmarshal_response_body_failed", http.StatusInternalServerError)
	}

	upstreamID := firstNonEmpty(parsed.ID, parsed.TaskID)
	if upstreamID == "" {
		// 上游回了 200 却没给任务 ID：既不能当成功（下游会拿到一个查不到的任务），
		// 也不能重试（可能已经建单计费）。按「结果未知」处理，交给上层判定。
		return "", nil, service.TaskErrorWrapper(
			fmt.Errorf("task id is empty, body: %s", truncate(responseBody)),
			"invalid_response", http.StatusInternalServerError)
	}

	// ⚠️ 必须在这里写出响应体。渠道框架**不会**替适配器回写 body ——
	//	缺了这一句，客户端拿到 HTTP 200 但 body 为空，连 task id 都取不到，
	//	只能回数据库/日志捞单，其他适配器（wan3 / manwu）也都在 DoResponse 里
	//	自己 c.JSON。
	//
	// 回的是 OpenAI Video 形状，且 id 用**对外公开单号**（不是上游单号）：
	//	上游单号会暴露真实货源，公开单号才是客户该拿到的东西。
	ov := dto.NewOpenAIVideo()
	ov.ID = publicTaskID(info)
	ov.TaskID = publicTaskID(info)
	ov.CreatedAt = time.Now().Unix()
	ov.Model = publicModelName(info)
	c.JSON(http.StatusOK, ov)

	return upstreamID, responseBody, nil
}

// publicTaskID 取对外公开单号。
//
// ⚠️ 优先用 info.PublicTaskID —— 它是本站生成的 task_xxx，客户按它查任务。
//
//	取不到才退回上游单号（此时至少还能查得到结果）。
func publicTaskID(info *relaycommon.RelayInfo) string {
	// ⚠️ 必须走 PublicTaskIDOrEmpty()：PublicTaskID 挂在**内嵌指针** TaskRelayInfo 上，
	//	直接访问 info.PublicTaskID 在 TaskRelayInfo 为 nil 时会 panic。
	return strings.TrimSpace(info.PublicTaskIDOrEmpty())
}

// publicModelName 取对外模型名（改派过就报客户请求的源模型）。
func publicModelName(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return info.PublicModelName()
}

// ────────────────────────────── 轮询 ──────────────────────────────

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	base := strings.TrimRight(baseUrl, "/")
	uri := fmt.Sprintf(base+fetchPath, taskID)

	req, err := http.NewRequest(http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

// taskDetail 是 GET /v1/video/generations/{id} 的响应。
//
// 上游文档给的完成形状是 {"id","status","data":[{"url":"..."}]}；
// 失败时 HTTP 200 也可能带 status:"failed" 与 error。
type taskDetail struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	Status string `json:"status"`
	Data   []struct {
		URL string `json:"url"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
	// 部分上游把成片地址平铺在顶层。
	URL string `json:"url,omitempty"`
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	detail := taskDetail{}
	if err := common.Unmarshal(respBody, &detail); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	result := &relaycommon.TaskInfo{Code: 0}
	switch strings.ToLower(strings.TrimSpace(detail.Status)) {
	case "pending", "queued", "submitted", "created", "in_queue":
		result.Status = model.TaskStatusQueued
		result.Progress = taskcommon.ProgressQueued
	case "running", "processing", "in_progress", "generating":
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	case "completed", "succeeded", "success":
		url := resolveResultURL(detail)
		if url == "" {
			// 终态却拿不到成片地址：不能判成功（客户端会拿到一个打不开的链接），
			// 也不能判失败（钱已经花了、片其实已经出了）。保持处理中，下轮重试。
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
		} else {
			result.Status = model.TaskStatusSuccess
			result.Progress = taskcommon.ProgressComplete
			result.Url = url
		}
	case "failed", "rejected", "canceled", "cancelled", "expired":
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(errorMessage(detail.Error), detail.Status, taskcommon.ReasonContentModeration)
	default:
		// 未知状态一律当「处理中」：判失败会误退款，判成功会交付空产物。
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

// ConvertToOpenAIVideo 把库里的任务回执转成 OpenAI video 对象。
//
// ⚠️ 这个方法是 **/v1/videos/{id} 查询能不能用的前提**：
//
//	relay_task.go 的 videoFetchByIDRespBodyBuilder 走 OpenAI Video API 分支时，
//	先问适配器要不要实现 channel.OpenAIVideoConverter；没实现就直接返回
//	`not_implemented:<platform>`（HTTP 501）。
//	后果是：任务照跑照扣费，但客户端**永远查不到结果** —— 只有本站轮询能出片。
//
// 成片地址口径：
//
//	· 上游给的 data[0].url 是**带签名的临时直链**（上游存储，签名有有效期）。
//	  但库里存的是轮询那一刻的地址，客户端可能过一会儿才来取片，
//	  签名过期就 404 —— 所以对外统一给**本站 /content 代理地址**，
//	  由 controller.VideoProxy 用渠道密钥回源实时取。
//	· 任务没成功时不给地址：给了也是错的（客户端应该继续轮询）。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	detail := taskDetail{}
	if len(originTask.Data) > 0 {
		// 解不开不算致命：库里还有状态与失败原因，足以回一个可读的响应。
		_ = common.Unmarshal(originTask.Data, &detail)
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.TaskID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = originTask.Properties.OriginModelName

	if originTask.Status == model.TaskStatusSuccess {
		if publicURL := taskcommon.BuildProxyURL(originTask.TaskID); publicURL != "" {
			openAIVideo.SetMetadata("url", publicURL)
		}
	}

	// 失败原因优先取上游回执原文（它更具体，比如「Failed to download ...」），
	// 回执里没有才用本站记的 FailReason。
	if originTask.Status == model.TaskStatusFailure {
		message := firstNonEmpty(
			errorMessage(detail.Error),
			strings.TrimSpace(originTask.FailReason),
			taskcommon.ReasonContentModeration,
		)
		code := ""
		if detail.Error != nil {
			code = detail.Error.Code
		}
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: taskcommon.NormalizeFailureReason(message),
			Code:    code,
		}
	}

	return common.Marshal(openAIVideo)
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ────────────────────────────── 内部工具 ──────────────────────────────

// resolveResultURL 从任务详情里取成片地址。
//
// 上游文档给的是 data[0].url；同时兼容顶层 url —— 不同版本形状有差异，
// 宁可多认几种，也不要因为形状猜错把已计费的成功任务判成未完成。
func resolveResultURL(detail taskDetail) string {
	for _, item := range detail.Data {
		if url := strings.TrimSpace(item.URL); url != "" {
			return url
		}
	}
	return strings.TrimSpace(detail.URL)
}

func errorMessage(err *struct {
	Message string `json:"message"`
	Code    string `json:"code"`
}) string {
	if err == nil {
		return ""
	}
	return firstNonEmpty(err.Message, err.Code)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(body []byte) string {
	const maxLen = 512
	if len(body) <= maxLen {
		return string(body)
	}
	return string(body[:maxLen]) + "..."
}

// resolveDuration 解析生成时长（秒）。
//
// 取值顺序：顶层 duration → 顶层 seconds → metadata.duration/seconds → 缺省 5 秒。
//
// ⚠️ 顶层 seconds 必须认（2026-11-24 排障）：漫屋等 OpenAI 视频协议客户端把时长放在
// `{"seconds": 2}`（Sora 协议字段），本适配器的上游合同用 duration。此前这里只读
// duration 与 metadata，seconds 被静默丢弃 → 按缺省 5 秒提交上游：实际生成 5 秒、
// 预扣与结算也都按 5 秒，客户传 2 秒却被多扣。属于「参数被吞」型故障，
// 与 relay_info.go 里 ratio 顶层字段那段是同一类问题。
func resolveDuration(req relaycommon.TaskSubmitReq) (int, error) {
	if req.Duration > 0 {
		if req.Duration < minDuration || req.Duration > maxDuration {
			return 0, fmt.Errorf("duration must be between %d and %d seconds", minDuration, maxDuration)
		}
		return req.Duration, nil
	}
	if raw := strings.TrimSpace(req.Seconds); raw != "" {
		seconds, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("seconds must be an integer number of seconds")
		}
		// 显式 0 视为未传：继续看 metadata 与缺省，避免客户误传 0 时把账单清零。
		if seconds != 0 {
			if seconds < minDuration || seconds > maxDuration {
				return 0, fmt.Errorf("duration must be between %d and %d seconds", minDuration, maxDuration)
			}
			return seconds, nil
		}
	}
	if req.Metadata != nil {
		for _, key := range []string{"duration", "seconds"} {
			raw, ok := req.Metadata[key]
			if !ok || raw == nil {
				continue
			}
			seconds, ok := toInt(raw)
			if !ok || seconds == 0 {
				continue
			}
			if seconds < minDuration || seconds > maxDuration {
				return 0, fmt.Errorf("duration must be between %d and %d seconds", minDuration, maxDuration)
			}
			return seconds, nil
		}
	}
	return defaultDuration, nil
}

// RequestDuration 按本渠道的口径解析请求时长（2–30 秒，缺省 5 秒）。
//
// 为什么导出：条件路由（middleware/wan3_ref_video_hk_route.go）把一份
// tj / prime-tj 的请求改派到 hk-wan3 时，必须把**本渠道解析出来的秒数**
// 显式写成目标请求里的 `duration`（hk-wan3 的下界是 4 秒、且只认 duration）。
// 同一份解析被 adaptor 与路由层共用，才不会出现「路由层按一个数、adaptor 按另一个数」。
func RequestDuration(req relaycommon.TaskSubmitReq) (int, error) {
	return resolveDuration(req)
}

// RequestResolution 按本渠道的口径解析输出分辨率档位（缺省 720P）。
//
// 导出理由同 RequestDuration：路由层要用**源模型的档位**去覆盖目标 adaptor
// 的 size 倍率，否则 1080P 会按 hk-wan3 的 2.667 倍收（tj 口径是 4 倍）。
func RequestResolution(req relaycommon.TaskSubmitReq) (string, error) {
	return resolveResolution(req)
}

// BillingSizeRatioForResolution 返回该模型该分辨率档相对 480P 基准价的倍率，
// 包含按模型声明的绝对档位价（tj 的 720P/1080P、prime-tj 的 720P/1080P）。
//
// 供条件路由写进 constant.ContextKeyRouteBillingSizeRatio：计费按客户请求的
// 源模型算，目标 adaptor 给的自己的倍率必须被覆盖。
func BillingSizeRatioForResolution(model, resolution string) float64 {
	resolution = normalizeResolution(resolution)
	if resolution == "" {
		resolution = defaultResolution
	}
	if ratio := resolutionRatioForModel(strings.TrimSpace(model), resolution); ratio > 0 {
		return ratio
	}
	return 1.0
}

// toInt 把 metadata 里可能是数字或数字字符串的值转成 int。
func toInt(raw any) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// resolveResolution 解析输出分辨率档位。
//
// 取值顺序：顶层 resolution → metadata.resolution → size 反推 → 缺省 720P。
func resolveResolution(req relaycommon.TaskSubmitReq) (string, error) {
	if r := normalizeResolution(req.Resolution); r != "" {
		return r, nil
	}
	if req.Metadata != nil {
		if raw, ok := req.Metadata["resolution"].(string); ok {
			if r := normalizeResolution(raw); r != "" {
				return r, nil
			}
		}
	}
	if r, ok := resolutionFromSize(req.Size); ok {
		return r, nil
	}
	return defaultResolution, nil
}

// normalizeResolution 归一分辨率写法。
//
// 上游文档写 `resolution`（如 "720P"、"1080P"，`size` 会转大写），
// 所以这里统一大写并只认三档；其余值返回 ""（走缺省或报错）。
func normalizeResolution(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "480", "480P":
		return resolution480P
	case "720", "720P":
		return resolution720P
	case "1080", "1080P":
		return resolution1080P
	default:
		return ""
	}
}

// resolutionFromSize 从 size 反推分辨率档位。
//
// size 可能是 "1280x720" 这类尺寸，也可能是直接写的 "720P"。
func resolutionFromSize(size string) (string, bool) {
	size = strings.ReplaceAll(strings.TrimSpace(size), "*", "x")
	if size == "" {
		return "", false
	}
	if r := normalizeResolution(size); r != "" {
		return r, true
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	switch {
	case w >= 1920 || h >= 1920:
		return resolution1080P, true
	case w >= 1280 || h >= 1280:
		return resolution720P, true
	default:
		return resolution480P, true
	}
}

// resolveRatio 解析画幅比例。
//
// 取值优先级统一走 relaycommon.ResolveTaskRatio：
// metadata.ratio > metadata.aspect_ratio > 顶层 ratio > 顶层 aspect_ratio > size 推断。
func resolveRatio(req relaycommon.TaskSubmitReq) (string, error) {
	result := relaycommon.ResolveTaskRatio(req, relaycommon.TaskRatioOptions{
		Candidates: relaycommon.TaskRatioCandidates{
			"16:9": true, "9:16": true,
			"4:3": true, "3:4": true,
			"1:1": true,
		},
		DefaultRatio: defaultRatio,
		SizeToRatio:  ratioFromSize,
	})
	if result.Ratio == "" {
		return defaultRatio, nil
	}
	return result.Ratio, nil
}

// ratioFromSize 把 "1280x720" 这类尺寸映射到画幅比例。
func ratioFromSize(size string) (string, bool) {
	size = strings.ReplaceAll(strings.TrimSpace(size), "*", "x")
	if size == "" {
		return "", false
	}
	parts := strings.Split(size, "x")
	if len(parts) != 2 {
		return "", false
	}
	w, errW := strconv.Atoi(strings.TrimSpace(parts[0]))
	h, errH := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errW != nil || errH != nil || w <= 0 || h <= 0 {
		return "", false
	}
	candidates := []struct {
		ratio string
		value float64
	}{
		{"16:9", 16.0 / 9.0},
		{"9:16", 9.0 / 16.0},
		{"4:3", 4.0 / 3.0},
		{"3:4", 3.0 / 4.0},
		{"1:1", 1.0},
	}
	actual := float64(w) / float64(h)
	for _, c := range candidates {
		if nearlyEqual(actual, c.value) {
			return c.ratio, true
		}
	}
	return "", false
}

func nearlyEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.01
}

// ────────────────────────────── 素材 ──────────────────────────────

// mediaBundle 是归一后的素材集合。
type mediaBundle struct {
	Images []mediaItem
	Videos []mediaItem
	Audios []mediaItem
	Files  []mediaItem
	Links  []mediaItem
}

// metadataMedia 解析 metadata.media / metadata.input.media 里的素材。
//
// 与 wan3 的 metadataMedia 逐字同口径（metadata.media 优先，input.media 兜底）：
// 两处读法一旦漂移，同一份请求在官网与客美链路上就会解析出不同素材。
func metadataMedia(metadata map[string]interface{}) ([]relaycommon.TaskMedia, error) {
	if metadata == nil {
		return nil, nil
	}
	raw, ok := metadata["media"]
	if !ok {
		if input, inputOK := metadata["input"].(map[string]interface{}); inputOK {
			raw, ok = input["media"]
		}
	}
	if !ok || raw == nil {
		return nil, nil
	}
	encoded, err := common.Marshal(raw)
	if err != nil {
		return nil, errors.Wrap(err, "marshal metadata media failed")
	}
	var media []relaycommon.TaskMedia
	if err := common.Unmarshal(encoded, &media); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata media failed")
	}
	return media, nil
}

// collectMedia 归一请求里的全部素材，并做场景互斥与数量上限校验。
//
// 归一顺序（与站内其它 wan3 渠道一致）：
// 显式 media → metadata.media / metadata.input.media → 老字段（images / image / input_reference）。
//
// ⚠️ metadata.input.media 这一层是 2026-11-21 补的：客户买的是
// wan3.0-video-官网，ArcReel 按官网契约把首帧放在 metadata.input.media，
// 网关改派到客美后原实现只读顶层 media，首帧被静默丢弃 ——
// 任务照建、钱照扣、成片完全没有首帧。形状与 wan3 的 metadataMedia 同口径。
//
// ⚠️ 老字段（images / image / input_reference）默认按**首帧**处理：
// 这三个字段的既有语义就是「第一张图当首帧」，与上游「1 张图 = first_frame」
// 的推断规则一致。老字段无法表达「参考图」语义 —— 需要多张参考图请用
// media 字段并显式写 role。
func collectMedia(req relaycommon.TaskSubmitReq) (*mediaBundle, error) {
	bundle := &mediaBundle{}

	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		fromMetadata, err := metadataMedia(req.Metadata)
		if err != nil {
			return nil, err
		}
		media = fromMetadata
	}

	for _, item := range media {
		url := strings.TrimSpace(item.URL)
		if url == "" {
			return nil, fmt.Errorf("media url is required")
		}
		role := strings.TrimSpace(item.Type)
		switch role {
		case roleFirstFrame, roleLastFrame, roleReferenceImage, roleReferenceVideo,
			roleReferenceAudio, roleFile, roleLink:
		default:
			return nil, fmt.Errorf("media type %q is unsupported", role)
		}
		entry := mediaItem{URL: url, Role: role}
		switch role {
		case roleFirstFrame, roleLastFrame, roleReferenceImage:
			bundle.Images = append(bundle.Images, entry)
		case roleReferenceVideo:
			bundle.Videos = append(bundle.Videos, entry)
		case roleReferenceAudio:
			bundle.Audios = append(bundle.Audios, entry)
		case roleFile:
			bundle.Files = append(bundle.Files, entry)
		case roleLink:
			bundle.Links = append(bundle.Links, entry)
		}
	}

	// 老字段：images / image / input_reference 三者取「第一个非空」，
	// 保持既有语义（避免同一张图被重复计入）。
	legacy := req.Images
	if len(legacy) == 0 && strings.TrimSpace(req.Image) != "" {
		legacy = []string{req.Image}
	}
	if len(legacy) == 0 && strings.TrimSpace(req.InputReference) != "" {
		legacy = []string{req.InputReference}
	}
	for _, url := range legacy {
		url = strings.TrimSpace(url)
		if url == "" {
			continue
		}
		role := roleFirstFrame
		if len(bundle.Images) > 0 {
			// 已有显式 media 的图时，老字段不再追加首帧 ——
			// 显式 media 优先，避免同一个位置塞两张首帧。
			continue
		}
		bundle.Images = append(bundle.Images, mediaItem{URL: url, Role: role})
	}

	// 顶层 videos / audios 也是常见写法，并入对应集合。
	for _, url := range req.Videos {
		if url = strings.TrimSpace(url); url != "" {
			bundle.Videos = append(bundle.Videos, mediaItem{URL: url, Role: roleReferenceVideo})
		}
	}
	for _, url := range req.Audios {
		if url = strings.TrimSpace(url); url != "" {
			bundle.Audios = append(bundle.Audios, mediaItem{URL: url, Role: roleReferenceAudio})
		}
	}
	if url := strings.TrimSpace(req.Audio); url != "" {
		bundle.Audios = append(bundle.Audios, mediaItem{URL: url, Role: roleReferenceAudio})
	}

	if err := validateMedia(bundle); err != nil {
		return nil, err
	}
	return bundle, nil
}

// ValidateIncomingMedia 用本渠道的素材契约预检一份「还没改派」的请求。
//
// 为什么导出：条件路由（middleware 里把请求改派到本渠道的那些规则）必须在
// **改派之前**判断「目标链路会不会直接 400」。源链路与目标链路对素材的宽容度
// 常常不同（官网适配器根本不读素材、data URI 与超限素材都被静默丢弃），
// 不预检就会把「以前能跑」的请求变成 400。
//
// 复用 collectMedia 的同一份实现（数量上限、首尾帧互斥、后缀白名单），
// 不另写一套判据 —— 两套判据迟早漂。
func ValidateIncomingMedia(req relaycommon.TaskSubmitReq) error {
	_, err := collectMedia(req)
	return err
}

// validateMedia 做场景互斥与数量上限校验。
//
// ⚠️ 上游文档明确「首尾帧与参考类素材不可混用」，混用上游报 400。
// 本地先拦：上游一建单就计费，混用请求不该走到那里。
func validateMedia(bundle *mediaBundle) error {
	// 先查素材后缀 —— 上游按后缀判格式，拒单发生在建单之后（钱已预扣）。
	if err := validateMediaExtensions(bundle); err != nil {
		return err
	}

	// 分别统计首帧 / 尾帧 / 参考图的数量。
	//
	// ⚠️ 首帧与尾帧**各最多一张**，两者**合计可以有两张**（首尾帧场景）。
	//	所以不能拿「首尾帧合计 > 1」当互斥判据 —— 那是把合法的
	//	「首帧 + 尾帧」两图请求误判成错误。
	firstCount, lastCount, referenceImageCount := 0, 0, 0
	for _, item := range bundle.Images {
		switch item.Role {
		case roleFirstFrame:
			firstCount++
		case roleLastFrame:
			lastCount++
		case roleReferenceImage:
			referenceImageCount++
		}
	}
	if firstCount > 1 || lastCount > 1 {
		return fmt.Errorf("at most one first_frame and one last_frame are accepted")
	}
	frameCount := firstCount + lastCount
	if frameCount > 0 {
		// 首尾帧场景：其余参考类素材一律不许混用。
		if referenceImageCount > 0 {
			return fmt.Errorf("first_frame / last_frame cannot be mixed with reference_image")
		}
		if len(bundle.Videos) > 0 || len(bundle.Audios) > 0 ||
			len(bundle.Files) > 0 || len(bundle.Links) > 0 {
			return fmt.Errorf("first_frame / last_frame cannot be mixed with reference videos, audios, files or links")
		}
	}
	// 参考场景：首尾帧与参考类素材同样互斥（上面已覆盖帧侧，这里覆盖反向）。
	if len(bundle.Videos) > 0 || len(bundle.Audios) > 0 ||
		len(bundle.Files) > 0 || len(bundle.Links) > 0 {
		if frameCount > 0 {
			return fmt.Errorf("reference videos, audios, files or links cannot be mixed with first_frame / last_frame")
		}
	}
	if len(bundle.Images) > maxReferenceImages {
		return fmt.Errorf("at most %d images are accepted", maxReferenceImages)
	}
	if len(bundle.Videos) > maxReferenceVideos {
		return fmt.Errorf("at most %d reference videos are accepted", maxReferenceVideos)
	}
	if len(bundle.Audios) > maxReferenceAudios {
		return fmt.Errorf("at most %d reference audios are accepted", maxReferenceAudios)
	}
	if len(bundle.Files) > maxReferenceFiles {
		return fmt.Errorf("at most %d reference files are accepted", maxReferenceFiles)
	}
	if len(bundle.Links) > maxReferenceLinks {
		return fmt.Errorf("at most %d reference links are accepted", maxReferenceLinks)
	}
	// 参考音频不能单独作为唯一参考（上游文档：至少需一张参考图或一段参考视频）。
	if len(bundle.Audios) > 0 && len(bundle.Images) == 0 && len(bundle.Videos) == 0 {
		return fmt.Errorf("reference audio requires at least one reference image or reference video")
	}
	return nil
}

// validateMediaExtensions 校验素材 URL 的文件后缀是否被上游接受。
//
// ⚠️ 上游**按后缀**判素材格式（2026-11-19 真机实测）：
//
//	传 .m4a 音频 → 上游回 "format m4a is not supported. Supported formats: ['wav', 'mp3']"。
//	这个拒绝发生在**建单之后**：钱已预扣、客户要等退款，体验最差。
//	所以本地校验阶段就拦，并明确告诉客户该用什么格式。
//
// ⚠️ 只看 URL 的**路径后缀**，不看 query：素材 URL 常带签名参数
//
//	（?sig=...&expires=...），不能因为有 query 就判不出后缀。
//	取不到后缀时**不拦** —— 上游那边还有一层真实下载校验，
//	本地过度拦截会误杀合法素材（改用无后缀的 COS 签名 URL 就很常见）。
func validateMediaExtensions(bundle *mediaBundle) error {
	checks := []struct {
		kind    string
		items   []mediaItem
		allowed []string
	}{
		{"image", bundle.Images, allowedImageExts},
		{"video", bundle.Videos, allowedVideoExts},
		{"audio", bundle.Audios, allowedAudioExts},
	}
	for _, check := range checks {
		for _, item := range check.items {
			ext := mediaExtension(item.URL)
			if ext == "" {
				continue
			}
			if !containsString(check.allowed, ext) {
				return fmt.Errorf("reference %s format %q is not supported, supported formats: %s",
					check.kind, strings.TrimPrefix(ext, "."), formatExtList(check.allowed))
			}
		}
	}
	return nil
}

// mediaExtension 取 URL 路径里的文件后缀（小写，含点）。
//
// 取不到就返回空串 —— 见 validateMediaExtensions 的说明：宁可不拦，也不误杀。
func mediaExtension(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	if ext == "." {
		return ""
	}
	return ext
}

// formatExtList 把允许后缀列表拼成给客户看的说明。
func formatExtList(allowed []string) []string {
	out := make([]string, 0, len(allowed))
	for _, ext := range allowed {
		out = append(out, strings.TrimPrefix(ext, "."))
	}
	return out
}

func containsString(list []string, target string) bool {
	for _, item := range list {
		if item == target {
			return true
		}
	}
	return false
}
