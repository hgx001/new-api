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

import en from '../../../i18n/locales/en.json'
import fr from '../../../i18n/locales/fr.json'
import ja from '../../../i18n/locales/ja.json'
import ru from '../../../i18n/locales/ru.json'
import vi from '../../../i18n/locales/vi.json'
import zh from '../../../i18n/locales/zh.json'
import { payServerOrder } from '../api'
import { renderWithI18n } from '../test-render'
import type { OwnedServer } from '../types'
import { CredentialsPanel } from './credentials-panel'

/**
 * 付款成功后的凭据展示契约。
 *
 * 用户原话：「用户选择付款后成功后显示：ip、用户、密码」。
 * 之前支付成功弹窗只有一句「可在我的服务器中查看」，
 * 用户要再点一次、翻一个 Tab 才能拿到 IP / 用户名 / 密码。
 * 现在凭据直接摆在支付成功弹窗里，并且密码是**高强度随机**的
 * （不是固定串，也不是「root / 123456」这种弱口令）。
 *
 * 这里锁住三件事：
 *   1. 弹窗里能看到 IP、用户名、密码三个字段与可复制的 SSH 命令
 *   2. 密码默认打码，但明文值确实存在于渲染结果里（可复制）
 *   3. 生成的密码满足强度要求（长度 + 四类字符齐全 + 不用易混字符）
 */

const locales = { en, zh, fr, ja, ru, vi }

function serverFor(overrides: Partial<OwnedServer> = {}): OwnedServer {
  return {
    id: 'srv-1',
    orderId: 'order-1',
    config: {
      region: 'guangzhou',
      plan: 'lite',
      specId: 'lite-2c2g',
      mbps: 20,
      durationId: '1mo',
    },
    ip: '43.142.108.151',
    username: 'ubuntu',
    password: 'Xk9$mQ2#pL7@vN4&wR6z',
    status: 'running',
    expireAt: 4102444800,
    createdAt: 1790000000,
    demo: true,
    ...overrides,
  }
}

describe('servers: 付款成功后的凭据面板', () => {
  test('IP、用户名、密码三个字段都在', async () => {
    const html = await renderWithI18n(<CredentialsPanel server={serverFor()} />)
    assert.ok(html.includes('Public IP'), '缺少公网 IP 字段')
    assert.ok(html.includes('43.142.108.151'), '没有显示公网 IP')
    assert.ok(html.includes('Username'), '缺少用户名字段')
    assert.ok(html.includes('ubuntu'), '没有显示用户名')
    assert.ok(html.includes('Password'), '缺少密码字段')
  })

  test('密码默认打码，且明文不出现在 DOM 里', async () => {
    const html = await renderWithI18n(<CredentialsPanel server={serverFor()} />)
    assert.ok(html.includes('••••••••••••'), '密码没有默认打码')
    // 打码的真正价值：明文不落在 DOM 里，截图/看源码都拿不到。
    // 明文只通过「显示」按钮或复制按钮取用。
    assert.ok(
      !html.includes('Xk9$mQ2#pL7@vN4&wR6z'),
      '打码状态下密码明文仍然出现在 DOM 里'
    )
    // 显示 / 隐藏开关必须存在，否则用户没法确认自己复制到的是什么
    assert.ok(html.includes('Toggle password visibility'), '缺少密码明文开关')
  })

  test('SSH 命令带上用户名与 IP，可一键复制', async () => {
    const html = await renderWithI18n(<CredentialsPanel server={serverFor()} />)
    assert.ok(
      html.includes('ssh ubuntu@43.142.108.151'),
      '缺少可复制的 SSH 命令'
    )
  })

  test('面板带标题与改密提示', async () => {
    const html = await renderWithI18n(<CredentialsPanel server={serverFor()} />)
    assert.ok(html.includes('Server credentials'), '缺少凭据面板标题')
    assert.ok(
      html.includes('Please change the password after your first login'),
      '缺少首次登录改密提示'
    )
  })
})

describe('servers: 开台返回的凭据', () => {
  test('公网 IP 与用户名是约定的固定值', async () => {
    const order = {
      id: 'srv_test',
      tradeNo: 'HBDTEST',
      config: serverFor().config,
      amount: 100,
      paymentMethod: 'demo',
      status: 'pending' as const,
      createdAt: Date.now(),
    }
    const result = await payServerOrder(order)
    assert.ok(result.server)
    assert.equal(result.server.ip, '43.142.108.151')
    assert.equal(result.server.username, 'ubuntu')
  })

  test('随机密码满足强度要求：20 位 + 大小写/数字/符号齐全，且不含易混字符', async () => {
    const order = {
      id: 'srv_test2',
      tradeNo: 'HBDTEST2',
      config: serverFor().config,
      amount: 100,
      paymentMethod: 'demo',
      status: 'pending' as const,
      createdAt: Date.now(),
    }
    const result = await payServerOrder(order)
    assert.ok(result.server)
    const password = result.server.password

    assert.ok(password.length >= 20, `密码太短: ${password.length}`)
    assert.match(password, /[A-Z]/, '缺大写字母')
    assert.match(password, /[a-z]/, '缺小写字母')
    assert.match(password, /[0-9]/, '缺数字')
    assert.match(password, /[^A-Za-z0-9]/, '缺符号')
    // 易混字符（0/O、1/l/I）在登录场景里最容易敲错
    assert.doesNotMatch(password, /[0O1lI]/, `密码含易混字符: ${password}`)
  })

  test('不同订单生成的密码不一样', async () => {
    const order = {
      id: 'srv_test3',
      tradeNo: 'HBDTEST3',
      config: serverFor().config,
      amount: 100,
      paymentMethod: 'demo',
      status: 'pending' as const,
      createdAt: Date.now(),
    }
    const first = await payServerOrder(order)
    const second = await payServerOrder(order)
    assert.ok(first.server && second.server)
    assert.notEqual(first.server.password, second.server.password)
  })
})

describe('servers: 凭据面板文案的 i18n 覆盖', () => {
  const CREDENTIAL_KEYS = [
    'Server credentials',
    'Your server is ready. Use the credentials below to log in right away.',
    'Please change the password after your first login. The credentials above are also listed under My servers.',
    'Public IP',
    'Username',
    'Password',
    'SSH command copied',
    'Copied to clipboard',
  ]

  for (const [locale, bundle] of Object.entries(locales)) {
    test(`${locale}: 凭据面板相关 key 都存在且已翻译`, () => {
      const table = bundle.translation as Record<string, string>
      for (const key of CREDENTIAL_KEYS) {
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
