package middleware

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type requestParamsRecorder struct {
	body   string
	params map[string]any
}

// 摘要必须记录**客户原始请求**：条件路由把 model 改写成内部目标模型之后，
// 日志里仍要显示客户买的那个模型，否则排查时会被「他从没用过这个模型」误导。
func TestCaptureTaskRequestParamsKeepsOriginalModelBeforeRewrite(t *testing.T) {
	stubChannelAvailable(t, true)
	rec := &requestParamsRecorder{}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CaptureTaskRequestParams())
	// 模拟条件路由改写 model：捕获必须发生在它之前。
	engine.Use(func(c *gin.Context) {
		body, ok := readJSONRequestBody(c)
		if ok {
			body["model"] = constant.Seedance25TeHuiModel
			applyRewrittenBodyKeepingOrigin(c, body, constant.Seedance25HuoDongModel)
		}
		c.Next()
	})
	engine.POST("/v1/video/generations", func(c *gin.Context) {
		rec.params = relaycommon.GetTaskRequestParams(c)
		storage, err := common.GetBodyStorage(c)
		require.NoError(t, err)
		raw, err := storage.Bytes()
		require.NoError(t, err)
		rec.body = string(raw)
		c.String(200, "ok")
	})

	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(`{
		"model":"`+constant.Seedance25HuoDongModel+`",
		"prompt":"a cat",
		"seconds":"10",
		"metadata":{"ratio":"16:9"},
		"media":[{"type":"reference_video","url":"https://a.example/v.mp4"}]
	}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code, response.Body.String())
	rewritten := decodeBody(t, rec.body)
	assert.Equal(t, constant.Seedance25TeHuiModel, rewritten["model"], "前置条件：请求确实被改派了")

	require.NotNil(t, rec.params)
	assert.Equal(t, constant.Seedance25HuoDongModel, rec.params["model"],
		"日志里的模型必须是客户请求的活动版，而不是改派后的特惠")
	assert.Equal(t, "10", rec.params["seconds"])
	assert.Equal(t, "16:9", rec.params["ratio"])

	media, ok := rec.params["media"].([]map[string]string)
	require.True(t, ok)
	require.Len(t, media, 1)
	assert.Equal(t, "reference_video", media[0]["type"])
	assert.Equal(t, "https://a.example/v.mp4", media[0]["url"])
}

// GET 查询任务没有请求参数，不能凭空写一份摘要。
func TestCaptureTaskRequestParamsIgnoresNonPost(t *testing.T) {
	rec := &requestParamsRecorder{}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CaptureTaskRequestParams())
	engine.GET("/v1/video/generations/task-1", func(c *gin.Context) {
		rec.params = relaycommon.GetTaskRequestParams(c)
		c.String(200, "ok")
	})

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest("GET", "/v1/video/generations/task-1", nil))

	require.Equal(t, 200, response.Code)
	assert.Nil(t, rec.params)
}

// 非 JSON 请求体不该影响后续中间件，也不该写出半份摘要。
func TestCaptureTaskRequestParamsIgnoresInvalidJSON(t *testing.T) {
	rec := &requestParamsRecorder{}

	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(CaptureTaskRequestParams())
	engine.POST("/v1/video/generations", func(c *gin.Context) {
		rec.params = relaycommon.GetTaskRequestParams(c)
		c.String(200, "ok")
	})

	request := httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString("not json"))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)

	require.Equal(t, 200, response.Code, "解析失败必须放行，不能挡客户请求")
	assert.Nil(t, rec.params)
}
