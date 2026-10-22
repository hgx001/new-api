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

import { getBandwidth, getPlan, getRegion, getTerm } from '../constants'
import type { PriceBreakdown, ServerConfig } from '../types'

/** 金额取整到分，避免浮点误差累积 */
export function roundMoney(value: number): number {
  return Math.round(value * 100) / 100
}

/**
 * 计算一台服务器的报价。
 *
 * 报价公式（占位价格模型，后端接入后由服务端报价覆盖）：
 *   型号基准月价 × 规格系数 = 机型月价
 *   机型月价 + 带宽加价     = 月价小计
 *   月价小计 × 地区系数     = 折算月价
 *   折算月价 × 月数 × 周期折扣 = 实付总价
 *   原价 = 折算月价 × 月数
 */
export function computePrice(config: ServerConfig): PriceBreakdown {
  const plan = getPlan(config.plan)
  const spec = plan.specs.find((s) => s.id === config.specId) ?? plan.specs[0]
  const bandwidth = getBandwidth(config.mbps)
  const region = getRegion(config.region)
  const term = getTerm(config.term)

  const baseMonthly = roundMoney(plan.basePricePerMonth * spec.multiplier)
  const bandwidthMonthly = roundMoney(bandwidth.pricePerMonth)
  const monthly = roundMoney(
    (baseMonthly + bandwidthMonthly) * region.priceFactor
  )

  const listTotal = roundMoney(monthly * term.months)
  const total = roundMoney(listTotal * term.discount)
  const saved = roundMoney(listTotal - total)

  return {
    baseMonthly,
    bandwidthMonthly,
    monthly,
    months: term.months,
    discount: term.discount,
    listTotal,
    total,
    saved,
    monthlyEquivalent: roundMoney(total / term.months),
  }
}

/** 周期折扣的百分比展示，例如 0.85 → 15 */
export function discountPercent(discount: number): number {
  return Math.round((1 - discount) * 100)
}

/** 服务器默认配置：取型号的第一个规格 */
export function defaultConfigFor(
  planId: string,
  overrides: Partial<ServerConfig> = {}
): ServerConfig {
  const plan = getPlan(planId)
  return {
    region: 'guangzhou',
    plan: plan.id,
    specId: plan.specs[0].id,
    mbps: 20,
    term: 'year',
    ...overrides,
  }
}
