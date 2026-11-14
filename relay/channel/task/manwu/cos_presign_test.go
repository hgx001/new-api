package manwu

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// COS 直传预签名是「素材在境外服务器」场景的正解：字节不经过我们的服务器。
// 契约错一环（路径/鉴权/字段名）整条链路就断，所以逐项钉死。

func TestPresignCosUploadPostsJSONWithServiceToken(t *testing.T) {
	var gotPath, gotAuth, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, 512)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://cos.test/put?sig=1","object_url":"https://cdn.test/inputs/u/a.png","key":"inputs/u/a.png","expires_in":1800,"max_bytes":26214400,"content_type":"image/png"}`)
	}))
	defer srv.Close()

	result, err := PresignCosUpload(context.Background(), srv.Client(), srv.URL, "sk-service", ".png")
	require.NoError(t, err)
	assert.Equal(t, cosPresignPath, gotPath)
	assert.Equal(t, "Bearer sk-service", gotAuth)
	assert.Equal(t, "application/json", gotContentType)
	assert.Contains(t, gotBody, `"filename"`)
	assert.Contains(t, gotBody, `.png`)
	assert.Equal(t, "https://cos.test/put?sig=1", result.URL)
	assert.Equal(t, "https://cdn.test/inputs/u/a.png", result.ObjectURL)
	assert.Equal(t, "inputs/u/a.png", result.Key)
	assert.Equal(t, 1800, result.ExpiresIn)
	assert.Equal(t, int64(26214400), result.MaxBytes)
	assert.Equal(t, "image/png", result.ContentType)
}

// 路径末尾多余的斜杠不能拼出双斜杠路径。
func TestPresignCosUploadTrimsTrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://cos.test/put","object_url":"https://cdn.test/a.png","key":"a.png"}`)
	}))
	defer srv.Close()

	_, err := PresignCosUpload(context.Background(), srv.Client(), srv.URL+"/", "sk-service", ".png")
	require.NoError(t, err)
	assert.Equal(t, cosPresignPath, gotPath)
}

// ArcReel 的校验失败（扩展名不在白名单 / COS 未配置）必须把状态码与 detail
// 带回来，不能吞成笼统的 500——App 端要靠它给用户可读的报错。
func TestPresignCosUploadPropagatesUpstreamError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"detail":"仅支持 png/jpg/webp 素材"}`)
	}))
	defer srv.Close()

	_, err := PresignCosUpload(context.Background(), srv.Client(), srv.URL, "sk-service", ".exe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
	assert.Contains(t, err.Error(), "仅支持")
}

// 响应缺 url / object_url 字段必须报错 —— 绝不能让客户端拿到空地址去 PUT。
func TestPresignCosUploadRejectsIncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"key":"inputs/u/a.png"}`)
	}))
	defer srv.Close()

	_, err := PresignCosUpload(context.Background(), srv.Client(), srv.URL, "sk-service", ".png")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "object_url")
}

// 响应不是合法 JSON 时报错，不 panic。
func TestPresignCosUploadRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `not-json`)
	}))
	defer srv.Close()

	_, err := PresignCosUpload(context.Background(), srv.Client(), srv.URL, "sk-service", ".png")
	require.Error(t, err)
}
