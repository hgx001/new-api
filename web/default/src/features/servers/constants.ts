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
  BillingMode,
  HourlyDuration,
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
        id: 'gpu-16c32g',
        cpu: 16,
        memory: 32,
        disk: 500,
        multiplier: 1,
        gpu: { model: 'RTX 4090', vram: 24, count: 1 },
      },
      {
        id: 'gpu-24c48g',
        cpu: 24,
        memory: 48,
        disk: 1000,
        multiplier: 1.7,
        gpu: { model: 'RTX 4090', vram: 24, count: 2 },
      },
    ],
  },
]

/** 可选带宽（独立于型号，价格按月加价） */
export const BANDWIDTHS: BandwidthOption[] = [
  { mbps: 5, pricePerMonth: 0, descriptionKey: '5 Mbps dedicated' },
  { mbps: 10, pricePerMonth: 18, descriptionKey: '10 Mbps dedicated' },
  { mbps: 20, pricePerMonth: 45, descriptionKey: '20 Mbps dedicated' },
  { mbps: 50, pricePerMonth: 120, descriptionKey: '50 Mbps dedicated' },
  { mbps: 100, pricePerMonth: 260, descriptionKey: '100 Mbps dedicated' },
]

/** 可选购买周期 */
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
 * 按小时计费时可购买的时长档。
 *
 * 最短 1 小时（不足 1 小时按 1 小时算），往上到 7 天。
 * 时长越长折扣越高：长期占用对平台是更稳定的需求（与 AutoDL 的阶梯口径一致）。
 */
export const HOURLY_DURATIONS: HourlyDuration[] = [
  { id: '1h', hours: 1, nameKey: '1 hour', discount: 1 },
  { id: '6h', hours: 6, nameKey: '6 hours', discount: 1 },
  { id: '12h', hours: 12, nameKey: '12 hours', discount: 1 },
  {
    id: '24h',
    hours: 24,
    nameKey: '24 hours',
    discount: 1,
    badgeKey: 'Most popular',
  },
  {
    id: '72h',
    hours: 72,
    nameKey: '3 days',
    discount: 0.95,
    badgeKey: 'Save 5%',
  },
  {
    id: '168h',
    hours: 168,
    nameKey: '7 days',
    discount: 0.9,
    badgeKey: 'Save 10%',
  },
]

/**
 * 计费方式选项。
 *
 * 两种口径的用户场景不同，所以都要给一句话说明，让用户在按下单前
 * 就知道自己买的是「用完就释放」还是「整段周期都留着」。
 */
export const BILLING_MODES: {
  id: BillingMode
  /** i18n key: 计费方式名 */
  nameKey: string
  /** i18n key: 一句话说明 */
  descriptionKey: string
}[] = [
  {
    id: 'hourly',
    nameKey: 'Pay as you go (hourly)',
    descriptionKey:
      'Pay only for the hours you use. The instance is released automatically when the purchased time runs out.',
  },
  {
    id: 'prepaid',
    nameKey: 'Prepaid monthly',
    descriptionKey:
      'Pay once for a fixed period and keep the instance for the whole term.',
  },
]

export const DEFAULT_REGION: RegionId = 'guangzhou'
export const DEFAULT_PLAN = 'general'
export const DEFAULT_MBPS = 20
export const DEFAULT_TERM = 'year'
/** 默认按包月预付；按小时是另一个入口，用户主动切过去 */
export const DEFAULT_BILLING: BillingMode = 'prepaid'
/** 默认 1 小时起步，用户可往上选 */
export const DEFAULT_HOURS = 1

export function getRegion(id: RegionId): Region {
  return REGIONS.find((r) => r.id === id) ?? REGIONS[0]
}

export function getPlan(id: string): ServerPlan {
  return PLANS.find((p) => p.id === id) ?? PLANS[0]
}

export function getBandwidth(mbps: number): BandwidthOption {
  return BANDWIDTHS.find((b) => b.mbps === mbps) ?? BANDWIDTHS[0]
}

export function getTerm(id: string): TermOption {
  return TERMS.find((t) => t.id === id) ?? TERMS[0]
}

/**
 * 取按小时计费的时长档。
 *
 * 传入未知时长（老数据 / 手改配置）时退回最短档，
 * 不能让它算出 0 小时或负数总价。
 */
export function getHourlyDuration(hours: number): HourlyDuration {
  return HOURLY_DURATIONS.find((d) => d.hours === hours) ?? HOURLY_DURATIONS[0]
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
