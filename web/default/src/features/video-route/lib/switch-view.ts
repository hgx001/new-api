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
import type { VideoRoutePolicy } from '../api'

/**
 * 把后端策略翻译成「开关」该显示的样子。
 *
 * 单独抽成纯函数是因为这里最容易出错：`paused` 有两种来源（人工关的、
 * 额度探针自动关的），两种的文案和恢复方式完全不同。混在一起写进 JSX
 * 里就没人测得动了。
 */
export type SwitchView = {
  /** 开关本身的状态：true = 改派生效中。 */
  on: boolean
  /** 决定卡片配色：on 绿、off 琥珀。 */
  tone: 'on' | 'off'
  /** 状态行主文案的 i18n key。 */
  statusKey: string
  /** 状态行副文案的 i18n key。 */
  detailKey: string
  detailParams: Record<string, string | number>
  /** 关闭时是否展示「一直关着（30 天）」这个次要点。 */
  showLongOff: boolean
}

export const LONG_OFF_SECONDS = 30 * 24 * 3600

export function buildSwitchView(policy: VideoRoutePolicy): SwitchView {
  const on = !policy.paused
  const baseParams = {
    seconds: policy.trigger_seconds,
    target: policy.target_model,
    time: policy.paused_until_text,
    reason: policy.pause_reason,
  }

  if (on) {
    return {
      on: true,
      tone: 'on',
      statusKey: 'Routing is on',
      detailKey: 'Requests longer than {{seconds}}s are routed to {{target}}',
      detailParams: baseParams,
      showLongOff: false,
    }
  }

  // 额度探针自动关的：必须把原因和恢复时间都写出来，否则运营会以为
  // 是自己关的，然后到处找是谁关的。
  if (policy.pause_source === 'auto') {
    return {
      on: false,
      tone: 'off',
      statusKey: 'Routing is off',
      detailKey: policy.paused_until_text
        ? 'Auto-paused: {{reason}}. Back on at {{time}}'
        : 'Auto-paused: {{reason}}',
      detailParams: baseParams,
      showLongOff: true,
    }
  }

  return {
    on: false,
    tone: 'off',
    statusKey: 'Routing is off',
    detailKey: policy.paused_until_text
      ? 'Off until {{time}}, then back on automatically'
      : 'Off until you turn it back on',
    detailParams: baseParams,
    showLongOff: true,
  }
}
