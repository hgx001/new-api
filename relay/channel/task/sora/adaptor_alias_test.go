package sora

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 别名模型（对外 seedance2.5-特惠、上游 wan-3.0）的**提交响应体**里，
// 上游回执的 model 字段必须换成对外别名 —— 这是下游第一眼看到的字段，
// 直接暴露 "wan-3.0" 就等于别名白起。
func TestDoResponseAliasModelReturnsPublicModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"id":"task_upstream_1","object":"video","model":"wan-3.0","status":"queued","progress":0}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "seedance2.5-特惠",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_1"},
	}

	upstreamID, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
	require.Nil(t, taskErr)
	assert.Equal(t, "task_upstream_1", upstreamID, "上游任务号要留给内部，落库用")
	assert.Contains(t, w.Body.String(), "task_public_1", "下游拿到的是我们的公开任务号")
	assert.Contains(t, w.Body.String(), "seedance2.5-特惠", "model 字段必须是对外别名")
	assert.NotContains(t, w.Body.String(), "wan-3.0", "响应体里不能出现真实上游模型名")
}

// 非别名模型：model 字段照旧回显上游值（行为不变，别把别人的接口改坏）。
func TestDoResponseNonAliasModelKeepsUpstreamModelName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := `{"id":"task_upstream_2","object":"video","model":"wan-3.0","status":"queued"}`
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     http.Header{},
	}

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)

	info := &relaycommon.RelayInfo{
		OriginModelName: "wan-3.0",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public_2"},
	}

	_, _, taskErr := (&TaskAdaptor{}).DoResponse(ctx, resp, info)
	require.Nil(t, taskErr)
	assert.Contains(t, w.Body.String(), "wan-3.0")
}
