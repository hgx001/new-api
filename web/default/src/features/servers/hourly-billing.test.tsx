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

import { payServerOrder } from './api'
import { BillingPanel } from './components/billing-panel'
import { ConfigSection } from './components/config-section'
import { MyServers } from './components/my-servers'
import { DURATIONS, getDuration } from './constants'
import {
  computePrice,
  resolveBilling,
  resolveDurationId,
  roundMoney,
} from './lib/pricing'
import { countOccurrences, renderWithI18n } from './test-render'
import type { ServerConfig } from './types'

/**
 * 服务器租赁「租用时长」维度的契约。
 *
 * 背景：页面原本是「计费方式（按小时按量 / 包月预付）+ 购买周期（半年 / 一年 /
 * 三年）」两层选择器。「包月」只是后面那组档位的统称，把它单列成一个开关，
 * 用户要先想口径再想周期，右侧凭空多一层 —— 用户明确指出这不合理。
 * 现在合成唯一的时间维度：从 1 小时排到 3 年，选中即决定用哪套计价模型。
 *
 * 锁住三件事：
 *   1. 计价：小时档按小时单价 × 小时数，月档按月价 × 月数，各自带阶梯折扣
 *   2. 兼容：旧数据（只有 term/billing/hours）映射到最接近的新档位
 *   3. 语义：到期时间跟着选中的档位走（小时档按小时，月档按 30 天/月）
 */

/** GPU 基准档 + 20Mbps + 广州 = ¥2.56/小时（2.50 机型 + 0.06 带宽） */
const GPU_HOURLY_PRICE = 2.56
/** GPU 基准档 + 20Mbps + 广州 = ¥1,325.00/月（1280 机型 + 45 带宽） */
const GPU_MONTHLY_PRICE = 1325

function configFor(overrides: Partial<ServerConfig> = {}): ServerConfig {
  return {
    region: 'guangzhou',
    plan: 'gpu',
    specId: 'gpu-16c64g',
    mbps: 20,
    durationId: '1y',
    ...overrides,
  }
}

describe('servers: 租用时长档位', () => {
  test('从 1 小时排到 3 年，是唯一的时间维度', () => {
    assert.deepEqual(
      DURATIONS.map((d) => d.id),
      ['1h', '6h', '12h', '24h', '3d', '7d', '1mo', '3mo', '6mo', '1y', '3y']
    )
  })

  test('按量档记小时数、包月档记月数', () => {
    assert.equal(getDuration('24h').pricing, 'hourly')
    assert.equal(getDuration('24h').amount, 24)
    assert.equal(getDuration('3d').pricing, 'hourly')
    assert.equal(getDuration('3d').amount, 72)
    assert.equal(getDuration('1y').pricing, 'prepaid')
    assert.equal(getDuration('1y').amount, 12)
  })

  test('长档位折扣更高（按量 3 天 -5%、7 天 -10%；包月 1 年 -15%、3 年 -28%）', () => {
    assert.equal(getDuration('3d').discount, 0.95)
    assert.equal(getDuration('7d').discount, 0.9)
    assert.equal(getDuration('1y').discount, 0.85)
    assert.equal(getDuration('3y').discount, 0.72)
  })

  test('未知档位回落到默认档，不能算出 0 时长或负数总价', () => {
    const price = computePrice(configFor({ durationId: 'nope' }))
    assert.ok(price.total > 0)
    assert.equal(price.units, 12)
  })
})

describe('servers: 按量（小时/天）档的报价', () => {
  test('小时单价 = 机型小时价 + 带宽小时加价（带宽按月加价折算）', () => {
    const price = computePrice(configFor({ durationId: '1h' }))
    assert.equal(price.billing, 'hourly')
    assert.equal(price.unit, 'hour')
    assert.equal(price.units, 1)
    assert.equal(price.unitPrice, GPU_HOURLY_PRICE)
    assert.equal(price.total, GPU_HOURLY_PRICE)
  })

  test('总价 = 小时单价 × 时长', () => {
    const price = computePrice(configFor({ durationId: '24h' }))
    assert.equal(price.total, roundMoney(GPU_HOURLY_PRICE * 24))
  })

  test('天档折成小时计价（3 天 = 72 小时）并带阶梯折扣', () => {
    const price = computePrice(configFor({ durationId: '3d' }))
    assert.equal(price.units, 72)
    assert.equal(price.listTotal, roundMoney(GPU_HOURLY_PRICE * 72))
    assert.equal(price.total, roundMoney(price.listTotal * 0.95))
    assert.equal(price.saved, roundMoney(price.listTotal - price.total))
  })

  test('规格系数参与小时单价（双卡档 = 基准 × 1.7）', () => {
    const price = computePrice(
      configFor({ durationId: '1h', specId: 'gpu-24c96g' })
    )
    assert.equal(price.unitPrice, 4.31)
  })

  test('地区系数参与小时单价（上海 1.08）', () => {
    const price = computePrice(
      configFor({ durationId: '1h', region: 'shanghai' })
    )
    assert.equal(price.unitPrice, roundMoney(GPU_HOURLY_PRICE * 1.08))
  })

  test('折合每月用于和包月价对比（730 小时/月）', () => {
    const price = computePrice(configFor({ durationId: '24h' }))
    assert.equal(price.monthlyEquivalent, roundMoney((price.total / 24) * 730))
  })
})

describe('servers: 包月档的报价', () => {
  test('总价 = 月价 × 月数 × 时长折扣', () => {
    const price = computePrice(configFor({ durationId: '1y' }))
    assert.equal(price.billing, 'prepaid')
    assert.equal(price.unit, 'month')
    assert.equal(price.units, 12)
    assert.equal(price.unitPrice, GPU_MONTHLY_PRICE)
    assert.equal(price.listTotal, roundMoney(GPU_MONTHLY_PRICE * 12))
    assert.equal(price.total, roundMoney(price.listTotal * 0.85))
    assert.equal(price.saved, roundMoney(price.listTotal - price.total))
  })

  test('半年档不打折（与改造前一致）', () => {
    const price = computePrice(configFor({ durationId: '6mo' }))
    assert.equal(price.units, 6)
    assert.equal(price.discount, 1)
    assert.equal(price.total, roundMoney(GPU_MONTHLY_PRICE * 6))
  })

  test('三年档 -28%', () => {
    const price = computePrice(configFor({ durationId: '3y' }))
    assert.equal(price.units, 36)
    assert.equal(price.total, roundMoney(price.listTotal * 0.72))
  })
})

describe('servers: 旧数据兼容', () => {
  test('旧数据只有 term 时映射到对应的新档位', () => {
    assert.equal(
      resolveDurationId({
        region: 'guangzhou',
        plan: 'gpu',
        specId: 'gpu-16c64g',
        mbps: 20,
        term: 'year',
      }),
      '1y'
    )
    assert.equal(
      resolveDurationId({
        region: 'guangzhou',
        plan: 'gpu',
        specId: 'gpu-16c64g',
        mbps: 20,
        term: 'half',
      }),
      '6mo'
    )
    assert.equal(
      resolveDurationId({
        region: 'guangzhou',
        plan: 'gpu',
        specId: 'gpu-16c64g',
        mbps: 20,
        term: 'triple',
      }),
      '3y'
    )
  })

  test('旧数据只有 billing + hours 时映射到按量档', () => {
    const legacy: ServerConfig = {
      region: 'guangzhou',
      plan: 'gpu',
      specId: 'gpu-16c64g',
      mbps: 20,
      term: 'year',
      billing: 'hourly',
      hours: 24,
    }
    assert.equal(resolveDurationId(legacy), '24h')
    assert.equal(resolveBilling(legacy), 'hourly')
    const price = computePrice(legacy)
    assert.equal(price.units, 24)
    assert.equal(price.total, roundMoney(GPU_HOURLY_PRICE * 24))
  })

  test('旧数据没有 durationId 时按包月处理（本地已存的订单）', () => {
    const legacy: ServerConfig = {
      region: 'guangzhou',
      plan: 'gpu',
      specId: 'gpu-16c64g',
      mbps: 20,
      term: 'year',
    }
    assert.equal(resolveBilling(legacy), 'prepaid')
    const price = computePrice(legacy)
    assert.equal(price.billing, 'prepaid')
    assert.equal(price.units, 12)
  })
})

describe('servers: 实例到期时间', () => {
  test('按量档到期 = 买的小时数之后', async () => {
    const order = {
      id: 'srv_test',
      tradeNo: 'HBDTEST',
      config: configFor({ durationId: '24h' }),
      amount: GPU_HOURLY_PRICE * 24,
      paymentMethod: 'demo',
      status: 'pending' as const,
      createdAt: Date.now(),
    }
    const before = Date.now()
    const result = await payServerOrder(order)
    const after = Date.now()

    assert.equal(result.success, true)
    const server = result.server
    assert.ok(server)
    const expireMs = server.expireAt * 1000
    assert.ok(
      expireMs >= before + 24 * 3600 * 1000 - 2000,
      `到期时间过早: ${new Date(expireMs).toISOString()}`
    )
    assert.ok(
      expireMs <= after + 24 * 3600 * 1000 + 2000,
      `到期时间过晚: ${new Date(expireMs).toISOString()}`
    )
  })

  test('包月档到期 = 月数 × 30 天', async () => {
    const order = {
      id: 'srv_test2',
      tradeNo: 'HBDTEST2',
      config: configFor({ durationId: '1y' }),
      amount: 100,
      paymentMethod: 'demo',
      status: 'pending' as const,
      createdAt: Date.now(),
    }
    const before = Date.now()
    const result = await payServerOrder(order)
    assert.ok(result.server)
    const expireMs = result.server.expireAt * 1000
    const expected = 12 * 30 * 24 * 3600 * 1000
    assert.ok(expireMs >= before + expected - 2000)
    assert.ok(expireMs <= Date.now() + expected + 2000)
  })
})

describe('servers: 租用时长的页面展示', () => {
  test('只有一个「租用时长」选择器，没有独立的「计费方式」开关', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '24h' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('Rental duration'))
    assert.ok(
      !html.includes('Billing method'),
      '不应再有独立的「计费方式」选择器'
    )
  })

  test('全部时长档平铺可见（1 小时 → 3 年）', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '24h' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    for (const label of [
      '1 hour',
      '6 hours',
      '12 hours',
      '24 hours',
      '3 days',
      '7 days',
      '1 month',
      '3 months',
      '6 months',
      '1 year',
      '3 years',
    ]) {
      assert.ok(html.includes(label), `时长档缺失: ${label}`)
    }
  })

  test('按量档显示「到期自动释放」说明，包月档显示「整段周期可用」', async () => {
    const hourly = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '24h' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(hourly.includes('Pay-as-you-go billing'))

    const prepaid = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '1y' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(prepaid.includes('Prepaid billing'))
  })

  test('价格区按口径显示单价与时长', async () => {
    const hourly = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '24h' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    // 小时单价是精确值（就是摘要各行相加的结果），不标「约」
    assert.ok(hourly.includes('¥2.56 / hour'), '缺少每小时单价')
    assert.ok(hourly.includes('24 hours'), '缺少总时长')
    // 折合每月是按 730 小时/月摊出来的折算值，才标「约」
    assert.ok(hourly.includes('≈ ¥1,868.80 / month'), '缺少折合每月的对比价')

    const prepaid = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '1y' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    // 包月档的月单价是精确值（未打折，= 摘要各行相加），不标「约」；
    // 折扣由时长行的「省 15%」和总价区的划线价体现
    assert.ok(prepaid.includes('¥1,325.00 / month'))
    assert.ok(prepaid.includes('1 year'))
  })

  test('时长用档位自己的名字，不能拼出「1 hours」「1 months」这种量词错误', async () => {
    const hourly = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '1h' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(hourly.includes('1 hour'))
    assert.ok(!hourly.includes('1 hours'), '出现了「1 hours」这种量词错误')

    const prepaid = await renderWithI18n(
      <BillingPanel
        config={configFor({ durationId: '1mo' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(prepaid.includes('1 month'))
    assert.ok(!prepaid.includes('1 months'), '出现了「1 months」这种量词错误')
  })

  test('套餐卡给出双口径起步价（按月 + 按小时）', async () => {
    const html = await renderWithI18n(
      <ConfigSection
        config={configFor({ durationId: '1y' })}
        onChange={() => {}}
      />
    )
    assert.ok(html.includes('From ¥1,280.00 / month'))
    assert.ok(html.includes('From ¥2.50 / hour'))
  })

  test('我的服务器卡片标出租用时长，按量实例按小时报剩余时间', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[
          {
            id: 'srv-1',
            orderId: 'order-1',
            config: configFor({ durationId: '24h' }),
            ip: '203.0.113.10',
            username: 'root',
            password: 'secret',
            status: 'running',
            expireAt: Math.floor(Date.now() / 1000) + 6 * 3600,
            createdAt: Date.now(),
            demo: false,
          },
        ]}
        onChangePassword={() => {}}
        onBrowse={() => {}}
      />
    )
    assert.ok(html.includes('24 hours'))
    assert.ok(html.includes('hours left'))
    assert.ok(!html.includes('days left'))
  })

  test('包月实例仍是「还剩 N 天」', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[
          {
            id: 'srv-2',
            orderId: 'order-2',
            config: configFor({ durationId: '1y' }),
            ip: '203.0.113.11',
            username: 'root',
            password: 'secret',
            status: 'running',
            expireAt: Math.floor(Date.now() / 1000) + 6 * 86400,
            createdAt: Date.now(),
            demo: false,
          },
        ]}
        onChangePassword={() => {}}
        onBrowse={() => {}}
      />
    )
    assert.ok(html.includes('1 year'))
    assert.ok(html.includes('days left'))
    assert.equal(countOccurrences(html, 'hours left'), 0)
  })
})

describe('servers: 价格逐项可核对', () => {
  // 用户原话：「你标的左右两边的价格都对不上，太不严谨了」。
  // 这里把「对得上」写成硬契约：
  //   ① 单价 = 裸机型价 + 规格加价 + 带宽加价，再乘地区系数
  //   ② 总价 = 单价 × 租用时长 × 时长折扣
  //   ③ 右侧摘要的「套餐」行 = 左侧套餐卡上标的机型价（同一个数）
  //   ④ 右侧摘要各行相加 = 右侧显示的单价
  const lite = (overrides: Partial<ServerConfig> = {}) =>
    configFor({
      plan: 'lite',
      specId: 'lite-2c2g',
      mbps: 20,
      region: 'guangzhou',
      durationId: '1mo',
      ...overrides,
    })

  test('单价 = 裸机型价 + 规格加价 + 带宽加价，再乘地区系数', () => {
    const price = computePrice(lite())
    assert.equal(price.planPrice, 68)
    assert.equal(price.specPrice, 0)
    assert.equal(price.bandwidthPrice, 45)
    assert.equal(price.regionFactor, 1)
    assert.equal(price.unitPrice, 113)
    assert.equal(
      roundMoney(
        (price.planPrice + price.specPrice + price.bandwidthPrice) *
          price.regionFactor
      ),
      price.unitPrice
    )
  })

  test('总价 = 单价 × 时长 × 折扣', () => {
    for (const durationId of ['1h', '24h', '3d', '1mo', '1y', '3y']) {
      const price = computePrice(lite({ durationId }))
      assert.equal(
        price.total,
        roundMoney(price.unitPrice * price.units * price.discount),
        `${durationId} 的总价与单价×时长×折扣对不上`
      )
    }
  })

  test('摘要逐行给出分项，各行相加正好是显示的单价', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={lite()}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    // 套餐行 = 裸机型价（与左侧套餐卡同一个数）
    assert.ok(html.includes('¥68.00 / month'), '缺少套餐行的机型价')
    // 基准规格没有规格加价
    assert.ok(html.includes('Base spec'), '基准规格未标明')
    // 带宽行
    assert.ok(html.includes('¥45.00 / month'), '缺少带宽行的加价')
    // 单价行 = 68 + 0 + 45
    assert.ok(html.includes('¥113.00 / month'), '缺少单价行')
    // 总价
    assert.ok(html.includes('¥113.00'), '缺少总价')
  })

  test('左侧套餐卡的价格与右侧「套餐」行是同一个数', async () => {
    const card = await renderWithI18n(
      <ConfigSection config={lite()} onChange={() => {}} />
    )
    const cardPrice = card.match(/From (¥[\d,.]+) \/ month/)?.[1]
    assert.ok(cardPrice, '套餐卡上没有解析到机型价')

    const panel = await renderWithI18n(
      <BillingPanel
        config={lite()}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(
      panel.includes(cardPrice),
      `右侧摘要里没有出现左侧卡片上的机型价 ${cardPrice}`
    )
  })

  test('规格按钮标出该档机型价，且 = 套餐行 + 规格加价', async () => {
    const cfg = lite({ specId: 'lite-4c4g' })
    const card = await renderWithI18n(
      <ConfigSection config={cfg} onChange={() => {}} />
    )
    const panel = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    // lite-4c4g 系数 1.7 → 68 × 1.7 = 115.60
    assert.ok(card.includes('¥115.60 / month'), '规格按钮没标该档机型价')
    assert.ok(panel.includes('¥68.00 / month'), '缺少套餐行的裸机型价')
    assert.ok(panel.includes('+¥47.60 / month'), '缺少规格加价')
    assert.ok(panel.includes('¥160.60 / month'), '单价与各行之和对不上')
  })

  test('地区系数单列显示，乘完正好是单价', async () => {
    const cfg = lite({ region: 'shanghai' })
    const price = computePrice(cfg)
    assert.equal(price.regionFactor, 1.08)
    assert.equal(price.unitPrice, 122.04)

    const html = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('×1.08'), '地区系数没有显示')
    assert.ok(html.includes('¥122.04 / month'), '乘完地区系数后单价对不上')
  })

  test('时长折扣单列显示，折完正好是总价', async () => {
    const cfg = lite({ durationId: '1y' })
    const price = computePrice(cfg)
    assert.equal(price.discount, 0.85)
    assert.equal(price.total, roundMoney(price.unitPrice * 12 * 0.85))

    const html = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('Save 15%'), '时长折扣没有显示')
    // 113 × 12 × 0.85 = 1152.60
    assert.ok(html.includes('¥1,152.60'), '折后总价对不上')
  })

  test('按量档：小时单价 = 裸机型时价 + 带宽时价，各行相加一致', async () => {
    const cfg = lite({ durationId: '24h' })
    const price = computePrice(cfg)
    assert.equal(price.planPrice, 0.16)
    assert.equal(price.bandwidthPrice, 0.06)
    assert.equal(price.unitPrice, 0.22)

    const html = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('¥0.16 / hour'), '缺少套餐行的小时价')
    assert.ok(html.includes('+¥0.06 / hour'), '缺少带宽行的小时加价')
    assert.ok(html.includes('¥0.22 / hour'), '小时单价与各行之和对不上')
  })
})

describe('servers: RTX 5090 档的定价与左右对账', () => {
  // 用户要求 GPU 型新增一档 RTX 5090，定价 ¥3.50/小时。
  //
  // 关键约束：**两个口径必须同源**。规格只带一个 multiplier，
  // ¥3.50 ÷ 基准时价 ¥2.50 = 1.4，包月价由同一个系数算出
  // （¥1,280 × 1.4 = ¥1,792/月）。所以按量/包月的比值仍锁定在
  // 该机型的 1.43 倍，不会出现「按量按一个比价、包月按另一个比价」。
  const SPEC_ID = 'gpu-16c64g-rtx5090'

  test('按量口径：机型价 ¥3.50/小时，规格加价 ¥1.00，各行相加 = 单价', () => {
    const price = computePrice(configFor({ durationId: '1h', specId: SPEC_ID }))
    assert.equal(price.planPrice, 2.5)
    assert.equal(price.specPrice, 1)
    assert.equal(price.bandwidthPrice, 0.06)
    assert.equal(price.unitPrice, 3.56)
    assert.equal(price.total, 3.56)
  })

  test('包月口径与按量同源：¥1,792/月，按量/包月比值仍是 1.43 倍', () => {
    const price = computePrice(
      configFor({ durationId: '1mo', specId: SPEC_ID })
    )
    assert.equal(price.planPrice, 1280)
    assert.equal(price.specPrice, 512)
    assert.equal(price.bandwidthPrice, 45)
    assert.equal(price.unitPrice, 1837)
    // ¥3.50 × 730 = ¥2,555；¥2,555 ÷ ¥1,792 ≈ 1.43
    // 与基准档（¥2.50 × 730 ÷ ¥1,280 ≈ 1.43）完全一致
    assert.equal(roundMoney((3.5 * 730) / 1792), roundMoney((2.5 * 730) / 1280))
  })

  test('左侧规格按钮标的价 = 右侧摘要的「套餐」行 + 「配置」行（按量）', async () => {
    const cfg = configFor({ durationId: '1h', specId: SPEC_ID })
    const card = await renderWithI18n(
      <ConfigSection config={cfg} onChange={() => {}} />
    )
    assert.ok(card.includes('¥3.50 / hour'), '规格按钮没标 ¥3.50/小时')

    const panel = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(panel.includes('¥2.50 / hour'), '缺少套餐行的裸机型时价')
    assert.ok(panel.includes('+¥1.00 / hour'), '缺少规格加价行')
    assert.ok(panel.includes('¥3.56 / hour'), '小时单价与各行之和对不上')
  })

  test('左侧规格按钮标的价 = 右侧摘要的「套餐」行 + 「配置」行（包月）', async () => {
    const cfg = configFor({ durationId: '1mo', specId: SPEC_ID })
    const card = await renderWithI18n(
      <ConfigSection config={cfg} onChange={() => {}} />
    )
    assert.ok(card.includes('¥1,792.00 / month'), '规格按钮没标 ¥1,792.00/月')

    const panel = await renderWithI18n(
      <BillingPanel
        config={cfg}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(panel.includes('¥1,280.00 / month'), '缺少套餐行的裸机型月价')
    assert.ok(panel.includes('+¥512.00 / month'), '缺少规格加价行')
    assert.ok(panel.includes('¥1,837.00 / month'), '月单价与各行之和对不上')
  })
})

describe('servers: 租用时长相关文案的 i18n 覆盖', () => {
  const DURATION_KEYS = [
    'Rental duration',
    'Pay-as-you-go',
    'Prepaid',
    'Pay-as-you-go billing: the instance is released automatically when the purchased time runs out. Unused time is not refundable.',
    'Prepaid billing: the instance stays available for the whole period you paid for.',
    '1 hour',
    '6 hours',
    '12 hours',
    '24 hours',
    '3 days',
    '7 days',
    '1 month',
    '3 months',
    '6 months',
    '1 year',
    '3 years',
    'From {{price}} / hour',
    'From {{price}} / month',
    '{{price}} / hour',
    '{{price}} / month',
    '+{{price}} / hour',
    '≈ +{{price}} / hour',
    '≈ {{amount}} / month',
    'Base spec',
    'Save {{percent}}%',
    'Plan prices are for the base spec and exclude bandwidth and regional pricing.',
    'Dedicated inbound and outbound bandwidth, charged monthly',
    'The rows above add up to the unit price; the unit price times the rental duration is the total.',
    '{{hours}} hours left',
    '{{days}} days left',
    'Can I pay by the hour?',
    'Pick a region, a plan, the specs and the bandwidth you need. Choose how long to rent — from a single hour up to three years — and get your server credentials right after the payment succeeds.',
    'Yes. The rental duration list runs from a single hour all the way to three years, so you can take one hour for a quick job or a prepaid term for a long-running service. Durations of a day or more get a small discount, and a pay-as-you-go instance is released automatically when the purchased time runs out.',
  ]

  for (const locale of ['en', 'zh', 'fr', 'ja', 'ru', 'vi'] as const) {
    test(`${locale}: 租用时长相关 key 都存在且已翻译`, async () => {
      const table = (
        await import(`../../i18n/locales/${locale}.json`, {
          with: { type: 'json' },
        })
      ).default.translation as Record<string, string>
      for (const key of DURATION_KEYS) {
        const value = table[key]
        assert.ok(
          typeof value === 'string' && value.length > 0,
          `${locale} 缺少 key: ${key}`
        )
        if (locale !== 'en') {
          assert.notEqual(value, key, `${locale} 的 ${key} 未翻译`)
        }
      }
    })
  }
})
