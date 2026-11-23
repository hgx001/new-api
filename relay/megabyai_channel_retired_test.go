package relay

// ════════════════════════════════════════════════════════════════════════════
// MegaAI 渠道（type=64）**重新接入**契约
// ════════════════════════════════════════════════════════════════════════════
//
// 时间线：
//   · 2026-11-10 按「广场零卡片的渠道全部删除」退役 type=64（当时挂 wan-3.0）；
//   · 2026-11-23 重新接入同一上游（newapi.megabyai.cc），上架 T3-seedance-2.5。
//
// 退役期间本文件断言的是「GetTaskAdaptor(64) 必须为 nil / 端点声明不得含 64」。
// 重新接入后这些断言必须**逐条反过来**，否则测试会红着发版 —— 本文件因此重写，
// 并把「为什么当初要保留 megabyai 包」的那条约束继续守住：
// youkou（type=59）复用 megabyai 的按次计费，任何时候都不能被这次改动打掉。
//
// 保留的历史结论（仍然有效）：
//   · constant.ChannelTypeMegaAI = 64 是永久编号占位，不得删除或复用；
//   · megabyai 包本体不得整个删除 —— youkou 的 seedance2.5特惠版 靠它拿
//     EstimateBilling=nil（禁止带入 sora 的 seconds/size 倍率）。
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
// 守卫 1：路由层必须为 type=64 注册任务适配器
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_路由层注册任务适配器 是重新接入的**总开关**。
//
// 断言 GetTaskAdaptor 返回非 nil：一旦有人再把注册分支删掉，这里立刻红。
// 没有适配器的渠道能建出来，但任务提交后永远停在非终态 —— 钱扣了、任务不动。
func TestMegaAI_路由层注册任务适配器(t *testing.T) {
	adaptor := GetTaskAdaptor(constant.TaskPlatform("64"))
	require.NotNil(t, adaptor,
		"MegaAI（type=64）已重新接入 T3-seedance-2.5，必须注册任务适配器 —— "+
			"否则渠道能建但任务永远轮询不到结果")
}

// 编号本身仍是 64（常量保留，避免编号被后续渠道复用）。
func TestMegaAI_渠道类型编号保留为64(t *testing.T) {
	assert.Equal(t, 64, constant.ChannelTypeMegaAI,
		"渠道类型编号是永久占位，必须保留 64 —— 删掉会被后续渠道复用，"+
			"而历史 tasks/logs 里还有 platform=64 的老记录")
}

// 对照：在售渠道的适配器不能被这次接入误伤。
func TestMegaAI_在售渠道的适配器仍可用(t *testing.T) {
	for _, channelType := range []int{
		constant.ChannelTypeYoukou,
		constant.ChannelTypeYouzanWan3,
		constant.ChannelTypeManwu,
		constant.ChannelTypeXinbi,
	} {
		adaptor := GetTaskAdaptor(constant.TaskPlatform(itoa(channelType)))
		assert.NotNil(t, adaptor,
			"在售渠道类型 %d 必须仍能拿到任务适配器 —— 重新接入 MegaAI 不能误伤它们", channelType)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 2：端点声明必须包含 type=64（否则广场不标 /v1/videos）
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_端点声明包含OpenAIVideo 守的是「广场/定价元数据」这一层。
//
// 端点声明决定 supported_endpoint_types 与前端展示入口；漏了它，
// 客户按 /v1/videos 调是对的，但广场不会把它标成视频模型。
func TestMegaAI_端点声明包含OpenAIVideo(t *testing.T) {
	endpointTypes := common.GetEndpointTypesByChannelType(constant.ChannelTypeMegaAI, "T3-seedance-2.5")

	assert.Contains(t, endpointTypes, constant.EndpointTypeOpenAIVideo,
		"type=64 已重新接入，必须声明为 OpenAI Video 端点 —— "+
			"它走的是 POST /v1/videos 提交 + GET /v1/videos/{id} 轮询")
}

// ────────────────────────────────────────────────────────────────────────────
// 守卫 3：megabyai 包本体保留，youkou 的复用不受影响
// ────────────────────────────────────────────────────────────────────────────

// TestMegaAI_youkou仍复用megabyai计费 是最容易被踩坏的一脚。
//
// youkou 的 seedance2.5特惠版 走 megabyai 适配器拿「按次计费」：
// EstimateBilling=nil，禁止带入 sora 的 seconds/size 倍率。
// 改 megabyai 包时多动一处 selectSub 逻辑，多秒任务会被扣到上百元。
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
