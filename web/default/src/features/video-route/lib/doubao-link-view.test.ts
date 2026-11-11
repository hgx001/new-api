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

function policy(
  overrides: Partial<VideoRoutePolicy> = {}
): VideoRoutePolicy {
  return {
    paused: false,
    paused_until: 0,
    paused_until_text: '',
    pause_source: '',
    pause_reason: '',
    seconds_rule_enabled: true,
    ratio_rule_enabled: true,
    frames_rule_enabled: true,
    source_switches: [
      {
        source_model: 'seedance2.5-特惠',
        seconds_rule_enabled: true,
        target_model: 'seedance-2-5',
      },
      {
        source_model: 'seedance2.5-活动',
        seconds_rule_enabled: true,
        target_model: 'seedance-2-5',
      },
    ],
    source_model: '',
    target_model: 'seedance-2-5',
    trigger_seconds: 20,
    ratio_target_model: '',
    trigger_ratio: '',
    frames_target_model: '',
    updated_at: 0,
    doubao_link_off: false,
    doubao_link_off_until: 0,
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
    ...overrides,
  }
}

describe('buildDoubaoLinkView', () => {
  test('开启：规则一改派到豆包，详情带阈值/目标/兜底', () => {
    const view = buildDoubaoLinkView(policy(), failover())
    assert.equal(view.on, true)
    assert.equal(view.tone, 'on')
    assert.equal(view.statusKey, 'Doubao link is on')
    assert.ok(view.detailKey.includes('{{seconds}}'))
    assert.ok(view.detailKey.includes('{{target}}'))
    assert.ok(view.detailKey.includes('{{backup}}'))
    assert.equal(view.detailParams.target, 'seedance-2-5')
    assert.equal(view.detailParams.backup, 'wan3.0-班车')
    assert.equal(view.detailParams.seconds, 20)
    assert.equal(view.quotaDown, false)
  })

  test('关闭：带自动恢复时间与常驻链路去向', () => {
    const view = buildDoubaoLinkView(
      policy({
        doubao_link_off: true,
        doubao_link_off_until: 1794442800,
        doubao_link_off_until_text: '2026-11-12 02:00:00',
      }),
      failover()
    )
    assert.equal(view.on, false)
    assert.equal(view.tone, 'off')
    assert.equal(view.statusKey, 'Doubao link is off')
    assert.ok(view.detailKey.includes('{{time}}'))
    assert.ok(view.detailKey.includes('{{backup}}'))
    assert.ok(view.detailKey.includes('{{sources}}'))
    assert.equal(view.detailParams.time, '2026-11-12 02:00:00')
    assert.equal(
      view.detailParams.sources,
      'seedance2.5-特惠, seedance2.5-活动'
    )
    assert.equal(view.quotaDown, false)
  })

  test('开启但源链路被探针标记不可用：降级提示，不误导成「关着」', () => {
    const view = buildDoubaoLinkView(
      policy(),
      failover({
        failover_unavailable: true,
        failover_unavailable_reason: '上游额度已用完',
      })
    )
    assert.equal(view.on, true, '开关位置反映人工意图，不被探针翻转')
    assert.equal(view.tone, 'degraded')
    assert.equal(view.statusKey, 'Source chain unavailable')
    assert.ok(view.detailKey.includes('{{reason}}'))
    assert.equal(view.detailParams.reason, '上游额度已用完')
    assert.equal(view.quotaDown, true)
  })

  test('关闭优先于探针标记：先看开关，再看额度', () => {
    const view = buildDoubaoLinkView(
      policy({ doubao_link_off: true, doubao_link_off_until_text: '2026-11-12 02:00:00' }),
      failover({ failover_unavailable: true })
    )
    assert.equal(view.on, false)
    assert.equal(view.tone, 'off')
    assert.equal(view.quotaDown, false)
  })

  // 备胎模型没配置时，关闭开关无法兑现「seedance-2-5 改派备胎」的承诺
  // （后端 target 为空直接放行回豆包）—— 视图必须把这件事暴露给卡片去禁用开关。
  test('备胎未配置：fallbackReady=false，卡片据此禁用开关', () => {
    const view = buildDoubaoLinkView(
      policy(),
      failover({ failover_configured: false, failover_target_model: '' })
    )
    assert.equal(view.fallbackReady, false)
  })

  test('备胎已配置时 fallbackReady=true；状态整体缺失时也不放行', () => {
    assert.equal(buildDoubaoLinkView(policy(), failover()).fallbackReady, true)
    assert.equal(buildDoubaoLinkView(policy(), null).fallbackReady, false)
  })
})
