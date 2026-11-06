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

import type { ServerInstance, ServerMetrics, ServerStatus } from '../types'

/**
 * 实例的运行时视图模型。
 *
 * 云厂商控制台（ECS 这类）的「实例」不只是 IP + 密码：它同时是一台
 * 带状态机（运行中 / 已停止 / 即将到期 / 已过期 / 已释放）、带监控指标、
 * 带计费口径的资源。购买页给的是「买什么」，这里给的是「现在它怎么样」。
 *
 * 状态与到期时间都以**服务端记录**为准；前端只做一层展示兜底：
 * 服务端每分钟扫描一次到期实例，两次扫描之间用 expire_at 先算成 expired，
 * 避免用户看到「已过期但显示运行中」。
 */

/** 状态标签的 i18n key */
export type InstanceStatusKey =
  | 'Running'
  | 'Stopped'
  | 'Provisioning'
  | 'Expired'
  | 'Released'
  | 'Expiring soon'

/** 剩余时间不足这个小时数就标「即将到期」 */
export const EXPIRING_SOON_HOURS = 72

export function isExpired(instance: ServerInstance): boolean {
  if (instance.status === 'released') return false
  // 等待分配的实例 expire_at 还是 0，不排除掉会被误判成「已过期」，
  // 用户刚付完钱就看见一台过期机器。
  if (instance.status === 'provisioning' || instance.status === 'pending') {
    return false
  }
  return instance.status === 'expired' || instance.expire_at * 1000 <= Date.now()
}

/** 实例的**有效**状态：已释放是终态，不能被到期时间覆盖。 */
export function resolveStatus(instance: ServerInstance): ServerStatus {
  if (instance.status === 'released') return 'released'
  if (isExpired(instance)) return 'expired'
  return instance.status
}

export function statusKeyOf(instance: ServerInstance): InstanceStatusKey {
  const status = resolveStatus(instance)
  if (status === 'released') return 'Released'
  if (status === 'stopped') return 'Stopped'
  if (status === 'provisioning' || status === 'pending') return 'Provisioning'
  if (status === 'expired') return 'Expired'
  if (hoursLeft(instance) <= EXPIRING_SOON_HOURS) return 'Expiring soon'
  return 'Running'
}

/** 按小时计费的实例剩余小时数（向上取整，不足 1 小时算 1 小时） */
export function hoursLeft(instance: ServerInstance): number {
  return Math.max(
    0,
    Math.ceil((instance.expire_at * 1000 - Date.now()) / 3600000)
  )
}

/** 包月实例剩余天数 */
export function daysLeft(instance: ServerInstance): number {
  return Math.max(
    0,
    Math.ceil((instance.expire_at * 1000 - Date.now()) / 86400000)
  )
}

export function formatDate(ts: number): string {
  return new Date(ts * 1000).toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  })
}

export function formatDateTime(ts: number): string {
  return new Date(ts * 1000).toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

/**
 * 监控数据的空态。
 *
 * 指标由服务端下发；请求返回前不能拿本地随机数顶上 —— 那会在数据到达时
 * 让曲线整体跳一下，用户以为机器状态突变。空态统一是 0，配合 loading 文案。
 */
export function emptyMetrics(instance: ServerInstance): ServerMetrics {
  const points = Array.from({ length: 12 }, () => ({ label: '', value: 0 }))
  return {
    cpu: points,
    memory: points,
    network: points,
    disk: 0,
    gpu: 0,
    temperature: 0,
    has_gpu: !!instance.spec.gpu,
    bandwidth_mbps: instance.mbps,
    range: '24h',
    generated_at: 0,
    status: instance.status,
  }
}
