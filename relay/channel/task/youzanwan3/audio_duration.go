package youzanwan3

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// refAudioSecondsContextKey 记录本次请求探测到的参考音频总秒数。
//
// ⚠️ 与参考视频不同：**参考音频不参与计费加价**。
// 计费秒数 = 生成秒数 + 参考视频总秒数（见 video_duration.go 与上游 usage.duration），
// 音频只是「给谁配音」的素材，不额外收钱。所以这里**不写**计费用的秒数，
// 只把「实测总时长」记下来供日志与错误文案使用。
const refAudioSecondsContextKey = "youzanwan3_ref_audio_seconds"

// setReferenceAudioSeconds 探测所有 reference_audio 素材的时长，累加后校验上限。
//
// 与 setReferenceVideoSeconds 的三处差别：
//  1. **不写计费用的秒数**——音频不参与加价，只记实测值供日志用；
//  2. **上限是累计值**——上游约束是「总时长 ≤15 秒」，不是「每条各 15 秒」；
//  3. **探测失败放行**——与视频那条相反（视频的探测结果参与计费，测不出来
//     就无法算钱，只能 fail-loud；音频不参与计费，测不出来可以先放过）。
func (a *TaskAdaptor) setReferenceAudioSeconds(c *gin.Context, media []relaycommon.TaskMedia) error {
	urls := make([]string, 0, len(media))
	for _, item := range media {
		if item.Type == "reference_audio" {
			urls = append(urls, item.URL)
		}
	}
	if len(urls) == 0 {
		return nil
	}

	client, err := getHTTPClient(a.proxy)
	if err != nil {
		return errors.Wrap(err, "new http client failed")
	}
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}

	unmeasured, enforceErr := taskcommon.EnforceReferenceAudioMaxSeconds(
		ctx, urls, maxTotalReferenceAudioSeconds, client)
	if enforceErr != nil {
		// 只有「实测超限」才是素材超限；其余（地址为空等）是参数/探测问题。
		// 两类错误的文案归属不同：超限要原样透传给客户看，
		// 其它保留技术信息便于运维定位。
		var limitErr *taskcommon.ReferenceAudioLimitError
		if errors.As(enforceErr, &limitErr) {
			return newReferenceLimitError("%s", limitErr.Error())
		}
		return enforceErr
	}
	if len(unmeasured) > 0 {
		// 探测不到的素材记日志但放行：客户的地址可能只对上游可达
		// （IP 白名单、私有签名），我们抓不到不代表它超长。
		logger.LogWarn(ctx, fmt.Sprintf(
			"youzanwan3: 未能探测参考音频时长，已放行: %s", strings.Join(unmeasured, ", ")))
	}
	return nil
}
