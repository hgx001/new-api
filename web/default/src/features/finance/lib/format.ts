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
import type { FinanceRange } from '../api'

export type FinanceRangePreset =
  | 'this_month'
  | 'last_month'
  | 'last_30_days'
  | 'this_year'
  | 'custom'

/** 金额后端已算好并以字符串下发，这里只做展示，不再参与任何计算。 */
export function formatCny(value?: string): string {
  const amount = Number(value ?? '0')
  if (!Number.isFinite(amount)) return '¥0.00'
  return `¥${amount.toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

export function formatPercent(value?: string): string {
  return `${value ?? '0.00'}%`
}

export function formatTimestamp(ts: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

export function timestampToDateInput(ts: number): string {
  const d = new Date(ts * 1000)
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${d.getFullYear()}-${month}-${day}`
}

function dateInputToTimestamp(value: string, endOfDay: boolean): number {
  const [year, month, day] = value.split('-').map(Number)
  if (!year || !month || !day) return 0
  const date = new Date(year, month - 1, day, 0, 0, 0, 0)
  return Math.floor(date.getTime() / 1000) + (endOfDay ? 86399 : 0)
}

export function parseDateInput(value: string): number {
  return dateInputToTimestamp(value, false)
}

export function todayDateInput(): string {
  return timestampToDateInput(Math.floor(Date.now() / 1000))
}

export function buildFinanceRange(
  preset: FinanceRangePreset,
  customStart: string,
  customEnd: string
): FinanceRange {
  const now = new Date()
  const end = Math.floor(now.getTime() / 1000)
  let start: number
  let rangeEnd = end

  switch (preset) {
    case 'last_month': {
      start = Math.floor(
        new Date(now.getFullYear(), now.getMonth() - 1, 1).getTime() / 1000
      )
      rangeEnd =
        Math.floor(
          new Date(now.getFullYear(), now.getMonth(), 1).getTime() / 1000
        ) - 1
      break
    }
    case 'last_30_days': {
      start = end - 30 * 86400
      break
    }
    case 'this_year': {
      start = Math.floor(new Date(now.getFullYear(), 0, 1).getTime() / 1000)
      break
    }
    case 'custom': {
      start = dateInputToTimestamp(customStart, false)
      rangeEnd = dateInputToTimestamp(customEnd, true)
      break
    }
    default: {
      start = Math.floor(
        new Date(now.getFullYear(), now.getMonth(), 1).getTime() / 1000
      )
      break
    }
  }
  return { start_timestamp: start, end_timestamp: Math.max(start, rangeEnd) }
}
