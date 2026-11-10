package controller

// ════════════════════════════════════════════════════════════════════════════
// 退役渠道类型的「建单闸门」契约
// ════════════════════════════════════════════════════════════════════════════
//
// 背景：2026-11-10 按老板指令清理「零上模型广场」的干扰渠道。
// MegaAI（type=64）是其中之一 —— 生产库里没有任何 type=64 的渠道，广场上也没有
// 挂在它下面的卡片，价格键早被摘干净（options 里没有任何含 megabyai 的键）。
//
// 为什么除了删注册分支，还要在「建渠道」这一层加闸门：
//
//	删掉 relay_adaptor.go 的注册分支后，type=64 依然会出现在**后台新建渠道的
//	类型下拉里**（前端 constants.ts 的 CHANNEL_TYPES 还有 64: 'MegaAI'）。
//	运营在后台新建一条 type=64 渠道 → 建单能过 → 任务提交时
//	relay.GetTaskAdaptor("64") 返回 nil → 任务永远停在非终态：既看不到失败原因，
//	也永远退不了款。客户侧表现为「钱扣了，任务不动」。
//
//	所以闸门要落在**建渠道这一刻**：把 type=64 明确拒绝掉，让问题在建渠道时就
//	暴露，而不是在客户提交任务时才炸。
//
// ⚠️ 闸门只针对**已退役**的类型，不是「禁建渠道」总开关 —— 在售渠道照常可建。
//
// 本文件把这条闸门变成可执行断言。

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/require"
)

// retiredChannelTypes 的定义在 controller/channel.go（实现侧），
// 本文件直接引用它 —— 避免测试与实现各写一份、改一处漏一处。
//
// 往里加编号时必须同时：
//  1. 在 relay/relay_adaptor.go 删掉对应注册分支；
//  2. 在 common/endpoint_type.go 删掉对应端点声明；
//  3. 在 controller/video_proxy.go 删掉对应取件分支；
//  4. 在 web/default/src/features/channels/constants.ts 删掉下拉项。

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：新建渠道时拒绝已退役类型
// ────────────────────────────────────────────────────────────────────────────

// TestValidateChannel_拒绝已退役渠道类型 是这条闸门的主断言。
func TestValidateChannel_拒绝已退役渠道类型(t *testing.T) {
	ch := &model.Channel{
		Type:    constant.ChannelTypeMegaAI,
		Key:     "sk-test-key",
		Name:    "MegaAI-残留",
		BaseURL: strPtr("https://newapi.megabyai.cc"),
		Models:  "wan-3.0",
		Status:  1,
	}

	err := validateChannel(ch, true)
	require.Error(t, err,
		"type=64（MegaAI）已于 2026-11-10 退役，新建渠道必须被拒绝 —— "+
			"否则会建出一条没有任务适配器承接的渠道，客户任务会永久卡在非终态")
	require.Contains(t, err.Error(), "64",
		"错误文案必须带上渠道类型编号，运营才知道是哪一条被拒")
}

// 更新已有渠道（isAdd=false）同样要拦 —— 否则可以把一条 type=64 的旧渠道
// 「改名后保存」重新激活。
func TestValidateChannel_更新时同样拒绝已退役渠道类型(t *testing.T) {
	ch := &model.Channel{
		Type:   constant.ChannelTypeMegaAI,
		Key:    "sk-test-key",
		Name:   "MegaAI-残留",
		Models: "wan-3.0",
	}

	err := validateChannel(ch, false)
	require.Error(t, err,
		"更新路径也必须拦已退役类型 —— 否则旧渠道可以改名保存重新激活")
}

// 对照组：在售渠道必须照常能建（证明闸门不是「禁建渠道」总开关）。
func TestValidateChannel_在售渠道照常通过(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeYoukou,
		constant.ChannelTypeYouzanWan3,
		constant.ChannelTypeManwu,
		constant.ChannelTypeXinbi,
		constant.ChannelTypeSanyuan,
	} {
		ch := &model.Channel{
			Type:    channelType,
			Key:     "sk-test-key",
			Name:    "in-stock",
			BaseURL: strPtr("https://example.com"),
			Models:  "wan-3.0-video",
		}
		require.NoError(t, validateChannel(ch, true),
			"在售渠道类型 %d 必须仍能正常创建 —— 退役闸门只针对已退役类型", channelType)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：退役名单与常量一致
// ────────────────────────────────────────────────────────────────────────────

// TestRetiredChannelTypes_编号不与在售渠道冲突 防止有人往退役名单里塞错编号。
func TestRetiredChannelTypes_编号不与在售渠道冲突(t *testing.T) {
	for channelType, name := range retiredChannelTypes {
		require.NotEqual(t, constant.ChannelTypeDummy, channelType,
			"退役名单里不能出现 ChannelTypeDummy（%d）", constant.ChannelTypeDummy)
		require.NotEmpty(t, name,
			"退役名单里的渠道类型必须有名字，错误文案要用", channelType)
	}
}

// strPtr 是本文件用的小工具（Channel.BaseURL 是 *string）。
func strPtr(s string) *string { return &s }
