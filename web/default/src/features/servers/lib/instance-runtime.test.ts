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

import type { ServerInstance } from '../types'
import {
  daysLeft,
  emptyMetrics,
  hoursLeft,
  isExpired,
  resolveStatus,
  statusKeyOf,
} from './instance-runtime'

/**
 * 控制台的「状态 / 剩余时间 / 空态」口径。
 *
 * 这三样是用户判断「机器还在不在、还能用多久」的依据，错一个就会出现
 * 「显示运行中但其实已过期」这类误导。监控曲线本身由服务端生成
 * （`model/server_instance_metrics.go`，跟着规格与带宽走），前端只负责
 * 请求前给一个诚实的空态，不能拿本地随机数顶上。
 */

const NOW = Math.floor(Date.now() / 1000)

function makeInstance(overrides: Partial<ServerInstance> = {}): ServerInstance {
  return {
    id: 1,
    name: 'ns-1',
    status: 'running',
    ip: '198.51.100.10',
    username: 'ubuntu',
    password: 'x'.repeat(20),
    password_readable: true,
    config: {
      region: 'guangzhou',
      plan: 'lite',
      specId: 'lite-2c2g',
      mbps: 5,
      durationId: '1mo',
    },
    spec: { id: 'lite-2c2g', cpu: 2, memory: 2, disk: 40, multiplier: 1 },
    region: 'guangzhou',
    plan: 'lite',
    mbps: 5,
    billing: 'prepaid',
    unit_price: 60,
    paid_amount: 60,
    refunded_amount: 0,
    provision_mode: 'simulated',
    expire_at: NOW + 30 * 86_400,
    created_at: NOW,
    released_at: 0,
    remaining_seconds: 30 * 86_400,
    order_id: 1,
    ...overrides,
  }
}

describe('servers: 实例状态口径', () => {
  test('到期时间过了就是过期，不管库里还写着 running', () => {
    const instance = makeInstance({ expire_at: NOW - 60 })
    assert.equal(isExpired(instance), true)
    assert.equal(resolveStatus(instance), 'expired')
    assert.equal(statusKeyOf(instance), 'Expired')
  })

  test('已释放是终态，不会被到期时间改写', () => {
    const instance = makeInstance({
      status: 'released',
      expire_at: NOW - 60,
      released_at: NOW - 30,
    })
    // 释放是用户主动操作的结果，语义上比「过期」更准确；
    // 若被改写成 expired，控制台会给出「续费」入口，但那台机器已经没了。
    assert.equal(isExpired(instance), false)
    assert.equal(resolveStatus(instance), 'released')
    assert.equal(statusKeyOf(instance), 'Released')
  })

  test('停机实例仍是 stopped，不会因为停机被当成过期', () => {
    const instance = makeInstance({ status: 'stopped' })
    assert.equal(resolveStatus(instance), 'stopped')
    assert.equal(statusKeyOf(instance), 'Stopped')
  })

  test('不足 72 小时标「即将到期」，其余是运行中', () => {
    assert.equal(statusKeyOf(makeInstance()), 'Running')
    assert.equal(
      statusKeyOf(makeInstance({ expire_at: NOW + 71 * 3600 })),
      'Expiring soon'
    )
    // 边界：正好 72 小时仍算即将到期（<=），避免到期提醒晚一个刷新周期
    assert.equal(
      statusKeyOf(makeInstance({ expire_at: NOW + 72 * 3600 })),
      'Expiring soon'
    )
    assert.equal(
      statusKeyOf(makeInstance({ expire_at: NOW + 73 * 3600 })),
      'Running'
    )
  })
})

describe('servers: 剩余时间', () => {
  test('按小时实例向上取整，不足 1 小时算 1 小时', () => {
    assert.equal(hoursLeft(makeInstance({ expire_at: NOW + 3600 })), 1)
    assert.equal(hoursLeft(makeInstance({ expire_at: NOW + 60 })), 1)
    assert.equal(hoursLeft(makeInstance({ expire_at: NOW + 7200 })), 2)
  })

  test('包月实例按天报，不出现「还剩 0 天」但仍在运行', () => {
    assert.equal(daysLeft(makeInstance({ expire_at: NOW + 86_400 })), 1)
    assert.equal(daysLeft(makeInstance({ expire_at: NOW + 60 })), 1)
  })

  test('过期后剩余时间归零，不是负数', () => {
    const expired = makeInstance({ expire_at: NOW - 10 * 86_400 })
    assert.equal(hoursLeft(expired), 0)
    assert.equal(daysLeft(expired), 0)
  })
})

describe('servers: 监控空态', () => {
  test('请求返回前全是 0，不编造随机负载', () => {
    const metrics = emptyMetrics(makeInstance())
    assert.equal(metrics.gpu, 0)
    assert.equal(metrics.disk, 0)
    assert.equal(metrics.temperature, 0)
    for (const point of [
      ...metrics.cpu,
      ...metrics.memory,
      ...metrics.network,
    ]) {
      assert.equal(point.value, 0)
    }
  })

  test('无显卡规格不谎称有 GPU', () => {
    assert.equal(emptyMetrics(makeInstance()).has_gpu, false)
  })

  test('有显卡规格才标 has_gpu', () => {
    const gpuInstance = makeInstance({
      plan: 'gpu',
      spec: {
        id: 'gpu-24c96g-rtxpro6000',
        cpu: 24,
        memory: 96,
        disk: 200,
        multiplier: 3,
        gpu: { model: 'RTX PRO 6000', vram: 96, count: 1 },
      },
    })
    assert.equal(emptyMetrics(gpuInstance).has_gpu, true)
    // 空态不预填 GPU 使用率：填了就会在真实数据到达时突降，看着像掉卡
    assert.equal(emptyMetrics(gpuInstance).gpu, 0)
  })

  test('带宽取自实例，不是目录默认值', () => {
    assert.equal(emptyMetrics(makeInstance({ mbps: 100 })).bandwidth_mbps, 100)
    assert.equal(emptyMetrics(makeInstance({ mbps: 5 })).bandwidth_mbps, 5)
  })
})
