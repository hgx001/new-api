package yuanmeng

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestContext(t *testing.T, req relaycommon.TaskSubmitReq) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("task_request", req)
	return c
}

func newAdaptor() *TaskAdaptor {
	return &TaskAdaptor{baseURL: "https://yuanmengstudio.com", apiKey: "test-token"}
}

// 时长：上游接受 2～30 的整数。区间外 fail-loud，不吸附 ——
// 静默改档会让用户按 31 秒的期待拿到 30 秒成片。
func TestResolveDuration(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    int
		wantErr bool
	}{
		{name: "缺省走默认 5 秒", req: relaycommon.TaskSubmitReq{}, want: 5},
		{name: "下界 2 秒", req: relaycommon.TaskSubmitReq{Duration: 2}, want: 2},
		{name: "区间内 15 秒", req: relaycommon.TaskSubmitReq{Duration: 15}, want: 15},
		{name: "上界 30 秒", req: relaycommon.TaskSubmitReq{Duration: 30}, want: 30},
		{name: "1 秒被拒", req: relaycommon.TaskSubmitReq{Duration: 1}, wantErr: true},
		{name: "31 秒被拒", req: relaycommon.TaskSubmitReq{Duration: 31}, wantErr: true},
		{name: "seconds 字符串形状", req: relaycommon.TaskSubmitReq{Seconds: "7"}, want: 7},
		{name: "seconds 45 被拒", req: relaycommon.TaskSubmitReq{Seconds: "45"}, wantErr: true},
		{name: "seconds 非数字走默认", req: relaycommon.TaskSubmitReq{Seconds: "abc"}, want: 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveDuration(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 分辨率：上游三档同价，但仍要 fail-loud 校验 ——
// 用户要 4K 却静默拿到 480P 是比报错更糟的体验。
func TestResolveResolution(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr bool
	}{
		{name: "缺省 720P", req: relaycommon.TaskSubmitReq{}, want: resolution720P},
		{name: "标准 1080P", req: relaycommon.TaskSubmitReq{Resolution: "1080P"}, want: resolution1080P},
		{name: "小写 1080p 归一", req: relaycommon.TaskSubmitReq{Resolution: "1080p"}, want: resolution1080P},
		{name: "纯数字 480", req: relaycommon.TaskSubmitReq{Resolution: "480"}, want: resolution480P},
		{name: "纯数字 720 归一", req: relaycommon.TaskSubmitReq{Resolution: "720"}, want: resolution720P},
		{name: "4K 被拒", req: relaycommon.TaskSubmitReq{Resolution: "4K"}, wantErr: true},
		{name: "2160P 被拒", req: relaycommon.TaskSubmitReq{Resolution: "2160P"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveResolution(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveRatio(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr bool
	}{
		{name: "缺省 16:9", req: relaycommon.TaskSubmitReq{}, want: "16:9"},
		{name: "metadata.ratio", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"ratio": "9:16"}}, want: "9:16"},
		{name: "metadata.aspect_ratio", req: relaycommon.TaskSubmitReq{Metadata: map[string]interface{}{"aspect_ratio": "1:1"}}, want: "1:1"},
		{name: "size 写比例", req: relaycommon.TaskSubmitReq{Size: "21:9"}, want: "21:9"},
		{name: "斜杠写法归一", req: relaycommon.TaskSubmitReq{Size: "4/3"}, want: "4:3"},
		{name: "全角冒号归一", req: relaycommon.TaskSubmitReq{Size: "3：4"}, want: "3:4"},
		{name: "非法比例被拒", req: relaycommon.TaskSubmitReq{Size: "5:2"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveRatio(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveMode(t *testing.T) {
	cases := []struct {
		name    string
		req     relaycommon.TaskSubmitReq
		want    string
		wantErr bool
	}{
		{name: "缺省 reference", req: relaycommon.TaskSubmitReq{}, want: modeReference},
		{name: "首帧", req: relaycommon.TaskSubmitReq{Mode: "first_frame"}, want: modeFirstFrame},
		{name: "首尾帧", req: relaycommon.TaskSubmitReq{Mode: "first_last"}, want: modeFirstLast},
		{name: "大写归一", req: relaycommon.TaskSubmitReq{Mode: "FIRST_FRAME"}, want: modeFirstFrame},
		{name: "未知模式被拒", req: relaycommon.TaskSubmitReq{Mode: "bogus"}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveMode(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// 素材上限严格按上游文档：图 10 / 音 5 / 视频 5 / 合计 20。
func TestBuildMediaEnforcesUpstreamLimits(t *testing.T) {
	t.Run("参考图超过 10 张被拒", func(t *testing.T) {
		var media []relaycommon.TaskMedia
		for i := 0; i < 11; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: "https://x/1.png"})
		}
		_, err := buildMedia(relaycommon.TaskSubmitReq{Media: media})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 10 reference images")
	})

	t.Run("参考图恰好 10 张通过", func(t *testing.T) {
		var media []relaycommon.TaskMedia
		for i := 0; i < 10; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: "https://x/1.png"})
		}
		_, err := buildMedia(relaycommon.TaskSubmitReq{Media: media})
		require.NoError(t, err)
	})

	t.Run("参考音频超过 5 段被拒", func(t *testing.T) {
		var media []relaycommon.TaskMedia
		for i := 0; i < 6; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://x/1.mp3"})
		}
		_, err := buildMedia(relaycommon.TaskSubmitReq{Media: media})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 5 reference audios")
	})

	t.Run("合计恰好 20 个通过（10图+5音+5视频）", func(t *testing.T) {
		var media []relaycommon.TaskMedia
		for i := 0; i < 10; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_image", URL: "https://x/1.png"})
		}
		for i := 0; i < 5; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_audio", URL: "https://x/1.mp3"})
		}
		for i := 0; i < 5; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_video", URL: "https://x/1.mp4"})
		}
		_, err := buildMedia(relaycommon.TaskSubmitReq{Media: media})
		require.NoError(t, err)
	})

	t.Run("视频超过 5 段被拒", func(t *testing.T) {
		var media []relaycommon.TaskMedia
		for i := 0; i < 6; i++ {
			media = append(media, relaycommon.TaskMedia{Type: "reference_video", URL: "https://x/1.mp4"})
		}
		_, err := buildMedia(relaycommon.TaskSubmitReq{Media: media})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 5 reference videos")
	})

	t.Run("未知素材类型被拒", func(t *testing.T) {
		_, err := buildMedia(relaycommon.TaskSubmitReq{
			Media: []relaycommon.TaskMedia{{Type: "weird", URL: "https://x/1.png"}},
		})
		require.Error(t, err)
	})
}

// 首尾帧模式与全模态素材互斥 —— 上游会拒单，本站提前给可读报错。
func TestBuildMediaRejectsFramesCombinedWithReferences(t *testing.T) {
	_, err := buildMedia(relaycommon.TaskSubmitReq{
		Mode:  modeFirstFrame,
		Media: []relaycommon.TaskMedia{{Type: "reference_image", URL: "https://x/1.png"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not accept reference")
}

func TestBuildMediaReadsMetadataMedia(t *testing.T) {
	metadata := map[string]interface{}{
		"media": []map[string]interface{}{
			{"type": "reference_image", "url": "https://x/1.png"},
		},
	}
	set, err := buildMedia(relaycommon.TaskSubmitReq{Metadata: metadata})
	require.NoError(t, err)
	require.Len(t, set.refs, 1)
	assert.Equal(t, "https://x/1.png", set.refs[0])
}

func TestBuildMediaReadsNestedInputMedia(t *testing.T) {
	metadata := map[string]interface{}{
		"input": map[string]interface{}{
			"media": []map[string]interface{}{
				{"type": "reference_video", "url": "https://x/1.mp4"},
			},
		},
	}
	set, err := buildMedia(relaycommon.TaskSubmitReq{Metadata: metadata})
	require.NoError(t, err)
	require.Len(t, set.videoRefs, 1)
}

// 建单请求体必须与上游字段名逐字一致（refs/audioRefs/videoRefs/firstFrame/lastFrame）。
func TestBuildRequestBodyUsesUpstreamFieldNames(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{
		Prompt:     "图1 里的人站在 海边",
		Duration:   10,
		Resolution: "1080P",
		Size:       "16:9",
		Media: []relaycommon.TaskMedia{
			{Type: "reference_image", URL: "https://x/1.png"},
			{Type: "reference_audio", URL: "https://x/1.mp3"},
			{Type: "reference_video", URL: "https://x/1.mp4"},
		},
	})

	reader, err := a.BuildRequestBody(c, nil)
	require.NoError(t, err)
	buf := make([]byte, 4096)
	n, _ := reader.Read(buf)
	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(buf[:n], &body))

	assert.Equal(t, "图1 里的人站在 海边", body["prompt"])
	assert.EqualValues(t, 10, body["duration"])
	assert.Equal(t, "1080P", body["resolution"])
	assert.Equal(t, "16:9", body["ratio"])
	assert.Equal(t, "reference", body["mode"])
	assert.Equal(t, true, body["audio"])

	refs, ok := body["refs"].([]interface{})
	require.True(t, ok, "refs 必须是数组")
	require.Len(t, refs, 1)
	assert.Equal(t, "https://x/1.png", refs[0])

	audioRefs, ok := body["audioRefs"].([]interface{})
	require.True(t, ok, "audioRefs 必须是数组")
	require.Len(t, audioRefs, 1)

	videoRefs, ok := body["videoRefs"].([]interface{})
	require.True(t, ok, "videoRefs 必须是数组")
	require.Len(t, videoRefs, 1)
}

// 鉴权头是 X-Token，**不是** Authorization: Bearer —— 写错就是 401。
func TestBuildRequestHeaderUsesXToken(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{})
	req, err := http.NewRequest(http.MethodPost, "https://yuanmengstudio.com/vr/api/tasks", nil)
	require.NoError(t, err)

	require.NoError(t, a.BuildRequestHeader(c, req, nil))
	assert.Equal(t, "test-token", req.Header.Get("X-Token"))
	assert.Empty(t, req.Header.Get("Authorization"), "上游不认 Bearer，不能发 Authorization 头")
}

func TestBuildRequestURLUsesUpstreamPath(t *testing.T) {
	a := newAdaptor()
	url, err := a.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://yuanmengstudio.com/vr/api/tasks", url)
}

func TestBuildRequestBodyRejectsShortPrompt(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{Prompt: "短"})
	_, err := a.BuildRequestBody(c, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too short")
}

func TestBuildRequestBodyRejectsEmptyPrompt(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{})
	_, err := a.BuildRequestBody(c, nil)
	require.Error(t, err)
}

func TestBuildRequestBodyRejectsTooLongPrompt(t *testing.T) {
	a := newAdaptor()
	c := newTestContext(t, relaycommon.TaskSubmitReq{Prompt: strings.Repeat("啊", 20001)})
	_, err := a.BuildRequestBody(c, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "too long")
}

// 上游可能用 HTTP 200 + ok:false 表达业务失败，必须 fail-loud。
func TestParseTaskResultTreatsNotOKAsFailure(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(`{"ok":false,"msg":"次数不足"}`))
	require.NoError(t, err)
	assert.Equal(t, "次数不足", result.Reason)
}

func TestParseTaskResultMapsUpstreamStatuses(t *testing.T) {
	a := newAdaptor()
	cases := []struct {
		name       string
		body       string
		wantStatus string
		wantProg   string
	}{
		{
			name:       "候车中",
			body:       `{"ok":true,"task":{"id":"T130","status":"pending"}}`,
			wantStatus: "IN_PROGRESS",
			wantProg:   taskcommon.ProgressInProgress,
		},
		{
			name:       "已发车",
			body:       `{"ok":true,"task":{"id":"T130","status":"submitted"}}`,
			wantStatus: "IN_PROGRESS",
			wantProg:   taskcommon.ProgressInProgress,
		},
		{
			name:       "生成中",
			body:       `{"ok":true,"task":{"id":"T130","status":"running"}}`,
			wantStatus: "IN_PROGRESS",
			wantProg:   taskcommon.ProgressInProgress,
		},
		{
			name:       "完成",
			body:       `{"ok":true,"task":{"id":"T130","status":"done","videoUrl":"https://x/1.mp4"}}`,
			wantStatus: "SUCCESS",
			wantProg:   taskcommon.ProgressComplete,
		},
		{
			name:       "失败带原因",
			body:       `{"ok":true,"task":{"id":"T130","status":"failed","failReason":"提示词违规"}}`,
			wantStatus: "FAILURE",
			wantProg:   taskcommon.ProgressComplete,
		},
		{
			name:       "失败无原因走兜底",
			body:       `{"ok":true,"task":{"id":"T130","status":"failed"}}`,
			wantStatus: "FAILURE",
			wantProg:   taskcommon.ProgressComplete,
		},
		{
			name:       "已取消",
			body:       `{"ok":true,"task":{"id":"T130","status":"cancelled"}}`,
			wantStatus: "FAILURE",
			wantProg:   taskcommon.ProgressComplete,
		},
		{
			name:       "未知状态当进行中",
			body:       `{"ok":true,"task":{"id":"T130","status":"weird"}}`,
			wantStatus: "IN_PROGRESS",
			wantProg:   taskcommon.ProgressInProgress,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := a.ParseTaskResult([]byte(tc.body))
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, string(result.Status))
			assert.Equal(t, tc.wantProg, result.Progress)
		})
	}
}

// ⚠️ 成片直链只有约 24 小时有效 —— **绝不能**透传给客户端。
// 透传等于给用户一个定时炸弹：隔天取片必然 404，而任务是「成功」状态。
func TestParseTaskResultHidesUpstreamDirectLink(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(
		`{"ok":true,"task":{"id":"T130","status":"done","videoUrl":"https://upstream/expire-in-24h.mp4"}}`))
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", string(result.Status))
	assert.Empty(t, result.Url, "上游直链有 24 小时有效期，不能透传给客户端")
}

// 终态却没给成片地址：不能当成功，否则客户端拿到打不开的地址。
func TestParseTaskResultDoesNotSucceedWithoutVideoURL(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(`{"ok":true,"task":{"id":"T130","status":"done"}}`))
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", string(result.Status))
}

// 列表里找不到目标任务时**不能判失败** —— 排队通道可能等几小时，
// 判失败会误退款。
func TestParseTaskResultMissingTaskStaysInProgress(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(`{"ok":true,"tasks":[],"total":0}`))
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", string(result.Status))
}

// 上游列表里的任务 ID 可能是数字 —— 必须能匹配上，否则任务永远卡在进行中。
// 注意：输入是**单任务**形态（FetchTask 定位后的形态）。
func TestParseTaskResultMatchesNumericTaskID(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(
		`{"ok":true,"task":{"id":130,"status":"done","videoUrl":"https://x/1.mp4"}}`))
	require.NoError(t, err)
	assert.Equal(t, "SUCCESS", string(result.Status))
}

// ⚠️ 列表形态下**绝不能**取第一条当结果 —— 上游没有单任务查询接口，
// 找不到目标任务时 FetchTask 会原样回传整个列表。取第一条 = 交付别人的成片。
func TestParseTaskResultNeverTakesFirstListEntry(t *testing.T) {
	a := newAdaptor()
	result, err := a.ParseTaskResult([]byte(
		`{"ok":true,"tasks":[{"id":"T999","status":"done","videoUrl":"https://x/other.mp4"}]}`))
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", string(result.Status),
		"列表形态下必须保持进行中，绝不能把别人的成片当成本人的结果")
}

// FetchTask 必须在列表里**精确定位**目标任务再包装 ——
// 上游返回的是整个列表，不定位的话会取到别人的任务。
func TestFetchTaskLocatesTargetTaskInList(t *testing.T) {
	a := newAdaptor()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"tasks":[
			{"id":"T100","status":"done","videoUrl":"https://x/other.mp4"},
			{"id":"T130","status":"running"},
			{"id":"T200","status":"failed"}
		]}`))
	}))
	defer server.Close()

	resp, err := a.FetchTask(server.URL, "tok", map[string]any{"task_id": "T130"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	result, err := a.ParseTaskResult(body)
	require.NoError(t, err)
	// 必须是 T130（running），不能是列表第一条 T100（done）
	assert.Equal(t, "IN_PROGRESS", string(result.Status))
	assert.Contains(t, string(body), "T130")
	assert.NotContains(t, string(body), "T100")
}

// 上游列表接口返回 5xx 时必须原样回传状态码 ——
// 框架靠这个判断「上游临时故障 ≠ 任务失败」，留给下一轮重试。
func TestFetchTaskPassesThroughUpstreamErrorStatus(t *testing.T) {
	a := newAdaptor()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"ok":false,"msg":"gateway"}`))
	}))
	defer server.Close()

	resp, err := a.FetchTask(server.URL, "tok", map[string]any{"task_id": "T130"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)
}

// 上游列表里找不到目标任务时保持进行中，**不判失败**（排队可能等几小时）。
func TestFetchTaskMissingTargetKeepsRawList(t *testing.T) {
	a := newAdaptor()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"tasks":[{"id":"T999","status":"done","videoUrl":"https://x/1.mp4"}]}`))
	}))
	defer server.Close()

	resp, err := a.FetchTask(server.URL, "tok", map[string]any{"task_id": "T130"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	result, err := a.ParseTaskResult(body)
	require.NoError(t, err)
	assert.Equal(t, "IN_PROGRESS", string(result.Status))
}

// 上游没有单任务查询接口，只能用列表接口 —— day 必须用 all，
// 否则昨天提交、今天才发车的任务会被漏掉。
func TestFetchTaskUsesDayAllAndXToken(t *testing.T) {
	a := newAdaptor()
	var gotPath, gotQuery, gotToken string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotToken = r.Header.Get("X-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"tasks":[]}`))
	}))
	defer server.Close()

	resp, err := a.FetchTask(server.URL, "my-token", map[string]any{"task_id": "T130"}, "")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, "/vr/api/tasks", gotPath)
	assert.Contains(t, gotQuery, "day=all")
	assert.Contains(t, gotQuery, "pageSize=100")
	assert.Equal(t, "my-token", gotToken)
}

func TestFetchTaskRejectsEmptyTaskID(t *testing.T) {
	a := newAdaptor()
	_, err := a.FetchTask("https://x", "k", map[string]any{"task_id": ""}, "")
	require.Error(t, err)
}

// 计费口径：按次。上游每条固定扣 1 次，**不能**按时长叠加倍率。
func TestEstimateBillingReturnsNoRatios(t *testing.T) {
	a := newAdaptor()
	assert.Nil(t, a.EstimateBilling(nil, nil))
}

// 对外模型名必须是本站的公开名，不能泄露上游渠道。
func TestChannelNameAndModelList(t *testing.T) {
	a := newAdaptor()
	assert.Equal(t, ChannelName, a.GetChannelName())
	assert.Equal(t, []string{PublicModel}, a.GetModelList())
}
