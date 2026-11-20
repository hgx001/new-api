/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY, without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import type { TaskHealthModel } from '@/features/performance-metrics/types'

import { shouldShowTaskHealth, splitTaskDuration } from './task-health'

function makeHealth(overrides: Partial<TaskHealthModel>): TaskHealthModel {
  return {
    model_name: 'wan3.0-video-tj',
    request_count: 1,
    success_count: 1,
    failure_count: 0,
    success_rate: 100,
    avg_duration_ms: 60_000,
    buckets: [],
    ...overrides,
  }
}

describe('shouldShowTaskHealth', () => {
  test('没有健康度数据（广场上从未有单）→ 不渲染整块', () => {
    assert.equal(shouldShowTaskHealth(undefined), false)
  })

  test('窗口内零样本 → 不渲染，而不是画 24 个灰块 + 「暂无完成请求」', () => {
    // 后端目前不会返回零样本条目（在途任务不计入结果），这里是显式契约：
    // 即使将来聚合口径变化把零样本带出来，界面也不该出现一块「什么都没说」的区域。
    assert.equal(
      shouldShowTaskHealth(
        makeHealth({
          request_count: 0,
          success_count: 0,
          failure_count: 0,
          success_rate: null,
          avg_duration_ms: 0,
        })
      ),
      false
    )
  })

  test('有已完成样本（哪怕全失败）→ 渲染，0% 也是有效信息', () => {
    assert.equal(
      shouldShowTaskHealth(
        makeHealth({
          request_count: 3,
          success_count: 0,
          failure_count: 3,
          success_rate: 0,
          avg_duration_ms: 0,
        })
      ),
      true
    )
  })

  test('仅成功也算样本', () => {
    assert.equal(shouldShowTaskHealth(makeHealth({})), true)
  })
})

describe('splitTaskDuration', () => {
  test('0 与负数视为没有耗时', () => {
    assert.equal(splitTaskDuration(0), null)
    assert.equal(splitTaskDuration(-1), null)
  })

  test('分秒拆分', () => {
    assert.deepEqual(splitTaskDuration(1_004_000), {
      hours: 0,
      minutes: 16,
      seconds: 44,
    })
  })
})
