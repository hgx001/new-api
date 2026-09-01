package wan3

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
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
	"github.com/tidwall/sjson"
)

// ============================
// Request / Response structures
// ============================

// wan3Error 是 wan3 统一的错误结构：{"ok":false,"error":{"code":...,"message":...}}
type wan3Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// submitRequest 提交生成任务的请求体（wan3 自定义协议，非 OpenAI 格式）
type submitRequest struct {
	Mode       string `json:"mode"`
	Model      string `json:"model"`
	Prompt     string `json:"prompt"`
	Resolution string `json:"resolution"`
	Ratio      string `json:"ratio"`
	Duration   int    `json:"duration"`
	Audio      bool   `json:"audio"`
	Watermark  bool   `json:"watermark"`
}

// submitResponse 提交响应。wan3 文档未给出响应体示例，
// 因此这里做多字段 / 多层嵌套的容错解析，以兼容 id / job_id / task_id / data.* / job.* 等形态。
type submitResponse struct {
	OK     bool            `json:"ok"`
	ID     string          `json:"id"`
	JobID  string          `json:"job_id"`
	TaskID string          `json:"task_id"`
	Data   *submitResponse `json:"data"`
	Job    *submitResponse `json:"job"`
	Video  *submitResponse `json:"video"`
	Error  *wan3Error      `json:"error"`
}

// extractID 按优先级从各个候选字段中取出任务 ID
func (r *submitResponse) extractID() string {
	if r == nil {
		return ""
	}
	for _, v := range []string{r.ID, r.JobID, r.TaskID} {
		if v != "" {
			return v
		}
	}
	if v := r.Data.extractID(); v != "" {
		return v
	}
	if v := r.Job.extractID(); v != "" {
		return v
	}
	return r.Video.extractID()
}

func (r *submitResponse) extractError() *wan3Error {
	if r == nil {
		return nil
	}
	if r.Error != nil {
		return r.Error
	}
	if r.Data != nil {
		return r.Data.extractError()
	}
	return nil
}

// taskResponse 轮询响应：GET /v1/videos/{id}
type taskResponse struct {
	OK           bool          `json:"ok"`
	Status       string        `json:"status"`
	HasResult    bool          `json:"has_result"`
	Progress     int           `json:"progress"`
	ErrorMessage string        `json:"error_message"`
	Data         *taskResponse `json:"data"`
	Job          *taskResponse `json:"job"`
	Error        *wan3Error    `json:"error"`
}

func (t *taskResponse) extractStatus() string {
	if t == nil {
		return ""
	}
	if t.Status != "" {
		return t.Status
	}
	if t.Data != nil {
		if v := t.Data.extractStatus(); v != "" {
			return v
		}
	}
	if t.Job != nil {
		return t.Job.extractStatus()
	}
	return ""
}

func (t *taskResponse) extractHasResult() bool {
	if t == nil {
		return false
	}
	if t.HasResult || t.Data != nil && t.Data.HasResult || t.Job != nil && t.Job.HasResult {
		return true
	}
	return false
}

func (t *taskResponse) extractErrorMessage() string {
	if t == nil {
		return ""
	}
	if t.ErrorMessage != "" {
		return t.ErrorMessage
	}
	if t.Error != nil && t.Error.Message != "" {
		return t.Error.Message
	}
	if t.Data != nil {
		return t.Data.extractErrorMessage()
	}
	if t.Job != nil {
		return t.Job.extractErrorMessage()
	}
	return ""
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType    int
	apiKey         string
	baseURL        string
	idempotencyKey string
}

// idempotencyKeyInvalidChars 匹配 Idempotency-Key 允许字符集（字母、数字、连字符）之外的字符
var idempotencyKeyInvalidChars = regexp.MustCompile(`[^A-Za-z0-9-]`)

// buildIdempotencyKey 由任务 ID 派生出稳定的幂等键（8–80 位字母/数字/连字符）。
// 必须是稳定值：new-api 内部重试时若每次生成新键，会导致 wan3 创建重复任务并重复扣分。
func buildIdempotencyKey(taskID string) string {
	key := "w3-" + idempotencyKeyInvalidChars.ReplaceAllString(taskID, "-")
	if len(key) > 80 {
		key = key[:80]
	}
	for len(key) < 8 {
		key += "-"
	}
	return key
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	// 幂等键必须绑定任务，保证重试不会重复提交。
	// 注意：轮询路径传入的 info 是空壳（TaskRelayInfo 可能为 nil），
	// 此时不需要幂等键，退化为时间戳键即可（先判内嵌指针再访问提升字段，避免 nil panic）。
	if info.TaskRelayInfo != nil && info.PublicTaskID != "" {
		a.idempotencyKey = buildIdempotencyKey(info.PublicTaskID)
	} else {
		a.idempotencyKey = "w3-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	return relaycommon.ValidateMultipartDirect(c, info)
}

// resolveDuration 解析时长并钳制到 wan3 允许的 2–30 秒
func resolveDuration(req relaycommon.TaskSubmitReq) int {
	d := req.Duration
	if d <= 0 {
		d, _ = strconv.Atoi(req.Seconds)
	}
	if d <= 0 {
		return defaultDuration
	}
	if d < minDuration {
		return minDuration
	}
	if d > maxDuration {
		return maxDuration
	}
	return d
}

// resolveResolution 从请求参数解析分辨率，优先级：
// 1. 顶层 resolution（"480P" / "720P"）
// 2. metadata.resolution（"480P" / "720P"）
// 3. size 映射
// 4. 默认 480P
func resolveResolution(req relaycommon.TaskSubmitReq) string {
	// 1. 顶层 resolution 优先
	if r := strings.ToUpper(strings.TrimSpace(req.Resolution)); r == "480P" || r == "720P" {
		return r
	}

	// 2. metadata.resolution
	if req.Metadata != nil {
		if r, ok := req.Metadata["resolution"].(string); ok {
			r = strings.ToUpper(strings.TrimSpace(r))
			if r == "480P" || r == "720P" {
				return r
			}
		}
	}

	// 3. size 映射
	if req.Size != "" {
		if r, ok := sizeToResolution[req.Size]; ok {
			return r
		}
	}

	// 4. 默认
	return defaultResolution
}

// sizeToResolution 将 size 字符串映射到分辨率
var sizeToResolution = map[string]string{
	"960x540":  "480P",
	"1280x720": "720P",
}

// resolveRatio 推断宽高比：优先 metadata 显式指定，其次由 size 推断，最后兜底 16:9
func resolveRatio(req relaycommon.TaskSubmitReq) string {
	if req.Metadata != nil {
		if r, ok := req.Metadata["ratio"].(string); ok && isValidRatio(r) {
			return r
		}
	}
	if r, ok := ratioFromSize(req.Size); ok {
		return r
	}
	return defaultRatio
}

func isValidRatio(r string) bool {
	switch r {
	case "adaptive", "16:9", "4:3", "1:1", "3:4", "9:16":
		return true
	}
	return false
}

// ratioFromSize 把 "1280x720" 这类尺寸映射到最接近的 wan3 ratio 档位
func ratioFromSize(size string) (string, bool) {
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
	candidates := map[string]float64{
		"16:9": 16.0 / 9.0,
		"9:16": 9.0 / 16.0,
		"4:3":  4.0 / 3.0,
		"3:4":  3.0 / 4.0,
		"1:1":  1.0,
	}
	ratio := float64(w) / float64(h)
	best := ""
	bestDiff := math.MaxFloat64
	for name, v := range candidates {
		if diff := math.Abs(ratio - v); diff < bestDiff {
			bestDiff = diff
			best = name
		}
	}
	if bestDiff > 0.25 {
		return "adaptive", true
	}
	return best, true
}

// EstimateBilling 按秒和分辨率计费：框架用 ModelPrice × seconds × size 计算配额
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	resolutionRatio := resolutionSizeRatio[resolveResolution(req)]
	if resolutionRatio == 0 {
		resolutionRatio = 1.0
	}
	return map[string]float64{
		"seconds": float64(resolveDuration(req)),
		"size":    resolutionRatio,
	}
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/videos", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	// wan3 要求必填 Idempotency-Key（8–80 位字母、数字或连字符）
	req.Header.Set("Idempotency-Key", a.idempotencyKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return nil, errors.New("field prompt is required")
	}

	// 从请求参数解析分辨率
	resolution := resolveResolution(req)

	body := submitRequest{
		Mode:       "t2v",
		Model:      upstreamModel,
		Prompt:     req.Prompt,
		Resolution: resolution,
		Ratio:      resolveRatio(req),
		Duration:   resolveDuration(req),
		Audio:      defaultAudio,
		Watermark:  false,
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal request body failed")
	}
	return bytes.NewReader(encoded), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err != nil {
		return nil, err
	}
	// wan3 提交成功返回 202 Accepted，而框架 relay_task.go 只接受 200，
	// 否则会直接抛 fail_to_fetch_task 而不解析响应体（任务其实已提交，会导致预扣费无法收回）。
	// 这里把 2xx 统一归一成 200。
	if resp != nil && resp.StatusCode == http.StatusAccepted {
		resp.StatusCode = http.StatusOK
	}
	return resp, nil
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}
	_ = resp.Body.Close()

	var dResp submitResponse
	if err := common.Unmarshal(responseBody, &dResp); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
		return
	}

	if apiErr := dResp.extractError(); apiErr != nil {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("wan3 error [%s]: %s", apiErr.Code, apiErr.Message),
			"upstream_error", resp.StatusCode)
		return
	}

	upstreamID := dResp.extractID()
	if upstreamID == "" {
		taskErr = service.TaskErrorWrapper(fmt.Errorf("wan3 returned empty job id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
		return
	}

	// 解析实际使用的分辨率
	req, _ := relaycommon.GetTaskRequest(c)
	resolution := resolveResolution(req)

	// 以 OpenAI video 风格返回，保持与其他视频通道一致的客户端体验
	now := time.Now().Unix()
	out := gin.H{
		"id":         info.PublicTaskID,
		"object":     "video",
		"model":      info.OriginModelName,
		"status":     "queued",
		"progress":   0,
		"created_at": now,
		"resolution": resolution,
		"seconds":    strconv.Itoa(resolveDurationForResponse(c)),
	}
	taskData, _ = common.Marshal(out)
	c.JSON(http.StatusOK, out)
	return upstreamID, taskData, nil
}

// resolveDurationForResponse 仅用于响应回显，取不到时回退默认值
func resolveDurationForResponse(c *gin.Context) int {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return defaultDuration
	}
	return resolveDuration(req)
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid task_id")
	}
	uri := fmt.Sprintf("%s/v1/videos/%s", baseUrl, taskID)
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

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res taskResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal task result failed")
	}

	taskResult := &relaycommon.TaskInfo{Code: 0}

	switch strings.ToUpper(res.extractStatus()) {
	case "SUBMITTING", "QUEUED", "PENDING":
		taskResult.Status = model.TaskStatusQueued
		taskResult.Progress = taskcommon.ProgressQueued
	case "RUNNING", "STORING", "PROCESSING", "IN_PROGRESS":
		taskResult.Status = model.TaskStatusInProgress
		taskResult.Progress = taskcommon.ProgressInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		if res.extractHasResult() {
			taskResult.Status = model.TaskStatusSuccess
			taskResult.Progress = taskcommon.ProgressComplete
			// Url 留空：由调用方用公开 task id 构造代理地址（见 controller/video_proxy.go）
		} else {
			// 已生成但成片尚未转存，继续轮询
			taskResult.Status = model.TaskStatusInProgress
			taskResult.Progress = taskcommon.ProgressInProgress
		}
	case "FAILED", "FAILURE", "CANCELLED", "CANCELED":
		taskResult.Status = model.TaskStatusFailure
		taskResult.Reason = res.extractErrorMessage()
		if taskResult.Reason == "" {
			taskResult.Reason = "task failed"
		}
	}

	return taskResult, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ConvertToOpenAIVideo 把任务数据转换成 OpenAI video 对象返回给客户端
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	data := task.Data
	var err error
	if data, err = sjson.SetBytes(data, "id", task.TaskID); err != nil {
		return nil, errors.Wrap(err, "set id failed")
	}
	if data, err = sjson.SetBytes(data, "object", "video"); err != nil {
		return nil, errors.Wrap(err, "set object failed")
	}
	return data, nil
}
