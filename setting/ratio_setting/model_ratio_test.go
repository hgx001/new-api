package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// gpt-6 家族定价：全部按 OpenAI 官网 Standard 档（2026-11-28 实查），
// ratio = 输入价($/1M) ÷ 2，output/input 官网统一 5 倍。
func TestDefaultModelRatioGpt6Family(t *testing.T) {
	InitRatioSettings()

	expect := map[string]float64{
		"gpt-6":         1.0,   // 官网无单独条目，按 sol 档 $2/$10
		"gpt-6.2-sol":   1.0,   // $2.00 / $10.00
		"gpt-6.2-astra": 4.0,   // $8.00 / $40.00
		"gpt-6-astra":   5.0,   // $10.00 / $50.00
		"gpt-6.1-sol":   1.0,   // $2.00 / $10.00
		"gpt-6.1-terra": 0.25,  // $0.50 / $2.50
		"gpt-6-luna":    0.025, // $0.05 / $0.25
		"gpt-6-sol":     1.0,   // sol 档统一 $2（官网无 gpt-6-sol 单独条目）
	}
	for model, want := range expect {
		got, ok, _ := GetModelRatio(model)
		require.True(t, ok, "%s must have an explicit ratio", model)
		require.InDelta(t, want, got, 1e-12, "%s ratio must follow official pricing", model)
		require.Equal(t, 5.0, GetCompletionRatio(model), "%s completion ratio must be 5x", model)
	}

	// 未定价的 gpt-6 不得登记：广场不上架，GetModelRatio 会回落到 37.5（$75/1M），
	// 宁可没有价格也不能带一个错价格上架。
	// 官网有价但号池不供的档位（cyber/rosalind）不得登记，避免挂上即 503。
	require.NotContains(t, defaultModelRatio, "gpt-6-cyber")
	require.NotContains(t, defaultModelRatio, "gpt-6-rosalind")

	// gpt-5.6 旧名随「只留 6.x」下线，不许再留别名倍率。
	for _, retired := range []string{"gpt-5.6", "gpt-5.6-luna", "gpt-5.6-sol", "gpt-5.6-terra"} {
		_, exists := defaultModelRatio[retired]
		require.False(t, exists, "%s must be removed from default ratios", retired)
	}
}

// opus-5.5 已下架：它把 gpt-6-luna 挂上 Claude 的名字售卖，实测模型自认 OpenAI，
// 2026-10-18 从生产与源码一并删除。倍率必须同时清掉，否则日后有人重新挂渠道时
// 它会带着 5 倍加价无声复活。
func TestDefaultModelRatioOpus55Removed(t *testing.T) {
	InitRatioSettings()

	_, ok, _ := GetModelRatio("opus-5.5")
	require.False(t, ok, "opus-5.5 must have no explicit ratio after removal")
	require.NotContains(t, defaultModelRatio, "opus-5.5")
	require.NotContains(t, defaultCompletionRatio, "opus-5.5")
}

// MiniMax-M3 / M3.1 的定价（2026-10-31 老板定：输出价在原价基础上涨 10 倍）。
//
// 换算（与 service/text_quota.go、前端 features/pricing/lib/price.ts 三方一致）：
//
//	¥ / 1M tokens = model_ratio × 1e6 ÷ QuotaPerUnit × USDExchangeRate
//	             = model_ratio × 1e6 ÷ 500000 × 7.3 = model_ratio × 14.6
//	输入价 = model_ratio × 14.6                    = 0.015103 × 14.6 = ¥0.2205/1M
//	输出价 = model_ratio × completion_ratio × 14.6
//	       旧: 0.015103 × 0.410959 × 14.6 = ¥0.0906/1M
//	       新: 0.015103 × 4.10959  × 14.6 = ¥0.9062/1M   ← 正好 10 倍
//
// ⚠️ M3 与 M3.1 必须同价：渠道 model_mapping 是 {"MiniMax-M3.1": "MiniMax-M3"}，
// M3.1 实际转发到上游 M3，共用同一份上游成本。只改一个就是套利漏洞。
func TestDefaultModelRatioMiniMaxM31MatchesM3Price(t *testing.T) {
	InitRatioSettings()

	for _, name := range []string{"MiniMax-M3", "MiniMax-M3.1"} {
		ratio, ok, _ := GetModelRatio(name)
		require.True(t, ok, "%s must have an explicit input price", name)
		require.Equal(t, 0.015103, ratio, "%s input ratio must stay at ¥0.22/1M", name)
		require.Equal(t, 4.10959, GetCompletionRatio(name),
			"%s output ratio must be 10x the old one (¥0.0906 → ¥0.9062 / 1M)", name)
	}
}

// wan3.0-video（标准版）按秒计费：480P 基准 ¥0.27/秒，USD 计价（USD2RMB=7.3）。
// 720P ¥0.54/秒、1080P ¥1.08/秒走分辨率倍率（2 / 4）。
// ⚠️ 2026-10-22 的调价**只针对官网渠道** wan3.0-video-官网，标准版与 prime 的价格一律不动。
func TestDefaultModelPriceWan3PerSecond(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan3.0-video", false)
	require.True(t, ok, "wan3.0-video must have an explicit per-second price")
	require.InDelta(t, 0.27/USD2RMB, price, 1e-7, "480P base must equal ¥0.27/sec in USD")
	require.InDelta(t, 0.0369863, price, 1e-7, "480P base must be ¥0.27/sec in USD")
	require.InDelta(t, 0.54/USD2RMB, price*2.0, 1e-7, "720P tier must equal ¥0.54/sec in USD")
	require.InDelta(t, 1.08/USD2RMB, price*4.0, 1e-7, "1080P tier must equal ¥1.08/sec in USD")

	primePrice, ok := GetModelPrice("wan3.0-video-prime", false)
	require.True(t, ok, "wan3.0-video-prime must have an explicit per-second price")
	require.InDelta(t, 0.405/USD2RMB, primePrice, 1e-8, "Prime 480P base must be ¥0.405/sec")
	require.InDelta(t, 1.5*price, primePrice, 1e-8, "Prime price must be 1.5x standard")
}

// 客美 Kemeitv（渠道 71）的两个万相特价变体：tj ¥0.138/秒、prime-tj ¥0.24/秒
// （480P 基准，内部按 USD 等值存储）。
//
// ⚠️ 这两个是**特价档**，与站内标准版 wan3.0-video（¥0.27）/ prime（¥0.405）
// 价格不同 —— 早先的兜底值照抄了标准档的 ¥0.27/¥0.405，是**错价**。
//
// 为什么这条兜底值必须准：
//   - model/option.go 的 InitOptionMap 用 defaultModelPrice 初始化
//     options.ModelPrice，代码默认值会直接写进 DB；
//   - relay/helper/price.go 的 ModelPriceHelperPerCall 在 DB 查不到时回落到
//     GetDefaultModelPriceMap()。
//
// 兜底值错了 = DB 被重置/迁移后价格静默回退到旧价。
func TestDefaultModelPriceKemeitvSpecialTier(t *testing.T) {
	InitRatioSettings()

	tj, ok := GetModelPrice("wan3.0-video-tj", false)
	require.True(t, ok, "wan3.0-video-tj must have an explicit per-second price")
	require.InDelta(t, 0.138/USD2RMB, tj, 1e-9, "tj 480P base must equal ¥0.138/sec in USD")
	// 生产库 options.ModelPrice 里的值，兜底值必须与之一致。
	require.InDelta(t, 0.0189041095890411, tj, 1e-12, "tj 480P base must match the production ModelPrice value")

	primeTJ, ok := GetModelPrice("wan3.0-video-prime-tj", false)
	require.True(t, ok, "wan3.0-video-prime-tj must have an explicit per-second price")
	require.InDelta(t, 0.24/USD2RMB, primeTJ, 1e-9, "prime-tj 480P base must equal ¥0.24/sec in USD")
	require.InDelta(t, 0.03287671232876712, primeTJ, 1e-12, "prime-tj 480P base must match the production ModelPrice value")

	// 特价档必须**低于**站内标准档 —— 写错成标准价就是错价。
	standard, ok := GetModelPrice("wan3.0-video", false)
	require.True(t, ok, "wan3.0-video must have an explicit per-second price")
	require.Less(t, tj, standard, "kemeitv tj must stay cheaper than the standard wan3.0-video tier")
	standardPrime, ok := GetModelPrice("wan3.0-video-prime", false)
	require.True(t, ok, "wan3.0-video-prime must have an explicit per-second price")
	require.Less(t, primeTJ, standardPrime, "kemeitv prime-tj must stay cheaper than the standard prime tier")
}

// wan3.0-video-官网（官网渠道独立模型名）2026-10-22 单独调价：// 480P ¥0.20/秒、720P ¥0.45/秒、1080P ¥0.80/秒，分辨率倍率 1 / 2.25 / 4。
// 与标准版 wan3.0-video（¥0.27 / ¥0.54 / ¥1.08）**不同价**，两者互不影响。
func TestDefaultModelPriceWan3OfficialPerSecond(t *testing.T) {
	InitRatioSettings()

	official, ok := GetModelPrice("wan3.0-video-官网", false)
	require.True(t, ok, "wan3.0-video-官网 must have an explicit per-second price")
	require.InDelta(t, 0.20/USD2RMB, official, 1e-9, "official 480P base must equal ¥0.20/sec in USD")
	require.InDelta(t, 0.0273972602739726, official, 1e-9, "official 480P base must be ¥0.20/sec in USD")
	require.InDelta(t, 0.45/USD2RMB, official*2.25, 1e-9, "official 720P tier must equal ¥0.45/sec in USD")
	require.InDelta(t, 0.80/USD2RMB, official*4.0, 1e-9, "official 1080P tier must equal ¥0.80/sec in USD")

	std, ok := GetModelPrice("wan3.0-video", false)
	require.True(t, ok)
	require.NotEqual(t, std, official, "official channel must be priced independently from the standard alias")
}

// wan3.0-smart（有赞智能调度版）按秒计费：480P 基准 ¥0.28/秒，
// 720P ¥0.32/秒、1080P ¥0.40/秒走分辨率倍率（0.32/0.28、0.40/0.28）。
// wan3.0-video-官网是官方渠道独立模型名，2026-10-22 起与标准版**不同价**。
func TestDefaultModelPriceWan3SmartPerSecond(t *testing.T) {
	InitRatioSettings()

	smart, ok := GetModelPrice("wan3.0-smart", false)
	require.True(t, ok, "wan3.0-smart must have an explicit per-second price")
	require.InDelta(t, 0.28/USD2RMB, smart, 1e-12, "smart 480P base must equal ¥0.28/sec in USD")
	require.InDelta(t, 0.32/USD2RMB, smart*(0.32/0.28), 1e-12, "smart 720P tier must equal ¥0.32/sec in USD")
	require.InDelta(t, 0.40/USD2RMB, smart*(0.40/0.28), 1e-12, "smart 1080P tier must equal ¥0.40/sec in USD")

	official, ok := GetModelPrice("wan3.0-video-官网", false)
	require.True(t, ok, "wan3.0-video-官网 must have an explicit per-second price")
	base, ok := GetModelPrice("wan3.0-video", false)
	require.True(t, ok)
	require.NotEqual(t, base, official, "official channel must be priced independently from the standard alias")
}

// wan2.7-r2v 对外全分辨率统一 ¥0.10/秒（无 480P，720P/1080P 同价）。
// 上游为按秒计费，约 2 点数/秒。
func TestDefaultModelPriceWan27R2VPerSecond(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan2.7-r2v", false)
	require.True(t, ok, "wan2.7-r2v must have an explicit per-second price")
	require.InDelta(t, 0.1/USD2RMB, price, 1e-12, "r2v base must equal ¥0.10/sec in USD")
}

// wan-3.0-特惠（漫屋 → Picsart 特惠档，2026-10-25 由 wan-3.0-无限 改名）：
// 按次计费，**480P ¥1.50 / 720P ¥2.00**。
// ModelPrice 配的是**最低档 480P 的 ¥1.50** —— 模型广场徽标直接显示它，
// 配 720P 的 ¥2.00 会让徽标显示 ¥2 而客户最低能买到 ¥1.50（老板 2026-10-25 定稿）。
// 720P 由 manwu adaptor 的 picsart720PSizeRatio 乘上去。
// 与有赞的 wan-3.0（¥0.23/秒）是**不同商品**（不同渠道/不同上游/不同计费口径），
// 价格必须各自独立，改动其中一条不能带走另一条。
func TestDefaultModelPriceWan3TeHuiPerCall(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan-3.0-特惠", false)
	require.True(t, ok, "wan-3.0-特惠 must have an explicit per-call price")
	require.InDelta(t, 1.5/USD2RMB, price, 1e-12, "picsart 特惠档基价必须等于最低档 480P 的 ¥1.50")

	// 旧名必须**仍有价**：DB 的 ModelPrice 改名后旧名会落到
	// ModelPriceHelperPerCall 的「查不到价」分支，任务按次计费算成 ¥0（静默免单）。
	legacy, legacyOk := GetModelPrice("wan-3.0-无限", false)
	require.True(t, legacyOk, "旧名 wan-3.0-无限 过渡期仍可调用，必须有价，否则按次计费静默算 ¥0")
	require.InDelta(t, price, legacy, 1e-12, "旧名与新名必须同价，否则两个名字下单是两个价")

	// 有赞的 wan-3.0-yz 是**按秒**商品（¥0.20/秒基准档），与本档 ¥1.50 基价不同 ——
	// 显式锁住这个差异，防止有人图省事把两条合并。
	base, ok := GetModelPrice("wan-3.0-yz", false)
	require.True(t, ok, "有赞 wan-3.0-yz 必须配价，否则按秒计费会落到「查不到价」分支算成 ¥0")
	require.InDelta(t, 0.20/USD2RMB, base, 1e-12, "有赞 wan-3.0-yz 基准档必须是 ¥0.20/秒")
	require.NotEqual(t, base, price,
		"两条是不同渠道的独立商品，价格不同；合并会让其中一侧算错账")
}

// 有赞渠道新独立模型 wan-3.0-yz（2026-11-18 上架，公开名带 yz 后缀）：
// 按秒计费，基准档 720P ¥0.20/秒。
//
// ⚠️ 这个 ModelPrice 键**曾经**是 MegaAI 渠道（已停售）的按次价 ¥3.00/条。
// 名字一旦复用而价格口径不同（次 vs 秒），计费会整个算错 —— 所以这里锁死
// 「有赞的 wan-3.0-yz 必须是 ¥0.20/秒」，不是 MegaAI 的 ¥3.00/次。
//
// ⚠️ 裸键 "wan-3.0" 现在**必须没有价**：它只是上游 ID，不是任何商品的公开名。
// 给它配价等于配了个没人调用的键，而真正在用的 "wan-3.0-yz" 反而查不到价
// → 按秒计费落到「查不到价」分支，任务静默算成 ¥0（免费白送）。
func TestDefaultModelPriceWan3YouzanPerSecond(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan-3.0-yz", false)
	require.True(t, ok, "有赞 wan-3.0-yz must have an explicit per-second price")
	require.InDelta(t, 0.20/USD2RMB, price, 1e-12, "有赞 wan-3.0-yz 基准档必须是 ¥0.20/秒")
	require.NotEqual(t, 3.0/USD2RMB, price,
		"不能残留 MegaAI 的 ¥3.00/次 —— 那是已停售渠道的按次价，计费口径完全不同")

	// 上游 ID 不是公开名：裸键不能有价，否则定价页会给一个没人能调用的模型标价。
	_, upstreamIDHasPrice := GetModelPrice("wan-3.0", false)
	require.False(t, upstreamIDHasPrice,
		"\"wan-3.0\" 只是上游 ID，不该有 ModelPrice —— 有价会给没人调用的键标价")
}

// 6/7.3 = 0.821918；任务链路按 ModelPrice 固定扣费，禁止乘 seconds。
func TestDefaultModelPriceSD25PerUse(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("sd-2.5", false)
	require.True(t, ok, "sd-2.5 must have an explicit per-use price")
	require.InDelta(t, 6.0/USD2RMB, price, 1e-6, "sd-2.5 must equal ¥6/call in USD")
	require.InDelta(t, 0.821918, price, 1e-6, "sd-2.5 must be ¥6/call in USD")
}

func TestDefaultModelPriceSeedance20PerUse(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("seedance-2.0", false)
	require.True(t, ok, "seedance-2.0 must have an explicit per-use price")
	require.InDelta(t, 1.0/USD2RMB, price, 1e-12, "seedance-2.0 must be ¥1/call in USD (2026-10-05 由 ¥1.5 下调)")
}

// 2026-10-12 豆包官网计价分家：
//   - seedance-2-0-fast（豆包 2.0 Fast）改按**秒**计价 ¥0.10/秒，由 EstimateBilling
//     返回 seconds 倍率相乘（与 wan3.0 家族同范式），时长 5–15 秒逐秒可选；
//   - seedance-2-5（豆包 2.5）按**次**计价 ¥3.50/次（2026-11-01 由 ¥3.00 上调到
//     ¥5.00，2026-11-04 再下调到 ¥3.50）；
//   - 旧公开名 db-seedance-2-0 / db-seedance-2-5 彻底下线，不得再有价格。
func TestDefaultModelPriceDoubaoSeedance(t *testing.T) {
	InitRatioSettings()

	fast, ok := GetModelPrice("seedance-2-0-fast", false)
	require.True(t, ok, "seedance-2-0-fast must have an explicit per-second price")
	require.InDelta(t, 0.1/USD2RMB, fast, 1e-12, "seedance-2-0-fast must be ¥0.10/sec in USD")
	require.InDelta(t, 0.0136986, fast, 1e-7, "seedance-2-0-fast must be ¥0.10/sec in USD")

	perCall, ok := GetModelPrice("seedance-2-5", false)
	require.True(t, ok, "seedance-2-5 must have an explicit per-use price")
	require.InDelta(t, 3.5/USD2RMB, perCall, 1e-12, "seedance-2-5 must be ¥3.50/call in USD")
	require.InDelta(t, 0.4794521, perCall, 1e-7, "seedance-2-5 must be ¥3.50/call in USD")

	for _, retired := range []string{"db-seedance-2-0", "db-seedance-2-5"} {
		_, exists := defaultModelPrice[retired]
		require.False(t, exists, "%s 必须从默认价格表移除", retired)
	}
}

// wan3.0-班车（2026-11-04 上架，原名「wan3.0-排队」）：视频生成车站排队通道，
// 按次 ¥3.50/次（2026-11-14 老板调价，原 ¥1.50；上游成本 ¥1/次）。
//
// ⚠️ 旧名「wan3.0-排队」不得再有价格 —— 改名时生产库尚无该模型的渠道与能力
// 记录，不存在旧名调用方，留着会让旧名在广场或计费里复活。
func TestDefaultModelPriceYuanmengBanChe(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("wan3.0-班车", false)
	require.True(t, ok, "wan3.0-班车 must have an explicit per-use price")
	require.InDelta(t, 3.5/USD2RMB, price, 1e-12, "wan3.0-班车 must be ¥3.50/call in USD")
	require.InDelta(t, 0.4794521, price, 1e-7, "wan3.0-班车 must be ¥3.50/call in USD")

	_, exists := defaultModelPrice["wan3.0-排队"]
	require.False(t, exists, "旧名「wan3.0-排队」必须从默认价格表移除")
}

func TestDefaultModelPriceSeedance25PerUse(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("seedance-2.5", false)
	require.True(t, ok, "seedance-2.5 must have an explicit per-use price")
	require.InDelta(t, 0.8/USD2RMB, price, 1e-12, "seedance-2.5 must be ¥0.8/call in USD (2026-10-05 由 ¥1.0 下调)")
}

func TestDefaultModelPriceGeminiWebVideoPerUse(t *testing.T) {
	InitRatioSettings()

	price, ok := GetModelPrice("gemini-web-video", false)
	require.True(t, ok, "gemini-web-video must have an explicit per-use price")
	require.InDelta(t, 1.0/USD2RMB, price, 1e-12, "gemini-web-video must be ¥1/call in USD")
}

func TestDefaultModelPriceAutoDLPerSecond(t *testing.T) {
	InitRatioSettings()

	want := 0.1 / USD2RMB
	models := []string{
		"autodl:minimax-h3-text-to-video",
		"autodl:minimax-h3-lightx2v-v5",
		"autodl:minimax-h3-lightx2v-v5-15s",
		"autodl:minimax-h3-b99-12s",
		"autodl:minimax-h3-u24",
		"autodl:minimax-h3-u08",
		"autodl:minimax-h3-image-audio-10s",
		"autodl:minimax-h3-image-audio-15s",
		"autodl:minimax-h3-lipsync",
	}

	for _, model := range models {
		price, ok := GetModelPrice(model, false)
		require.True(t, ok, "AutoDL model must have an explicit per-second price: %s", model)
		require.InDelta(t, want, price, 1e-12, "AutoDL base price must be ¥0.10/sec: %s", model)
	}
}

// gpt-6 家族官网 output/input 全部是 5 倍，不能沿用 gpt-5.6 的 8 倍。
func TestGpt6CompletionRatio(t *testing.T) {
	for _, model := range []string{
		"gpt-6", "gpt-6-astra", "gpt-6-sol", "gpt-6-luna", "gpt-6.1-sol", "gpt-6.1-terra",
	} {
		assert.Equal(t, 5.0, GetCompletionRatio(model), "model=%s", model)
	}
	// gpt-5.x 保持原样，不受影响
	assert.Equal(t, 8.0, GetCompletionRatio("gpt-5.6-sol"))
	assert.Equal(t, 6.0, GetCompletionRatio("gpt-5.5"))
	assert.Equal(t, 6.0, GetCompletionRatio("gpt-5.4"))
}
