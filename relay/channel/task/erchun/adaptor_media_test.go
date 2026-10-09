package erchun

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BuildRequestBody 是本渠道最贵的代码路径：把下游给的素材 URL 下载下来、上传到上游、
// 再把返回的 media_id 组进建单体。2026-10-06 这条链路第一次只能靠**真实付费任务**
// 验证（480P / 2 秒 / 带视频参考，产物 h264 854x480 5.038s 6.13MB 合法 mp4）。
// 任何改动都必须能用离线测试回归，否则下次只能再烧一次钱才能发现。
//
// 用 data: URL 提供素材（readAsset 直接解析，不过网络），只 mock 上游上传端点。

type capturedUpload struct {
	fileName    string
	contentType string
	size        int
}

// newTestAdaptor 起一个只认 /v1/media/uploads 的假上游；override 非 nil 时接管该端点响应。
func newTestAdaptor(t *testing.T, override http.HandlerFunc) (*TaskAdaptor, *[]capturedUpload) {
	t.Helper()
	uploads := &[]capturedUpload{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != uploadPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if override != nil {
			override(w, r)
			return
		}
		if _, params, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err == nil && params["boundary"] != "" {
			mr := multipart.NewReader(r.Body, params["boundary"])
			for {
				part, err := mr.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					break
				}
				data, _ := io.ReadAll(part)
				*uploads = append(*uploads, capturedUpload{
					fileName:    part.FileName(),
					contentType: part.Header.Get("Content-Type"),
					size:        len(data),
				})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"object":"media","media_id":"md_test_001"}`))
	}))
	t.Cleanup(srv.Close)
	return &TaskAdaptor{ChannelType: 66, apiKey: "test-key", baseURL: srv.URL}, uploads
}

func dataURL(mimeType string, payload []byte) string {
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(payload)
}

// 只要 http.DetectContentType 能认出 MIME 即可，无需可解码
func fakeMP4() []byte  { return append([]byte("\x00\x00\x00\x20ftypisom"), bytes.Repeat([]byte{0x20}, 64)...) }
func fakePNG() []byte { return append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 48)...) }

func newRequestCtx(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Set("task_request", req)
	return c
}

func taskInfo(publicTaskID string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}, TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: publicTaskID}}
}

func mustBody(t *testing.T, a *TaskAdaptor, c *gin.Context) io.Reader {
	t.Helper()
	r, err := a.BuildRequestBody(c, taskInfo(""))
	require.NoError(t, err)
	require.NotNil(t, r)
	return r
}

func decodeBody(t *testing.T, r io.Reader) createRequest {
	t.Helper()
	var body createRequest
	require.NoError(t, json.NewDecoder(r).Decode(&body))
	return body
}

// 参考视频必须落到 input_videos，不能落到 input_images。
// 与今天 wan3.0-smart 踩的是同一个坑：素材形状不对时会被静默忽略，
// 任务照样成功，只是参考视频根本没生效。这里把三条路都钉死。
func TestBuildRequestBodyRoutesReferenceMediaToCorrectArray(t *testing.T) {
	a, uploads := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "a rabbit runs through a meadow",
		Resolution: "480P",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: dataURL("image/png", fakePNG())},
			{Type: "reference_video", URL: dataURL("video/mp4", fakeMP4())},
			{Type: "reference_audio", URL: dataURL("audio/mpeg", []byte("ID3fake-mpeg-audio-bytes"))},
		},
	})

	body := decodeBody(t, mustBody(t, a, c))

	require.Len(t, body.InputImages, 1, "参考图进 input_images")
	require.Len(t, body.InputVideos, 1, "参考视频必须进 input_videos")
	require.Len(t, body.InputAudios, 1, "参考音频进 input_audios")
	assert.Equal(t, "md_test_001", body.InputVideos[0].MediaID)
	assert.Equal(t, "md_test_001", body.InputImages[0].MediaID)
	assert.Len(t, *uploads, 3, "三条素材都要上传换 media_id")
}

// multipart part 声明的 MIME 必须与真实内容一致，上游会校验、不一致返 415。
// CreateFormFile 会写死 application/octet-stream 触发 415，所以代码里手工构造了 part header。
func TestUploadMediaSendsRealContentTypeNotOctetStream(t *testing.T) {
	a, uploads := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Resolution: "480P",
		Media:      []relaycommon.TaskMedia{{Type: "reference_video", URL: dataURL("video/mp4", fakeMP4())}},
	})
	mustBody(t, a, c)
	require.Len(t, *uploads, 1)
	assert.Equal(t, "video/mp4", (*uploads)[0].contentType, "part 的 Content-Type 不能是 octet-stream")
	assert.Contains(t, (*uploads)[0].fileName, ".mp4")
}

// 声明成 video 但内容其实是图片，必须按真实 MIME 拒绝。
func TestBuildRequestBodyRejectsMIMEMismatch(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Resolution: "480P",
		Media:      []relaycommon.TaskMedia{{Type: "reference_video", URL: dataURL("image/png", fakePNG())}},
	})
	_, err := a.BuildRequestBody(c, taskInfo(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "video reference does not accept image/png")
}

// 上传返回非 201/200 时不能把错误页当 media_id 用。
func TestUploadMediaFailsLoudlyOnUpstreamError(t *testing.T) {
	a, _ := newTestAdaptor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"quota exhausted"}`))
	})
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Resolution: "480P",
		Media:      []relaycommon.TaskMedia{{Type: "reference_image", URL: dataURL("image/png", fakePNG())}},
	})
	_, err := a.BuildRequestBody(c, taskInfo(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "erchun media upload failed: HTTP 403")
}

// 上传成功但 media_id 为空：不能拿空串建单，否则下游拿到一个永远 pending 的任务。
func TestUploadMediaRejectsEmptyMediaID(t *testing.T) {
	a, _ := newTestAdaptor(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"media","media_id":"   "}`))
	})
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Resolution: "480P",
		Media:      []relaycommon.TaskMedia{{Type: "reference_image", URL: dataURL("image/png", fakePNG())}},
	})
	_, err := a.BuildRequestBody(c, taskInfo(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty media_id")
}

// 建单体必须发上游稳定 ID，不能把我们自己的对外名发过去。
func TestBuildRequestBodySendsUpstreamStableModelID(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{Prompt: "x", Resolution: "480P"})
	assert.Equal(t, UpstreamModelWan3, decodeBody(t, mustBody(t, a, c)).Model)

	// 渠道配了映射就用映射值
	mapped := taskInfo("")
	mapped.UpstreamModelName = "mdl_custom_0001"
	c2 := newRequestCtx(t, relaycommon.TaskSubmitReq{Prompt: "x", Resolution: "480P"})
	r, err := a.BuildRequestBody(c2, mapped)
	require.NoError(t, err)
	assert.Equal(t, "mdl_custom_0001", decodeBody(t, r).Model)

	// 非 mdl_ 前缀的映射值视为无效，回落到稳定 ID（绝不能把对外名发出去）
	junk := taskInfo("")
	junk.UpstreamModelName = PublicModel
	c3 := newRequestCtx(t, relaycommon.TaskSubmitReq{Prompt: "x", Resolution: "480P"})
	r3, err := a.BuildRequestBody(c3, junk)
	require.NoError(t, err)
	assert.Equal(t, UpstreamModelWan3, decodeBody(t, r3).Model)
}

// ChannelMeta 为 nil 时不能 panic —— UpstreamModelName 挂在这个内嵌指针上。
// 线上 GenRelayInfo 总会填它，但适配器不该依赖调用方一定填对。
func TestUpstreamModelSurvivesNilEmbeddedPointers(t *testing.T) {
	a := &TaskAdaptor{}
	assert.Equal(t, UpstreamModelWan3, a.upstreamModel(nil))
	assert.Equal(t, UpstreamModelWan3, a.upstreamModel(&relaycommon.RelayInfo{}))
	assert.Equal(t, UpstreamModelWan3,
		a.upstreamModel(&relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}}))
}

// duration_seconds 必须来自 resolveDuration，包括 Seconds 字符串回退。
// 2026-10-06 首测因漏读 Seconds 按默认 5 秒计费，多收 2.5 倍且全程零报错。
func TestBuildRequestBodyDurationHonoursSecondsString(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)
	for _, tt := range []struct {
		name string
		req  relaycommon.TaskSubmitReq
		want int
	}{
		{"Duration 字段", relaycommon.TaskSubmitReq{Prompt: "x", Duration: 7, Resolution: "480P"}, 7},
		{"Seconds 字符串（真实入参形状）", relaycommon.TaskSubmitReq{Prompt: "x", Seconds: "2", Resolution: "480P"}, 2},
		{"两者都没给 -> 上游默认", relaycommon.TaskSubmitReq{Prompt: "x", Resolution: "480P"}, 5},
		{"低于下界抬到 2", relaycommon.TaskSubmitReq{Prompt: "x", Seconds: "1", Resolution: "480P"}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := newRequestCtx(t, tt.req)
			assert.Equal(t, tt.want, decodeBody(t, mustBody(t, a, c)).DurationSecond)
		})
	}
}

// 幂等键：下游没带时用 task_id 派生，保证同一任务重试复用同一个 Key（否则超时重试会重复下单）。
func TestBuildRequestHeaderDerivesStableIdempotencyKey(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)

	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	req1, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/videos", nil)
	require.NoError(t, a.BuildRequestHeader(c1, req1, taskInfo("task_abc123")))
	assert.Equal(t, "newapi-task_abc123", req1.Header.Get("Idempotency-Key"))
	assert.Equal(t, "Bearer test-key", req1.Header.Get("Authorization"))

	// 下游显式带 Idempotency-Key 时以它为准
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c2.Request.Header.Set("Idempotency-Key", "caller-supplied-key")
	req2, _ := http.NewRequest(http.MethodPost, "https://example.com/v1/videos", nil)
	require.NoError(t, a.BuildRequestHeader(c2, req2, taskInfo("task_abc123")))
	assert.Equal(t, "caller-supplied-key", req2.Header.Get("Idempotency-Key"))
}

// 旧版扁平字段 images 一律当参考图（与其它渠道口径一致），且不能混进 input_videos。
func TestBuildRequestBodyAcceptsLegacyImageField(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{
		Prompt:     "x",
		Resolution: "480P",
		Images:     []string{dataURL("image/png", fakePNG())},
	})
	body := decodeBody(t, mustBody(t, a, c))
	require.Len(t, body.InputImages, 1)
	assert.Empty(t, body.InputVideos)
	assert.Empty(t, body.InputAudios)
}

// prompt 与素材都为空时必须报错，不能建出一个空任务。
func TestBuildRequestBodyRejectsEmptyRequest(t *testing.T) {
	a, _ := newTestAdaptor(t, nil)
	c := newRequestCtx(t, relaycommon.TaskSubmitReq{Resolution: "480P"})
	_, err := a.BuildRequestBody(c, taskInfo(""))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires prompt or reference media")
}

// 非 http(s) 也非 data: 的素材地址必须拒绝。
func TestReadAssetRejectsNonHTTPScheme(t *testing.T) {
	_, _, _, err := readAsset("file:///etc/passwd", maxImageBytes)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requires http(s) URL or data URL")
	assert.True(t, strings.Contains(err.Error(), "requires http(s)"))
}