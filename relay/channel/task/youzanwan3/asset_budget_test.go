package youzanwan3

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/require"
)

// 2026-11-12 客户反馈（threeFatAi）：工作台提交超时**写死 60 秒**，而我们的
// POST /v1/videos 是同步拉取素材的 —— 21.89MB 的参考视频放在境外源站上，
// 我们迟迟不返回，客户先超时。
//
// 本文件锁住三条约束：
//   1. 素材处理必须跟着请求 context 走：客户断开就停，不能继续下载并建单扣费；
//   2. 总预算必须明显小于客户的 60 秒，超时要给出可照做的中文文案；
//   3. 多个素材要并发上传，不能一个 20MB 视频把后面所有素材的窗口挤掉。

// ---- 测试替身 ----

// mediaServer 同时扮演「素材源站」和「上游 /v1/assets」。
type mediaServer struct {
	*httptest.Server
	// downloadDelay 按素材序号给出下载延迟，便于制造「后发的先完成」。
	downloadDelay func(index int) time.Duration
	uploadDelay   time.Duration
	// hang 为 true 时下载端永久挂起，只能靠 context 结束。
	hang atomic.Bool

	mu        sync.Mutex
	uploadSeq []string
}

func newMediaServer(t *testing.T) *mediaServer {
	t.Helper()
	disableSSRF(t)
	server := &mediaServer{downloadDelay: func(int) time.Duration { return 0 }}
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/", func(writer http.ResponseWriter, request *http.Request) {
		if server.hang.Load() {
			<-request.Context().Done()
			return
		}
		index, _ := strconv.Atoi(request.URL.Path[len("/asset/"):])
		if delay := server.downloadDelay(index); delay > 0 {
			select {
			case <-time.After(delay):
			case <-request.Context().Done():
				return
			}
		}
		_, _ = writer.Write([]byte("asset-bytes-" + strconv.Itoa(index)))
	})
	mux.HandleFunc("/v1/assets", func(writer http.ResponseWriter, request *http.Request) {
		if server.uploadDelay > 0 {
			select {
			case <-time.After(server.uploadDelay):
			case <-request.Context().Done():
				return
			}
		}
		kind := request.URL.Query().Get("kind")
		body, _ := io.ReadAll(request.Body)
		server.mu.Lock()
		server.uploadSeq = append(server.uploadSeq, kind)
		seq := len(server.uploadSeq)
		server.mu.Unlock()
		// 回执 url 里带上 kind 与收到的字节标记：并发下「到达顺序」不再等于
		// 「入参顺序」，测试必须靠来源标记判断第 i 个位置的素材来自第 i 个入参。
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"asset":{"id":"asset-` + strconv.Itoa(seq) +
			`","kind":"` + kind + `"},"url":"https://signed.example/` + kind + `/` +
			sourceMarker(body) + `-` + strconv.Itoa(seq) + `"}`))
	})
	server.Server = httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

// sourceMarker 从上传的字节里提取一个可断言来源的短标记。
// 参考音频原样上传，标记就是它下载到的内容；图片会被 flattenImage 重编码，
// 只能退化成 binary（这类素材靠 kind 断言位置）。
func sourceMarker(body []byte) string {
	if len(body) <= 64 && utf8.Valid(body) {
		return string(body)
	}
	return "binary"
}

func (s *mediaServer) assetURL(index int) string {
	return fmt.Sprintf("%s/asset/%d", s.URL, index)
}

func (s *mediaServer) uploadedKinds() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.uploadSeq...)
}

// audioMedia 用 reference_audio 绕开 flattenImage：本文件只关心网络与并发，
// 图片重编码的成本由 image_size_test.go 单独覆盖。
func audioMedia(server *mediaServer, index int) relaycommon.TaskMedia {
	return relaycommon.TaskMedia{Type: "reference_audio", URL: server.assetURL(index)}
}

func audioRequest(media ...relaycommon.TaskMedia) relaycommon.TaskSubmitReq {
	return relaycommon.TaskSubmitReq{
		Model:    "wan3.0-video",
		Prompt:   "并发上传",
		Media:    media,
		Duration: 5,
	}
}

// ---- 1. context 传递 ----

func TestReadAssetStopsWhenClientDisconnects(t *testing.T) {
	server := newMediaServer(t)
	server.hang.Store(true)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	_, _, _, err := adaptor.readAsset(ctx, server.assetURL(1))
	elapsed := time.Since(started)

	require.Error(t, err)
	// 关键：客户断开后必须立刻收手，而不是把 20MB 下完。
	require.Less(t, elapsed, 2*time.Second, "context 取消后 readAsset 仍在等待，说明没有接 context")
}

func TestReadAssetDeadlineGivesActionableMessage(t *testing.T) {
	server := newMediaServer(t)
	server.hang.Store(true)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, _, _, err := adaptor.readAsset(ctx, server.assetURL(1))
	require.Error(t, err)

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr, "素材超时必须降级成 400，不能算渠道故障（500 会重试 + AutoBan）")
	require.Contains(t, userErr.Message, "超时")
	require.Contains(t, userErr.Message, "下载")
	// 文案必须告诉客户「怎么办」：换地址或先上传。
	require.Contains(t, userErr.Message, "国内")
}

func TestUploadAssetUploadStageTimeoutIsDistinguished(t *testing.T) {
	server := newMediaServer(t)
	server.uploadDelay = 2 * time.Second
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := adaptor.uploadAsset(ctx, "reference_audio", server.assetURL(1))
	require.Error(t, err)

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr)
	// 下载快、上传慢：文案必须指向「上传」，否则运维会去查素材源站。
	require.Contains(t, userErr.Message, "上传")
	require.NotContains(t, userErr.Message, "下载")
}

func TestUploadAssetPassesClientCancelThrough(t *testing.T) {
	server := newMediaServer(t)
	server.hang.Store(true)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	_, err := adaptor.uploadAsset(ctx, "reference_audio", server.assetURL(1))
	require.Error(t, err)
	// 客户已经断开，没有人会读这条消息：不能包装成「给客户看的 400 文案」。
	var userErr *taskcommon.UserError
	require.False(t, errors.As(err, &userErr), "客户端主动断开不是参数错误，不该走 UserError")
}

func TestBuildRequestBodyPropagatesParentCancellation(t *testing.T) {
	server := newMediaServer(t)
	server.hang.Store(true)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(60 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	_, err := adaptor.buildRequestBody(ctx, audioRequest(audioMedia(server, 1)))
	require.Error(t, err)
	require.Less(t, time.Since(started), 2*time.Second)
	require.Zero(t, len(server.uploadedKinds()), "素材没拿到就不该向上游建单")
}

// ---- 2. 预算 ----

func TestAssetTotalBudgetLeavesRoomForClientTimeout(t *testing.T) {
	// 客户工作台的提交超时写死 60 秒。我们必须在它之前收场，
	// 否则客户看到的是自己的超时，而我们还在后台下载、还会建单扣费。
	require.Less(t, assetTotalBudget, 60*time.Second)
	require.GreaterOrEqual(t, assetTotalBudget, 20*time.Second, "预算太小会把正常的 20MB 素材误杀")
	require.LessOrEqual(t, assetMaxConcurrency, 4, "并发太高会把内存和出口带宽一起打满")
	require.GreaterOrEqual(t, assetMaxConcurrency, 2)
}

func TestAssetBudgetContextActuallyAppliesDeadline(t *testing.T) {
	ctx, cancel := assetBudgetContext(context.Background())
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok, "素材处理必须带 deadline，否则源站卡住就无限等")
	require.WithinDuration(t, time.Now().Add(assetTotalBudget), deadline, 2*time.Second)
}

func TestAssetBudgetContextNeverRelaxesParentDeadline(t *testing.T) {
	parent, parentCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer parentCancel()

	ctx, cancel := assetBudgetContext(parent)
	defer cancel()

	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	// 父级更早的 deadline（比如服务端整体请求超时）必须原样保留，
	// 不能被 50 秒的预算覆盖成更宽松的上限。
	require.WithinDuration(t, time.Now().Add(2*time.Second), deadline, 500*time.Millisecond)
}

func TestAssetBudgetContextSurvivesNilParent(t *testing.T) {
	ctx, cancel := assetBudgetContext(nil)
	defer cancel()
	require.NoError(t, ctx.Err())
	_, ok := ctx.Deadline()
	require.True(t, ok)
}

// ---- 3. 并发 ----

func TestBuildRequestBodyUploadsMediaConcurrently(t *testing.T) {
	server := newMediaServer(t)
	// 每个素材 120ms 下载 + 120ms 上传 = 240ms。
	// 串行 4 个 = 960ms；并发（上限 3）= 2 轮 ≈ 480ms。
	server.downloadDelay = func(int) time.Duration { return 120 * time.Millisecond }
	server.uploadDelay = 120 * time.Millisecond
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	request := audioRequest(
		audioMedia(server, 1), audioMedia(server, 2),
		audioMedia(server, 3), audioMedia(server, 4),
	)

	started := time.Now()
	body, err := adaptor.buildRequestBody(context.Background(), request)
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.Less(t, elapsed, 800*time.Millisecond, "4 个素材的耗时接近串行（≈960ms），并发没生效")

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["media"].([]any), 4)
	require.Len(t, server.uploadedKinds(), 4)
}

func TestBuildRequestBodyKeepsMediaOrderUnderConcurrency(t *testing.T) {
	server := newMediaServer(t)
	// 越靠后的素材越快完成，逼出「完成顺序 ≠ 入参顺序」。
	delays := []time.Duration{150 * time.Millisecond, 100 * time.Millisecond, 50 * time.Millisecond}
	server.downloadDelay = func(index int) time.Duration {
		if index-1 >= 0 && index-1 < len(delays) {
			return delays[index-1]
		}
		return 0
	}
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	request := audioRequest(audioMedia(server, 1), audioMedia(server, 2), audioMedia(server, 3))
	body, err := adaptor.buildRequestBody(context.Background(), request)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	media := payload["media"].([]any)
	require.Len(t, media, 3)

	// 上游按数组顺序对应 @音频1/@音频2/@音频3，顺序错了就是串素材。
	for index, item := range media {
		entry := item.(map[string]any)
		require.Equal(t, "reference_audio", entry["type"])
		require.Contains(t, entry["url"], "asset-bytes-"+strconv.Itoa(index+1),
			"第 %d 个位置的素材串位了：%s", index, entry["url"])
	}
}

func TestBuildRequestBodyNoMediaSkipsAssetPipeline(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://upstream.example", apiKey: "k"}
	body, err := adaptor.buildRequestBody(context.Background(), relaycommon.TaskSubmitReq{
		Model:    "wan3.0-video",
		Prompt:   "纯文生视频",
		Duration: 5,
	})
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.NotContains(t, payload, "media", "没有素材时不能凭空塞一个空 media 数组")
}

// data URL 素材不经过网络，也必须能在并发管线下正常工作。
func TestBuildRequestBodyDataURLMediaUnderConcurrency(t *testing.T) {
	server := newMediaServer(t)
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	request := audioRequest(
		relaycommon.TaskMedia{Type: "reference_audio", URL: "data:audio/mpeg;base64," +
			base64.StdEncoding.EncodeToString([]byte("a"))},
		relaycommon.TaskMedia{Type: "reference_audio", URL: "data:audio/mpeg;base64," +
			base64.StdEncoding.EncodeToString([]byte("b"))},
	)
	body, err := adaptor.buildRequestBody(context.Background(), request)
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["media"].([]any), 2)
}
