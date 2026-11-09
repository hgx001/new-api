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
import type { VpnOrderStatus } from '../api'

/** 金额展示：后端统计已给出十进制字符串，明细 money 是数值，统一按 ¥ 展示。 */
export function formatVpnMoney(value: string | number): string {
  const text = typeof value === 'number' ? value.toFixed(2) : value
  return `¥${text}`
}

/** 秒级时间戳转本地时间；0/负值表示尚未发生。 */
export function formatVpnTime(timestamp: number): string {
  if (!timestamp || timestamp <= 0) return '-'
  return new Date(timestamp * 1000).toLocaleString()
}

/** 状态展示统一映射到 i18n 键，不在组件里硬编码英文。 */
export function vpnStatusLabelKey(status: VpnOrderStatus): string {
  switch (status) {
    case 'success':
      return 'Success'
    case 'pending':
      return 'Pending'
    case 'failed':
      return 'Failed'
    case 'expired':
      return 'Expired'
    default:
      return 'Unknown'
  }
}

/** 收款通道：官方直连与聚合网关要区分开，历史空值单独标注。 */
export function vpnProviderLabel(provider: string): string {
  switch (provider) {
    case 'alipay':
      return 'Alipay (direct)'
    case 'epay':
      return 'Epay'
    case 'unknown':
      return 'Unknown'
    default:
      return provider || 'Unknown'
  }
}

/** 分页区间文案所需的起止序号，越界时收敛到 total。 */
export function vpnPageRange(
  page: number,
  pageSize: number,
  total: number
): [number, number] {
  if (total <= 0) return [0, 0]
  const from = (page - 1) * pageSize + 1
  const to = Math.min(page * pageSize, total)
  return [from, to]
}
