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

import type { VideoRouteFailoverState } from '../api'
import { buildFailoverView } from './failover-view'

function state(
  overrides: Partial<VideoRouteFailoverState> = {}
): VideoRouteFailoverState {
  return {
    failover_enabled: false,
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

describe('buildFailoverView', () => {
  test('未配置模型名时显示为未配置', () => {
    const view = buildFailoverView(
      state({ failover_configured: false, failover_source_model: '' })
    )
    assert.equal(view.tone, 'unset')
    assert.equal(view.statusKey, 'Failover not configured')
  })

  test('开关关闭且源链路正常时说明兜底目标', () => {
    const view = buildFailoverView(state())
    assert.equal(view.on, false)
    assert.equal(view.tone, 'off')
    assert.equal(view.statusKey, 'Failover is off')
    assert.ok(view.detailKey.includes('{{target}}'))
    assert.equal(view.sourceDown, false)
  })

  test('开关关闭但源链路已不可用时必须说出来', () => {
    const view = buildFailoverView(
      state({
        failover_unavailable: true,
        failover_unavailable_reason: '额度用完',
      })
    )
    assert.equal(view.sourceDown, true)
    assert.ok(view.detailKey.includes('but failover is off'))
  })

  test('开关开启且源链路不可用时显示生效中', () => {
    const view = buildFailoverView(
      state({
        failover_enabled: true,
        failover_unavailable: true,
        failover_unavailable_reason: '额度用完',
      })
    )
    assert.equal(view.on, true)
    assert.equal(view.tone, 'on')
    assert.equal(view.statusKey, 'Failover active')
    assert.equal(view.manuallyMarked, false)
  })

  test('人工标记与自动标记的文案不同', () => {
    const auto = buildFailoverView(
      state({
        failover_enabled: true,
        failover_unavailable: true,
        failover_unavailable_source: 'auto',
        failover_unavailable_reason: '额度用完',
      })
    )
    const manual = buildFailoverView(
      state({
        failover_enabled: true,
        failover_unavailable: true,
        failover_unavailable_source: 'manual',
        failover_unavailable_reason: '额度用完',
      })
    )
    assert.equal(auto.manuallyMarked, false)
    assert.equal(manual.manuallyMarked, true)
    assert.notEqual(manual.detailKey, auto.detailKey)
  })

  test('开关开启但源链路正常时是待命态', () => {
    const view = buildFailoverView(state({ failover_enabled: true }))
    assert.equal(view.on, true)
    assert.equal(view.tone, 'off')
    assert.equal(view.statusKey, 'Failover armed')
    assert.equal(view.sourceDown, false)
  })

  test('备胎无可用渠道时必须提醒', () => {
    const view = buildFailoverView(
      state({ failover_enabled: true, failover_target_available: false })
    )
    assert.equal(view.targetReady, false)
    assert.ok(view.detailKey.includes('no available channel'))
  })

  test('备胎可用时详情不出现缺渠道提示', () => {
    const view = buildFailoverView(state({ failover_enabled: true }))
    assert.equal(view.targetReady, true)
    assert.ok(view.detailKey.includes('if it goes down'))
  })
})
