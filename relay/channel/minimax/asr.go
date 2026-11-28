package minimax

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

// ASR（asr-1.0）只认这几个表单字段，多余的 OpenAI 参数（temperature / prompt /
// timestamp_granularities）会被上游当成非法参数拒绝，所以这里按白名单重建表单。
// ⚠️ language 不在表单里 —— 官方文档把它定义成**请求头**（BCP-47 标签）。
var minimaxAsrFormFields = []string{"response_format", "timestamp_level"}

// OpenAI 的 response_format 允许 plain text，MiniMax 没有这个取值，统一归到 json。
func normalizeAsrResponseFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "verbose_json", "srt", "vtt":
		return strings.ToLower(strings.TrimSpace(format))
	default:
		return "json"
	}
}

// buildAsrRequestBody 把客户提交的 multipart 转成 MiniMax /v1/speech_to_text 的表单。
//
// 与 OpenAI 的 /v1/audio/transcriptions 相比只有两点差异：白名单字段、response_format 取值，
// 文件本身原样转发（上游限制 500 秒 / 50MB）。
func buildAsrRequestBody(c *gin.Context, request dto.AudioRequest) (io.Reader, string, error) {
	formData, err := common.ParseMultipartFormReusable(c)
	if err != nil {
		return nil, "", fmt.Errorf("error parsing multipart form: %w", err)
	}

	fileHeaders := formData.File["file"]
	if len(fileHeaders) == 0 {
		return nil, "", fmt.Errorf("file is required")
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if err := writer.WriteField("model", request.Model); err != nil {
		return nil, "", err
	}
	// 表单里没带 response_format 时按 json 兜底，避免上游按自己的默认值返回。
	format := normalizeAsrResponseFormat(formData.Value["response_format"][0])
	if len(formData.Value["response_format"]) == 0 {
		format = normalizeAsrResponseFormat(request.ResponseFormat)
	}
	if err := writer.WriteField("response_format", format); err != nil {
		return nil, "", err
	}
	for _, key := range minimaxAsrFormFields {
		if key == "response_format" {
			continue
		}
		for _, value := range formData.Value[key] {
			if err := writer.WriteField(key, value); err != nil {
				return nil, "", err
			}
		}
	}

	// language 是请求头而不是表单字段，带了就必须搬到 header 上，否则上游按混合语言识别。
	if language := firstNonEmpty(formData.Value["language"]); language != "" {
		c.Request.Header.Set("language", language)
	}

	fileHeader := fileHeaders[0]
	file, err := fileHeader.Open()
	if err != nil {
		return nil, "", fmt.Errorf("error opening audio file: %w", err)
	}
	defer file.Close()

	part, err := writer.CreateFormFile("file", fileHeader.Filename)
	if err != nil {
		return nil, "", fmt.Errorf("create form file failed: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, "", fmt.Errorf("copy file failed: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return &body, writer.FormDataContentType(), nil
}

// asrResponse 是 MiniMax 语音识别的回执。duration 为输入音频秒数，上游按它计费。
type asrResponse struct {
	Text      string          `json:"text"`
	Duration  float64         `json:"duration"`
	NSpeakers int             `json:"n_speakers,omitempty"`
	Segments  []asrSegment    `json:"segments,omitempty"`
	TraceID   string          `json:"trace_id,omitempty"`
	BaseResp  *baseRespInline `json:"base_resp,omitempty"`
}

type baseRespInline struct {
	StatusCode int    `json:"status_code"`
	StatusMsg  string `json:"status_msg"`
}

type asrSegment struct {
	ID      int     `json:"id"`
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker string  `json:"speaker,omitempty"`
	Text    string  `json:"text"`
}

// handleAsrResponse 把 MiniMax 的回执转成客户用 response_format 指定的形态。
//
// OpenAI 的字段是 text / duration / segments，MiniMax 多一个 n_speakers 与 trace_id；
// srt 与 vtt 由上游直接返回纯文本，原样透传即可。
func handleAsrResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo, responseFormat string) (*dto.Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read minimax response: %w", err)
	}
	defer resp.Body.Close()

	format := normalizeAsrResponseFormat(responseFormat)
	if format == "srt" || format == "vtt" {
		c.Data(resp.StatusCode, resp.Header.Get("Content-Type"), body)
		return asrUsageFromDuration(info), nil
	}

	var parsed asrResponse
	if err := common.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("failed to unmarshal minimax asr response: %w", err)
	}
	if parsed.BaseResp != nil && parsed.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax asr error: %d - %s", parsed.BaseResp.StatusCode, parsed.BaseResp.StatusMsg)
	}

	out := map[string]any{
		"text":     parsed.Text,
		"duration": parsed.Duration,
	}
	if format == "verbose_json" {
		segments := make([]map[string]any, 0, len(parsed.Segments))
		for _, segment := range parsed.Segments {
			item := map[string]any{
				"id":    segment.ID,
				"start": segment.Start,
				"end":   segment.End,
				"text":  segment.Text,
			}
			if segment.Speaker != "" {
				item["speaker"] = segment.Speaker
			}
			segments = append(segments, item)
		}
		out["segments"] = segments
		if parsed.NSpeakers > 0 {
			out["n_speakers"] = parsed.NSpeakers
		}
	}
	c.JSON(resp.StatusCode, out)
	return asrUsageFromDuration(info), nil
}

// asrUsageFromDuration 复用转录模式的时长口径：预扣费阶段已按音频时长折算成 token
// （1 分钟 1000 token），结算沿用同一个估算值，避免上下游两套单位打架。
func asrUsageFromDuration(info *relaycommon.RelayInfo) *dto.Usage {
	tokens := info.GetEstimatePromptTokens()
	return &dto.Usage{
		PromptTokens:     tokens,
		CompletionTokens: 0,
		TotalTokens:      tokens,
	}
}

// firstNonEmpty 取第一个非空值；表单字段优先于 DTO 字段（客户显式传的更可信）。
func firstNonEmpty(groups ...[]string) string {
	for _, group := range groups {
		for _, value := range group {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				return trimmed
			}
		}
	}
	return ""
}
