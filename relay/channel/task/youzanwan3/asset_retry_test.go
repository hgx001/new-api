package youzanwan3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游真实回包（logs id=18830 的 content 原文）。
const assetDownloadFailedBody = `{"code":"fail_to_fetch_task","message":"{\"error\":{\"code\":\"asset_download_failed\",\"retry_action\":\"fix_request\",\"message\":\"连接素材服务器超时。尚未提交上游生成。\",\"retryable\":false}}"}`

func newTestResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

func TestDoWithAssetDownloadRetry_重试一次后成功(t *testing.T) {
	var calls int
	resp, err := doWithAssetDownloadRetry(context.Background(), func() (*http.Response, error) {
		calls++
		if calls == 1 {
			return newTestResponse(http.StatusBadRequest, assetDownloadFailedBody), nil
		}
		return newTestResponse(http.StatusOK, `{"id":"task_ok"}`), nil
	}, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 2, calls, "第一次素材超时后应重发一次")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"task_ok"}`, string(body), "重试成功时 DoResponse 还要读 body")
}

func TestDoWithAssetDownloadRetry_其它400不重试(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"素材格式非法", `{"error":{"code":"media_invalid","message":"bad image"}}`},
		{"参数错误", `{"error":{"code":"invalid_request","message":"ratio unsupported"}}`},
		{"空 body", ``},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			resp, err := doWithAssetDownloadRetry(context.Background(), func() (*http.Response, error) {
				calls++
				return newTestResponse(http.StatusBadRequest, tc.body), nil
			}, 1, 0)
			require.NoError(t, err)
			assert.Equal(t, 1, calls, "非素材下载失败的重试只会重复烧额度")
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
			// 失败响应体必须还在：DoResponse 靠它透出上游原因。
			body, readErr := io.ReadAll(resp.Body)
			require.NoError(t, readErr)
			assert.Equal(t, tc.body, string(body))
		})
	}
}

func TestDoWithAssetDownloadRetry_响应不是400不重试(t *testing.T) {
	var calls int
	resp, err := doWithAssetDownloadRetry(context.Background(), func() (*http.Response, error) {
		calls++
		// 500 也带同样的错误码：上游内部故障不该用重发掩盖。
		return newTestResponse(http.StatusInternalServerError, assetDownloadFailedBody), nil
	}, 1, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, calls)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestDoWithAssetDownloadRetry_客户端已断开不重试(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	resp, err := doWithAssetDownloadRetry(ctx, func() (*http.Response, error) {
		calls++
		cancel() // 模拟客户端 60 秒预算耗尽后断开
		return newTestResponse(http.StatusBadRequest, assetDownloadFailedBody), nil
	}, 1, time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "ctx 已取消时不能再等一轮重试")
	require.NotNil(t, resp)
	body, readErr := io.ReadAll(resp.Body)
	require.NoError(t, readErr)
	assert.Contains(t, string(body), "asset_download_failed")
}

func TestDoWithAssetDownloadRetry_重试次数用尽后返回最后一次(t *testing.T) {
	var calls int
	resp, err := doWithAssetDownloadRetry(context.Background(), func() (*http.Response, error) {
		calls++
		return newTestResponse(http.StatusBadRequest, assetDownloadFailedBody), nil
	}, 2, 0)
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "maxRetries=2 表示总共 3 次尝试")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestIsAssetDownloadFailed(t *testing.T) {
	assert.True(t, isAssetDownloadFailed(http.StatusBadRequest, []byte(assetDownloadFailedBody)))
	assert.False(t, isAssetDownloadFailed(http.StatusBadRequest, []byte(`{"code":"media_invalid"}`)))
	assert.False(t, isAssetDownloadFailed(http.StatusOK, []byte(assetDownloadFailedBody)))
	assert.False(t, isAssetDownloadFailed(http.StatusBadRequest, nil))
}

// 上游抓素材失败（预热+重发也没成功）时，错误文案必须让运维知道「没扣费、
// 可直接重试」，而不是丢一段 {"code":"fail_to_fetch_task"} 让人猜是不是参数错了。
//
// 2026-11-21：文案还要点明真因是 CDN 的 404 负缓存 —— 运维看到「超时」
// 会去查网络和素材，只有看到「缓存了 404」才知道该往 CDN 方向查。
func TestDoResponse_素材超时给可重试结论(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	adaptor := &TaskAdaptor{}
	taskID, _, taskErr := adaptor.DoResponse(context, newTestResponse(http.StatusBadRequest, assetDownloadFailedBody), nil)

	require.Empty(t, taskID, "没有上游 task id 就不能当成功返回")
	require.NotNil(t, taskErr)
	assert.Contains(t, taskErr.Message, "上游抓取参考素材失败")
	assert.Contains(t, taskErr.Message, "未建单、未扣费")
	assert.Contains(t, taskErr.Message, "可直接重试")
	assert.Contains(t, taskErr.Message, "asset_download_failed", "上游原文要保留，排障还得能搜到错误码")
	assert.Contains(t, taskErr.Message, "404", "要写明真因是 CDN 缓存了 404，否则运维会去查网络")
}
