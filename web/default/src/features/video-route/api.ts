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
   * 规则六的**第三条分支**（CapCut 出口 cp-seedance-2.5flash）的开关。
   *
   * ⚠️ 三条短时长分支**同时只能有一条生效**：心笔内链 / 有赞标准版 /
   * CapCut 满血档。打开本条会自动关掉另外两条（后端写入口保证），
   * 读侧还有一层按「心笔 > 有赞 > CapCut」的优先级兜底。
   */
  short_capcut_enabled: boolean
  /** CapCut 满血档的目标模型。 */
  short_capcut_target_model: string
  /**
   * 两个短时长出口的**分界线**（秒，后端给值，默认 10）。
   *
   * < 该值 → CapCut 满血档；≥ 该值（且 ≤ short_trigger_seconds）→ 有赞标准版。
   * 卡片文案必须写出这条分界，否则运营看到两个按钮都亮着，会以为
   * 两个开关在跑同一批请求。
   *
   * 可选：老版本后端没有该字段时前端回退到 10（见两个短时长卡片的取值）。
   */
  short_capcut_min_seconds?: number
  /**
   * 当前生效的短时长分支（读侧优先级兜底后的结果）。
   * 空串 = 三条都没开 = 短请求走原链路。
   */
  short_active_target: string
  /** 配置上开着的短时长分支列表（按优先级排序）。 */
  short_active_targets: string[]
  /**
   * 规则七（电商/产品推荐内容 → 有赞优先，接不住才 CapCut）的总开关。
   *
   * 打开：提示词命中电商/产品推荐关键词（或勾选的参数条件命中）的请求
   * **留在常驻有赞链路**（10–29 秒走标准版、30 秒由 adaptor 映射 prime）；
   * 只有有赞物理接不住的（<10 秒 / 带参考音视频）才改派 cp-seedance-2.5flash。
   * 关闭（默认）：这类请求按原规则走。
   *
   * ⚠️ 带参考音视频的请求会改派 CapCut —— 有赞上游只收参考图。
   */
  ecommerce_enabled: boolean
  /** 规则七的兜底目标模型（默认 cp-seedance-2.5flash，有赞接不住时才用）。 */
  ecommerce_target_model: string
  /** 是否勾选「9:16 比例」入口。 */
  ecommerce_ratio_condition: boolean
  /** 是否勾选「时长超过阈值」入口。 */
  ecommerce_seconds_condition: boolean
  /** 「时长条件」的阈值（秒），**超过**该值命中。 */
  ecommerce_trigger_seconds: number
  /**
   * 「万相官网 → 客美 tj」手动换货开关（2026-11-19）。
   *
   * 打开：`wan3_guanwang_tj_source_model`（wan3.0-video-官网）的请求
   * 内部改名为 `wan3_guanwang_tj_target_model`（wan3.0-video-tj，渠道 32
   * 客美 Kemeitv）执行，计费仍按客户请求的官网模型；
   * 关闭（默认）：留在官网渠道。
   *
   * ⚠️ 与上面那批促销族规则**不同商品、不同源模型**，也不依赖
   * `enabled`（规则总开关）—— 两个按钮互不连坐。
   */
  wan3_guanwang_tj_enabled: boolean
  /** 换货开关的源模型（客户请求的那个名字）。 */
  wan3_guanwang_tj_source_model: string
  /** 换货开关的目标模型（实际执行的渠道上的那个名字）。 */
  wan3_guanwang_tj_target_model: string
  /**
   * 「hk-wan3 → 客美 tj」手动换货开关（2026-11-22）。
   *
   * 打开：`wan3_hk_wan3_tj_source_model`（hk-wan3）的请求（不带参考视频）
   * 内部改名为 `wan3_hk_wan3_tj_target_model`（wan3.0-video-tj，渠道 32
   * 客美 Kemeitv）执行，计费仍按客户请求的 hk-wan3；
   * 关闭（默认）：留在 hk-wan3。
   *
   * 带参考视频的请求不改派：客美会把参考视频秒数并入计费。
   */
  wan3_hk_wan3_tj_enabled: boolean
  /** 换货开关的源模型（客户请求的那个名字）。 */
  wan3_hk_wan3_tj_source_model: string
  /** 换货开关的目标模型（实际执行的渠道上的那个名字）。 */
  wan3_hk_wan3_tj_target_model: string
  /**
   * 「带参考视频的万相请求 → hk-wan3」开关（2026-11-20）。
   *
   * **缺省开启**（后端 nil = 开）：带 reference_video 的官网/tj/prime-tj
   * 请求（生成 4–15 秒）改派 hk-wan3 执行 —— hk-wan3 不收参考视频的钱，
   * 而官网适配器会把音视频素材整段丢弃、tj 会把参考视频秒数并入计费。
   * 关掉 = 这类请求留在原链路（即现状）。
   */
  wan3_ref_video_hk_enabled: boolean
  /** 这个开关有没有被显式配过（false = 一直在吃缺省「开启」）。 */
  wan3_ref_video_hk_configured: boolean
  /** 目标模型（固定 hk-wan3）。 */
  wan3_ref_video_hk_target_model: string
  /** 适用的源模型清单（官网 / tj / prime-tj）。 */
  wan3_ref_video_hk_source_models: string[]
  /** 可路由的生成时长下界（秒，hk-wan3 上游 4 秒起）。 */
  wan3_ref_video_hk_min_seconds?: number
  /** 可路由的生成时长上界（秒，带参考视频时 hk-wan3 上限 15 秒）。 */
  wan3_ref_video_hk_max_seconds?: number
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
   * 规则八（全量兜底 → cp-seedance-2.5flash）的开关。
   *
   * 打开：seedance2.5-特惠 / seedance2.5-活动 收到的**所有**请求无条件
   * 改派 cp-seedance-2.5flash，不看时长/比例/提示词内容/首尾帧。
   *
   * 用途只有一个：**源链路（有赞）彻底不能用**时的逃生口。日常不要开 ——
   * 一开就等于把这两个模型的全部流量挪到另一条上游。
   *
   * ⚠️ 目标链路的物理能力仍然生效：1080P / 2–3 秒 / 带首尾帧 /
   * 自适应比例带参考图 这四类仍留在原链路（改派过去必 400）。
   * 参考音视频 2026-11-15 起已放开，照常改派。
   *
   * ⚠️ 它**不依赖**规则总开关（enabled）—— 应急时不该要求先确认别的开关。
   */
  total_fallback_enabled: boolean
  /** 规则八的**长档**目标模型（16 秒及以上，默认 cp-seedance-2.5flash）。 */
  total_fallback_target_model: string
  /** 规则八的**短档**目标模型（≤15 秒，默认 cp-seedance-2.0）。 */
  total_fallback_short_target_model: string
  /** 短档时长上界（含），由后端给出，前端不硬编码。 */
  total_fallback_short_max_seconds: number
  /** 短档允许的最大参考图张数；超过时仍走长档，不丢回源链路。 */
  total_fallback_short_max_reference_images: number
  /**
   * 规则一的历史比例闸门。
   *
   * **2026-11-20 起已失效**：规则一不再看时长与比例，只按「带不带参考音视频」
   * 判定。后端也不再下发这两个字段（判定函数已删除），类型标记为可选只是为了
   * 兼容旧版本后端返回的策略快照。前端不展示、也不写入。
   */
  seconds_rule_ratio_gate?: string
  /** 同上，历史字段，后端已停发，仅供旧快照解析。 */
  seconds_rule_ratio_gate_default?: string
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
 * 切换规则六的**第三条分支**（CapCut 出口 cp-seedance-2.5flash）。
 *
 * 打开：心笔与有赞标准版都接不住时，≤阈值的短请求改派
 * cp-seedance-2.5flash；关闭（默认）：不走这条。
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
 * 切换规则七（电商/产品推荐内容 → 有赞优先，接不住才 CapCut）的总开关。
 *
 * 打开：seedance2.5-特惠 / seedance2.5-活动 的请求，只要提示词命中
 * 电商/产品推荐关键词（或勾选的参数条件命中），就**留在常驻有赞链路**；
 * 只有有赞接不住的（<10 秒 / 带参考音视频）才改派 cp-seedance-2.5flash。
 * 关闭（默认）：这类请求按原规则走。
 *
 * ⚠️ 带参考音视频的请求会改派 CapCut（有赞上游只收参考图）。
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
    ...(triggerSeconds === undefined
      ? {}
      : { trigger_seconds: triggerSeconds }),
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

/**
 * 切换规则八（全量兜底 → cp-seedance-2.5flash）。
 *
 * 打开：seedance2.5-特惠 / seedance2.5-活动 的**所有**请求无条件改派
 * cp-seedance-2.5flash —— 不看时长、不看比例、不看提示词内容、不看首尾帧。
 *
 * 只有一个用途：源链路（有赞）彻底不能用时的逃生口。日常必须关着 ——
 * 开着就等于把这两个模型的全部流量挪到另一条上游。
 *
 * ⚠️ 目标链路的物理能力边界仍然生效（1080P / 2–3 秒 / 参考视频音频 /
 * 自适应比例带参考图 留原链路），那几类改派过去必然 400。
 */
export async function setTotalFallbackEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/total-fallback', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换「万相官网 → 客美 tj」手动换货开关（2026-11-19）。
 *
 * 打开：wan3.0-video-官网 的请求内部改名为 wan3.0-video-tj（渠道 32 客美
 * Kemeitv）执行，计费仍按客户请求的官网模型；
 * 关闭（默认）：请求留在官网渠道。
 *
 * 与豆包/规则六七八那批**不同商品、不同源模型**，也**不受它们的总开关
 * Enabled 影响** —— 两个按钮互不连坐。
 *
 * 中间件会放行两类请求（留在官网）：目标模型在当前分组没有可用渠道、
 * 请求画幅是 adaptive（客美没有这一档）。
 */
export async function setWan3GuanwangTjEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/wan3-guanwang-tj', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换「hk-wan3 → 客美 tj」开关（2026-11-22，默认关闭）。
 *
 * 打开：`wan3_hk_wan3_tj_source_model`（hk-wan3）上**不带参考视频**的请求改派
 * `wan3_hk_wan3_tj_target_model`（wan3.0-video-tj，渠道 32 客美）执行，
 * 计费仍按客户请求的 hk-wan3；关闭：留在 hk-wan3。
 */
export async function setWan3HkWan3TjEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/wan3-hk-wan3-tj', {
    enabled,
  })
  return res.data.data
}

/**
 * 切换「带参考视频的万相请求 → hk-wan3」开关（2026-11-20）。
 *
 * 打开（默认）：`wan3_ref_video_hk_source_models`（官网 / tj / prime-tj）上带
 * `reference_video`、生成时长 4–15 秒的请求改派 `wan3_ref_video_hk_target_model`
 * （hk-wan3）执行 —— 该模型**不收参考视频的钱**，计费仍按客户请求的源模型。
 * 关闭：这类请求留在原链路（官网会丢弃素材、tj 会把参考视频秒数并入计费）。
 *
 * 准入不满足的请求即使开关开着也留在原链路：时长不在 4–15 秒、画幅 adaptive、
 * 带首尾帧、素材超 hk-wan3 上限、目标模型无可用渠道。
 */
export async function setWan3RefVideoHkEnabled(
  enabled: boolean
): Promise<VideoRoutePolicy> {
  const res = await api.post('/api/video-route/policy/wan3-ref-video-hk', {
    enabled,
  })
  return res.data.data
}
