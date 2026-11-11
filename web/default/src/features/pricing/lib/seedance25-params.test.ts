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
/**
 * seedance2.5 促销族的**参数表**守卫。
 *
 * 参数表是客户判断「这个模型能传什么」的地方。它一旦与后端校验口径不一致，
 * 客户会传一个后端必然 400 的参数 —— 而且是「参数表说可以、实际不行」，
 * 属于最难解释的那一类问题。
 *
 * 历史偏差（均已修复，保留作回归背景）：
 *   1. seedance2.5-活动 原先落到通用 VIDEO_PARAMS → 显示「duration 1~60」，
 *      实际当时锁死 30 秒（2026-11-11 起放开 2-30 秒逐秒可选）。
 *   2. seedance2.5-特惠 原先落到通用 wan3 表 → 显示「resolution 480P/720P/1080P，
 *      默认 480P」，实际它是 prime 别名，只接受 **720P/1080P，默认 720P**。
 */
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { PricingModel } from '../types'
import { buildSupportedParameters } from './mock-stats'
import {
  SEEDANCE25_HUODONG,
  SEEDANCE25_TEHUI,
  getSeedance25SampleSpec,
} from './seedance25-family'

function videoModel(model_name: string): PricingModel {
  return {
    id: 1,
    model_name,
    quota_type: 1,
    model_ratio: 0,
    model_price: 0,
    completion_ratio: 0,
    enable_groups: ['default'],
    supported_endpoint_types: ['openai-video'],
  } as unknown as PricingModel
}

function parametersByName(modelName: string) {
  return Object.fromEntries(
    buildSupportedParameters(videoModel(modelName)).map((parameter) => [
      parameter.name,
      parameter,
    ])
  )
}

describe('seedance2.5 促销族的参数表', () => {
  // 活动版：按次档。duration 2-30 秒可选（默认 30），resolution 只有 720P。
  test('活动版的 duration 是 2~30 秒可选、默认 30 秒', () => {
    const params = parametersByName(SEEDANCE25_HUODONG)
    const duration = params.duration
    assert.ok(duration, '活动版必须列出 duration 参数')
    assert.equal(
      duration.defaultValue,
      30,
      '活动版 duration 默认值应是 30（不传时长时后端按 30 秒跑）'
    )
    assert.equal(
      duration.range,
      '2 ~ 30',
      '2026-11-11 起活动版时长放开 2-30 秒 —— 参数表必须显示区间，否则客户不知道能传短时长'
    )
    assert.equal(
      duration.enumValues,
      undefined,
      '活动版 duration 是区间不是枚举'
    )
  })

  test('活动版的 resolution 只有 720P 一个取值', () => {
    const params = parametersByName(SEEDANCE25_HUODONG)
    const resolution = params.resolution
    assert.ok(resolution, '活动版必须列出 resolution 参数')
    assert.deepEqual(
      resolution.enumValues,
      ['720P'],
      '活动版只支持 720P —— 1080P 成本是 1.33 倍，后端直接 400'
    )
    assert.equal(resolution.defaultValue, '720P')
  })

  // 母本：按秒计费、时长可选、分辨率 720P/1080P 可选。
  test('母本的 duration 是 2~30 秒可选', () => {
    const params = parametersByName(SEEDANCE25_TEHUI)
    assert.equal(
      params.duration?.range,
      '2 ~ 30',
      '母本按秒计费、时长可选，不能写成固定 30'
    )
    assert.equal(params.duration?.defaultValue, undefined)
  })

  test('母本的 resolution 只有 720P/1080P（prime 档位），默认 720P', () => {
    const params = parametersByName(SEEDANCE25_TEHUI)
    assert.deepEqual(
      params.resolution?.enumValues,
      ['720P', '1080P'],
      '母本是 prime 别名，不接受 480P'
    )
    assert.equal(
      params.resolution?.defaultValue,
      '720P',
      '母本默认取最低档 720P'
    )
  })

  // 参数表里的档位必须与促销族定义一致（单一来源）。
  test('参数表的固定档与促销族定义一致', () => {
    const spec = getSeedance25SampleSpec(SEEDANCE25_HUODONG)
    assert.ok(spec)
    const params = parametersByName(SEEDANCE25_HUODONG)
    assert.equal(params.duration?.defaultValue, spec.seconds)
    assert.deepEqual(params.resolution?.enumValues, [spec.resolution])
  })
})
