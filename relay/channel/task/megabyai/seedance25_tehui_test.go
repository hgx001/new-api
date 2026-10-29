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
//   - 上游 model 名恒为 wan-3.0（别名绝不能原样发给上游）
//
// ⚠️ 计费口径 2026-10-29 变过一次：这个模型已迁到**有赞 Wan3** 渠道，
// 计价从「固定 ¥3.0/条」改成「**按秒** ¥0.20/秒（720P）/ ¥0.30/秒（1080P）」。
// 本文件里与「档位门控」相关的断言（只出 720p、只出 30 秒）仍适用于 MegaAI 渠道，
// 但**价格断言已按新口径更新**（见 TestSeedance25TeHuiPrice）。

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

// 别名模型的**本地报错**不能点名上游：文案写死 "wan-3.0" 等于把真实上游
// 告诉下游（脱敏层只能把整句换成误导性的「上游 accepts...」）。
// 这类上限是本站的限制，说 "this model" 对两个对外名都成立。
func TestSeedance25TeHuiErrorMessagesNeverNameUpstream(t *testing.T) {
	tooManyImages := `{"model":"seedance2.5-特惠","prompt":"p","images":[` +
		`"https://e.com/1.png","https://e.com/2.png","https://e.com/3.png","https://e.com/4.png","https://e.com/5.png",` +
		`"https://e.com/6.png","https://e.com/7.png","https://e.com/8.png","https://e.com/9.png","https://e.com/10.png",` +
		`"https://e.com/11.png"]}`

	cases := []struct {
		name string
		raw  string
	}{
		{"参考图超上限", tooManyImages},
		{"非 720p", `{"model":"seedance2.5-特惠","prompt":"p","resolution":"1080p"}`},
		{"非 30 秒", `{"model":"seedance2.5-特惠","prompt":"p","duration":5}`},
		{"data URI 素材", `{"model":"seedance2.5-特惠","prompt":"p","images":["data:image/png;base64,AAAA"]}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := runChainWithModel(t, tc.raw, Seedance25TeHuiModel, "")
			require.NotNil(t, result.taskErr)
			require.NotContains(t, result.taskErr.Message, "wan-3.0", "别名模型的报错不得点名上游模型")
			require.NotContains(t, result.taskErr.Message, "wan3")
			require.NotEmpty(t, result.taskErr.Message, "报错文案要能指导用户怎么改")
		})
	}
}

// 固定价：只认 720p / 30 秒这一档，且不叠加任何倍率。
func TestSeedance25TeHuiIsFixedPerCallPrice(t *testing.T) {
	// 唯一放行的档位：720p / 30 秒，倍率表为空（固定价）
	allowed := runChainWithModel(t,
		`{"model":"seedance2.5-特惠","prompt":"p","duration":30,"resolution":"720p"}`, Seedance25TeHuiModel, "")
	require.Nil(t, allowed.taskErr)
	require.Equal(t, "720p", allowed.body["resolution"])
	require.Equal(t, float64(30), allowed.body["duration"])
	require.Nil(t, allowed.ratios, "固定按次价：不得叠加分辨率档倍率")

	// 其余分辨率一律 400（能力门控，不静默改档）
	for _, resolution := range []string{"480p", "1080p"} {
		raw := `{"model":"seedance2.5-特惠","prompt":"p","resolution":"` + resolution + `"}`
		result := runChainWithModel(t, raw, Seedance25TeHuiModel, "")
		require.NotNil(t, result.taskErr, resolution)
		require.Equal(t, "invalid_resolution", result.taskErr.Code, resolution)
	}
}

// 对外价锁死：¥0.20/秒（基准档 720P）。
//
// ⚠️ 2026-10-29 起 seedance2.5-特惠 已迁到有赞 Wan3 渠道，**按秒**计费：
// 720P ¥0.20/秒、1080P ¥0.30/秒（1080P 的 1.5 倍率由 youzanwan3 的
// tehuiResolutionSizeRatio 乘上去，与 prime 的 1.2 不同 —— 两处不要互相抄）。
//
// 这里断言的是 **defaultModelPrice 的兜底值**（生产以 options 的 ModelPrice 为准）：
// 它必须与有赞渠道的口径一致，否则一旦生产那个键丢了，
// 模型会退回「¥3.0/条、不乘时长」的老口径 —— 那等于静默算错价。
//
// 迁移前这里是 ¥3.0/条（按次），与现在的按秒口径不可比，改价会红：
// 改价要同时改 defaultModelPrice、有赞渠道的 ModelPrice 和生产 options。
func TestSeedance25TeHuiPrice(t *testing.T) {
	basePrice, ok := ratio_setting.GetDefaultModelPriceMap()[Seedance25TeHuiModel]
	require.True(t, ok, "seedance2.5-特惠 必须配置按秒单价")
	quota := int(basePrice * common.QuotaPerUnit)
	actualCny := float64(quota) / common.QuotaPerUnit * ratio_setting.USD2RMB
	require.InDelta(t, 0.20, actualCny, 0.0001,
		"特惠的兜底价必须是 ¥0.20/秒（基准档 720P），与有赞渠道口径一致")
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

// 锁定档位：只出 720p、只出 30 秒。
//
// 客户端给别的档位必须 400（fail-loud），不能静默改档：
// 静默降档 = 用户付了 720p 的钱拿到 480p；静默拉时长 = 客户端以为省了钱。
func TestSeedance25TeHuiLocksResolutionAndDuration(t *testing.T) {
	// 没给分辨率 / 时长 → 用锁定档位并**显式下发**给上游
	defaults := runChainWithModel(t, `{"model":"seedance2.5-特惠","prompt":"p"}`, Seedance25TeHuiModel, "")
	require.Nil(t, defaults.taskErr)
	require.Equal(t, "720p", defaults.body["resolution"])
	require.Equal(t, float64(30), defaults.body["duration"])

	// 正好 720p / 30 秒 → 放行
	allowed := runChainWithModel(t,
		`{"model":"seedance2.5-特惠","prompt":"p","resolution":"720p","duration":30}`, Seedance25TeHuiModel, "")
	require.Nil(t, allowed.taskErr)
	require.Equal(t, "720p", allowed.body["resolution"])
	require.Equal(t, float64(30), allowed.body["duration"])

	// 别的分辨率 → 400 invalid_resolution
	for _, raw := range []string{
		`{"model":"seedance2.5-特惠","prompt":"p","resolution":"480p"}`,
		`{"model":"seedance2.5-特惠","prompt":"p","resolution":"1080p"}`,
		`{"model":"seedance2.5-特惠","prompt":"p","size":"1920x1080"}`,
		`{"model":"seedance2.5-特惠","prompt":"p","size":"854x480"}`,
		`{"model":"seedance2.5-特惠","prompt":"p","metadata":{"resolution":"1080p"}}`,
	} {
		bad := runChainWithModel(t, raw, Seedance25TeHuiModel, "")
		require.NotNil(t, bad.taskErr, raw)
		require.Equal(t, "invalid_resolution", bad.taskErr.Code, raw)
	}

	// 别的时长 → 400 invalid_duration
	for _, raw := range []string{
		`{"model":"seedance2.5-特惠","prompt":"p","duration":5}`,
		`{"model":"seedance2.5-特惠","prompt":"p","duration":15}`,
		`{"model":"seedance2.5-特惠","prompt":"p","seconds":"10"}`,
		`{"model":"seedance2.5-特惠","prompt":"p","duration":30,"seconds":"5"}`,
	} {
		bad := runChainWithModel(t, raw, Seedance25TeHuiModel, "")
		require.NotNil(t, bad.taskErr, raw)
		require.Equal(t, "invalid_duration", bad.taskErr.Code, raw)
	}
}

// 锁定档位只作用于别名模型：wan-3.0 仍然支持三档分辨率与多档时长。
func TestWan30KeepsUnlockedTiersAndDurations(t *testing.T) {
	for _, raw := range []string{
		`{"model":"wan-3.0","prompt":"p","resolution":"480p"}`,
		`{"model":"wan-3.0","prompt":"p","resolution":"1080p"}`,
		`{"model":"wan-3.0","prompt":"p","duration":5}`,
		`{"model":"wan-3.0","prompt":"p","duration":15}`,
		`{"model":"wan-3.0","prompt":"p","seconds":"10"}`,
	} {
		result := runChainWithModel(t, raw, wan30Model, wan30Model)
		require.Nil(t, result.taskErr, raw)
	}
}
