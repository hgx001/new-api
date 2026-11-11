package constant

import "strings"

// ════════════════════════════════════════════════════════════════════════════
// seedance2.5 促销族 —— 能力定义的**唯一来源**
// ════════════════════════════════════════════════════════════════════════════
//
// 背景：seedance2.5-特惠 与 seedance2.5-活动 是同一个商品的两种售价形态
// （母本 = 按秒 ¥0.20/秒，活动版 = 按次 ¥4.5/次固定 30 秒），能力完全相同：
// 参考图 ≤10、参考视频 ≤5、参考音频 ≤5、媒体合计 ≤20、无首尾帧、
// 提示词上限 5000 字符、上游恒为 wan3.0-video-prime。
//
// 为什么要有这个文件：这些能力原先散落在 11 处精确集合里
// （youzanwan3 的 isPrimeModel / isTehuiModel / upstreamModelFor、
// megabyai 的三个 Models、upstream_redact 的脱敏名单、pricing 的广场白名单…）。
// 复制一份模型就要在 11 处各写一遍名字 —— 于是必然出现「母本加了能力、
// 活动版没加」的不一致，而且没人会发现。
//
// 这个文件把「谁是这一族」集中成一份定义，所有判定点改为查它：
// **新增成员只改这一处，其余全部自动同步。** 这正是需求方要的
// 「如果其中一个模型有新增的能力，也要同步到另外一个」。
//
// ⚠️ 为什么放 constant 包：model 包不能 import relay/... （会成 import 环），
// 而脱敏名单（model/upstream_redact.go）必须能引用这份定义。
// constant 包不 import 任何业务包（已核实），所以 model / relay / middleware /
// setting 都能单向依赖它。
//
// ⚠️ 必须用**精确集合**，不能用前缀：站内还有 seedance-2-5 / seedance-2.5 /
// seedance2.5特惠版 等一堆相近的 seedance 模型名，前缀匹配会误命中别家渠道的
// 枚举表（megabyai 的 constants.go 里已经踩过这个坑并写明）。

const (
	// Seedance25TeHuiModel 是 seedance2.5 促销族的**母本**：按秒计费。
	//
	// 上游是渠道 17「有赞 Wan3」(type 63) 的 wan3.0-video-prime，
	// 720P ¥0.20/秒、1080P ¥0.30/秒，独立定价不跟随 prime。
	Seedance25TeHuiModel = "seedance2.5-特惠"

	// Seedance25HuoDongModel 是 seedance2.5 促销族的**活动版**：按次计费。
	//
	// 2026-11-02 上架：¥4.5/次、固定 720P；2026-11-11 起时长放开为
	// **2-30 秒逐秒可选**（售价仍恒 ¥4.5/条，不随时长变化）。
	// 与母本**同货源同链路**（有赞 prime），只是计费口径从「按秒」改成「按次」——
	// 这是「单纯把价格改下促销、差异化销售」的技术含义。
	Seedance25HuoDongModel = "seedance2.5-活动"

	// Seedance25PerCallFixedSeconds 是按次档的**默认时长**（秒）。
	//
	// 2026-11-11 起不再是「硬固定」：客户可以显式传 2-30 任一秒数，
	// 售价恒 ¥4.5/条（计费与时长解耦）。这里只在**客户没传**时作为默认值 ——
	// 保持旧客户「不传时长拿 30 秒片」的行为不变。
	//
	// ⚠️ 毛利提示：30 秒是本档的成本上限（30 × ¥0.15 = ¥4.50 正好贴售价），
	// 客户传满 30 秒时这一单毛利为 0；传 10 秒时成本 ¥1.50、毛利 67%。
	// 价格与上限是**成对**的商业决策，改任一边都要重算。
	//
	// ⚠️ 这是**唯一来源**：源链路（youzanwan3 的默认值）与条件路由层
	// （middleware 的时长归一）都从这里派生。之前两处各写一份 `= 30`，
	// 任何一边单独改动都会让「按 30 秒卖」和「实际跑多久」悄悄脱节 ——
	// 改一边的当晚不会报错，只会在客户拿到片之后才发现。
	Seedance25PerCallFixedSeconds = 30

	// Seedance25PerCallFixedResolution 是按次固定档的**固定分辨率**。
	//
	// 按次只有一档价（¥4.5/次，与分辨率无关），所以锁死 720P ——
	// 上游 prime 提供 720P/1080P，1080P 的成本是 720P 的 1.33 倍，
	// 卖同一个价就会在 1080P 上倒挂。锁 720P 才能保证「一口价 = 一个成本」。
	Seedance25PerCallFixedResolution = "720P"
)

// Seedance25Family 是 seedance2.5 促销族的**成员集合**。
//
// 这是能力定义的唯一来源。判定点（档位校验、上游映射、脱敏、路由、
// 广场白名单）都查它，不各自维护名单。
//
// ⚠️ 新增成员只改这里 —— 其余判定点自动同步。
var Seedance25Family = map[string]bool{
	Seedance25TeHuiModel:   true,
	Seedance25HuoDongModel: true,
}

// Seedance25PerCallModels 是族内**按次计费**的成员。
//
// 按次 = 售价固定、不乘时长倍率、不乘分辨率倍率（EstimateBilling 返回 nil）。
// 前端据此显示「/次」而不是「/秒」。
//
// ⚠️ 与 Seedance25PerSecondModels **必须互斥**：一个模型同时进两个集合，
// 前端会显示「/秒」但后端按次结算 —— 静默错价，比报错更难发现。
var Seedance25PerCallModels = map[string]bool{
	Seedance25HuoDongModel: true,
}

// Seedance25PerSecondModels 是族内**按秒计费**的成员。
//
// 按秒 = 售价 = 基准价 × 时长倍率（EstimateBilling 返回 {"seconds": N, "size": R}）。
var Seedance25PerSecondModels = map[string]bool{
	Seedance25TeHuiModel: true,
}

// IsSeedance25Family 判断模型名是否属于 seedance2.5 促销族。
//
// 对外模型名可能有首尾空格（生产上真出现过「 seedance2.5-特惠」），
// 所以先 TrimSpace 再比对 —— 与 model/log.go 里那条 Trim 口径一致。
func IsSeedance25Family(model string) bool {
	return Seedance25Family[strings.TrimSpace(model)]
}

// IsSeedance25PerCall 判断模型名是否属于族内的**按次**成员。
func IsSeedance25PerCall(model string) bool {
	return Seedance25PerCallModels[strings.TrimSpace(model)]
}

// IsSeedance25PerSecond 判断模型名是否属于族内的**按秒**成员。
func IsSeedance25PerSecond(model string) bool {
	return Seedance25PerSecondModels[strings.TrimSpace(model)]
}
