package setting

import (
	"fmt"
	"math"

	"github.com/shopspring/decimal"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// 云服务器租赁的目录与价格模型。
//
// 这里的数字必须与前端 `web/default/src/features/servers/constants.ts` 完全一致：
// 前端只负责展示，**唯一可信的金额来自本文件的 ComputeServerQuote**。
// 下单接口不接受前端传来的价格，只接受「地区/机型/规格/带宽/时长」五个选择，
// 由服务端重新算一次 —— 否则改一个请求体就能用 1 分钱买走一台 GPU 机器。
//
// 改价流程：先改这里（后端），再同步前端常量；两边都有测试锁定关键价位
// （100M = 10M × 3、GPU 三档时价 5.6 / 7.8 / 19.9 且显卡占机型价大头），
// 改单边会直接测挂。

const ServerHoursPerMonth = 730

// floatPtr 给可选的浮点字段一个取址入口。
//
// ServerSpec.MonthlyPrice 是 *float64：0 是一个合法价格（免费档），
// 用值类型 + 零值判断就分不清「没填」和「填了 0」。
func floatPtr(v float64) *float64 {
	return &v
}

const (
	ServerBillingHourly  = "hourly"
	ServerBillingPrepaid = "prepaid"
)

// ServerGPU 显卡配置。显存决定模型装不装得下，必须与卡型一起下发。
type ServerGPU struct {
	Model string `json:"model"`
	VRAM  int    `json:"vram"`
	Count int    `json:"count"`
}

type ServerSpec struct {
	Id     string `json:"id"`
	CPU    int    `json:"cpu"`
	Memory int    `json:"memory"`
	Disk   int    `json:"disk"`
	// DataDisk 是数据盘 GB。真实机器的盘不是一个数 —— 系统盘装系统、
	// 数据盘放数据集，扩容时也只扩数据盘，合成一个数字会对不上交付内容。
	DataDisk int `json:"data_disk,omitempty"`
	// DataDiskMax 是数据盘可扩容到的上限 GB。
	DataDiskMax int     `json:"data_disk_max,omitempty"`
	CPUModel    string  `json:"cpu_model,omitempty"`
	Multiplier  float64 `json:"multiplier"`
	// HourlyPrice / MonthlyPrice 是两个口径各自独立的**机型价**，
	// 填了就直接覆盖「基准价 × Multiplier」的推导，不填才回落到系数。
	//
	// 为什么需要显式定价：按量与包月是两套独立的价格体系，不能互推。
	// GPU 三档就是典型 —— 用户指定 4090 ¥5.60 / 5090 ¥7.80 /
	// PRO 6000 ¥19.90 每小时，这些数除以基准时价 ¥2.50 会得到
	// 2.24 / 3.12 / 7.96 这种没人看得懂的系数，而且改一个价就要反算一遍。
	// 同理 PRO 6000 包月卖 ¥6,580，¥6,580 ÷ ¥1,280 = 5.140625，
	// 与按量口径也完全对不上。硬套系数会算错钱且不报错。
	//
	// 语义：HourlyPrice 只在按量口径生效，MonthlyPrice 只在包月口径生效，
	// 互不污染。Multiplier 仍保留给未显式定价的档位以及历史数据展示。
	HourlyPrice  *float64 `json:"hourly_price,omitempty"`
	MonthlyPrice *float64 `json:"monthly_price,omitempty"`

	// GpuHourlyPrice / GpuMonthlyPrice 是机型价里**显卡那部分**（¥/小时、¥/月），
	// 与两个口径一一对应，同样互不读取对方的字段。
	//
	// 为什么要单独拆出来：GPU 机器值钱就值在那张卡上，但价格只给一个总数时，
	// 用户看到的是「24 vCPU / 96 GB 的主机收了 ¥17.40/小时」—— 主机的加价
	// 比显卡还显眼，贵得没有道理，也没法判断「换一张卡要加多少钱」。
	// 拆开之后摘要里显卡单独一行、占大头，主机只收成本价。
	//
	// 拆的是**展示口径**，不改变总价：HourlyPrice 仍是该档的总机型价，
	// GpuHourlyPrice 是它的一个子项，配置行拿的是差值
	// （机型价 − 套餐基准 − 显卡），三行相加恒等于机型价。
	GpuHourlyPrice  *float64 `json:"gpu_hourly_price,omitempty"`
	GpuMonthlyPrice *float64 `json:"gpu_monthly_price,omitempty"`

	// DefaultMbps 是选中该规格时前端自动套用的带宽档。
	//
	// 大卡机器默认给人配 5 Mbps 是没意义的 —— 拉一个 30 GB 的模型权重
	// 要跑一整天。所以 PRO 6000 这类旗舰档直接默认 100 Mbps，
	// 用户想省钱再自己往下调。0 表示不指定，沿用页面默认值。
	DefaultMbps int `json:"default_mbps,omitempty"`

	GPU *ServerGPU `json:"gpu,omitempty"`
}

type ServerPlan struct {
	Id                string       `json:"id"`
	BasePricePerMonth float64      `json:"base_price_per_month"`
	BasePricePerHour  float64      `json:"base_price_per_hour"`
	Specs             []ServerSpec `json:"specs"`
}

type ServerRegion struct {
	Id          string  `json:"id"`
	Line        string  `json:"line"`
	Latency     string  `json:"latency"`
	PriceFactor float64 `json:"price_factor"`
}

type ServerBandwidth struct {
	Mbps          int     `json:"mbps"`
	PricePerMonth float64 `json:"price_per_month"`
}

type ServerDuration struct {
	Id       string  `json:"id"`
	Pricing  string  `json:"pricing"`
	Amount   int     `json:"amount"`
	Discount float64 `json:"discount"`
}

var ServerRegions = []ServerRegion{
	{Id: "guangzhou", Line: "CN2 GIA", Latency: "12ms", PriceFactor: 1},
	{Id: "shanghai", Line: "CN2 GIA", Latency: "28ms", PriceFactor: 1.08},
}

var ServerPlans = []ServerPlan{
	{
		Id:                "lite",
		BasePricePerMonth: 68,
		BasePricePerHour:  0.16,
		Specs: []ServerSpec{
			{Id: "lite-2c2g", CPU: 2, Memory: 2, Disk: 40, Multiplier: 1},
			{Id: "lite-2c4g", CPU: 2, Memory: 4, Disk: 60, Multiplier: 1.25},
			{Id: "lite-4c4g", CPU: 4, Memory: 4, Disk: 80, Multiplier: 1.7},
		},
	},
	{
		Id:                "general",
		BasePricePerMonth: 88,
		BasePricePerHour:  0.21,
		Specs: []ServerSpec{
			{Id: "general-4c8g", CPU: 4, Memory: 8, Disk: 100, Multiplier: 1},
			{Id: "general-8c16g", CPU: 8, Memory: 16, Disk: 200, Multiplier: 1.9},
		},
	},
	{
		Id:                "compute",
		BasePricePerMonth: 228,
		BasePricePerHour:  0.55,
		Specs: []ServerSpec{
			{Id: "compute-8c16g", CPU: 8, Memory: 16, Disk: 200, Multiplier: 1},
			{Id: "compute-16c32g", CPU: 16, Memory: 32, Disk: 400, Multiplier: 1.85},
		},
	},
	{
		Id:                "memory",
		BasePricePerMonth: 368,
		BasePricePerHour:  0.88,
		Specs: []ServerSpec{
			{Id: "memory-8c32g", CPU: 8, Memory: 32, Disk: 200, Multiplier: 1},
			{Id: "memory-16c64g", CPU: 16, Memory: 64, Disk: 400, Multiplier: 1.9},
		},
	},
	{
		Id: "gpu",
		// GPU 机型把「主机」和「显卡」分开计价：
		//
		//   套餐  = 平台基础（机房、网络、系统镜像），按量 ¥2.50/小时
		//   配置  = 这台机器的主机部分（vCPU / 内存 / 磁盘），只收成本价
		//   显卡  = 卡本身，机型价里最大的那一块
		//
		// 包月基准单独给 ¥300 —— 按量与包月本来就是两套独立的价格体系，
		// 而且 4090 包月整机只卖 ¥1,280，基准若还是 ¥1,280 就没有余量
		// 留给显卡那一行了（会算出负数加价）。
		BasePricePerMonth: 300,
		BasePricePerHour:  2.5,
		Specs: []ServerSpec{
			// GPU 三档的按量时价由用户直接指定（¥/小时），不再由系数推导。
			// 包月价同样显式指定，两个口径各写各的，改价时不会互相带偏。
			//
			// 三档的拆账（按量 / 包月），显卡必须占大头：
			//   4090      2.50 + 0.00 +  3.10 =  5.60   |  300 +   0 +  980 = 1280
			//   5090      2.50 + 0.00 +  5.30 =  7.80   |  300 +   0 + 1492 = 1792
			//   PRO 6000  2.50 + 0.50 + 16.90 = 19.90   |  300 + 180 + 6100 = 6580
			//
			// 4090 与 5090 是同一台主机（16 vCPU / 64 GB / 同盘），所以配置行
			// 都是 0，两档的差价**全部**落在显卡上 —— 用户换卡时看得很清楚：
			// 从 4090 升到 5090 每小时多 ¥2.20，多的就是那张卡的钱。
			{
				Id:              "gpu-16c64g",
				CPU:             16,
				Memory:          64,
				Disk:            30,
				DataDisk:        200,
				Multiplier:      1,
				HourlyPrice:     floatPtr(5.6),
				MonthlyPrice:    floatPtr(1280),
				GpuHourlyPrice:  floatPtr(3.1),
				GpuMonthlyPrice: floatPtr(980),
				GPU:             &ServerGPU{Model: "RTX 4090", VRAM: 24, Count: 1},
			},
			{
				Id:              "gpu-16c64g-rtx5090",
				CPU:             16,
				Memory:          64,
				Disk:            30,
				DataDisk:        200,
				Multiplier:      1.4,
				HourlyPrice:     floatPtr(7.8),
				MonthlyPrice:    floatPtr(1792),
				GpuHourlyPrice:  floatPtr(5.3),
				GpuMonthlyPrice: floatPtr(1492),
				GPU:             &ServerGPU{Model: "RTX 5090", VRAM: 32, Count: 1},
			},
			{
				// RTX PRO 6000 单卡档：按量 ¥19.90/小时，包月 ¥6,580/月。
				// 主机配置：24 vCPU / 96 GB / 系统盘 30 GB + 数据盘 200 GB，
				// 单卡 RTX PRO 6000 96 GB。
				//
				// 这档默认给 100 Mbps：96 GB 显存的卡是拿来跑大模型的，
				// 权重动辄几十 GB，默认带宽给低了用户第一步就卡住。
				Id:              "gpu-24c96g-rtxpro6000",
				CPU:             24,
				Memory:          96,
				Disk:            30,
				DataDisk:        200,
				Multiplier:      6,
				HourlyPrice:     floatPtr(19.9),
				MonthlyPrice:    floatPtr(6580),
				GpuHourlyPrice:  floatPtr(16.9),
				GpuMonthlyPrice: floatPtr(6100),
				DefaultMbps:     100,
				GPU:             &ServerGPU{Model: "RTX PRO 6000", VRAM: 96, Count: 1},
			},
		},
	},
}

var ServerBandwidths = []ServerBandwidth{
	{Mbps: 5, PricePerMonth: 0},
	{Mbps: 10, PricePerMonth: 18},
	{Mbps: 20, PricePerMonth: 30},
	{Mbps: 50, PricePerMonth: 42},
	{Mbps: 100, PricePerMonth: 54},
}

var ServerDurations = []ServerDuration{
	{Id: "1h", Pricing: ServerBillingHourly, Amount: 1, Discount: 1},
	{Id: "6h", Pricing: ServerBillingHourly, Amount: 6, Discount: 1},
	{Id: "12h", Pricing: ServerBillingHourly, Amount: 12, Discount: 1},
	{Id: "24h", Pricing: ServerBillingHourly, Amount: 24, Discount: 1},
	{Id: "3d", Pricing: ServerBillingHourly, Amount: 72, Discount: 0.95},
	{Id: "7d", Pricing: ServerBillingHourly, Amount: 168, Discount: 0.9},
	{Id: "1mo", Pricing: ServerBillingPrepaid, Amount: 1, Discount: 1},
	{Id: "3mo", Pricing: ServerBillingPrepaid, Amount: 3, Discount: 1},
	{Id: "6mo", Pricing: ServerBillingPrepaid, Amount: 6, Discount: 1},
	{Id: "1y", Pricing: ServerBillingPrepaid, Amount: 12, Discount: 0.85},
	{Id: "3y", Pricing: ServerBillingPrepaid, Amount: 36, Discount: 0.72},
}

// ServerConfig 是下单/报价的唯一输入。价格不在这里 —— 由服务端算。
type ServerConfig struct {
	Region     string `json:"region"`
	Plan       string `json:"plan"`
	SpecId     string `json:"spec_id"`
	Mbps       int    `json:"mbps"`
	DurationId string `json:"duration_id"`
}

type ServerQuote struct {
	Billing           string  `json:"billing"`
	Unit              string  `json:"unit"`
	Units             int     `json:"units"`
	UnitPrice         float64 `json:"unit_price"`
	Discount          float64 `json:"discount"`
	ListTotal         float64 `json:"list_total"`
	Total             float64 `json:"total"`
	Saved             float64 `json:"saved"`
	MonthlyEquivalent float64 `json:"monthly_equivalent"`
	PlanPrice         float64 `json:"plan_price"`
	SpecPrice         float64 `json:"spec_price"`
	// GpuPrice 是机型价里显卡那部分（当前口径）。
	//
	// 三行相加恒等于机型价：PlanPrice + SpecPrice + GpuPrice = 机型价。
	// 没买卡的机型这一项恒为 0。
	GpuPrice       float64 `json:"gpu_price"`
	BandwidthPrice float64 `json:"bandwidth_price"`
	RegionFactor   float64 `json:"region_factor"`
	PeriodSeconds  int64   `json:"period_seconds"`
	Region         string  `json:"region"`
	Plan           string  `json:"plan"`
	SpecId         string  `json:"spec_id"`
	Mbps           int     `json:"mbps"`
	DurationId     string  `json:"duration_id"`
}

func GetServerPlanById(id string) (ServerPlan, bool) {
	for _, plan := range ServerPlans {
		if plan.Id == id {
			return plan, true
		}
	}
	return ServerPlan{}, false
}

func GetServerSpec(plan ServerPlan, specId string) (ServerSpec, bool) {
	for _, spec := range plan.Specs {
		if spec.Id == specId {
			return spec, true
		}
	}
	return ServerSpec{}, false
}

func GetServerRegionById(id string) (ServerRegion, bool) {
	for _, region := range ServerRegions {
		if region.Id == id {
			return region, true
		}
	}
	return ServerRegion{}, false
}

func GetServerBandwidthByMbps(mbps int) (ServerBandwidth, bool) {
	for _, bandwidth := range ServerBandwidths {
		if bandwidth.Mbps == mbps {
			return bandwidth, true
		}
	}
	return ServerBandwidth{}, false
}

func GetServerDurationById(id string) (ServerDuration, bool) {
	for _, duration := range ServerDurations {
		if duration.Id == id {
			return duration, true
		}
	}
	return ServerDuration{}, false
}

// ServerPeriodSeconds 返回一个时长档对应的租期秒数。
//
// 包月档按 30 天/月折算 —— 与前端 `payServerOrder` 早先的口径一致。
// 「1 年」因此是 360 天而不是 365 天，这是**已确认的产品口径**：
// 档位文案写的是 12 个月，不是自然年。改成自然月会同时改变到期时间与退款
// 计算，必须前后端一起改，不能只动一边。
func ServerPeriodSeconds(duration ServerDuration) int64 {
	if duration.Pricing == ServerBillingHourly {
		return int64(duration.Amount) * 3600
	}
	return int64(duration.Amount) * 30 * 24 * 3600
}

func roundServerMoney(value float64) float64 {
	return math.Round(value*100) / 100
}

func serverPlanBasePrice(plan ServerPlan, billing string) float64 {
	if billing == ServerBillingHourly {
		return roundServerMoney(plan.BasePricePerHour)
	}
	return roundServerMoney(plan.BasePricePerMonth)
}

// serverSpecGpuPrice 取该规格在当前计费口径下显卡那部分的加价。
//
// 只读当前口径的字段：按量读 GpuHourlyPrice、包月读 GpuMonthlyPrice，
// 与 HourlyPrice / MonthlyPrice 的隔离规则一致（两套定价不能互推）。
// 没配显卡的规格、或没填显卡价的老档位，一律返回 0 —— 整块机型价都算作
// 「套餐 + 配置」，与改造前的展示完全一致，不会因为加了这层拆分而变价。
func serverSpecGpuPrice(spec ServerSpec, billing string) float64 {
	if spec.GPU == nil {
		return 0
	}
	if billing == ServerBillingHourly && spec.GpuHourlyPrice != nil {
		return roundServerMoney(*spec.GpuHourlyPrice)
	}
	if billing == ServerBillingPrepaid && spec.GpuMonthlyPrice != nil {
		return roundServerMoney(*spec.GpuMonthlyPrice)
	}
	return 0
}

// ComputeServerQuote 计算一台服务器的报价。
//
// 公式（与前端 computePrice 同源，改一处必须改另一处）：
//
//	包月：裸机型月价 + 配置加价 + 显卡加价 + 带宽月加价 = 月价小计
//	      月价小计 × 地区系数 = 月单价
//	      月单价 × 月数 × 时长折扣 = 实付总价
//	按量：裸机型时价 + 配置加价 + 显卡加价 + 带宽时价（月价 ÷ 730）= 时价小计
//	      时价小计 × 地区系数 = 小时单价
//	      小时单价 × 小时数 × 时长折扣 = 实付总价
//
// 「配置加价」与「显卡加价」是机型价的两个子项，两者相加再加裸机型价
// 恒等于该档的机型价。拆账只影响展示，不改变任何一分总价。
//
// 未知机型/规格/带宽/时长/地区一律返回错误，不回落默认值：
// 悄悄换档会让用户付了钱却拿到另一台机器。
func ComputeServerQuote(config ServerConfig) (*ServerQuote, error) {
	plan, ok := GetServerPlanById(config.Plan)
	if !ok {
		return nil, fmt.Errorf("未知机型: %s", config.Plan)
	}
	spec, ok := GetServerSpec(plan, config.SpecId)
	if !ok {
		return nil, fmt.Errorf("机型 %s 下没有规格 %s", config.Plan, config.SpecId)
	}
	bandwidth, ok := GetServerBandwidthByMbps(config.Mbps)
	if !ok {
		return nil, fmt.Errorf("不支持的带宽: %d Mbps", config.Mbps)
	}
	region, ok := GetServerRegionById(config.Region)
	if !ok {
		return nil, fmt.Errorf("未知地区: %s", config.Region)
	}
	duration, ok := GetServerDurationById(config.DurationId)
	if !ok {
		return nil, fmt.Errorf("未知租用时长: %s", config.DurationId)
	}

	quote := &ServerQuote{
		Billing:        duration.Pricing,
		Units:          duration.Amount,
		Discount:       duration.Discount,
		RegionFactor:   region.PriceFactor,
		PeriodSeconds:  ServerPeriodSeconds(duration),
		Region:         region.Id,
		Plan:           plan.Id,
		SpecId:         spec.Id,
		Mbps:           bandwidth.Mbps,
		DurationId:     duration.Id,
		PlanPrice:      serverPlanBasePrice(plan, duration.Pricing),
		BandwidthPrice: roundServerMoney(bandwidth.PricePerMonth),
	}
	machinePrice := roundServerMoney(quote.PlanPrice * spec.Multiplier)
	// 显式机型价覆盖系数推导，且**按量与包月互不读取对方的字段**：
	// GPU 三档的时价（5.6 / 7.8 / 19.9）与包月价（1280 / 1792 / 6580）
	// 是两套独立定价，硬用 Multiplier 互推会算错钱还不报错。
	// 只有当前口径对应的字段生效，另一口径的值不会污染本次报价。
	if duration.Pricing == ServerBillingHourly && spec.HourlyPrice != nil {
		machinePrice = roundServerMoney(*spec.HourlyPrice)
	}
	if duration.Pricing == ServerBillingPrepaid && spec.MonthlyPrice != nil {
		machinePrice = roundServerMoney(*spec.MonthlyPrice)
	}
	// 显卡部分同样「只读当前口径」：按量读 GpuHourlyPrice，包月读
	// GpuMonthlyPrice，没买卡的机型为 0。配置行拿的是差值 ——
	// 套餐 + 配置 + 显卡 三行相加恒等于机型价，前端摘要就按这个加法展示。
	quote.GpuPrice = serverSpecGpuPrice(spec, duration.Pricing)
	quote.SpecPrice = roundServerMoney(machinePrice - quote.PlanPrice - quote.GpuPrice)

	if duration.Pricing == ServerBillingHourly {
		quote.Unit = "hour"
		quote.BandwidthPrice = roundServerMoney(bandwidth.PricePerMonth / ServerHoursPerMonth)
		quote.UnitPrice = roundServerMoney((machinePrice + quote.BandwidthPrice) * region.PriceFactor)
		quote.ListTotal = roundServerMoney(quote.UnitPrice * float64(duration.Amount))
		quote.Total = roundServerMoney(quote.ListTotal * duration.Discount)
		quote.Saved = roundServerMoney(quote.ListTotal - quote.Total)
		quote.MonthlyEquivalent = roundServerMoney(quote.Total / float64(duration.Amount) * ServerHoursPerMonth)
		return quote, nil
	}

	quote.Unit = "month"
	quote.UnitPrice = roundServerMoney((machinePrice + quote.BandwidthPrice) * region.PriceFactor)
	quote.ListTotal = roundServerMoney(quote.UnitPrice * float64(duration.Amount))
	quote.Total = roundServerMoney(quote.ListTotal * duration.Discount)
	quote.Saved = roundServerMoney(quote.ListTotal - quote.Total)
	quote.MonthlyEquivalent = roundServerMoney(quote.Total / float64(duration.Amount))
	return quote, nil
}

// ServerQuotaForAmount 把人民币金额换算成站内额度（quota 以美元计价）。
//
// 站内 1 USD = QuotaPerUnit 额度，余额显示时再乘 USDExchangeRate 折成 ¥。
// 因此 ¥ 金额要先除以汇率再乘额度单位，**不能**直接乘 QuotaPerUnit ——
// 那会把 ¥68/月 的机器按 $68 扣款，多收 7.3 倍。
func ServerQuotaForAmount(amountCNY float64) int {
	if amountCNY <= 0 || common.QuotaPerUnit <= 0 {
		return 0
	}
	rate := operation_setting.USDExchangeRate
	if rate <= 0 {
		rate = 7.3
	}
	quota := decimal.NewFromFloat(amountCNY).
		Div(decimal.NewFromFloat(rate)).
		Mul(decimal.NewFromFloat(common.QuotaPerUnit)).
		Ceil().
		IntPart()
	return int(quota)
}

// ServerAmountFromQuota 是 ServerQuotaForAmount 的反向换算，用于退款金额展示。
func ServerAmountFromQuota(quota int) float64 {
	if quota <= 0 || common.QuotaPerUnit <= 0 {
		return 0
	}
	rate := operation_setting.USDExchangeRate
	if rate <= 0 {
		rate = 7.3
	}
	amount := decimal.NewFromInt(int64(quota)).
		Div(decimal.NewFromFloat(common.QuotaPerUnit)).
		Mul(decimal.NewFromFloat(rate)).
		Round(2)
	value, _ := amount.Float64()
	return value
}

// ServerCatalog 是下发给前端的价格目录（缓存友好，不含任何用户数据）。
type ServerCatalog struct {
	Regions    []ServerRegion    `json:"regions"`
	Plans      []ServerPlan      `json:"plans"`
	Bandwidths []ServerBandwidth `json:"bandwidths"`
	Durations  []ServerDuration  `json:"durations"`
}

func GetServerCatalog() ServerCatalog {
	return ServerCatalog{
		Regions:    ServerRegions,
		Plans:      ServerPlans,
		Bandwidths: ServerBandwidths,
		Durations:  ServerDurations,
	}
}
