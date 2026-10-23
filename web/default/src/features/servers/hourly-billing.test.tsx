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
import { HOURLY_DURATIONS } from './constants'
import {
  computePrice,
  resolveBilling,
  resolveHours,
  roundMoney,
} from './lib/pricing'
import { countOccurrences, renderWithI18n } from './test-render'
import type { ServerConfig } from './types'

/**
 * 服务器租赁「按小时按量」计费的契约。
 *
 * 背景：原来只有包月预付（半年 / 一年 / 三年），GPU 算力的真实场景里
 * 「只想跑几个小时」是高频需求（临时训练、一次性推理），
 * 只给包月等于把这类用户挡在门外。
 *
 * 这里锁住三件事：
 *   1. 计价：小时单价 × 时长 × 时长折扣，规格/带宽/地区系数都要参与
 *   2. 兼容：旧数据（本地已存的订单，没有 billing 字段）仍按包月处理
 *   3. 语义：按小时买的实例，到期时间是「小时数之后」而不是「月数之后」
 */

function configFor(overrides: Partial<ServerConfig> = {}): ServerConfig {
  return {
    region: 'guangzhou',
    plan: 'gpu',
    specId: 'gpu-16c32g',
    mbps: 20,
    term: 'year',
    billing: 'prepaid',
    hours: 1,
    ...overrides,
  }
}

/** GPU 基准档 + 20Mbps + 广州 = ¥2.56/小时（2.50 机型 + 0.06 带宽） */
const GPU_HOURLY_PRICE = 2.56

describe('servers: 按小时计费的报价模型', () => {
  test('小时单价 = 机型小时价 + 带宽小时加价（带宽按月加价折算）', () => {
    const price = computePrice(configFor({ billing: 'hourly', hours: 1 }))
    assert.equal(price.billing, 'hourly')
    assert.equal(price.unit, 'hour')
    assert.equal(price.units, 1)
    assert.equal(price.unitPrice, GPU_HOURLY_PRICE)
    assert.equal(price.total, GPU_HOURLY_PRICE)
  })

  test('总价 = 小时单价 × 时长', () => {
    const price = computePrice(configFor({ billing: 'hourly', hours: 24 }))
    assert.equal(price.total, roundMoney(GPU_HOURLY_PRICE * 24))
    assert.equal(price.units, 24)
  })

  test('时长越长折扣越高，优惠金额单列出来', () => {
    const week = computePrice(configFor({ billing: 'hourly', hours: 168 }))
    assert.equal(week.discount, 0.9)
    assert.equal(week.listTotal, roundMoney(GPU_HOURLY_PRICE * 168))
    assert.equal(week.total, roundMoney(week.listTotal * 0.9))
    assert.equal(week.saved, roundMoney(week.listTotal - week.total))
    assert.ok(week.saved > 0)
  })

  test('规格系数参与小时单价（双卡档 = 基准 × 1.7）', () => {
    const price = computePrice(
      configFor({ billing: 'hourly', hours: 1, specId: 'gpu-24c48g' })
    )
    assert.equal(price.unitPrice, 4.31)
  })

  test('地区系数参与小时单价（上海 1.08）', () => {
    const price = computePrice(
      configFor({ billing: 'hourly', hours: 1, region: 'shanghai' })
    )
    assert.equal(price.unitPrice, roundMoney(GPU_HOURLY_PRICE * 1.08))
  })

  test('折合每月用于和包月价对比（730 小时/月）', () => {
    const price = computePrice(configFor({ billing: 'hourly', hours: 24 }))
    assert.equal(price.monthlyEquivalent, roundMoney((price.total / 24) * 730))
  })

  test('包月口径不受影响：总价 = 月价 × 月数 × 周期折扣', () => {
    const price = computePrice(configFor({ billing: 'prepaid' }))
    assert.equal(price.billing, 'prepaid')
    assert.equal(price.unit, 'month')
    assert.equal(price.units, 12)
    assert.equal(price.discount, 0.85)
    assert.equal(price.total, roundMoney(price.unitPrice * 12 * 0.85))
  })

  test('旧数据没有 billing 字段时按包月处理（本地已存的订单）', () => {
    const legacy: ServerConfig = {
      region: 'guangzhou',
      plan: 'gpu',
      specId: 'gpu-16c32g',
      mbps: 20,
      term: 'year',
    }
    assert.equal(resolveBilling(legacy), 'prepaid')
    assert.equal(resolveHours(legacy), 1)
    const price = computePrice(legacy)
    assert.equal(price.billing, 'prepaid')
    assert.equal(price.units, 12)
  })

  test('时长档从 1 小时到 7 天，且时长递增', () => {
    const hours = HOURLY_DURATIONS.map((d) => d.hours)
    assert.deepEqual(hours, [1, 6, 12, 24, 72, 168])
    for (let i = 1; i < hours.length; i++) {
      assert.ok(hours[i] > hours[i - 1])
    }
  })

  test('未知时长退回最短档，不能算出 0 小时或负数总价', () => {
    const price = computePrice(configFor({ billing: 'hourly', hours: 999 }))
    assert.equal(price.units, 1)
    assert.ok(price.total > 0)
  })
})

describe('servers: 按小时实例的到期时间', () => {
  test('到期时间是「购买的小时数之后」，不是按月折算', async () => {
    const order = {
      id: 'srv_test',
      tradeNo: 'HBDTEST',
      config: configFor({ billing: 'hourly', hours: 24 }),
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

  test('包月实例的到期时间仍是 months × 30 天', async () => {
    const order = {
      id: 'srv_test2',
      tradeNo: 'HBDTEST2',
      config: configFor({ billing: 'prepaid', term: 'year' }),
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

describe('servers: 按小时计费的页面展示', () => {
  test('购买面板出现计费方式切换与租用时长选择器', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor({ billing: 'hourly', hours: 24 })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('Billing method'), '缺少计费方式切换')
    assert.ok(html.includes('Pay as you go (hourly)'))
    assert.ok(html.includes('Prepaid monthly'))
    assert.ok(html.includes('Rental duration'), '缺少租用时长选择器')
    // 时长档：1/6/12/24/72/168 小时
    for (const label of [
      '1 hour',
      '6 hours',
      '12 hours',
      '24 hours',
      '3 days',
      '7 days',
    ]) {
      assert.ok(html.includes(label), `时长档缺失: ${label}`)
    }
  })

  test('按小时时价格区显示单价与总时长，并说明到期自动释放', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor({ billing: 'hourly', hours: 24 })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('≈ ¥2.56 / hour'), '缺少每小时单价')
    assert.ok(html.includes('24 hours'), '缺少总时长')
    assert.ok(
      html.includes(
        'The instance is released automatically when the purchased hours run out'
      ),
      '缺少「到期自动释放」说明'
    )
  })

  test('包月模式显示购买周期，不出现租用时长', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor({ billing: 'prepaid' })}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('Term'))
    assert.ok(!html.includes('Rental duration'))
    assert.ok(!html.includes('7 days'))
  })

  test('套餐卡的起步价跟着计费方式走', async () => {
    const hourly = await renderWithI18n(
      <ConfigSection
        config={configFor({ billing: 'hourly', hours: 1 })}
        onChange={() => {}}
      />
    )
    assert.ok(hourly.includes('From ¥2.50 / hour'), 'GPU 套餐卡缺少小时价')
    assert.ok(!hourly.includes('From ¥1,280.00 / month'))

    const prepaid = await renderWithI18n(
      <ConfigSection
        config={configFor({ billing: 'prepaid' })}
        onChange={() => {}}
      />
    )
    assert.ok(prepaid.includes('From ¥1,280.00 / month'))
    assert.ok(!prepaid.includes('From ¥2.50 / hour'))
  })

  test('我的服务器卡片标出计费方式，按小时实例按小时报剩余时间', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[
          {
            id: 'srv-1',
            orderId: 'order-1',
            config: configFor({ billing: 'hourly', hours: 24 }),
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
    assert.ok(html.includes('Pay as you go (hourly)'))
    assert.ok(html.includes('hours left'))
    assert.ok(!html.includes('days left'))
  })

  test('非按小时实例仍是「还剩 N 天」', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[
          {
            id: 'srv-2',
            orderId: 'order-2',
            config: configFor({ billing: 'prepaid' }),
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
    assert.ok(html.includes('Prepaid monthly'))
    assert.ok(html.includes('days left'))
    assert.equal(countOccurrences(html, 'hours left'), 0)
  })
})

describe('servers: 按小时相关文案的 i18n 覆盖', () => {
  const HOURLY_KEYS = [
    'Billing method',
    'Pay as you go (hourly)',
    'Prepaid monthly',
    'Rental duration',
    'From {{price}} / hour',
    '≈ {{amount}} / hour',
    '{{hours}} hours left',
    '1 hour',
    '6 hours',
    '12 hours',
    '24 hours',
    '3 days',
    '7 days',
    'Can I pay by the hour?',
  ]

  for (const locale of ['en', 'zh', 'fr', 'ja', 'ru', 'vi'] as const) {
    test(`${locale}: 按小时相关 key 都存在且已翻译`, async () => {
      const table = (
        await import(`../../i18n/locales/${locale}.json`, {
          with: { type: 'json' },
        })
      ).default.translation as Record<string, string>
      for (const key of HOURLY_KEYS) {
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
