package youzanwan3

import (
	"context"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/pkg/errors"
)

// 参考视频按秒加价：计费秒数 = 生成秒数 + 参考视频总秒数，再乘所选分辨率单价。
// 口径与 wan3.0-video-官网（relay/channel/task/wan3）一致 —— 不是另设一个
// multiplier，而是把参考视频的秒数当成「要付钱的秒数」加到一起。
//
// 为什么必须在 validate 阶段探测：
// EstimateBilling 没有错误返回值，解析失败时无法拒单。拿不到时长就会按纯生成
// 时长收费 —— 不报错、不少一单，只是每条带参考视频的单都少收钱。所以探测放在
// ValidateRequestAndSetAction（预扣之前）里，解析失败直接 400，不建单不扣费。
const refVideoSecondsContextKey = "youzanwan3_ref_video_seconds"

// durationEpsilon 保留为包内别名：解析与容差已统一到 taskcommon，这里只是让
// 本包的比较写法保持可读。
const durationEpsilon = taskcommon.DurationEpsilon

// referenceLimitError 标记「素材本身超出上游硬限制」。
//
// 为什么单独一个类型：同样是 400，两类错误的**文案归属**不同。
//   - 超限是客户端参数问题，文案本身就是写给客户看的中文提示，必须原样透传；
//   - 探测失败（下载不通、不是合法 MP4）是技术错误，需要技术前缀便于运维定位。
//
// 不区分的话，下游看到的会是
//
//	probe reference video duration failed: 参考视频时长 32.39 秒超过 15 秒上限
//
// 前半截是内部实现细节，客户读不懂，还容易误以为是我们这边故障。
type referenceLimitError struct{ error }

func (e *referenceLimitError) Unwrap() error { return e.error }

// newReferenceLimitError 造一个「素材超限」错误。
func newReferenceLimitError(format string, args ...any) error {
	return &referenceLimitError{errors.Errorf(format, args...)}
}

// isReferenceLimitError 判断 err 是否属于「素材超限」。
//
// 用 errors.As 而不是字符串匹配：文案会随需求改（比如把「秒」改成「秒钟」），
// 靠字面量判断早晚会失效。
func isReferenceLimitError(err error) bool {
	var target *referenceLimitError
	return errors.As(err, &target)
}

// mp4Seconds 保留为本包的薄封装：解析逻辑已统一到 taskcommon.ParseMP4Seconds，
// 单一实现，多一个名字而已。
func mp4Seconds(data []byte) (float64, bool) {
	return taskcommon.ParseMP4Seconds(data)
}

// probeReferenceVideoSeconds 探测单个参考视频的时长，返回四舍五入后的整秒。
//
// 支持 data URL 与公网 http(s) URL（与 readAsset 的上传路径一致）。
// 解析不出时长时返回错误（fail-loud）：宁可拒单，也不能按 0 秒收钱。
func probeReferenceVideoSeconds(rawURL, proxy string) (int, error) {
	client, err := getHTTPClient(proxy)
	if err != nil {
		return 0, errors.Wrap(err, "new http client failed")
	}
	seconds, err := probeReferenceVideoSecondsWithClient(context.Background(), rawURL, client)
	if err != nil {
		return 0, err
	}
	return taskcommon.RoundSecondsToBilling(seconds), nil
}

// probeReferenceVideoSecondsWithClient 是探测主体。client 由调用方注入，
// 便于单测替换成 httptest 服务器。
func probeReferenceVideoSecondsWithClient(ctx context.Context, rawURL string, client *http.Client) (float64, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return 0, errors.New("reference video url is required")
	}
	seconds, err := taskcommon.ProbeReferenceVideoDuration(ctx, rawURL, client)
	if err != nil {
		return 0, err
	}
	if seconds <= durationEpsilon {
		return 0, errors.Errorf("reference video duration %.3fs is not billable", seconds)
	}
	// 上游硬限制：单条参考视频最长 15 秒（2026-10-31 实测原文：
	// 「参考素材时长过长：当前为 31.609 秒，最长允许 15 秒」）。
	//
	// 必须本地拦：不拦的话请求会一路走到「建单 → 预扣费 → 上游拒绝 → 退款」，
	// 客户白占一次余额、等几分钟退款，还看不出该怎么改。本地 400 是
	// TaskErrorWrapperLocal，既不扣费也不触发渠道 AutoBan。
	if seconds > float64(maxReferenceVideoSeconds)+durationEpsilon {
		return 0, newReferenceLimitError("参考视频时长 %.2f 秒超过 %d 秒上限，请裁剪后重试",
			seconds, maxReferenceVideoSeconds)
	}
	return seconds, nil
}
