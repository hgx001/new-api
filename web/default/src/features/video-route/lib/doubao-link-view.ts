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
import type { VideoRouteFailoverState, VideoRoutePolicy } from '../api'

/**
 * 把后端策略翻译成「豆包链路开关」该显示的样子。
 *
 * 一个开关 = 豆包通不通：运营读一个状态就行，不用分别理解
 * 「跨模型改派」与「故障转移」两张卡 —— 那两张卡曾让运营以为
 * 关掉成本优化会把出片兜底一起关掉。
 *
 * 单独抽成纯函数：关闭 / 正常 / 降级三种形态的文案与配色各不相同，
 * 写进 JSX 就没人测得动了。
 */
export type DoubaoLinkView = {
  /** 开关位置：true = 豆包可用。反映人工意图，不被额度探针翻转。 */
  on: boolean
  /** 决定卡片配色：on 绿、off 琥珀、degraded 灰。 */
  tone: 'on' | 'off' | 'degraded'
  /** 状态行主文案的 i18n key。 */
  statusKey: string
  /** 状态行副文案的 i18n key。 */
  detailKey: string
  detailParams: Record<string, string | number>
  /** 开着但源链路被判定不可用（额度耗尽等，自动降级中）。 */
  quotaDown: boolean
  /**
   * 备胎链路是否已配置齐全（源/目标模型都在）。
   *
   * 关闭开关承诺「seedance-2-5 改派备胎」，备胎没配置时后端会直接放行
   * 原请求（仍打已宣布关闭的豆包）—— 卡片必须据此禁用开关，否则承诺落空。
   */
  fallbackReady: boolean
  /**
   * 规则一「可选比例条件」是否勾上。
   *
   * 与 `on` 是两件事：这里表达「哪些请求有资格改派」，
   * `on` 表达「目标链路通不通」。勾上条件不等于关掉链路，
   * 取消条件也不等于打开链路 —— 混成一个布尔会让运营误判影响面。
   */
  ratioGateOn: boolean
  /** 勾选框标签里显示的比例；未勾选时给默认值，让运营知道勾上会要求什么。 */
  ratioGateValue: string

  // ─────────────────── 豆包链路接单上限（2026-11-13） ───────────────────

  /**
   * 「最多接多少单」是否已配置。
   *
   * 上限 0 = 不限（默认状态）。前端据此决定**要不要显示限额控件** ——
   * 不限时显示一个空输入框，运营会以为「限额默认是 0（不接单）」。
   */
  limitConfigured: boolean
  /** 上限值。0 = 不限。 */
  maxTasks: number
  /** 本轮已受理多少单。 */
  acceptedTasks: number
  /** 还能接几单。0 = 不限。 */
  remaining: number
  /** 链路当前是否还在接单。false = 已关闭（人工关或接满）。 */
  accepting: boolean
  /** 是否已接满上限。 */
  limitReached: boolean
  /**
   * 关闭原因：'manual'（人工关，有自动恢复时间）/ 'limit'（接满，等人再打开）/ ''（没关）。
   *
   * 两者对应的下一步动作完全不同：人工关 = 「等凌晨 2 点」，
   * 接满 = 「再给我一批额度」。混在一起运营会误判。
   */
  offReason: '' | 'manual' | 'limit'
  /**
   * 关闭后是否有自动恢复时间。
   *
   * 只有人工关闭有。限额关闭**没有** —— 额度是运营手动给的，
   * 视图说成「会自动恢复」就是在骗运营。
   */
  hasAutoRecover: boolean
}

/**
 * 从策略派生勾选状态。
 *
 * 配置值带首尾空格也算「已勾上」（生产上出现过带空格的模型名，
 * 配置侧一律 Trim）；但显示给运营时要用 Trim 后的值，否则标签上
 * 会莫名多出空白。
 *
 * 未勾选时显示的默认比例取自后端给的 seconds_rule_ratio_gate_default，
 * 比例只有一个事实源，改默认值不用改前后端两处。末尾的 '9:16' 是
 * 后端没给这个字段时的兜底（前后端同版本发布，正常走不到）。
 */
function ratioGateState(
  policy: VideoRoutePolicy
): Pick<DoubaoLinkView, 'ratioGateOn' | 'ratioGateValue'> {
  const value = (policy.seconds_rule_ratio_gate ?? '').trim()
  return {
    ratioGateOn: value.length > 0,
    ratioGateValue: value || policy.seconds_rule_ratio_gate_default || '9:16',
  }
}

/** 源模型清单：按源开关列表取，回落到单值字段。 */
function sourceList(policy: VideoRoutePolicy): string {
  const names = policy.source_switches
    .map((item) => item.source_model)
    .filter(Boolean)
  if (names.length > 0) {
    return names.join(', ')
  }
  return policy.source_model
}

/**
 * 从策略派生「接单上限」相关状态。
 *
 * 数值全部取自后端下发（policy 里的三个字段），前端**不做算术** ——
 * 「剩余 = 上限 - 已接」在前端算就多了一份真相源，后端改口径时前端会跟着错。
 */
function limitState(
  policy: VideoRoutePolicy
): Pick<
  DoubaoLinkView,
  | 'limitConfigured'
  | 'maxTasks'
  | 'acceptedTasks'
  | 'remaining'
  | 'limitReached'
> {
  const maxTasks = Math.max(0, policy.doubao_link_max_tasks ?? 0)
  const acceptedTasks = Math.max(0, policy.doubao_link_accepted_tasks ?? 0)
  const remaining = Math.max(0, policy.doubao_link_remaining ?? 0)
  return {
    limitConfigured: maxTasks > 0,
    maxTasks,
    acceptedTasks,
    remaining,
    limitReached: policy.doubao_link_off_reason === 'limit',
  }
}

export function buildDoubaoLinkView(
  policy: VideoRoutePolicy,
  failover: VideoRouteFailoverState | null
): DoubaoLinkView {
  const backup = failover?.failover_target_model ?? ''
  const sources = sourceList(policy)
  // 备胎链路配置齐全（failover_configured = 源+目标都在）才谈得上"关闭后改派备胎"。
  const fallbackReady = Boolean(failover?.failover_configured)
  const limit = limitState(policy)
  const offReason = policy.doubao_link_off_reason ?? ''
  // 有自动恢复时间 = 人工关闭。限额关闭没有恢复时间，必须等人再打开。
  const hasAutoRecover = offReason === 'manual'

  // 关闭态优先：开关是运营的主动意图，先于任何自动判断展示。
  if (policy.doubao_link_off) {
    return {
      on: false,
      tone: 'off',
      statusKey: 'Doubao link is off',
      detailKey:
        backup && sources && policy.doubao_link_off_until_text
          ? 'Off until {{time}}: {{target}} routes to {{backup}}, {{sources}} stay on resident channels'
          : 'Off until {{time}}',
      detailParams: {
        time: policy.doubao_link_off_until_text ?? '',
        target: policy.target_model,
        backup,
        sources,
      },
      quotaDown: false,
      fallbackReady,
      ...ratioGateState(policy),
      ...limit,
      accepting: false,
      offReason,
      hasAutoRecover,
    }
  }

  // 开着但源链路不可用（额度探针标记等）：降级展示，不误导成「关着」——
  // 开关反映的是人工意图，额度状态由自动标记另行表达。
  if (
    failover &&
    failover.failover_configured &&
    failover.failover_unavailable
  ) {
    return {
      on: true,
      tone: 'degraded',
      statusKey: 'Source chain unavailable',
      detailKey: 'Fallback active: requests go to {{backup}} ({{reason}})',
      detailParams: {
        backup,
        reason: failover.failover_unavailable_reason,
      },
      quotaDown: true,
      fallbackReady,
      ...ratioGateState(policy),
      ...limit,
      accepting: true,
      offReason,
      hasAutoRecover,
    }
  }

  return {
    on: true,
    tone: 'on',
    statusKey: 'Doubao link is on',
    detailKey:
      'Requests longer than {{seconds}}s route to {{target}}; auto-fallback to {{backup}} when the source chain goes down',
    detailParams: {
      seconds: policy.trigger_seconds,
      target: policy.target_model,
      backup,
    },
    quotaDown: false,
    fallbackReady,
    ...ratioGateState(policy),
    ...limit,
    accepting: true,
    offReason,
    hasAutoRecover,
  }
}
