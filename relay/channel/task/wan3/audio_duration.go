package wan3

import (
	"context"

	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/pkg/errors"
)

// validateReferenceAudioDurations 校验参考音频的**合计**时长上限。
//
// 官方《万相3.0-视频生成API参考》：reference_audio 最多 5 段，
// 单个 [1, 15] 秒，**总时长不大于 15 秒**。
//
// ⚠️ 这条限制在 constants.go 里早就写好了（maxReferenceAudioTotalSeconds），
// 但一直没有代码强制它 —— 常量注释当时写着「本站不探测音频时长，
// 超限只能由上游拒绝」。现在 taskcommon 有了 WAV/MP3 时长解析，补上这道闸门。
//
// 真实失败样本（tasks id=1258~1262，2026-11-02，渠道 17 有赞 Wan3）：
// 4 条音频实测 8.10/3.11/5.00/2.98 秒，单条都在 15 秒内，**累计 19.20 秒**，
// 上游 MEDIA_DURATION_INVALID 拒单。客户连续重试 5 次，每次白占余额、
// 等 1~3 分钟退款，而上游那句「参考素材时长不符合要求」没说清是哪条素材。
//
// 与视频那条的关键差别：
//   - **视频**探测失败必须 fail-loud —— 探测结果参与计费（生成秒数 + 视频总秒数），
//     测不出来就无法算钱，只能拒单；
//   - **音频**不参与计费，探测失败可以放行（客户的地址可能只对上游可达）。
func validateReferenceAudioDurations(media []relaycommon.TaskMedia) error {
	urls := make([]string, 0, len(media))
	for _, item := range media {
		if item.Type == "reference_audio" {
			urls = append(urls, item.URL)
		}
	}
	if len(urls) == 0 {
		return nil
	}

	client, err := service.GetHttpClientWithProxy("")
	if err != nil {
		return errors.Wrap(err, "new http client failed")
	}
	_, enforceErr := taskcommon.EnforceReferenceAudioMaxSeconds(
		context.Background(), urls, maxReferenceAudioTotalSeconds, client)
	if enforceErr != nil {
		return enforceErr
	}
	return nil
}
