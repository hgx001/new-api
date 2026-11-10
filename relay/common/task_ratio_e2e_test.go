package common

import (
	"bytes"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 全链路比例回归测试（2026-11-10 审查新增）。
//
// 为什么单独写这个文件：首轮修复的测试都停在**函数级**
// （直接调 resolveRatio / 调 validateMultipartTaskRequest），
// 没有一个测试走完整链路。而首轮 bug 的本质恰恰是「链路某一段把字段丢了」——
// 函数级全绿，线上照样丢比例。
//
// 真实链路是：
//
//	middleware.Distributor
//	  → common.UnmarshalBodyReusable(c, &modelRequest)   ← 第一次读 body
//	  → relay/common.ValidateBasicTaskRequest(c, ...)    ← 第二次读 body
//	    → validateMultipartTaskRequest（仅 multipart）
//	    → common.UnmarshalBodyReusable(c, &req)
//	  → StoreTaskRequest(c, info, action, req)
//
// body 被读两次（multipart 尤其容易出问题），必须用真实顺序测。

// newRatioTestContext 构造一个带真实 body 的 gin context。
func newRatioTestContext(t *testing.T, contentType string, body []byte) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)

	request := httptest.NewRequest("POST", "/v1/videos", bytes.NewReader(body))
	request.Header.Set("Content-Type", contentType)

	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = request
	return context
}

// simulateDistributorRead 模拟 middleware.Distributor 的第一次 body 读取。
//
// 这一步不能省：distributor 会先 UnmarshalBodyReusable，把 c.Request.Body
// 换成可 seek 的 BodyStorage。后面 ValidateBasicTaskRequest 才能再读一次。
// 少了这一步，multipart 会因 body 已被消费而报 "NextPart: EOF"。
func simulateDistributorRead(t *testing.T, context *gin.Context) {
	t.Helper()
	var probe map[string]any
	require.NoError(t, common.UnmarshalBodyReusable(context, &probe))
}

// JSON 路径：客户端按 OpenAI 风格把比例放在请求体顶层。
func TestE2EJSONTopLevelRatioSurvives(t *testing.T) {
	context := newRatioTestContext(t, "application/json",
		[]byte(`{"prompt":"p","model":"seedance2.5-特惠","ratio":"9:16"}`))
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "9:16", stored.Ratio, "JSON 顶层 ratio 必须一路活到 TaskSubmitReq")
}

// JSON 路径：顶层 aspect_ratio。
func TestE2EJSONTopLevelAspectRatioSurvives(t *testing.T) {
	context := newRatioTestContext(t, "application/json",
		[]byte(`{"prompt":"p","model":"seedance2.5-特惠","aspect_ratio":"9:16"}`))
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "9:16", stored.AspectRatio, "JSON 顶层 aspect_ratio 必须一路活到 TaskSubmitReq")
}

// JSON 路径：metadata 里的比例（生产上已验证的传法）。
func TestE2EJSONMetadataRatioSurvives(t *testing.T) {
	context := newRatioTestContext(t, "application/json",
		[]byte(`{"prompt":"p","model":"seedance2.5-特惠","metadata":{"ratio":"9:16"}}`))
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "9:16", stored.Metadata["ratio"])
}

// multipart 路径：顶层 ratio。
func TestE2EMultipartTopLevelRatioSurvives(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("prompt", "p"))
	require.NoError(t, writer.WriteField("model", "seedance2.5-特惠"))
	require.NoError(t, writer.WriteField("ratio", "9:16"))
	require.NoError(t, writer.Close())

	context := newRatioTestContext(t, writer.FormDataContentType(), body.Bytes())
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "9:16", stored.Ratio, "multipart 顶层 ratio 必须一路活到 TaskSubmitReq")
	// multipart 的未知字段本来就会进 metadata —— 这条向后兼容路径不能断。
	require.Equal(t, "9:16", stored.Metadata["ratio"])
}

// multipart 路径：顶层 aspect_ratio。
func TestE2EMultipartTopLevelAspectRatioSurvives(t *testing.T) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("prompt", "p"))
	require.NoError(t, writer.WriteField("model", "seedance2.5-特惠"))
	require.NoError(t, writer.WriteField("aspect_ratio", "9:16"))
	require.NoError(t, writer.Close())

	context := newRatioTestContext(t, writer.FormDataContentType(), body.Bytes())
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "9:16", stored.AspectRatio, "multipart 顶层 aspect_ratio 必须一路活到 TaskSubmitReq")
	require.Equal(t, "9:16", stored.Metadata["aspect_ratio"])
}

// 没传比例时不得凭空造出。
func TestE2ENoRatioStaysEmpty(t *testing.T) {
	context := newRatioTestContext(t, "application/json",
		[]byte(`{"prompt":"p","model":"seedance2.5-特惠"}`))
	simulateDistributorRead(t, context)

	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}
	require.Nil(t, ValidateBasicTaskRequest(context, info, "generate"))

	stored, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Empty(t, stored.Ratio)
	require.Empty(t, stored.AspectRatio)
}
