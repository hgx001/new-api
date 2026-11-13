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
	"github.com/QuantumNous/new-api/setting/system_setting"
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
	// inflight / inflightMax 记录下载的瞬时并发，用来验证并发上限真的生效。
	inflight    atomic.Int32
	inflightMax atomic.Int32
	// imageBytes 是 /image/ 路径返回的真实 JPEG：参考图要走 flattenImage，
	// 假字节会被「无法解析」拦掉，测不出并发。
	imageBytes []byte

	mu        sync.Mutex
	uploadSeq []string
}

func (s *mediaServer) maxInflight() int32 { return s.inflightMax.Load() }

// track 在请求进出时维护并发峰值。
func (s *mediaServer) track() func() {
	current := s.inflight.Add(1)
	for {
		previous := s.inflightMax.Load()
		if current <= previous || s.inflightMax.CompareAndSwap(previous, current) {
			break
		}
	}
	return func() { s.inflight.Add(-1) }
}

func newMediaServer(t *testing.T) *mediaServer {
	t.Helper()
	disableSSRF(t)
	server := &mediaServer{downloadDelay: func(int) time.Duration { return 0 }}
	server.imageBytes = makeJPEG(t, 800, 800)
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/", func(writer http.ResponseWriter, request *http.Request) {
		defer server.track()()
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
	mux.HandleFunc("/image/", func(writer http.ResponseWriter, request *http.Request) {
		defer server.track()()
		if delay := server.downloadDelay(0); delay > 0 {
			select {
			case <-time.After(delay):
			case <-request.Context().Done():
				return
			}
		}
		writer.Header().Set("Content-Type", "image/jpeg")
		_, _ = writer.Write(server.imageBytes)
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
	// /v1/uploads 只收图片（25MB、只验图片魔数）。文案必须把这条建议限定在
	// 参考图上，否则视频客户会照着一个必然失败的入口去试。
	require.Contains(t, userErr.Message, "参考图")
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

	// 2026-11-13：并发 2 → 6。一次提交常见 6~7 张参考图（实测 4 张图 2 路并发
	// 就吃掉 6.7 秒，7 张直接吃光客户的 60 秒窗口）。上限仍是 6，不是无限：
	// 每张图解码后是 width×height×4 字节，6 路的峰值内存要压在 3.7GB 机器的余量内。
	require.GreaterOrEqual(t, assetMaxConcurrency, 6, "6 张图的常规提交必须一轮跑完")
	require.LessOrEqual(t, assetMaxConcurrency, 6, "再高就会把内存和出口带宽一起打满")

	// 参考视频单个可达 100MB，不能和图片共用 6 路；单独压到 2 路。
	require.LessOrEqual(t, assetVideoMaxConcurrency, 2)
	require.GreaterOrEqual(t, assetVideoMaxConcurrency, 1)
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
	// 每个素材 160ms 下载 + 160ms 上传 = 320ms。
	// 串行 4 个 = 1280ms；并发（上限 2，两轮）= 640ms。
	// 阈值取中间 950ms：两边各留 300ms 余量，机器负载高时也不至于误报。
	server.downloadDelay = func(int) time.Duration { return 160 * time.Millisecond }
	server.uploadDelay = 160 * time.Millisecond
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	request := audioRequest(
		audioMedia(server, 1), audioMedia(server, 2),
		audioMedia(server, 3), audioMedia(server, 4),
	)

	started := time.Now()
	body, err := adaptor.buildRequestBody(context.Background(), request)
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.Less(t, elapsed, 950*time.Millisecond, "4 个素材的耗时接近串行（≈1280ms），并发没生效")

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["media"].([]any), 4)
	require.Len(t, server.uploadedKinds(), 4)
}

func TestBuildRequestBodyKeepsMediaOrderUnderConcurrency(t *testing.T) {
	server := newMediaServer(t)
	// 越靠后的素材越快完成，逼出「完成顺序 ≠ 入参顺序」。
	// 并发上限是 2，所以第 3 个素材要等前两个里先完成那个让位：
	// 完成顺序是 2 → 3 → 1，与入参顺序完全相反。
	delays := []time.Duration{200 * time.Millisecond, 60 * time.Millisecond, 40 * time.Millisecond}
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

// 6 张参考图必须一轮并发跑完。
//
// 实测口径（2026-11-13，生产同款源站 Cloudflare）：一张参考图约 300KB、
// 下载 ~1.6 秒；2 路并发的 4 张图吃掉 6.7 秒，7 张就要 11 秒以上，
// 叠加在上游 26 秒的受理时间上，正好撞穿客户 60 秒的提交超时。
func TestUploadMediaRunsSixSmallAssetsInOneRound(t *testing.T) {
	server := newMediaServer(t)
	// 每个素材 150ms 下载 + 150ms 上传 = 300ms。
	// 串行 6 个 = 1800ms；2 路 = 3 轮 900ms；6 路 = 1 轮 300ms。
	server.downloadDelay = func(int) time.Duration { return 150 * time.Millisecond }
	server.uploadDelay = 150 * time.Millisecond
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	// 用真实 JPEG：参考图要过 flattenImage，假字节会被「无法解析」拦掉。
	images := make([]relaycommon.TaskMedia, 0, 6)
	for index := 1; index <= 6; index++ {
		images = append(images, relaycommon.TaskMedia{
			Type: "reference_image",
			URL:  fmt.Sprintf("%s/image/%d", server.URL, index),
		})
	}

	started := time.Now()
	body, err := adaptor.buildRequestBody(context.Background(), audioRequest(images...))
	elapsed := time.Since(started)

	require.NoError(t, err)
	require.Less(t, elapsed, 700*time.Millisecond,
		"6 个素材耗时 %s，接近 2 路并发的 900ms —— 并发上限没提到 6", elapsed)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["media"].([]any), 6)
	require.GreaterOrEqual(t, server.maxInflight(), int32(5),
		"实测峰值并发只有 %d，没有真的跑满", server.maxInflight())
}

// 参考视频单独限流：单个上限 100MB，6 路同时下载会在 3.7GB 的机器上撞 OOM。
func TestUploadMediaCapsReferenceVideosAtTwo(t *testing.T) {
	server := newMediaServer(t)
	server.downloadDelay = func(int) time.Duration { return 120 * time.Millisecond }
	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}

	videos := []relaycommon.TaskMedia{
		{Type: "reference_video", URL: server.assetURL(1)},
		{Type: "reference_video", URL: server.assetURL(2)},
		{Type: "reference_video", URL: server.assetURL(3)},
		{Type: "reference_video", URL: server.assetURL(4)},
	}

	body, err := adaptor.buildRequestBody(context.Background(), audioRequest(videos...))
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, common.Unmarshal(body, &payload))
	require.Len(t, payload["media"].([]any), 4)
	require.LessOrEqual(t, server.maxInflight(), int32(assetVideoMaxConcurrency),
		"参考视频同时下载了 %d 路，超过上限 %d", server.maxInflight(), assetVideoMaxConcurrency)
	require.GreaterOrEqual(t, server.maxInflight(), int32(2), "视频限流不该退化成串行")
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

// ---- 4. 素材源站的问题一律 400，上游的问题一律 500 ----
//
// 这条边界不能糊：素材地址是客户填的，错了要 400（不 AutoBan、不换渠道重试）；
// 上传到上游失败才是渠道故障，必须 500 让重试与 AutoBan 正常工作。
// 边界糊了的后果是「一个客户填错 URL 把整条有赞渠道打到禁用」。

func TestAssetSourceFailuresAreUserErrors(t *testing.T) {
	// disableSSRF 让 httptest 的 127.0.0.1 随机端口能通过校验；
	// 「被 SSRF 拦下」是另一条路径，单独在下面用开启保护的用例覆盖。
	disableSSRF(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/missing", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
	})
	notFound := httptest.NewServer(mux)
	t.Cleanup(notFound.Close)

	cases := []struct {
		name   string
		source string
	}{
		{name: "源站 404", source: notFound.URL + "/missing"},
		{name: "协议不是 http(s)", source: "ftp://example.com/a.mp4"},
		{name: "data URL 损坏", source: "data:audio/mpeg;base64,!!!!"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			adaptor := &TaskAdaptor{baseURL: "https://upstream.example", apiKey: "k"}
			_, _, _, err := adaptor.readAsset(context.Background(), testCase.source)
			require.Error(t, err)

			var userErr *taskcommon.UserError
			require.ErrorAs(t, err, &userErr,
				"素材源站的问题必须降级成 400，否则 500 会触发 AutoBan：%v", err)
		})
	}
}

func TestSSRFBlockedAssetIsUserError(t *testing.T) {
	// 与上一条相反：这里必须**开着** SSRF 保护，验证「客户把素材指向内网」
	// 也是 400 而不是 500 —— 同样是换个地址就能修的问题。
	fetch := system_setting.GetFetchSetting()
	original := *fetch
	fetch.EnableSSRFProtection = true
	t.Cleanup(func() { *fetch = original })

	adaptor := &TaskAdaptor{baseURL: "https://upstream.example", apiKey: "k"}
	_, _, _, err := adaptor.readAsset(context.Background(), "http://10.0.0.8/secret.mp4")
	require.Error(t, err)

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr, "内网素材地址是客户入参问题，不该让渠道背锅：%v", err)
}

func TestAssetTooLargeIsUserError(t *testing.T) {
	disableSSRF(t)
	// 用 Content-Length 直接判超限，不真的传 100MB。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(maxAssetBytes+1))
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}
	_, _, _, err := adaptor.readAsset(context.Background(), server.URL+"/huge.mp4")
	require.Error(t, err)

	var userErr *taskcommon.UserError
	require.ErrorAs(t, err, &userErr)
	require.Contains(t, userErr.Message, "上限")
}

func TestUpstreamAssetUploadFailureStaysServerError(t *testing.T) {
	disableSSRF(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/asset/", func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("asset-bytes"))
	})
	mux.HandleFunc("/v1/assets", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
		_, _ = writer.Write([]byte(`{"ok":false,"error":{"message":"upstream boom"}}`))
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	adaptor := &TaskAdaptor{baseURL: server.URL, apiKey: "k"}
	_, err := adaptor.uploadAsset(context.Background(), "reference_audio", server.URL+"/asset/1")
	require.Error(t, err)

	var userErr *taskcommon.UserError
	require.False(t, errors.As(err, &userErr),
		"上游接收素材失败是渠道故障，必须保留 500 语义（要重试、要 AutoBan），不能降级成 400")
}

func TestClientGoneIsNotDowngradedToUserError(t *testing.T) {
	require.True(t, isClientGone(asAssetSourceError(errClientGone)))
	require.True(t, isClientGone(asAssetSourceError(fmt.Errorf("包一层: %w", errClientGone))))
}
