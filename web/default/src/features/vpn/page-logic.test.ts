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
  ALIPAY_CHECKOUT_HOSTS,
  POLL_FAST_MS,
  POLL_SLOW_MS,
  daysLeft,
  formatDate,
  formatDateTime,
  isTrustedAlipayUrl,
  resolvePollIntervalMs,
  resolveVpnState,
} from './page-logic'

/**
 * VPN 产品页的纯逻辑契约。
 *
 * 这些函数直接决定两件跟钱有关的事：
 *   1. 用户被整页跳转到哪里（跳错 = 钓鱼，付出去的钱到不了支付宝）
 *   2. 待支付订单还要不要继续轮询（停了 = 迟到的付款通知没人接，用户付了钱拿不到密钥）
 */

describe('vpn: 支付宝跳转信任', () => {
  test('只放行支付宝官方收银台域名', () => {
    assert.equal(
      isTrustedAlipayUrl('https://openapi.alipay.com/gateway.do?xxx=1'),
      true
    )
    assert.equal(
      isTrustedAlipayUrl('https://openapi.alipaydev.com/gateway.do?xxx=1'),
      true
    )
  })

  test('仿冒域名一律拒绝：多级后缀、同前缀不同域都不算', () => {
    // 攻击者最常见的仿冒：把官方域名当子域或路径
    assert.equal(
      isTrustedAlipayUrl('https://openapi.alipay.com.evil.com/gateway.do'),
      false,
      '官方域名被当成子域时必须拒绝'
    )
    assert.equal(
      isTrustedAlipayUrl('https://alipay.com.evil.com/gateway.do'),
      false
    )
    assert.equal(
      isTrustedAlipayUrl('https://evil.com/openapi.alipay.com'),
      false,
      '官方域名出现在路径里不算数'
    )
    assert.equal(
      isTrustedAlipayUrl('https://openapi.alipay.com.evil.com/gateway.do'),
      false
    )
  })

  test('非 https 与畸形 URL 一律拒绝', () => {
    assert.equal(
      isTrustedAlipayUrl('http://openapi.alipay.com/gateway.do'),
      false,
      'http 明文跳转不允许'
    )
    assert.equal(isTrustedAlipayUrl('javascript:alert(1)'), false)
    assert.equal(isTrustedAlipayUrl(''), false)
    assert.equal(isTrustedAlipayUrl('not a url'), false)
  })

  test('白名单本身只含支付宝官方两个域名', () => {
    assert.deepEqual([...ALIPAY_CHECKOUT_HOSTS].sort(), [
      'openapi.alipay.com',
      'openapi.alipaydev.com',
    ])
  })
})

describe('vpn: 页面状态机', () => {
  test('有密钥就是已开通，优先级最高', () => {
    assert.equal(
      resolveVpnState({ hasKey: true, isExpired: false, hasPending: false }),
      'active'
    )
    // 续费中的老用户仍然显示已开通，不能被 pending 覆盖
    assert.equal(
      resolveVpnState({ hasKey: true, isExpired: false, hasPending: true }),
      'active'
    )
  })

  test('已到期其次：哪怕还有 pending 单也先告知到期', () => {
    assert.equal(
      resolveVpnState({ hasKey: false, isExpired: true, hasPending: true }),
      'expired'
    )
  })

  test('有待支付单显示待支付', () => {
    assert.equal(
      resolveVpnState({ hasKey: false, isExpired: false, hasPending: true }),
      'pending'
    )
  })

  test('什么都没有就是未购买', () => {
    assert.equal(
      resolveVpnState({ hasKey: false, isExpired: false, hasPending: false }),
      'none'
    )
  })
})

describe('vpn: 待支付订单的轮询策略', () => {
  test('刚下单用 3 秒快轮询', () => {
    assert.equal(resolvePollIntervalMs(0), POLL_FAST_MS)
    assert.equal(resolvePollIntervalMs(59), POLL_FAST_MS)
  })

  test('超过 30 分钟降到 30 秒，但仍然在轮询', () => {
    assert.equal(resolvePollIntervalMs(30 * 60 + 1), POLL_SLOW_MS)
    assert.equal(resolvePollIntervalMs(24 * 3600), POLL_SLOW_MS)
  })

  test('任何输入都不会返回 0 / 负数：停了轮询就等于放弃这一单', () => {
    for (const age of [-1, 0, 1, 1800, 1801, 1e9]) {
      const ms = resolvePollIntervalMs(age)
      assert.ok(
        ms >= POLL_FAST_MS,
        `pendingAgeSec=${age} 得到 ${ms}ms，轮询不能停`
      )
    }
  })
})

describe('vpn: 到期时间展示', () => {
  test('剩余天数向下取整，到期后归零不显示负数', () => {
    const nowSec = Math.floor(Date.now() / 1000)
    assert.equal(daysLeft(nowSec + 3 * 86400 + 60), 4)
    assert.equal(daysLeft(nowSec + 86400), 1)
    assert.equal(daysLeft(nowSec - 86400), 0)
    assert.equal(daysLeft(0), 0)
  })

  test('没有时间戳时显示占位符，不显示 1970', () => {
    assert.equal(formatDate(0), '-')
    assert.equal(formatDateTime(0), '-')
  })

  test('时间戳按秒解释并格式化为本地时间', () => {
    // 用本地时间构造，避免测试机时区影响断言（toLocaleString 按本机时区格式化）
    const ts = Math.floor(new Date(2026, 9, 25, 8, 0, 0).getTime() / 1000)
    assert.match(formatDate(ts), /2026/)
    assert.match(formatDate(ts), /10/)
    assert.match(formatDate(ts), /25/)
    assert.match(formatDateTime(ts), /08:00/)
  })
})
