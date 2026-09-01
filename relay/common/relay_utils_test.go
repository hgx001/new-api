package common

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestValidateMultipartDirectParsesResolutionAndSeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"autodl:multiref-video-1","prompt":"animate","resolution":"1080p横","seed":999}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "1080p横", storedReq.Resolution)
	require.NotNil(t, storedReq.Seed)
	require.Equal(t, int64(999), *storedReq.Seed)
}

func TestValidateMultipartDirectRejectsInvalidSeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"autodl:multiref-video-1","prompt":"animate","seed":"not-a-number"}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}

	taskErr := ValidateMultipartDirect(context, info)

	require.NotNil(t, taskErr)
	require.Equal(t, "invalid_json", taskErr.Code)
	require.ErrorContains(t, taskErr.Error, "seed must be an integer")
}

func TestValidateMultipartDirectParsesMultipartResolutionAndSeed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader("model=autodl%3Amultiref-video-1&prompt=animate&resolution=1080p%E6%A8%AA&seed=999")
	request := httptest.NewRequest(http.MethodPost, "/v1/videos", body)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{TaskRelayInfo: &TaskRelayInfo{}}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, "1080p横", storedReq.Resolution)
	require.NotNil(t, storedReq.Seed)
	require.Equal(t, int64(999), *storedReq.Seed)
}

func TestValidateMultipartDirectNormalizesImageField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := strings.NewReader(`{"model":"wan2.7-i2v","prompt":"animate","image":" https://example.com/first.png "}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/video/generations", body)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	info := &RelayInfo{
		TaskRelayInfo: &TaskRelayInfo{},
	}

	taskErr := ValidateMultipartDirect(context, info)

	require.Nil(t, taskErr)
	storedReq, err := GetTaskRequest(context)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/first.png"}, storedReq.Images)
	require.Equal(t, constant.TaskActionGenerate, info.Action)
}
