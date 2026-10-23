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

import { createInstance } from 'i18next'
import { renderToStaticMarkup } from 'react-dom/server'
import { I18nextProvider, initReactI18next } from 'react-i18next'

import en from '../../../i18n/locales/en.json'
import fr from '../../../i18n/locales/fr.json'
import ja from '../../../i18n/locales/ja.json'
import ru from '../../../i18n/locales/ru.json'
import vi from '../../../i18n/locales/vi.json'
import zh from '../../../i18n/locales/zh.json'
import { PLANS, formatGpu, formatSpec, planGpuSummary } from '../constants'
import type { OwnedServer, PlanId, ServerConfig, ServerPlan } from '../types'
import { BillingPanel } from './billing-panel'
import { ConfigSection } from './config-section'
import { MyServers } from './my-servers'

/**
 * 服务器租赁页「GPU 型」的显卡展示契约。
 *
 * 背景：用户反馈「GPU 型套餐的配置完全没有显卡型号/显存」——
 * ServerSpec 当初只有 cpu/memory/disk，数据模型里根本没有显卡字段，
 * 于是套餐卡、规格选择、规格摘要、购买摘要、订单确认全都没地方显示显卡。
 *
 * 这里用 SSR 渲染真实组件（不依赖浏览器交互），逐个展示位断言：
 * 显卡型号 + 显存必须在，缺一不可；非 GPU 机型不能被误加显卡行。
 */

const locales = { en, zh, fr, ja, ru, vi }

async function renderWithI18n(
  node: React.ReactNode,
  locale: 'en' | 'zh' = 'en'
) {
  const i18n = createInstance()
  await i18n.use(initReactI18next).init({
    lng: locale,
    fallbackLng: 'en',
    resources: {
      en: { translation: en.translation },
      zh: { translation: zh.translation },
    },
    interpolation: { escapeValue: false },
  })
  return renderToStaticMarkup(
    <I18nextProvider i18n={i18n}>{node}</I18nextProvider>
  )
}

/** 统计子串在 HTML 中出现的次数（用于区分「套餐卡徽标」与「规格按钮/摘要行」） */
function countOccurrences(html: string, needle: string): number {
  return html.split(needle).length - 1
}

function planById(id: PlanId): ServerPlan {
  const plan = PLANS.find((p) => p.id === id)
  assert.ok(plan, `unknown plan: ${id}`)
  return plan
}

function configFor(planId: PlanId, specId?: string): ServerConfig {
  const plan = planById(planId)
  return {
    region: 'guangzhou',
    plan: plan.id,
    specId: specId ?? plan.specs[0].id,
    mbps: 20,
    term: 'year',
  }
}

describe('servers: GPU 规格的展示口径', () => {
  test('formatGpu 输出「卡数× 卡型 显存」，显存必须带在后面', () => {
    assert.equal(
      formatGpu({ model: 'RTX 4090', vram: 24, count: 1 }),
      '1× RTX 4090 24GB'
    )
    assert.equal(
      formatGpu({ model: 'RTX 4090', vram: 24, count: 2 }),
      '2× RTX 4090 24GB'
    )
  })

  test('formatGpu 对没有显卡的规格返回空串（而不是 undefined）', () => {
    assert.equal(formatGpu(undefined), '')
  })

  test('formatSpec 只描述 CPU/内存/磁盘，显卡单独一行', () => {
    assert.equal(
      formatSpec(planById('general').specs[0]),
      '4 vCPU · 8 GB · 100 GB'
    )
  })

  test('GPU 型套餐的每个规格都带显卡与显存', () => {
    for (const spec of planById('gpu').specs) {
      assert.ok(spec.gpu, `spec ${spec.id} 缺少显卡配置`)
      assert.ok(spec.gpu.vram > 0, `spec ${spec.id} 缺少显存`)
    }
  })

  test('planGpuSummary 取套餐基准档的显卡，非 GPU 机型为空', () => {
    assert.equal(planGpuSummary(planById('gpu')), '1× RTX 4090 24GB')
    assert.equal(planGpuSummary(planById('general')), '')
  })
})

describe('servers: GPU 套餐在配置步骤的展示', () => {
  test('套餐卡徽标显示显卡型号与显存', async () => {
    const html = await renderWithI18n(
      <ConfigSection config={configFor('gpu')} onChange={() => {}} />
    )
    assert.ok(html.includes('1× RTX 4090 24GB'), 'GPU 套餐卡缺少显卡徽标')
  })

  test('规格按钮每一档都带显卡型号与显存', async () => {
    const html = await renderWithI18n(
      <ConfigSection config={configFor('gpu')} onChange={() => {}} />
    )
    assert.ok(html.includes('16 vCPU · 32 GB · 500 GB'))
    assert.ok(html.includes('24 vCPU · 48 GB · 1000 GB'))
    // 单卡文案出现 3 次：套餐卡徽标 + 基准档规格按钮 + 规格摘要行
    assert.equal(countOccurrences(html, '1× RTX 4090 24GB'), 3)
    // 双卡文案出现 1 次：只有双卡规格按钮
    assert.equal(countOccurrences(html, '2× RTX 4090 24GB'), 1)
  })

  test('规格摘要行显示显卡（带 Gpu 图标），步骤提示提到显卡', async () => {
    const html = await renderWithI18n(
      <ConfigSection config={configFor('gpu')} onChange={() => {}} />
    )
    assert.ok(
      html.includes('vCPU, memory, system disk and graphics card'),
      '步骤 3 提示未提到显卡'
    )
    assert.ok(html.includes('lucide-gpu'), '规格摘要行缺少显卡图标')
  })

  test('非 GPU 机型的提示仍是旧文案，不会被误加显卡', async () => {
    const html = await renderWithI18n(
      <ConfigSection config={configFor('general')} onChange={() => {}} />
    )
    assert.ok(html.includes('vCPU, memory and system disk'))
    assert.ok(!html.includes('vCPU, memory, system disk and graphics card'))
    // 显卡文案此时只剩 GPU 型套餐卡的徽标（1 次），
    // 规格按钮与规格摘要行都没有显卡
    assert.equal(countOccurrences(html, '1× RTX 4090 24GB'), 1)
    assert.equal(countOccurrences(html, '2× RTX 4090 24GB'), 0)
  })
})

describe('servers: 购买摘要的显卡行', () => {
  test('GPU 套餐的购买摘要单列一行显卡（型号 + 显存）', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor('gpu')}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('Graphics card'), '购买摘要缺少显卡行')
    assert.ok(html.includes('1× RTX 4090 24GB'))
  })

  test('切换到双卡规格后，购买摘要同步显示双卡', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor('gpu', 'gpu-24c48g')}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(html.includes('2× RTX 4090 24GB'))
  })

  test('非 GPU 机型的购买摘要没有显卡行', async () => {
    const html = await renderWithI18n(
      <BillingPanel
        config={configFor('general')}
        onChange={() => {}}
        onCheckout={() => {}}
        requiresLogin={false}
      />
    )
    assert.ok(!html.includes('Graphics card'))
    assert.ok(!html.includes('RTX 4090'))
  })
})

describe('servers: 我的服务器卡片上的显卡', () => {
  const ownedGpuServer: OwnedServer = {
    id: 'server-gpu-1',
    orderId: 'order-gpu-1',
    config: configFor('gpu'),
    ip: '203.0.113.10',
    username: 'root',
    password: 'secret',
    status: 'running',
    expireAt: 4102444800,
    createdAt: 1790000000,
    demo: false,
  }

  test('已购 GPU 实例的卡片描述行带显卡型号与显存', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[ownedGpuServer]}
        onChangePassword={() => {}}
        onBrowse={() => {}}
      />
    )
    assert.ok(html.includes('1× RTX 4090 24GB'))
    assert.ok(html.includes('lucide-gpu'))
  })

  test('已购非 GPU 实例的卡片不会多出显卡', async () => {
    const html = await renderWithI18n(
      <MyServers
        servers={[
          { ...ownedGpuServer, id: 'server-1', config: configFor('general') },
        ]}
        onChangePassword={() => {}}
        onBrowse={() => {}}
      />
    )
    assert.ok(!html.includes('RTX 4090'))
    assert.ok(!html.includes('lucide-gpu'))
  })
})

describe('servers: 显卡相关文案的 i18n 覆盖', () => {
  const GPU_KEYS = [
    'Graphics card',
    'vCPU, memory, system disk and graphics card',
    'Which GPU should I choose for my workload?',
  ]

  for (const [locale, bundle] of Object.entries(locales)) {
    test(`${locale}: 显卡相关 key 都存在且已翻译`, () => {
      const table = bundle.translation as Record<string, string>
      for (const key of GPU_KEYS) {
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
