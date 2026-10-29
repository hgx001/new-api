package wan3

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// 上游模型名兜底（2026-10-30 修）。
//
// 背景：渠道 12 的 model_mapping 原本只有 {"wan3.0-video-官网": "wan3.0-video"}，
// 新增对外名 wan3.0-720p-官网 时漏配了。BuildRequestBody 直接用
// info.UpstreamModelName（来自渠道映射），映射里没有这个键时它仍是对外名，
// 上游会收到「wan3.0-720p-官网」—— 一个它根本不认识的模型名，
// 表现为「用户付了钱、上游直接拒」。
//
// 修复：官网这两个对外名的上游语义是固定的（永远是标准版 wan3.0-video），
// 在代码里锁死，不依赖可配的渠道 model_mapping。

func TestUpstreamModelForMapsOfficialAliases(t *testing.T) {
	require.Equal(t, "wan3.0-video", upstreamModelFor(OfficialModelName))
	require.Equal(t, "wan3.0-video", upstreamModelFor(Official720PModelName))
}

func TestUpstreamModelForKeepsStandardModel(t *testing.T) {
	// 标准版本来就是上游认识的名字，不能被改写。
	require.Equal(t, "wan3.0-video", upstreamModelFor("wan3.0-video"))
	// 其它变体（prime 等）由渠道 model_mapping 负责，代码里不越权处理。
	require.Equal(t, "wan3.0-video-prime", upstreamModelFor("wan3.0-video-prime"))
}

func TestUpstreamModelForTrimsAndPassesThroughUnknown(t *testing.T) {
	require.Equal(t, "wan3.0-video", upstreamModelFor("  "+Official720PModelName+"  "))
	require.Equal(t, "some-other-model", upstreamModelFor("some-other-model"))
}

// 回归守卫：渠道映射漏配新模型名时，请求体里的 model 仍必须是上游认识的名字。
//
// 这里直接模拟 ModelMappedHelper 的行为（映射里没有该键 → UpstreamModelName 保持对外名），
// 再喂给 upstreamModelFor，验证兜底生效。
func TestUpstreamModelForSurvivesMissingChannelMapping(t *testing.T) {
	// 渠道 12 漏配时的实际状态
	channelMapping := map[string]string{"wan3.0-video-官网": "wan3.0-video"}

	// ModelMappedHelper：映射里没有这个键 → IsModelMapped 保持 false
	// → info.UpstreamModelName 仍是对外名
	upstreamModelName := Official720PModelName
	if mapped, ok := channelMapping[Official720PModelName]; ok && mapped != "" {
		upstreamModelName = mapped
	}
	require.Equal(t, Official720PModelName, upstreamModelName,
		"前提：渠道映射漏配时 UpstreamModelName 仍是对外名（这正是缺陷的触发条件）")

	// 兜底后必须是上游认识的名字
	require.Equal(t, "wan3.0-video", upstreamModelFor(upstreamModelName))
}
