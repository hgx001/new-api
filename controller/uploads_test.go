package controller

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /v1/uploads 是「本地参考图走黑白道渠道」的网关入口：工作台把本地图 POST 到这里，
// 这里转投 ArcReel 暂存图床并把公网绝对 URL 交回。渠道解析/转发/错误透传逐项钉死。

func newUploadRequest(t *testing.T, filename, contentType string, data []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if filename != "" {
		part, err := writer.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/uploads", body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Request.ContentLength = int64(body.Len())
	return c, recorder
}

func withStubChannel(t *testing.T, baseURL string) {
	t.Helper()
	orig := resolveManwuStagingChannel
	resolveManwuStagingChannel = func() (*model.Channel, error) {
		return &model.Channel{BaseURL: &baseURL, Key: "sk-service", Status: 1}, nil
	}
	t.Cleanup(func() { resolveManwuStagingChannel = orig })
}

func TestUploadStagingFileForwardsAndReturnsAbsoluteURL(t *testing.T) {
	initHTTPClient(t)
	var gotAuth, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key":"abc/image.png","path":"/api/v1/remote-generation/staging/abc/image.png"}`)
	}))
	defer srv.Close()
	withStubChannel(t, srv.URL)

	c, recorder := newUploadRequest(t, "1.png", "image/png", []byte("png-bytes"))
	UploadStagingFile(c)

	assert.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, "/api/v1/remote-generation/staging/uploads", gotPath)
	assert.Equal(t, "Bearer sk-service", gotAuth)
	assert.Contains(t, recorder.Body.String(), `"url":"`+srv.URL+`/api/v1/remote-generation/staging/abc/image.png"`)
	assert.Contains(t, recorder.Body.String(), `"filename":"1.png"`)
}

func TestUploadStagingFileRejectsMissingFile(t *testing.T) {
	withStubChannel(t, "https://arcreel.example.com")
	c, _ := newUploadRequest(t, "", "", nil)
	UploadStagingFile(c)
	assert.Equal(t, http.StatusBadRequest, c.Writer.Status())
}

func TestUploadStagingFileRejectsOversize(t *testing.T) {
	origCap := maxStagingUploadBytes
	maxStagingUploadBytes = 16
	t.Cleanup(func() { maxStagingUploadBytes = origCap })

	withStubChannel(t, "https://arcreel.example.com")
	c, _ := newUploadRequest(t, "1.png", "image/png", bytes.Repeat([]byte("x"), 32))
	UploadStagingFile(c)
	assert.Equal(t, http.StatusRequestEntityTooLarge, c.Writer.Status())
}

func TestUploadStagingFileReportsNoChannel(t *testing.T) {
	initHTTPClient(t)
	orig := resolveManwuStagingChannel
	resolveManwuStagingChannel = func() (*model.Channel, error) {
		return nil, fmt.Errorf("no manwu channel")
	}
	t.Cleanup(func() { resolveManwuStagingChannel = orig })

	c, _ := newUploadRequest(t, "1.png", "image/png", []byte("png"))
	UploadStagingFile(c)
	assert.Equal(t, http.StatusServiceUnavailable, c.Writer.Status())
}

// ArcReel 侧的 400/413 原样透传给调用方——错误文案里带得上游 detail，
// App 端才能提示「仅支持 png/jpg/webp」而不是笼统的 502。
func TestUploadStagingFilePassesThroughUpstreamDetail(t *testing.T) {
	initHTTPClient(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"detail":"仅支持 png/jpg/webp 图片（按文件内容识别）"}`)
	}))
	defer srv.Close()
	withStubChannel(t, srv.URL)

	c, recorder := newUploadRequest(t, "1.png", "image/png", []byte("not-an-image"))
	UploadStagingFile(c)
	assert.Equal(t, http.StatusBadGateway, recorder.Code)
	assert.True(t, strings.Contains(recorder.Body.String(), "png/jpg/webp"), recorder.Body.String())
}
