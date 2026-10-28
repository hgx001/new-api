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
import { beforeEach, describe, test } from 'node:test'

import { useAuthStore } from '@/stores/auth-store'

import { useServerStore } from './store'
import type { OwnedServer, ServerOrder } from './types'

/**
 * 服务器本地状态的闭环契约。
 *
 * 这一层是「凭据安全」的底线：演示态把实例凭据（公网 IP + root 密码）
 * 存进了浏览器 localStorage，如果没有账号隔离，同一台电脑上换账号登录
 * 就能直接看到上一个账号的密码。
 *
 * 这里锁住三件事：
 *   1. 每条记录都带 ownerUid，读的时候只取当前账号自己的
 *   2. 同一笔订单重复结算只生成一台实例（回调重放不能凭空多出机器）
 *   3. 支付失败的订单要落成 failed，不能永远停在 pending
 */

function resetStore() {
  useServerStore.setState({ orders: [], servers: [] })
}

/**
 * 构造一笔订单。
 *
 * `ownerUid` 传 undefined 表示「这笔订单还没标记归属」——
 * store 的 `addOrder` 会用 `order.ownerUid ?? currentOwnerId()` 补上当前账号。
 * 注意不能传空串：空串不是 undefined，`??` 不会回填。
 */
function orderFor(ownerUid: string | undefined, id = 'srv_1'): ServerOrder {
  return {
    id,
    tradeNo: `HBD${id}`,
    config: {
      region: 'guangzhou',
      plan: 'lite',
      specId: 'lite-2c2g',
      mbps: 20,
      durationId: '1mo',
    },
    amount: 113,
    paymentMethod: 'demo',
    status: 'pending',
    createdAt: 1_700_000_000_000,
    ownerUid,
  }
}

function serverFor(orderId: string, ownerUid: string | undefined): OwnedServer {
  return {
    id: 'node_1',
    orderId,
    config: orderFor(ownerUid).config,
    ip: '43.142.108.151',
    username: 'ubuntu',
    password: 'Str0ng!Passw0rd#2026x',
    status: 'running',
    expireAt: 1_800_000_000,
    createdAt: 1_700_000_000_000,
    demo: true,
    ownerUid,
  }
}

/**
 * 把 auth store 里的当前用户设成指定 id（null = 未登录）。
 *
 * 注意：只替换 `user`，保留 auth 对象上的 `setUser` / `reset` 方法 ——
 * 整个 auth 对象被换掉会让 store 的其他方法变成 undefined。
 */
function loginAs(userId: number | null) {
  useAuthStore.setState((state) => ({
    auth: {
      ...state.auth,
      user:
        userId === null
          ? null
          : ({ id: userId, username: `u${userId}`, role: 1 } as never),
    },
  }))
}

describe('servers store: 账号隔离', () => {
  beforeEach(() => {
    resetStore()
    loginAs(28)
  })

  test('记录自动带上当前登录账号的 ownerUid', () => {
    loginAs(28)
    useServerStore.getState().addOrder(orderFor(undefined, 'srv_a'))
    const stored = useServerStore.getState().orders[0]
    assert.equal(stored.ownerUid, '28', '订单没有记录所属账号')
  })

  test('结算出的实例也带上 ownerUid', () => {
    loginAs(28)
    const store = useServerStore.getState()
    store.addOrder(orderFor(undefined, 'srv_b'))
    store.settleOrder('srv_b', serverFor('srv_b', undefined))
    const instance = useServerStore.getState().servers[0]
    assert.equal(instance.ownerUid, '28', '实例没有记录所属账号')
  })

  test('换账号登录后看不到上一个账号的实例凭据', () => {
    // 账号 28 购买
    loginAs(28)
    useServerStore.getState().addOrder(orderFor(undefined, 'srv_c'))
    useServerStore.getState().settleOrder('srv_c', serverFor('srv_c', undefined))
    assert.equal(useServerStore.getState().servers.length, 1)

    // 同一个浏览器换成账号 99
    loginAs(99)
    const visible = useServerStore
      .getState()
      .servers.filter((s) => (s.ownerUid ?? '') === '99')
    assert.equal(
      visible.length,
      0,
      '换账号后仍能看到上一个账号的实例凭据 —— 密码泄露'
    )

    // 账号 28 回来仍然看得到自己的
    loginAs(28)
    const own = useServerStore
      .getState()
      .servers.filter((s) => (s.ownerUid ?? '') === '28')
    assert.equal(own.length, 1, '原账号反而看不到自己的实例')
  })
})

describe('servers store: 订单结算的幂等性', () => {
  beforeEach(() => {
    resetStore()
    loginAs(28)
  })

  test('同一笔订单重复结算只生成一台实例', () => {
    const store = useServerStore.getState()
    store.addOrder(orderFor('28', 'srv_d'))
    store.settleOrder('srv_d', serverFor('srv_d', '28'))
    // 支付回调重放 / 「轮询 + 回调」并发
    useServerStore.getState().settleOrder('srv_d', serverFor('srv_d', '28'))

    const servers = useServerStore.getState().servers
    assert.equal(
      servers.length,
      1,
      `同一订单结算了两次，生成了 ${servers.length} 台实例`
    )
  })

  test('重复结算不会覆盖第一次生成的密码', () => {
    const store = useServerStore.getState()
    store.addOrder(orderFor('28', 'srv_e'))
    const first = { ...serverFor('srv_e', '28'), password: 'FirstPassword!1' }
    store.settleOrder('srv_e', first)
    useServerStore
      .getState()
      .settleOrder('srv_e', { ...serverFor('srv_e', '28'), password: 'SecondPassword!2' })

    assert.equal(
      useServerStore.getState().servers[0].password,
      'FirstPassword!1',
      '重复结算把已发给用户的密码覆盖了 —— 用户拿到的密码会失效'
    )
  })

  test('订单结算后状态变成 success', () => {
    const store = useServerStore.getState()
    store.addOrder(orderFor('28', 'srv_f'))
    store.settleOrder('srv_f', serverFor('srv_f', '28'))
    const order = useServerStore.getState().orders[0]
    assert.equal(order.status, 'success')
    assert.ok(order.paidAt, '结算后没有记录支付时间')
  })
})

describe('servers store: 支付失败', () => {
  beforeEach(() => {
    resetStore()
    loginAs(28)
  })

  test('failOrder 把订单落成 failed，不是永远 pending', () => {
    useServerStore.getState().addOrder(orderFor('28', 'srv_g'))
    useServerStore.getState().failOrder('srv_g')
    const order = useServerStore.getState().orders[0]
    assert.equal(order.status, 'failed', '支付失败的订单没有落到 failed 状态')
  })

  test('失败的订单不产生任何实例', () => {
    useServerStore.getState().addOrder(orderFor('28', 'srv_h'))
    useServerStore.getState().failOrder('srv_h')
    assert.equal(
      useServerStore.getState().servers.length,
      0,
      '支付失败却生成了实例'
    )
  })
})
