/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

package controller

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newVideoProxyTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	return c, recorder
}

// TestApplyVideoProxyRangeHeaders 锁住断点续传的入口：
// 客户端带了 Range/If-Range 就必须透传给回源请求，否则每次断流都会从 0 重下。
func TestApplyVideoProxyRangeHeaders(t *testing.T) {
	t.Run("透传 Range 与 If-Range", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://cdn.example.com/a.mp4", nil)
		require.NoError(t, err)

		clientHeader := http.Header{}
		clientHeader.Set("Range", "bytes=14155095-")
		clientHeader.Set("If-Range", `"etag-123"`)

		applyVideoProxyRangeHeaders(req, clientHeader)

		assert.Equal(t, "bytes=14155095-", req.Header.Get("Range"))
		assert.Equal(t, `"etag-123"`, req.Header.Get("If-Range"))
	})

	t.Run("客户端没带 Range 时不发空头", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, "https://cdn.example.com/a.mp4", nil)
		require.NoError(t, err)

		applyVideoProxyRangeHeaders(req, http.Header{})

		assert.Empty(t, req.Header.Get("Range"))
		assert.Empty(t, req.Header.Get("If-Range"))
	})
}

// TestIsVideoProxyRelayableStatus 防止 206 被当成上游故障包成 502：
// 透传 Range 后上游必然回 206，若不在白名单里，断点续传直接失效。
func TestIsVideoProxyRelayableStatus(t *testing.T) {
	allowed := []int{
		http.StatusOK,
		http.StatusPartialContent,
		http.StatusRequestedRangeNotSatisfiable,
	}
	for _, code := range allowed {
		assert.True(t, isVideoProxyRelayableStatus(code), "%d 应当原样转发", code)
	}

	blocked := []int{
		http.StatusMovedPermanently,
		http.StatusNotFound,
		http.StatusForbidden,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	}
	for _, code := range blocked {
		assert.False(t, isVideoProxyRelayableStatus(code), "%d 不应原样转发", code)
	}
}

// TestWriteVideoProxyContent 覆盖转发语义：
// 200 可以长缓存；206/416 是分片响应，绝不能打 public 长缓存，
// 否则中间缓存可能把半截文件当完整产物回给后续请求。
func TestWriteVideoProxyContent(t *testing.T) {
	t.Run("206 透传 Content-Range 且不缓存", func(t *testing.T) {
		c, recorder := newVideoProxyTestContext()
		partial := []byte("0123456789")
		resp := &http.Response{
			StatusCode: http.StatusPartialContent,
			Header: http.Header{
				"Content-Range": []string{"bytes 5-14/100"},
				"Content-Type":  []string{"video/mp4"},
			},
			Body: io.NopCloser(bytes.NewReader(partial)),
		}

		require.NoError(t, writeVideoProxyContent(c.Writer, resp))

		assert.Equal(t, http.StatusPartialContent, recorder.Code)
		assert.Equal(t, "bytes 5-14/100", recorder.Header().Get("Content-Range"))
		assert.Equal(t, "video/mp4", recorder.Header().Get("Content-Type"))
		assert.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
		assert.Equal(t, string(partial), recorder.Body.String())
	})

	t.Run("200 完整内容可缓存一天", func(t *testing.T) {
		c, recorder := newVideoProxyTestContext()
		full := []byte("full-video-bytes")
		resp := &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp4"}},
			Body:       io.NopCloser(bytes.NewReader(full)),
		}

		require.NoError(t, writeVideoProxyContent(c.Writer, resp))

		assert.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "public, max-age=86400", recorder.Header().Get("Cache-Control"))
		assert.Equal(t, string(full), recorder.Body.String())
	})
}
