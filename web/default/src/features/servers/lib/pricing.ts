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

import {
  DEFAULT_BILLING,
  DEFAULT_HOURS,
  getBandwidth,
  getHourlyDuration,
  getPlan,
  getRegion,
  getTerm,
} from '../constants'
import type { BillingMode, PriceBreakdown, ServerConfig } from '../types'

/**
 * 一个月的折算小时数。
 *
 * 用 730（365 × 2）而不是 720：按小时单价与包月价的换算、
 * 「折合每月」这类对比值都要用它，口径必须固定，否则两处会算出不同的数。
 */
export const HOURS_PER_MONTH = 730

/** 金额取整到分，避免浮点误差累积 */
export function roundMoney(value: number): number {
  return Math.round(value * 100) / 100
}

/**
 * 金额展示口径：统一 ¥ + 千分位 + 两位小数。
 *
 * 按小时单价会出现 ¥0.09 这种小数，不统一的话套餐卡写「¥0.09」、
 * 摘要写「¥0.1」，同一笔钱两个写法。
 */
export function formatAmount(value: number): string {
  return `¥${value.toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

/**
 * 取计费方式。旧数据（本地已存的订单/实例）没有这个字段，按包月处理。
 */
export function resolveBilling(config: ServerConfig): BillingMode {
  return config.billing ?? DEFAULT_BILLING
}

/**
 * 取按小时计费的租用时长（小时）。旧数据没有这个字段，退回最短档。
 */
export function resolveHours(config: ServerConfig): number {
  return config.hours ?? DEFAULT_HOURS
}

/**
 * 计算一台服务器的报价。
 *
 * 报价公式（占位价格模型，后端接入后由服务端报价覆盖）：
 *
 * 包月预付：
 *   型号基准月价 × 规格系数 = 机型月价
 *   机型月价 + 带宽加价     = 月价小计
 *   月价小计 × 地区系数     = 折算月价
 *   折算月价 × 月数 × 周期折扣 = 实付总价
 *   原价 = 折算月价 × 月数
 *
 * 按小时按量：
 *   型号基准小时价 × 规格系数 = 机型小时价
 *   机型小时价 + 带宽小时加价 = 小时单价小计
 *   小时单价小计 × 地区系数   = 折算小时价
 *   折算小时价 × 小时数 × 时长折扣 = 实付总价
 *   原价 = 折算小时价 × 小时数
 */
export function computePrice(config: ServerConfig): PriceBreakdown {
  const plan = getPlan(config.plan)
  const spec = plan.specs.find((s) => s.id === config.specId) ?? plan.specs[0]
  const bandwidth = getBandwidth(config.mbps)
  const region = getRegion(config.region)

  if (resolveBilling(config) === 'hourly') {
    const duration = getHourlyDuration(resolveHours(config))
    const baseHourly = roundMoney(plan.basePricePerHour * spec.multiplier)
    // 带宽在按小时口径下同样按小时计价：用月加价折算成小时值
    const bandwidthHourly = roundMoney(
      bandwidth.pricePerMonth / HOURS_PER_MONTH
    )
    const unitPrice = roundMoney(
      (baseHourly + bandwidthHourly) * region.priceFactor
    )
    const listTotal = roundMoney(unitPrice * duration.hours)
    const total = roundMoney(listTotal * duration.discount)

    return {
      billing: 'hourly',
      unit: 'hour',
      units: duration.hours,
      unitPrice,
      discount: duration.discount,
      listTotal,
      total,
      saved: roundMoney(listTotal - total),
      // 折合每月：让用户能直接和包月价比，判断「按量租一周」贵不贵
      monthlyEquivalent: roundMoney((total / duration.hours) * HOURS_PER_MONTH),
    }
  }

  const term = getTerm(config.term)
  const baseMonthly = roundMoney(plan.basePricePerMonth * spec.multiplier)
  const bandwidthMonthly = roundMoney(bandwidth.pricePerMonth)
  const monthly = roundMoney(
    (baseMonthly + bandwidthMonthly) * region.priceFactor
  )

  const listTotal = roundMoney(monthly * term.months)
  const total = roundMoney(listTotal * term.discount)

  return {
    billing: 'prepaid',
    unit: 'month',
    units: term.months,
    unitPrice: monthly,
    discount: term.discount,
    listTotal,
    total,
    saved: roundMoney(listTotal - total),
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
    billing: DEFAULT_BILLING,
    hours: DEFAULT_HOURS,
    ...overrides,
  }
}
