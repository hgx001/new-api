package megabyai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/require"
)

// seedance2.5-特惠 是 wan-3.0 的对外别名（2026-10-22 新增）：
//
//   - 参数传递**完全一致**（同一套上游统一参数契约）
//   - 计价是**固定 ¥5.0/条**（不分分辨率档、不乘时长）
//   - 上游 model 名恒为 wan-3.0（别名绝不能原样发给上游）

// 别名与 wan-3.0 的上游载荷必须逐字段一致，只有 model 名不同。
func TestSeedance25TeHuiSharesParameterContractWithWan30(t *testing.T) {
	raw := `{"model":"seedance2.5-特惠","prompt":"海边日落","duration":30,"ratio":"16:9",` +
		`"resolution":"720p","images":["https://e.com/a.png"],"seed":42}`

	alias := runChainWithModel(t, raw, Seedance25TeHuiModel, "")
	require.Nil(t, alias.taskErr)

	base := runChainWithModel(t, raw, wan30Model, wan30Model)
	require.Nil(t, base.taskErr)

	// model 是唯一允许不同的字段：别名必须下发上游真名 wan-3.0。
	require.Equal(t, "wan-3.0", alias.body["model"], "别名不能原样发给上游")
	require.Equal(t, base.body["prompt"], alias.body["prompt"])
	require.Equal(t, base.body["duration"], alias.body["duration"])
	require.Equal(t, base.body["ratio"], alias.body["ratio"])
	require.Equal(t, base.body["resolution"], alias.body["resolution"])
	require.Equal(t, base.body["referenceImages"], alias.body["referenceImages"])
	require.Equal(t, base.body["seed"], alias.body["seed"])

	// 别名不得混入经典字段（上游文档明确不要混用）
	for _, forbidden := range []string{"seconds", "size", "images", "input_reference", "media"} {
		require.NotContains(t, alias.body, forbidden)
	}
}

// 固定价：任意分辨率、任意时长都是同一个价，且不叠加任何倍率。
func TestSeedance25TeHuiIsFixedPerCallPrice(t *testing.T) {
	for _, resolution := range []string{"480p", "720p", "1080p"} {
		raw := `{"model":"seedance2.5-特惠","prompt":"p","duration":30,"resolution":"` + resolution + `"}`
		result := runChainWithModel(t, raw, Seedance25TeHuiModel, "")
		require.Nil(t, result.taskErr)
		require.Equal(t, resolution, result.body["resolution"], "分辨率仍要原样下发给上游")
		require.Nil(t, result.ratios, "固定按次价：不得叠加分辨率档倍率")
	}

	// 时长不参与计价：30 秒与 5 秒同价。
	short := runChainWithModel(t, `{"model":"seedance2.5-特惠","prompt":"p","duration":5}`, Seedance25TeHuiModel, "")
	long := runChainWithModel(t, `{"model":"seedance2.5-特惠","prompt":"p","duration":30}`, Seedance25TeHuiModel, "")
	require.Nil(t, short.taskErr)
	require.Nil(t, long.taskErr)
	require.Nil(t, short.ratios)
	require.Nil(t, long.ratios)
}

// 对外价锁死：¥5.0/条。改价会红：改价要同时改 defaultModelPrice、
// 本测试的期望值和生产 ModelPrice option。
func TestSeedance25TeHuiPrice(t *testing.T) {
	basePrice, ok := ratio_setting.GetDefaultModelPriceMap()[Seedance25TeHuiModel]
	require.True(t, ok, "seedance2.5-特惠 必须配置按次单价")
	quota := int(basePrice * common.QuotaPerUnit)
	actualCny := float64(quota) / common.QuotaPerUnit * ratio_setting.USD2RMB
	require.InDelta(t, 5.0, actualCny, 0.0001)
}

// 上游 model 名的解析顺序：渠道 mapping 优先，其次兜底表，最后 wan-3.0。
func TestResolveMegabyaiUpstreamModel(t *testing.T) {
	// 渠道漏配 mapping：靠兜底表，别名绝不能原样发给上游。
	require.Equal(t, "wan-3.0", resolveMegabyaiUpstreamModel(&relaycommon.RelayInfo{
		OriginModelName: Seedance25TeHuiModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: Seedance25TeHuiModel},
	}))
	// 渠道配了 mapping：以 mapping 为准。
	require.Equal(t, "wan-3.0", resolveMegabyaiUpstreamModel(&relaycommon.RelayInfo{
		OriginModelName: Seedance25TeHuiModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "wan-3.0"},
	}))
	// 映射到别的上游：尊重渠道配置。
	require.Equal(t, "wan-3.0-turbo", resolveMegabyaiUpstreamModel(&relaycommon.RelayInfo{
		OriginModelName: Seedance25TeHuiModel,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: "wan-3.0-turbo"},
	}))
	// wan-3.0 本身不变。
	require.Equal(t, "wan-3.0", resolveMegabyaiUpstreamModel(&relaycommon.RelayInfo{
		OriginModelName: wan30Model,
		ChannelMeta:     &relaycommon.ChannelMeta{UpstreamModelName: wan30Model},
	}))
}

// 别名必须走统一参数契约，sd-2.5 仍走 Sora 透传（不动既有路径）。
func TestUnifiedContractModelSet(t *testing.T) {
	require.True(t, megabyaiUnifiedContractModels[wan30Model])
	require.True(t, megabyaiUnifiedContractModels[Seedance25TeHuiModel])
	require.False(t, megabyaiUnifiedContractModels["sd-2.5"])
	// 相近的 seedance 名字不能被误判进来（站内有很多 seedance-* 模型）。
	for _, other := range []string{"seedance-2-5", "seedance-2.5", "seedance2.5特惠版", "seedance-2-0-fast"} {
		require.False(t, megabyaiUnifiedContractModels[other], other)
	}
	// 只有 wan-3.0 按分辨率档计价。
	require.True(t, megabyaiResolutionTierModels[wan30Model])
	require.False(t, megabyaiResolutionTierModels[Seedance25TeHuiModel])
}
