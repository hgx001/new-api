package common

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 2026-11-10 排障：客户端按 OpenAI 风格把比例放在请求体顶层时，
// 顶层字段被 Go 解码器静默丢弃（TaskSubmitReq 此前没有 ratio 字段）。
//
// 这里锁死 multipart 与 JSON 两条路径的**一致性**：
// multipart 的 ratio / aspect_ratio 必须同时落到 metadata（向后兼容）
// 和顶层字段（与 JSON 路径对齐），不能一条路径有效另一条失效。

func multipartTaskRequest(t *testing.T, fields map[string]string) TaskSubmitReq {
	t.Helper()
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		require.NoError(t, writer.WriteField(key, value))
	}
	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/videos", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request

	req, err := validateMultipartTaskRequest(context, &RelayInfo{}, "generate")
	require.NoError(t, err)
	return req
}

func TestMultipartRatioLandsInBothTopLevelAndMetadata(t *testing.T) {
	req := multipartTaskRequest(t, map[string]string{
		"prompt": "test prompt",
		"model":  "seedance2.5-特惠",
		"ratio":  "9:16",
	})

	require.Equal(t, "9:16", req.Ratio)
	require.Equal(t, "9:16", req.Metadata["ratio"])
}

func TestMultipartAspectRatioLandsInBothTopLevelAndMetadata(t *testing.T) {
	req := multipartTaskRequest(t, map[string]string{
		"prompt":      "test prompt",
		"model":       "seedance2.5-特惠",
		"aspect_ratio": "9:16",
	})

	require.Equal(t, "9:16", req.AspectRatio)
	require.Equal(t, "9:16", req.Metadata["aspect_ratio"])
}

// 没传比例时不得凭空造出值。
func TestMultipartWithoutRatioLeavesFieldsEmpty(t *testing.T) {
	req := multipartTaskRequest(t, map[string]string{
		"prompt": "test prompt",
		"model":  "seedance2.5-特惠",
	})

	require.Empty(t, req.Ratio)
	require.Empty(t, req.AspectRatio)
	_, hasRatio := req.Metadata["ratio"]
	require.False(t, hasRatio)
}

// JSON 路径：顶层比例必须被解码保留（此前被静默丢弃）。
func TestJSONTopLevelRatioIsDecoded(t *testing.T) {
	body := []byte(`{"prompt":"test prompt","model":"seedance2.5-特惠","ratio":"9:16"}`)
	req := TaskSubmitReq{}
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, "9:16", req.Ratio)
}

func TestJSONTopLevelAspectRatioIsDecoded(t *testing.T) {
	body := []byte(`{"prompt":"test prompt","model":"seedance2.5-特惠","aspect_ratio":"9:16"}`)
	req := TaskSubmitReq{}
	require.NoError(t, json.Unmarshal(body, &req))
	require.Equal(t, "9:16", req.AspectRatio)
}
