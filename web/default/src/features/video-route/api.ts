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
   * 规则六的**第三条分支**（CapCut 快速档 cp-seedance-2.0fast）的开关。
   *
   * ⚠️ 三条短时长分支**同时只能有一条生效**：心笔内链 / 有赞标准版 /
   * CapCut 快速档。打开本条会自动关掉另外两条（后端写入口保证），
   * 读侧还有一层按「心笔 > 有赞 > CapCut」的优先级兜底。
   */
  short_capcut_enabled: boolean
  /** CapCut 快速档的目标模型。 */
  short_capcut_target_model: string
  /**
   * 当前生效的短时长分支（读侧优先级兜底后的结果）。
   * 空串 = 三条都没开 = 短请求走原链路。
   */
  short_active_target: string
  /** 配置上开着的短时长分支列表（按优先级排序）。 */
  short_active_targets: string[]
  /**
   * 规则七（电商/产品推荐内容 → CapCut 快速档）的总开关。
   *
   * 打开：提示词命中电商/产品推荐关键词（或勾选的参数条件命中）的请求
   * 改派 cp-seedance-2.5flash；关闭（默认）：这类请求留在源链路。
   *
   * ⚠️ 带参考视频的请求**永远不路由**，与本开关无关。
   */
  ecommerce_enabled: boolean
  /** 规则七的目标模型（默认 cp-seedance-2.5flash）。 */
  ecommerce_target_model: string
  /** 是否勾选「9:16 比例」入口。 */
  ecommerce_ratio_condition: boolean
  /** 是否勾选「时长超过阈值」入口。 */
  ecommerce_seconds_condition: boolean
  /** 「时长条件」的阈值（秒），**超过**该值命中。 */
  ecommerce_trigger_seconds: number
  /**
   * 「9:16 比例」入口对应的比例 —— 由后端给出唯一事实源，
   * 前端不硬编码比例字符串（两处各写一份必然漂）。
   */
  ecommerce_ratio_condition_default: string
  /** 当前生效的电商关键词表（运营配了就给配的，否则给默认表）。 */
  ecommerce_keywords: string[]
  /** 默认关键词表（供前端做「恢复默认」用）。 */
  ecommerce_keywords_default: string[]
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
  /**
   * 备选备胎（CapCut 2.5 Flash）的开关 —— 决定「改派到哪一条」。
   *
   * 与 `failover_enabled` 是两件事：后者决定「要不要改派」，
   * 这个决定改派到班车还是 CapCut。两者都开着时 CapCut 优先。
   */
  failover_alt_enabled: boolean
  /** 配置的备选模型名（可能为空 = 用默认值）。 */
  failover_alt_model: string
  /** 生效的备选模型名（空配置时回落默认 cp-seedance-2.5flash）。 */
  failover_alt_target_model: string
  /** 备选模型名是否被本链路支持（只有 2.5 Flash 一档生效）。 */
  failover_alt_target_supported: boolean
  /** CapCut 那条路上当前有没有可用渠道。 */
  failover_alt_target_available: boolean
  /** 超分链路（腾讯 MPS）是否配好 —— 没配就只能走班车。 */
  failover_alt_upscale_configured: boolean
  /**
   * 综合结论：开关 + 模型名 + 超分配置 + 渠道可用 四项全满足。
   *
   * ⚠️ 只有它为 true 时「豆包挂了真的会走 CapCut」。开关开着但它是 false
   * 的四种情况必须由 `failover_alt_note` 说明，否则运营会以为已经生效。
   */
  failover_alt_effective: boolean
  /** 一句话说明当前处于哪种状态（后端算，前端直接展示）。 */
  failover_alt_note: string
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
 * 切换规则五的**备选备胎**（CapCut 2.5 Flash，480p 生成 + 超分 720P 交付）。
 *
 * 打开：豆包不可用时改派 cp-seedance-2.5flash（按 480p 生成，交付前用腾讯
 * MPS 超分到 720P）；关闭（默认）：仍走 wan3.0-班车。
 *
 * 前提不变：只有豆包链路真的不可用（渠道被关 / AutoBan / 额度探针标记 /
 * 豆包链路开关人工关闭）时才触发，豆包好用的时候这条链路永远不参与调度。
 *
 * ⚠️ 超分没配好时后端会自动回落班车 —— 返回的 `failover_alt_effective`
 * 会是 false，界面必须据此提示「已打开但不会生效」。
 */
export async function setFailoverAltEnabled(
  enabled: boolean,
  model?: string
): Promise<VideoRouteFailoverState> {
  const res = await api.post('/api/video-route/policy/failover/alt', {
    enabled,
    ...(model === undefined ? {} : { model }),
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

/**
 * 切换规则六的**第三条分支**（CapCut 快速档 cp-seedance-2.0fast）。
 *
 * 打开：心笔与有赞标准版都接不住时，≤阈值的短请求改派
 * cp-seedance-2.0fast；关闭（默认）：不走这条。
 *
 * ⚠️ 打开本条会**自动关掉**另外两条短时长分支（心笔内链 / 有赞标准版）——
 * 三条是同一个时长区间的三个出口，同时开着时「落到哪一条」必须是明确的
 * 经营意图，而不是「谁写在配置里靠前」。关掉本条时不动另外两条。
 */
export async function setShortCapcutEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/short-capcut-link', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换规则七（电商/产品推荐内容 → CapCut 快速档）的总开关。
 *
 * 打开：seedance2.5-特惠 / seedance2.5-活动 的请求，只要提示词命中
 * 电商/产品推荐关键词（或勾选的参数条件命中），就改派 cp-seedance-2.5flash；
 * 关闭（默认）：这类请求留在源链路。
 *
 * ⚠️ 带参考视频的请求**永远不路由**（老板明确要求），与本开关无关。
 */
export async function setEcommerceEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/ecommerce', {
    enabled,
  })
  return res.data.data
}

/**
 * 设置规则七的**参数条件**（后台勾选）。
 *
 * 命中 = 内容命中（电商关键词）**或** 参数命中（勾选的项）——
 * 两者是并列的进料口，不是串联的闸门。
 *
 * - ratioEnabled：勾上「9:16 比例」入口（比例由后端给默认值）；
 * - secondsEnabled：勾上「时长超过阈值」入口；
 * - triggerSeconds：时长阈值；不传则沿用当前配置（避免「只改勾选却把阈值重置」）。
 */
export async function setEcommerceConditions(
  ratioEnabled: boolean,
  secondsEnabled: boolean,
  triggerSeconds?: number
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/ecommerce/conditions', {
    ratio_enabled: ratioEnabled,
    seconds_enabled: secondsEnabled,
    ...(triggerSeconds === undefined ? {} : { trigger_seconds: triggerSeconds }),
  })
  return res.data.data
}

/**
 * 设置规则七的电商关键词表。
 *
 * 判定口径是大小写不敏感的**子串**匹配。传空数组 = 回落后端默认表
 * （不是「关掉内容判定」—— 内容判定是这条规则的主入口）。
 */
export async function setEcommerceKeywords(
  keywords: string[]
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/ecommerce/keywords', {
    keywords,
  })
  return res.data.data
}
