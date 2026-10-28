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

import { getDuration } from './constants'
import { resolveDurationId } from './lib/pricing'
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

/**
 * 后端接入后置为 true。
 *
 * 为 true 时不再提供演示支付渠道，支付方式完全以服务端下发的为准；
 * 同时 `createServerOrder` / `payServerOrder` / `changeServerPassword`
 * 换成真实请求。
 */
export const PAYMENT_BACKEND_ENABLED = false

/**
 * 演示支付渠道。
 *
 * 只在后端未接入、且站点没有开通在线支付时出现，并且**明确标注为演示**，
 * 不冒充支付宝 / 微信等真实渠道，避免用户以为站点真的能收款。
 */
const DEMO_PAY_METHOD: PayMethod = {
  type: 'demo',
  nameKey: 'Demo payment (no real charge)',
  demo: true,
}

/** 后端 pay_methods 的原始结构（min_topup 是字符串） */
interface RawPayMethod {
  name?: string
  type?: string
  icon?: string
  min_topup?: string | number
}

function parsePayMethods(raw: unknown): PayMethod[] {
  if (!Array.isArray(raw)) return []
  return raw
    .filter((m): m is RawPayMethod => !!m && typeof m === 'object')
    .filter((m) => !!m.type)
    .map((m) => ({
      name: m.name,
      type: m.type as string,
      icon: m.icon,
      min_topup: Number(m.min_topup) || 0,
    }))
}

function delay(ms: number) {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

function randomId(prefix: string) {
  const rand = Math.random().toString(36).slice(2, 10)
  return `${prefix}_${Date.now().toString(36)}${rand}`
}

/**
 * 生成高强度随机密码。
 *
 * 密码会明文展示给用户并写在「我的服务器」里，必须满足：
 * 大小写字母 + 数字 + 符号各至少一个，且不用易混字符（0/O、1/l/I）。
 * 长度 20 位，前 4 位强制各取自一类（保证类别不缺失），
 * 剩余位从全集随机，最后再打乱顺序（避免「类别固定在前几位」的结构特征）。
 */
function randomPassword() {
  const upper = 'ABCDEFGHJKLMNPQRSTUVWXYZ'
  const lower = 'abcdefghijkmnpqrstuvwxyz'
  const digits = '23456789'
  const symbols = '!@#$%^&*-_=+'
  const all = upper + lower + digits + symbols
  const pick = (chars: string) =>
    chars[Math.floor(Math.random() * chars.length)]

  const length = 20
  const chars = [
    pick(upper),
    pick(lower),
    pick(digits),
    pick(symbols),
    ...Array.from({ length: length - 4 }, () => pick(all)),
  ]

  // Fisher-Yates 打乱：强制类别的那 4 位不能留在固定位置
  for (let i = chars.length - 1; i > 0; i--) {
    const j = Math.floor(Math.random() * (i + 1))
    ;[chars[i], chars[j]] = [chars[j], chars[i]]
  }

  return chars.join('')
}

/**
 * 演示态的公网 IP 与登录用户。
 *
 * 演示态没有真实机器池，IP / 用户名是**固定**的（用户指定）。
 * 后端接入后这两个值由开台接口返回，这里只是占位。
 */
const DEMO_SERVER_IP = '43.142.108.151'
const DEMO_SERVER_USERNAME = 'ubuntu'

/**
 * 获取可用支付方式。
 *
 * ⚠️ **后端未接入时，只返回演示渠道 —— 绝不返回站点的真实渠道。**
 *
 * 原因：`payServerOrder` 目前是本地模拟（必定成功、不扣款）。如果把站点
 * 真实的「支付宝 / 微信」渠道摆出来，用户会以为点下去真的付了钱，
 * 实际既没扣款也没开台 —— 支付闭环直接断裂，还可能被当成站点在收钱。
 * 所以真实渠道只在 `PAYMENT_BACKEND_ENABLED = true`（即 `payServerOrder`
 * 换成真实请求）之后才允许出现。
 *
 * 返回空数组表示「站点当前没有可用的在线支付渠道」，调用方需要禁用支付并提示。
 */
export async function getServerPayMethods(): Promise<PayMethod[]> {
  // 后端未接入：无论站点配了什么支付渠道，一律只给演示渠道
  if (!PAYMENT_BACKEND_ENABLED) {
    return [DEMO_PAY_METHOD]
  }
  try {
    const res = await api.get('/api/user/topup/info', {
      skipBusinessError: true,
      // 未登录时这个接口必然 401，这里自己兜底，不要弹全局的「会话已过期」
      skipErrorHandler: true,
    })
    const info = res.data?.data as
      | { pay_methods?: unknown; enable_online_topup?: boolean }
      | undefined
    const methods = parsePayMethods(info?.pay_methods)
    if (info?.enable_online_topup && methods.length > 0) {
      return methods
    }
  } catch {
    // 网络异常：按「站点未开通在线支付」处理，不伪造渠道
  }
  return []
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

  const duration = getDuration(resolveDurationId(order.config))
  // 到期时间跟着选中的时长档走：
  // 按量档（小时/天）= 买的小时数之后释放；包月档 = 月数 × 30 天
  const durationMs =
    duration.pricing === 'hourly'
      ? duration.amount * 3600 * 1000
      : duration.amount * 30 * 24 * 3600 * 1000
  const expireAt = Math.floor((Date.now() + durationMs) / 1000)

  const server: OwnedServer = {
    id: randomId('node'),
    orderId: order.id,
    config: order.config,
    ip: DEMO_SERVER_IP,
    username: DEMO_SERVER_USERNAME,
    password: randomPassword(),
    status: 'running',
    expireAt,
    createdAt: Date.now(),
    demo: true,
  }

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
