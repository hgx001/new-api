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
  DEFAULT_DURATION_ID,
  DURATIONS,
  TERMS,
  getBandwidth,
  getDuration,
  getPlan,
  getRegion,
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
 * 取配置对应的租用时长档 id。
 *
 * 新配置直接带 `durationId`；本地已存的旧订单/实例是「计费方式 + 周期」
 * 时代的产物，只有 term/billing/hours，这里映射到最接近的新档位，
 * 保证老数据打开页面时算得出价、显示得出剩余时间。
 */
export function resolveDurationId(config: ServerConfig): string {
  if (config.durationId) return config.durationId

  if (config.billing === 'hourly') {
    const hours = config.hours ?? 1
    const matched = DURATIONS.find(
      (d) => d.pricing === 'hourly' && d.amount === hours
    )
    return matched?.id ?? DEFAULT_DURATION_ID
  }

  const months =
    TERMS.find((t) => t.id === (config.term ?? 'year'))?.months ?? 12
  const matched = DURATIONS.find(
    (d) => d.pricing === 'prepaid' && d.amount === months
  )
  return matched?.id ?? DEFAULT_DURATION_ID
}

/**
 * 取计费口径（按量 / 包月预付）。
 *
 * 由选中的时长档决定 —— 页面上不再有独立的「计费方式」开关，
 * 口径是时长的属性，不是另一个维度。
 */
export function resolveBilling(config: ServerConfig): BillingMode {
  return getDuration(resolveDurationId(config)).pricing
}

/**
 * 计算一台服务器的报价。
 *
 * 报价公式（占位价格模型，后端接入后由服务端报价覆盖）：
 *
 * 按量（小时 / 天）：
 *   型号基准小时价 × 规格系数 = 机型小时价
 *   机型小时价 + 带宽小时加价 = 小时单价小计
 *   小时单价小计 × 地区系数   = 折算小时价
 *   折算小时价 × 小时数 × 时长折扣 = 实付总价
 *   原价 = 折算小时价 × 小时数
 *
 * 包月预付：
 *   型号基准月价 × 规格系数 = 机型月价
 *   机型月价 + 带宽加价     = 月价小计
 *   月价小计 × 地区系数     = 折算月价
 *   折算月价 × 月数 × 时长折扣 = 实付总价
 *   原价 = 折算月价 × 月数
 */
export function computePrice(config: ServerConfig): PriceBreakdown {
  const plan = getPlan(config.plan)
  const spec = plan.specs.find((s) => s.id === config.specId) ?? plan.specs[0]
  const bandwidth = getBandwidth(config.mbps)
  const region = getRegion(config.region)
  const duration = getDuration(resolveDurationId(config))

  if (duration.pricing === 'hourly') {
    const baseHourly = roundMoney(plan.basePricePerHour * spec.multiplier)
    // 带宽在按量口径下同样按小时计价：用月加价折算成小时值
    const bandwidthHourly = roundMoney(
      bandwidth.pricePerMonth / HOURS_PER_MONTH
    )
    const unitPrice = roundMoney(
      (baseHourly + bandwidthHourly) * region.priceFactor
    )
    const listTotal = roundMoney(unitPrice * duration.amount)
    const total = roundMoney(listTotal * duration.discount)

    return {
      billing: 'hourly',
      unit: 'hour',
      units: duration.amount,
      unitPrice,
      discount: duration.discount,
      listTotal,
      total,
      saved: roundMoney(listTotal - total),
      // 折合每月：让用户能直接和包月价比，判断「按量租一周」贵不贵
      monthlyEquivalent: roundMoney(
        (total / duration.amount) * HOURS_PER_MONTH
      ),
    }
  }

  const baseMonthly = roundMoney(plan.basePricePerMonth * spec.multiplier)
  const bandwidthMonthly = roundMoney(bandwidth.pricePerMonth)
  const monthly = roundMoney(
    (baseMonthly + bandwidthMonthly) * region.priceFactor
  )

  const listTotal = roundMoney(monthly * duration.amount)
  const total = roundMoney(listTotal * duration.discount)

  return {
    billing: 'prepaid',
    unit: 'month',
    units: duration.amount,
    unitPrice: monthly,
    discount: duration.discount,
    listTotal,
    total,
    saved: roundMoney(listTotal - total),
    monthlyEquivalent: roundMoney(total / duration.amount),
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
    durationId: DEFAULT_DURATION_ID,
    ...overrides,
  }
}
