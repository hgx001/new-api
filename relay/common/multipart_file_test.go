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

	require.NoError(t, writer.WriteField("model", "autodl:minimax-h3-lightx2v-v5-15s"))
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

	require.Equal(t, "autodl:minimax-h3-lightx2v-v5-15s", req.Model)
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

	require.NoError(t, writer.WriteField("model", "autodl:minimax-h3-lightx2v-v5-15s"))
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
	jsonData := `{"model":"autodl:minimax-h3-lightx2v-v5-15s","prompt":"animate","input_reference":["data:image/png;base64,AAA","data:image/png;base64,BBB"]}`

	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	require.Equal(t, "autodl:minimax-h3-lightx2v-v5-15s", req.Model)
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

func TestTaskSubmitReqUnmarshalJSONHandlesAudioInputs(t *testing.T) {
	jsonData := `{"model":"autodl:minimax-h3-u24","prompt":"animate","images":["https://example.com/ref.png"],"audios":["https://example.com/voice.mp3","https://example.com/music.wav"],"audio_duration":"12","seed":0}`

	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)

	require.Equal(t, []string{"https://example.com/voice.mp3", "https://example.com/music.wav"}, req.Audios)
	require.NotNil(t, req.AudioDuration)
	require.Equal(t, 12, *req.AudioDuration)
	require.True(t, req.HasAudio())
	require.NotNil(t, req.Seed)
	require.Equal(t, int64(0), *req.Seed)
}

func TestTaskSubmitReqUnmarshalJSONHandlesAutoDLNativeAudioFields(t *testing.T) {
	jsonData := `{"model":"autodl:minimax-h3-u08","prompt":"animate","ref_audio_0":"https://example.com/voice.mp3","ref_audio_1":"https://example.com/music.wav"}`

	var req TaskSubmitReq
	err := commonpkg.Unmarshal([]byte(jsonData), &req)
	require.NoError(t, err)
	require.Equal(t, []string{"https://example.com/voice.mp3", "https://example.com/music.wav"}, req.Audios)
	require.True(t, req.HasAudio())
}

func TestParseMultipartFormDataHandlesAutoDLAudioFiles(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	require.NoError(t, writer.WriteField("model", "autodl:minimax-h3-lipsync"))
	require.NoError(t, writer.WriteField("audio_duration", "7"))

	imagePart, err := writer.CreateFormFile("images", "portrait.png")
	require.NoError(t, err)
	_, err = imagePart.Write([]byte("fake-image"))
	require.NoError(t, err)

	audioPart, err := writer.CreateFormFile("audios", "speech.mp3")
	require.NoError(t, err)
	_, err = audioPart.Write([]byte("fake-audio"))
	require.NoError(t, err)
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
	require.Len(t, req.Audios, 1)
	require.NotNil(t, req.AudioDuration)
	require.Equal(t, 7, *req.AudioDuration)
	require.True(t, req.HasImage())
	require.True(t, req.HasAudio())
	require.Equal(t, "data:audio/mpeg;base64,ZmFrZS1hdWRpbw==", req.Audios[0])
}
