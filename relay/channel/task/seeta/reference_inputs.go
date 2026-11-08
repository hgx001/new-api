package seeta

import (
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/pkg/errors"
)

// validateOmniReferenceInputs 在**预扣费之前**拦掉上游注定会拒的素材组合。
//
// 上游 H3 网关 derive_video_kind() 按请求内容派生语义（2026-11-08 真机验证）：
//   - 出现 ref_image_N / ref_video_N / ref_audio_N 任一 → multi_reference 档，
//     该档强制至少一张设定参考图；
//   - 只有 first_frame / last_frame → first_last 档；
//   - 全空 → text 档（纯文生视频，允许 0 张图）。
//
// 网关把 images 转成 ref_image_N、media 里的视频/音频转成
// ref_video_N / ref_audio_N、metadata 首尾帧转成 first_frame / last_frame。
// 所以「带参考视频或参考音频却没有设定参考图」必然落到 multi_reference 且缺图，
// 上游会在提交阶段 400，并把内部工作流 ID 写进报错文案。本地先拦：
// 客户不必白等一次上游往返，也不会出现「先预扣、再退款」的窗口。
//
// 只对 Omni（MaxVideos > 0）生效：V2 / 旧别名走自己的关键帧规则。
func validateOmniReferenceInputs(req relaycommon.TaskSubmitReq, modelName string) *dto.TaskError {
	videos := collectReferenceVideos(req)
	audios := collectReferenceAudios(req)
	images := collectReferenceImages(req)
	if (len(videos) > 0 || len(audios) > 0) && len(images) == 0 {
		return service.TaskErrorWrapperLocal(
			errors.Errorf(
				"模型 %s 带参考视频/参考音频时属于多参考档，必须同时提供至少一张设定参考图（images 或 media type=reference_image）；纯文生视频或只用首尾帧时请不要带参考视频/音频",
				modelName),
			"invalid_reference_media", http.StatusBadRequest)
	}

	// 上游首尾帧锚定要求首帧先给（multi_reference 与 first_last 两条分支都拒）。
	// 只有尾帧的请求在上游是 400，本地提前拦，避免白扣一次预扣费。
	if metadataString(req, "last_frame") != "" && metadataString(req, "first_frame") == "" {
		return service.TaskErrorWrapperLocal(
			errors.New("只有 metadata.last_frame 没有 metadata.first_frame：首尾帧锚定要求先给首帧"),
			"invalid_first_frame", http.StatusBadRequest)
	}
	return nil
}
