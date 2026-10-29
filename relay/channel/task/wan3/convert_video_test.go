package wan3

import (
	"encoding/json"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"

	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// ConvertToOpenAIVideo 的成片地址口径
//
// 背景：wan3.0-720p-官网 是**两跳**链路（官网 480P → 云端提升 → 交付 720P）。
// 上游回执里的 VideoURL 永远是 **480P 原片**。
//
// 如果这里把上游直链给出去，用户付了 720P 的钱、拿到的却是 480P ——
// 而且客户端从 metadata.url 直连的是**错档产物**，连本站 /content 代理都绕过了。
// ─────────────────────────────────────────────────────────────────────────────

// upstream480PReceipt 是上游（官网）返回的 480P 成片回执。
const upstream480PReceipt = `{"request_id":"req-1","output":{"task_status":"SUCCEEDED","video_url":"https://wan3-oss.example.com/result/20261029/task-abc.mp4?sign=xyz"}}`

// TestConvertToOpenAIVideoGivesProxyURLForTwoHopTask 锁定两跳链路的地址口径。
//
// 成功后给「本站 /content 代理地址」，而不是上游 480P 直链：
//   · /content 代理会解析库里的占位符 → 302 到 720P 产物的临时直链；
//   · 上游直链是 480P，给出去就是交付错档产物。
func TestConvertToOpenAIVideoGivesProxyURLForTwoHopTask(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID:   "task_abc",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
		PrivateData: model.TaskPrivateData{
			// 库里存的是内部占位符（成片提升成功后写入）。
			ResultURL: taskcommon.BuildCOSProxyMarker(
				"my-bucket-1250000000", "ap-guangzhou", "mps_api_test/out/task-abc.mp4"),
		},
	}
	task.Data = json.RawMessage(upstream480PReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))

	url, ok := video.Metadata["url"].(string)
	require.True(t, ok, "成功任务必须给出下载地址")
	require.Equal(t, taskcommon.BuildProxyURL(task.TaskID), url,
		"两跳链路必须给本站代理地址，不能给上游 480P 直链")
	require.NotContains(t, url, "wan3-oss.example.com", "上游直链不能漏给客户端")
	require.NotContains(t, url, "cos-proxy://", "内部标记不能漏给客户端")
}

// TestConvertToOpenAIVideoKeepsDirectLinkForSingleHop 锁定单跳链路直链原样保留。
//
// 标准版 wan3.0-video 的成片在上游 OSS 签名直链上，客户端直连最快。
// 换成本站代理反而多经一次本机 3Mbps 出口（30MB 要 87 秒）。
func TestConvertToOpenAIVideoKeepsDirectLinkForSingleHop(t *testing.T) {
	adaptor := &TaskAdaptor{}
	direct := "https://wan3-oss.example.com/result/20261029/task-xyz.mp4?sign=xyz"
	task := &model.Task{
		TaskID:   "task_xyz",
		Status:   model.TaskStatusSuccess,
		Progress: "100%",
		Properties: model.Properties{
			OriginModelName: "wan3.0-video",
		},
		PrivateData: model.TaskPrivateData{ResultURL: direct},
	}
	task.Data = json.RawMessage(upstream480PReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	require.Equal(t, direct, video.Metadata["url"],
		"单跳链路的直链应原样保留，客户端直连最快")
}

// TestConvertToOpenAIVideoGivesNoURLBeforeSuccess 锁定「未完成不给地址」。
//
// 未完成时上游回执里的 VideoURL 多半还是空的；就算有（历史数据），
// 两跳链路下它也只会是 480P —— 未完成的客户端应继续轮询。
func TestConvertToOpenAIVideoGivesNoURLBeforeSuccess(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_pending",
		Status: model.TaskStatusInProgress,
		Properties: model.Properties{
			OriginModelName: Official720PModelName,
		},
	}
	task.Data = json.RawMessage(upstream480PReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	require.NotContains(t, video.Metadata, "url",
		"任务没完成就不能给下载地址，否则客户端会去拉一个半成品")
}

// TestConvertToOpenAIVideoFallsBackToUpstreamWhenNoStoredURL 锁定历史数据兜底。
//
// 老任务（改造前落库）没有占位符也没有 ResultURL，但回执里有直链 ——
// 这种情况要能给出地址，不能让历史任务变成「下载不到」。
func TestConvertToOpenAIVideoFallsBackToUpstreamWhenNoStoredURL(t *testing.T) {
	adaptor := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_legacy",
		Status: model.TaskStatusSuccess,
		Properties: model.Properties{
			OriginModelName: "wan3.0-video",
		},
	}
	task.Data = json.RawMessage(upstream480PReceipt)

	raw, err := adaptor.ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(raw, &video))
	url, ok := video.Metadata["url"].(string)
	require.True(t, ok)
	require.Contains(t, url, "wan3-oss.example.com",
		"历史任务要能从回执里取到直链")
}
