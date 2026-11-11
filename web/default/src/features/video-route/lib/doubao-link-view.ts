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

export function buildDoubaoLinkView(
  policy: VideoRoutePolicy,
  failover: VideoRouteFailoverState | null
): DoubaoLinkView {
  const backup = failover?.failover_target_model ?? ''
  const sources = sourceList(policy)

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
    }
  }

  // 开着但源链路不可用（额度探针标记等）：降级展示，不误导成「关着」——
  // 开关反映的是人工意图，额度状态由自动标记另行表达。
  if (failover && failover.failover_configured && failover.failover_unavailable) {
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
  }
}
