package manwu

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 幂等键必须按「客户端本次请求」固定：重试会重新走 ValidateRequestAndSetAction，
// 若每次都生成新 UUID，ArcReel 的幂等重放永远不命中，504 这类「结果未知」失败的
// 重试会重复建单（gen-78754689fdc325f5ce17510c 事故根因之一）。

func validateNorm(t *testing.T, c *gin.Context) *normalizedRequest {
	t.Helper()
	v, ok := c.Get(normalizedRequestKey)
	require.True(t, ok, "normalizedRequest 必须写回 context")
	norm, ok := v.(*normalizedRequest)
	require.True(t, ok)
	return norm
}

func replaySameContext(t *testing.T, c *gin.Context, a *TaskAdaptor, body string) {
	t.Helper()
	// 模拟重试：同一 gin context 再次走 validate（生产上重试换渠道也复用同一 context，
	// GetBodyStorage 支持重复读 body，这里直接重建 request）。
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	require.Nil(t, a.ValidateRequestAndSetAction(c, fixedModelInfo()))
}

func TestIdempotencyKeyIsStableAcrossRetries(t *testing.T) {
	body := `{"model":"Nano Banana Pro","prompt":"一只柴犬"}`
	c, _, a := postVideoCtx(t, body)

	require.Nil(t, a.ValidateRequestAndSetAction(c, fixedModelInfo()))
	first := validateNorm(t, c).IdempotencyKey
	require.True(t, strings.HasPrefix(first, idempotencyKeyPrefix), "首次生成必须带 manwu- 前缀: %q", first)

	replaySameContext(t, c, a, body)
	second := validateNorm(t, c).IdempotencyKey
	assert.Equal(t, first, second, "重试必须复用同一幂等键")

	// 幂等标记必须置位，controller 据此对 408/504/524 放行重试。
	marked, ok := c.Get(CtxKeyTaskSubmitIdempotent)
	require.True(t, ok, "CtxKeyTaskSubmitIdempotent 必须置位")
	assert.Equal(t, true, marked)
}

func TestIdempotencyKeyRespectsClientKey(t *testing.T) {
	body := `{"model":"Nano Banana Pro","prompt":"一只柴犬","idempotency_key":"client-key-1"}`
	c, _, a := postVideoCtx(t, body)

	require.Nil(t, a.ValidateRequestAndSetAction(c, fixedModelInfo()))
	first := validateNorm(t, c).IdempotencyKey
	assert.Equal(t, "client-key-1", first)

	replaySameContext(t, c, a, body)
	assert.Equal(t, "client-key-1", validateNorm(t, c).IdempotencyKey,
		"客户端显式 key 原样透传，重试不漂移")
}

func TestIdempotencyKeyDiffersAcrossRequests(t *testing.T) {
	body := `{"model":"Nano Banana Pro","prompt":"一只柴犬"}`
	c1, _, a1 := postVideoCtx(t, body)
	c2, _, a2 := postVideoCtx(t, body)

	require.Nil(t, a1.ValidateRequestAndSetAction(c1, fixedModelInfo()))
	require.Nil(t, a2.ValidateRequestAndSetAction(c2, fixedModelInfo()))

	assert.NotEqual(t, validateNorm(t, c1).IdempotencyKey, validateNorm(t, c2).IdempotencyKey,
		"不同客户端请求必须各拿各的 key，不能串单")
}
