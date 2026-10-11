package manwu

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// staging 上传是「本地参考图走黑白道渠道」闭环的网关侧一跳：工作台把本地图交给
// new-api，new-api 原样转发到 ArcReel 暂存图床，把返回的站内路径拼成公网绝对
// URL 交给调用方塞进 input_reference。契约错一环（字段名/鉴权/URL 拼接）整条
// 链路就断，所以这里逐项钉死。

func TestStagingUploadPostsMultipartWithServiceToken(t *testing.T) {
	var gotPath, gotAuth, gotField, gotFilename, gotContentType string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		require.NoError(t, r.ParseMultipartForm(32<<20))
		file, header, err := r.FormFile("file")
		require.NoError(t, err)
		defer file.Close()
		gotField, gotFilename, gotContentType = "file", header.Filename, header.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(file)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key":"abc123/image.png","path":"/api/v1/remote-generation/staging/abc123/image.png"}`)
	}))
	defer srv.Close()

	data := []byte("png-bytes")
	stagingPath, err := StagingUpload(context.Background(), srv.Client(), srv.URL, "sk-service", "1.png", "image/png", data)
	require.NoError(t, err)
	assert.Equal(t, "/api/v1/remote-generation/staging/uploads", gotPath)
	assert.Equal(t, "Bearer sk-service", gotAuth)
	assert.Equal(t, "file", gotField)
	assert.Equal(t, "1.png", gotFilename)
	assert.Equal(t, "image/png", gotContentType)
	assert.Equal(t, data, gotBody)
	assert.Equal(t, "/api/v1/remote-generation/staging/abc123/image.png", stagingPath)
}

// ArcReel 的校验失败（非图片 / 超限）必须把状态码与 detail 带回来，不能吞成
// 笼统的 500——App 端要靠它给用户可读的报错。
func TestStagingUploadPropagatesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		fmt.Fprint(w, `{"detail":"文件超过 25MB 上限"}`)
	}))
	defer srv.Close()

	_, err := StagingUpload(context.Background(), srv.Client(), srv.URL, "sk-service", "big.png", "image/png", []byte("x"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "413")
	assert.Contains(t, err.Error(), "25MB")
}

func TestStagingUploadRejectsMalformedUpstreamBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"unexpected": true}`)
	}))
	defer srv.Close()

	_, err := StagingUpload(context.Background(), srv.Client(), srv.URL, "sk-service", "1.png", "image/png", []byte("x"))
	require.Error(t, err, "缺 path 字段的响应必须报错，不能静默产出空 URL")
}

func TestStagingURLJoinsBaseAndPath(t *testing.T) {
	assert.Equal(t,
		"https://arcreel.example.com/api/v1/remote-generation/staging/abc/image.png",
		StagingURL("https://arcreel.example.com/", "/api/v1/remote-generation/staging/abc/image.png"),
	)
	assert.Equal(t,
		"https://arcreel.heibaidao.cn/api/v1/remote-generation/staging/abc/image.png",
		StagingURL("https://arcreel.heibaidao.cn", "api/v1/remote-generation/staging/abc/image.png"),
		"缺前导斜杠的脏 path 也要拼出合法 URL",
	)
}
