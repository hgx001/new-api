package common

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http/httptest"
	"net/textproto"
	"testing"

	commonpkg "github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestParseMultipartFormDataHandlesFileFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	require.NoError(t, writer.WriteField("model", "autodl:multiref-video-2"))
	require.NoError(t, writer.WriteField("prompt", "animate"))

	part1, err := writer.CreateFormFile("input_reference", "ref1.png")
	require.NoError(t, err)
	part1.Write([]byte("fake-png-data-1"))

	part2, err := writer.CreateFormFile("input_reference", "ref2.png")
	require.NoError(t, err)
	part2.Write([]byte("fake-png-data-2"))

	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/videos", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	var req TaskSubmitReq
	err = commonpkg.UnmarshalBodyReusable(context, &req)
	require.NoError(t, err)

	require.Equal(t, "autodl:multiref-video-2", req.Model)
	require.Equal(t, "animate", req.Prompt)
	require.Len(t, req.Images, 2)
	require.True(t, req.HasImage())

	expectedDataURI1 := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fake-png-data-1"))
	expectedDataURI2 := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fake-png-data-2"))
	require.Equal(t, expectedDataURI1, req.Images[0])
	require.Equal(t, expectedDataURI2, req.Images[1])
}

func TestParseMultipartFormDataHandlesSingleFileField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	require.NoError(t, writer.WriteField("model", "sora-2"))
	require.NoError(t, writer.WriteField("prompt", "ocean waves"))

	part, err := writer.CreateFormFile("input_reference", "ref.png")
	require.NoError(t, err)
	part.Write([]byte("fake-png-data"))

	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/videos", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	var req TaskSubmitReq
	err = commonpkg.UnmarshalBodyReusable(context, &req)
	require.NoError(t, err)

	require.Equal(t, "sora-2", req.Model)
	require.Len(t, req.Images, 1)

	expectedDataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("fake-png-data"))
	require.Equal(t, expectedDataURI, req.Images[0])
}

func TestParseMultipartFormDataHandlesFileWithExplicitMIME(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	require.NoError(t, writer.WriteField("model", "wan3"))
	require.NoError(t, writer.WriteField("prompt", "dance"))

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", `form-data; name="input_reference"; filename="ref.mp4"`)
	h.Set("Content-Type", "video/mp4")
	part, err := writer.CreatePart(h)
	require.NoError(t, err)
	part.Write([]byte("fake-mp4-data"))

	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/videos", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	var req TaskSubmitReq
	err = commonpkg.UnmarshalBodyReusable(context, &req)
	require.NoError(t, err)

	require.Len(t, req.Images, 1)
	expectedDataURI := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString([]byte("fake-mp4-data"))
	require.Equal(t, expectedDataURI, req.Images[0])
}

func TestParseMultipartFormDataMergesTextAndFileFields(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	require.NoError(t, writer.WriteField("model", "autodl:multiref-video-2"))
	require.NoError(t, writer.WriteField("prompt", "animate"))
	require.NoError(t, writer.WriteField("images", "https://example.com/url-ref.png"))

	part, err := writer.CreateFormFile("input_reference", "file-ref.png")
	require.NoError(t, err)
	part.Write([]byte("fake-png-data"))

	require.NoError(t, writer.Close())

	request := httptest.NewRequest("POST", "/v1/videos", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	var req TaskSubmitReq
	err = commonpkg.UnmarshalBodyReusable(context, &req)
	require.NoError(t, err)

	require.True(t, req.HasImage())
	require.GreaterOrEqual(t, len(req.Images), 2)
}

func TestTaskSubmitReqUnmarshalJSONHandlesArrayInputReference(t *testing.T) {
	jsonData := `{"model":"autodl:multiref-video-2","prompt":"animate","input_reference":["data:image/png;base64,AAA","data:image/png;base64,BBB"]}`

	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	require.Equal(t, "autodl:multiref-video-2", req.Model)
	require.Empty(t, req.InputReference)
	require.Len(t, req.Images, 2)
	require.Equal(t, "data:image/png;base64,AAA", req.Images[0])
	require.Equal(t, "data:image/png;base64,BBB", req.Images[1])
}

func TestTaskSubmitReqUnmarshalJSONHandlesStringInputReference(t *testing.T) {
	jsonData := `{"model":"sora-2","prompt":"ocean","input_reference":"https://example.com/ref.png"}`

	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	require.Equal(t, "https://example.com/ref.png", req.InputReference)
}
