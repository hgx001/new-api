/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
package sanyuan

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// TaskAdaptor 对接三元巡狩视频生成平台（Flask 自建，非 OpenAI 兼容）。
//
// 协议三步：POST /v1/video/generate 提交（body 带 client_request_id 幂等键，
// 返回 event_id）→ POST /v1/video/check 轮询 → GET /v1/video/file/{event_id}
// 下载产物（需渠道 Bearer，走站内代理）。素材只收 data URL：http(s) 源在本地
// 下载后转成 data URL 塞进 image_data_urls / audio_data_urls。
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
	proxy       string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info == nil {
		return
	}
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
	a.proxy = info.ChannelSetting.Proxy
}

func (a *TaskAdaptor) GetChannelName() string { return ChannelName }

func (a *TaskAdaptor) GetModelList() []string {
	return []string{SlowPublicModel, PrimePublicModel}
}

// ---------------------------------------------------------------- 模型档位

// requestProfile 按请求的模型名取档位（工作流 + 各自的时长/分辨率/素材约束）。
//
// 模型名取自请求体（req.Model）或路由时的 info.OriginModelName —— 两者都是**用户
// 请求的原始模型名**：relay_task.go 在 ValidateRequestAndSetAction 之后才应用渠道的
// model_mapping，所以这里拿到的始终是原始名，不会被映射结果污染。
func requestProfile(req relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*modelProfile, error) {
	name := strings.TrimSpace(req.Model)
	if name == "" && info != nil {
		name = strings.TrimSpace(info.OriginModelName)
	}
	if name == "" {
		return nil, errors.New("sanyuan requires a model name")
	}
	profile, ok := profilesByModel[name]
	if !ok {
		return nil, errors.Errorf("sanyuan does not serve model %q", name)
	}
	return profile, nil
}

// ---------------------------------------------------------------- 请求校验

// ValidateRequestAndSetAction 强制本渠道的档位与素材约束。
// 校验放在计费预扣之前：400 不触发渠道重试，被拒请求不消耗上游额度。
func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	profile, profileErr := requestProfile(req, info)
	if profileErr != nil {
		return service.TaskErrorWrapperLocal(profileErr, "invalid_model", http.StatusBadRequest)
	}
	if _, mediaErr := buildMedia(req, profile); mediaErr != nil {
		return service.TaskErrorWrapperLocal(mediaErr, "invalid_request", http.StatusBadRequest)
	}
	if _, resErr := resolveResolution(req, profile); resErr != nil {
		return service.TaskErrorWrapperLocal(resErr, "invalid_resolution", http.StatusBadRequest)
	}
	if _, durErr := resolveDuration(req, profile); durErr != nil {
		return service.TaskErrorWrapperLocal(durErr, "invalid_duration", http.StatusBadRequest)
	}
	return nil
}

// resolveDuration 时长按档位解析：慢速档 1..30 自由整数，区间外报错不吸附；
// 满血档固定 30 秒（与原 youzan prime 的对外语义一致：传其它值被忽略）。
//
// **req.Seconds 是必须的回退路径**：下游按 OpenAI video 形状传的是 {"seconds":"7"}
//（字符串），落到 TaskSubmitReq.Seconds 而不是 Duration。漏读 Seconds 会静默按
// 默认 5 秒建单 —— 二春渠道 2026-10-06 首次真机验证就是这么多收了 2.5 倍。
func resolveDuration(req relaycommon.TaskSubmitReq, profile *modelProfile) (int, error) {
	if profile.FixedDuration > 0 {
		return profile.FixedDuration, nil
	}
	if req.Duration > 0 {
		if req.Duration < profile.MinDuration || req.Duration > profile.MaxDuration {
			return 0, durationError(req.Duration, profile)
		}
		return req.Duration, nil
	}
	if seconds := strings.TrimSpace(req.Seconds); seconds != "" {
		if parsed, err := strconv.Atoi(seconds); err == nil {
			if parsed < profile.MinDuration || parsed > profile.MaxDuration {
				return 0, durationError(parsed, profile)
			}
			return parsed, nil
		}
	}
	return profile.DefaultDuration, nil
}

func durationError(got int, profile *modelProfile) error {
	return errors.Errorf("%s accepts %d-%d seconds, got %d",
		profile.PublicModel, profile.MinDuration, profile.MaxDuration, got)
}

// resolveResolution 解析分辨率：
//   - 自由档位（慢速）：完整档位（如 "1080p横"）大小写归一后精确匹配；tier-only
//     （如 "1080p"）从 metadata.orientation / ratio / size 推方向；无提示默认竖；
//     越界 400 不吸附。
//   - 固定档位（满血 1080p）：忽略请求里的 tier（对外承诺「固定 1080P，传其它值会被
//     忽略」），但**保留方向** —— 完整档位 "1080p横" 要落到 1080p横，不能被拍成竖屏。
func resolveResolution(req relaycommon.TaskSubmitReq, profile *modelProfile) (string, error) {
	raw := strings.ToLower(strings.TrimSpace(req.Resolution))
	tier := profile.FixedTier
	if tier == "" {
		switch {
		case raw == "":
			tier = profile.DefaultTier
		case profile.Resolutions[raw]:
			return raw, nil
		default:
			parsed, ok := parseTier(raw)
			if !ok {
				return "", errors.Errorf("%s only supports resolutions %s, got %q",
					profile.PublicModel, strings.Join(sortedResolutionKeys(profile), "/"), req.Resolution)
			}
			tier = parsed
		}
	}
	direction := directionOfResolution(raw)
	if direction == "" {
		direction = resolveDirection(req)
	}
	return tier + direction, nil
}

// directionOfResolution 从完整档位里取方向后缀（"1080p横" → 横）；tier-only 档位
// 没有方向，返回空串交给 metadata/size 推断。
func directionOfResolution(raw string) string {
	switch {
	case strings.HasSuffix(raw, directionPortrait):
		return directionPortrait
	case strings.HasSuffix(raw, directionLandscape):
		return directionLandscape
	}
	return ""
}

func parseTier(raw string) (string, bool) {
	switch raw {
	case "480", "480p":
		return "480p", true
	case "720", "720p":
		return "720p", true
	case "1080", "1080p":
		return "1080p", true
	}
	return "", false
}

// resolveDirection 方向优先级：metadata.orientation → metadata.ratio / aspect_ratio
// → size（WxH）→ 默认竖。上游只有竖/横两种画幅，1:1 或解析失败的值当无提示处理。
func resolveDirection(req relaycommon.TaskSubmitReq) string {
	if req.Metadata != nil {
		if v, ok := req.Metadata["orientation"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "landscape", "horizontal", "横":
				return directionLandscape
			case "portrait", "vertical", "竖":
				return directionPortrait
			}
		}
		for _, key := range []string{"ratio", "aspect_ratio"} {
			if v, ok := req.Metadata[key].(string); ok {
				if dir, ok := directionFromDimensions(v); ok {
					return dir
				}
			}
		}
	}
	if dir, ok := directionFromDimensions(req.Size); ok {
		return dir
	}
	return directionPortrait
}

// directionFromDimensions 把 "9:16" / "1280x720" 解析成竖/横；w==h 或解析失败回
// false（当无提示，走默认竖）。
func directionFromDimensions(value string) (string, bool) {
	value = strings.ToLower(strings.TrimSpace(value))
	var parts []string
	switch {
	case strings.Contains(value, ":"):
		parts = strings.SplitN(value, ":", 2)
	case strings.Contains(value, "x"):
		parts = strings.SplitN(value, "x", 2)
	default:
		return "", false
	}
	width, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	height, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || width <= 0 || height <= 0 || width == height {
		return "", false
	}
	if height > width {
		return directionPortrait, true
	}
	return directionLandscape, true
}

// ---------------------------------------------------------------- 素材

// buildMedia 归一化素材，取值顺序沿用 youzanwan3：显式 media → metadata.media →
// 旧版扁平字段。first_frame 接受（上游把第一张图当首帧）并前插占额；
// reference_video / last_frame / 未知类型一律拒绝（开放 API 没有这两个入参）。
func buildMedia(req relaycommon.TaskSubmitReq, profile *modelProfile) ([]relaycommon.TaskMedia, error) {
	media := append([]relaycommon.TaskMedia(nil), req.Media...)
	if len(media) == 0 {
		var err error
		if media, err = metadataMedia(req.Metadata); err != nil {
			return nil, err
		}
	}
	if len(media) == 0 {
		images := req.Images
		if len(images) == 0 && strings.TrimSpace(req.Image) != "" {
			images = []string{req.Image}
		}
		if len(images) == 0 && strings.TrimSpace(req.InputReference) != "" {
			images = []string{req.InputReference}
		}
		for _, item := range images {
			if strings.TrimSpace(item) != "" {
				media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: strings.TrimSpace(item)})
			}
		}
		audios := req.Audios
		if len(audios) == 0 && strings.TrimSpace(req.Audio) != "" {
			audios = []string{req.Audio}
		}
		for _, item := range audios {
			if strings.TrimSpace(item) != "" {
				media = append(media, relaycommon.TaskMedia{Type: "reference_audio", URL: strings.TrimSpace(item)})
			}
		}
	}
	ordered := make([]relaycommon.TaskMedia, 0, len(media))
	for _, item := range media {
		if strings.TrimSpace(item.Type) == "first_frame" {
			ordered = append(ordered, item)
		}
	}
	for _, item := range media {
		if strings.TrimSpace(item.Type) != "first_frame" {
			ordered = append(ordered, item)
		}
	}
	return ordered, validateMedia(ordered, profile)
}

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

func validateMedia(media []relaycommon.TaskMedia, profile *modelProfile) error {
	imageCount, audioCount := 0, 0
	for _, item := range media {
		item.Type = strings.TrimSpace(item.Type)
		item.URL = strings.TrimSpace(item.URL)
		if item.URL == "" {
			return errors.New("sanyuan media url is required")
		}
		switch item.Type {
		case "reference_image", "first_frame":
			imageCount++
		case "reference_audio":
			audioCount++
		default:
			return errors.Errorf("sanyuan does not accept media type %q (upstream workflow supports reference images and audio only)", item.Type)
		}
	}
	if imageCount > profile.MaxReferenceImages {
		return errors.Errorf("sanyuan accepts at most %d reference images", profile.MaxReferenceImages)
	}
	if audioCount > profile.MaxReferenceAudios {
		return errors.Errorf("sanyuan accepts at most %d reference audios", profile.MaxReferenceAudios)
	}
	return nil
}

// toDataURL data: URL 原样透传（不解码再编码）；http(s) 下载后转成 base64 data URL
// —— 上游 image_data_urls / audio_data_urls 只收 data URL。
func (a *TaskAdaptor) toDataURL(source string) (string, error) {
	if strings.HasPrefix(strings.ToLower(source), "data:") {
		return source, nil
	}
	parsed, err := url.Parse(source)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("sanyuan requires http(s) URL or data URL media")
	}
	fetchSetting := system_setting.GetFetchSetting()
	if err := common.ValidateURLWithFetchSetting(source, fetchSetting.EnableSSRFProtection,
		fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode,
		fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain); err != nil {
		return "", errors.Wrap(err, "sanyuan media URL blocked")
	}
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return "", err
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", errors.Errorf("download media failed: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxMediaBytes+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxMediaBytes {
		return "", errors.Errorf("media file exceeds the %d byte limit", maxMediaBytes)
	}
	if len(data) == 0 {
		return "", errors.New("media file is empty")
	}
	return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// ---------------------------------------------------------------- 计费

// EstimateBilling 继承 BaseBilling 返回 nil：拼车批次按次计费（¥2/次，与时长/
// 分辨率无关）。**不能把时长当倍率返回** —— 按次模型叠加 seconds 倍率会重蹈
// TASK_PRICE_PATCH 跳过相乘的事故形态（少收或错收）。

// ---------------------------------------------------------------- 建单

type createRequest struct {
	Prompt           string   `json:"prompt"`
	Workflow         string   `json:"workflow"`
	Duration         int      `json:"duration"`
	ImageDataURLs    []string `json:"image_data_urls,omitempty"`
	AudioDataURLs    []string `json:"audio_data_urls,omitempty"`
	Resolution       string   `json:"resolution"`
	Seed             *int64   `json:"seed,omitempty"`
	ContextIREnabled *bool    `json:"context_ir_enabled,omitempty"`
	ClientRequestID  string   `json:"client_request_id"`
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return a.baseURL + createPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	profile, profileErr := requestProfile(req, info)
	if profileErr != nil {
		return nil, taskcommon.NewUserError(profileErr, "invalid_model")
	}
	media, err := buildMedia(req, profile)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_request")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" && len(media) == 0 {
		return nil, taskcommon.NewUserError(errors.New("sanyuan requires prompt or reference media"), "invalid_request")
	}
	resolution, err := resolveResolution(req, profile)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_resolution")
	}
	duration, err := resolveDuration(req, profile)
	if err != nil {
		return nil, taskcommon.NewUserError(err, "invalid_duration")
	}

	body := createRequest{
		Prompt:           prompt,
		Workflow:         profile.Workflow,
		Duration:         duration,
		Resolution:       resolution,
		ContextIREnabled: resolveContextIREnabled(req),
		ClientRequestID:  clientRequestID(info),
	}
	if req.Seed != nil {
		seed := *req.Seed
		body.Seed = &seed
	}
	for _, item := range media {
		dataURL, err := a.toDataURL(item.URL)
		if err != nil {
			return nil, taskcommon.NewUserError(err, "invalid_request")
		}
		switch item.Type {
		case "reference_audio":
			body.AudioDataURLs = append(body.AudioDataURLs, dataURL)
		default:
			body.ImageDataURLs = append(body.ImageDataURLs, dataURL)
		}
	}
	encoded, err := common.Marshal(body)
	if err != nil {
		return nil, errors.Wrap(err, "marshal sanyuan request body failed")
	}
	return bytes.NewReader(encoded), nil
}

// resolveContextIREnabled 默认 true（上游语境 IR 增强），metadata 可显式覆盖。
func resolveContextIREnabled(req relaycommon.TaskSubmitReq) *bool {
	value := true
	if req.Metadata != nil {
		if raw, ok := req.Metadata["context_ir_enabled"].(bool); ok {
			value = raw
		}
	}
	return &value
}

// clientRequestID 幂等键：上游对同一 client_request_id 的重提返回确定结果而不是
// 重复建单（2026-10-14 预验证实证：submission_uncertain 后同一 ID 重提可解锁）。
// 固定用 newapi-+公开任务 ID，同一任务内稳定。
func clientRequestID(info *relaycommon.RelayInfo) string {
	if info != nil && info.TaskRelayInfo != nil && info.PublicTaskID != "" {
		return "newapi-" + info.PublicTaskID
	}
	return ""
}

// ---------------------------------------------------------------- 提交响应

type createResponse struct {
	OK                  bool   `json:"ok"`
	EventID             string `json:"event_id"`
	Error               string `json:"error"`
	SubmissionUncertain bool   `json:"submission_uncertain"`
	NotSubmitted        bool   `json:"not_submitted"`
	ClientRequestID     string `json:"client_request_id"`
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	// 上游提交成功即 200，无需 202/201 归一化。
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrap(err, "read_response_body_failed"), "read_response_body_failed", http.StatusInternalServerError)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, upstreamError(resp.StatusCode, responseBody)
	}
	var created createResponse
	if err := common.Unmarshal(responseBody, &created); err != nil {
		return "", nil, service.TaskErrorWrapper(errors.Wrapf(err, "body: %s", responseBody), "unmarshal_response_body_failed", http.StatusInternalServerError)
	}
	if !created.OK {
		// ok:false 含两阶段失败形态：普通拒单与 submission_uncertain / not_submitted
		//（上游确认超时或连接失败、请求 ID 已锁定未扣费）。一律如实透传 fail-loud，
		// 不假装成功 —— 预扣费由框架失败路径退款。
		return "", nil, service.TaskErrorWrapper(
			errors.Errorf("sanyuan submit failed: %s", upstreamSubmitMessage(created)),
			"upstream_error", http.StatusBadRequest)
	}
	taskID := strings.TrimSpace(created.EventID)
	if taskID == "" {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("sanyuan returned empty event_id, body: %s", responseBody),
			"invalid_response", http.StatusInternalServerError)
	}

	modelName := PublicModel
	if info != nil && info.OriginModelName != "" {
		modelName = info.OriginModelName
	}
	publicID := ""
	if info != nil && info.TaskRelayInfo != nil {
		publicID = info.PublicTaskID
	}
	out := gin.H{
		"id":       publicID,
		"object":   "video",
		"model":    modelName,
		"status":   dto.VideoStatusQueued,
		"progress": 0,
	}
	if publicID != "" {
		out["status_url"] = taskcommon.BuildProxyURL(publicID)
	}
	c.JSON(http.StatusOK, out)
	return taskID, responseBody, nil
}

func upstreamSubmitMessage(created createResponse) string {
	if message := strings.TrimSpace(created.Error); message != "" {
		return message
	}
	if created.SubmissionUncertain {
		return fmt.Sprintf("submission_uncertain (client_request_id=%s): 上游确认超时，请稍后重试", created.ClientRequestID)
	}
	if created.NotSubmitted {
		return "not_submitted: 上游连接失败，未扣费"
	}
	return "unknown error"
}

func upstreamError(statusCode int, body []byte) *dto.TaskError {
	var wrapper struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	_ = common.Unmarshal(body, &wrapper)
	message := firstNonEmpty(wrapper.Error, wrapper.Message, string(body))
	return service.TaskErrorWrapper(
		errors.Errorf("sanyuan: HTTP %d: %s", statusCode, message),
		"upstream_error", statusCode)
}

// ---------------------------------------------------------------- 轮询

type checkResponse struct {
	OK          bool   `json:"ok"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url"`
	Error       string `json:"error"`
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, errors.New("invalid task id")
	}
	payload, err := common.Marshal(map[string]string{"event_id": taskID})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseUrl+checkPath, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, errors.Wrap(err, "new sanyuan http client failed")
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var res checkResponse
	if err := common.Unmarshal(respBody, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal sanyuan task result failed")
	}
	result := &relaycommon.TaskInfo{Code: 0}
	if !res.OK {
		result.Status = model.TaskStatusFailure
		result.Progress = taskcommon.ProgressComplete
		result.Reason = firstNonEmpty(res.Error, reasonUpstreamFailed)
		return result, nil
	}
	switch strings.ToLower(strings.TrimSpace(res.Status)) {
	case "done":
		if strings.TrimSpace(res.DownloadURL) == "" {
			// 终态却没给 download_url：不能当成功，否则客户端拿到打不开的地址。
			result.Status = model.TaskStatusInProgress
			result.Progress = taskcommon.ProgressInProgress
			break
		}
		result.Status = model.TaskStatusSuccess
		result.Progress = taskcommon.ProgressComplete
		// download_url 是相对路径且需渠道 Bearer，**故意留空 Url**：
		// service/task_polling.go 在 Url 为空时落成站内 content 代理地址，
		// 由 controller/video_proxy.go 的 ChannelTypeSanyuan 分支带密钥回源。
	default:
		// 上游文档只有 processing / done 两态，未知状态一律当进行中兜底。
		result.Status = model.TaskStatusInProgress
		result.Progress = taskcommon.ProgressInProgress
	}
	return result, nil
}

// ConvertToOpenAIVideo 把三元巡狩任务记录转成 OpenAI Video API 查询响应。
// 产物 URL 契约与 ParseTaskResult 同口径：download_url 需渠道 Bearer，
// **不透传上游相对路径**，metadata.url 统一落成站内 /v1/videos/{task}/content 代理。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var res checkResponse
	if err := common.Unmarshal(originTask.Data, &res); err != nil {
		return nil, errors.Wrap(err, "unmarshal sanyuan task data failed")
	}

	openAIVideo := dto.NewOpenAIVideo()
	openAIVideo.ID = originTask.TaskID
	openAIVideo.Status = originTask.Status.ToVideoStatus()
	openAIVideo.SetProgressStr(originTask.Progress)
	openAIVideo.CreatedAt = originTask.CreatedAt
	openAIVideo.CompletedAt = originTask.UpdatedAt
	openAIVideo.Model = PublicModel
	if originTask.Properties.OriginModelName != "" {
		openAIVideo.Model = originTask.Properties.OriginModelName
	}

	switch originTask.Status {
	case model.TaskStatusSuccess:
		openAIVideo.SetMetadata("url", taskcommon.BuildProxyURL(originTask.TaskID))
	case model.TaskStatusFailure:
		openAIVideo.Error = &dto.OpenAIVideoError{
			Message: firstNonEmpty(originTask.FailReason, res.Error, taskcommon.ReasonContentModeration),
		}
	}
	return common.Marshal(openAIVideo)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func sortedResolutionKeys(profile *modelProfile) []string {
	keys := make([]string, 0, len(profile.Resolutions))
	for key := range profile.Resolutions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
