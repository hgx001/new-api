/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
Free Software Foundation, either version 3 of the License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { afterEach, beforeEach, describe, test } from 'node:test'

import { api } from '@/lib/api'

import {
  ServerApiError,
  getServerOrderStatus,
  listServerPayMethods,
  purchaseServer,
  requestServerEpayPay,
  serverErrorMessageKey,
} from './api'
import type { ServerConfig } from './types'

/**
 * 服务器订单支付链路的前端契约。
 *
 * 这里锁的是「请求体发对了吗」——最容易出错的一环：
 *  - 第三方支付必须带上 payment_method，否则服务端按余额扣款；
 *  - 下单与拉起收银台必须用同一个 trade_no，否则拉起的是别人的订单；
 *  - 轮询订单状态时 trade_no 要做 URL 转义。
 *
 * 至于「钱到了没有」，由服务端回调 + 幂等保证，前端只负责把状态读回来。
 */

const config: ServerConfig = {
  region: 'guangzhou',
  plan: 'gpu',
  specId: 'gpu-24c96g-rtxpro6000',
  mbps: 20,
  durationId: '1mo',
}

interface Captured {
  url: string
  method: string
  body: unknown
}

let captured: Captured[] = []
let responder: (req: Captured) => { status?: number; payload: unknown }
let restoreAdapter: (() => void) | null = null

beforeEach(() => {
  captured = []
  responder = () => ({ payload: { success: true, data: {} } })

  // axios 的 adapter 是拦请求最干净的位置：请求参数与真实调用完全一致，
  // 不用去猜 axios 内部怎么拼 URL。
  const originalAdapter = api.defaults.adapter
  api.defaults.adapter = (async (req: {
    url?: string
    method?: string
    data?: unknown
  }) => {
    const entry: Captured = {
      url: req.url ?? '',
      method: (req.method ?? 'get').toUpperCase(),
      body: typeof req.data === 'string' ? safeParse(req.data) : req.data,
    }
    captured.push(entry)
    const { status = 200, payload } = responder(entry)
    return {
      data: payload,
      status,
      statusText: 'OK',
      headers: {},
      config: req,
    }
  }) as never

  restoreAdapter = () => {
    api.defaults.adapter = originalAdapter
  }
})

afterEach(() => {
  restoreAdapter?.()
  restoreAdapter = null
})

function safeParse(raw: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

describe('服务器下单的支付方式参数', () => {
  test('余额支付：请求体带 payment_method=balance 与幂等号', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          order: { trade_no: 'SRV1', status: 'success' },
          instance: { id: 7, name: 'gpu-1106-abcd' },
          pay_mode: 'balance',
          quota: 100,
          balance_amount: 50,
        },
      },
    })

    const result = await purchaseServer(config, 'req-1', 'balance')

    assert.equal(captured.length, 1)
    assert.equal(captured[0].url, '/api/server/orders')
    assert.equal(captured[0].method, 'POST')
    const body = captured[0].body as Record<string, unknown>
    assert.equal(body.payment_method, 'balance')
    assert.equal(body.client_request_id, 'req-1')
    // 配置照旧提交，价格**不提交** —— 金额由服务端算
    assert.equal(body.spec_id, 'gpu-24c96g-rtxpro6000')
    assert.equal('amount' in body, false)
    assert.equal('total' in body, false)
    assert.equal(result.pay_mode, 'balance')
    assert.equal(result.instance?.id, 7)
  })

  test('第三方支付：请求体带 payment_method=alipay，且响应无实例', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          order: { trade_no: 'SRV2', status: 'pending' },
          pay_mode: 'epay',
          quota: 100,
          balance_amount: 50,
        },
      },
    })

    const result = await purchaseServer(config, 'req-2', 'alipay')

    const body = captured[0].body as Record<string, unknown>
    assert.equal(body.payment_method, 'alipay')
    assert.equal(result.pay_mode, 'epay')
    // 异步链路：下单这一刻机器还没开，实例必须为空
    assert.equal(result.instance, undefined)
    assert.equal(result.order.trade_no, 'SRV2')
  })

  test('默认走余额支付：不传 payment_method 时也带上 balance', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          order: { trade_no: 'SRV3' },
          pay_mode: 'balance',
          quota: 1,
          balance_amount: 1,
        },
      },
    })

    await purchaseServer(config, 'req-3')
    const body = captured[0].body as Record<string, unknown>
    assert.equal(body.payment_method, 'balance')
  })
})

describe('拉起第三方收银台', () => {
  test('带上订单号与支付方式，拿到网关地址和表单参数', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          pay_mode: 'form',
          url: 'https://pay.example.test/submit.php',
          params: { pid: '1001', money: '6580.00' },
          trade_no: 'SRV2',
        },
      },
    })

    const launch = await requestServerEpayPay('SRV2', 'alipay')

    assert.equal(captured[0].url, '/api/server/pay/epay')
    assert.equal(captured[0].method, 'POST')
    assert.deepEqual(captured[0].body, {
      trade_no: 'SRV2',
      payment_method: 'alipay',
    })
    assert.equal(launch.pay_mode, 'form')
    assert.equal(launch.url, 'https://pay.example.test/submit.php')
    assert.equal(launch.params.money, '6580.00')
  })
})

describe('轮询订单支付状态', () => {
  test('订单号做 URL 转义，避免拼出非法路径', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          order: { trade_no: 'SRV/../evil' },
          paid: true,
          instance: { id: 9, name: 'gpu-1106-ffff' },
          quota: 1,
          balance_amount: 1,
        },
      },
    })

    const status = await getServerOrderStatus('SRV/../evil')

    assert.equal(captured[0].url, '/api/server/orders/SRV%2F..%2Fevil')
    assert.equal(status.paid, true)
    assert.equal(status.instance?.id, 9)
  })
})

describe('可用支付方式', () => {
  test('服务端下发余额 + 第三方渠道', async () => {
    responder = () => ({
      payload: {
        success: true,
        data: {
          methods: [
            { name: '余额支付', type: 'balance' },
            { name: '支付宝', type: 'alipay', icon: 'SiAlipay' },
          ],
          quota: 100,
          balance_amount: 50,
        },
      },
    })

    const result = await listServerPayMethods()

    assert.equal(captured[0].url, '/api/server/pay-methods')
    assert.equal(result.methods.length, 2)
    assert.equal(result.methods[0].type, 'balance')
    assert.equal(result.methods[1].type, 'alipay')
  })
})

describe('服务端错误码的本地化', () => {
  test('支付相关错误码都有对应的可读文案', () => {
    const codes = [
      'order_not_found',
      'order_paid',
      'order_not_payable',
      'order_expired',
      'payment_method_invalid',
      'payment_not_configured',
      'payment_launch_failed',
    ] as const
    for (const code of codes) {
      const key = serverErrorMessageKey(new ServerApiError(code, '服务端中文兜底'))
      assert.ok(key, `${code} 必须有本地化文案`)
      assert.notEqual(key, '服务端中文兜底', `${code} 不该退回服务端原文`)
    }
  })

  test('未知错误码退回 null，由调用方用服务端原文', () => {
    assert.equal(serverErrorMessageKey(new Error('boom')), null)
  })
})
