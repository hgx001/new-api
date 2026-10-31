package wan3

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"

	"github.com/pkg/errors"
)

// refVideoSecondsContextKey 保存 validate 阶段探测出的参考视频总秒数（向上取整），
// EstimateBilling 读取它把秒数并入计费（生成秒数 + 参考视频总秒数）。
const refVideoSecondsContextKey = "wan3_ref_video_seconds"

// durationEpsilon 是浮点比较容差：mvhd 的 duration/timescale 除法可能让
// 恰好 15 秒的片子算出 15.000000000000002，用严格大于会误拒合法请求。
const durationEpsilon = taskcommon.DurationEpsilon

// mp4Seconds 保留为本包的薄封装：解析逻辑已统一到 taskcommon.ParseMP4Seconds，
// 这里只是为了不让本包测试与调用点跟着改（单一实现，多一个名字而已）。
func mp4Seconds(data []byte) (float64, bool) {
	return taskcommon.ParseMP4Seconds(data)
}

// probedVideo 是一次参考视频探测的结果。
type probedVideo struct {
	// seconds 是计费口径：四舍五入后的整秒（与上游 input_video_duration 对齐）。
	seconds int
	// realSeconds 是真实时长，用于官方范围校验（单个 [1,15] 秒、总计 ≤15 秒）。
	realSeconds float64
}

// probeReferenceVideoSeconds 探测单个参考视频的时长。
// 支持 data URL 与公网 http(s) URL；解析不出时长时返回错误（fail-loud，防少收）。
//
// 单个时长必须落在官方区间 [1, 15] 秒 —— 超限或过短都在本地拒：
// 上游会拒，但那时本站已经按超限秒数预扣了钱。
func probeReferenceVideoSeconds(rawURL string) (probedVideo, error) {
	return probeReferenceVideoSecondsContext(context.Background(), rawURL)
}

// probeReferenceVideoSecondsContext 是带取消能力的探测实现。
//
// 为什么需要 ctx：service.GetHttpClient 在 RELAY_TIMEOUT=0（生产默认）时
// **不设 Timeout**，一个「连得上但一直不吐数据」的地址会让 Body.Read 无限阻塞。
// adaptor 里探测至少还有请求级超时兜底，而条件路由中间件的探测跑在请求最前面，
// 挂住就是整个请求挂住，所以中间件那条路径必须自带超时。
func probeReferenceVideoSecondsContext(ctx context.Context, rawURL string) (probedVideo, error) {
	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return probedVideo{}, errors.Wrap(err, "new http client failed")
	}
	seconds, err := taskcommon.ProbeReferenceVideoDuration(ctx, rawURL, client)
	if err != nil {
		return probedVideo{}, err
	}
	// 官方《万相3.0-视频生成API参考》：参考视频单个时长 [1, 15] 秒。
	if seconds < 1-durationEpsilon {
		return probedVideo{}, errors.Errorf("reference video duration %.2fs is below the 1s minimum", seconds)
	}
	if seconds > maxReferenceVideoTotalSeconds+durationEpsilon {
		return probedVideo{}, errors.Errorf("reference video duration %.2fs exceeds the %ds per-video limit", seconds, maxReferenceVideoTotalSeconds)
	}
	return probedVideo{seconds: taskcommon.RoundSecondsToBilling(seconds), realSeconds: seconds}, nil
}

// MaxReferenceVideoTotalSeconds 是官方允许的**参考视频总时长**上限（秒）。
//
// 官方《万相3.0-视频生成API参考》：reference_video 最多 5 段，
// **单个 [1, 15] 秒，总时长不大于 15 秒**。
const MaxReferenceVideoTotalSeconds = maxReferenceVideoTotalSeconds

// MaxTotalDurationWithVideo 是「输入视频总时长 + 输出视频时长」的上限（秒）。
//
// 官方同一条约束：**有视频输入时，输入视频总时长 + 输出视频时长不超过 30 秒**。
// 本站计费秒数（生成 + 参考视频）与上游 usage.duration 是同一个数。
const MaxTotalDurationWithVideo = maxTotalDurationWithVideo

// 探测预算。为什么必须有：service.GetHttpClient 在 RELAY_TIMEOUT=0（生产默认）
// 时**不设 Timeout**，一个「连得上但一直不吐数据」的地址会让 Body.Read 无限阻塞。
// adaptor 里探测至少还有请求级超时兜底，而条件路由中间件的探测跑在请求最前面，
// 挂住就是整个请求挂住，所以那条路径必须自带超时。
//
// 分两级而不是一个总预算：客户可以传最多 5 段参考视频，共用一个总超时会让
// 第 5 段几乎没时间可用（前面几段吃光了预算），把合规请求误判成探测失败。
const (
	// probePerReferenceVideoTimeout 是单条参考视频的下载预算。
	probePerReferenceVideoTimeout = 10 * time.Second
	// probeReferenceVideosTimeout 是整组的上限，防止 5 条各耗 10 秒。
	probeReferenceVideosTimeout = 30 * time.Second
)

// ProbeReferenceVideos 探测一组参考视频的总时长，供条件路由中间件在**改派前**预检。
//
// 为什么中间件需要它：wan3 对参考视频有两条硬限制（单条 ≤15 秒、总量 ≤15 秒、
// 输入+输出 ≤30 秒），超限会在提交阶段直接 400。而改派发生在**选渠道之前** ——
// 中间件不知道这些限制，就会把「源链路本来能跑」的请求扔给 wan3，客户白等一次
// 还看不懂失败原因。生产实例（2026-10-31 01:10，logs id=9379）：
//
//	status_code=400, probe reference video duration failed:
//	reference video duration 32.39s exceeds the 15s per-video limit
//
// 同一条请求走 16:9（不改派）本来是可以成功的 —— 这是路由造成的功能退化。
//
// 返回值：totalSeconds 是计费口径（四舍五入整秒）的合计，realSeconds 是真实合计
// （用于 15 秒总量校验）。**任一视频探测失败或整体超时都返回 error**：
// 调用方必须保守地放弃改派 —— 探测不出时长时无法判断是否会超限，
// 宁可走贵一点但能跑的源链路。
//
// ⚠️ 成本：这里会真的下载视频（moov 在尾部时要下全文件），而 wan3 adaptor 的
// validate 阶段**还会再探测一次**。之所以不复用结果：两边解析出的素材列表
// 必须完全一致才能共用，而 buildWan3Media 还要过 validateWan3Media（数量/组合
// 校验），一旦有偏差就会把计费秒数算错 —— 宁可多下一次，也不冒算错钱的风险。
func ProbeReferenceVideos(urls []string) (totalSeconds int, realSeconds float64, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), probeReferenceVideosTimeout)
	defer cancel()
	for _, raw := range urls {
		itemCtx, itemCancel := context.WithTimeout(ctx, probePerReferenceVideoTimeout)
		probed, probeErr := probeReferenceVideoSecondsContext(itemCtx, raw)
		itemCancel()
		if probeErr != nil {
			return 0, 0, probeErr
		}
		totalSeconds += probed.seconds
		realSeconds += probed.realSeconds
	}
	return totalSeconds, realSeconds, nil
}
