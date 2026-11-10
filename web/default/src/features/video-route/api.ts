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
/**
 * 单个源模型的改派开关。
 *
 * 促销族两个成员的经营口径不同：母本按秒（改派豆包赚差价）、活动版按次
 * （改派豆包每单倒贴）。全局暂停一停就是两个一起停，所以运营需要一个
 * 只作用于单个模型的开关。
 */
export type VideoRouteSourceSwitch = {
  source_model: string
  seconds_rule_enabled: boolean
  target_model: string
}

export type VideoRoutePolicy = {
  paused: boolean
  paused_until: number
  paused_until_text: string
  pause_source: '' | 'manual' | 'auto'
  pause_reason: string
  seconds_rule_enabled: boolean
  ratio_rule_enabled: boolean
  frames_rule_enabled: boolean
  source_switches: VideoRouteSourceSwitch[]
  source_model: string
  target_model: string
  trigger_seconds: number
  ratio_target_model: string
  trigger_ratio: string
  frames_target_model: string
  updated_at: number
}

/**
 * 规则五（故障转移）的状态。
 *
 * 核心区分：**`failover_enabled` 是「要不要改派」，`failover_unavailable`
 * 是「源链路现在好不好使」**。两者不是一回事 ——
 * 源链路挂着但故障转移没开 = 请求照旧打过去并失败；
 * 源链路好好的但故障转移开着 = 请求照旧打过去，什么也没发生。
 * 界面上必须分开，否则运营会以为「开着就等于源链路没问题」。
 */
export type VideoRouteFailoverState = {
  /** 规则五总开关。 */
  failover_enabled: boolean
  /** 源/目标模型是否都配好了（不要求开关开着）。 */
  failover_configured: boolean
  failover_source_model: string
  failover_target_model: string
  /** 源链路当前在分组下有没有启用中的渠道（静态可用性）。 */
  failover_source_available: boolean
  /** 备胎链路当前有没有可用渠道。 */
  failover_target_available: boolean
  /** 源链路当前是否被判定为不可用（静态没渠道 或 动态被标记）。 */
  failover_unavailable: boolean
  failover_unavailable_reason: string
  failover_unavailable_until: number
  /** 标记来源：auto（额度探针）/ manual（人工）/ ''（未标记）。 */
  failover_unavailable_source: '' | 'auto' | 'manual'
  failover_unavailable_until_text: string
}

export async function getVideoRoutePolicy(): Promise<VideoRoutePolicy> {
  const res = await api.get('/api/video-route/policy')
  return res.data.data
}

export async function getVideoRouteFailover(): Promise<VideoRouteFailoverState> {
  const res = await api.get('/api/video-route/policy/failover')
  return res.data.data
}

/**
 * 启停规则五（故障转移）。
 *
 * 开启要求源/目标模型都已配置 —— 否则改派会把请求写成一个空模型名。
 */
export async function setVideoRouteFailoverEnabled(
  enabled: boolean
): Promise<VideoRouteFailoverState> {
  const res = await api.post('/api/video-route/policy/failover/switch', {
    enabled,
  })
  return res.data.data
}

/** 配置规则五的源/目标模型（一次性配置，与开关分开）。 */
export async function setVideoRouteFailoverModels(
  sourceModel: string,
  targetModel: string
): Promise<VideoRouteFailoverState> {
  const res = await api.post('/api/video-route/policy/failover/models', {
    source_model: sourceModel,
    target_model: targetModel,
  })
  return res.data.data
}

/**
 * 人工把源链路标记为不可用。
 *
 * until 省略 = 北京时间凌晨 2 点（上游日额度的真实重置点，对应「今天额度用完了」）。
 * 与额度探针的自动标记分开：人工标记不会被一次成功自动解除。
 */
export async function markVideoRouteFailoverUnavailable(
  until?: number,
  reason?: string
): Promise<VideoRouteFailoverState> {
  const res = await api.post('/api/video-route/policy/failover/unavailable', {
    until: until ?? 0,
    reason: reason ?? '',
  })
  return res.data.data
}

/** 人工解除「源链路不可用」标记（不动开关）。 */
export async function clearVideoRouteFailoverUnavailable(): Promise<VideoRouteFailoverState> {
  const res = await api.post('/api/video-route/policy/failover/available', {})
  return res.data.data
}

/**
 * 暂停「按时长改派」这条规则。
 *
 * until 省略 = 北京时间凌晨 2 点（上游日额度的真实重置点，
 * 对应「今天额度用完了」这个最常见的场景）。
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

/**
 * 单独开关某个源模型的「按时长改派」。
 *
 * 全局暂停（pauseVideoRoute）优先级更高：豆包真没额度时，
 * 把单个模型打开也跑不通。
 */
export async function setSourceSecondsRule(
  sourceModel: string,
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/source-switch', {
    source_model: sourceModel,
    enabled,
  })
  return res.data.data
}

export async function resumeVideoRoute(): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/resume', {})
  return res.data.data
}
