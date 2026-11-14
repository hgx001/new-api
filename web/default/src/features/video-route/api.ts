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
  /**
   * 规则六（短时长分流 → 心笔内链）的全局链路开关。
   * true = 短请求改派 short_target_model；false = 不路由，走原链路。
   * 与豆包链路开关同款语义，但没有自动恢复时间（心笔没有日额度）。
   */
  short_link_enabled: boolean
  /** 规则六的目标模型（心笔内链）。空 = 规则六未配置。 */
  short_target_model: string
  /** 规则六的时长阈值（秒），≤ 该值命中。 */
  short_trigger_seconds: number
  /**
   * 规则六的「有赞标准版兜底」开关（2026-11-14）。
   * true = 心笔接不住时短请求改派 short_youzan_target_model；
   * false（默认）= 不兜底。心笔开关优先，两个都关闭走原链路。
   */
  short_youzan_enabled: boolean
  /** 有赞兜底的目标模型（有赞标准版 wan3.0-video）。 */
  short_youzan_target_model: string
  /**
   * 规则一的可选比例条件（后台勾选）。
   * 非空 = 只有该比例的请求才改派目标链路；空 = 不限比例。
   * 与 `trigger_ratio`（规则二自己的触发比例）是两个独立字段。
   */
  seconds_rule_ratio_gate: string
  /**
   * 闸门为空时「勾上会要求什么比例」——由后端给出唯一事实源，
   * 前端不硬编码比例字符串（两处各写一份必然漂）。
   */
  seconds_rule_ratio_gate_default: string
  updated_at: number
  /**
   * 豆包链路开关：true = 运营关闭了豆包（规则一停用 + 故障转移强制生效），
   * 到 doubao_link_off_until（下一个北京时间凌晨 2 点，豆包额度重置点）自动恢复。
   */
  doubao_link_off: boolean
  doubao_link_off_until: number
  /** 自动恢复时间文案（北京时间，仅关闭时返回）。 */
  doubao_link_off_until_text?: string
  /**
   * 「最多接多少单」。0 = 不限（默认状态）。
   *
   * 豆包链路上游给的是运营自己掌握的一小批额度：设成 3 就是
   * 「这批只接 3 单，接满自动关闭，我手动再打开时又来 3 单」。
   */
  doubao_link_max_tasks: number
  /** 本轮已受理多少单。手动重新打开时归零。 */
  doubao_link_accepted_tasks: number
  /** 还能接几单。0 = 不限。由后端算，前端不做减法。 */
  doubao_link_remaining: number
  /**
   * 关闭原因：'manual'（人工关，有自动恢复时间）/ 'limit'（接满，等人再打开）/ ''。
   */
  doubao_link_off_reason: '' | 'manual' | 'limit'
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
 * 单独开关某个源模型的「按时长改派」。
 *
 * 全局暂停优先级更高：豆包真没额度时，把单个模型打开也跑不通。
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

/**
 * 切换豆包链路开关 —— 运营唯一的豆包开关（一个开关 = 豆包通不通）。
 *
 * 关闭：超时长请求留在常驻链路（特惠→有赞、活动→心笔）、seedance-2-5
 * 全部改派备胎（wan3.0-班车），到下一个北京时间凌晨 2 点自动恢复；
 * 开启：立即恢复，并清掉旧式人工标记（开关是唯一事实源）。
 * 额度探针的自动标记保留 —— 人工开关不能覆盖真实额度状态。
 */
export async function setDoubaoLinkEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/doubao-link', {
    enabled,
  })
  return res.data.data
}

/**
 * 设置「豆包链路最多接多少单」。
 *
 * 0 = 不限（默认状态）。正数 = 这一轮总共接多少单，接满后豆包链路自动关闭，
 * 后续请求落备胎班车。
 *
 * ⚠️ 设置上限会把接单计数**清零**：这是「给一批新的 N 单额度」的意思。
 * 运营手动重新打开开关时同样归零 —— 那也是「我给了一批新额度」。
 */
export async function setDoubaoLinkMaxTasks(
  maxTasks: number
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/doubao-link-max-tasks', {
    max_tasks: maxTasks,
  })
  return res.data.data
}

/**
 * 切换规则六（短时长分流）的全局链路开关 —— 与豆包链路开关同款语义。
 *
 * 打开：≤阈值（默认 10 秒）的短请求改派到心笔内链（480P 生成 + 超分 720P）；
 * 关闭：规则六对所有源模型停用，短请求走原链路。
 * 纯手动开关：心笔没有按天重置的额度，无自动恢复时间。
 */
export async function setShortLinkEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/short-link', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换规则六的「有赞标准版兜底」开关（2026-11-14）。
 *
 * 打开：心笔内链接不住（关闭 / 无可用渠道 / 契约不兼容）时，
 * ≤阈值的短请求改派有赞标准版 wan3.0-video（同一 adaptor，绕开
 * prime 的 PRIME_DURATION_TOO_SHORT）；
 * 关闭（默认）：不兜底，短请求走原链路。
 *
 * 优先级：心笔开关 > 本开关；两个都关闭 = 不路由。
 */
export async function setShortYouzanEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/short-youzan-link', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换规则一的「可选比例条件」（后台勾选）。
 *
 * 勾上 = 只有指定比例（后端默认 9:16）且时长超阈值的请求才改派目标链路；
 * 取消 = 清空闸门，任何比例超阈值都改派。
 *
 * 刻意不传 ratio：运营看到的是一个勾选框，比例由后端给默认值，
 * 前端再写一份 "9:16" 就是两个事实源，迟早会漂。需要换比例时
 * 由管理员改配置，不在抽屉里开放输入框。
 *
 * 与豆包链路开关分开：这个条件决定「哪些请求有资格改派」，
 * 开关决定「目标链路通不通」，两者互不覆盖。
 */
export async function setSecondsRuleRatioGate(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/ratio-gate', {
    enabled,
  })
  return res.data.data
}
