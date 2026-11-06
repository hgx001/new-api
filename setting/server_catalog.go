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
// （100M = 10M × 3、RTX PRO 6000 = ¥15/小时），改单边会直接测挂。

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
	// MonthlyPrice 是**包月口径的机型月价**（¥/月），可选。
	//
	// 什么时候需要它：按量与包月是两个独立的价格体系，不总能由同一个
	// 系数推出来。RTX PRO 6000 档就是这种 —— 按量卖 ¥15/小时
	// （系数 6 × 基准时价 ¥2.50），包月卖 ¥6,580/月，
	// 而 ¥6,580 ÷ ¥1,280 = 5.140625 与按量系数 6 对不上：
	// 硬套系数会算出 ¥7,680，比用户要的贵 ¥1,100/月，且没有任何报错。
	//
	// 语义：填了就**直接作为该档的包月机型价**，覆盖「基准月价 × 系数」；
	// 不填则回落到系数推导（绝大多数档位保持单一系数，两口径天然自洽）。
	MonthlyPrice *float64   `json:"monthly_price,omitempty"`
	GPU          *ServerGPU `json:"gpu,omitempty"`
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
		Id:                "gpu",
		BasePricePerMonth: 1280,
		BasePricePerHour:  2.5,
		Specs: []ServerSpec{
			{Id: "gpu-16c64g", CPU: 16, Memory: 64, Disk: 500, Multiplier: 1, GPU: &ServerGPU{Model: "RTX 4090", VRAM: 24, Count: 1}},
			{Id: "gpu-16c64g-rtx5090", CPU: 16, Memory: 64, Disk: 500, Multiplier: 1.4, GPU: &ServerGPU{Model: "RTX 5090", VRAM: 32, Count: 1}},
			{
				// RTX PRO 6000 单卡档：¥15.00/小时。
				//
				// 按量卖 ¥15.00/小时：系数 = ¥15.00 ÷ 基准时价 ¥2.50 = 6.00。
				//
				// 包月卖 ¥6,580/月（用户指定）。注意这**不是** ¥1,280 × 6：
				// ¥6,580 ÷ ¥1,280 = 5.140625，与按量系数 6 不一致 —— 按量与包月
				// 是两套独立定价，不能用同一个系数推导。所以这里用
				// MonthlyPrice 显式指定包月机型价，按量仍走系数 6。
				//
				// 主机配置：24 vCPU / 96 GB / 1000 GB，单卡 RTX PRO 6000 96 GB。
				Id:           "gpu-24c96g-rtxpro6000",
				CPU:          24,
				Memory:       96,
				Disk:         1000,
				Multiplier:   6,
				MonthlyPrice: floatPtr(6580),
				GPU:          &ServerGPU{Model: "RTX PRO 6000", VRAM: 96, Count: 1},
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
	BandwidthPrice    float64 `json:"bandwidth_price"`
	RegionFactor      float64 `json:"region_factor"`
	PeriodSeconds     int64   `json:"period_seconds"`
	Region            string  `json:"region"`
	Plan              string  `json:"plan"`
	SpecId            string  `json:"spec_id"`
	Mbps              int     `json:"mbps"`
	DurationId        string  `json:"duration_id"`
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

// ComputeServerQuote 计算一台服务器的报价。
//
// 公式（与前端 computePrice 同源，改一处必须改另一处）：
//
//	包月：裸机型月价 + 规格加价 + 带宽月加价 = 月价小计
//	      月价小计 × 地区系数 = 月单价
//	      月单价 × 月数 × 时长折扣 = 实付总价
//	按量：裸机型时价 + 规格加价 + 带宽时价（月价 ÷ 730）= 时价小计
//	      时价小计 × 地区系数 = 小时单价
//	      小时单价 × 小时数 × 时长折扣 = 实付总价
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
	// 包月口径下规格若显式指定了月价（MonthlyPrice），以它为准。
	//
	// 按量与包月是两套独立定价，不能强行用同一个系数互推：
	// RTX PRO 6000 档按量 ¥15/小时（系数 6），包月 ¥6,580/月
	// （÷ ¥1,280 = 5.140625）。硬套系数会算成 ¥7,680，比用户要的贵 ¥1,100。
	//
	// 只有包月口径读这个字段 —— 按量仍由 Multiplier 推导，两边各自独立。
	if duration.Pricing == ServerBillingPrepaid && spec.MonthlyPrice != nil {
		machinePrice = roundServerMoney(*spec.MonthlyPrice)
	}
	quote.SpecPrice = roundServerMoney(machinePrice - quote.PlanPrice)

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
