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
    // 规则六（短时长分流）：默认开关开启、目标为空（未配置）、阈值 10 秒。
    short_link_enabled: true,
    short_target_model: '',
    short_trigger_seconds: 10,
    // 规则六的有赞标准版兜底：默认关闭，目标固定 wan3.0-video。
    short_youzan_enabled: false,
    short_youzan_target_model: 'wan3.0-video',
    // 规则六的第三条分支（CapCut 快速档）：默认关闭；三路互斥的当前生效方为空串。
    short_capcut_enabled: false,
    short_capcut_target_model: 'cp-seedance-2.0fast',
    short_active_target: '',
    short_active_targets: [],
    // 规则七（电商内容路由）：默认关闭；参数条件默认都不勾；阈值 20 秒。
    ecommerce_enabled: false,
    ecommerce_target_model: 'cp-seedance-2.5flash',
    ecommerce_ratio_condition: false,
    ecommerce_seconds_condition: false,
    ecommerce_trigger_seconds: 20,
    // 万相官网 → 客美 tj 手动换货（2026-11-19）：默认关闭。
    wan3_guanwang_tj_enabled: false,
    wan3_guanwang_tj_source_model: 'wan3.0-video-官网',
    wan3_guanwang_tj_target_model: 'wan3.0-video-tj',
    // 带参考视频 → hk-wan3（2026-11-20）：默认开启，旧 option 没配过。
    wan3_ref_video_hk_enabled: true,
    wan3_ref_video_hk_configured: false,
    wan3_ref_video_hk_target_model: 'hk-wan3',
    wan3_ref_video_hk_source_models: [
      'wan3.0-video-官网',
      'wan3.0-video-tj',
      'wan3.0-video-prime-tj',
    ],
    ecommerce_ratio_condition_default: '9:16',
    ecommerce_keywords: [],
    ecommerce_keywords_default: [],
    // 规则八（全量兜底）：默认关闭，目标固定 cp-seedance-2.5flash。
    total_fallback_enabled: false,
    total_fallback_target_model: 'cp-seedance-2.5flash',
    total_fallback_short_target_model: 'cp-seedance-2.0',
    total_fallback_short_max_seconds: 15,
    total_fallback_short_max_reference_images: 9,
    seconds_rule_ratio_gate: '',
    seconds_rule_ratio_gate_default: '9:16',
    updated_at: 0,
    doubao_link_off: false,
    doubao_link_off_until: 0,
    // 接单上限（2026-11-13 老板定）：默认 0 = 不限，剩余 0，关闭原因为空。
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
    // effective=false 是「开关关着」的正常状态，不是异常。
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

describe('buildDoubaoLinkView', () => {
  test('开启：规则一改派到豆包，详情带触发条件/目标/兜底', () => {
    const view = buildDoubaoLinkView(policy(), failover())
    assert.equal(view.on, true)
    assert.equal(view.tone, 'on')
    assert.equal(view.statusKey, 'Doubao link is on')
    // 2026-11-20 起触发条件是「不带参考视频/音频」，不再有阈值参数。
    // 文案里出现 {{seconds}} 会让运营以为还在按秒数卡。
    assert.ok(!view.detailKey.includes('{{seconds}}'))
    assert.ok(view.detailKey.includes('reference video/audio'))
    assert.ok(view.detailKey.includes('{{target}}'))
    assert.ok(view.detailKey.includes('{{backup}}'))
    assert.equal(view.detailParams.target, 'seedance-2-5')
    assert.equal(view.detailParams.backup, 'wan3.0-班车')
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
      policy({
        doubao_link_off: true,
        doubao_link_off_until_text: '2026-11-12 02:00:00',
      }),
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
