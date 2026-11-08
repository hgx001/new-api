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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { VideoRoutePolicy } from '../api'
import { buildSwitchView, LONG_OFF_SECONDS } from './switch-view'

const base: VideoRoutePolicy = {
  paused: false,
  paused_until: 0,
  paused_until_text: '',
  pause_source: '',
  pause_reason: '',
  seconds_rule_enabled: true,
  ratio_rule_enabled: true,
  frames_rule_enabled: true,
  source_switches: [],
  source_model: 'seedance2.5-特惠',
  target_model: 'seedance-2-5',
  trigger_seconds: 30,
  ratio_target_model: 'wan3.0-720p-特惠',
  trigger_ratio: '9:16',
  frames_target_model: 'wan3.0-720p-特惠',
  updated_at: 1793507950,
}

describe('buildSwitchView', () => {
  test('没暂停时开关是开的，并说明改派规则', () => {
    const view = buildSwitchView(base)

    assert.equal(view.on, true)
    assert.equal(view.tone, 'on')
    assert.equal(view.statusKey, 'Routing is on')
    assert.equal(
      view.detailKey,
      'Requests longer than {{seconds}}s are routed to {{target}}'
    )
    assert.equal(view.detailParams.seconds, 30)
    assert.equal(view.detailParams.target, 'seedance-2-5')
    assert.equal(view.showLongOff, false)
  })

  test('人工关闭且有时限时，把恢复时间写清楚', () => {
    const view = buildSwitchView({
      ...base,
      paused: true,
      pause_source: 'manual',
      paused_until_text: '2026-11-02 00:00:00',
    })

    assert.equal(view.on, false)
    assert.equal(view.tone, 'off')
    assert.equal(view.statusKey, 'Routing is off')
    assert.equal(
      view.detailKey,
      'Off until {{time}}, then back on automatically'
    )
    assert.equal(view.detailParams.time, '2026-11-02 00:00:00')
    assert.equal(view.showLongOff, true)
  })

  test('人工关闭但没有时限时，不能说「会自动恢复」', () => {
    const view = buildSwitchView({
      ...base,
      paused: true,
      pause_source: 'manual',
    })

    assert.equal(view.on, false)
    // 没有时限却提示「到点自动恢复」，运营会等一个永远不会来的恢复。
    assert.equal(view.detailKey, 'Off until you turn it back on')
  })

  test('自动暂停要同时给出原因和恢复时间', () => {
    const view = buildSwitchView({
      ...base,
      paused: true,
      pause_source: 'auto',
      pause_reason: '上游渠道今日额度已用完',
      paused_until_text: '2026-11-02 00:00:00',
    })

    assert.equal(view.on, false)
    assert.equal(view.detailKey, 'Auto-paused: {{reason}}. Back on at {{time}}')
    assert.equal(view.detailParams.reason, '上游渠道今日额度已用完')
    assert.equal(view.detailParams.time, '2026-11-02 00:00:00')
  })

  test('自动暂停但拿不到原因时也要能渲染，不留空占位', () => {
    const view = buildSwitchView({
      ...base,
      paused: true,
      pause_source: 'auto',
    })

    assert.equal(view.detailKey, 'Auto-paused: {{reason}}')
    assert.equal(view.on, false)
  })

  test('未知的 pause_source 按人工处理，不会误报成自动', () => {
    const view = buildSwitchView({ ...base, paused: true, pause_source: '' })

    assert.equal(view.detailKey, 'Off until you turn it back on')
  })

  test('30 天的秒数正好是后端允许的上限', () => {
    assert.equal(LONG_OFF_SECONDS, 2592000)
  })
})
