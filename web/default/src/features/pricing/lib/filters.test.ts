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

import { SORT_OPTIONS } from '../constants'
import type { PricingModel } from '../types'
import { sortModels } from './filters'

function pricingModel(
  modelName: string,
  price: number,
  quotaType = 1
): PricingModel {
  return {
    id: 1,
    model_name: modelName,
    quota_type: quotaType,
    model_ratio: quotaType === 0 ? price : 0,
    completion_ratio: 0,
    model_price: quotaType === 1 ? price : undefined,
    enable_groups: ['default'],
  }
}

function names(models: PricingModel[]): string[] {
  return models.map((m) => m.model_name)
}

describe('sortModels 置顶', () => {
  // 客美两个 WAN3 档位是运营指定的首屏推荐位。字母序上 'prime' < 'tj'，
  // 不置顶会被 prime-tj 压住，而 tj 更便宜、更该先被看到。
  test('置顶模型按名单顺序排在最前（字母序本应相反）', () => {
    const models = [
      pricingModel('wan3.0-video-prime-tj', 0.96),
      pricingModel('wan3.0-video-tj', 0.552),
      pricingModel('hailuo-h3', 0.3),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'wan3.0-video-tj',
      'wan3.0-video-prime-tj',
      'hailuo-h3',
    ])
  })

  test('未置顶模型之间仍按名称排序', () => {
    const models = [
      pricingModel('z-model', 1),
      pricingModel('a-model', 1),
      pricingModel('m-model', 1),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'a-model',
      'm-model',
      'z-model',
    ])
  })

  test('价格排序不受置顶影响', () => {
    // 用户主动按价格排序时不该被置顶项打断，否则排序看起来是错的。
    const models = [
      pricingModel('wan3.0-video-tj', 0.552),
      pricingModel('cheap-model', 0.01),
      pricingModel('pricey-model', 9.9),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.PRICE_LOW)), [
      'cheap-model',
      'wan3.0-video-tj',
      'pricey-model',
    ])
    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.PRICE_HIGH)), [
      'pricey-model',
      'wan3.0-video-tj',
      'cheap-model',
    ])
  })

  test('置顶模型不在当前列表时不产生空位或报错', () => {
    const models = [pricingModel('b-model', 1), pricingModel('a-model', 1)]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'a-model',
      'b-model',
    ])
  })

  test('不修改入参数组', () => {
    const models = [
      pricingModel('wan3.0-video-prime-tj', 0.96),
      pricingModel('wan3.0-video-tj', 0.552),
    ]
    const original = names(models)

    sortModels(models, SORT_OPTIONS.NAME)

    assert.deepEqual(names(models), original)
  })
})
