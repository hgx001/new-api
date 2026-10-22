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

import { api } from '@/lib/api'

import { getPlan, getRegion, getTerm } from './constants'
import type { OwnedServer, PayMethod, ServerConfig, ServerOrder } from './types'

/**
 * 服务器租赁的数据访问层。
 *
 * ⚠️ **当前为前端演示实现**（后端接口尚未提供）。
 *    - 支付方式列表走真实接口 `/api/user/topup/info`（复用钱包充值，已上线）
 *    - 下单 / 支付 / 改密走本地模拟，接口形状已按后端 REST 约定预留
 *    后端接入时，只需把 `createServerOrder` / `payServerOrder` / `changePassword`
 *    三个函数内部换成真实请求，调用方无需改动。
 */

/** 真实支付方式拉取失败时的兜底，保证收银台始终可用 */
const FALLBACK_PAY_METHODS: PayMethod[] = [
  { name: 'Alipay', type: 'alipay' },
  { name: 'WeChat Pay', type: 'wechat' },
  { name: 'Balance', type: 'balance' },
]

function delay(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function randomId(prefix: string) {
  const rand = Math.random().toString(36).slice(2, 10)
  return `${prefix}_${Date.now().toString(36)}${rand}`
}

function randomPassword() {
  const upper = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
  const lower = 'abcdefghijkmnpqrstuvwxyz'
  const digits = '23456789'
  const symbols = '!@#%^&*'
  const all = upper + lower + digits + symbols
  const pick = (chars: string) =>
    chars[Math.floor(Math.random() * chars.length)]
  const body = Array.from({ length: 12 }, () => pick(all)).join('')
  return `${pick(upper)}${pick(upper)}${pick(digits)}${body}`
}

/** 按地区生成看起来合理的公网 IP */
function generateIp(regionId: string) {
  if (regionId === 'shanghai') {
    const second = 80 + Math.floor(Math.random() * 60)
    return `47.${second}.${Math.floor(Math.random() * 254)}.${Math.floor(
      Math.random() * 254
    )}`
  }
  const second = 100 + Math.floor(Math.random() * 60)
  return `121.${second}.${Math.floor(Math.random() * 254)}.${Math.floor(
    Math.random() * 254
  )}`
}

/** 获取可用支付方式（优先真实配置，回退到内置列表） */
export async function getServerPayMethods(): Promise<PayMethod[]> {
  try {
    const res = await api.get('/api/user/topup/info', {
      skipBusinessError: true,
      // 未登录时这个接口必然 401，这里自己兜底，不要弹全局的「会话已过期」
      skipErrorHandler: true,
    })
    const info = res.data?.data as
      | { pay_methods?: PayMethod[]; enable_online_topup?: boolean }
      | undefined
    if (info?.enable_online_topup && info.pay_methods?.length) {
      return info.pay_methods
    }
  } catch {
    // 网络异常时使用兜底支付方式
  }
  return FALLBACK_PAY_METHODS
}

/** 创建订单（模拟后端下单） */
export async function createServerOrder(
  config: ServerConfig,
  amount: number,
  paymentMethod: string
): Promise<ServerOrder> {
  await delay(420)
  const now = Date.now()
  return {
    id: randomId('srv'),
    tradeNo: `HBD${now.toString().slice(-10)}`,
    config,
    amount,
    paymentMethod,
    status: 'pending',
    createdAt: now,
  }
}

/**
 * 发起支付并等待结果。
 *
 * 模拟真实收银台的异步过程：拉起 → 用户确认 → 回调。
 * 后端接入后，这个函数应替换为「拉起支付 + 轮询订单状态」。
 */
export async function payServerOrder(
  order: ServerOrder
): Promise<{ success: boolean; server?: OwnedServer; messageKey?: string }> {
  await delay(1400)

  const plan = getPlan(order.config.plan)
  const spec =
    plan.specs.find((s) => s.id === order.config.specId) ?? plan.specs[0]
  const term = getTerm(order.config.term)
  const region = getRegion(order.config.region)
  const expireAt = Math.floor(
    (Date.now() + term.months * 30 * 24 * 3600 * 1000) / 1000
  )

  const server: OwnedServer = {
    id: randomId('node'),
    orderId: order.id,
    config: order.config,
    ip: generateIp(region.id),
    username: 'root',
    password: randomPassword(),
    status: 'running',
    expireAt,
    createdAt: Date.now(),
    demo: true,
  }

  // spec / region 引用到的字段用于保证配置完整
  void spec
  return { success: true, server }
}

/** 修改登录密码（模拟后端接口） */
export async function changeServerPassword(
  _serverId: string,
  password: string
): Promise<{ success: boolean; messageKey?: string }> {
  await delay(600)
  if (!password || password.length < 8) {
    return {
      success: false,
      messageKey: 'Password must be at least 8 characters',
    }
  }
  return { success: true }
}
