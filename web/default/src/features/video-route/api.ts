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

/**
 * 视频条件路由（跨模型改派）的策略快照。
 *
 * 字段与后端 controller.doubaoRouteAdminState 一一对应。注意 `pause_source`：
 * 运营需要区分「额度用完自动停的」和「人为停的」—— 前者到点自己恢复，
 * 后者不会，混在一起会让人误判。
 */
export type VideoRoutePolicy = {
  paused: boolean
  paused_until: number
  paused_until_text: string
  pause_source: '' | 'manual' | 'auto'
  pause_reason: string
  seconds_rule_enabled: boolean
  ratio_rule_enabled: boolean
  frames_rule_enabled: boolean
  source_model: string
  target_model: string
  trigger_seconds: number
  ratio_target_model: string
  trigger_ratio: string
  frames_target_model: string
  updated_at: number
}

export async function getVideoRoutePolicy(): Promise<VideoRoutePolicy> {
  const res = await api.get('/api/video-route/policy')
  return res.data.data
}

/**
 * 暂停「按时长改派」这条规则。
 *
 * until 省略 = 北京时间次日 0 点（对应「今天额度用完了」这个最常见的场景）。
 */
export async function pauseVideoRoute(
  until?: number
): Promise<VideoRoutePolicy> {
  const res = await api.post(
    '/api/video-route/policy/pause',
    until ? { until } : {}
  )
  return res.data.data
}

export async function resumeVideoRoute(): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/resume', {})
  return res.data.data
}
