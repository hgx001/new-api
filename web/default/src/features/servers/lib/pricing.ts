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
  getSpec,
} from '../constants'
import type {
  BillingMode,
  PriceBreakdown,
  ServerConfig,
  ServerPlan,
  ServerSpec,
} from '../types'

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
 * 机型的裸基准价（不含规格系数、不含带宽、不含地区），按当前口径。
 *
 * 这个数就是左侧套餐卡上标的数字 —— 右侧摘要的「套餐」行也用它，
 * 两边必须是同一个值，否则读者对不上账。
 */
export function planBasePrice(plan: ServerPlan, billing: BillingMode): number {
  return billing === 'hourly'
    ? roundMoney(plan.basePricePerHour)
    : roundMoney(plan.basePricePerMonth)
}

/**
 * 某个规格的机型价（不含带宽、不含地区），按当前口径。
 *
 * 左侧规格按钮标的就是它；右侧摘要的「套餐」行 + 「配置」行相加也等于它。
 */
export function specMachinePrice(
  plan: ServerPlan,
  spec: ServerSpec,
  billing: BillingMode
): number {
  return roundMoney(planBasePrice(plan, billing) * spec.multiplier)
}

/**
 * 某个规格相对基准规格的加价（按当前口径）。
 *
 * 基准规格为 0。左侧规格按钮用「规格机型价」展示，右侧摘要的「配置」行
 * 用这个加价展示 —— 两者必须出自同一个函数，否则左右两边会对不上。
 */
export function specSurcharge(
  plan: ServerPlan,
  spec: ServerSpec,
  billing: BillingMode
): number {
  return roundMoney(
    specMachinePrice(plan, spec, billing) - planBasePrice(plan, billing)
  )
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
 *   裸机型小时价 + 规格加价 + 带宽小时加价 = 时价小计
 *   时价小计 × 地区系数                     = 小时单价
 *   小时单价 × 小时数 × 时长折扣            = 实付总价
 *
 * 包月预付：
 *   裸机型月价 + 规格加价 + 带宽月加价 = 月价小计
 *   月价小计 × 地区系数                = 月单价
 *   月单价 × 月数 × 时长折扣           = 实付总价
 *
 * 为什么要拆成「裸机型价 + 规格加价 + 带宽加价」三项：
 * 裸机型价必须与左侧套餐卡上标的数字**完全一致**，各行相加又能正好等于
 * 右侧显示的单价 —— 读者在页面上就能自己把总价加出来，不必相信我们的算法。
 */
export function computePrice(config: ServerConfig): PriceBreakdown {
  const plan = getPlan(config.plan)
  const spec = getSpec(plan, config.specId)
  const bandwidth = getBandwidth(config.mbps)
  const region = getRegion(config.region)
  const duration = getDuration(resolveDurationId(config))

  if (duration.pricing === 'hourly') {
    // 拆成可加总的分项：读者在摘要里把各行加起来就能得到单价。
    //   裸机型时价 + 规格加价 + 带宽时价 = 时价小计；× 地区系数 = 小时单价
    const planPrice = planBasePrice(plan, 'hourly')
    const planUnit = specMachinePrice(plan, spec, 'hourly')
    const specPrice = specSurcharge(plan, spec, 'hourly')
    // 带宽在按量口径下同样按小时计价：用月加价折算成小时值
    const bandwidthPrice = roundMoney(bandwidth.pricePerMonth / HOURS_PER_MONTH)
    const unitPrice = roundMoney(
      (planUnit + bandwidthPrice) * region.priceFactor
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
      planPrice,
      specPrice,
      bandwidthPrice,
      regionFactor: region.priceFactor,
    }
  }

  const planPrice = planBasePrice(plan, 'prepaid')
  const planUnit = specMachinePrice(plan, spec, 'prepaid')
  const specPrice = specSurcharge(plan, spec, 'prepaid')
  const bandwidthPrice = roundMoney(bandwidth.pricePerMonth)
  const monthly = roundMoney((planUnit + bandwidthPrice) * region.priceFactor)

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
    planPrice,
    specPrice,
    bandwidthPrice,
    regionFactor: region.priceFactor,
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
