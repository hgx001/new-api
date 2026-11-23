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
  quotaType = 1,
  sortWeight?: number
): PricingModel {
  return {
    id: 1,
    model_name: modelName,
    quota_type: quotaType,
    model_ratio: quotaType === 0 ? price : 0,
    completion_ratio: 0,
    model_price: quotaType === 1 ? price : undefined,
    enable_groups: ['default'],
    sort_weight: sortWeight,
  }
}

function names(models: PricingModel[]): string[] {
  return models.map((m) => m.model_name)
}

describe('sortModels 置顶（后端 sort_weight）', () => {
  // 置顶已改成后端数据驱动：新建模型卡时自动取 MAX+1，新上架的天然排在最前。
  // 前端不再持有名单 —— 这几条用例守的是「权重优先、其余仍按名称」这条规则。
  test('权重越大越靠前（字母序本该相反）', () => {
    const models = [
      pricingModel('hailuo-h3', 0.3, 1, 0),
      pricingModel('wan3.0-video-tj', 0.552, 1, 2),
      pricingModel('wan3.0-video-prime-tj', 0.96, 1, 1),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'wan3.0-video-tj',
      'wan3.0-video-prime-tj',
      'hailuo-h3',
    ])
  })

  test('新上架模型（权重最高）排在其他置顶模型之前', () => {
    const models = [
      pricingModel('wan3.0-video-tj', 0.552, 1, 2),
      pricingModel('T3-seedance-2.5', 4, 1, 3),
      pricingModel('wan3.0-video-prime-tj', 0.96, 1, 1),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'T3-seedance-2.5',
      'wan3.0-video-tj',
      'wan3.0-video-prime-tj',
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

  test('权重为 0 / 缺失 / 负数都当未置顶（不能因为脏数据把它顶到前面）', () => {
    const models = [
      pricingModel('b-model', 1),
      pricingModel('neg-model', 1, 1, -3),
      pricingModel('a-model', 1),
      pricingModel('zero-model', 1, 1, 0),
    ]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'a-model',
      'b-model',
      'neg-model',
      'zero-model',
    ])
  })

  test('价格排序不受权重影响', () => {
    // 用户主动按价格排序时不该被置顶项打断，否则排序看起来是错的。
    const models = [
      pricingModel('wan3.0-video-tj', 0.552, 1, 2),
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

  test('当前列表里没有置顶模型时不产生空位或报错', () => {
    const models = [pricingModel('b-model', 1), pricingModel('a-model', 1)]

    assert.deepEqual(names(sortModels(models, SORT_OPTIONS.NAME)), [
      'a-model',
      'b-model',
    ])
  })

  test('不修改入参数组', () => {
    const models = [
      pricingModel('wan3.0-video-prime-tj', 0.96, 1, 1),
      pricingModel('wan3.0-video-tj', 0.552, 1, 2),
    ]
    const original = names(models)

    sortModels(models, SORT_OPTIONS.NAME)

    assert.deepEqual(names(models), original)
  })
})
