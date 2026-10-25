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

/**
 * VPN 产品页的纯逻辑：跳转信任、状态机、到期计算、轮询策略。
 *
 * 从 index.tsx 抽出来只为两件事：这些规则是资金/安全相关的（跳错页面 = 钓鱼，
 * 轮询停掉 = 迟到的付款通知没人接），值得单独锁住；组件文件本身不需要
 * 为了可测性被改成一团参数。
 */

/** 支付宝官方直连的收银台域名白名单：只允许跳转到支付宝自己的域名 */
export const ALIPAY_CHECKOUT_HOSTS = new Set([
  'openapi.alipay.com',
  'openapi.alipaydev.com',
])

/** 待支付订单超过这个时间仍未确认，就把轮询频率降下来（仍在轮询，不放弃） */
export const PENDING_FAST_WINDOW_SEC = 30 * 60

export const POLL_FAST_MS = 3000
export const POLL_SLOW_MS = 30000

export type VpnState = 'active' | 'pending' | 'expired' | 'none'

export function formatDate(ts: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

export function formatDateTime(ts: number) {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function daysLeft(expireTs: number) {
  return Math.max(0, Math.ceil((expireTs * 1000 - Date.now()) / 86400000))
}

/** 只允许整页跳转到支付宝官方收银台，避免把用户送去任意地址 */
export function isTrustedAlipayUrl(url: string) {
  try {
    const parsed = new URL(url)
    return (
      parsed.protocol === 'https:' && ALIPAY_CHECKOUT_HOSTS.has(parsed.hostname)
    )
  } catch {
    return false
  }
}

export function resolveVpnState({
  hasKey,
  isExpired,
  hasPending,
}: {
  hasKey: boolean
  isExpired: boolean
  hasPending: boolean
}): VpnState {
  if (hasKey) return 'active'
  if (isExpired) return 'expired'
  if (hasPending) return 'pending'
  return 'none'
}

/**
 * 待支付订单的轮询间隔：刚下单 3 秒一次，超过 30 分钟降到 30 秒一次。
 *
 * 注意只有两档、没有 0：支付宝的异步通知可能迟到（网络抖动、网关重试队列），
 * 停了轮询就等于放弃这一单 —— 用户付了钱却永远拿不到密钥。
 */
export function resolvePollIntervalMs(pendingAgeSec: number): number {
  return pendingAgeSec > PENDING_FAST_WINDOW_SEC ? POLL_SLOW_MS : POLL_FAST_MS
}
