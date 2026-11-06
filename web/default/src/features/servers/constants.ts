/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import type {
  BandwidthOption,
  DurationOption,
  Region,
  RegionId,
  ServerGpu,
  ServerPlan,
  ServerSpec,
  TermOption,
} from './types'

/**
 * 服务器租赁的静态配置与价格模型。
 *
 * ⚠️ 价格是**前端占位模型值**（用户明确要求「先随便弄一个模型值，后期来改」），
 *    真实定价以后由后端 option / 接口下发。改价时优先改这里，
 *    前端先跑通闭环，后端接入后由服务端返回的报价覆盖。
 */

/** 可选机房 */
export const REGIONS: Region[] = [
  {
    id: 'guangzhou',
    nameKey: 'Guangzhou',
    descriptionKey: 'South China hub with direct access to mainland networks',
    line: 'CN2 GIA',
    latency: '12ms',
    priceFactor: 1,
  },
  {
    id: 'shanghai',
    nameKey: 'Shanghai',
    descriptionKey: 'East China hub with the strongest international routing',
    line: 'CN2 GIA',
    latency: '28ms',
    priceFactor: 1.08,
  },
]

/** 可选服务器型号 */
export const PLANS: ServerPlan[] = [
  {
    id: 'lite',
    nameKey: 'Lite',
    taglineKey: 'Best for personal projects and blogs',
    descriptionKey:
      'Entry-level instances for websites, blogs and small services. Instant provisioning with a public IPv4 address.',
    basePricePerMonth: 68,
    // 按小时口径：约合 ¥116.8/月（包月 ¥68/月 的 1.72 倍），
    // 按量要为闲置/停机时段付费，所以比包月折算价贵
    basePricePerHour: 0.16,
    specs: [
      { id: 'lite-2c2g', cpu: 2, memory: 2, disk: 40, multiplier: 1 },
      { id: 'lite-2c4g', cpu: 2, memory: 4, disk: 60, multiplier: 1.25 },
      { id: 'lite-4c4g', cpu: 4, memory: 4, disk: 80, multiplier: 1.7 },
    ],
  },
  {
    id: 'general',
    nameKey: 'General Purpose',
    taglineKey: 'Balanced compute and memory for most workloads',
    descriptionKey:
      'General purpose instances for APIs, CI runners and container hosts. SSD storage and private networking included.',
    basePricePerMonth: 88,
    // ≈ ¥153/月（包月 ¥88/月 的 1.7 倍）
    basePricePerHour: 0.21,
    badgeKey: 'Most popular',
    specs: [
      { id: 'general-4c8g', cpu: 4, memory: 8, disk: 100, multiplier: 1 },
      { id: 'general-8c16g', cpu: 8, memory: 16, disk: 200, multiplier: 1.9 },
    ],
  },
  {
    id: 'compute',
    nameKey: 'Compute Optimized',
    taglineKey: 'High frequency cores for heavy computation',
    descriptionKey:
      'Compute optimized instances for batch jobs, model inference and data processing. High clock speed vCPUs.',
    basePricePerMonth: 228,
    // ≈ ¥402/月（包月 ¥228/月 的 1.75 倍）
    basePricePerHour: 0.55,
    specs: [
      { id: 'compute-8c16g', cpu: 8, memory: 16, disk: 200, multiplier: 1 },
      {
        id: 'compute-16c32g',
        cpu: 16,
        memory: 32,
        disk: 400,
        multiplier: 1.85,
      },
    ],
  },
  {
    id: 'memory',
    nameKey: 'Memory Optimized',
    taglineKey: 'Large memory pools for databases and caching',
    descriptionKey:
      'Memory optimized instances for MySQL, PostgreSQL, Elasticsearch and in-memory caches. Large NVMe volumes.',
    basePricePerMonth: 368,
    // ≈ ¥642/月（包月 ¥368/月 的 1.75 倍）
    basePricePerHour: 0.88,
    specs: [
      { id: 'memory-8c32g', cpu: 8, memory: 32, disk: 200, multiplier: 1 },
      { id: 'memory-16c64g', cpu: 16, memory: 64, disk: 400, multiplier: 1.9 },
    ],
  },
  {
    id: 'gpu',
    nameKey: 'GPU Accelerated',
    taglineKey: 'Dedicated GPU for inference and rendering',
    descriptionKey:
      'GPU accelerated instances with dedicated graphics cards for model inference, rendering and video processing.',
    basePricePerMonth: 300,
    // ≈ ¥1825/月（包月 ¥1280/月 的 1.43 倍）
    // 单卡 4090 的量级参照 AutoDL 同档卡型（¥1.8~2.6/小时）
    basePricePerHour: 2.5,
    badgeKey: 'New',
    // 按价格从低到高排：单卡 4090 → 单卡 5090 → 单卡 RTX PRO 6000。
    //
    // 三档都把「主机」和「显卡」分开定价（用户要求：显卡必须是最贵、
    // 占大头）。拆账公式见每条规格上的注释，三行相加恒等于机型价。
    //   4090      2.50 + 0.00 +  3.10 =  5.60   |  300 +   0 +  980 = 1280
    //   5090      2.50 + 0.00 +  5.30 =  7.80   |  300 +   0 + 1492 = 1792
    //   PRO 6000  2.50 + 0.50 + 16.90 = 19.90   |  300 +  180 + 6100 = 6580
    //
    // 包月基准单独给 ¥300（不是 ¥1,280）：4090 包月整机只卖 ¥1,280，
    // 基准若还是 ¥1,280 就没有余量留给显卡那一行了（会算出负数加价）。
    // 与后端 setting/server_catalog.go 一一对应。
    specs: [
      {
        id: 'gpu-16c64g',
        cpu: 16,
        memory: 64,
        disk: 30,
        dataDisk: 200,
        multiplier: 1,
        hourlyPrice: 5.6,
        monthlyPrice: 1280,
        gpuHourlyPrice: 3.1,
        gpuMonthlyPrice: 980,
        gpu: { model: 'RTX 4090', vram: 24, count: 1 },
      },
      {
        id: 'gpu-16c64g-rtx5090',
        cpu: 16,
        memory: 64,
        disk: 30,
        dataDisk: 200,
        multiplier: 1.4,
        hourlyPrice: 7.8,
        monthlyPrice: 1792,
        gpuHourlyPrice: 5.3,
        gpuMonthlyPrice: 1492,
        gpu: { model: 'RTX 5090', vram: 32, count: 1 },
      },
      {
        /**
         * RTX PRO 6000 单卡档：按量 ¥19.90/小时，包月 ¥6,580/月。
         * 主机配置：24 vCPU / 96 GB / 系统盘 30 GB + 数据盘 200 GB，
         * 单卡 RTX PRO 6000 96 GB。
         *
         * 选中这档时带宽自动套到 100 Mbps（defaultMbps）——
         * 96 GB 显存是拿来跑大模型的，权重动辄几十 GB，
         * 默认带宽给低了用户第一步就卡住。
         */
        id: 'gpu-24c96g-rtxpro6000',
        cpu: 24,
        memory: 96,
        disk: 30,
        dataDisk: 200,
        multiplier: 6,
        hourlyPrice: 19.9,
        monthlyPrice: 6580,
        gpuHourlyPrice: 16.9,
        gpuMonthlyPrice: 6100,
        defaultMbps: 100,
        gpu: { model: 'RTX PRO 6000', vram: 96, count: 1 },
      },
    ],
  },
]

/**
 * 旧规格 id → 新规格 id 的兼容映射。
 *
 * 规格 id 里编码了规格本身（`gpu-16c64g` = 16 vCPU / 64 GB，
 * 带卡的档位还会在后缀里带上卡型，如 `gpu-16c64g-rtx5090`），
 * 内存翻倍后 id 跟着变。历史订单与实例存在服务端数据库里，
 * 老记录仍带旧 id；没有这张映射的话 `specs.find(...)` 会落空并回落到
 * `specs[0]`，把「双卡 96 GB」的老实例显示成「单卡 64 GB」——
 * 卡数、内存、倍率全错，而且没有任何报错。
 *
 * `gpu-24c96g`（双卡 4090）已经下线，被单卡 RTX PRO 6000 顶替。
 * 老实例**不能**直接落到新档位上 —— 那是另一块卡、另一个价，
 * 用户会以为自己的机器被换过。所以下线档位单独留一份定义，
 * 老实例照旧显示「双卡 4090 / 96 GB」，价格也按原系数算。
 */
const LEGACY_SPEC_IDS: Record<string, string> = {
  'gpu-16c32g': 'gpu-16c64g',
  'gpu-24c48g': 'gpu-24c96g',
  'gpu-24c96g': 'gpu-24c96g',
}

/**
 * 已下线的规格定义（只用于让历史订单/实例仍能正确显示与计价）。
 *
 * 这些档位不出现在购买页的规格选择器里 —— 它们已经卖完了。
 * 但历史订单、老实例还带着这些 id，
 * 没有这份定义的话 `getSpec` 会回落到 specs[0]，
 * 把「双卡 4090 / 24 vCPU / 96 GB」显示成「单卡 4090 / 16 vCPU / 64 GB」，
 * 卡数、内存、倍率全错且不报错。
 */
const RETIRED_SPECS: Record<string, ServerSpec[]> = {
  gpu: [
    {
      id: 'gpu-24c96g',
      cpu: 24,
      memory: 96,
      disk: 1000,
      // 原双卡 4090 档的价格系数，保持不变，老订单的价格才对得上账。
      //
      // 机型基准月价改到 ¥300 后，光靠 multiplier 1.7 会算成 ¥510/月
      // （老价是 ¥1,280 × 1.7 = ¥2,176）。老实例的价格是既成事实，
      // 不能因为新档位改基准就跟着变，所以两个口径都写死成原价。
      multiplier: 1.7,
      hourlyPrice: 4.25,
      monthlyPrice: 2176,
      gpu: { model: 'RTX 4090', vram: 24, count: 2 },
    },
  ],
}

/**
 * 可选带宽（独立于型号，价格按月加价）
 *
 * 定价口径：**以 10 Mbps 为基准单价**，带宽越大单位价格越低（规模效应），
 * 且 100 Mbps 的价格恰好是 10 Mbps 的 3 倍（¥54 = ¥18 × 3）。
 *
 * 为什么这么定：用户明确要求「100M 是 10M 价格的 3 倍，以 10M 的价格为基准」。
 * 如果只把 100M 改小而中间档不动，价格曲线会倒挂
 * （100M 比 50M 还便宜），用户一比价就发现算错了。
 * 所以整条阶梯一起重排：单位价格从 1.80 元/Mbps 递减到 0.54 元/Mbps，
 * 既满足 3 倍锚点，又保持「带宽越大越便宜、总额越大」的单调性。
 */
export const BANDWIDTHS: BandwidthOption[] = [
  { mbps: 5, pricePerMonth: 0, descriptionKey: '5 Mbps dedicated' },
  { mbps: 10, pricePerMonth: 18, descriptionKey: '10 Mbps dedicated' },
  { mbps: 20, pricePerMonth: 30, descriptionKey: '20 Mbps dedicated' },
  { mbps: 50, pricePerMonth: 42, descriptionKey: '50 Mbps dedicated' },
  { mbps: 100, pricePerMonth: 54, descriptionKey: '100 Mbps dedicated' },
]

/**
 * 旧的包月周期表。
 *
 * @deprecated 只用于把本地已存的旧订单/实例（只有 term，没有 durationId）
 * 映射到新的时长档，不再用于新配置。新配置一律用 `DURATIONS`。
 */
export const TERMS: TermOption[] = [
  { id: 'half', nameKey: '6 months', months: 6, discount: 1 },
  {
    id: 'year',
    nameKey: '1 year',
    months: 12,
    discount: 0.85,
    badgeKey: 'Save 15%',
  },
  {
    id: 'triple',
    nameKey: '3 years',
    months: 36,
    discount: 0.72,
    badgeKey: 'Save 28%',
  },
]

/**
 * 可购买的租用时长档 —— 页面上**唯一**的时间维度。
 *
 * 为什么是一份平铺列表而不是「计费方式 + 周期」两层：
 * 「包月预付」只是「半年 / 一年 / 三年」那组档位的统称，把它单列成一个
 * 开关，用户要先选口径再选周期，右侧凭空多一层。按 AutoDL 的口径，
 * 时长从 1 小时排到 3 年，选中哪一档就决定了用哪套计价模型。
 *
 * 阶梯折扣的理由与 AutoDL 一致：租得越久对平台是更稳定的需求，
 * 所以长档位给一点折扣，而不是全都一个价。
 */
export const DURATIONS: DurationOption[] = [
  // 按量：小时
  { id: '1h', pricing: 'hourly', amount: 1, nameKey: '1 hour', discount: 1 },
  { id: '6h', pricing: 'hourly', amount: 6, nameKey: '6 hours', discount: 1 },
  {
    id: '12h',
    pricing: 'hourly',
    amount: 12,
    nameKey: '12 hours',
    discount: 1,
  },
  {
    id: '24h',
    pricing: 'hourly',
    amount: 24,
    nameKey: '24 hours',
    discount: 1,
    badgeKey: 'Most popular',
  },
  // 按量：天（折成小时计价，amount 记小时数）
  {
    id: '3d',
    pricing: 'hourly',
    amount: 72,
    nameKey: '3 days',
    discount: 0.95,
    badgeKey: 'Save 5%',
  },
  {
    id: '7d',
    pricing: 'hourly',
    amount: 168,
    nameKey: '7 days',
    discount: 0.9,
    badgeKey: 'Save 10%',
  },
  // 包月预付：月
  { id: '1mo', pricing: 'prepaid', amount: 1, nameKey: '1 month', discount: 1 },
  {
    id: '3mo',
    pricing: 'prepaid',
    amount: 3,
    nameKey: '3 months',
    discount: 1,
  },
  {
    id: '6mo',
    pricing: 'prepaid',
    amount: 6,
    nameKey: '6 months',
    discount: 1,
  },
  {
    id: '1y',
    pricing: 'prepaid',
    amount: 12,
    nameKey: '1 year',
    discount: 0.85,
    badgeKey: 'Save 15%',
  },
  {
    id: '3y',
    pricing: 'prepaid',
    amount: 36,
    nameKey: '3 years',
    discount: 0.72,
    badgeKey: 'Save 28%',
  },
]

export const DEFAULT_REGION: RegionId = 'guangzhou'
export const DEFAULT_PLAN = 'general'
export const DEFAULT_MBPS = 20
/** 默认包月一年（与改造前的默认购买周期一致） */
export const DEFAULT_DURATION_ID = '1y'

export function getRegion(id: RegionId): Region {
  return REGIONS.find((r) => r.id === id) ?? REGIONS[0]
}

export function getPlan(id: string): ServerPlan {
  return PLANS.find((p) => p.id === id) ?? PLANS[0]
}

export function getBandwidth(mbps: number): BandwidthOption {
  return BANDWIDTHS.find((b) => b.mbps === mbps) ?? BANDWIDTHS[0]
}

/**
 * 取规格。
 *
 * 三级回落：直接命中 → 旧 id 兼容映射 → 该机型第一档。
 * 中间那级不能省：规格 id 里带内存，内存翻倍后 id 变了，
 * 历史订单与实例还带旧 id，直接回落第一档会把双卡老实例
 * 显示成单卡（卡数、内存、倍率全错且不报错）。
 *
 * 已下线的档位（如双卡 4090）走 `RETIRED_SPECS`：老实例仍按原卡型、
 * 原系数显示与计价，不会被悄悄换成新卡。
 */
export function getSpec(plan: ServerPlan, specId: string): ServerSpec {
  const direct = plan.specs.find((s) => s.id === specId)
  if (direct) return direct
  // 旧 id 先过一层兼容映射，再在「在售档位」与「已下线档位」里找
  const alias = LEGACY_SPEC_IDS[specId] ?? specId
  const mapped = plan.specs.find((s) => s.id === alias)
  if (mapped) return mapped
  const retired = RETIRED_SPECS[plan.id]?.find((s) => s.id === alias)
  return retired ?? plan.specs[0]
}

/**
 * 取租用时长档。
 *
 * 传入未知 id（老数据 / 手改配置）时退回**默认档**（DEFAULT_DURATION_ID），
 * 不能退回列表第一项 —— 第一项是 1 小时，落到它等于把老订单
 * 悄悄改成按量 1 小时，价格和到期时间全错。
 */
export function getDuration(id: string): DurationOption {
  return (
    DURATIONS.find((d) => d.id === id) ??
    DURATIONS.find((d) => d.id === DEFAULT_DURATION_ID) ??
    DURATIONS[0]
  )
}

/**
 * 显卡展示文案，例如「1× RTX 4090 24GB」。
 *
 * 显存必须和卡型一起出现：用户判断「我的模型装不装得下」只认显存
 * （24GB 能跑 7B 推理，13B 全参微调就得 40GB+），
 * 只给卡型等于把选型责任推回给用户。
 */
export function formatGpu(gpu?: ServerGpu): string {
  if (!gpu) return ''
  return `${gpu.count}× ${gpu.model} ${gpu.vram}GB`
}

/**
 * 规格的 CPU / 内存 / 磁盘摘要（不含显卡，显卡单独一行）。
 *
 * 磁盘报的是**可用容量**：有数据盘时报数据盘（系统盘 30 GB 只装系统，
 * 用户真正拿来放数据/权重的是数据盘），没有分盘时就是单一磁盘。
 * 分盘的明细由 `formatDiskBreakdown` 负责，用在控制台的「本地磁盘」列。
 *
 * `t` 由调用方传入 —— 磁盘文案要跟随界面语言，不能在这里写死中文。
 */
export function formatSpec(
  spec: ServerSpec,
  _t?: (key: string) => string
): string {
  const capacity = spec.dataDisk ?? spec.disk
  return `${spec.cpu} vCPU · ${spec.memory} GB · ${capacity} GB`
}

/**
 * 分盘明细，例如「系统盘 30 GB · 数据盘 200 GB」。
 *
 * 控制台的「本地磁盘」列用它 —— 买的时候只看总容量，交付之后
 * 用户要分清楚「哪个盘能随便写、哪个盘写满了系统会崩」。
 */
export function formatDiskBreakdown(
  spec: ServerSpec,
  t?: (key: string) => string
): string {
  const system = `${spec.disk} GB ${t?.('System disk') ?? 'System disk'}`
  if (!spec.dataDisk) return system
  return `${system} · ${spec.dataDisk} GB ${t?.('Data disk') ?? 'Data disk'}`
}

/**
 * 数据盘信息 —— 购买页用 camelCase，实例快照是后端原样的 snake_case，
 * 两边字段名不同。这里只取算扩容余量需要的两个数，两种命名都收。
 */
export interface DataDiskInfo {
  dataDisk?: number
  dataDiskMax?: number
  data_disk?: number
  data_disk_max?: number
}

/** 数据盘可扩容说明，例如「可扩容 23 GB」；没有扩容能力时返回空串 */
export function formatDataDiskExpand(
  spec: DataDiskInfo,
  t?: (key: string, options?: Record<string, unknown>) => string
): string {
  const current = spec.dataDisk ?? spec.data_disk
  const max = spec.dataDiskMax ?? spec.data_disk_max
  if (!current || !max) return ''
  const room = max - current
  if (room <= 0) return ''
  return t
    ? t('Expandable by {{size}} GB', { size: room })
    : `Expandable by ${room} GB`
}

/** 套餐卡的显卡概要（取基准档的显卡）；非 GPU 机型返回空串 */
export function planGpuSummary(plan: ServerPlan): string {
  return formatGpu(plan.specs[0]?.gpu)
}
