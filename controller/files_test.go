package controller

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// /v1/files 是「素材直传对象存储」的网关侧入口：只签发许可，字节不经本机。
// 参数校验必须与 ArcReel 侧白名单同口径 —— 网关先拒能给客户端更快反馈，
// 但两侧必须同步放宽/收紧，否则会出现「一边放行一边拒」的错位。

func newFilesTestContext(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/files", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}

func TestPresignCosUploadFileRejectsMissingFilename(t *testing.T) {
	c, rec := newFilesTestContext(t, `{}`)
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPresignCosUploadFileRejectsNonJSONBody(t *testing.T) {
	c, rec := newFilesTestContext(t, `not-json`)
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPresignCosUploadFileRejectsOverlongFilename(t *testing.T) {
	long := strings.Repeat("a", cosPresignMaxFilenameLen+1)
	c, rec := newFilesTestContext(t, fmt.Sprintf(`{"filename":%q}`, long+".png"))
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// 不在白名单的扩展名必须在网关侧就被拒 —— 不能白跑一次跨服务器往返。
func TestPresignCosUploadFileRejectsDisallowedExtension(t *testing.T) {
	for _, name := range []string{"a.exe", "a.html", "a.svg", "a.php", "a.sh", "a"} {
		c, rec := newFilesTestContext(t, fmt.Sprintf(`{"filename":%q}`, name))
		PresignCosUploadFile(c)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "filename=%s 应被拒", name)
	}
}

// 大小写扩展名要归一化后再判白名单。
func TestPresignCosUploadFileAcceptsUppercaseExtension(t *testing.T) {
	orig := resolveCosPresignChannel
	defer func() { resolveCosPresignChannel = orig }()

	var gotFilename string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		_ = json.NewDecoder(r.Body).Decode(&payload)
		gotFilename = payload["filename"]
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://cos.test/put","object_url":"https://cdn.test/a.png","key":"inputs/u/a.png"}`)
	}))
	defer srv.Close()

	baseURL := srv.URL
	resolveCosPresignChannel = func() (*model.Channel, error) {
		return &model.Channel{
			BaseURL: &baseURL,
			Key:     "sk-service",
		}, nil
	}

	c, rec := newFilesTestContext(t, `{"filename":"A.PNG"}`)
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, ".png", gotFilename, "扩展名应归一化为小写带点形式")
}

// 没有可用漫屋渠道时返回 503，而不是 500。
func TestPresignCosUploadFileReturns503WhenNoChannel(t *testing.T) {
	orig := resolveCosPresignChannel
	defer func() { resolveCosPresignChannel = orig }()

	resolveCosPresignChannel = func() (*model.Channel, error) {
		return nil, fmt.Errorf("没有已启用的漫屋渠道")
	}

	c, rec := newFilesTestContext(t, `{"filename":"a.png"}`)
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// 上游失败时状态码与文案要能透出，App 端要给用户可读提示。
func TestPresignCosUploadFilePropagatesUpstreamFailure(t *testing.T) {
	orig := resolveCosPresignChannel
	defer func() { resolveCosPresignChannel = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"detail":"仅支持 png/jpg/webp 素材"}`)
	}))
	defer srv.Close()

	baseURL := srv.URL
	resolveCosPresignChannel = func() (*model.Channel, error) {
		return &model.Channel{BaseURL: &baseURL, Key: "sk-service"}, nil
	}

	c, rec := newFilesTestContext(t, `{"filename":"a.png"}`)
	PresignCosUploadFile(c)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "仅支持")
}

// 成功路径：返回 url / object_url，且 content_type 与扩展名对应。
func TestPresignCosUploadFileReturnsPresignPayload(t *testing.T) {
	orig := resolveCosPresignChannel
	defer func() { resolveCosPresignChannel = orig }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"url":"https://cos.test/put?sig=1","object_url":"https://cdn.test/inputs/u/a.mp4","key":"inputs/u/a.mp4","expires_in":1800,"max_bytes":26214400,"content_type":"video/mp4"}`)
	}))
	defer srv.Close()

	baseURL := srv.URL
	resolveCosPresignChannel = func() (*model.Channel, error) {
		return &model.Channel{BaseURL: &baseURL, Key: "sk-service"}, nil
	}

	c, rec := newFilesTestContext(t, `{"filename":"clip.mp4"}`)
	PresignCosUploadFile(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "https://cos.test/put?sig=1", body["url"])
	assert.Equal(t, "https://cdn.test/inputs/u/a.mp4", body["object_url"])
	assert.Equal(t, "video/mp4", body["content_type"])
	assert.Equal(t, float64(1800), body["expires_in"])
}
