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

import type { VideoRouteFailoverState, VideoRoutePolicy } from '../api'
import { buildDoubaoLinkView } from './doubao-link-view'

// 豆包链路的「最多接单数」（2026-11-13 老板定）：
//
//	「这批额度只接 3 单，接满就关掉，我手动再打开时又来 3 单。」
//
// 视图要表达三件事，缺一件运营就会误判：
//   1. 还能接几单（剩余）；
//   2. 为什么关（人工关 vs 额度接满 —— 后者没有自动恢复时间）；
//   3. 「重新打开会再给一批」这个承诺（不能与「限额关闭」混为一谈）。

function policy(overrides: Partial<VideoRoutePolicy> = {}): VideoRoutePolicy {
  return {
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
    trigger_seconds: 20,
    ratio_target_model: '',
    trigger_ratio: '',
    frames_target_model: '',
    // 规则六（短时长分流）：默认开关开启、目标为空（未配置）、阈值 10 秒。
    short_link_enabled: true,
    short_target_model: '',
    short_trigger_seconds: 10,
    // 规则六的有赞标准版兜底：默认关闭，目标固定 wan3.0-video。
    short_youzan_enabled: false,
    short_youzan_target_model: 'wan3.0-video',
    seconds_rule_ratio_gate: '',
    seconds_rule_ratio_gate_default: '9:16',
    updated_at: 0,
    doubao_link_off: false,
    doubao_link_off_until: 0,
    doubao_link_max_tasks: 0,
    doubao_link_accepted_tasks: 0,
    doubao_link_remaining: 0,
    doubao_link_off_reason: '',
    ...overrides,
  }
}

function failover(
  overrides: Partial<VideoRouteFailoverState> = {}
): VideoRouteFailoverState {
  return {
    failover_enabled: true,
    failover_configured: true,
    failover_source_model: 'seedance-2-5',
    failover_target_model: 'wan3.0-班车',
    failover_source_available: true,
    failover_target_available: true,
    failover_unavailable: false,
    failover_unavailable_reason: '',
    failover_unavailable_until: 0,
    failover_unavailable_source: '',
    failover_unavailable_until_text: '',
    // 规则五的备选备胎（2026-11-14）：默认关闭，关闭时走班车。
    failover_alt_enabled: false,
    failover_alt_model: '',
    failover_alt_target_model: 'cp-seedance-2.5flash',
    failover_alt_target_supported: true,
    failover_alt_target_available: true,
    failover_alt_upscale_configured: true,
    failover_alt_effective: false,
    failover_alt_note: '',
    ...overrides,
  }
}

describe('豆包链路接单上限', () => {
  test('没配上限：不限单，视图不显示限额信息', () => {
    const view = buildDoubaoLinkView(policy(), failover())

    assert.equal(view.maxTasks, 0, '上限 0 = 不限')
    assert.equal(view.accepting, true)
    assert.equal(view.remaining, 0)
    assert.equal(view.limitConfigured, false, '没配上限就不该显示限额控件')
  })

  test('配了上限：显示已接/剩余', () => {
    const view = buildDoubaoLinkView(
      policy({
        doubao_link_max_tasks: 3,
        doubao_link_accepted_tasks: 1,
        doubao_link_remaining: 2,
      }),
      failover()
    )

    assert.equal(view.limitConfigured, true)
    assert.equal(view.maxTasks, 3)
    assert.equal(view.acceptedTasks, 1)
    assert.equal(view.remaining, 2)
    assert.equal(view.accepting, true)
  })

  test('接满：链路关闭，原因是限额，不是人工关闭', () => {
    const view = buildDoubaoLinkView(
      policy({
        doubao_link_off: true,
        doubao_link_max_tasks: 3,
        doubao_link_accepted_tasks: 3,
        doubao_link_remaining: 0,
        doubao_link_off_reason: 'limit',
      }),
      failover()
    )

    assert.equal(view.on, false)
    assert.equal(view.accepting, false)
    assert.equal(view.limitReached, true)
    assert.equal(view.offReason, 'limit')
    assert.equal(view.hasAutoRecover, false, '限额关闭没有自动恢复时间')
  })

  test('人工关闭：保留自动恢复时间的承诺', () => {
    const view = buildDoubaoLinkView(
      policy({
        doubao_link_off: true,
        doubao_link_off_until: 1794592800,
        doubao_link_off_until_text: '2026-11-14 02:00:00',
        doubao_link_off_reason: 'manual',
      }),
      failover()
    )

    assert.equal(view.offReason, 'manual')
    assert.equal(view.hasAutoRecover, true)
  })

  test('限额关闭优先于人工关闭的自动恢复文案', () => {
    // 两者同时成立时，运营真正要的是「等我再打开」—— 手动关闭有恢复时间，
    // 限额满没有。视图不能把限额关闭说成「会自动恢复」。
    const view = buildDoubaoLinkView(
      policy({
        doubao_link_off: true,
        doubao_link_off_until: 1794592800,
        doubao_link_off_until_text: '2026-11-14 02:00:00',
        doubao_link_max_tasks: 3,
        doubao_link_accepted_tasks: 3,
        doubao_link_remaining: 0,
        doubao_link_off_reason: 'limit',
      }),
      failover()
    )

    assert.equal(view.offReason, 'limit')
    assert.equal(view.hasAutoRecover, false)
  })
})
