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
  Region,
  RegionId,
  ServerPlan,
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
    badgeKey: 'New',
    specs: [
      {
        id: 'gpu-16c32g',
        cpu: 16,
        memory: 32,
        disk: 500,
        multiplier: 1,
      },
      {
        id: 'gpu-24c48g',
        cpu: 24,
        memory: 48,
        disk: 1000,
        multiplier: 1.7,
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

export const DEFAULT_REGION: RegionId = 'guangzhou'
export const DEFAULT_PLAN = 'general'
export const DEFAULT_MBPS = 20
export const DEFAULT_TERM = 'year'

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
