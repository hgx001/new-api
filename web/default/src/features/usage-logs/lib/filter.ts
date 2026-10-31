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
 * Utility functions for usage logs filters
 */
import { LOG_CATEGORY_LABELS } from '../constants'
import type {
  LogCategory,
  LogFilters,
  CommonLogFilters,
  DrawingLogFilters,
  TaskLogFilters,
} from '../types'

// ============================================================================
// Filter Building Functions
// ============================================================================

/** 去掉首尾空白，空串归一成 undefined（这样 URL 里就不会出现 `model=+xxx`）。 */
function trimText(value: string | undefined): string | undefined {
  const trimmed = value?.trim()
  return trimmed ? trimmed : undefined
}

/**
 * Build search params from filters based on log category
 */
export function buildSearchParams(
  filters: LogFilters,
  logCategory: LogCategory
): Record<string, unknown> {
  const baseParams: Record<string, unknown> = {
    ...(filters.startTime && { startTime: filters.startTime.getTime() }),
    ...(filters.endTime && { endTime: filters.endTime.getTime() }),
    ...(filters.channel && { channel: filters.channel }),
  }

  switch (logCategory) {
    case 'common': {
      const commonFilters = filters as CommonLogFilters
      // 文本筛选项一律 trim 后再写进 URL。
      //
      // 2026-11-01 线上问题：从别处粘贴模型名时带上了前导空格，URL 变成
      // `model=+seedance2.5-特惠`，后端等值匹配命中 0 条，页面显示
      // 「未查询到日志」—— 而输入框里根本看不出那个空格。
      // 前端先 trim，URL 干净、分享出去也不会带着空格。
      const model = trimText(commonFilters.model)
      const token = trimText(commonFilters.token)
      const group = trimText(commonFilters.group)
      const username = trimText(commonFilters.username)
      const requestId = trimText(commonFilters.requestId)
      const upstreamRequestId = trimText(commonFilters.upstreamRequestId)
      return {
        ...baseParams,
        ...(model && { model }),
        ...(token && { token }),
        ...(group && { group }),
        ...(username && { username }),
        ...(requestId && { requestId }),
        ...(upstreamRequestId && { upstreamRequestId }),
      }
    }
    case 'drawing': {
      const drawingFilters = filters as DrawingLogFilters
      return {
        ...baseParams,
        ...(drawingFilters.mjId && { filter: drawingFilters.mjId }),
      }
    }
    case 'task': {
      const taskFilters = filters as TaskLogFilters
      return {
        ...baseParams,
        ...(taskFilters.taskId && { filter: taskFilters.taskId }),
      }
    }
    default:
      return baseParams
  }
}

/**
 * Get log category display name
 */
export function getLogCategoryLabel(category: LogCategory): string {
  return LOG_CATEGORY_LABELS[category]
}
