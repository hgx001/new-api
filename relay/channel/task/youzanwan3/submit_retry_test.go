package youzanwan3

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 上游真实回包（logs id=18984 的 content 原文，促销族 30 秒单）。
const noAvailableChannelBody = `{"error":{"code":"model_not_found","message":"No available channel for model mw-sd2.5-flash under group default: the model is claimed by a task plugin, which has no enabled channel serving it (distributor) (request id: 202611210449158760633948268d9d6zCfO6RBE)","type":"new_api_error"}}`

func TestIsUpstreamNoAvailableChannel_只认503且带渠道缺失标记(t *testing.T) {
	assert.True(t, isUpstreamNoAvailableChannel(http.StatusServiceUnavailable, []byte(noAvailableChannelBody)))
	assert.True(t, isUpstreamNoAvailableChannel(http.StatusServiceUnavailable, []byte(`{"error":{"code":"model_not_found"}}`)),
		"code=model_not_found 是同一件事的另一处写法")
	assert.True(t, isUpstreamNoAvailableChannel(http.StatusServiceUnavailable, []byte("no available channel")),
		"大小写不敏感，对方措辞随版本变过")

	assert.False(t, isUpstreamNoAvailableChannel(http.StatusBadRequest, []byte(noAvailableChannelBody)),
		"400 不是渠道缺失，重发只会烧上游额度")
	assert.False(t, isUpstreamNoAvailableChannel(http.StatusServiceUnavailable, []byte(`{"error":{"message":"gateway restarting"}}`)),
		"503 但和渠道缺失无关，不归这条规则管")
	assert.False(t, isUpstreamNoAvailableChannel(http.StatusServiceUnavailable, nil))
	assert.False(t, isUpstreamNoAvailableChannel(http.StatusOK, []byte(noAvailableChannelBody)))
}

func noChannelRule(maxRetries int) submitRetryRule {
	return submitRetryRule{
		name:       "no_available_channel",
		maxRetries: maxRetries,
		delay:      time.Millisecond,
		match:      isUpstreamNoAvailableChannel,
	}
}

func TestDoWithSubmitRetry_渠道缺失后重发成功(t *testing.T) {
	var calls int
	var logged []string
	resp, err := doWithSubmitRetry(context.Background(), func() (*http.Response, error) {
		calls++
		if calls <= 2 {
			return newTestResponse(http.StatusServiceUnavailable, noAvailableChannelBody), nil
		}
		return newTestResponse(http.StatusOK, `{"id":"task_ok"}`), nil
	}, []submitRetryRule{noChannelRule(4)}, func(rule submitRetryRule, retryNo int, statusCode int, body []byte) {
		assert.Equal(t, http.StatusServiceUnavailable, statusCode)
		assert.Contains(t, string(body), "mw-sd2.5-flash")
		logged = append(logged, rule.name)
	})
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "两次 503 之后第三次成功")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, []string{"no_available_channel", "no_available_channel"}, logged)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"task_ok"}`, string(body), "成功响应必须原样留给 DoResponse")
}

func TestDoWithSubmitRetry_用尽预算后把上游原文交回去(t *testing.T) {
	var calls int
	resp, err := doWithSubmitRetry(context.Background(), func() (*http.Response, error) {
		calls++
		return newTestResponse(http.StatusServiceUnavailable, noAvailableChannelBody), nil
	}, []submitRetryRule{noChannelRule(2)}, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "首次 + 2 次重发，之后立刻放弃")
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, noAvailableChannelBody, string(body), "交回上游原文，DoResponse 才能把真实原因透给客户")
}

func TestDoWithSubmitRetry_不匹配的失败不重发(t *testing.T) {
	var calls int
	resp, err := doWithSubmitRetry(context.Background(), func() (*http.Response, error) {
		calls++
		return newTestResponse(http.StatusBadRequest, `{"code":"media_invalid","message":"参考图张数超限"}`), nil
	}, []submitRetryRule{noChannelRule(4)}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "参数错重发多少次都一样")
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestDoWithSubmitRetry_两类失败各算各的预算(t *testing.T) {
	var calls int
	resp, err := doWithSubmitRetry(context.Background(), func() (*http.Response, error) {
		calls++
		switch calls {
		case 1:
			return newTestResponse(http.StatusBadRequest, assetDownloadFailedBody), nil
		case 2, 3:
			return newTestResponse(http.StatusServiceUnavailable, noAvailableChannelBody), nil
		default:
			return newTestResponse(http.StatusOK, `{"id":"task_ok"}`), nil
		}
	}, []submitRetryRule{
		noChannelRule(2),
		{name: "asset_download_failed", maxRetries: 1, delay: time.Millisecond, match: isAssetDownloadFailed},
	}, nil)
	require.NoError(t, err)
	assert.Equal(t, 4, calls, "素材失败 1 次 + 渠道缺失 2 次，各自的预算互不占用")
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDoWithSubmitRetry_客户端断开后不再重发(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls int
	resp, err := doWithSubmitRetry(ctx, func() (*http.Response, error) {
		calls++
		cancel() // 第一次失败时客户端已经走了
		return newTestResponse(http.StatusServiceUnavailable, noAvailableChannelBody), nil
	}, []submitRetryRule{noChannelRule(4)}, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, calls, "客户都走了，重发只是白等上游")
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestDoWithSubmitRetry_请求直接报错不重发(t *testing.T) {
	var calls int
	resp, err := doWithSubmitRetry(context.Background(), func() (*http.Response, error) {
		calls++
		return nil, assert.AnError
	}, []submitRetryRule{noChannelRule(4)}, nil)
	require.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, resp)
	assert.Equal(t, 1, calls)
}
