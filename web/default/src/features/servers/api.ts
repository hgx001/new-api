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

import type {
  ServerConfig,
  ServerInstance,
  ServerInstanceEvent,
  ServerMetrics,
  ServerOrderRecord,
  ServerQuoteResult,
} from './types'

/**
 * 云服务器租赁的数据访问层。
 *
 * 所有金额都由服务端计算：下单只提交「地区/机型/规格/带宽/时长」，
 * 前端算出的价格仅用于展示。这样改请求体改不动价格。
 */

/** 业务失败的稳定错误码，前端据此做本地化提示 */
export type ServerErrorCode =
  | 'insufficient_quota'
  | 'instance_not_found'
  | 'instance_released'
  | 'instance_expired'
  | 'instance_not_active'
  | 'concurrent_update'
  | 'invalid_config'
  | 'password_too_short'
  | 'order_not_found'
  | 'order_paid'
  | 'order_not_payable'
  | 'order_expired'
  | 'payment_method_invalid'
  | 'payment_not_configured'
  | 'payment_launch_failed'
  | 'server_error'

export class ServerApiError extends Error {
  code: ServerErrorCode

  constructor(code: ServerErrorCode, message: string) {
    super(message)
    this.name = 'ServerApiError'
    this.code = code
  }
}

interface Envelope<T> {
  success?: boolean
  message?: string
  code?: ServerErrorCode
  data?: T
}

/**
 * 稳定错误码 → i18n key。
 *
 * 服务端只回中文兜底文案；要跟站点语言一致，必须由前端按 code 翻译。
 * 未识别的 code 退回服务端 message（至少能看到具体原因）。
 */
export function serverErrorMessageKey(error: unknown): string | null {
  if (!(error instanceof ServerApiError)) return null
  switch (error.code) {
    case 'insufficient_quota':
      return 'Insufficient balance, please top up first'
    case 'instance_not_found':
      return 'Instance not found'
    case 'instance_released':
      return 'This instance has been released'
    case 'instance_expired':
      return 'This instance has expired, please renew it first'
    case 'instance_not_active':
      return 'This operation is not available in the current state'
    case 'concurrent_update':
      return 'The instance changed, please refresh and try again'
    case 'invalid_config':
      return 'Invalid server configuration'
    case 'password_too_short':
      return 'Password must be at least 8 characters'
    case 'order_not_found':
      return 'Order not found'
    case 'order_paid':
      return 'This order has already been paid'
    case 'order_not_payable':
      return 'This order cannot be paid'
    case 'order_expired':
      return 'This order has expired, please place a new one'
    case 'payment_method_invalid':
      return 'This payment method is not available'
    case 'payment_not_configured':
      return 'Online payment is not configured yet'
    case 'payment_launch_failed':
      return 'Failed to open the payment page'
    default:
      return null
  }
}

/**
 * 写操作的统一请求配置。
 *
 * - `skipBusinessError` / `skipErrorHandler`：错误由调用方翻译后自己提示，
 *   否则全局拦截器会先弹一条中文、我们再弹一条本地化的，用户看到两条。
 * - `disableDuplicate`：关掉同 URL 的并发去重 —— 幂等由 `client_request_id`
 *   在服务端保证，前端去重只会在「关掉弹窗立刻重开」时把新请求吞掉。
 */
const WRITE_OPTIONS = {
  skipBusinessError: true,
  skipErrorHandler: true,
  disableDuplicate: true,
} as const

async function unwrap<T>(request: Promise<{ data: Envelope<T> }>): Promise<T> {
  const response = await request
  const payload = response.data
  if (payload?.success === false) {
    throw new ServerApiError(
      payload.code ?? 'server_error',
      payload.message || 'Request failed'
    )
  }
  if (!payload?.data) {
    throw new ServerApiError(
      'server_error',
      payload?.message || 'Empty response'
    )
  }
  return payload.data
}

/** 生成一次下单/续费请求的幂等号：双击或重试不会买出两台机器。 */
export function newRequestId(): string {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return crypto.randomUUID()
  }
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`
}

function configBody(config: ServerConfig) {
  return {
    region: config.region,
    plan: config.plan,
    spec_id: config.specId,
    mbps: config.mbps,
    duration_id: config.durationId ?? '',
  }
}

export interface ServerWalletSnapshot {
  quota: number
  balance_amount: number
}

export interface PurchaseResult extends ServerWalletSnapshot {
  order: ServerOrderRecord
  /** 站内余额支付时立即返回；第三方支付要等回调，此刻为空 */
  instance?: ServerInstance
  /** balance：余额同步开通；epay：先拉起收银台，机器在回调后开通 */
  pay_mode: 'balance' | 'epay'
}

/** 收银台上的一个支付方式 */
export interface ServerPayMethod {
  name: string
  type: string
  icon?: string
}

/** 可用支付方式（余额 + 服务端下发的第三方渠道） */
export interface ServerPayMethodsResult extends ServerWalletSnapshot {
  methods: ServerPayMethod[]
}

/** 第三方收银台参数：需要前端提交表单跳转到网关 */
export interface ServerPayLaunch {
  pay_mode: 'form' | 'redirect'
  url: string
  params: Record<string, string>
  trade_no: string
}

/** 订单支付状态（前端轮询用） */
export interface ServerOrderStatus extends ServerWalletSnapshot {
  order: ServerOrderRecord
  paid: boolean
  instance?: ServerInstance
}

/** 服务端报价（下单前的权威金额） */
export async function quoteServer(
  config: ServerConfig
): Promise<ServerQuoteResult> {
  // 报价是只读的，但用户改了配置就立刻重报，不能被并发去重挡住
  return unwrap<ServerQuoteResult>(
    api.post('/api/server/quote', configBody(config), WRITE_OPTIONS)
  )
}

/** 可用的支付方式（余额 + 第三方渠道） */
export async function listServerPayMethods(): Promise<ServerPayMethodsResult> {
  return unwrap(api.get('/api/server/pay-methods'))
}

/**
 * 下单购买一台服务器。
 *
 * paymentMethod 为 'balance' 时同步开通并返回 instance；
 * 第三方渠道则返回 pending 订单与 trade_no，需要再调 requestServerEpayPay。
 */
export async function purchaseServer(
  config: ServerConfig,
  clientRequestId: string,
  paymentMethod = 'balance'
): Promise<PurchaseResult> {
  return unwrap<PurchaseResult>(
    api.post(
      '/api/server/orders',
      {
        ...configBody(config),
        client_request_id: clientRequestId,
        payment_method: paymentMethod,
      },
      WRITE_OPTIONS
    )
  )
}

/** 拉起第三方收银台：拿到网关地址与表单参数 */
export async function requestServerEpayPay(
  tradeNo: string,
  paymentMethod: string
): Promise<ServerPayLaunch> {
  return unwrap<ServerPayLaunch>(
    api.post(
      '/api/server/pay/epay',
      { trade_no: tradeNo, payment_method: paymentMethod },
      WRITE_OPTIONS
    )
  )
}

/** 查询订单支付状态（收银台跳回来后轮询） */
export async function getServerOrderStatus(
  tradeNo: string
): Promise<ServerOrderStatus> {
  return unwrap<ServerOrderStatus>(
    api.get(`/api/server/orders/${encodeURIComponent(tradeNo)}`)
  )
}

/** 续费：余额同步扣款，实例立即更新，所以 instance 一定存在 */
export interface RenewResult extends ServerWalletSnapshot {
  order: ServerOrderRecord
  instance: ServerInstance
}

/** 续费 */
export async function renewServer(
  instanceId: number,
  durationId: string,
  clientRequestId: string
): Promise<RenewResult> {
  return unwrap<RenewResult>(
    api.post(
      `/api/server/instances/${instanceId}/renew`,
      { duration_id: durationId, client_request_id: clientRequestId },
      WRITE_OPTIONS
    )
  )
}

export interface ReleaseResult extends ServerWalletSnapshot {
  instance: ServerInstance
  refunded: number
  refund_order?: ServerOrderRecord
}

/** 释放实例（按剩余时长退款） */
export async function releaseServer(
  instanceId: number
): Promise<ReleaseResult> {
  return unwrap<ReleaseResult>(
    api.post(
      `/api/server/instances/${instanceId}/release`,
      undefined,
      WRITE_OPTIONS
    )
  )
}

export async function listServerInstances(
  includeReleased = false
): Promise<{ items: ServerInstance[] } & ServerWalletSnapshot> {
  return unwrap(
    api.get('/api/server/instances', {
      params: { include_released: includeReleased ? 'true' : 'false' },
    })
  )
}

export async function getServerInstance(id: number): Promise<ServerInstance> {
  return unwrap(api.get(`/api/server/instances/${id}`))
}

export async function getServerMetrics(id: number): Promise<ServerMetrics> {
  return unwrap(api.get(`/api/server/instances/${id}/metrics`))
}

export async function getServerEvents(
  id: number
): Promise<{ items: ServerInstanceEvent[] }> {
  return unwrap(api.get(`/api/server/instances/${id}/events`))
}

export async function listServerOrders(): Promise<{
  items: ServerOrderRecord[]
}> {
  return unwrap(api.get('/api/server/orders'))
}

export async function startServer(id: number): Promise<ServerInstance> {
  return unwrap(
    api.post(`/api/server/instances/${id}/start`, undefined, WRITE_OPTIONS)
  )
}

export async function stopServer(id: number): Promise<ServerInstance> {
  return unwrap(
    api.post(`/api/server/instances/${id}/stop`, undefined, WRITE_OPTIONS)
  )
}

export async function rebootServer(id: number): Promise<ServerInstance> {
  return unwrap(
    api.post(`/api/server/instances/${id}/reboot`, undefined, WRITE_OPTIONS)
  )
}

export async function resetServerPassword(
  id: number,
  password: string
): Promise<{ password: string }> {
  return unwrap(
    api.post(
      `/api/server/instances/${id}/reset-password`,
      { password },
      WRITE_OPTIONS
    )
  )
}
