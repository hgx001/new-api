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
import type { LogRequestParams } from '../types'

export interface RequestParamRow {
  labelKey: string
  value: string
}

/**
 * 把后端捕获的请求参数摘要转成日志详情里的行。
 *
 * 顺序固定：模型 → 提示词 → 时长 → 画幅 → 素材。
 * 空值一律不渲染，避免详情里出现一排「-」。
 */
export function buildRequestParamRows(
  params?: LogRequestParams
): RequestParamRow[] {
  if (!params) return []

  const rows: RequestParamRow[] = []
  const push = (labelKey: string, value?: string | number) => {
    if (value === undefined || value === null) return
    const text = String(value).trim()
    if (!text) return
    rows.push({ labelKey, value: text })
  }

  push('Model', params.model)
  push('Prompt', params.prompt)
  push('Seconds', params.seconds)
  push('Duration', params.duration)
  push('Resolution', params.resolution)
  push('Size', params.size)
  push('Ratio', params.ratio)
  push('Seed', params.seed)

  const mediaLines = (params.media ?? [])
    .map((item) => {
      const type = (item.type ?? '').trim()
      const url = (item.url ?? '').trim()
      if (!url) return ''
      return type ? `${type}: ${url}` : url
    })
    .filter(Boolean)
  if (mediaLines.length > 0) {
    rows.push({ labelKey: 'Media', value: mediaLines.join('\n') })
  } else if (params.media_counts) {
    const counts = Object.entries(params.media_counts)
      .filter(([, count]) => count > 0)
      .map(([type, count]) => `${type} ×${count}`)
    if (counts.length > 0) {
      rows.push({ labelKey: 'Media', value: counts.join(', ') })
    }
  }

  // 素材来源字段：适配器对 media / images / image 的取舍不同，
  // 排查「素材没生效」时第一个要看的就是客户写在了哪个字段。
  if (params.media_fields) {
    const fields = Object.entries(params.media_fields)
      .filter(([, count]) => count > 0)
      .map(([field, count]) => `${field}: ${count}`)
    if (fields.length > 0) {
      rows.push({ labelKey: 'Media Fields', value: fields.join(', ') })
    }
  }

  return rows
}
