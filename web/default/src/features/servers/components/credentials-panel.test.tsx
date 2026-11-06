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
import { renderWithI18n } from '../test-render'
import type { ServerInstance } from '../types'
import { CredentialsPanel } from './credentials-panel'

/**
 * 付款成功后的凭据展示契约。
 *
 * 用户原话：「用户选择付款后成功后显示：ip、用户、密码」。
 *
 * 这里锁住三件事：
 *   1. 弹窗里能看到 IP、用户名、密码三个字段与可复制的 SSH 命令
 *   2. 密码默认打码，明文不落进 DOM（截图/看源码都拿不到）
 *   3. 未接入真实开台时必须自曝「Simulated instance」
 *
 * 密码强度、随机性、只存密文由服务端保证，锁在
 * `model/server_instance_test.go`，不在前端重复断言。
 */

const locales = { en, zh, fr, ja, ru, vi }

function serverFor(overrides: Partial<ServerInstance> = {}): ServerInstance {
  return {
    id: 1,
    name: 'ns-1',
    status: 'running',
    ip: '43.142.108.151',
    username: 'ubuntu',
    password: 'Xk9$mQ2#pL7@vN4&wR6z',
    password_readable: true,
    config: {
      region: 'guangzhou',
      plan: 'lite',
      specId: 'lite-2c2g',
      mbps: 20,
      durationId: '1mo',
    },
    spec: {
      id: 'lite-2c2g',
      cpu: 2,
      memory: 2,
      disk: 40,
      multiplier: 1,
    },
    region: 'guangzhou',
    plan: 'lite',
    mbps: 20,
    billing: 'prepaid',
    unit_price: 60,
    paid_amount: 60,
    refunded_amount: 0,
    provision_mode: 'simulated',
    expire_at: 4102444800,
    created_at: 1790000000,
    released_at: 0,
    remaining_seconds: 86_400,
    order_id: 1,
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

  test('模拟开台必须自曝身份，不能冒充真实机器', async () => {
    const simulated = await renderWithI18n(
      <CredentialsPanel server={serverFor()} />
    )
    assert.ok(
      simulated.includes('Simulated instance'),
      '模拟实例没有标注，用户会以为拿到了真实机器'
    )

    const live = await renderWithI18n(
      <CredentialsPanel server={serverFor({ provision_mode: 'live' })} />
    )
    assert.ok(!live.includes('Simulated instance'), '真实开台不该带模拟徽标')
  })

  test('密码不可回显时给出解释，而不是渲染空字段', async () => {
    const html = await renderWithI18n(
      <CredentialsPanel
        server={serverFor({
          password: '',
          password_readable: false,
        })}
      />
    )
    assert.ok(
      html.includes('Password cannot be displayed'),
      '密码不可读时没有给出任何解释'
    )
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
    'Simulated instance',
    'Password cannot be displayed. Reset it from My servers before logging in.',
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
