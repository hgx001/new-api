package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	//PerCallPrice   types.PriceData
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			return service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			sizeStr, _ := taskData["size"].(string)
			if info.PriceData.OtherRatios == nil {
				info.PriceData.OtherRatios = map[string]float64{}
			}
			info.PriceData.OtherRatios["seconds"] = float64(seconds)
			info.PriceData.OtherRatios["size"] = 1
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.OtherRatios["size"] = 1.666667
			}
		}
	}

	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)

	// 记录条件路由改派前的源模型名：**只用于日志展示**，计费仍走
	// info.OriginModelName（= 改派后的目标模型），否则 30 秒 → 豆包按次
	// ¥3.00 会被算成源模型的 ¥0.20/秒。详见 constant.ContextKeyRouteOriginModel。
	info.RouteOriginModel = common.GetContextKeyString(c, constant.ContextKeyRouteOriginModel)

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
	}
	info.PriceData = priceData

	// 5. 计费估算：让适配器根据用户请求提供 OtherRatios（时长、分辨率等）
	//    必须在 ModelPriceHelperPerCall 之后调用（它会重建 PriceData）。
	//    ResolveOriginTask 可能已在 remix 路径中预设了 OtherRatios，此处合并。
	if estimatedRatios := adaptor.EstimateBilling(c, info); len(estimatedRatios) > 0 {
		for k, v := range estimatedRatios {
			info.PriceData.AddOtherRatio(k, v)
		}
	}

	// 6. 将 OtherRatios 应用到基础额度
	//
	// 必须先把所有倍率乘完再取整，不能边遍历边 int()：OtherRatios 是 map，
	// Go 的 map 迭代顺序是随机的，逐项截断会让**同一个请求两次扣费得到不同结果**。
	// 2026-10-05 生产实测（wan3.0-smart 1080p/5 秒，seconds=5 与 size=0.30/0.28 都不是 1）：
	//   base=int(0.038356164383561646*500000)=19178
	//   size 先  → int(19178*0.30/0.28)=20547 → *5 = 102735
	//   seconds 先 → int(19178*5)=95990 → *0.30/0.28 = 102739
	// 同一请求 3 次里出现了 102739、102739、102735 两个值，且每级截断误差会累积。
	// service/task_billing.go 与 service/text_quota.go 都是单次累乘后取整，与此保持一致。
	if !common.StringsContains(constant.TaskPricePatches, modelName) {
		info.PriceData.Quota = applyTaskOtherRatios(info.PriceData.Quota, info.PriceData.OtherRatios)
	}

	// 7. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
			return nil, service.TaskErrorFromAPIError(apiErr)
		}
	}

	// 8. 构建请求体
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		// 适配器可能已经给出「请求不合法」的**本端**错误（能力越界、入参非法）。
		// 直接降级成 500 build_request_failed 有两个坏后果：① 归咎渠道，500 会
		// 触发重试与 AutoBan，一个用户传错参数就能把渠道打掉；② 客户端拿到
		// 「服务端错误」而不是可读的原因。所以 taskcommon.UserError 转成 400 透传。
		var userErr *taskcommon.UserError
		if errors.As(err, &userErr) {
			return nil, service.TaskErrorWrapperLocal(userErr.Err, userErr.Code, http.StatusBadRequest)
		}
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}

	// 9. 发送请求
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		// 提交超时/请求中断属于「结果未知」：上游可能已受理，重试会重复提交计费，
		// 用 408 告诉重试策略不要换渠道重发。
		statusCode := http.StatusInternalServerError
		if service.IsSubmitOutcomeUnknown(err, err.Error()) {
			statusCode = service.SubmitOutcomeUnknownStatusCode
		}
		return nil, service.TaskErrorWrapper(err, "do_request_failed", statusCode)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		message := string(responseBody)
		statusCode := resp.StatusCode
		if service.IsSubmitOutcomeUnknown(nil, message) {
			statusCode = service.SubmitOutcomeUnknownStatusCode
		}
		return nil, service.TaskErrorWrapper(fmt.Errorf("%s", message), "fail_to_fetch_task", statusCode)
	}

	// 10. 返回 OtherRatios 给下游（header 必须在 DoResponse 写 body 之前设置）
	otherRatios := info.PriceData.OtherRatios
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 11. 解析响应
	upstreamTaskID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		return nil, taskErr
	}

	// 11. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, taskData); len(adjustedRatios) > 0 {
		// 基于调整后的 ratios 重新计算 quota
		finalQuota = recalcQuotaFromRatios(info, adjustedRatios)
		info.PriceData.OtherRatios = adjustedRatios
		info.PriceData.Quota = finalQuota
	}

	return &TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
	}, nil
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) int {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	originalQuota := info.PriceData.Quota
	// 先除掉原有的 OtherRatios 恢复基础额度。
	//
	// 不变量：**结果与 map 遍历顺序无关**。倍率先累乘、最后只做一次截断，而不是
	// 边遍历边 int(float64(q)/ra) —— Go map 的迭代顺序随机，三个及以上倍率时同一
	// 请求会重算出两个不同额度（实测 {seconds:3, size:1.1, extra:1.3} 在 2000 次
	// 采样里落到 47894 与 47896），而 tasks 表只记最后一次，差额无人认领也无处对账。
	// 与 applyTaskOtherRatios 是同一个坑，见 commit 860b5402。
	divisor := 1.0
	for _, ra := range info.PriceData.OtherRatios {
		if ra != 1.0 && ra > 0 {
			divisor *= ra
		}
	}
	baseQuota := originalQuota
	if divisor != 1.0 {
		baseQuota = int(float64(originalQuota) / divisor)
	}
	// 应用新的 ratios
	return applyTaskOtherRatios(baseQuota, ratios)
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			// 别名模型：converter 返回的是上游原始回执（上游模型名 + 上游成片直链 +
			// 上游成本），必须换成对外口径，别名才不会被反推出真实上游。
			respBody = redactAliasOpenAIVideo(originTask, openAIVideoData)
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	// 通用 TaskDto 格式
	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，还会构建自定义格式的响应体。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := channelModel.GetBaseURL()
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	resp, err := adaptor.FetchTask(baseURL, channelModel.Key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, err := adaptor.ParseTaskResult(body)
	if err != nil || ti == nil {
		return nil
	}

	snap := task.Snapshot()

	// 将上游最新状态更新到 task
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	// 非 OpenAI Video API: 构建自定义格式响应
	format := detectVideoFormat(body)
	out := map[string]any{
		"error":    nil,
		"format":   format,
		"metadata": nil,
		"status":   mapTaskStatusToSimple(task.Status),
		"task_id":  task.TaskID,
		"url":      task.GetResultURL(),
	}
	respBody, _ := common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: out,
	})
	return respBody
}

// detectVideoFormat 从 Gemini/Vertex 原始响应中探测视频格式
func detectVideoFormat(rawBody []byte) string {
	var raw map[string]any
	if err := common.Unmarshal(rawBody, &raw); err != nil {
		return "mp4"
	}
	respObj, ok := raw["response"].(map[string]any)
	if !ok {
		return "mp4"
	}
	vids, ok := respObj["videos"].([]any)
	if !ok || len(vids) == 0 {
		return "mp4"
	}
	v0, ok := vids[0].(map[string]any)
	if !ok {
		return "mp4"
	}
	mt, ok := v0["mimeType"].(string)
	if !ok || mt == "" || strings.Contains(mt, "mp4") {
		return "mp4"
	}
	return mt
}

// mapTaskStatusToSimple 将内部 TaskStatus 映射为简化状态字符串
func mapTaskStatusToSimple(status model.TaskStatus) string {
	switch status {
	case model.TaskStatusSuccess:
		return "succeeded"
	case model.TaskStatusFailure:
		return "failed"
	case model.TaskStatusQueued, model.TaskStatusSubmitted:
		return "queued"
	default:
		return "processing"
	}
}

// TaskModel2Dto 面向下游客户端：别名模型必须遮住真实上游。
func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	out := buildTaskDto(task)
	// 失败文案里的上游地址对所有模型一律摘掉：地址是最硬的渠道证据，
	// 客户拿到就能直接访问上游。身份字样（aliyun/wan3）仍只对别名模型替换。
	out.FailReason = model.RedactUpstreamAddress(out.FailReason)
	redactAliasTaskUpstream(out, task)
	// 模型名单独兜一层，不受别名白名单约束：条件路由改派过就必须换回源模型名，
	// 哪怕目标模型不是别名（规则一 → seedance-2-5 就不在白名单里）。
	redactRouteOriginModel(out, task)
	// 渠道痕迹最后清：channel_id / platform / upstream_model_name 都是货源线索，
	// 客户不需要知道任务跑在哪条渠道上。
	redactChannelInfo(out, task)
	// 内部占位符 → 本站 /content 代理地址。
	//
	// 为什么必须换：占位符（cos-proxy://…）是**内部协议**，客户端拿到没法下载。
	// 它出现在「下载地址」字段里等于交付失败 —— 而用户已经为这条成片付过钱了。
	//
	// 为什么放在这里而不是 buildTaskDto：管理员界面（TaskModel2DtoForAdmin）
	// 要看到库里的原文（排查产物落在哪个桶/路径），不在这儿换。
	//
	// 别名模型的 ResultURL 已在上一步重设成本站代理地址，不是占位符，这里不动。
	if taskcommon.IsResultURLProxyMarker(out.ResultURL) {
		out.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}
	return out
}

// TaskModel2DtoForAdmin 面向管理员界面：保留上游原文，方便排查真实货源。
// 管理员手里的原文来自数据库与 GetAllLogs，这里不用遮。
func TaskModel2DtoForAdmin(task *model.Task) *dto.TaskDto {
	return buildTaskDto(task)
}

func buildTaskDto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		ResultText: task.PrivateData.ResultText,
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}

// redactAliasTaskUpstream 把「上游别名模型」的真实上游从下游可见字段里抹掉。
//
// 为什么要抹：别名模型对外是 seedance2.5-特惠，但下游能从这些字段反推真实货源：
//   - properties.upstream_model_name = 上游模型名（wan-3.0）
//   - data = 上游原始回执（上游任务号、上游成本 billing_amount、上游成片地址）
//   - fail_reason = 上游失败文案
//   - result_url = 成片直链（上游存储域名 + hailuo-video 货源目录）
//
// 只在出口层（DTO 构造）做：数据库里保留原文，管理员排障仍能看到真实上游；
// 换渠道重试与 AutoBan 判定读的是上游原文，不受影响。
func redactAliasTaskUpstream(out *dto.TaskDto, task *model.Task) {
	// 口径按**用户请求的源模型**判断，不是改派后的目标模型。
	//
	// 为什么：条件路由把 seedance2.5-特惠 改派到 seedance-2-5（规则一）时，
	// 目标模型不在 upstreamAliasModels 里 —— 用 OriginModelName 判断会直接 return，
	// 于是 data（含上游 OSS 签名直链）、fail_reason、result_url 全部原样漏给下游。
	// 用户请求的既然是别名，对外就该按别名口径遮，改派一次不能把白名单绕过去。
	origin := task.PublicModelName()
	if !model.IsUpstreamAliasModel(origin) {
		return
	}
	// properties：上游模型名直接换成对外名，字段结构保持不变
	props := task.Properties
	props.UpstreamModelName = origin
	out.Properties = props
	// fail_reason：上游失败文案脱敏 —— 字样换成中性词，上游地址整个摘掉。
	// 交付地址已改走本站代理，文案里再留一个上游地址没有任何用途。
	// 基于 out.FailReason（已在入口摘过一遍地址），不是 task.FailReason。
	out.FailReason = model.RedactUpstreamText(out.FailReason, origin)
	// data：上游原始回执整体不给下游 —— 里面有上游任务号、上游成本和上游地址，
	// 都不是下游该知道的；前端只用 data 渲染 Suno 音频片段，视频任务不依赖它。
	out.Data = nil
	// result_url：这个字段只放「下载地址」。
	//   - 成功任务：成片直链改指向本站 /content 代理。直链里带着上游存储域名与
	//     货源目录（R2 域名 + hailuo-video 路径），原样给下游等于直接报出真实上游
	//     是谁；代理回源的是库里的原始地址，交付行为不变（管理员仍看得到原始直链），
	//     只是多一跳本站带宽。
	//   - 未完成 / 失败任务：置空。GetResultURL() 在没有产物时会回退到 FailReason，
	//     而失败文案里带的是上游原文（"model aliyun:wan-3.0 ..."）—— 留在
	//     「下载地址」字段里等于换个字段继续报出真实上游。失败原因由 fail_reason 承载。
	if task.Status == model.TaskStatusSuccess && out.ResultURL != "" && task.TaskID != "" {
		out.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	} else if task.Status != model.TaskStatusSuccess {
		out.ResultURL = ""
	}
}

// redactRouteOriginModel 把对外可见的模型名换回用户真正请求的源模型。
//
// 为什么需要独立一层（不并进 redactAliasTaskUpstream）：
// 那个函数以「源模型是不是别名」为开关，而模型名这一项**无论是不是别名**都必须换 ——
// 将来若把路由规则挂到别的模型上，也不会因为不在白名单里就把改派泄露出去。
//
// 覆盖 properties.origin_model_name：客户在任务列表（/api/task/self）里直接看得到它，
// 那是他最容易发现「我调 A 却被记成 B」的地方。
func redactRouteOriginModel(out *dto.TaskDto, task *model.Task) {
	if task == nil || task.PrivateData.RouteOriginModel == "" {
		return
	}
	props := task.Properties
	props.OriginModelName = task.PrivateData.RouteOriginModel
	out.Properties = props
}

// redactChannelInfo 清掉任务 DTO 里的渠道痕迹。
//
// 口径（2026-10-30 老板定）：**有渠道的，对外一律说「上游」或者干脆不给**。
// 客户不需要知道任务跑在哪条渠道上 —— 知道了他就能反推货源、比价、
// 甚至绕过我们直接找上游。
//
//   - channel_id：一看编号就能对上渠道，置 0。
//   - platform：其实是**渠道类型编号**（"63" = 有赞 Wan3），直接换成「上游」。
//     为什么不置空：前端任务日志列要拿它做 i18n 显示（t(log.platform)），
//     清空会显示成空白；suno 保留原值 —— 那是公开产品线，且前端按
//     log.platform === 'suno' 判断要不要渲染音频播放器。
//   - properties.upstream_model_name：上游真实模型名（"wan-3.0"），
//     换回对外模型名，字段结构保持不变。
func redactChannelInfo(out *dto.TaskDto, task *model.Task) {
	// Suno / Midjourney 的 data 是**交付物**（音频片段 / 图片），必须留；
	// 其余（视频任务）的 data 是上游原始回执，前端不读，清掉。
	keepsData := out.Platform == string(constant.TaskPlatformSuno) ||
		out.Platform == string(constant.TaskPlatformMidjourney)
	out.ChannelId = 0
	if out.Platform != string(constant.TaskPlatformSuno) {
		out.Platform = "上游"
	}
	if !keepsData {
		// data 存的是**上游完整响应体**（task_polling.go: task.Data = responseBody），
		// 里面有上游任务号、上游成本 billing_amount、上游成片直链、480P 原片 sourceUrl
		// —— 全是渠道痕迹。前端只在 Suno 音频场景读它（task-logs-columns.tsx 按
		// platform === 'suno' 判断），视频任务不依赖，整体清掉。
		out.Data = nil
	}
	if task == nil {
		return
	}
	// 从 out 上取（不是 task.Properties）：前面 redactRouteOriginModel 已经把
	// OriginModelName 换回源模型了，从 task 重新取会把那次改写覆盖掉。
	props, ok := out.Properties.(model.Properties)
	if !ok {
		return
	}
	props.UpstreamModelName = task.PublicModelName()
	out.Properties = props
}

// redactAliasOpenAIVideo 把 OpenAI Video 响应体换成对外口径。
//
// 分两层：
//
//	① 模型名换回源模型 —— **所有模型**都做。条件路由改派过就必须换，
//	   否则 GET /v1/videos/{id} 直接把改派结果告诉客户端。
//	② 上游回执重建（url / task_id / 成本 / error）—— 只对**别名模型**做。
//	   别名模型的 ConvertToOpenAIVideo 返回的是上游原始回执，里面有
//	   "model": "wan-3.0"、"url"（上游成片直链）、"billing_amount"（上游成本）。
//	   任何一项漏出去，别名就白起了。
//
// 非别名模型除模型名外逐字返回原响应体：它们的回执本来就是对外的，
// 遮了只会让客户端拿不到信息。
func redactAliasOpenAIVideo(task *model.Task, raw []byte) []byte {
	// 同 redactAliasTaskUpstream：按**用户请求的源模型**判断是否别名。
	// 否则规则一改派到 seedance-2-5 后白名单失效，上游回执（model / url /
	// billing_amount）会原样返回给客户端 —— 客户用 OpenAI SDK 一眼就看到。
	origin := task.PublicModelName()
	if !model.IsUpstreamAliasModel(origin) {
		// 非别名模型：只换模型名，别的不动。
		//
		// 为什么要单独处理：客户调 seedance2.5-特惠 被改派到 seedance-2-5 时，
		// 源模型是别名（走重建分支）；但反过来 —— 客户直接调一个**非别名**模型、
		// 而它恰好是改派目标时，Properties.OriginModelName 就是目标模型名，
		// 直接返回会把改派说出去。
		return replaceOpenAIVideoModel(raw, origin)
	}
	video := task.ToOpenAIVideo()
	// model：ToOpenAIVideo 取的是 Properties.OriginModelName（= 改派后的目标模型），
	// 必须换回源模型名，否则 GET /v1/videos/{id} 直接把改派结果告诉客户端。
	video.Model = origin
	// 兼容字段 task_id：上游回执里放的是**上游任务号**，必须换成对外任务号。
	video.TaskID = task.TaskID
	// ToOpenAIVideo() 预置的 url 取的是原始直链（非成功任务还会回退到 FailReason），
	// 先删掉再按对外口径重设。
	delete(video.Metadata, "url")
	if task.Status == model.TaskStatusSuccess && strings.TrimSpace(task.GetResultURL()) != "" && task.TaskID != "" {
		video.SetMetadata("url", taskcommon.BuildProxyURL(task.TaskID))
	}
	if task.Status == model.TaskStatusFailure {
		// 上游回执里只有提交时的快照（真正的原因在 fail_reason），失败原因在这里补上，
		// 否则客户端只会看到一个永远停在 queued 的任务。
		video.Error = &dto.OpenAIVideoError{
			Message: model.RedactUpstreamText(task.FailReason, origin),
		}
	}
	encoded, err := common.Marshal(video)
	if err != nil {
		// 兜底：编不出来就退回原文，绝不把下游的响应体搞坏
		return raw
	}
	return encoded
}

// replaceOpenAIVideoModel 只把响应体里的 model 字段换成对外模型名，其余逐字保留。
//
// 为什么需要它：非别名模型的 ConvertToOpenAIVideo 返回的是上游原始回执，
// 不能整体重建（会丢掉客户端需要的字段），但其中的 "model" 字段在条件路由
// 改派过时是**目标模型名** —— 直接返回等于告诉客户「你调 A、实际跑 B」。
//
// 解析失败就原样返回：绝不能因为脱敏把下游的响应体搞坏。
func replaceOpenAIVideoModel(raw []byte, publicModel string) []byte {
	if publicModel == "" {
		return raw
	}
	var video dto.OpenAIVideo
	if err := common.Unmarshal(raw, &video); err != nil {
		return raw
	}
	if video.Model == publicModel {
		return raw
	}
	video.Model = publicModel
	encoded, err := common.Marshal(video)
	if err != nil {
		return raw
	}
	return encoded
}

// applyTaskOtherRatios 把时长/分辨率等附加倍率乘到任务预扣额度上。
//
// 不变量：**结果与 map 遍历顺序无关**。倍率全部乘完再取整，而不是边遍历边 int()，
// 否则 Go map 的随机迭代序会让同一个请求两次扣费不同（详见调用处注释）。
func applyTaskOtherRatios(quota int, ratios map[string]float64) int {
	multiplier := 1.0
	for _, ratio := range ratios {
		multiplier *= ratio
	}
	if multiplier == 1.0 {
		return quota
	}
	return int(float64(quota) * multiplier)
}
