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

export type VpnOrderStatus = 'pending' | 'success' | 'failed' | 'expired'

export interface VpnAdminOrder {
  id: number
  user_id: number
  username: string
  trade_no: string
  plan: string
  money: number
  payment_method: string
  payment_provider: string
  status: VpnOrderStatus
  create_time: number
  complete_time: number
}

export interface VpnProviderStat {
  provider: string
  paid_count: number
  paid_amount: string
}

export interface VpnOrderStats {
  paid_count: number
  paid_amount: string
  pending_count: number
  today_paid_count: number
  today_paid_amount: string
  by_provider: VpnProviderStat[]
}

export interface VpnAdminOrdersResponse {
  items: VpnAdminOrder[]
  total: number
  page: number
  page_size: number
  stats: VpnOrderStats
}

export interface VpnAdminOrdersParams {
  p: number
  page_size: number
  keyword?: string
  status?: VpnOrderStatus | ''
}

/** 管理端 VPN 套餐订单（只读）。套餐独立结算，不进用户额度与充值记录。 */
export async function getVpnAdminOrders(
  params: VpnAdminOrdersParams
): Promise<VpnAdminOrdersResponse> {
  const res = await api.get('/api/vpn/admin/orders', { params })
  return res.data?.data as VpnAdminOrdersResponse
}
