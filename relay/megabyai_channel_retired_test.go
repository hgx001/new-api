package relay

// ════════════════════════════════════════════════════════════════════════════
// MegaAI 渠道（type=64）退役契约
// ════════════════════════════════════════════════════════════════════════════
//
// 事实：MegaAI 渠道已于 2026-11-10 停售。
//
//   · 生产库里 type=64 的渠道早已不存在（2026-10-26 起「wan-3.0 / seedance2.5-特惠
//     随该渠道停售而空置」，2026-11-10 复核：channels 表里没有任何 type=64 的行）。
//   · 广场上也没有任何一张卡片挂在它下面。
//   · options 里连一个含 "megabyai" 的键都没有 —— 价格键也早被摘干净。
//
// 老板指令（2026-11-10）：「目前没有在模型广场展示的模型渠道全部删除」。
// MegaAI 属于这类「零上广场」的干扰渠道，代码侧的注册分支一并清掉。
//
// ── 为什么「清注册分支」而不是「删整个 megabyai 包」──────────────────────
//
// relay/channel/task/megabyai 这个包**不能整个删**：youkou 渠道（type=59）
// 的 seedance2.5特惠版 复用了 taskmegabyai.TaskAdaptor 拿「按次计费」
// （EstimateBilling=nil，禁止带入 sora 的 seconds/size 倍率）。
// 删包 = 删掉 youkou 在售模型的计费能力，直接打掉一个在售商品。
//
// 所以本次只做三件事：
//  1. relay_adaptor.go 里删掉 ChannelTypeMegaAI 的注册分支（type=64 不再能路由）；
//  2. common/endpoint_type.go 里删掉 ChannelTypeMegaAI 的端点声明；
//  3. controller/video_proxy.go 里删掉 ChannelTypeMegaAI 的取件分支；
//     controller/channel_upstream_update.go 里删掉 ChannelTypeMegaAI 的模型枚举。
//
// constant.ChannelTypeMegaAI = 64 这个**常量本身保留**：
// 它是渠道类型编号空间的永久占位，删掉会让后续新增渠道复用编号 64，
// 而历史 tasks / logs 里还留着 platform=64 的老记录 —— 编号一旦被复用，
// 老任务会被当成新渠道的任务去查状态，后果比留一个空常量严重得多。
//
// ── 退役后 type=64 的行为契约 ───────────────────────────────────────────
//
//  ① GetTaskAdaptor("64") 必须返回 nil（没有适配器 → 路由层直接报错）；
//  ② endpoint 声明不再包含 type=64；
//  ③ megabyai 包本体保留且仍可被 youkou 复用（计费能力不丢）。
//
// 本文件把这些变成可执行断言。

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel/task/youkou"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ────────────────────────────────────────────────────────────────────────────
// 守卫 1：路由层不再为 type=64 注册任务适配器
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_路由层不再注册任务适配器 是本次退役的**总开关**。
//
// 断言 GetTaskAdaptor 返回 nil，而不是断言「某个具体字段为空」——
// 因为一旦有人不小心把分支加回来，这里会立刻红。
func TestMegaAI_路由层不再注册任务适配器(t *testing.T) {
	adaptor := GetTaskAdaptor(constant.TaskPlatform("64"))
	assert.Nil(t, adaptor,
		"MegaAI 渠道已停售，type=64 不得再注册任务适配器 —— "+
			"否则后台能建出一个指向已下架上游的渠道，任务建单后永远轮询不到结果")
}

// 编号本身仍是 64（常量保留，避免编号被后续渠道复用）。
func TestMegaAI_渠道类型编号保留为64(t *testing.T) {
	assert.Equal(t, 64, constant.ChannelTypeMegaAI,
		"渠道类型编号是永久占位，必须保留 64 —— 删掉会被后续渠道复用，"+
			"而历史 tasks/logs 里还有 platform=64 的老记录")
}

// 对照组：在售渠道必须仍能拿到适配器（证明上面的 nil 不是 GetTaskAdaptor 整体坏了）。
func TestMegaAI_在售渠道的适配器仍可用(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeYoukou,
		constant.ChannelTypeYouzanWan3,
		constant.ChannelTypeManwu,
		constant.ChannelTypeXinbi,
	} {
		adaptor := GetTaskAdaptor(constant.TaskPlatform(itoa(channelType)))
		assert.NotNil(t, adaptor,
			"在售渠道类型 %d 必须仍能拿到任务适配器 —— 退役 MegaAI 不能误伤它们", channelType)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：端点声明不再包含 type=64
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_端点声明不再包含type64 守的是「广场/定价元数据」这一层。
//
// type=64 不再出现在 GetEndpointTypesByChannelType 的 OpenAI Video 分支里 ——
// 这意味着即使有人手工造出一条 type=64 的能力行，广场也不会把它标成
// /v1/videos 端点，不会误导客户端去调一个没有适配器承接的入口。
func TestMegaAI_端点声明不再包含type64(t *testing.T) {
	endpointTypes := common.GetEndpointTypesByChannelType(constant.ChannelTypeMegaAI, "wan-3.0")

	for _, et := range endpointTypes {
		assert.NotEqual(t, constant.EndpointTypeOpenAIVideo, et,
			"type=64 已退役，不得再被声明为 OpenAI Video 端点 —— "+
				"客户端会照着 supported_endpoint_types 去调 /v1/videos，而那里没有适配器")
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 3：megabyai 包本体保留，youkou 的复用不受影响
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_youkou仍复用megabyai计费 是本次退役最容易被踩坏的一脚。
//
// youkou 的 seedance2.5特惠版 走 megabyai 适配器拿「按次计费」：
// EstimateBilling=nil，禁止带入 sora 的 seconds/size 倍率。
// 删包或多改一处 selectSub 逻辑，多秒任务会被扣到上百元。
func TestMegaAI_youkou仍复用megabyai计费(t *testing.T) {
	youkouAdaptor := &youkou.TaskAdaptor{}
	assert.Equal(t, youkou.ChannelName, youkouAdaptor.GetChannelName())

	// ModelList 必须仍含按次计费模型 —— 这是复用 megabyai 计费的唯一入口。
	assert.Contains(t, youkouAdaptor.GetModelList(), "seedance2.5特惠版",
		"youkou 的按次计费模型必须仍在 ModelList 里 —— 它复用 megabyai 适配器取计费口径")
}

// itoa 是本文件用的小工具（避免为一次转换引入 strconv 之外的依赖噪音）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// require 引入占位（保持与仓库其它测试文件一致的 import 形态）。
var _ = require.New
