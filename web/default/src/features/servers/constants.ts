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
    basePricePerMonth: 38,
    // 按小时口径：约合 ¥65.7/月（包月 ¥38/月 的 1.7 倍），
    // 按量要为闲置/停机时段付费，所以比包月折算价贵
    basePricePerHour: 0.09,
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
    basePricePerMonth: 1280,
    // ≈ ¥1825/月（包月 ¥1280/月 的 1.43 倍）
    // 单卡 4090 的量级参照 AutoDL 同档卡型（¥1.8~2.6/小时）
    basePricePerHour: 2.5,
    badgeKey: 'New',
    specs: [
      {
        id: 'gpu-16c64g',
        cpu: 16,
        memory: 64,
        disk: 500,
        multiplier: 1,
        gpu: { model: 'RTX 4090', vram: 24, count: 1 },
      },
      {
        id: 'gpu-24c96g',
        cpu: 24,
        memory: 96,
        disk: 1000,
        multiplier: 1.7,
        gpu: { model: 'RTX 4090', vram: 24, count: 2 },
      },
    ],
  },
]

/**
 * 旧规格 id → 新规格 id 的兼容映射。
 *
 * 规格 id 里编码了规格本身（`gpu-16c64g` = 16 vCPU / 64 GB），
 * 内存翻倍后 id 跟着变。演示态的订单与实例存在浏览器 localStorage 里，
 * 老记录仍带旧 id；没有这张映射的话 `specs.find(...)` 会落空并回落到
 * `specs[0]`，把「双卡 96 GB」的老实例显示成「单卡 64 GB」——
 * 卡数、内存、倍率全错，而且没有任何报错。
 */
const LEGACY_SPEC_IDS: Record<string, string> = {
  'gpu-16c32g': 'gpu-16c64g',
  'gpu-24c48g': 'gpu-24c96g',
}

/** 可选带宽（独立于型号，价格按月加价） */
export const BANDWIDTHS: BandwidthOption[] = [
  { mbps: 5, pricePerMonth: 0, descriptionKey: '5 Mbps dedicated' },
  { mbps: 10, pricePerMonth: 18, descriptionKey: '10 Mbps dedicated' },
  { mbps: 20, pricePerMonth: 45, descriptionKey: '20 Mbps dedicated' },
  { mbps: 50, pricePerMonth: 120, descriptionKey: '50 Mbps dedicated' },
  { mbps: 100, pricePerMonth: 260, descriptionKey: '100 Mbps dedicated' },
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
 * localStorage 里的老订单/实例还带旧 id，直接回落第一档会把双卡老实例
 * 显示成单卡（卡数、内存、倍率全错且不报错）。
 */
export function getSpec(plan: ServerPlan, specId: string): ServerSpec {
  const direct = plan.specs.find((s) => s.id === specId)
  if (direct) return direct
  const alias = LEGACY_SPEC_IDS[specId]
  const mapped = alias ? plan.specs.find((s) => s.id === alias) : undefined
  return mapped ?? plan.specs[0]
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

/** 规格的 CPU / 内存 / 磁盘摘要（不含显卡，显卡单独一行） */
export function formatSpec(spec: ServerSpec): string {
  return `${spec.cpu} vCPU · ${spec.memory} GB · ${spec.disk} GB`
}

/** 套餐卡的显卡概要（取基准档的显卡）；非 GPU 机型返回空串 */
export function planGpuSummary(plan: ServerPlan): string {
  return formatGpu(plan.specs[0]?.gpu)
}
