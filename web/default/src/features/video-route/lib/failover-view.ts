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
import type { VideoRouteFailoverState } from '../api'

/**
 * 规则五（故障转移）的展示视图。
 *
 * 单独抽成纯函数是因为这里最容易出错：界面要同时表达**三件独立的事**，
 * 混在一起写进 JSX 里就没人测得动了：
 *
 *  1. 规则五开没开（failover_enabled）—— 经营决策；
 *  2. 源链路现在好不好使（failover_unavailable）—— 运行状态；
 *  3. 不可用的**原因**（渠道被关 / 额度用完 / 人工标记）—— 要分别对待。
 *
 * 三者的组合有 6 种，每种的文案与配色都不同，用 if 嵌套写必然出错。
 */
export type FailoverView = {
  /** 开关本身的状态：true = 故障转移生效中。 */
  on: boolean
  /** 卡片配色：正常绿、异常琥珀、未配置灰。 */
  tone: 'on' | 'off' | 'unset'
  /** 状态行主文案的 i18n key。 */
  statusKey: string
  /** 状态行副文案的 i18n key。 */
  detailKey: string
  detailParams: Record<string, string | number>
  /** 源链路当前是否不可用（决定要不要显示「标记/解除」按钮）。 */
  sourceDown: boolean
  /** 不可用标记是否由人工写入（人工标记不会被一次成功自动解除）。 */
  manuallyMarked: boolean
  /** 备胎链路当前是否可用（不可用时提示「改派了也跑不了」）。 */
  targetReady: boolean
}

export function buildFailoverView(
  state: VideoRouteFailoverState
): FailoverView {
  const baseParams = {
    source: state.failover_source_model,
    target: state.failover_target_model,
    reason: state.failover_unavailable_reason,
    time: state.failover_unavailable_until_text,
  }

  const manuallyMarked = state.failover_unavailable_source === 'manual'

  // 没配模型名：开关点不动（后端会拒绝），卡片只说明缺什么。
  if (!state.failover_configured) {
    return {
      on: false,
      tone: 'unset',
      statusKey: 'Failover not configured',
      detailKey:
        'Set the source and backup models before enabling failover ({{source}} → {{target}})',
      detailParams: baseParams,
      sourceDown: state.failover_unavailable,
      manuallyMarked,
      targetReady: state.failover_target_available,
    }
  }

  if (!state.failover_enabled) {
    return {
      on: false,
      tone: 'off',
      statusKey: 'Failover is off',
      detailKey: state.failover_unavailable
        ? 'Source {{source}} is down ({{reason}}), but failover is off'
        : 'When {{source}} is down, requests are routed to {{target}}',
      detailParams: baseParams,
      sourceDown: state.failover_unavailable,
      manuallyMarked,
      targetReady: state.failover_target_available,
    }
  }

  // 开关开着 + 源链路不可用 = 故障转移正在生效。
  if (state.failover_unavailable) {
    return {
      on: true,
      tone: 'on',
      statusKey: 'Failover active',
      detailKey: manuallyMarked
        ? 'Source {{source}} marked unavailable ({{reason}}), requests go to {{target}}'
        : 'Source {{source}} is down ({{reason}}), requests go to {{target}}',
      detailParams: baseParams,
      sourceDown: true,
      manuallyMarked,
      targetReady: state.failover_target_available,
    }
  }

  // 开关开着但源链路正常 = 规则待命。此时备胎不可用是要提醒的：
  // 真出事的时候改派过去也跑不了。
  return {
    on: true,
    tone: 'off',
    statusKey: 'Failover armed',
    detailKey: state.failover_target_available
      ? 'Source {{source}} is healthy; requests fall back to {{target}} if it goes down'
      : 'Source {{source}} is healthy, but {{target}} has no available channel',
    detailParams: baseParams,
    sourceDown: false,
    manuallyMarked,
    targetReady: state.failover_target_available,
  }
}
