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

export interface FinanceRange {
  start_timestamp: number
  end_timestamp: number
}

/** 金额一律是「分」精度的十进制字符串，前端只做展示格式化，不参与金额计算。 */
export interface FinanceSummary {
  start: number
  end: number
  revenue: string
  revenue_count: number
  cost: string
  cost_count: number
  net: string
  margin_rate: string
  billed: string
  billed_quota: number
  requests: number
  diff: string
  unallocated_cost: string
  quota_per_unit: number
  usd_exchange_rate: number
}

export interface FinanceChannelStat {
  channel_id: number
  channel_name: string
  billed_quota: number
  billed: string
  cost: string
  gross_profit: string
  margin_rate: string
  requests: number
  token_used: number
  cost_share: string
}

export interface FinanceTrendPoint {
  period: string
  revenue: string
  cost: string
  billed: string
  net: string
}

export interface FinanceChannelOption {
  id: number
  name: string
}

export type FinanceCostCategory =
  | 'topup'
  | 'api_prepay'
  | 'bandwidth'
  | 'other'
  | 'refund'

export interface FinanceCost {
  id: number
  channel_id: number
  channel_name: string
  amount: string
  occurred_at: number
  category: FinanceCostCategory
  reference_no: string
  note: string
  status: 'active' | 'voided'
  created_by: number
  created_at: number
  updated_at: number
}

export interface FinanceCostsResponse {
  items: FinanceCost[]
  total: number
  page: number
  page_size: number
  total_amount: string
}

export interface FinanceCostInput {
  channel_id: number
  amount: string
  occurred_at: number
  category: FinanceCostCategory
  reference_no: string
  note: string
}

type Envelope<T> = { success: boolean; message?: string; data?: T }

export async function getFinanceSummary(
  params: FinanceRange
): Promise<FinanceSummary> {
  const res = await api.get<Envelope<FinanceSummary>>('/api/finance/summary', {
    params,
  })
  return res.data?.data as FinanceSummary
}

export async function getFinanceChannels(
  params: FinanceRange
): Promise<FinanceChannelStat[]> {
  const res = await api.get<Envelope<{ items: FinanceChannelStat[] }>>(
    '/api/finance/channels',
    { params }
  )
  return res.data?.data?.items ?? []
}

export async function getFinanceTrend(
  params: FinanceRange,
  granularity: 'day' | 'month'
): Promise<FinanceTrendPoint[]> {
  const res = await api.get<Envelope<{ items: FinanceTrendPoint[] }>>(
    '/api/finance/trend',
    { params: { ...params, granularity } }
  )
  return res.data?.data?.items ?? []
}

export async function getFinanceChannelOptions(): Promise<
  FinanceChannelOption[]
> {
  const res = await api.get<Envelope<{ items: FinanceChannelOption[] }>>(
    '/api/finance/channels/options'
  )
  return res.data?.data?.items ?? []
}

export async function getFinanceCosts(params: {
  p: number
  page_size: number
  keyword?: string
  channel_id?: number
  start_timestamp?: number
  end_timestamp?: number
  include_voided?: boolean
}): Promise<FinanceCostsResponse> {
  const res = await api.get<Envelope<FinanceCostsResponse>>(
    '/api/finance/costs',
    { params }
  )
  return res.data?.data as FinanceCostsResponse
}

export async function createFinanceCost(
  input: FinanceCostInput
): Promise<{ id: number; duplicate_ids: number[] }> {
  const res = await api.post<Envelope<{ id: number; duplicate_ids: number[] }>>(
    '/api/finance/costs',
    input
  )
  return res.data?.data as { id: number; duplicate_ids: number[] }
}

export async function updateFinanceCost(
  id: number,
  input: FinanceCostInput
): Promise<void> {
  await api.put(`/api/finance/costs/${id}`, input)
}

export async function voidFinanceCost(id: number): Promise<void> {
  await api.delete(`/api/finance/costs/${id}`)
}

export async function exportFinanceCosts(params: FinanceRange): Promise<void> {
  const res = await api.get('/api/finance/export', {
    params,
    responseType: 'blob',
  })
  const blob = new Blob([res.data as BlobPart], {
    type: 'text/csv;charset=utf-8',
  })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = 'finance-costs.csv'
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}
