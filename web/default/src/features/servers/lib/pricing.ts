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
 * 按小时单价会出现 ¥0.16 这种小数，不统一的话套餐卡写「¥0.16」、
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
 *
 * 规格若显式指定了当前口径的机型价，以它为准，且**只读取当前口径的字段**：
 * GPU 三档的时价（5.6 / 7.8 / 19.9）与包月价（1280 / 1792 / 6580）是两套
 * 独立定价，硬用 multiplier 互推会算错钱还不报错。
 * 与后端 `ComputeServerQuote` 同口径，两边报价必须一致。
 */
export function specMachinePrice(
  plan: ServerPlan,
  spec: ServerSpec,
  billing: BillingMode
): number {
  if (billing === 'hourly' && spec.hourlyPrice !== undefined) {
    return roundMoney(spec.hourlyPrice)
  }
  if (billing === 'prepaid' && spec.monthlyPrice !== undefined) {
    return roundMoney(spec.monthlyPrice)
  }
  return roundMoney(planBasePrice(plan, billing) * spec.multiplier)
}

/**
 * 某个规格里**显卡那部分**的加价（按当前口径）。
 *
 * 与 `specMachinePrice` 同样的隔离规则：按量只读 `gpuHourlyPrice`、
 * 包月只读 `gpuMonthlyPrice`，两套定价不互推。
 * 没配显卡的规格、或没填显卡价的老档位一律返回 0 —— 整块机型价都算作
 * 「套餐 + 配置」，与拆分前的展示一致，不会因为多了这一层而变价。
 *
 * 与后端 `serverSpecGpuPrice` 同口径。
 */
export function specGpuPrice(
  spec: ServerSpec,
  billing: BillingMode
): number {
  if (!spec.gpu) return 0
  if (billing === 'hourly' && spec.gpuHourlyPrice !== undefined) {
    return roundMoney(spec.gpuHourlyPrice)
  }
  if (billing === 'prepaid' && spec.gpuMonthlyPrice !== undefined) {
    return roundMoney(spec.gpuMonthlyPrice)
  }
  return 0
}

/**
 * 某个规格相对基准规格的加价（按当前口径）。
 *
 * 基准规格为 0。注意要减掉显卡价 —— 显卡在摘要里单独一行，
 * 不然「配置」行会把卡钱也算进去，看起来像主机很贵（这正是要修的观感问题）。
 * 三行相加：套餐 + 配置 + 显卡 = 机型价，恒等。
 */
export function specSurcharge(
  plan: ServerPlan,
  spec: ServerSpec,
  billing: BillingMode
): number {
  return roundMoney(
    specMachinePrice(plan, spec, billing) -
      planBasePrice(plan, billing) -
      specGpuPrice(spec, billing)
  )
}

/**
 * 型号的「起步价」：该型号下最便宜的规格价（按当前口径）。
 *
 * 套餐卡上写的是「From ¥X」—— 必须是用户真能买到的那个价，
 * 而不是裸机型基准价。GPU 机型的基准价（¥2.50/小时）比它最便宜的
 * 整机（¥5.60/小时）低一半，拿基准价当起步价会让人以为显卡机器很便宜。
 */
export function planFromPrice(plan: ServerPlan, billing: BillingMode): number {
  return plan.specs.reduce(
    (min, spec) => Math.min(min, specMachinePrice(plan, spec, billing)),
    Number.POSITIVE_INFINITY
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
    //   裸机型时价 + 配置加价 + 显卡加价 + 带宽时价 = 时价小计；× 地区系数 = 小时单价
    const planPrice = planBasePrice(plan, 'hourly')
    const planUnit = specMachinePrice(plan, spec, 'hourly')
    const specPrice = specSurcharge(plan, spec, 'hourly')
    const gpuPrice = specGpuPrice(spec, 'hourly')
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
      gpuPrice,
      bandwidthPrice,
      regionFactor: region.priceFactor,
    }
  }

  const planPrice = planBasePrice(plan, 'prepaid')
  const planUnit = specMachinePrice(plan, spec, 'prepaid')
  const specPrice = specSurcharge(plan, spec, 'prepaid')
  const gpuPrice = specGpuPrice(spec, 'prepaid')
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
    gpuPrice,
    bandwidthPrice,
    regionFactor: region.priceFactor,
  }
}

/** 周期折扣的百分比展示，例如 0.85 → 15 */
export function discountPercent(discount: number): number {
  return Math.round((1 - discount) * 100)
}

/** 服务器默认配置：取型号的第一个规格（带宽跟随该规格的默认值） */
export function defaultConfigFor(
  planId: string,
  overrides: Partial<ServerConfig> = {}
): ServerConfig {
  const plan = getPlan(planId)
  const spec = plan.specs[0]
  return {
    region: 'guangzhou',
    plan: plan.id,
    specId: spec.id,
    mbps: spec.defaultMbps ?? 20,
    durationId: DEFAULT_DURATION_ID,
    ...overrides,
  }
}
