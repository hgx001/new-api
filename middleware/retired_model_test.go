package middleware

// 「已下线模型」拦截的测试。
//
// 两条硬约束：
//  1. 命中已下线模型 → 400 + 明确的「已下线 + 替代型号」文案，不能静默放行；
//  2. 未命中的请求 → **body 必须原样可用**。拦截器排在一串会读 body 的中间件
//     之前，一旦它把 body 读走没还回去，后面的参数解析、计费口径全部拿到空值。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/QuantumNous/new-api/common"
)

const (
	retiredTestModel       = "seedance2.5-活动"
	retiredTestReplacement = "seedance2.5-特惠"
)

func newRetiredProbeContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestRetiredModelMessage_命中已下线模型(t *testing.T) {
	message, retired := RetiredModelMessage(retiredTestModel)
	require.True(t, retired)
	assert.Contains(t, message, "已下线")
	assert.Contains(t, message, retiredTestReplacement, "必须告诉客户该换成哪个型号")
}

func TestRetiredModelMessage_在售模型不拦截(t *testing.T) {
	for _, modelName := range []string{
		"seedance2.5-特惠",
		"seedance-2-5",
		"wan3.0-video-tj",
		"",
	} {
		message, retired := RetiredModelMessage(modelName)
		assert.False(t, retired, "%q 不该被当成已下线模型", modelName)
		assert.Empty(t, message)
	}
}

func TestRejectRetiredModel_命中时返回明确提示(t *testing.T) {
	c, recorder := newRetiredProbeContext(t, `{"model":"`+retiredTestModel+`","prompt":"x"}`)

	RejectRetiredModel()(c)

	require.True(t, c.IsAborted(), "命中已下线模型必须中止，不能继续选渠道")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	body := recorder.Body.String()
	assert.Contains(t, body, "已下线")
	assert.Contains(t, body, retiredTestReplacement)
	// 必须是 OpenAI 兼容的错误结构，客户端才认得出
	assert.Contains(t, body, `"error"`)
	assert.Contains(t, body, `"new_api_error"`)
}

func TestRejectRetiredModel_模型名带空格也命中(t *testing.T) {
	c, recorder := newRetiredProbeContext(t, `{"model":"  `+retiredTestModel+`  "}`)

	RejectRetiredModel()(c)

	require.True(t, c.IsAborted())
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestRejectRetiredModel_未命中时放行且body可复用(t *testing.T) {
	c, _ := newRetiredProbeContext(t, `{"model":"seedance2.5-特惠","prompt":"保留我"}`)

	RejectRetiredModel()(c)

	require.False(t, c.IsAborted(), "在售模型的请求不能被挡")

	// 后续中间件必须还能读到同一个 model —— 拦截器不能把 body 吃掉
	var probe struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	require.NoError(t, common.UnmarshalBodyReusable(c, &probe))
	assert.Equal(t, "seedance2.5-特惠", probe.Model)
	assert.Equal(t, "保留我", probe.Prompt)
}

func TestRejectRetiredModel_无法解析的body放行(t *testing.T) {
	// 非 JSON / 空 body 不该由「下线拦截」报错：那是参数校验的职责，
	// 这里报错会把「格式错」伪装成「模型已下线」，排查时误导人。
	for _, body := range []string{"", "not-json"} {
		c, _ := newRetiredProbeContext(t, body)
		RejectRetiredModel()(c)
		assert.False(t, c.IsAborted(), "body=%q 应放行给后续中间件处理", body)
	}
}
