package openai

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// Youkou async image tasks (TT Image 2 / 2.5 via api.erchun.youkou.cc).
//
// The upstream speaks task-style under the OpenAI images endpoint:
// POST /v1/images/generations {"model":"<mdl_id>","prompt":"..."} (with an
// Idempotency-Key header) returns {"object":"generation.task","status":...,
// "id":"<taskid>","status_url":"/v1/tasks/<id>"}; polling
// GET /v1/tasks/<id> yields {"status":"completed","content_url":"..."} after
// ~60-70s; GET content_url 302-redirects to a media PNG.
//
// This file implements sync-over-async inside the OpenAI adaptor so downstream
// OpenAI clients keep working unchanged: the task response is detected by
// shape (object == "generation.task"), polled to completion with a bounded
// timeout, and converted to a standard OpenAI images response. Detection is
// response-shape based, so existing OpenAI channels are unaffected (real
// OpenAI never returns this object type).
const youkouImageTaskObject = "generation.task"

// youkouImageUpstreamModels maps downstream model names to upstream mdl ids.
// Production channel #19 normally supplies these via ModelMapping; this table
// is only a fallback so the flow also works when the mapping is absent.
// Unknown aliases pass through untouched and surface the upstream
// {"code":"model_not_found"} error.
var youkouImageUpstreamModels = map[string]string{
	"gpt-image-2":   "mdl_bfb3f6112aa85bd4a309e842c33c7448",
	"gpt-image-2.5": "mdl_5230f896b94966ce7463817af855bbdd",
}

// youkouImagePollInterval is the delay between task status polls. It is a var
// so tests can shrink it; production uses 4s.
var youkouImagePollInterval = 4 * time.Second

// youkouImageMaxPollTimeout caps a single image task wait. Production uses
// ~280s, under the default STREAMING_TIMEOUT of 300s.
var youkouImageMaxPollTimeout = 280 * time.Second

const youkouImageMaxContentBytes = 32 << 20

// youkouImageSubmitRequest is the minimal upstream submit payload. Only
// model+prompt are sent: the upstream accepts exactly this shape and may
// reject unrelated OpenAI fields.
type youkouImageSubmitRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// youkouImageTaskResponse is the async submit acknowledgement.
type youkouImageTaskResponse struct {
	Object    string `json:"object"`
	ID        string `json:"id"`
	Status    string `json:"status"`
	StatusURL string `json:"status_url"`
}

// youkouImageTaskStatus is a single poll response.
type youkouImageTaskStatus struct {
	Status     string `json:"status"`
	ContentURL string `json:"content_url"`
	// Defensive aliases: the completed payload is flat today, but accept the
	// camelCase variant and a generic url field if the upstream ever changes.
	ContentURLCamel string `json:"contentUrl"`
	URL             string `json:"url"`
	Message         string `json:"message"`
	Error           string `json:"error"`
}

// youkouImageTerminalFailures are poll statuses that will never become
// completed, so polling stops immediately with an error.
var youkouImageTerminalFailures = map[string]bool{
	"failed":    true,
	"error":     true,
	"cancelled": true,
	"canceled":  true,
	"timeout":   true,
	"expired":   true,
}

// isYoukouImageChannel reports whether the request targets a Youkou image
// upstream. Channel #19 keeps type OpenAI(1) with a youkou base URL, so both
// the channel type and the base URL are accepted.
func isYoukouImageChannel(info *relaycommon.RelayInfo) bool {
	if info == nil || info.ChannelMeta == nil {
		return false
	}
	if info.ChannelType == constant.ChannelTypeYoukou {
		return true
	}
	return strings.Contains(strings.ToLower(info.ChannelBaseUrl), "youkou")
}

// resolveYoukouImageModel maps a downstream image model to its upstream mdl
// id when known, otherwise returns the name untouched (already-mapped mdl ids
// and unknown aliases pass through).
func resolveYoukouImageModel(name string) string {
	if id, ok := youkouImageUpstreamModels[name]; ok {
		return id
	}
	return name
}

// detectYoukouImageTask reports whether an upstream images response body is a
// Youkou async task acknowledgement.
func detectYoukouImageTask(body []byte) (taskID, statusURL string, ok bool) {
	var task youkouImageTaskResponse
	if err := common.Unmarshal(body, &task); err != nil {
		return "", "", false
	}
	if task.Object != youkouImageTaskObject || task.ID == "" {
		return "", "", false
	}
	return task.ID, task.StatusURL, true
}

// peekYoukouImageTask reads the upstream response body to detect a Youkou
// async task, then restores resp.Body so the normal handlers are unaffected
// when this is not a task response.
func peekYoukouImageTask(resp *http.Response) (taskID, statusURL string, ok bool) {
	if resp == nil || resp.Body == nil {
		return "", "", false
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return "", "", false
	}
	return detectYoukouImageTask(body)
}

// joinUpstreamURL resolves a possibly-relative upstream reference against the
// channel base URL.
func joinUpstreamURL(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	return strings.TrimRight(base, "/") + "/" + strings.TrimLeft(ref, "/")
}

// youkouImagePollTimeout bounds the whole poll wait under STREAMING_TIMEOUT
// without changing any global setting.
func youkouImagePollTimeout() time.Duration {
	if constant.StreamingTimeout > 40 {
		if capped := time.Duration(constant.StreamingTimeout-20) * time.Second; capped < youkouImageMaxPollTimeout {
			return capped
		}
	}
	return youkouImageMaxPollTimeout
}

// pollYoukouImageTask polls an absolute task status URL until completion and
// returns the (possibly relative) content URL.
func pollYoukouImageTask(ctx context.Context, client *http.Client, statusURL, apiKey string, timeout, interval time.Duration) (string, *types.NewAPIError) {
	if client == nil {
		client = http.DefaultClient
	}
	fail := func(err error, code types.ErrorCode, status int) (string, *types.NewAPIError) {
		return "", types.NewErrorWithStatusCode(err, code, status)
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return fail(fmt.Errorf("youkou image task wait cancelled: %w", err), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
		}
		if time.Now().After(deadline) {
			return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		if err != nil {
			return fail(fmt.Errorf("youkou image task poll request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			// Transient transport error: keep polling until the deadline.
			if sleepYoukouPoll(ctx, interval) != nil {
				return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
			}
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if err != nil {
			if sleepYoukouPoll(ctx, interval) != nil {
				return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
			if sleepYoukouPoll(ctx, interval) != nil {
				return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fail(fmt.Errorf("youkou image task poll failed with status %d: %s", resp.StatusCode, truncateYoukouBody(body)), types.ErrorCodeDoRequestFailed, resp.StatusCode)
		}
		var status youkouImageTaskStatus
		if err := common.Unmarshal(body, &status); err != nil {
			if sleepYoukouPoll(ctx, interval) != nil {
				return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
			}
			continue
		}
		switch normalized := strings.ToLower(strings.TrimSpace(status.Status)); normalized {
		case "completed", "succeeded", "success":
			contentURL := status.ContentURL
			if contentURL == "" {
				contentURL = status.ContentURLCamel
			}
			if contentURL == "" {
				contentURL = status.URL
			}
			if contentURL == "" {
				return fail(fmt.Errorf("youkou image task completed without content_url"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
			}
			return contentURL, nil
		default:
			if youkouImageTerminalFailures[normalized] {
				detail := status.Message
				if detail == "" {
					detail = status.Error
				}
				if detail == "" {
					detail = string(truncateYoukouBody(body))
				}
				return fail(fmt.Errorf("youkou image task %s: %s", normalized, detail), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
			}
		}
		if sleepYoukouPoll(ctx, interval) != nil {
			return fail(fmt.Errorf("youkou image task timed out after %s: %s", timeout, statusURL), types.ErrorCodeDoRequestFailed, http.StatusGatewayTimeout)
		}
	}
}

// sleepYoukouPoll waits for the poll interval, returning non-nil when the
// caller context is done (or, via time.After-style deadline expiry, when the
// overall timeout elapsed and the caller re-checks).
func sleepYoukouPoll(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// fetchYoukouImageResult resolves the completed content URL to a downstream
// image reference. In url mode the 302 Location (public media URL) is
// returned as-is; when b64_json was requested, or no redirect is offered, the
// bytes are downloaded and returned for base64 encoding.
func fetchYoukouImageResult(ctx context.Context, client, noRedirectClient *http.Client, contentURL, apiKey, responseFormat string) (imageURL, imageB64 string, apiErr *types.NewAPIError) {
	fail := func(err error, code types.ErrorCode, status int) (string, string, *types.NewAPIError) {
		return "", "", types.NewErrorWithStatusCode(err, code, status)
	}
	newGet := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, contentURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		return req, nil
	}
	if !strings.EqualFold(strings.TrimSpace(responseFormat), "b64_json") {
		req, err := newGet()
		if err != nil {
			return fail(fmt.Errorf("youkou image content request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		resp, err := noRedirectClient.Do(req)
		if err != nil {
			return fail(fmt.Errorf("youkou image content request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
		}
		location := resp.Header.Get("Location")
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		_ = resp.Body.Close()
		if resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.TrimSpace(location) != "" {
			return joinUpstreamURL(contentURL, location), "", nil
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 && len(body) > 0 {
			// No redirect offered: fall through to a full download below.
		} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fail(fmt.Errorf("youkou image content failed with status %d: %s", resp.StatusCode, truncateYoukouBody(body)), types.ErrorCodeDoRequestFailed, resp.StatusCode)
		}
	}
	req, err := newGet()
	if err != nil {
		return fail(fmt.Errorf("youkou image content request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fail(fmt.Errorf("youkou image content request failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	defer service.CloseResponseBodyGracefully(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return fail(fmt.Errorf("youkou image content failed with status %d: %s", resp.StatusCode, truncateYoukouBody(body)), types.ErrorCodeDoRequestFailed, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, youkouImageMaxContentBytes+1))
	if err != nil {
		return fail(fmt.Errorf("youkou image content read failed: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(data) > youkouImageMaxContentBytes {
		return fail(fmt.Errorf("youkou image content exceeds %d bytes", youkouImageMaxContentBytes), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if len(data) == 0 {
		return fail(fmt.Errorf("youkou image content is empty"), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	return "", base64.StdEncoding.EncodeToString(data), nil
}

// buildYoukouImageResponse renders the standard OpenAI images response.
func buildYoukouImageResponse(imageURL, imageB64 string) ([]byte, error) {
	resp := dto.ImageResponse{
		Data:    []dto.ImageData{{Url: imageURL, B64Json: imageB64}},
		Created: time.Now().Unix(),
	}
	return common.Marshal(resp)
}

// doYoukouImageTaskFlow polls a Youkou image task to completion and writes a
// standard OpenAI images response downstream, returning per-call billing
// usage. Billing stays on the existing ModelPrice path (quota_type=1 keyed by
// OriginModelName such as gpt-image-2); one completed image counts one call.
func doYoukouImageTaskFlow(c *gin.Context, info *relaycommon.RelayInfo, submitResp *http.Response, taskID, statusURL string) (any, *types.NewAPIError) {
	fail := func(err error, code types.ErrorCode, status int) (any, *types.NewAPIError) {
		return nil, types.NewErrorWithStatusCode(err, code, status)
	}
	if info == nil || info.ChannelMeta == nil {
		return fail(fmt.Errorf("youkou image task missing relay info"), types.ErrorCodeInvalidRequest, http.StatusInternalServerError)
	}
	if strings.TrimSpace(statusURL) == "" {
		statusURL = "/v1/tasks/" + taskID
	}
	statusURL = joinUpstreamURL(info.ChannelBaseUrl, statusURL)
	client, err := service.GetHttpClientWithProxy(info.ChannelSetting.Proxy)
	if err != nil {
		return fail(fmt.Errorf("youkou image task http client failed: %w", err), types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	// Wrap the shared transport with a per-request timeout so one hung poll
	// cannot outlive the overall deadline below. A nil client (e.g. in unit
	// tests where the service client was never initialized) falls back to
	// the default transport.
	pollClient := &http.Client{
		Transport: clientTransport(client),
		Timeout:   60 * time.Second,
	}
	noRedirectClient := &http.Client{
		Transport:     clientTransport(client),
		Timeout:       60 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	contentRef, apiErr := pollYoukouImageTask(ctx, pollClient, statusURL, info.ApiKey, youkouImagePollTimeout(), youkouImagePollInterval)
	if apiErr != nil {
		service.CloseResponseBodyGracefully(submitResp)
		return nil, apiErr
	}
	responseFormat := ""
	if req, ok := info.Request.(*dto.ImageRequest); ok && req != nil {
		responseFormat = req.ResponseFormat
	}
	imageURL, imageB64, apiErr := fetchYoukouImageResult(ctx, pollClient, noRedirectClient, joinUpstreamURL(info.ChannelBaseUrl, contentRef), info.ApiKey, responseFormat)
	if apiErr != nil {
		service.CloseResponseBodyGracefully(submitResp)
		return nil, apiErr
	}
	finalBody, err := buildYoukouImageResponse(imageURL, imageB64)
	service.CloseResponseBodyGracefully(submitResp)
	if err != nil {
		return fail(fmt.Errorf("youkou image response build failed: %w", err), types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}
	if info.IsStream {
		synth := &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(finalBody)),
		}
		return OpenaiImageJSONAsStreamHandler(c, info, synth)
	}
	service.IOCopyBytesGracefully(c, submitResp, finalBody)
	usage := &dto.Usage{PromptTokens: 1, TotalTokens: 1}
	applyUsagePostProcessing(info, usage, finalBody)
	return usage, nil
}

func truncateYoukouBody(body []byte) []byte {
	if len(body) > 500 {
		return body[:500]
	}
	return body
}

// clientTransport extracts the shared transport, tolerating a nil client.
func clientTransport(client *http.Client) http.RoundTripper {
	if client == nil {
		return nil
	}
	return client.Transport
}
