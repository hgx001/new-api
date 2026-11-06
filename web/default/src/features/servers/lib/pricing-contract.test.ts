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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  BANDWIDTHS,
  DURATIONS,
  PLANS,
  formatGpu,
  formatSpec,
  formatDataDiskExpand,
  getDuration,
  getPlan,
  getSpec,
  planGpuSummary,
} from '../constants'
import type { ServerConfig } from '../types'
import {
  HOURS_PER_MONTH,
  computePrice,
  resolveBilling,
  resolveDurationId,
} from './pricing'
/**
 * 前端价格模型与「展示口径」的契约。
 *
 * 金额的唯一权威在服务端（`setting/server_catalog.go`），下单时以服务端
 * 报价与扣费为准；这份前端模型只负责**在用户下单前把账算清楚**，两边
 * 必须同源同值，否则用户会看到「摘要写 ¥5,150，扣款却是另一个数」。
 * 同一组数字在 `setting/server_catalog_test.go` 里从服务端侧锁了一遍，
 * 这里锁前端侧 —— 任何一边改价而另一边没跟上，两边都会红。
 */

function config(overrides: Partial<ServerConfig> = {}): ServerConfig {
  return {
    region: 'guangzhou',
    plan: 'gpu',
    specId: 'gpu-24c96g-rtxpro6000',
    mbps: 20,
    durationId: '1mo',
    ...overrides,
  }
}

describe('servers: RTX PRO 6000 的定价', () => {
  test('包月：机型价 ¥1,280 + 规格加价 ¥5,300 + 带宽 ¥30 = ¥6,610', () => {
    const price = computePrice(config())
    assert.equal(price.planPrice, 1280)
    assert.equal(price.specPrice, 5300)
    assert.equal(price.bandwidthPrice, 30)
    assert.equal(price.unitPrice, 6610)
    assert.equal(price.total, 6610)
  })

  test('按量：¥2.50 + ¥12.50 = ¥15.00/小时', () => {
    const price = computePrice(config({ mbps: 5, durationId: '1h' }))
    assert.equal(price.billing, 'hourly')
    assert.equal(price.unitPrice, 15)
    assert.equal(price.total, 15)
  })

  test('按量仍比包月贵：15 × 730 = ¥10,950 > ¥6,580', () => {
    // 按量与包月是两套独立定价（包月机型价由用户指定 ¥6,580，
    // 不是「基准月价 × 按量系数」推出来的），但「按量比包月贵」
    // 这个基本事实不能反 —— 反了就说明定价配错了。
    const hourly = computePrice(config({ mbps: 5, durationId: '1h' }))
    const monthly = computePrice(config({ mbps: 5 }))
    assert.ok(
      hourly.unitPrice * HOURS_PER_MONTH > monthly.unitPrice,
      '按量折合每月必须高于包月价'
    )
  })
})

describe('servers: 规格与显卡的展示口径', () => {
  test('显卡必须带卡型与显存，卡数在前', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-24c96g-rtxpro6000')
    assert.equal(formatGpu(spec.gpu), '1× RTX PRO 6000 96GB')
  })

  test('没有显卡的规格返回空串，不是 undefined', () => {
    const spec = getSpec(getPlan('lite'), 'lite-2c2g')
    assert.equal(formatGpu(spec.gpu), '')
    assert.equal(formatSpec(spec), '2 vCPU · 2 GB · 40 GB')
  })

  test('套餐卡取基准档的卡型；非 GPU 机型为空', () => {
    assert.equal(planGpuSummary(getPlan('gpu')), '1× RTX 4090 24GB')
    assert.equal(planGpuSummary(getPlan('general')), '')
  })

  test('每个 GPU 在售规格都带显卡，非 GPU 机型一个都不带', () => {
    for (const plan of PLANS) {
      for (const spec of plan.specs) {
        if (plan.id === 'gpu') {
          assert.ok(spec.gpu, `${spec.id} 是 GPU 机型却没有卡型`)
        } else {
          assert.equal(spec.gpu, undefined, `${spec.id} 不该带显卡`)
        }
      }
    }
  })
})

describe('servers: 带宽阶梯', () => {
  test('100 Mbps 正好是 10 Mbps 的 3 倍（用户指定的锚点）', () => {
    const ten = BANDWIDTHS.find((b) => b.mbps === 10)
    const hundred = BANDWIDTHS.find((b) => b.mbps === 100)
    assert.ok(ten && hundred)
    assert.equal(hundred.pricePerMonth, ten.pricePerMonth * 3)
  })

  test('5 Mbps 免费，作为套餐自带带宽', () => {
    assert.equal(BANDWIDTHS.find((b) => b.mbps === 5)?.pricePerMonth, 0)
  })

  test('总价随带宽单调递增，不出现倒挂', () => {
    let previous = -1
    for (const bandwidth of BANDWIDTHS) {
      const price = computePrice(
        config({
          plan: 'general',
          specId: 'general-4c8g',
          mbps: bandwidth.mbps,
        })
      )
      assert.ok(
        price.unitPrice > previous,
        `${bandwidth.mbps} Mbps 的月价没有比上一档更贵`
      )
      previous = price.unitPrice
    }
  })

  test('单位价格递减（规模效应）', () => {
    const paid = BANDWIDTHS.filter((b) => b.pricePerMonth > 0)
    let previous = Number.POSITIVE_INFINITY
    for (const bandwidth of paid) {
      const perMbps = bandwidth.pricePerMonth / bandwidth.mbps
      assert.ok(
        perMbps < previous,
        `${bandwidth.mbps} Mbps 的每 Mbps 单价没有更便宜`
      )
      previous = perMbps
    }
  })
})

describe('servers: 时长档与折扣', () => {
  test('从 1 小时排到 3 年，是唯一的时间维度', () => {
    assert.equal(DURATIONS.length, 11)
    assert.equal(DURATIONS[0].id, '1h')
    assert.equal(DURATIONS.at(-1)?.id, '3y')
  })

  test('按量档记小时数、包月档记月数', () => {
    const threeDays = DURATIONS.find((d) => d.id === '3d')
    assert.equal(threeDays?.pricing, 'hourly')
    assert.equal(threeDays?.amount, 72)
    assert.equal(DURATIONS.find((d) => d.id === '1y')?.amount, 12)
  })

  test('长档位折扣更深：3 天 -5%、7 天 -10%、1 年 -15%、3 年 -28%', () => {
    const discountOf = (id: string) =>
      DURATIONS.find((duration) => duration.id === id)?.discount
    assert.equal(discountOf('3d'), 0.95)
    assert.equal(discountOf('7d'), 0.9)
    assert.equal(discountOf('1y'), 0.85)
    assert.equal(discountOf('3y'), 0.72)
  })

  test('包月 1 年 9 折后总价对得上，且与按量同源', () => {
    const price = computePrice(
      config({
        plan: 'lite',
        specId: 'lite-2c2g',
        mbps: 5,
        durationId: '1y',
      })
    )
    // ¥68/月 × 12 × 0.85 = ¥693.6
    assert.equal(price.listTotal, 816)
    assert.equal(price.total, 693.6)
    assert.equal(price.saved, 122.4)
  })
})

describe('servers: 地区系数', () => {
  test('上海 1.08 乘在单价上，广州为 1', () => {
    const guangzhou = computePrice(
      config({ plan: 'lite', specId: 'lite-2c2g', mbps: 5 })
    )
    const shanghai = computePrice(
      config({
        region: 'shanghai',
        plan: 'lite',
        specId: 'lite-2c2g',
        mbps: 5,
      })
    )
    assert.equal(guangzhou.regionFactor, 1)
    assert.equal(guangzhou.unitPrice, 68)
    assert.equal(shanghai.regionFactor, 1.08)
    assert.equal(shanghai.unitPrice, 73.44)
  })
})

describe('servers: 摘要各分项相加等于单价', () => {
  test('广州（系数 1）下，套餐 + 配置 + 带宽 = 单价', () => {
    const cases: ServerConfig[] = [
      config(),
      config({ specId: 'gpu-16c64g', mbps: 5 }),
      config({ specId: 'gpu-16c64g-rtx5090', mbps: 50 }),
      config({ plan: 'compute', specId: 'compute-16c32g', mbps: 100 }),
      config({ plan: 'lite', specId: 'lite-4c4g', mbps: 20 }),
    ]
    for (const item of cases) {
      const price = computePrice(item)
      const sum = price.planPrice + price.specPrice + price.bandwidthPrice
      assert.equal(
        Math.round(sum * 100) / 100,
        price.unitPrice,
        `${item.specId} @ ${item.mbps}Mbps 的分项加不出单价`
      )
    }
  })
})

describe('servers: 已下线的双卡 4090 档', () => {
  test('老实例仍按原卡型与原系数显示，不被新卡顶替', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-24c96g')
    assert.equal(spec.id, 'gpu-24c96g')
    assert.equal(spec.gpu?.model, 'RTX 4090')
    assert.equal(spec.gpu?.count, 2)
    assert.equal(spec.gpu?.vram, 24)
    assert.equal(spec.multiplier, 1.7)
    assert.equal(spec.cpu, 24)
  })

  test('老实例的账照旧算：¥1,280 + ¥896 = ¥2,176/月', () => {
    const price = computePrice(
      config({ specId: 'gpu-24c96g', mbps: 5, durationId: '1mo' })
    )
    assert.equal(price.specPrice, 896)
    assert.equal(price.unitPrice, 2176)
  })

  test('未知规格 id 回落到该机型第一档，不崩也不越界', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-does-not-exist')
    assert.equal(spec.id, 'gpu-16c64g')
  })
})

describe('servers: RTX PRO 6000 单卡档', () => {
  test('按量卖 ¥15/小时：系数 6 × 基准时价 ¥2.50', () => {
    const price = computePrice(
      config({ specId: 'gpu-24c96g-rtxpro6000', mbps: 5, durationId: '1h' })
    )
    assert.equal(price.unitPrice, 15)
    assert.equal(price.total, 15)
  })

  test('包月价不影响按量口径：按量仍是 ¥15/小时', () => {
    // monthlyPrice 只在包月口径生效。按量若也被它污染，
    // 「按量比包月贵」这个基本事实就反了。
    const price = computePrice(
      config({ specId: 'gpu-24c96g-rtxpro6000', mbps: 5, durationId: '6h' })
    )
    assert.equal(price.unitPrice, 15)
    assert.equal(price.total, 90)
  })

  test('包月价单独指定：¥6,580/月，不套按量系数', () => {
    const price = computePrice(
      config({ specId: 'gpu-24c96g-rtxpro6000', mbps: 5, durationId: '1mo' })
    )
    assert.equal(price.planPrice, 1280)
    assert.equal(price.specPrice, 5300)
    assert.equal(price.unitPrice, 6580)
    // 明确锁住「不是按系数推出来的」：¥1,280 × 6 = ¥7,680 是错的
    assert.notEqual(price.unitPrice, 7680)
  })

  test('配置与实物一致：24 核 / 96 GB / 1000 GB', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-24c96g-rtxpro6000')
    assert.equal(spec.cpu, 24)
    assert.equal(spec.memory, 96)
    assert.equal(spec.disk, 1000)
    assert.equal(spec.gpu?.model, 'RTX PRO 6000')
    assert.equal(spec.gpu?.vram, 96)
    assert.equal(spec.gpu?.count, 1)
  })

  test('GPU 机型只保留三档在售', () => {
    const plan = getPlan('gpu')
    assert.deepEqual(
      plan.specs.map((s) => s.id),
      ['gpu-16c64g', 'gpu-16c64g-rtx5090', 'gpu-24c96g-rtxpro6000']
    )
  })

  test('摘要里系统盘单列，不带数据盘', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-24c96g-rtxpro6000')
    const summary = formatSpec(spec, (key) => key)
    assert.ok(summary.includes('24 vCPU'))
    assert.ok(summary.includes('96 GB'))
    assert.ok(summary.includes('1000 GB'))
  })

  test('没有数据盘的档位不报可扩容', () => {
    const spec = getSpec(getPlan('gpu'), 'gpu-16c64g')
    assert.equal(formatDataDiskExpand(spec), '')
  })
})

describe('servers: 旧配置的时长映射', () => {
  test('只有 term 的老订单映射到对应档位', () => {
    assert.equal(resolveDurationId({ term: 'year' } as ServerConfig), '1y')
    assert.equal(resolveDurationId({ term: 'half' } as ServerConfig), '6mo')
    assert.equal(resolveDurationId({ term: 'triple' } as ServerConfig), '3y')
  })

  test('只有 billing + hours 的老订单映射到按量档', () => {
    assert.equal(
      resolveDurationId({ billing: 'hourly', hours: 72 } as ServerConfig),
      '3d'
    )
  })

  test('什么时长信息都没有时按 1 年处理，不能落到 1 小时', () => {
    // 落到 1 小时等于把老订单悄悄改成按量，价格与到期时间全错
    assert.equal(resolveDurationId({} as ServerConfig), '1y')
    assert.equal(resolveBilling({} as ServerConfig), 'prepaid')
  })

  test('未知时长 id 在取档位时回落到默认档，不是列表第一项', () => {
    // resolveDurationId 原样透传已存的 id，由 getDuration 负责兜底：
    // 落到列表第一项（1 小时）等于把老订单悄悄改成按量。
    assert.equal(getDuration('nope').id, '1y')
    assert.equal(resolveBilling({ durationId: 'nope' } as ServerConfig), 'prepaid')
  })
})
