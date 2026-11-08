package seeta

import (
	"context"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// refVideoSecondsContextKey 保存 validate 阶段探测出的参考视频计费总秒数。
// EstimateBilling 读它把秒数并入计费（生成秒数 + 参考视频总秒数），与 wan3 同口径。
const refVideoSecondsContextKey = "seeta_ref_video_seconds"

// 探测预算。service.GetHttpClient 在 RELAY_TIMEOUT=0（生产默认）时不设 Timeout，
// 一个「连得上但一直不吐数据」的地址会让 Body.Read 无限阻塞。分两级而不是一个
// 总预算：Omni 最多 3 段参考视频，共用一份预算会让最后一段几乎没时间可用。
const (
	probeRefVideoTimeout       = 10 * time.Second
	probeRefVideosTotalTimeout = 30 * time.Second
)

// probeReferenceVideosSeconds 探测一组参考视频的计费总秒数：逐条实测时长、
// 四舍五入到整秒后相加（与 taskcommon.RoundSecondsToBilling / wan3 一致）。
//
// ⚠️ 计费依赖这个结果，所以解析不出时长必须 fail-loud 返回 error：
// 悄悄按 0 秒放行等于少收钱，而 EstimateBilling 没有错误返回值、无法在计费
// 阶段再拒单。调用方必须在 validate 阶段（预扣费之前）转成 400 本地错误。
//
// ⚠️ 这里会真的下载 http(s) 素材（BuildRequestBody 转 data URI 时还会再下一次）。
// 之所以不复用字节：两条路径的素材解析与校验必须完全一致才能共用，稍有偏差
// 就会把钱算错 —— 宁可多下一次，也不冒算错钱的风险（与 wan3 的取舍相同）。
func probeReferenceVideosSeconds(c *gin.Context, urls []string) (int, error) {
	if len(urls) == 0 {
		return 0, nil
	}

	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	ctx, cancel := context.WithTimeout(ctx, probeRefVideosTotalTimeout)
	defer cancel()

	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return 0, errors.Wrap(err, "new http client failed")
	}
	if client == nil {
		client = http.DefaultClient
	}

	total := 0
	for i, raw := range urls {
		itemCtx, itemCancel := context.WithTimeout(ctx, probeRefVideoTimeout)
		seconds, probeErr := taskcommon.ProbeReferenceVideoDuration(itemCtx, raw, client)
		itemCancel()
		if probeErr != nil {
			// 底层 http 错误会带完整 URL（可能含签名参数），不能原样回给客户或写进日志。
			// 素材地址统一走 RedactMaterialURL 压成摘要，错误文本再过一次统一脱敏。
			return 0, errors.Errorf(
				"reference video #%d (%s) duration unavailable for per-second billing (readable MP4/MOV required): %s",
				i+1, taskcommon.RedactMaterialURL(raw), common.MaskSensitiveInfo(probeErr.Error()))
		}
		total += taskcommon.RoundSecondsToBilling(seconds)
	}
	return total, nil
}
